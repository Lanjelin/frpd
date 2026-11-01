package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Lanjelin/frpd/internal/config"
	"github.com/Lanjelin/frpd/internal/frps"
	"github.com/Lanjelin/frpd/internal/store"
	"github.com/Lanjelin/frpd/internal/web"
)

func main() {
	configPath := flag.String("c", config.DefaultPath(), "path to frpd.toml configuration")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("frpd", buildVersion())
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	httpClient := &http.Client{Timeout: 10 * time.Second}
	server, err := web.New(cfg, httpClient)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("frpd %s listening on %s", buildVersion(), listener.Addr())
	log.Printf("%d FRPS server(s), %d FRPC instance(s) configured", len(cfg.FRPSServers), len(cfg.Instances))
	warnIfExposed(cfg.Username, listener.Addr())
	go verifyConnections(cfg, httpClient)

	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()
	select {
	case err := <-serveErr:
		log.Fatal(err)
	case <-stop.Done():
	}
	log.Printf("shutting down")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func verifyConnections(cfg config.Config, httpClient *http.Client) {
	var probes sync.WaitGroup
	for _, server := range cfg.FRPSServers {
		probes.Add(1)
		go func(server config.FRPSServer) {
			defer probes.Done()
			if _, err := frps.New(server, httpClient).ServerInfo(context.Background()); err != nil {
				log.Printf("FRPS %q at %s unavailable: %v", server.Name, server.URL, err)
				return
			}
			log.Printf("connected to FRPS %q at %s", server.Name, server.URL)
		}(server)
	}
	for _, instance := range cfg.Instances {
		probes.Add(1)
		go func(instance config.Instance) {
			defer probes.Done()
			if _, err := store.New(instance, httpClient).ListProxies(context.Background()); err != nil {
				log.Printf("FRPC %q at %s unavailable: %v", instance.Name, instance.URL, err)
				return
			}
			log.Printf("connected to FRPC %q at %s", instance.Name, instance.URL)
		}(instance)
	}
	probes.Wait()
}
