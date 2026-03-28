package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xoai/sageclaw/pkg/relay"

	_ "modernc.org/sqlite"
)

func main() {
	// Load config.
	cfgPath := "relay.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := relay.LoadConfig(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("No config file at %s — using defaults", cfgPath)
			cfg = relay.Config{}.Defaults()
		} else {
			log.Fatalf("config: %v", err)
		}
	}

	// Create token store.
	var tokens relay.TokenStore
	if cfg.Auth.Mode == "token" && cfg.Auth.Token != "" {
		// Self-hosted: single shared secret.
		tokens = relay.NewSharedSecretTokenStore(cfg.Auth.Token)
		log.Printf("relay: using shared secret auth")
	} else {
		// Managed: SQLite token store.
		sqlTokens, err := relay.NewSQLiteTokenStore(cfg.DB)
		if err != nil {
			log.Fatalf("token store: %v", err)
		}
		defer sqlTokens.Close()
		tokens = sqlTokens
		log.Printf("relay: using SQLite token store (%s)", cfg.DB)

		// Periodic cleanup.
		go func() {
			for {
				time.Sleep(24 * time.Hour)
				if err := tokens.Cleanup(); err != nil {
					log.Printf("relay: token cleanup: %v", err)
				}
			}
		}()
	}

	// Create relay.
	r := relay.New(cfg, tokens)

	// Create HTTP server.
	srv := &http.Server{
		Addr:         cfg.Listen,
		Handler:      r.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Configure TLS if cert files provided.
	if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			log.Fatalf("TLS: %v", err)
		}
		srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}

	// Start server.
	go func() {
		if srv.TLSConfig != nil {
			log.Printf("relay: listening on %s (TLS)", cfg.Listen)
			if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Fatalf("relay: %v", err)
			}
		} else {
			log.Printf("relay: listening on %s (plain HTTP — use reverse proxy for TLS)", cfg.Listen)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("relay: %v", err)
			}
		}
	}()

	fmt.Printf("sageclaw-relay started\n")
	fmt.Printf("  domain: %s\n", cfg.Domain)
	fmt.Printf("  listen: %s\n", cfg.Listen)
	fmt.Printf("  max tunnels: %d\n", cfg.Limits.MaxTunnels)

	// Wait for interrupt.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Printf("relay: shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Printf("relay: stopped")
}
