package tool

import "testing"

func TestConsentStore_SafeAutoConsent(t *testing.T) {
	cs := NewConsentStore()

	// Safe tools should always have consent (auto-consent).
	if !cs.HasConsent("session1", GroupMemory) {
		t.Error("memory group should auto-consent (safe)")
	}
	if !cs.HasConsent("session1", GroupAudit) {
		t.Error("audit group should auto-consent (safe)")
	}
}

func TestConsentStore_ModerateRequiresConsent(t *testing.T) {
	cs := NewConsentStore()

	// Moderate tools should NOT auto-consent.
	if cs.HasConsent("session1", GroupFS) {
		t.Error("fs group should not auto-consent (moderate)")
	}
	if cs.HasConsent("session1", GroupWeb) {
		t.Error("web group should not auto-consent (moderate)")
	}
}

func TestConsentStore_GrantAndCheck(t *testing.T) {
	cs := NewConsentStore()

	cs.Grant("session1", GroupRuntime)
	if !cs.HasConsent("session1", GroupRuntime) {
		t.Error("should have consent after grant")
	}

	// Different session should not have consent.
	if cs.HasConsent("session2", GroupRuntime) {
		t.Error("different session should not have consent")
	}
}

func TestConsentStore_DenyAndCheck(t *testing.T) {
	cs := NewConsentStore()

	cs.Deny("session1", GroupMCP)

	if cs.HasConsent("session1", GroupMCP) {
		t.Error("denied group should not have consent")
	}
	if !cs.IsDenied("session1", GroupMCP) {
		t.Error("should be marked as denied")
	}
}

func TestConsentStore_GrantAll(t *testing.T) {
	cs := NewConsentStore()

	cs.GrantAll("session1")

	// All groups should have consent.
	for group := range GroupRisk {
		if !cs.HasConsent("session1", group) {
			t.Errorf("GrantAll should consent all groups, missing: %s", group)
		}
	}
}

func TestConsentStore_ClearSession(t *testing.T) {
	cs := NewConsentStore()

	cs.Grant("session1", GroupRuntime)
	cs.ClearSession("session1")

	if cs.HasConsent("session1", GroupRuntime) {
		t.Error("consent should be cleared after ClearSession")
	}
}

func TestConsentStore_IsDenied_NotAskedYet(t *testing.T) {
	cs := NewConsentStore()

	// Never asked → not denied.
	if cs.IsDenied("session1", GroupRuntime) {
		t.Error("should not be denied if never asked")
	}
}

func TestRiskExplanation(t *testing.T) {
	tests := []struct {
		group    string
		contains string
	}{
		{GroupMemory, "stored data"},
		{GroupFS, "read and write files"},
		{GroupWeb, "internet"},
		{GroupRuntime, "shell commands"},
		{GroupMCP, "external MCP server"},
		{GroupOrchestration, "delegate"},
	}

	for _, tt := range tests {
		t.Run(tt.group, func(t *testing.T) {
			exp := RiskExplanation(tt.group)
			if exp == "" {
				t.Error("explanation should not be empty")
			}
			// Just check it's not the unknown fallback for known groups.
			if exp == "Unknown tool group." {
				t.Errorf("known group %s should have a specific explanation", tt.group)
			}
		})
	}
}
