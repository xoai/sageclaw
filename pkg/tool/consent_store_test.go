package tool

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
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

func TestPersistentConsentStore_NoAutoConsent(t *testing.T) {
	db := setupTestDB(t)
	cs := NewPersistentConsentStore(db)

	// HasConsent no longer auto-consents safe groups — profile handles that.
	if cs.HasConsent("s1", "owner1", "telegram", GroupMemory) {
		t.Error("HasConsent should not auto-consent without explicit grant")
	}
}

func TestPersistentConsentStore_SessionGrant(t *testing.T) {
	db := setupTestDB(t)
	cs := NewPersistentConsentStore(db)

	if cs.HasConsent("s1", "owner1", "telegram", GroupRuntime) {
		t.Error("should not have consent before grant")
	}

	cs.GrantOnce("s1", GroupRuntime)

	if !cs.HasConsent("s1", "owner1", "telegram", GroupRuntime) {
		t.Error("should have consent after GrantOnce")
	}

	// Different session should not have consent.
	if cs.HasConsent("s2", "owner1", "telegram", GroupRuntime) {
		t.Error("different session should not have consent")
	}
}

func TestPersistentConsentStore_PersistentGrant(t *testing.T) {
	db := setupTestDB(t)
	cs := NewPersistentConsentStore(db)

	err := cs.GrantAlways("owner1", "telegram", GroupFS)
	if err != nil {
		t.Fatalf("GrantAlways: %v", err)
	}

	// Any session for this owner+platform should have consent.
	if !cs.HasConsent("s1", "owner1", "telegram", GroupFS) {
		t.Error("should have persistent consent")
	}
	if !cs.HasConsent("s2", "owner1", "telegram", GroupFS) {
		t.Error("persistent consent should work across sessions")
	}

	// Different owner should not have consent.
	if cs.HasConsent("s3", "owner2", "telegram", GroupFS) {
		t.Error("different owner should not have consent")
	}

	// Different platform should not have consent.
	if cs.HasConsent("s4", "owner1", "discord", GroupFS) {
		t.Error("different platform should not have consent")
	}
}

func TestPersistentConsentStore_PersistAcrossInstances(t *testing.T) {
	db := setupTestDB(t)

	// First instance grants.
	cs1 := NewPersistentConsentStore(db)
	if err := cs1.GrantAlways("owner1", "telegram", GroupWeb); err != nil {
		t.Fatalf("GrantAlways: %v", err)
	}

	// Second instance (simulates restart) should see the grant.
	cs2 := NewPersistentConsentStore(db)
	if !cs2.HasConsent("new-session", "owner1", "telegram", GroupWeb) {
		t.Error("persistent grant should survive across instances")
	}
}

func TestPersistentConsentStore_Deny_Cooldown(t *testing.T) {
	db := setupTestDB(t)
	cs := NewPersistentConsentStore(db)

	cs.Deny("s1", GroupMCP)

	if !cs.InCooldown("s1", GroupMCP) {
		t.Error("should be in cooldown after deny")
	}
	if cs.HasConsent("s1", "", "", GroupMCP) {
		t.Error("denied group should not have consent")
	}

	// Different session not in cooldown.
	if cs.InCooldown("s2", GroupMCP) {
		t.Error("different session should not be in cooldown")
	}
}

func TestPersistentConsentStore_Revoke(t *testing.T) {
	db := setupTestDB(t)
	cs := NewPersistentConsentStore(db)

	if err := cs.GrantAlways("owner1", "telegram", GroupFS); err != nil {
		t.Fatalf("GrantAlways: %v", err)
	}
	if !cs.HasConsent("s1", "owner1", "telegram", GroupFS) {
		t.Fatal("should have consent after grant")
	}

	if err := cs.Revoke("owner1", "telegram", GroupFS); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if cs.HasConsent("s1", "owner1", "telegram", GroupFS) {
		t.Error("should not have consent after revoke")
	}
}

