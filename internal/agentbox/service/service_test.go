package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentbox/internal/agentbox/assets"
	authpkg "agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/types"
)

func TestSessionAndCredentialResolveSameUserWithDistinctActors(t *testing.T) {
	passwordHash, err := authpkg.HashPassword("secret-password")
	if err != nil {
		t.Fatal(err)
	}
	repo := &db.MemoryRepository{
		Tenants: []types.Tenant{{ID: types.DefaultTenantID, Slug: "default", Name: "Default"}},
		Users: []types.User{{
			ID:           "usr_owner",
			TenantID:     types.DefaultTenantID,
			Email:        "owner@example.com",
			DisplayName:  "Owner Person",
			PasswordHash: &passwordHash,
			Role:         "admin",
			IsOwner:      true,
		}},
	}
	svc := New(repo, &assets.FakeStore{})

	sessionAuth, sessionSecret, err := svc.Login(context.Background(), "ten_wrong", "owner@example.com", "secret-password")
	if err != nil {
		t.Fatal(err)
	}
	if sessionAuth.UserID != "usr_owner" || sessionAuth.UserDisplayName != "Owner Person" || !sessionAuth.IsOwner || sessionAuth.ActorID == "" || sessionAuth.ActorID != sessionAuth.SessionID {
		t.Fatalf("unexpected browser auth context: %#v", sessionAuth)
	}
	if sessionAuth.TenantID != types.DefaultTenantID {
		t.Fatalf("tenant selector changed account choice: %#v", sessionAuth)
	}

	credential, err := svc.CreateAPIKeyWithPurposeAndScopes(context.Background(), sessionAuth, "chatgpt", "chatgpt", []string{"threads:read", "threads:write"})
	if err != nil {
		t.Fatal(err)
	}
	if credential.UserID != sessionAuth.UserID || credential.Purpose != "chatgpt" {
		t.Fatalf("unexpected credential: %#v", credential)
	}
	credentialAuth, err := svc.AuthenticateAPIKey(context.Background(), credential.Key)
	if err != nil {
		t.Fatal(err)
	}
	if credentialAuth == nil || credentialAuth.UserID != sessionAuth.UserID || credentialAuth.UserDisplayName != sessionAuth.UserDisplayName {
		t.Fatalf("credential did not resolve the browser user: session=%#v key=%#v", sessionAuth, credentialAuth)
	}
	if credentialAuth.ActorID != credential.ID || credentialAuth.KeyID != credential.ID || credentialAuth.ActorName != "chatgpt" || credentialAuth.IsOwner {
		t.Fatalf("credential actor or owner authority is wrong: %#v", credentialAuth)
	}
	if credentialAuth.ActorID == sessionAuth.ActorID {
		t.Fatalf("browser and credential actors collapsed: session=%#v key=%#v", sessionAuth, credentialAuth)
	}

	disabledAt := "2026-08-01T00:00:00.000Z"
	repo.Users[0].DisabledAt = &disabledAt
	if authenticated, err := svc.AuthenticateSession(context.Background(), sessionSecret); err != nil || authenticated != nil {
		t.Fatalf("disabled user browser session authenticated: auth=%#v err=%v", authenticated, err)
	}
	if authenticated, err := svc.AuthenticateAPIKey(context.Background(), credential.Key); err != nil || authenticated != nil {
		t.Fatalf("disabled user credential authenticated: auth=%#v err=%v", authenticated, err)
	}
}

