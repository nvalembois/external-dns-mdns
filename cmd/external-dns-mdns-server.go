// Command external-dns-mdns-server runs an ExternalDNS webhook provider
// that is backed by an in-process mDNS (RFC 6762) responder instead of a
// real DNS API. ExternalDNS computes desired records from Kubernetes
// Service/Ingress objects and POSTs them to this webhook; this process
// answers LAN clients' mDNS queries from that record set.
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"

	"github.com/nvalembois/external-dns-mdns-server/pkg/mdns"
	"github.com/nvalembois/external-dns-mdns-server/pkg/store"
	"github.com/nvalembois/external-dns-mdns-server/pkg/webhook"
)

func main() {
	var (
		listenAddr   = flag.String("listen-address", "127.0.0.1:8888", "address the ExternalDNS webhook server listens on (loopback only — never expose off-host)")
		domainFilter = flag.String("domain-filter", "local", "comma-separated list of domains this provider accepts records for")
		iface        = flag.String("interface", "", "network interface to bind mDNS multicast to (empty = OS default)")
	)
	flag.Parse()

	s := store.New()

	responder, err := mdns.New(s, *iface)
	if err != nil {
		log.Fatalf("failed to start mDNS responder: %v", err)
	}
	go func() {
		log.Printf("mDNS responder listening on %s (iface=%q)", mdns.Addr, *iface)
		if err := responder.Serve(); err != nil {
			log.Fatalf("mDNS responder stopped: %v", err)
		}
	}()

	filters := strings.Split(*domainFilter, ",")
	ws := webhook.NewServer(s, filters)

	mux := http.NewServeMux()
	ws.Routes(mux)

	log.Printf("ExternalDNS webhook server listening on %s (domain-filter=%v)", *listenAddr, filters)
	if err := http.ListenAndServe(*listenAddr, mux); err != nil {
		log.Fatalf("webhook server stopped: %v", err)
	}
}
