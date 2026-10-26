package db

import (
	"errors"
	"testing"
	"time"

	"agentbox/internal/agentbox/types"
)

func TestUserPasswordResetIsAtomicHashedAndPreservesAgentKeys(t *testing.T) {
	repo, ctx := openPostgresTestRepository(t)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repo.BootstrapOwner(ctx, "reset-owner@example.com", "Reset Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repo.CreateUser(ctx, "member@example.com", "Member", ptrString("original-hash"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateAPIKey(ctx, member.ID, "agent", "chatgpt", "key-hash", "agkey_", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateUserSession(ctx, types.UserSession{UserID: member.ID, SecretHash: "session-hash", ExpiresAt: isoMillis(time.Now().Add(time.Hour))})
	if err != nil {
		t.Fatal(err)
	}

	first, err := repo.CreateUserPasswordReset(ctx, owner.ID, member.ID, "secret-hash-1", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if first.UserID != member.ID {
		t.Fatalf("bad target: %#v", first)
	}
	var hash string
	if err := repo.pool.QueryRow(ctx, `select token_hash from user_password_resets where id=$1`, first.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != "secret-hash-1" {
		t.Fatalf("incorrect stored hash %q", hash)
	}
	second, err := repo.CreateUserPasswordReset(ctx, owner.ID, member.ID, "secret-hash-2", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("duplicate token")
	}
	if err := repo.ConsumeUserPasswordReset(ctx, "secret-hash-1", "bad-hash"); !errors.Is(err, types.ErrPasswordResetInvalid) {
		t.Fatalf("older token accepted: %v", err)
	}
	if reset, err := repo.FindValidUserPasswordReset(ctx, "secret-hash-2"); err != nil || reset == nil {
		t.Fatalf("valid reset missing: reset=%v err=%v", reset, err)
	}
	if err := repo.ConsumeUserPasswordReset(ctx, "secret-hash-2", "new-hash"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConsumeUserPasswordReset(ctx, "secret-hash-2", "replay-hash"); !errors.Is(err, types.ErrPasswordResetInvalid) {
		t.Fatalf("replay accepted: %v", err)
	}
	updated, err := repo.GetUserByID(ctx, member.ID)
	if err != nil || updated == nil || updated.PasswordHash == nil || *updated.PasswordHash != "new-hash" {
		t.Fatalf("password didn't update: user=%v err=%v", updated, err)
	}
	var sessionRevoked bool
	if err := repo.pool.QueryRow(ctx, `select revoked_at is not null from user_sessions where user_id=$1`, member.ID).Scan(&sessionRevoked); err != nil || !sessionRevoked {
		t.Fatalf("old session not revoked: %v %v", sessionRevoked, err)
	}
	var keyRevoked bool
	if err := repo.pool.QueryRow(ctx, `select revoked_at is not null from api_keys where user_id=$1`, member.ID).Scan(&keyRevoked); err != nil || keyRevoked {
		t.Fatalf("agent credential unexpectedly revoked: %v %v", keyRevoked, err)
	}
	exp, err := repo.CreateUserPasswordReset(ctx, owner.ID, member.ID, "expired-hash", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.pool.Exec(ctx, `update user_password_resets set expires_at=now()-interval '1 second' where id=$1`, exp.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConsumeUserPasswordReset(ctx, "expired-hash", "bad-hash"); !errors.Is(err, types.ErrPasswordResetInvalid) {
		t.Fatalf("expired token used: %v", err)
	}
	if _, err := repo.CreateUserPasswordReset(ctx, owner.ID, owner.ID, "owner-hash", time.Now().Add(time.Hour)); !errors.Is(err, types.ErrOwnerCannotBeDisabled) {
		t.Fatalf("owner reset allowed: %v", err)
	}
	if _, err := repo.CreateUserPasswordReset(ctx, owner.ID, "missing-user", "missing-hash", time.Now().Add(time.Hour)); !errors.Is(err, types.ErrUserNotFound) {
		t.Fatalf("missing user reset allowed: %v", err)
	}
}

func ptrString(value string) *string { return &value }
