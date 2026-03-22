package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/xoai/sageclaw/pkg/tunnel"
)

func runTunnelCommand(args []string) error {
	if len(args) == 0 {
		return runTunnelQuick()
	}

	switch args[0] {
	case "start":
		return runTunnelQuick()
	case "status":
		return runTunnelStatus()
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

func runTunnelQuick() error {
	// Detect cloudflared.
	installed, ver, _ := tunnel.Detect()
	if !installed {
		fmt.Println("cloudflared not found.")
		fmt.Println()
		fmt.Println("Install it:")
		fmt.Println("  " + tunnel.InstallHint())
		fmt.Println()
		fmt.Println("Then run: sageclaw tunnel")
		return fmt.Errorf("cloudflared not installed")
	}

	fmt.Printf("cloudflared found (version %s)\n", ver)

	// Determine port.
	port := 9090
	if p := os.Getenv("SAGECLAW_RPC_ADDR"); p != "" {
		// Extract port from ":9090" or "0.0.0.0:9090"
		for i := len(p) - 1; i >= 0; i-- {
			if p[i] == ':' {
				if n, err := strconv.Atoi(p[i+1:]); err == nil {
					port = n
				}
				break
			}
		}
	}

	fmt.Printf("Creating tunnel to localhost:%d...\n", port)
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t := tunnel.New(port, func(status tunnel.Status) {
		if status.URL != "" {
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("Public URL:           %s\n", status.URL)
			fmt.Println()
			fmt.Println("Webhook URLs:")
			for ch, url := range status.WebhookURLs() {
				fmt.Printf("  %-10s  %s\n", ch+":", url)
			}
			fmt.Println("─────────────────────────────────────────")
			fmt.Println()
			fmt.Println("Copy the webhook URL and paste it in your channel settings.")
			fmt.Println("Press Ctrl+C to stop the tunnel.")
		}
		if !status.Running && status.Error != "" {
			fmt.Printf("Tunnel error: %s\n", status.Error)
		}
	})

	if err := t.StartQuick(ctx); err != nil {
		return err
	}

	// Wait for interrupt.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("\nStopping tunnel...")
	t.Stop()
	fmt.Println("Tunnel stopped.")
	return nil
}

func runTunnelStatus() error {
	installed, ver, path := tunnel.Detect()
	if !installed {
		fmt.Println("cloudflared: not installed")
		fmt.Println("Install: " + tunnel.InstallHint())
		return nil
	}

	fmt.Printf("cloudflared: installed\n")
	fmt.Printf("  version: %s\n", ver)
	fmt.Printf("  path:    %s\n", path)
	fmt.Println()
	fmt.Println("Run 'sageclaw tunnel' to start a quick tunnel.")
	return nil
}

func printTunnelHelp() {
	fmt.Println(`sageclaw tunnel — Manage Cloudflare Tunnel

Usage:
  sageclaw tunnel           Start a quick tunnel (no account needed)
  sageclaw tunnel start     Same as above
  sageclaw tunnel status    Check if cloudflared is installed
  sageclaw tunnel --help    Show this help

The quick tunnel creates a free trycloudflare.com URL that forwards
webhook traffic to your local SageClaw instance. No Cloudflare account
required.

Use the webhook URLs for WhatsApp and Zalo channel configuration.
Telegram and Discord don't need tunnels (they use polling/WebSocket).

The tunnel runs in the foreground. Press Ctrl+C to stop.
The URL changes each time you restart — for a permanent URL,
use 'cloudflared tunnel create' and configure a named tunnel.`)
}
