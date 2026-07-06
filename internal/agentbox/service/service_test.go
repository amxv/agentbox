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

func testAuth(tenantID string, actorName string) types.AuthContext {
	return types.AuthContext{
		TenantID:    tenantID,
		SubjectType: types.AuthSubjectAPIKey,
		ActorName:   actorName,
		KeyID:       "key_" + tenantID,
	}
}
