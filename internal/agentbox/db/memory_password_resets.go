package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
)

func (m *MemoryRepository) CreateUserPasswordReset(_ context.Context, issuerUserID string, userID string, tokenHash string, expiresAt time.Time) (types.PasswordResetToken, error) {
	m.purgeMutex.Lock()
	defer m.purgeMutex.Unlock()
	if issuerUserID == "" || userID == "" || tokenHash == "" || !expiresAt.After(time.Now().UTC()) {
		return types.PasswordResetToken{}, errors.New("invalid password reset request")
	}
	var ownerIssuer bool
	for _, user := range m.Users {
		if user.ID == issuerUserID && user.IsOwner && user.DisabledAt == nil {
			ownerIssuer = true
			break
		}
	}
	if !ownerIssuer {
		return types.PasswordResetToken{}, errors.New("issuer must be active owner")
	}
	var target *types.User
	for i := range m.Users {
		if m.Users[i].ID == userID {
			target = &m.Users[i]
			break
		}
	}
	if target == nil {
		return types.PasswordResetToken{}, types.ErrUserNotFound
	}
	if target.IsOwner {
		return types.PasswordResetToken{}, types.ErrOwnerCannotBeDisabled
	}
	if target.DisabledAt != nil {
		return types.PasswordResetToken{}, types.ErrUserDisabled
	}
	now := isoMillis(time.Now().UTC())
	for i := range m.PasswordResets {
		t := &m.PasswordResets[i].Token
		if t.UserID == userID && t.ConsumedAt == nil && t.RevokedAt == nil {
			t.RevokedAt = &now
		}
	}
	token := types.PasswordResetToken{ID: "pr_" + uuid.NewString(), UserID: userID, CreatedAt: now, ExpiresAt: isoMillis(expiresAt)}
	m.PasswordResets = append(m.PasswordResets, memoryPasswordReset{Token: token, TokenHash: tokenHash})
	return token, nil
}

func (m *MemoryRepository) FindValidUserPasswordReset(_ context.Context, tokenHash string) (*types.PasswordResetToken, error) {
	m.purgeMutex.Lock()
	defer m.purgeMutex.Unlock()
	for _, entry := range m.PasswordResets {
		if entry.TokenHash != tokenHash || !validMemoryReset(entry.Token) {
			continue
		}
		for _, user := range m.Users {
			if user.ID == entry.Token.UserID && !user.IsOwner && user.DisabledAt == nil {
				reset := entry.Token
				return &reset, nil
			}
		}
	}
	return nil, nil
}

func validMemoryReset(token types.PasswordResetToken) bool {
	expires, err := time.Parse(time.RFC3339Nano, token.ExpiresAt)
	return err == nil && expires.After(time.Now().UTC()) && token.RevokedAt == nil && token.ConsumedAt == nil
}

func (m *MemoryRepository) ConsumeUserPasswordReset(_ context.Context, tokenHash string, passwordHash string) error {
	m.purgeMutex.Lock()
	defer m.purgeMutex.Unlock()
	if tokenHash == "" || strings.TrimSpace(passwordHash) == "" {
		return types.ErrPasswordResetInvalid
	}
	for i := range m.PasswordResets {
		reset := &m.PasswordResets[i]
		if reset.TokenHash != tokenHash || !validMemoryReset(reset.Token) {
			continue
		}
		for j := range m.Users {
			user := &m.Users[j]
			if user.ID != reset.Token.UserID || user.IsOwner || user.DisabledAt != nil {
				continue
			}
			now := isoMillis(time.Now().UTC())
			user.PasswordHash = &passwordHash
			user.UpdatedAt = now
			reset.Token.ConsumedAt = &now
			for k := range m.Sessions {
				session := &m.Sessions[k]
				if session.UserID == user.ID && session.RevokedAt == nil {
					session.RevokedAt = &now
				}
			}
			return nil
		}
	}
	return types.ErrPasswordResetInvalid
}
