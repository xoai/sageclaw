package channel

import (
	"context"
	"log"
)

// OwnerStore is the interface needed by the owner auto-capture helper.
type OwnerStore interface {
	UpdateConnection(ctx context.Context, id string, fields map[string]any) error
}

// CaptureOwner auto-captures the owner_user_id on a connection from inbound
// message metadata. Called by adapters on first inbound message. No-op if
// the owner is already set or the user ID is empty.
func CaptureOwner(ctx context.Context, store OwnerStore, connID, currentOwner, userID string) {
	if currentOwner != "" || userID == "" || store == nil {
		return
	}
	if err := store.UpdateConnection(ctx, connID, map[string]any{"owner_user_id": userID}); err != nil {
		log.Printf("channel: auto-capture owner for %s failed: %v", connID, err)
		return
	}
	log.Printf("channel: auto-captured owner %s for connection %s", userID, connID)
}
