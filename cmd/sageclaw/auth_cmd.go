package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xoai/sageclaw/pkg/auth"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

func runAuthCommand(args []string) error {
	if len(args) == 0 {
		printAuthHelp()
		return nil
	}

	switch args[0] {
	case "setup-2fa":
		return runAuthSetup2FA()
	case "reset-2fa":
		return runAuthReset2FA()
	case "--help", "-h", "help":
		printAuthHelp()
		return nil
	default:
		printAuthHelp()
		return fmt.Errorf("unknown auth command: %s", args[0])
	}
}

func runAuthSetup2FA() error {
	dbPath := envOrDefault("SAGECLAW_DB", filepath.Join("data", "sageclaw.db"))
	store, err := sqlite.New(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer store.Close()

	encKey, err := loadOrGenerateEncKey(store.DB())
	if err != nil {
		return fmt.Errorf("encryption key: %w", err)
	}

	totp := auth.NewTOTP(store.DB(), encKey)

	if totp.IsEnabled() {
		fmt.Println("2FA is already enabled.")
		fmt.Println("Use 'sageclaw auth reset-2fa' to disable it first.")
		return nil
	}

	fmt.Print("Enter your password: ")
	password := readLine()

	secret, uri, err := totp.Setup(password)
	if err != nil {
		return fmt.Errorf("2FA setup failed: %w", err)
	}

	fmt.Println()
	fmt.Println("─────────────────────────────────────────")
	fmt.Println("2FA Setup Complete")
	fmt.Println()
	fmt.Printf("Secret: %s\n", secret)
	fmt.Println()
	fmt.Println("Add to your authenticator app:")
	fmt.Printf("  %s\n", uri)
	fmt.Println()
	fmt.Println("Or scan the QR code from the dashboard.")
	fmt.Println("─────────────────────────────────────────")
	fmt.Println()
	fmt.Println("Important: save the secret in a safe place.")
	fmt.Println("You'll need it if you lose access to your authenticator.")

	return nil
}

func runAuthReset2FA() error {
	dbPath := envOrDefault("SAGECLAW_DB", filepath.Join("data", "sageclaw.db"))
	store, err := sqlite.New(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer store.Close()

	encKey, err := loadOrGenerateEncKey(store.DB())
	if err != nil {
		return fmt.Errorf("encryption key: %w", err)
	}

	totp := auth.NewTOTP(store.DB(), encKey)

	if !totp.IsEnabled() {
		fmt.Println("2FA is not enabled.")
		return nil
	}

	fmt.Print("Enter your password to disable 2FA: ")
	password := readLine()

	if err := totp.Disable(password); err != nil {
		return fmt.Errorf("2FA reset failed: %w", err)
	}

	fmt.Println("2FA has been disabled.")
	return nil
}

func readLine() string {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

func printAuthHelp() {
	fmt.Println(`sageclaw auth — Authentication Management

Usage:
  sageclaw auth setup-2fa    Enable TOTP two-factor authentication
  sageclaw auth reset-2fa    Disable TOTP (requires password)
  sageclaw auth --help       Show this help

Two-factor authentication adds a TOTP code requirement to dashboard login.
Use any authenticator app (Google Authenticator, Authy, etc).

Recommended when using the tunnel to expose your dashboard to the internet.`)
}
