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
	actor := types.Actor{Name: "author", KeyName: "primary"}

	thread, err := svc.CreateThread(context.Background(), actor, "Phase 2")
	if err != nil {
		t.Fatal(err)
	}
	message, err := svc.PostMessage(context.Background(), actor, PostMessageParams{
		ThreadID: thread.ID,
		Body:     "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Author != "author" || len(message.Assets) != 0 {
		t.Fatalf("unexpected message: %#v", message)
	}

	got, err := svc.GetThread(context.Background(), thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Body != "hello" {
		t.Fatalf("unexpected thread: %#v", got)
	}

	_, err = svc.GetThread(context.Background(), "thr_missing")
	if !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("expected ErrThreadNotFound, got %v", err)
	}
}
