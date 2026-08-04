package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/types"
)

func TestRaycastInstallationsShareCredentialInventoryAndRemainIndependent(t *testing.T) {
	ctx := context.Background()
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	userA := types.User{ID: "usr_raycast_a", Email: "a@example.invalid", DisplayName: "A"}
	userB := types.User{ID: "usr_raycast_b", Email: "b@example.invalid", DisplayName: "B"}
	repo.Users = append(repo.Users, userA, userB)
	authA := types.AuthContext{UserID: userA.ID, UserDisplayName: userA.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard", SessionID: "sess_a"}
	authB := types.AuthContext{UserID: userB.ID, UserDisplayName: userB.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard", SessionID: "sess_b"}

	macbook, err := svc.CreateRaycastInstallation(ctx, authA, "MacBook Air", "https://dashboard.example/")
	if err != nil {
		t.Fatal(err)
	}
	studio, err := svc.CreateRaycastInstallation(ctx, authA, "Studio Mac", "https://dashboard.example")
	if err != nil {
		t.Fatal(err)
	}
	if macbook.Credential.ID == studio.Credential.ID || macbook.Credential.Key == studio.Credential.Key {
		t.Fatalf("Raycast installations reused identity or secret: macbook=%#v studio=%#v", macbook.Credential, studio.Credential)
	}
	for _, result := range []RaycastInstallationResult{macbook, studio} {
		if result.Credential.Purpose != "raycast" || strings.Join(result.Credential.Scopes, ",") != "threads:read,threads:write,assets:read,assets:write" || strings.Contains(strings.Join(result.Credential.Scopes, ","), "mcp:use") {
			t.Fatalf("Raycast least-privilege contract changed: %#v", result.Credential)
		}
		if result.Setup.CredentialID != result.Credential.ID || result.Setup.Label != result.Credential.Name || result.Setup.APIKey != result.Credential.Key || result.Setup.BaseURL != "https://dashboard.example" || !strings.Contains(result.Setup.FinalCheck, "Browse Threads") {
			t.Fatalf("Raycast setup bundle is incomplete: %#v", result.Setup)
		}
	}
	if _, err := svc.CreateRaycastInstallation(ctx, authA, "MacBook Air", "https://dashboard.example"); codedErrorCode(err) != "CREDENTIAL_LABEL_CONFLICT" {
		t.Fatalf("duplicate installation label error=%v", err)
	}

	if authenticated, err := svc.AuthenticateAPIKey(ctx, macbook.Credential.Key); err != nil || authenticated == nil || authenticated.KeyID != macbook.Credential.ID {
		t.Fatalf("MacBook credential did not authenticate independently: auth=%#v err=%v", authenticated, err)
	}
	if authenticated, err := svc.AuthenticateAPIKey(ctx, studio.Credential.Key); err != nil || authenticated == nil || authenticated.KeyID != studio.Credential.ID {
		t.Fatalf("Studio credential did not authenticate independently: auth=%#v err=%v", authenticated, err)
	}

	page, err := svc.ListAPIKeysPage(ctx, authA, types.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Credentials) != 1 || !page.Page.HasMore || page.Page.NextCursor == nil {
		t.Fatalf("bounded credential page=%#v", page)
	}
	secondPage, err := svc.ListAPIKeysPage(ctx, authA, types.PageRequest{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Credentials) != 1 || secondPage.Credentials[0].ID == page.Credentials[0].ID {
		t.Fatalf("credential continuation page=%#v", secondPage)
	}

	oldMacbookSecret := macbook.Credential.Key
	rotated, rotatedSetup, err := svc.RotateAPIKeyByID(ctx, authA, macbook.Credential.ID, "https://dashboard.example")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.ID != macbook.Credential.ID || rotated.Key == "" || rotated.Key == oldMacbookSecret || rotatedSetup == nil || rotatedSetup.APIKey != rotated.Key {
		t.Fatalf("stable-ID Raycast rotation failed: credential=%#v setup=%#v", rotated, rotatedSetup)
	}
	if authenticated, err := svc.AuthenticateAPIKey(ctx, oldMacbookSecret); err != nil || authenticated != nil {
		t.Fatalf("rotated Raycast secret remained active: auth=%#v err=%v", authenticated, err)
	}
	if authenticated, err := svc.AuthenticateAPIKey(ctx, rotated.Key); err != nil || authenticated == nil || authenticated.KeyID != rotated.ID {
		t.Fatalf("replacement Raycast secret did not authenticate: auth=%#v err=%v", authenticated, err)
	}
	if authenticated, err := svc.AuthenticateAPIKey(ctx, studio.Credential.Key); err != nil || authenticated == nil || authenticated.KeyID != studio.Credential.ID {
		t.Fatalf("rotating MacBook affected Studio: auth=%#v err=%v", authenticated, err)
	}

	reopened, err := svc.RaycastInstallationSetup(ctx, authA, macbook.Credential.ID, "https://dashboard.example")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.APIKey != "" || reopened.CredentialID != macbook.Credential.ID || reopened.Label != "MacBook Air" || reopened.BaseURL != "https://dashboard.example" {
		t.Fatalf("persisted non-secret setup=%#v", reopened)
	}

	if err := svc.RevokeAPIKeyByID(ctx, authA, studio.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if authenticated, err := svc.AuthenticateAPIKey(ctx, studio.Credential.Key); err != nil || authenticated != nil {
		t.Fatalf("revoked Studio secret authenticated: auth=%#v err=%v", authenticated, err)
	}
	inventory, err := svc.ListAPIKeysPage(ctx, authA, types.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Credentials) != 2 {
		t.Fatalf("revoked credential disappeared: %#v", inventory.Credentials)
	}
	var revokedSeen bool
	for _, credential := range inventory.Credentials {
		if credential.ID == studio.Credential.ID {
			revokedSeen = credential.RevokedAt != nil
		}
	}
	if !revokedSeen {
		t.Fatalf("revocation history missing: %#v", inventory.Credentials)
	}

	if _, _, err := svc.RotateAPIKeyByID(ctx, authB, macbook.Credential.ID, "https://dashboard.example"); codedErrorCode(err) != "CREDENTIAL_NOT_FOUND" {
		t.Fatalf("cross-user rotate error=%v", err)
	}
	if err := svc.RevokeAPIKeyByID(ctx, authB, macbook.Credential.ID); codedErrorCode(err) != "CREDENTIAL_NOT_FOUND" {
		t.Fatalf("cross-user revoke error=%v", err)
	}
	if _, err := svc.RaycastInstallationSetup(ctx, authB, macbook.Credential.ID, "https://dashboard.example"); codedErrorCode(err) != "CREDENTIAL_NOT_FOUND" {
		t.Fatalf("cross-user setup error=%v", err)
	}
	otherInventory, err := svc.ListAPIKeysPage(ctx, authB, types.PageRequest{Limit: 10})
	if err != nil || len(otherInventory.Credentials) != 0 {
		t.Fatalf("cross-user inventory leaked: page=%#v err=%v", otherInventory, err)
	}
}

func TestCredentialCreateConflictsAndLegacyRaycastRotationIsAtomic(t *testing.T) {
	ctx := context.Background()
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	user := types.User{ID: "usr_atomic_credentials", Email: "atomic@example.invalid", DisplayName: "Atomic"}
	repo.Users = append(repo.Users, user)
	browser := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard", SessionID: "sess_atomic"}

	created, err := svc.CreateAPIKeyWithPurposeAndScopes(ctx, browser, "Local Mac", "local", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAPIKeyWithPurposeAndScopes(ctx, browser, "local mac", "local", []string{"threads:write"}); codedErrorCode(err) != "CREDENTIAL_LABEL_CONFLICT" {
		t.Fatalf("duplicate create error=%v", err)
	}
	if authenticated, err := svc.AuthenticateAPIKey(ctx, created.Key); err != nil || authenticated == nil || authenticated.KeyID != created.ID {
		t.Fatalf("duplicate create replaced original credential: auth=%#v err=%v", authenticated, err)
	}

	legacySecret := "agb_legacy_raycast"
	legacy, err := repo.CreateAPIKey(ctx, user.ID, "Legacy Raycast", "raycast", hashSecret(legacySecret), tokenPrefix(legacySecret), ConnectorAPIKeyScopes("raycast"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.RotateAPIKeyByID(ctx, browser, legacy.ID, ""); codedErrorCode(err) != "RAYCAST_SETUP_UNAVAILABLE" {
		t.Fatalf("legacy rotation without trusted origin error=%v", err)
	}
	if authenticated, err := svc.AuthenticateAPIKey(ctx, legacySecret); err != nil || authenticated == nil || authenticated.KeyID != legacy.ID {
		t.Fatalf("failed legacy rotation invalidated old secret: auth=%#v err=%v", authenticated, err)
	}

	rotated, setup, err := svc.RotateAPIKeyByID(ctx, browser, legacy.ID, "https://dashboard.example")
	if err != nil {
		t.Fatal(err)
	}
	if setup == nil || setup.BaseURL != "https://dashboard.example" || setup.APIKey != rotated.Key || rotated.ID != legacy.ID {
		t.Fatalf("legacy rotation did not atomically backfill setup: rotated=%#v setup=%#v", rotated, setup)
	}
	if authenticated, err := svc.AuthenticateAPIKey(ctx, legacySecret); err != nil || authenticated != nil {
		t.Fatalf("successful legacy rotation left old secret active: auth=%#v err=%v", authenticated, err)
	}
	reopened, err := svc.RaycastInstallationSetup(ctx, browser, legacy.ID, "https://other.example")
	if err != nil || reopened.BaseURL != "https://dashboard.example" || reopened.APIKey != "" {
		t.Fatalf("reopened legacy setup=%#v err=%v", reopened, err)
	}
}

func codedErrorCode(err error) string {
	var coded CodedError
	if errors.As(err, &coded) {
		return coded.Code
	}
	return ""
}
