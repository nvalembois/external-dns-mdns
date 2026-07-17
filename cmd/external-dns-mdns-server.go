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
		webhookListenAddr = flag.String("webhook-listen-address", "127.0.0.1:8888", "address the ExternalDNS webhook server listens on (loopback only — never expose off-host)")
		healthListenAddr  = flag.String("health-listen-address", "0.0.0.0:8080", "address the ExternalDNS health server listens on (loopback only — never expose off-host)")
		domainFilter      = flag.String("domain-filter", "local", "comma-separated list of domains this provider accepts records for")
		iface             = flag.String("interface", "", "network interface to bind mDNS multicast to (empty = OS default)")
		wait              = flag.Duration("graceful-timeout", time.Second*60, "the duration for which the server gracefully wait for existing connections to finish - e.g. 15s or 1m")
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
	// start webhook http server
	webhookSrv := startHttpServer("webhook", webhookListenAddr, ws.Routes)

	// start health http server
	healthSrv := startHttpServer("health", healthListenAddr, health.Routes)

	// Wait for graceful shutdown signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	// Block until we receive our signal.
	<-c

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), *wait)
	defer cancel()

	// Shutdown servers
	log.Println("shutting down")
	webhookSrv.Shutdown(ctx)
	healthSrv.Shutdown(ctx)

	log.Println("end")
}

func startHttpServer(name string, listenAddr *string, routes func(mux *http.ServeMux)) *http.Server {
	mux := http.NewServeMux()
	routes(mux)
	srv := &http.Server{
		Addr:         *listenAddr,
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      mux,
	}

	// Run our server in a goroutine so that it doesn't block.
	go func() {
		log.Printf("ExternalDNS %s server listening on %s", name, *listenAddr)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("%s server stopped: %v", name, err)
		}
	}()

	return srv
}
