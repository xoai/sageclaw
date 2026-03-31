package sqlite

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/xoai/sageclaw/pkg/store"
	_ "modernc.org/sqlite"
)

// Store is the concrete SQLite storage backend.
type Store struct {
	db *sql.DB
}

// New opens a SQLite database and runs migrations.
func New(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Apply pragmas for performance and correctness.
	// Note: these apply to the connection that runs them. With Go's pool,
	// new connections won't inherit them. We mitigate this by using a
	// generous busy_timeout and WAL mode (set once, persists in the file).
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=10000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA cache_size=2000",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting pragma %q: %w", p, err)
		}
	}

	// Keep pool small to reduce connections without busy_timeout.
	// 2 allows one reader + one writer without deadlock.
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection for direct access.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Compile-time interface check.
var _ store.Store = (*Store)(nil)

func (s *Store) migrate() error {
	// Create migrations tracking table.
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		name TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	// Read migration files.
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("reading migrations directory: %w", err)
	}

	// Sort by filename to ensure order.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		// Check if already applied.
		var count int
		err := s.db.QueryRow("SELECT COUNT(*) FROM _migrations WHERE name = ?", entry.Name()).Scan(&count)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", entry.Name(), err)
		}
		if count > 0 {
			continue
		}

		// Read and execute migration.
		content, err := fs.ReadFile(migrationsFS, "migrations/"+entry.Name())
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", entry.Name(), err)
		}

		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction for %s: %w", entry.Name(), err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("executing migration %s: %w", entry.Name(), err)
		}

		if _, err := tx.Exec("INSERT INTO _migrations (name) VALUES (?)", entry.Name()); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording migration %s: %w", entry.Name(), err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// AppliedMigrations returns the list of applied migration names.
func (s *Store) AppliedMigrations() ([]string, error) {
	rows, err := s.db.Query("SELECT name FROM _migrations ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		names = append(names, name)
	}
	return names, nil
}

// TotalMigrations returns the total number of migration files.
func (s *Store) TotalMigrations() int {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			count++
		}
	}
	return count
}
