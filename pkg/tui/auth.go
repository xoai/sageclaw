package tui

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh/terminal"
)

// Bootstrap handles auth before bubbletea starts.
// Runs in plain terminal mode (no bubbletea event loop).
// Returns nil when authenticated and ready to proceed.
func Bootstrap(client *TUIClient) error {
	state, err := client.CheckAuth()
	if err != nil {
		return fmt.Errorf("cannot reach server: %w", err)
	}

	switch state.State {
	case "ready":
		return nil // Already authenticated or no auth configured.

	case "setup":
		fmt.Print("First-time setup. Create a password: ")
		pw, err := readPassword()
		if err != nil {
			return fmt.Errorf("reading password: %w", err)
		}
		if err := client.Setup(pw); err != nil {
			return fmt.Errorf("setup failed: %w", err)
		}
		fmt.Println("\nPassword set. Logged in.")
		return nil

	case "login":
		fmt.Print("Password: ")
		pw, err := readPassword()
		if err != nil {
			return fmt.Errorf("reading password: %w", err)
		}
		if err := client.Login(pw); err != nil {
			return fmt.Errorf("login failed: %w", err)
		}
		fmt.Println("\nLogged in.")
		return nil

	default:
		return fmt.Errorf("unexpected auth state: %s", state.State)
	}
}

// readPassword reads a password from the terminal without echoing.
func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if terminal.IsTerminal(fd) {
		pw, err := terminal.ReadPassword(fd)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(pw)), nil
	}
	// Fallback for non-terminal (piped input).
	var pw string
	_, err := fmt.Scanln(&pw)
	return strings.TrimSpace(pw), err
}
