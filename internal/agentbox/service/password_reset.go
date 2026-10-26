package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/types"
)

const passwordResetTTL = time.Hour

type PasswordResetLinkResult struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type PasswordResetInspection struct {
	Valid     bool   `json:"valid"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Service) IssueUserPasswordReset(ctx context.Context, authContext types.AuthContext, userID string) (PasswordResetLinkResult, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return PasswordResetLinkResult{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return PasswordResetLinkResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "user_id is required."}
	}
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return PasswordResetLinkResult{}, err
	}
	secret := "agpr_" + base64.RawURLEncoding.EncodeToString(data)
	reset, err := s.repo.CreateUserPasswordReset(ctx, authContext.UserID, userID, hashSecret(secret), time.Now().UTC().Add(passwordResetTTL))
	if err != nil {
		return PasswordResetLinkResult{}, passwordResetError(err)
	}
	return PasswordResetLinkResult{Token: secret, ExpiresAt: reset.ExpiresAt}, nil
}

func (s *Service) InspectUserPasswordReset(ctx context.Context, secret string) (PasswordResetInspection, error) {
	if !strings.HasPrefix(secret, "agpr_") || len(secret) != len("agpr_")+43 {
		return PasswordResetInspection{}, invalidPasswordReset()
	}
	reset, err := s.repo.FindValidUserPasswordReset(ctx, hashSecret(secret))
	if err != nil {
		return PasswordResetInspection{}, err
	}
	if reset == nil {
		return PasswordResetInspection{}, invalidPasswordReset()
	}
	return PasswordResetInspection{Valid: true, ExpiresAt: reset.ExpiresAt}, nil
}

func (s *Service) CompleteUserPasswordReset(ctx context.Context, secret string, password string) error {
	if !strings.HasPrefix(secret, "agpr_") || len(secret) != len("agpr_")+43 {
		return invalidPasswordReset()
	}
	if len(password) > 1024 || utf8.RuneCountInString(password) < 12 || strings.TrimSpace(password) == "" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "Password must be at least 12 characters and at most 1024 bytes."}
	}
	// Reject random invalid tokens before running expensive password hashing.
	// Consumption independently rechecks the token under the account lock.
	if _, err := s.InspectUserPasswordReset(ctx, secret); err != nil {
		return err
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	return passwordResetError(s.repo.ConsumeUserPasswordReset(ctx, hashSecret(secret), passwordHash))
}

func invalidPasswordReset() error {
	return CodedError{Code: "INVALID_PASSWORD_RESET_TOKEN", Message: "This password reset link is invalid, expired, or already used."}
}

func passwordResetError(err error) error {
	switch {
	case errors.Is(err, types.ErrPasswordResetInvalid):
		return invalidPasswordReset()
	case errors.Is(err, types.ErrUserNotFound):
		return CodedError{Code: "USER_NOT_FOUND", Message: "User not found."}
	case errors.Is(err, types.ErrUserDisabled):
		return CodedError{Code: "USER_DISABLED", Message: "Enable this account before generating a password reset link."}
	case errors.Is(err, types.ErrOwnerCannotBeDisabled):
		return CodedError{Code: "OWNER_IMMUTABLE", Message: "Use the separate owner recovery flow for the deployment owner."}
	default:
		return err
	}
}
