package db

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryBootstrapOwnerIsIdempotent(t *testing.T) {
	repository := &MemoryRepository{}
	first, err := repository.BootstrapOwner(context.Background(), "owner@example.com", "Owner", "hash-one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.BootstrapOwner(context.Background(), "OWNER@example.com", "Updated", "hash-two")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || !second.IsOwner || second.DisplayName != "Updated" || second.PasswordHash == nil || *second.PasswordHash != "hash-two" {
		t.Fatalf("unexpected idempotent bootstrap result: first=%#v second=%#v", first, second)
	}
	if _, err := repository.BootstrapOwner(context.Background(), "other@example.com", "Other", "hash-three"); !errors.Is(err, ErrOwnerAlreadyExists) {
		t.Fatalf("second owner error = %v, want ErrOwnerAlreadyExists", err)
	}
	if len(repository.Users) != 1 {
		t.Fatalf("users = %d, want 1", len(repository.Users))
	}
}
