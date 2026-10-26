package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) CreateUserPasswordReset(ctx context.Context, issuerUserID string, userID string, tokenHash string, expiresAt time.Time) (types.PasswordResetToken, error) {
	if issuerUserID == "" || strings.TrimSpace(userID) == "" || tokenHash == "" || !expiresAt.After(time.Now().UTC()) {
		return types.PasswordResetToken{}, errors.New("invalid password reset request")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.PasswordResetToken{}, err
	}
	defer tx.Rollback(ctx)
	// Lock the target account first in both issuance and redemption transactions.
	// Reissuing a link and consuming an older link cannot race or deadlock.
	user, err := scanUser(tx.QueryRow(ctx, `
        select id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
        from users where id = $1 for update
    `, strings.TrimSpace(userID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return types.PasswordResetToken{}, types.ErrUserNotFound
	}
	if err != nil {
		return types.PasswordResetToken{}, err
	}
	if user.IsOwner {
		return types.PasswordResetToken{}, types.ErrOwnerCannotBeDisabled
	}
	var issuerIsOwner bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from users where id = $1 and is_owner and disabled_at is null)`, issuerUserID).Scan(&issuerIsOwner); err != nil {
		return types.PasswordResetToken{}, err
	}
	if !issuerIsOwner {
		return types.PasswordResetToken{}, errors.New("issuer must be active owner")
	}
	if user.DisabledAt != nil {
		return types.PasswordResetToken{}, types.ErrUserDisabled
	}

	if _, err = tx.Exec(ctx, `update user_password_resets set revoked_at = now() where user_id = $1 and consumed_at is null and revoked_at is null`, user.ID); err != nil {
		return types.PasswordResetToken{}, err
	}
	token, err := scanUserPasswordReset(tx.QueryRow(ctx, `
        insert into user_password_resets(id, user_id, token_hash, created_by_user_id, expires_at)
        values($1, $2, $3, $4, $5)
        returning id, user_id, created_at, expires_at, consumed_at, revoked_at
    `, "pr_"+uuid.NewString(), user.ID, tokenHash, issuerUserID, expiresAt))
	if err != nil {
		return types.PasswordResetToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.PasswordResetToken{}, err
	}
	return token, nil
}

func (r *Repository) FindValidUserPasswordReset(ctx context.Context, tokenHash string) (*types.PasswordResetToken, error) {
	token, err := scanUserPasswordReset(r.pool.QueryRow(ctx, `
        select p.id, p.user_id, p.created_at, p.expires_at, p.consumed_at, p.revoked_at
        from user_password_resets p
        join users u on u.id = p.user_id
        where p.token_hash = $1 and p.expires_at > now() and p.consumed_at is null
          and p.revoked_at is null and u.is_owner = false and u.disabled_at is null
    `, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func (r *Repository) ConsumeUserPasswordReset(ctx context.Context, tokenHash string, passwordHash string) error {
	if tokenHash == "" || strings.TrimSpace(passwordHash) == "" {
		return types.ErrPasswordResetInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Read only the ID first, then lock the account before changing token state.
	// This matches the lock order in CreateUserPasswordReset.
	var userID string
	if err := tx.QueryRow(ctx, `select user_id from user_password_resets where token_hash = $1`, tokenHash).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return types.ErrPasswordResetInvalid
	} else if err != nil {
		return err
	}
	user, err := scanUser(tx.QueryRow(ctx, `
        select id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
        from users where id = $1 for update
    `, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return types.ErrPasswordResetInvalid
	}
	if err != nil {
		return err
	}
	if user.IsOwner || user.DisabledAt != nil {
		return types.ErrPasswordResetInvalid
	}

	var consumedID string
	err = tx.QueryRow(ctx, `
        update user_password_resets set consumed_at = now()
        where token_hash = $1 and user_id = $2 and consumed_at is null and revoked_at is null
          and expires_at > now()
        returning id
    `, tokenHash, user.ID).Scan(&consumedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.ErrPasswordResetInvalid
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update users set password_hash = $1, updated_at = now() where id = $2`, passwordHash, user.ID); err != nil {
		return err
	}
	// Reset browser access while keeping CLI/MCP/Raycast API keys unchanged.
	if _, err := tx.Exec(ctx, `update user_sessions set revoked_at = now() where user_id = $1 and revoked_at is null`, user.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanUserPasswordReset(row pgx.Row) (types.PasswordResetToken, error) {
	var token types.PasswordResetToken
	var createdAt, expiresAt time.Time
	var consumedAt, revokedAt *time.Time
	err := row.Scan(&token.ID, &token.UserID, &createdAt, &expiresAt, &consumedAt, &revokedAt)
	if err != nil {
		return types.PasswordResetToken{}, err
	}
	token.CreatedAt = isoMillis(createdAt)
	token.ExpiresAt = isoMillis(expiresAt)
	if consumedAt != nil {
		value := isoMillis(*consumedAt)
		token.ConsumedAt = &value
	}
	if revokedAt != nil {
		value := isoMillis(*revokedAt)
		token.RevokedAt = &value
	}
	return token, nil
}
