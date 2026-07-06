package service

import (
	"context"
	"errors"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/types"
)

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
	adminAuth := types.AuthContext{TenantID: "ten_a", SubjectType: types.AuthSubjectAdmin, ActorName: "admin", Role: "admin"}
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
		SubjectType: types.AuthSubjectAPIKey,
		ActorName:   actorName,
		KeyID:       "key_" + tenantID,
	}
}
