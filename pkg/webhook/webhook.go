// Package webhook implements the ExternalDNS webhook provider HTTP contract:
// https://github.com/kubernetes-sigs/external-dns/blob/master/provider/webhook/webhook.go
package webhook

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/nvalembois/external-dns-mdns-server/pkg/store"
)

const (
	HeaderContentType = "Content-Type"
	MediaType         = "application/external.dns.webhook+json;version=1"
)

// domainFilterResponse is returned on GET / during the negotiation phase.
// ExternalDNS calls this once at startup to learn which domains this
// provider is willing to accept records for.
type domainFilterResponse struct {
	Filters []string `json:"filters"`
}

type Server struct {
	store        *store.Store
	domainFilter []string
}

func NewServer(s *store.Store, domainFilter []string) *Server {
	return &Server{store: s, domainFilter: domainFilter}
}

// Routes registers all webhook endpoints on the given mux.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/", s.handleNegotiate)
	mux.HandleFunc("/records", s.handleRecords)
	mux.HandleFunc("/adjustendpoints", s.handleAdjustEndpoints)
	mux.HandleFunc("/healthz", s.handleHealthz)
}

// GET / — negotiation. ExternalDNS calls this at startup to confirm the
// webhook is alive and to fetch the DomainFilter this provider accepts.
func (s *Server) handleNegotiate(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rw.Header().Set(HeaderContentType, MediaType)
	writeJSON(rw, domainFilterResponse{Filters: s.domainFilter})
}

// GET /records — return current state so ExternalDNS can diff against
// what it thinks should exist.
// POST /records — apply a Changes payload (Create/UpdateOld/UpdateNew/Delete).
func (s *Server) handleRecords(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rw.Header().Set(HeaderContentType, MediaType)
		writeJSON(rw, s.store.List())

	case http.MethodPost:
		var changes store.Changes
		if err := json.NewDecoder(r.Body).Decode(&changes); err != nil {
			http.Error(rw, "invalid changes payload: "+err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("applying changes: +%d create, +%d update, -%d delete",
			len(changes.Create), len(changes.UpdateNew), len(changes.Delete))
		s.store.Apply(changes)
		rw.WriteHeader(http.StatusNoContent)

	default:
		rw.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// POST /adjustendpoints — optional hook ExternalDNS calls before planning,
// letting the provider normalize/filter endpoints (e.g. drop unsupported
// record types, clamp TTLs) before they're diffed. Here we just pass
// endpoints through unchanged, which is a valid no-op implementation.
func (s *Server) handleAdjustEndpoints(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var endpoints []*store.Endpoint
	if err := json.NewDecoder(r.Body).Decode(&endpoints); err != nil {
		http.Error(rw, "invalid endpoints payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	rw.Header().Set(HeaderContentType, MediaType)
	writeJSON(rw, endpoints)
}

func (s *Server) handleHealthz(rw http.ResponseWriter, r *http.Request) {
	rw.WriteHeader(http.StatusOK)
	_, _ = rw.Write([]byte("ok"))
}

func writeJSON(rw http.ResponseWriter, v interface{}) {
	if err := json.NewEncoder(rw).Encode(v); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}
