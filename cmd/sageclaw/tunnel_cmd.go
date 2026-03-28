package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/xoai/sageclaw/pkg/config"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
	"github.com/xoai/sageclaw/pkg/tunnel"
)

func runTunnelCommand(args []string) error {
	if len(args) == 0 {
		return runTunnelStart()
	}

	switch args[0] {
	case "start":
		return runTunnelStart()
	case "status":
		return runTunnelStatus()
	case "token":
		rotate := len(args) > 1 && args[1] == "--rotate"
		return runTunnelToken(rotate)
	case "stop":
		fmt.Println("Tunnel runs in foreground — press Ctrl+C to stop.")
		return nil
	case "--help", "-h", "help":
		printTunnelHelp()
		return nil
	default:
		printTunnelHelp()
		return fmt.Errorf("unknown tunnel command: %s", args[0])
	}
}

func runTunnelStart() error {
	// Determine port.
	port := 9090
	if p := os.Getenv("SAGECLAW_RPC_ADDR"); p != "" {
		for i := len(p) - 1; i >= 0; i-- {
			if p[i] == ':' {
				if n, err := strconv.Atoi(p[i+1:]); err == nil {
					port = n
				}
				break
			}
		}
	}

	// Load config.
	configDir := envOrDefault("SAGECLAW_CONFIG_DIR", ".")
	cfg, _ := config.Load(configDir)
	if cfg == nil {
		cfg = &config.AppConfig{}
	}

	// Open database.
	dbPath := envOrDefault("SAGECLAW_DB", filepath.Join("data", "sageclaw.db"))
	store, err := sqlite.New(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer store.Close()

	tunnelCfg := tunnel.Config{
		Mode:        cfg.Tunnel.Mode,
		RelayURL:    cfg.Tunnel.RelayURL,
		Token:       cfg.Tunnel.Token,
		AutoWebhook: cfg.Tunnel.AutoWebhook,
		LocalPort:   port,
	}.Defaults()

	fmt.Printf("Starting native tunnel (%s mode)...\n", tunnelCfg.Mode)
	fmt.Printf("Relay: %s\n", tunnelCfg.RelayURL)
	fmt.Printf("Forwarding to localhost:%d\n\n", port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := tunnel.NewClient(store.DB(), tunnelCfg, func(status tunnel.Status) {
		if status.URL != "" {
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("Public URL:  %s\n", status.URL)
			fmt.Println()
			fmt.Println("Webhook URLs (configure in channel settings):")
			fmt.Printf("  whatsapp:  %s/webhook/whatsapp\n", status.URL)
			fmt.Printf("  zalo:      %s/webhook/zalo\n", status.URL)
			fmt.Println("─────────────────────────────────────────")
			fmt.Println()
			fmt.Println("Press Ctrl+C to stop the tunnel.")
		}
		if !status.Running && status.Error != "" {
			fmt.Printf("Tunnel error: %s\n", status.Error)
		}
		if status.Latency > 0 {
			fmt.Printf("Relay latency: %dms\n", status.Latency)
		}
	})
	if err != nil {
		return fmt.Errorf("creating tunnel client: %w", err)
	}

	if err := client.Start(ctx); err != nil {
		return err
	}

	// Wait for interrupt.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("\nStopping tunnel...")
	client.Stop()
	fmt.Println("Tunnel stopped.")
	return nil
}

func runTunnelStatus() error {
	// Open database to check instance ID and token.
	dbPath := envOrDefault("SAGECLAW_DB", filepath.Join("data", "sageclaw.db"))
	store, err := sqlite.New(dbPath)
	if err != nil {
		fmt.Println("tunnel: database not accessible")
		return nil
	}
	defer store.Close()

	instanceID, _ := tunnel.EnsureInstanceID(store.DB())
	token, _ := tunnel.EnsureToken(store.DB())

	fmt.Println("SageClaw Native Tunnel")
	fmt.Println()
	fmt.Printf("  instance ID: %s\n", instanceID)
	if len(token) > 8 {
		fmt.Printf("  token:       %s...%s\n", token[:4], token[len(token)-4:])
	}
	fmt.Println()
	fmt.Println("Run 'sageclaw tunnel' to start the tunnel.")
	return nil
}

func runTunnelToken(rotate bool) error {
	dbPath := envOrDefault("SAGECLAW_DB", filepath.Join("data", "sageclaw.db"))
	store, err := sqlite.New(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer store.Close()

	if rotate {
		tok, err := tunnel.RotateToken(store.DB())
		if err != nil {
			return fmt.Errorf("rotating token: %w", err)
		}
		fmt.Printf("Token rotated: %s\n", tok)
		fmt.Println("Note: restart the tunnel for the new token to take effect.")
		return nil
	}

	tok, err := tunnel.EnsureToken(store.DB())
	if err != nil {
		return fmt.Errorf("loading token: %w", err)
	}
	fmt.Printf("Tunnel token: %s\n", tok)
	return nil
}

func printTunnelHelp() {
	fmt.Println(`sageclaw tunnel — Native Reverse Tunnel

Usage:
  sageclaw tunnel              Start the tunnel (foreground)
  sageclaw tunnel start        Same as above
  sageclaw tunnel status       Show instance ID and token info
  sageclaw tunnel token        Show current tunnel token
  sageclaw tunnel token --rotate  Generate a new token
  sageclaw tunnel --help       Show this help

The native tunnel creates a persistent WebSocket connection to a relay
server, forwarding webhook traffic to your local SageClaw instance.
No external binaries required.

Use the webhook URLs for WhatsApp and Zalo channel configuration.
Telegram and Discord don't need tunnels (they use polling/WebSocket).

The tunnel runs in the foreground. Press Ctrl+C to stop.

Configure in your config YAML:
  tunnel:
    mode: managed          # managed (default) or self-hosted
    auto_start: false      # start tunnel on boot
    auto_webhook: true     # auto-register webhooks`)
}
