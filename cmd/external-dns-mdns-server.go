// Command external-dns-mdns-server runs an ExternalDNS webhook provider
// that is backed by an in-process mDNS (RFC 6762) responder instead of a
// real DNS API. ExternalDNS computes desired records from Kubernetes
// Service/Ingress objects and POSTs them to this webhook; this process
// answers LAN clients' mDNS queries from that record set.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/nvalembois/external-dns-mdns-server/pkg/health"
	"github.com/nvalembois/external-dns-mdns-server/pkg/mdns"
	"github.com/nvalembois/external-dns-mdns-server/pkg/store"
	"github.com/nvalembois/external-dns-mdns-server/pkg/webhook"
)

func main() {
	// command args
	var (
		listenAddr   = flag.String("listen-address", "127.0.0.1:8888", "address the ExternalDNS webhook server listens on (loopback only — never expose off-host)")
		domainFilter = flag.String("domain-filter", "local", "comma-separated list of domains this provider accepts records for")
		iface        = flag.String("interface", "", "network interface to bind mDNS multicast to (empty = OS default)")
	)
	flag.Parse()

	// initialize records store
	s := store.New()

	// start mdns responder
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

	// initialize webhook responder
	filters := strings.Split(*domainFilter, ",")
	ws := webhook.NewServer(s, filters)
	log.Printf("ExternalDNS webhook server initialized for domains %v", filters)

	// start http server
	mux := http.NewServeMux()
	ws.Routes(mux)
	health.Routes(mux)

	srv := &http.Server{
		Addr: *listenAddr,
		// Good practice to set timeouts to avoid Slowloris attacks.
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      mux,
	}

	// Run our server in a goroutine so that it doesn't block.
	go func() {
		log.Printf("ExternalDNS webhook server listening on %s", *listenAddr)
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("webhook server stopped: %v", err)
		}
	}()

	// Handle shutdown
	c := make(chan os.Signal, 1)
	// We'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
	// SIGKILL, SIGQUIT or SIGTERM (Ctrl+/) will not be caught.
	signal.Notify(c, os.Interrupt)

	// Block until we receive our signal.
	<-c

	// Create a deadline to wait for.
	var wait time.Duration
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	// Doesn't block if no connections, but will otherwise wait
	// until the timeout deadline.
	srv.Shutdown(ctx)
	// Optionally, you could run srv.Shutdown in a goroutine and block on
	// <-ctx.Done() if your application should wait for other services
	// to finalize based on context cancellation.
	log.Println("shutting down")

}
