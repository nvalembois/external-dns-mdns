// Package store holds the in-memory record set shared between the
// ExternalDNS webhook HTTP handlers and the mDNS responder. It is the
// single source of truth both sides read from and write to.
package store

import (
	"strings"
	"sync"
)

// Endpoint mirrors ExternalDNS's wire format for a DNS record.
// https://github.com/kubernetes-sigs/external-dns webhook provider contract.
type Endpoint struct {
	DNSName          string                     `json:"dnsName"`
	Targets          []string                   `json:"targets"`
	RecordType       string                     `json:"recordType"`
	SetIdentifier    string                     `json:"setIdentifier,omitempty"`
	RecordTTL        int64                      `json:"recordTTL,omitempty"`
	Labels           map[string]string          `json:"labels,omitempty"`
	ProviderSpecific []ProviderSpecificProperty `json:"providerSpecific,omitempty"`
}

type ProviderSpecificProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Changes mirrors the payload ExternalDNS POSTs to /records.
type Changes struct {
	Create    []*Endpoint `json:"Create,omitempty"`
	UpdateOld []*Endpoint `json:"UpdateOld,omitempty"`
	UpdateNew []*Endpoint `json:"UpdateNew,omitempty"`
	Delete    []*Endpoint `json:"Delete,omitempty"`
}

// Key uniquely identifies a record by name+type (mirrors ExternalDNS's own keying).
type Key struct {
	Name       string
	RecordType string
}

func keyOf(e *Endpoint) Key {
	return Key{
		Name:       strings.ToLower(strings.TrimSuffix(e.DNSName, ".")),
		RecordType: strings.ToUpper(e.RecordType),
	}
}

// Store is the concurrency-safe record table. All access is mutex-guarded
// because the HTTP webhook goroutines and the mDNS UDP read loop both
// touch it concurrently.
type Store struct {
	mu      sync.RWMutex
	records map[Key]*Endpoint
}

func New() *Store {
	return &Store{records: make(map[Key]*Endpoint)}
}

// List returns a snapshot of all current endpoints (used by GET /records).
func (s *Store) List() []*Endpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Endpoint, 0, len(s.records))
	for _, e := range s.records {
		out = append(out, e)
	}
	return out
}

// Apply applies a Changes payload. UpdateOld is only used for matching/logging;
// the new state always wins for a given key.
func (s *Store) Apply(c Changes) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, e := range c.Delete {
		delete(s.records, keyOf(e))
	}
	for _, e := range c.Create {
		s.records[keyOf(e)] = e
	}
	for _, e := range c.UpdateNew {
		s.records[keyOf(e)] = e
	}
}

// Lookup returns the endpoint (if any) matching a queried name+type,
// used by the mDNS responder to answer incoming questions.
func (s *Store) Lookup(name, recordType string) (*Endpoint, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.records[Key{
		Name:       strings.ToLower(strings.TrimSuffix(name, ".")),
		RecordType: strings.ToUpper(recordType),
	}]
	return e, ok
}
