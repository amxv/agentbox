package service

import (
	"context"
	"strings"
	"time"

	"agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/types"
)

func (s *Service) AuthenticateAPIKey(ctx context.Context, secret string) (*types.AuthContext, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, nil
	}
	key, user, err := s.repo.FindAPIKeyBySecret(ctx, secret)
	if err != nil {
		return nil, err
	}
	if key == nil || user == nil {
		return nil, nil
	}
	if key.ID != "" {
		if err := s.repo.MarkAPIKeyUsed(ctx, key.ID); err != nil {
			return nil, err
		}
	}
	return &types.AuthContext{
		UserID:          user.ID,
		UserDisplayName: user.DisplayName,
		SubjectType:     types.AuthSubjectAPIKey,
		ActorID:         key.ID,
		ActorName:       key.Name,
		KeyID:           key.ID,
		Scopes:          append([]string(nil), key.Scopes...),
	}, nil
}

func (s *Service) Login(ctx context.Context, _ string, email string, password string) (types.AuthContext, string, error) {
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return types.AuthContext{}, "", ErrInvalidLogin
	}
	user, err := s.repo.FindUserByEmail(ctx, email)
	if err != nil {
		return types.AuthContext{}, "", err
	}
	if user == nil || user.PasswordHash == nil || !auth.VerifyPassword(password, *user.PasswordHash) {
		return types.AuthContext{}, "", ErrInvalidLogin
	}
	return s.createSessionForUser(ctx, *user)
}

func (s *Service) createSessionForUser(ctx context.Context, user types.User) (types.AuthContext, string, error) {
	secret, err := generateSessionSecret()
	if err != nil {
		return types.AuthContext{}, "", err
	}
	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	session, err := s.repo.CreateUserSession(ctx, types.UserSession{
		UserID:     user.ID,
		SecretHash: hashSecret(secret),
		ExpiresAt:  expiresAt,
	})
	if err != nil {
		return types.AuthContext{}, "", err
	}
	return authContextForUserSession(session, user), secret, nil
}

func (s *Service) AuthenticateSession(ctx context.Context, secret string) (*types.AuthContext, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, nil
	}
	session, user, err := s.repo.FindUserSessionBySecretHash(ctx, hashSecret(secret))
	if err != nil {
		return nil, err
	}
	if session == nil || user == nil {
		return nil, nil
	}
	if session.ID != "" {
		if err := s.repo.MarkUserSessionUsed(ctx, session.ID); err != nil {
			return nil, err
		}
	}
	authContext := authContextForUserSession(*session, *user)
	return &authContext, nil
}

func (s *Service) LogoutSession(ctx context.Context, secret string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	session, _, err := s.repo.FindUserSessionBySecretHash(ctx, hashSecret(secret))
	if err != nil {
		return err
	}
	if session == nil {
		return nil
	}
	return s.repo.RevokeUserSession(ctx, session.ID)
}

func (s *Service) AuthorizeCLILogin(ctx context.Context, authContext types.AuthContext, state string, redirectURI string) (CLILoginAuthorizeResult, error) {
	if err := requireAuthContext(authContext); err != nil {
		return CLILoginAuthorizeResult{}, err
	}
	if authContext.SubjectType != types.AuthSubjectUserSession || strings.TrimSpace(authContext.UserID) == "" {
		return CLILoginAuthorizeResult{}, CodedError{Code: "PERMISSION_DENIED", Message: "Browser session authentication is required."}
	}
	state = strings.TrimSpace(state)
	redirectURI = strings.TrimSpace(redirectURI)
	if state == "" {
		return CLILoginAuthorizeResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "state is required."}
	}
	if err := validateCLIRedirectURI(redirectURI); err != nil {
		return CLILoginAuthorizeResult{}, err
	}
	code, err := generateCLILoginCode()
	if err != nil {
		return CLILoginAuthorizeResult{}, err
	}
	expiresAt := time.Now().UTC().Add(5 * time.Minute).Format("2006-01-02T15:04:05.000Z")
	if _, err := s.repo.CreateCLILoginCode(ctx, types.CLILoginCode{
		UserID:      authContext.UserID,
		CodeHash:    hashSecret(code),
		StateHash:   hashSecret(state),
		RedirectURI: redirectURI,
		ExpiresAt:   expiresAt,
	}); err != nil {
		return CLILoginAuthorizeResult{}, err
	}
	return CLILoginAuthorizeResult{Code: code, RedirectURI: redirectURI}, nil
}

func (s *Service) ExchangeCLILogin(ctx context.Context, code string, state string, redirectURI string, keyName string) (CLILoginExchangeResult, error) {
	code = strings.TrimSpace(code)
	state = strings.TrimSpace(state)
	redirectURI = strings.TrimSpace(redirectURI)
	if code == "" || state == "" {
		return CLILoginExchangeResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "code and state are required."}
	}
	if err := validateCLIRedirectURI(redirectURI); err != nil {
		return CLILoginExchangeResult{}, err
	}
	loginCode, user, err := s.repo.ConsumeCLILoginCode(ctx, hashSecret(code), hashSecret(state), redirectURI)
	if err != nil {
		return CLILoginExchangeResult{}, err
	}
	if loginCode == nil || user == nil {
		return CLILoginExchangeResult{}, CodedError{Code: "PERMISSION_DENIED", Message: "Invalid or expired CLI login code."}
	}
	keyName = strings.TrimSpace(keyName)
	if keyName == "" {
		keyName = defaultCLIKeyName()
	}
	key, err := s.CreateAPIKeyWithPurposeAndScopes(ctx, types.AuthContext{
		UserID:          user.ID,
		UserDisplayName: user.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		ActorID:         loginCode.ID,
		ActorName:       user.DisplayName,
		IsOwner:         user.IsOwner,
	}, keyName, "local", cliAPIKeyScopes())
	if err != nil {
		return CLILoginExchangeResult{}, err
	}
	return CLILoginExchangeResult{
		APIKey:   key,
		User:     *user,
		AuthType: "api_key",
	}, nil
}
