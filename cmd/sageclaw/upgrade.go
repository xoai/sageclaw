package main

import (
	"fmt"
	"os"
	"time"

	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

func runUpgrade() {
	fmt.Println("SageClaw Upgrade")
	fmt.Println("================")
	fmt.Println()

	dbPath := defaultDBPath()

	// Check if database exists.
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Println("No database found. Run 'sageclaw' first to create one.")
		return
	}

	// Step 1: Backup.
	fmt.Print("1. Backing up database... ")
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("sageclaw-backup-%s.db", timestamp)
	if err := copyFile(dbPath, backupPath); err != nil {
		fmt.Printf("FAILED: %v\n", err)
		fmt.Println("   Upgrade aborted. Fix the issue and try again.")
		os.Exit(1)
	}
	fmt.Printf("OK (%s)\n", backupPath)

	// Step 2: Run migrations (opening the store applies them automatically).
	fmt.Print("2. Running pending migrations... ")
	store, err := sqlite.New(dbPath)
	if err != nil {
		fmt.Printf("FAILED: %v\n", err)
		fmt.Printf("   Your backup is at %s — restore with: sageclaw restore %s\n", backupPath, backupPath)
		os.Exit(1)
	}

	applied, _ := store.AppliedMigrations()
	total := store.TotalMigrations()
	fmt.Printf("OK (%d/%d migrations applied)\n", len(applied), total)

	// Step 3: Validate.
	fmt.Print("3. Validating database... ")
	var integrity string
	store.DB().QueryRow("PRAGMA integrity_check").Scan(&integrity)
	if integrity != "ok" {
		fmt.Printf("WARNING: %s\n", integrity)
	} else {
		fmt.Println("OK")
	}

	// Step 4: Report.
	fmt.Println()
	fmt.Println("Upgrade complete.")
	fmt.Printf("   Database: %s\n", dbPath)
	fmt.Printf("   Backup:   %s\n", backupPath)
	fmt.Printf("   Migrations: %d applied\n", len(applied))

	if len(applied) > 0 {
		fmt.Println("\n   Applied migrations:")
		for _, name := range applied {
			fmt.Printf("     - %s\n", name)
		}
	}

	store.Close()
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
