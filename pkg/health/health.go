// Package health implements the health HTTP endpoint:
package health

import (
	"net/http"
)

// Routes registers all webhook endpoints on the given mux.
func Routes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", handleHealthz)
}

func handleHealthz(rw http.ResponseWriter, r *http.Request) {
	rw.WriteHeader(http.StatusOK)
	_, _ = rw.Write([]byte("ok"))
}