func TestPersistentConsentStore_ReGrantAfterRevoke(t *testing.T) {
	db := setupTestDB(t)
	cs := NewPersistentConsentStore(db)

	// Grant -> revoke -> re-grant (upsert).
	cs.GrantAlways("owner1", "telegram", GroupFS)
	cs.Revoke("owner1", "telegram", GroupFS)

	if err := cs.GrantAlways("owner1", "telegram", GroupFS); err != nil {
		t.Fatalf("re-grant should succeed: %v", err)
	}
	if !cs.HasConsent("s1", "owner1", "telegram", GroupFS) {
		t.Error("should have consent after re-grant")
	}
}

func TestPersistentConsentStore_ListGrants(t *testing.T) {
	db := setupTestDB(t)
	cs := NewPersistentConsentStore(db)

	cs.GrantAlways("owner1", "telegram", GroupFS)
	cs.GrantAlways("owner1", "telegram", GroupWeb)
	cs.GrantAlways("owner1", "discord", GroupRuntime)
	cs.GrantAlways("owner2", "telegram", GroupFS)

	// All grants for owner1/telegram.
	grants, err := cs.ListGrants("owner1", "telegram")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 2 {
		t.Errorf("expected 2 grants, got %d", len(grants))
	}

	// All grants for owner1 (any platform).
	grants, err = cs.ListGrants("owner1", "")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 3 {
		t.Errorf("expected 3 grants, got %d", len(grants))
	}

	// Revoked grants should not appear.
	cs.Revoke("owner1", "telegram", GroupFS)
	grants, err = cs.ListGrants("owner1", "telegram")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 1 {
		t.Errorf("expected 1 grant after revoke, got %d", len(grants))
	}
}

func TestPersistentConsentStore_RevokeByID(t *testing.T) {
	db := setupTestDB(t)
	cs := NewPersistentConsentStore(db)

	cs.GrantAlways("owner1", "telegram", GroupFS)

	grants, _ := cs.ListGrants("owner1", "telegram")
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}

	if err := cs.RevokeByID(grants[0].ID); err != nil {
		t.Fatalf("RevokeByID: %v", err)
	}

	if cs.HasConsent("s1", "owner1", "telegram", GroupFS) {
		t.Error("should not have consent after RevokeByID")
	}
}

func TestPersistentConsentStore_ClearSession(t *testing.T) {
	db := setupTestDB(t)
	cs := NewPersistentConsentStore(db)

	cs.GrantOnce("s1", GroupRuntime)
	cs.Deny("s1", GroupMCP)
	cs.ClearSession("s1")

	if cs.HasConsent("s1", "", "", GroupRuntime) {
		t.Error("session grant should be cleared")
	}
	if cs.InCooldown("s1", GroupMCP) {
		t.Error("session cooldown should be cleared")
	}
}

func TestPersistentConsentStore_InvalidateSessionGrants(t *testing.T) {
	db := setupTestDB(t)
	cs := NewPersistentConsentStore(db)

	// Grant session consent for fs, web, and runtime.
	cs.GrantOnce("s1", GroupFS)
	cs.GrantOnce("s1", GroupWeb)
	cs.GrantOnce("s1", GroupRuntime)

	// Switch to messaging profile (web, memory, team).
	newProfile := map[string]bool{GroupWeb: true, GroupMemory: true, GroupTeam: true}
	cs.InvalidateSessionGrants("s1", newProfile)

	// web should survive (still in new profile).
	if !cs.HasConsent("s1", "", "", GroupWeb) {
		t.Error("web grant should survive profile change (still in profile)")
	}
	// runtime should survive (always-consent group).
	if !cs.HasConsent("s1", "", "", GroupRuntime) {
		t.Error("runtime grant should survive (always-consent group)")
	}
	// fs should be gone (not in messaging profile, not always-consent).
	if cs.HasConsent("s1", "", "", GroupFS) {
		t.Error("fs grant should be invalidated (not in new profile)")
	}
}

func TestPersistentConsentStore_RevokeNotFound(t *testing.T) {
	db := setupTestDB(t)
	cs := NewPersistentConsentStore(db)

	err := cs.Revoke("nobody", "telegram", GroupFS)
	if err == nil {
		t.Error("should error when no active grant found")
	}
}