func TestOwnerSetupTokensBootstrapRecoverRevokeAndRejectReplay(t *testing.T) {
	repo := &db.MemoryRepository{
		Tenants: []types.Tenant{{ID: types.DefaultTenantID, Slug: "default", Name: "Default"}},
	}
	svc := New(repo, &assets.FakeStore{})

	first, err := svc.IssueOwnerSetupToken(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.Purpose != "bootstrap" || !strings.HasPrefix(first.Token, "agos_") {
		t.Fatalf("unexpected first setup token: %#v", first)
	}
	second, err := svc.IssueOwnerSetupToken(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.Purpose != "bootstrap" || second.Token == first.Token {
		t.Fatalf("unexpected replacement token: first=%#v second=%#v", first, second)
	}
	if _, _, _, err := svc.CompleteOwnerSetup(context.Background(), first.Token, "owner@example.com", "Owner", "first-password"); !hasCodedError(err, "INVALID_OWNER_SETUP_TOKEN") {
		t.Fatalf("revoked token error = %v", err)
	}

	authContext, sessionSecret, owner, err := svc.CompleteOwnerSetup(context.Background(), second.Token, "owner@example.com", "Owner", "first-password")
	if err != nil {
		t.Fatal(err)
	}
	if owner.ID == "" || !owner.IsOwner || authContext.UserID != owner.ID || !authContext.IsOwner || authContext.SubjectType != types.AuthSubjectUserSession || sessionSecret == "" {
		t.Fatalf("unexpected owner completion: auth=%#v owner=%#v secret=%q", authContext, owner, sessionSecret)
	}
	if _, _, _, err := svc.CompleteOwnerSetup(context.Background(), second.Token, "owner@example.com", "Owner", "first-password"); !hasCodedError(err, "INVALID_OWNER_SETUP_TOKEN") {
		t.Fatalf("replayed token error = %v", err)
	}

	recovery, err := svc.IssueOwnerSetupToken(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Purpose != "recovery" {
		t.Fatalf("recovery purpose = %q", recovery.Purpose)
	}
	if _, _, _, err := svc.CompleteOwnerSetup(context.Background(), recovery.Token, "other@example.com", "Wrong", "second-password"); !hasCodedError(err, "OWNER_EMAIL_MISMATCH") {
		t.Fatalf("wrong-email recovery error = %v", err)
	}
	recoveredAuth, _, recoveredOwner, err := svc.CompleteOwnerSetup(context.Background(), recovery.Token, "OWNER@example.com", "Recovered Owner", "second-password")
	if err != nil {
		t.Fatal(err)
	}
	if recoveredOwner.ID != owner.ID || recoveredOwner.DisplayName != "Recovered Owner" || recoveredAuth.UserID != owner.ID {
		t.Fatalf("recovery changed owner identity: before=%#v after=%#v", owner, recoveredOwner)
	}
	if _, _, err := svc.Login(context.Background(), "ignored", "owner@example.com", "second-password"); err != nil {
		t.Fatalf("recovered password did not authenticate: %v", err)
	}
	if _, _, err := svc.Login(context.Background(), "ignored", "owner@example.com", "first-password"); !errors.Is(err, ErrInvalidLogin) {
		t.Fatalf("old password still authenticated: %v", err)
	}
}

func TestOwnerSetupTokenTTLIsBounded(t *testing.T) {
	svc := New(&db.MemoryRepository{}, &assets.FakeStore{})
	if _, err := svc.IssueOwnerSetupToken(context.Background(), 25*time.Hour); !hasCodedError(err, "INVALID_ARGUMENT") {
		t.Fatalf("oversized TTL error = %v", err)
	}
}

func hasCodedError(err error, code string) bool {
	var coded CodedError
	return errors.As(err, &coded) && coded.Code == code
}

func TestServiceThreadAndMessageFlow(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	auth := testAuth("ten_a", "author")

	thread, err := svc.CreateThread(context.Background(), auth, "Phase 2")
	if err != nil {
		t.Fatal(err)
	}
	message, err := svc.PostMessage(context.Background(), auth, PostMessageParams{
		ThreadID: thread.ID,
		Body:     "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Author != "author" || len(message.Assets) != 0 {
		t.Fatalf("unexpected message: %#v", message)
	}
	if message.BodyContentType == nil || *message.BodyContentType != "text/plain" {
		t.Fatalf("message content type = %#v", message.BodyContentType)
	}

	got, err := svc.GetThread(context.Background(), auth, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Body != "hello" {
		t.Fatalf("unexpected thread: %#v", got)
	}

	results, err := svc.SearchThreads(context.Background(), auth, types.SearchThreadParams{Query: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != thread.ID || results[0].MessageCount != 1 || results[0].LastMessagePreview != "hello" {
		t.Fatalf("search results = %#v", results)
	}

	_, err = svc.GetThread(context.Background(), auth, "thr_missing")
	if !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("expected ErrThreadNotFound, got %v", err)
	}
	var coded CodedError
	if !errors.As(err, &coded) || coded.Code != "THREAD_NOT_FOUND" {
		t.Fatalf("expected THREAD_NOT_FOUND, got %#v", err)
	}

	_, err = svc.PostMessage(context.Background(), auth, PostMessageParams{
		ThreadID: "thr_missing",
		Body:     "bad",
	})
	if !errors.As(err, &coded) || coded.Code != "THREAD_NOT_FOUND" {
		t.Fatalf("expected coded missing-thread post error, got %#v", err)
	}

	threadWithMessage, firstMessage, err := svc.CreateThreadWithMessage(context.Background(), auth, "Initial", "first body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if firstMessage.ThreadID != threadWithMessage.ID || firstMessage.Body != "first body" {
		t.Fatalf("threadWithMessage=%#v firstMessage=%#v", threadWithMessage, firstMessage)
	}
}

func TestServiceTenantIsolationAndAPIKeys(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	tenantA := testAuth("ten_a", "shared")
	tenantB := testAuth("ten_b", "shared")
	repo.Users = append(repo.Users,
		types.User{ID: tenantA.UserID, TenantID: "ten_a", Email: "a@example.com", DisplayName: "User A", Role: "member"},
		types.User{ID: tenantB.UserID, TenantID: "ten_b", Email: "b@example.com", DisplayName: "User B", Role: "member"},
	)

	keyA, err := svc.CreateAPIKey(context.Background(), tenantA, "shared")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := svc.CreateAPIKey(context.Background(), tenantB, "shared")
	if err != nil {
		t.Fatal(err)
	}
	if keyA.Key == "" || keyB.Key == "" || keyA.Key == keyB.Key {
		t.Fatalf("keys not unique: %#v %#v", keyA, keyB)
	}
	if keyA.TokenHash == "" || keyA.TokenPrefix == "" || keyA.KeyMasked == "" {
		t.Fatalf("key metadata missing: %#v", keyA)
	}

	authA, err := svc.AuthenticateAPIKey(context.Background(), keyA.Key)
	if err != nil {
		t.Fatal(err)
	}
	authB, err := svc.AuthenticateAPIKey(context.Background(), keyB.Key)
	if err != nil {
		t.Fatal(err)
	}
	if authA == nil || authA.TenantID != "ten_a" || authA.ActorName != "shared" || authB == nil || authB.TenantID != "ten_b" {
		t.Fatalf("auth contexts: %#v %#v", authA, authB)
	}

	threadA, err := svc.CreateThread(context.Background(), *authA, "Tenant A")
	if err != nil {
		t.Fatal(err)
	}
	threadB, err := svc.CreateThread(context.Background(), *authB, "Tenant B")
	if err != nil {
		t.Fatal(err)
	}

	threadsA, err := svc.ListThreads(context.Background(), *authA, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(threadsA) != 1 || threadsA[0].ID != threadA.ID {
		t.Fatalf("tenant A list leaked or missed data: %#v", threadsA)
	}
	if _, err := svc.GetThread(context.Background(), *authA, threadB.ID); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("tenant A get tenant B err = %v", err)
	}
	if _, err := svc.PostMessage(context.Background(), *authA, PostMessageParams{ThreadID: threadB.ID, Body: "nope"}); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("tenant A post tenant B err = %v", err)
	}

	if err := svc.RevokeAPIKey(context.Background(), tenantA, "shared"); err != nil {
		t.Fatal(err)
	}
	revokedA, err := svc.AuthenticateAPIKey(context.Background(), keyA.Key)
	if err != nil {
		t.Fatal(err)
	}
	stillB, err := svc.AuthenticateAPIKey(context.Background(), keyB.Key)
	if err != nil {
		t.Fatal(err)
	}
	if revokedA != nil || stillB == nil || stillB.TenantID != "ten_b" {
		t.Fatalf("revoke result revokedA=%#v stillB=%#v", revokedA, stillB)
	}
}

func TestServiceEnforcesAPIKeyScopes(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	adminAuth := types.AuthContext{TenantID: "ten_a", UserID: "usr_admin", SubjectType: types.AuthSubjectUserSession, ActorName: "admin", Role: "admin"}
	repo.Users = append(repo.Users, types.User{ID: adminAuth.UserID, TenantID: "ten_a", Email: "admin@example.com", DisplayName: "Admin", Role: "admin"})
	thread, err := svc.CreateThread(context.Background(), adminAuth, "Scoped")
	if err != nil {
		t.Fatal(err)
	}
	message, err := svc.PostMessageWithAsset(context.Background(), adminAuth, PostMessageWithAssetParams{
		ThreadID: thread.ID,
		Body:     "with asset",
		Bytes:    []byte("asset"),
		FileName: "asset.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Assets) != 1 {
		t.Fatalf("expected one asset, got %#v", message.Assets)
	}

	restrictedKey, err := svc.CreateAPIKeyWithScopes(context.Background(), adminAuth, "keys-only", []string{"keys:read"})
	if err != nil {
		t.Fatal(err)
	}
	restrictedAuth, err := svc.AuthenticateAPIKey(context.Background(), restrictedKey.Key)
	if err != nil {
		t.Fatal(err)
	}
	if restrictedAuth == nil {
		t.Fatal("restricted key did not authenticate")
	}
	assertScopeDenied := func(label string, err error) {
		t.Helper()
		var coded CodedError
		if !errors.As(err, &coded) || coded.Code != "PERMISSION_DENIED" {
			t.Fatalf("%s expected PERMISSION_DENIED, got %#v", label, err)
		}
	}
	_, err = svc.ListThreads(context.Background(), *restrictedAuth, 10)
	assertScopeDenied("list", err)
	_, err = svc.GetThread(context.Background(), *restrictedAuth, thread.ID)
	assertScopeDenied("get thread", err)
	_, err = svc.CreateThread(context.Background(), *restrictedAuth, "Nope")
	assertScopeDenied("create thread", err)
	_, err = svc.PostMessage(context.Background(), *restrictedAuth, PostMessageParams{ThreadID: thread.ID, Body: "nope"})
	assertScopeDenied("post message", err)
	_, err = svc.CreatePresignedUploads(context.Background(), *restrictedAuth, thread.ID, []types.UploadIntentFile{{FileName: "asset.txt"}})
	assertScopeDenied("upload intent", err)
	_, err = svc.GetAsset(context.Background(), *restrictedAuth, message.Assets[0].ID)
	assertScopeDenied("get asset", err)
	_, err = svc.SignedAssetDownloadURL(context.Background(), *restrictedAuth, message.Assets[0], 300)
	assertScopeDenied("sign asset", err)

	scopedKey, err := svc.CreateAPIKeyWithScopes(context.Background(), adminAuth, "worker", []string{"threads:read", "threads:write", "assets:read", "assets:write"})
	if err != nil {
		t.Fatal(err)
	}
	scopedAuth, err := svc.AuthenticateAPIKey(context.Background(), scopedKey.Key)
	if err != nil {
		t.Fatal(err)
	}
	if scopedAuth == nil {
		t.Fatal("scoped key did not authenticate")
	}
	if _, err := svc.ListThreads(context.Background(), *scopedAuth, 10); err != nil {
		t.Fatalf("scoped list failed: %v", err)
	}
	if _, err := svc.PostMessage(context.Background(), *scopedAuth, PostMessageParams{ThreadID: thread.ID, Body: "ok"}); err != nil {
		t.Fatalf("scoped post failed: %v", err)
	}
	if _, err := svc.GetAsset(context.Background(), *scopedAuth, message.Assets[0].ID); err != nil {
		t.Fatalf("scoped get asset failed: %v", err)
	}
	if _, err := svc.SignedAssetDownloadURL(context.Background(), *scopedAuth, message.Assets[0], 300); err != nil {
		t.Fatalf("scoped sign asset failed: %v", err)
	}
	if _, err := svc.CreatePresignedUploads(context.Background(), *scopedAuth, thread.ID, []types.UploadIntentFile{{FileName: "next.txt"}}); err != nil {
		t.Fatalf("scoped upload intent failed: %v", err)
	}
}

func TestServiceProvisionTenantIsIdempotentAndHashesPassword(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})

	first, err := svc.ProvisionTenant(context.Background(), ProvisionTenantParams{
		TenantSlug: "acme",
		TenantName: "Acme",
		UserEmail:  "admin@example.com",
		UserName:   "Acme Admin",
		Password:   "secret-password",
		CreateKey:  true,
		KeyName:    "workstation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Tenant.ID != "ten_acme" || first.User.Role != "admin" || first.APIKey == nil || first.APIKey.Key == "" {
		t.Fatalf("first result = %#v", first)
	}
	if len(repo.Tenants) != 1 || len(repo.Users) != 1 || len(repo.APIKeys) != 1 {
		t.Fatalf("repo counts tenants=%d users=%d keys=%d", len(repo.Tenants), len(repo.Users), len(repo.APIKeys))
	}
	if repo.Users[0].PasswordHash == nil || *repo.Users[0].PasswordHash == "secret-password" {
		t.Fatalf("password was not hashed: %#v", repo.Users[0].PasswordHash)
	}
	if _, _, err := svc.Login(context.Background(), "ten_acme", "admin@example.com", "secret-password"); err != nil {
		t.Fatalf("login with provisioned password failed: %v", err)
	}

	second, err := svc.ProvisionTenant(context.Background(), ProvisionTenantParams{
		TenantSlug: "acme",
		TenantName: "Acme Renamed",
		UserEmail:  "admin@example.com",
		UserName:   "Acme Admin",
		Password:   "secret-password",
		CreateKey:  true,
		KeyName:    "workstation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Tenant.ID != first.Tenant.ID || second.User.ID != first.User.ID {
		t.Fatalf("provisioning was not idempotent: first=%#v second=%#v", first, second)
	}
	if len(repo.Tenants) != 1 || len(repo.Users) != 1 || len(repo.APIKeys) != 1 {
		t.Fatalf("repo counts after second tenants=%d users=%d keys=%d", len(repo.Tenants), len(repo.Users), len(repo.APIKeys))
	}
}

func TestServiceProvisionUserSetupToken(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	if _, err := repo.UpsertTenant(context.Background(), types.Tenant{ID: "ten_acme", Slug: "acme", Name: "Acme"}); err != nil {
		t.Fatal(err)
	}
	user, setupToken, err := svc.ProvisionUser(context.Background(), ProvisionUserParams{
		TenantIDOrSlug: "acme",
		Email:          "new@example.com",
		DisplayName:    "New Admin",
		Role:           "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == "" || setupToken == "" {
		t.Fatalf("user=%#v setupToken=%q", user, setupToken)
	}
	if _, _, err := svc.Login(context.Background(), "ten_acme", "new@example.com", setupToken); err != nil {
		t.Fatalf("login with setup token failed: %v", err)
	}
	_, secondToken, err := svc.ProvisionUser(context.Background(), ProvisionUserParams{
		TenantIDOrSlug: "acme",
		Email:          "new@example.com",
		DisplayName:    "New Admin",
		Role:           "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondToken != "" {
		t.Fatalf("existing user should not receive a new setup token, got %q", secondToken)
	}
}

func testAuth(tenantID string, actorName string) types.AuthContext {
	return types.AuthContext{
		TenantID:    tenantID,
		UserID:      "usr_" + tenantID,
		SubjectType: types.AuthSubjectAPIKey,
		ActorName:   actorName,
		KeyID:       "key_" + tenantID,
	}
}
