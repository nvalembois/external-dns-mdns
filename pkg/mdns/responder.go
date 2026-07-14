// Package mdns implements a minimal RFC 6762 multicast DNS responder that
// answers queries from a shared store.Store, which is kept up to date by
// the webhook package's HTTP handlers.
package mdns

import (
	"log"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/nvalembois/external-dns-mdns-server/pkg/store"
)

const (
	Addr       = "224.0.0.251:5353"
	defaultTTL = 120 * time.Second
)

// Responder answers mDNS queries by looking up records in the shared
// Store. It does not implement probing, conflict detection, or
// cache-flush/goodbye-packet semantics — see the accompanying notes on
// when that matters for your use case.
type Responder struct {
	store *store.Store
	iface *net.Interface
	conn  *net.UDPConn
}

// New binds to the mDNS multicast group on the given interface. ifaceName
// may be empty to let the OS pick a default multicast-capable interface,
// but on a multi-homed node (typical for a hostNetwork pod) you should
// pass the interface facing the target LAN explicitly.
func New(s *store.Store, ifaceName string) (*Responder, error) {
	var iface *net.Interface
	if ifaceName != "" {
		i, err := net.InterfaceByName(ifaceName)
		if err != nil {
			return nil, err
		}
		iface = i
	}

	group, err := net.ResolveUDPAddr("udp4", Addr)
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenMulticastUDP("udp4", iface, group)
	if err != nil {
		return nil, err
	}
	// mDNS packets are small; this is generous headroom for a handful of RRs.
	_ = conn.SetReadBuffer(65536)

	return &Responder{store: s, iface: iface, conn: conn}, nil
}

// Serve blocks, reading and answering queries until the connection is closed.
func (r *Responder) Serve() error {
	buf := make([]byte, 65536)
	for {
		n, src, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		msg := new(dns.Msg)
		if err := msg.Unpack(buf[:n]); err != nil {
			continue // not a well-formed DNS/mDNS packet, ignore
		}
		if msg.Response || len(msg.Question) == 0 {
			continue // ignore other responders' announcements, and empty queries
		}
		r.handleQuery(msg, src)
	}
}

func (r *Responder) Close() error {
	return r.conn.Close()
}

func (r *Responder) handleQuery(query *dns.Msg, src *net.UDPAddr) {
	resp := new(dns.Msg)
	resp.SetReply(query)
	resp.Response = true
	resp.Authoritative = true
	// mDNS responses conventionally omit the question section (RFC 6762 §6).
	resp.Question = nil

	answered := false
	unicastRequested := false

	for _, q := range query.Question {
		// The top bit of the qclass field is the mDNS "QU" (unicast-response) bit.
		if q.Qclass&0x8000 != 0 {
			unicastRequested = true
		}
		qclass := q.Qclass &^ 0x8000
		if qclass != dns.ClassINET && qclass != dns.ClassANY {
			continue
		}

		rrs := r.answer(q.Name, dns.TypeToString[q.Qtype])
		if len(rrs) > 0 {
			resp.Answer = append(resp.Answer, rrs...)
			answered = true
		}
	}

	if !answered {
		return // RFC 6762: stay silent rather than send NXDOMAIN-equivalent
	}

	out, err := resp.Pack()
	if err != nil {
		log.Printf("failed to pack mDNS response: %v", err)
		return
	}

	if unicastRequested {
		if _, err := r.conn.WriteToUDP(out, src); err != nil {
			log.Printf("unicast reply failed: %v", err)
		}
		return
	}

	group, _ := net.ResolveUDPAddr("udp4", Addr)
	if _, err := r.conn.WriteToUDP(out, group); err != nil {
		log.Printf("multicast reply failed: %v", err)
	}
}

// answer translates a Store lookup into concrete RRs for the given
// question name+type, handling the couple of record shapes DNS-SD/mDNS
// clients actually ask for.
func (r *Responder) answer(qname, qtype string) []dns.RR {
	name := ensureDot(qname)
	ep, ok := r.store.Lookup(qname, qtype)
	if !ok {
		return nil
	}

	ttl := uint32(defaultTTL.Seconds())
	if ep.RecordTTL > 0 {
		ttl = uint32(ep.RecordTTL)
	}

	var rrs []dns.RR
	for _, target := range ep.Targets {
		var rr dns.RR
		switch strings.ToUpper(qtype) {
		case "A":
			rr = &dns.A{
				Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
				A:   net.ParseIP(target).To4(),
			}
		case "AAAA":
			rr = &dns.AAAA{
				Hdr:  dns.RR_Header{Name: name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttl},
				AAAA: net.ParseIP(target),
			}
		case "PTR":
			rr = &dns.PTR{
				Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: ttl},
				Ptr: ensureDot(target),
			}
		case "TXT":
			rr = &dns.TXT{
				Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: ttl},
				Txt: []string{target},
			}
		case "SRV":
			// Expect target encoded as "priority weight port host",
			// matching how you'd populate it from a Service's ProviderSpecific
			// fields when generating the Endpoint in your ExternalDNS source config.
			rr = parseSRV(name, target, ttl)
		default:
			continue
		}
		if rr != nil {
			rrs = append(rrs, rr)
		}
	}
	return rrs
}

func parseSRV(name, target string, ttl uint32) dns.RR {
	fields := strings.Fields(target)
	if len(fields) != 4 {
		log.Printf("skipping malformed SRV target %q for %s (want 'priority weight port host')", target, name)
		return nil
	}
	parseUint16 := func(s string) uint16 {
		var v uint16
		for _, c := range s {
			if c < '0' || c > '9' {
				return 0
			}
			v = v*10 + uint16(c-'0')
		}
		return v
	}
	return &dns.SRV{
		Hdr:      dns.RR_Header{Name: name, Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: ttl},
		Priority: parseUint16(fields[0]),
		Weight:   parseUint16(fields[1]),
		Port:     parseUint16(fields[2]),
		Target:   ensureDot(fields[3]),
	}
}

func ensureDot(name string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}
