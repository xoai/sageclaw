package agentcfg

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
)

// MigrateFromDB exports existing DB agents to file-based config.
// Only migrates agents that don't already have a folder on disk.
func MigrateFromDB(db *sql.DB, agentsDir string) (int, error) {
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return 0, err
	}

	rows, err := db.QueryContext(context.Background(),
		`SELECT id, name, COALESCE(system_prompt,''), model, max_tokens, tools FROM agents`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	migrated := 0
	for rows.Next() {
		var id, name, prompt, model, tools string
		var maxTokens int
		if err := rows.Scan(&id, &name, &prompt, &model, &maxTokens, &tools); err != nil {
			continue
		}

		agentDir := filepath.Join(agentsDir, id)

		// Skip if folder already exists.
		if _, err := os.Stat(filepath.Join(agentDir, "identity.yaml")); err == nil {
			continue
		}

		cfg := Defaults(id)
		cfg.Identity.Name = name
		cfg.Identity.Model = model
		if maxTokens > 0 {
			cfg.Identity.MaxTokens = maxTokens
		}

		// Best-effort: put the entire system_prompt into soul.md.
		// Users can later split into soul.md + behavior.md manually.
		if prompt != "" {
			cfg.Soul = prompt
		}

		if err := SaveAgent(&cfg, agentDir); err != nil {
			log.Printf("agentcfg: migration failed for %s: %v", id, err)
			continue
		}

		migrated++
		log.Printf("agentcfg: migrated agent %q to %s/", id, agentDir)
	}

	return migrated, nil
}
