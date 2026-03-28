package rpc

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/tool"

	_ "modernc.org/sqlite"
)

func TestHandleConsentResponse_NonceFormat(t *testing.T) {
	var gotNonce, gotTier string
	var gotGranted bool

	srv := &Server{
		consentHandler: func(nonce string, granted bool, tier string) error {
			gotNonce = nonce
			gotGranted = granted
			gotTier = tier
			return nil
		},
	}

	body := `{"nonce":"abc123","granted":true,"tier":"always"}`
	req := httptest.NewRequest("POST", "/api/consent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleConsentResponse(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if gotNonce != "abc123" {
		t.Errorf("nonce = %q, want abc123", gotNonce)
	}
	if !gotGranted {
		t.Error("expected granted=true")
	}
	if gotTier != "always" {
		t.Errorf("tier = %q, want always", gotTier)
	}
}

func TestHandleConsentResponse_NonceDeny(t *testing.T) {
	var gotGranted bool

	srv := &Server{
		consentHandler: func(nonce string, granted bool, tier string) error {
			gotGranted = granted
			return nil
		},
	}

	body := `{"nonce":"abc123","granted":false,"tier":"deny"}`
	req := httptest.NewRequest("POST", "/api/consent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleConsentResponse(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if gotGranted {
		t.Error("expected granted=false for deny")
	}
}

func TestHandleConsentResponse_DefaultTierOnce(t *testing.T) {
	var gotTier string

	srv := &Server{
		consentHandler: func(nonce string, granted bool, tier string) error {
			gotTier = tier
			return nil
		},
	}

	// No tier specified — should default to "once".
	body := `{"nonce":"abc123","granted":true}`
	req := httptest.NewRequest("POST", "/api/consent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleConsentResponse(w, req)

	if gotTier != "once" {
		t.Errorf("tier = %q, want 'once' (default)", gotTier)
	}
}

func TestHandleConsentResponse_NoNonce(t *testing.T) {
	srv := &Server{}

	body := `{"group":"runtime","granted":true}`
	req := httptest.NewRequest("POST", "/api/consent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleConsentResponse(w, req)

	// Without a nonce or consent handler, should return error.
	if w.Code == http.StatusOK {
		t.Error("should not return 200 without consent handler")
	}
}

func TestHandleConsentResponse_MissingBoth(t *testing.T) {
	srv := &Server{}

	body := `{"granted":true}`
	req := httptest.NewRequest("POST", "/api/consent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handleConsentResponse(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing nonce and group", w.Code)
	}
}

func TestHandleConsentGrants_Empty(t *testing.T) {
	srv := &Server{} // No consent store.

	req := httptest.NewRequest("GET", "/api/consent/grants", nil)
	w := httptest.NewRecorder()

	srv.handleConsentGrants(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var grants []any
	json.Unmarshal(w.Body.Bytes(), &grants)
	if len(grants) != 0 {
		t.Errorf("expected empty array, got %d items", len(grants))
	}
}

func TestHandleConsentGrants_WithGrants(t *testing.T) {
	db := setupTestConsentDB(t)
	cs := tool.NewPersistentConsentStore(db)
	cs.GrantAlways("owner1", "telegram", "runtime")
	cs.GrantAlways("owner1", "telegram", "fs")

	srv := &Server{consentStore: cs}

	req := httptest.NewRequest("GET", "/api/consent/grants?owner_id=owner1&platform=telegram", nil)
	w := httptest.NewRecorder()

	srv.handleConsentGrants(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var grants []tool.ConsentGrant
	json.Unmarshal(w.Body.Bytes(), &grants)
	if len(grants) != 2 {
		t.Errorf("expected 2 grants, got %d", len(grants))
	}
}

func TestHandleConsentRevokeGrant(t *testing.T) {
	db := setupTestConsentDB(t)
	cs := tool.NewPersistentConsentStore(db)
	cs.GrantAlways("owner1", "telegram", "runtime")

	grants, _ := cs.ListGrants("owner1", "telegram")
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	grantID := grants[0].ID

	srv := &Server{consentStore: cs}

	req := httptest.NewRequest("DELETE", "/api/consent/grants/"+grantID, nil)
	w := httptest.NewRecorder()

	srv.handleConsentRevokeGrant(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	// Verify grant was revoked.
	remaining, _ := cs.ListGrants("owner1", "telegram")
	if len(remaining) != 0 {
		t.Errorf("expected 0 grants after revoke, got %d", len(remaining))
	}
}

func TestHandleConsentRevokeGrant_NotFound(t *testing.T) {
	db := setupTestConsentDB(t)
	cs := tool.NewPersistentConsentStore(db)

	srv := &Server{consentStore: cs}

	req := httptest.NewRequest("DELETE", "/api/consent/grants/nonexistent", nil)
	w := httptest.NewRecorder()

	srv.handleConsentRevokeGrant(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// setupTestConsentDB creates an in-memory SQLite DB with consent_grants table.
func setupTestConsentDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE consent_grants (
			id          TEXT PRIMARY KEY,
			owner_id    TEXT NOT NULL,
			platform    TEXT NOT NULL,
			tool_group  TEXT NOT NULL,
			granted_at  TEXT NOT NULL DEFAULT (datetime('now')),
			revoked_at  TEXT,
			UNIQUE(owner_id, platform, tool_group)
		)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}
