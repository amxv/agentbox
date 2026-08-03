package db

import (
	"errors"
	"testing"

	"agentbox/internal/agentbox/types"
)

func TestPostgresCredentialInventoryRetainsRaycastInstallationsAndRevocations(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "credential-owner@example.invalid", "Credential Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	other, err := repository.CreateUser(ctx, "credential-other@example.invalid", "Credential Other", nil)
	if err != nil {
		t.Fatal(err)
	}
	scopes := []string{"threads:read", "threads:write", "assets:read", "assets:write"}
	macbookSecret := "agb_pg_macbook"
	macbook, err := repository.CreateRaycastAPIKey(ctx, owner.ID, "MacBook Air", hashSecret(macbookSecret), "agb_pg_macbo", scopes, "https://dashboard.example/")
	if err != nil {
		t.Fatal(err)
	}
	studioSecret := "agb_pg_studio"
	studio, err := repository.CreateRaycastAPIKey(ctx, owner.ID, "Studio Mac", hashSecret(studioSecret), "agb_pg_stud", scopes, "https://dashboard.example")
	if err != nil {
		t.Fatal(err)
	}
	if macbook.ID == studio.ID {
		t.Fatalf("Raycast installations reused credential ID: %#v %#v", macbook, studio)
	}
	if _, err := repository.CreateRaycastAPIKey(ctx, owner.ID, "MacBook Air", hashSecret("duplicate"), "duplicate", scopes, "https://dashboard.example"); !errors.Is(err, types.ErrCredentialLabelConflict) {
		t.Fatalf("duplicate active label error=%v", err)
	}

	firstPage, err := repository.ListAPIKeysPage(ctx, owner.ID, types.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Credentials) != 1 || !firstPage.Page.HasMore || firstPage.Page.NextCursor == nil {
		t.Fatalf("first page=%#v", firstPage)
	}
	secondPage, err := repository.ListAPIKeysPage(ctx, owner.ID, types.PageRequest{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Credentials) != 1 || secondPage.Credentials[0].ID == firstPage.Credentials[0].ID {
		t.Fatalf("second page=%#v", secondPage)
	}

	setupKey, setupBaseURL, err := repository.GetAPIKeySetup(ctx, owner.ID, macbook.ID)
	if err != nil {
		t.Fatal(err)
	}
	if setupKey == nil || setupKey.ID != macbook.ID || setupBaseURL != "https://dashboard.example" {
		t.Fatalf("persisted Raycast setup key=%#v base=%q", setupKey, setupBaseURL)
	}
	if crossUserKey, crossUserBase, err := repository.GetAPIKeySetup(ctx, other.ID, macbook.ID); err != nil || crossUserKey != nil || crossUserBase != "" {
		t.Fatalf("cross-user setup leaked: key=%#v base=%q err=%v", crossUserKey, crossUserBase, err)
	}

	rotatedSecret := "agb_pg_macbook_rotated"
	rotated, err := repository.RotateAPIKeyForUserByID(ctx, owner.ID, macbook.ID, hashSecret(rotatedSecret), "agb_pg_macbo")
	if err != nil {
		t.Fatal(err)
	}
	if rotated == nil || rotated.ID != macbook.ID || rotated.TokenHash == macbook.TokenHash {
		t.Fatalf("stable-ID rotation failed: before=%#v after=%#v", macbook, rotated)
	}
	if key, user, err := repository.FindAPIKeyBySecret(ctx, macbookSecret); err != nil || key != nil || user != nil {
		t.Fatalf("old secret authenticated: key=%#v user=%#v err=%v", key, user, err)
	}
	if key, user, err := repository.FindAPIKeyBySecret(ctx, rotatedSecret); err != nil || key == nil || user == nil || key.ID != macbook.ID || user.ID != owner.ID {
		t.Fatalf("rotated secret failed: key=%#v user=%#v err=%v", key, user, err)
	}
	if key, user, err := repository.FindAPIKeyBySecret(ctx, studioSecret); err != nil || key == nil || user == nil || key.ID != studio.ID {
		t.Fatalf("rotating MacBook affected Studio: key=%#v user=%#v err=%v", key, user, err)
	}
	if crossUserRotation, err := repository.RotateAPIKeyForUserByID(ctx, other.ID, macbook.ID, hashSecret("cross"), "cross"); err != nil || crossUserRotation != nil {
		t.Fatalf("cross-user rotation changed credential: key=%#v err=%v", crossUserRotation, err)
	}

	if revoked, err := repository.RevokeAPIKeyForUserByID(ctx, owner.ID, studio.ID); err != nil || !revoked {
		t.Fatalf("revoke Studio revoked=%t err=%v", revoked, err)
	}
	if revoked, err := repository.RevokeAPIKeyForUserByID(ctx, other.ID, macbook.ID); err != nil || revoked {
		t.Fatalf("cross-user revoke revoked=%t err=%v", revoked, err)
	}
	inventory, err := repository.ListAPIKeysPage(ctx, owner.ID, types.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Credentials) != 2 {
		t.Fatalf("revoked credential disappeared: %#v", inventory.Credentials)
	}
	var revokedSeen bool
	for _, credential := range inventory.Credentials {
		if credential.ID == studio.ID {
			revokedSeen = credential.RevokedAt != nil
		}
	}
	if !revokedSeen {
		t.Fatalf("revocation timestamp missing: %#v", inventory.Credentials)
	}
	otherInventory, err := repository.ListAPIKeysPage(ctx, other.ID, types.PageRequest{Limit: 10})
	if err != nil || len(otherInventory.Credentials) != 0 {
		t.Fatalf("cross-user inventory leaked: page=%#v err=%v", otherInventory, err)
	}
}
