package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
	"github.com/google/uuid"
)

var ErrThreadNotFound = errors.New("Thread not found.")
var ErrAPIKeyNotFound = errors.New("API key not found.")
var ErrInvalidLogin = errors.New("Invalid email or password.")

type Repository interface {
	EnsureSchema(ctx context.Context) error
	ListThreads(ctx context.Context, tenantID string, limit int) ([]types.Thread, error)
	SearchThreads(ctx context.Context, tenantID string, params types.SearchThreadParams) ([]types.SearchThreadResult, error)
	CreateThread(ctx context.Context, tenantID string, title string, auth types.AuthContext) (types.Thread, error)
	CreateThreadWithMessage(ctx context.Context, tenantID string, title string, auth types.AuthContext, body string, bodyContentType *string) (types.Thread, types.Message, error)
	GetThread(ctx context.Context, tenantID string, threadID string) (*types.ThreadWithMessages, error)
	GetAsset(ctx context.Context, tenantID string, assetID string) (*types.Asset, error)
	CreatePendingUpload(ctx context.Context, upload types.PendingUpload) (types.PendingUpload, error)
	GetPendingUploads(ctx context.Context, tenantID string, threadID string, uploadIDs []string, owner types.AuthContext) ([]types.PendingUpload, error)
	MarkPendingUploadsConsumed(ctx context.Context, tenantID string, threadID string, uploadIDs []string, owner types.AuthContext) error
	PostMessage(ctx context.Context, tenantID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, assets []types.NewAsset) (types.Message, error)
	CreateAPIKey(ctx context.Context, tenantID string, userID string, name string, key string, tokenHash string, tokenPrefix string, scopes []string) (types.APIKey, error)
	ListAPIKeys(ctx context.Context, tenantID string) ([]types.APIKey, error)
	RevokeAPIKey(ctx context.Context, tenantID string, name string) (bool, error)
	FindAPIKeyBySecret(ctx context.Context, key string) (*types.APIKey, error)
	MarkAPIKeyUsed(ctx context.Context, keyID string) error
	UpsertTenant(ctx context.Context, tenant types.Tenant) (types.Tenant, error)
	GetTenant(ctx context.Context, idOrSlug string) (*types.Tenant, error)
	UpsertProvisionedUser(ctx context.Context, tenantID string, email string, displayName string, passwordHash *string, role string) (types.User, error)
	FindUserByEmail(ctx context.Context, tenantID string, email string) (*types.User, error)
	CreateUserSession(ctx context.Context, session types.UserSession) (types.UserSession, error)
	FindUserSessionBySecretHash(ctx context.Context, secretHash string) (*types.UserSession, *types.User, error)
	MarkUserSessionUsed(ctx context.Context, sessionID string) error
	RevokeUserSession(ctx context.Context, sessionID string) error
	CreateCLILoginCode(ctx context.Context, code types.CLILoginCode) (types.CLILoginCode, error)
	ConsumeCLILoginCode(ctx context.Context, codeHash string, stateHash string, redirectURI string) (*types.CLILoginCode, *types.User, error)
}

type Service struct {
	repo   Repository
	assets assets.AssetStore
}

func New(repo Repository, assetStore assets.AssetStore) *Service {
	return &Service{repo: repo, assets: assetStore}
}

func (s *Service) ListThreads(ctx context.Context, auth types.AuthContext, limit int) ([]types.Thread, error) {
	if err := requireAuthContext(auth); err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 50
	}
	return s.repo.ListThreads(ctx, auth.TenantID, limit)
}

func (s *Service) SearchThreads(ctx context.Context, auth types.AuthContext, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
	if err := requireAuthContext(auth); err != nil {
		return nil, err
	}
	params.Query = strings.TrimSpace(params.Query)
	if params.Query == "" {
		return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "query is required."}
	}
	if params.Limit == 0 {
		params.Limit = 20
	}
	if params.Limit < 1 {
		params.Limit = 1
	}
	if params.Limit > 100 {
		params.Limit = 100
	}
	if params.CreatedBy != nil {
		value := strings.TrimSpace(*params.CreatedBy)
		params.CreatedBy = &value
	}
	if params.UpdatedAfter != nil {
		value := strings.TrimSpace(*params.UpdatedAfter)
		params.UpdatedAfter = &value
	}
	if params.UpdatedAfter != nil && *params.UpdatedAfter != "" {
		if _, err := time.Parse(time.RFC3339, *params.UpdatedAfter); err != nil {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "updated_after must be an RFC3339 timestamp."}
		}
	}
	return s.repo.SearchThreads(ctx, auth.TenantID, params)
}

func (s *Service) CreateThread(ctx context.Context, auth types.AuthContext, title string) (types.Thread, error) {
	if err := requireAuthContext(auth); err != nil {
		return types.Thread{}, err
	}
	if err := validate.CreateThreadTitle(title); err != nil {
		return types.Thread{}, err
	}
	return s.repo.CreateThread(ctx, auth.TenantID, title, auth)
}

func (s *Service) CreateThreadWithMessage(ctx context.Context, auth types.AuthContext, title string, body string, bodyContentType *string) (types.Thread, types.Message, error) {
	if err := requireAuthContext(auth); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	if err := validate.CreateThreadTitle(title); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	resolvedContentType, err := messageformat.Resolve(bodyContentType, body, "")
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	return s.repo.CreateThreadWithMessage(ctx, auth.TenantID, title, auth, body, &resolvedContentType)
}

func (s *Service) GetThread(ctx context.Context, auth types.AuthContext, threadID string) (*types.ThreadWithMessages, error) {
	if err := requireAuthContext(auth); err != nil {
		return nil, err
	}
	thread, err := s.repo.GetThread(ctx, auth.TenantID, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	return thread, nil
}

func (s *Service) GetAsset(ctx context.Context, auth types.AuthContext, assetID string) (*types.Asset, error) {
	if err := requireAuthContext(auth); err != nil {
		return nil, err
	}
	return s.repo.GetAsset(ctx, auth.TenantID, assetID)
}

func (s *Service) PostMessage(ctx context.Context, auth types.AuthContext, params PostMessageParams) (types.Message, error) {
	if err := requireAuthContext(auth); err != nil {
		return types.Message{}, err
	}
	if err := validate.PostMessage(params.ThreadID); err != nil {
		return types.Message{}, err
	}
	thread, err := s.repo.GetThread(ctx, auth.TenantID, params.ThreadID)
	if err != nil {
		return types.Message{}, err
	}
	if thread == nil {
		return types.Message{}, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	bodyContentType, err := messageformat.Resolve(params.BodyContentType, params.Body, "")
	if err != nil {
		return types.Message{}, err
	}
	newAssets := []types.NewAsset{}
	if params.File != nil {
		asset, err := s.assets.UploadChatGPTFile(ctx, auth.TenantID, params.ThreadID, *params.File)
		if err != nil {
			return types.Message{}, err
		}
		asset.TenantID = auth.TenantID
		newAssets = append(newAssets, asset)
	}
	if len(params.UploadedAssets) > 0 {
		assets, err := s.pendingUploadsToAssets(ctx, auth, params.ThreadID, params.UploadedAssets)
		if err != nil {
			return types.Message{}, err
		}
		newAssets = append(newAssets, assets...)
	}
	message, err := s.repo.PostMessage(ctx, auth.TenantID, params.ThreadID, auth, params.Body, &bodyContentType, newAssets)
	if err != nil {
		return types.Message{}, err
	}
	if len(params.UploadedAssets) > 0 {
		ids := make([]string, 0, len(params.UploadedAssets))
		for _, uploaded := range params.UploadedAssets {
			ids = append(ids, strings.TrimSpace(uploaded.UploadID))
		}
		if err := s.repo.MarkPendingUploadsConsumed(ctx, auth.TenantID, params.ThreadID, ids, auth); err != nil {
			return types.Message{}, err
		}
	}
	return message, nil
}

func (s *Service) PostMessageWithAsset(ctx context.Context, auth types.AuthContext, params PostMessageWithAssetParams) (types.Message, error) {
	if err := requireAuthContext(auth); err != nil {
		return types.Message{}, err
	}
	if err := validate.PostMessage(params.ThreadID); err != nil {
		return types.Message{}, err
	}
	thread, err := s.repo.GetThread(ctx, auth.TenantID, params.ThreadID)
	if err != nil {
		return types.Message{}, err
	}
	if thread == nil {
		return types.Message{}, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	bodyContentType, err := messageformat.Resolve(params.BodyContentType, params.Body, "")
	if err != nil {
		return types.Message{}, err
	}
	newAssets := []types.NewAsset{}
	if len(params.Bytes) > 0 || params.FileName != "" {
		asset, err := s.assets.UploadAssetBytes(ctx, assets.UploadBytesParams{
			TenantID: auth.TenantID,
			ThreadID: params.ThreadID,
			Bytes:    params.Bytes,
			FileName: params.FileName,
			MimeType: params.MimeType,
		})
		if err != nil {
			return types.Message{}, err
		}
		asset.TenantID = auth.TenantID
		newAssets = append(newAssets, asset)
	}
	return s.repo.PostMessage(ctx, auth.TenantID, params.ThreadID, auth, params.Body, &bodyContentType, newAssets)
}

func (s *Service) CreatePresignedUploads(ctx context.Context, auth types.AuthContext, threadID string, files []types.UploadIntentFile) ([]types.PresignedUpload, error) {
	if err := requireAuthContext(auth); err != nil {
		return nil, err
	}
	if err := validate.PostMessage(threadID); err != nil {
		return nil, err
	}
	thread, err := s.repo.GetThread(ctx, auth.TenantID, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	if len(files) == 0 {
		return []types.PresignedUpload{}, nil
	}
	if len(files) > 10 {
		return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "At most 10 files can be uploaded at once."}
	}
	uploads := make([]types.PresignedUpload, 0, len(files))
	for _, file := range files {
		file.FileName = strings.TrimSpace(file.FileName)
		if file.FileName == "" {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "file_name is required."}
		}
		if file.SizeBytes < 0 {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "size_bytes must be >= 0."}
		}
		uploadID := "upl_" + uuid.NewString()
		presigned, err := s.assets.CreatePresignedAssetUploadURL(ctx, assets.PresignedUploadParams{
			TenantID:         auth.TenantID,
			ThreadID:         threadID,
			UploadID:         uploadID,
			FileName:         file.FileName,
			MimeType:         file.MimeType,
			SizeBytes:        file.SizeBytes,
			ExpiresInSeconds: 900,
		})
		if err != nil {
			return nil, err
		}
		expiresAt := time.Now().UTC().Add(time.Duration(presigned.ExpiresIn) * time.Second).Format("2006-01-02T15:04:05.000Z")
		if _, err := s.repo.CreatePendingUpload(ctx, types.PendingUpload{
			ID:              presigned.UploadID,
			TenantID:        auth.TenantID,
			ThreadID:        threadID,
			StorageKey:      presigned.StorageKey,
			FileName:        presigned.FileName,
			MimeType:        presigned.MimeType,
			SizeBytes:       presigned.SizeBytes,
			PublicURL:       presigned.PublicURL,
			ExpiresAt:       expiresAt,
			CreatedBy:       auth.ActorName,
			CreatedByUserID: optionalString(auth.UserID),
			CreatedByKeyID:  optionalString(auth.KeyID),
		}); err != nil {
			return nil, err
		}
		uploads = append(uploads, presigned)
	}
	return uploads, nil
}

func (s *Service) pendingUploadsToAssets(ctx context.Context, auth types.AuthContext, threadID string, refs []types.UploadedAssetReference) ([]types.NewAsset, error) {
	ids := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		id := strings.TrimSpace(ref.UploadID)
		if id == "" {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "upload_id is required."}
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return []types.NewAsset{}, nil
	}
	pending, err := s.repo.GetPendingUploads(ctx, auth.TenantID, threadID, ids, auth)
	if err != nil {
		return nil, err
	}
	byID := map[string]types.PendingUpload{}
	for _, upload := range pending {
		byID[upload.ID] = upload
	}
	now := time.Now().UTC()
	assets := make([]types.NewAsset, 0, len(ids))
	for _, id := range ids {
		upload, ok := byID[id]
		if !ok {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "Upload was not found or is no longer available."}
		}
		if upload.ConsumedAt != nil {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "Upload has already been used."}
		}
		if parsed, err := time.Parse(time.RFC3339, upload.ExpiresAt); err == nil && now.After(parsed) {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "Upload has expired."}
		}
		assets = append(assets, types.NewAsset{
			TenantID:   auth.TenantID,
			StorageKey: upload.StorageKey,
			FileName:   upload.FileName,
			MimeType:   upload.MimeType,
			SizeBytes:  upload.SizeBytes,
			PublicURL:  upload.PublicURL,
		})
	}
	return assets, nil
}

func (s *Service) SignedAssetDownloadURL(ctx context.Context, asset types.Asset, expiresInSeconds int) (string, error) {
	return s.assets.CreateSignedAssetDownloadURL(ctx, assets.SignedURLParams{
		StorageKey:       asset.StorageKey,
		FileName:         asset.FileName,
		MimeType:         asset.MimeType,
		ExpiresInSeconds: validate.ClampSignedURLExpiry(expiresInSeconds),
	})
}

func (s *Service) CreateAPIKey(ctx context.Context, auth types.AuthContext, name string) (types.APIKey, error) {
	return s.CreateAPIKeyWithScopes(ctx, auth, name, defaultAPIKeyScopes())
}

func (s *Service) CreateAPIKeyWithScopes(ctx context.Context, auth types.AuthContext, name string, scopes []string) (types.APIKey, error) {
	if err := requireAuthContext(auth); err != nil {
		return types.APIKey{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return types.APIKey{}, errors.New("API key name is required.")
	}
	secret, err := generateSecret()
	if err != nil {
		return types.APIKey{}, err
	}
	return s.repo.CreateAPIKey(ctx, auth.TenantID, auth.UserID, name, secret, hashSecret(secret), tokenPrefix(secret), normalizeScopes(scopes))
}

func (s *Service) ListAPIKeys(ctx context.Context, auth types.AuthContext) ([]types.APIKey, error) {
	if err := requireAuthContext(auth); err != nil {
		return nil, err
	}
	return s.repo.ListAPIKeys(ctx, auth.TenantID)
}

func (s *Service) RevokeAPIKey(ctx context.Context, auth types.AuthContext, name string) error {
	if err := requireAuthContext(auth); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("API key name is required.")
	}
	removed, err := s.repo.RevokeAPIKey(ctx, auth.TenantID, name)
	if err != nil {
		return err
	}
	if !removed {
		return ErrAPIKeyNotFound
	}
	return nil
}

type ProvisionTenantParams struct {
	TenantSlug string
	TenantName string
	UserEmail  string
	UserName   string
	Password   string
	CreateKey  bool
	KeyName    string
	UserRole   string
}

type ProvisionTenantResult struct {
	Tenant     types.Tenant  `json:"tenant"`
	User       types.User    `json:"user,omitempty"`
	APIKey     *types.APIKey `json:"api_key,omitempty"`
	SetupToken string        `json:"setup_token,omitempty"`
}

type CLILoginAuthorizeResult struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

type CLILoginExchangeResult struct {
	ProfileName string       `json:"profile_name,omitempty"`
	BaseURL     string       `json:"base_url,omitempty"`
	APIKey      types.APIKey `json:"api_key"`
	Tenant      types.Tenant `json:"tenant"`
	User        types.User   `json:"user"`
	AuthType    string       `json:"auth_type"`
}

type ProvisionUserParams struct {
	TenantIDOrSlug string
	Email          string
	DisplayName    string
	Password       string
	Role           string
}

func (s *Service) ProvisionTenant(ctx context.Context, params ProvisionTenantParams) (ProvisionTenantResult, error) {
	slug, err := normalizeTenantSlug(params.TenantSlug)
	if err != nil {
		return ProvisionTenantResult{}, err
	}
	name := strings.TrimSpace(params.TenantName)
	if name == "" {
		name = slug
	}
	tenant, err := s.repo.UpsertTenant(ctx, types.Tenant{
		ID:   tenantIDForSlug(slug),
		Slug: slug,
		Name: name,
	})
	if err != nil {
		return ProvisionTenantResult{}, err
	}
	user, setupToken, err := s.provisionUser(ctx, tenant.ID, ProvisionUserParams{
		Email:       params.UserEmail,
		DisplayName: params.UserName,
		Password:    params.Password,
		Role:        firstNonEmpty(params.UserRole, "admin"),
	})
	if err != nil {
		return ProvisionTenantResult{}, err
	}
	result := ProvisionTenantResult{Tenant: tenant, User: user, SetupToken: setupToken}
	if params.CreateKey {
		keyName := strings.TrimSpace(params.KeyName)
		if keyName == "" {
			keyName = "cli"
		}
		key, err := s.CreateAPIKeyWithScopes(ctx, types.AuthContext{
			TenantID:    tenant.ID,
			TenantSlug:  tenant.Slug,
			UserID:      user.ID,
			SubjectType: types.AuthSubjectAdmin,
			ActorName:   "admin",
			Role:        "admin",
		}, keyName, cliAPIKeyScopes())
		if err != nil {
			return ProvisionTenantResult{}, err
		}
		result.APIKey = &key
	}
	return result, nil
}

func (s *Service) ProvisionUser(ctx context.Context, params ProvisionUserParams) (types.User, string, error) {
	tenantIDOrSlug := strings.TrimSpace(params.TenantIDOrSlug)
	if tenantIDOrSlug == "" {
		return types.User{}, "", CodedError{Code: "INVALID_ARGUMENT", Message: "tenant_id is required."}
	}
	tenant, err := s.repo.GetTenant(ctx, tenantIDOrSlug)
	if err != nil {
		return types.User{}, "", err
	}
	if tenant == nil {
		return types.User{}, "", CodedError{Code: "TENANT_NOT_FOUND", Message: "Tenant not found."}
	}
	return s.provisionUser(ctx, tenant.ID, params)
}

func (s *Service) ProvisionTenantAPIKey(ctx context.Context, tenantIDOrSlug string, name string) (types.APIKey, error) {
	tenantIDOrSlug = strings.TrimSpace(tenantIDOrSlug)
	if tenantIDOrSlug == "" {
		return types.APIKey{}, CodedError{Code: "INVALID_ARGUMENT", Message: "tenant_id is required."}
	}
	tenant, err := s.repo.GetTenant(ctx, tenantIDOrSlug)
	if err != nil {
		return types.APIKey{}, err
	}
	if tenant == nil {
		return types.APIKey{}, CodedError{Code: "TENANT_NOT_FOUND", Message: "Tenant not found."}
	}
	return s.CreateAPIKeyWithScopes(ctx, types.AuthContext{
		TenantID:    tenant.ID,
		TenantSlug:  tenant.Slug,
		SubjectType: types.AuthSubjectAdmin,
		ActorName:   "admin",
		Role:        "admin",
	}, name, cliAPIKeyScopes())
}

func (s *Service) provisionUser(ctx context.Context, tenantID string, params ProvisionUserParams) (types.User, string, error) {
	email := strings.TrimSpace(params.Email)
	if email == "" {
		return types.User{}, "", CodedError{Code: "INVALID_ARGUMENT", Message: "user_email is required."}
	}
	displayName := strings.TrimSpace(params.DisplayName)
	if displayName == "" {
		displayName = email
	}
	role := strings.TrimSpace(params.Role)
	if role == "" {
		role = "admin"
	}
	if role != "admin" && role != "member" {
		return types.User{}, "", CodedError{Code: "INVALID_ARGUMENT", Message: "role must be admin or member."}
	}
	password := strings.TrimSpace(params.Password)
	setupToken := ""
	if password == "" {
		existing, err := s.repo.FindUserByEmail(ctx, tenantID, email)
		if err != nil {
			return types.User{}, "", err
		}
		if existing == nil || existing.PasswordHash == nil {
			token, err := generateSetupToken()
			if err != nil {
				return types.User{}, "", err
			}
			password = token
			setupToken = token
		}
	}
	var passwordHash *string
	if password != "" {
		hashed, err := auth.HashPassword(password)
		if err != nil {
			return types.User{}, "", err
		}
		passwordHash = &hashed
	}
	user, err := s.repo.UpsertProvisionedUser(ctx, tenantID, email, displayName, passwordHash, role)
	if err != nil {
		return types.User{}, "", err
	}
	return user, setupToken, nil
}

func (s *Service) AuthenticateAPIKey(ctx context.Context, secret string) (*types.AuthContext, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, nil
	}
	key, err := s.repo.FindAPIKeyBySecret(ctx, secret)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, nil
	}
	if key.ID != "" {
		if err := s.repo.MarkAPIKeyUsed(ctx, key.ID); err != nil {
			return nil, err
		}
	}
	tenantID := key.TenantID
	if tenantID == "" {
		tenantID = types.DefaultTenantID
	}
	authContext := &types.AuthContext{
		TenantID:    tenantID,
		UserID:      stringValue(key.UserID),
		SubjectType: types.AuthSubjectAPIKey,
		ActorName:   key.Name,
		KeyID:       key.ID,
		Scopes:      append([]string(nil), key.Scopes...),
	}
	if tenant, err := s.repo.GetTenant(ctx, tenantID); err != nil {
		return nil, err
	} else if tenant != nil {
		authContext.TenantSlug = tenant.Slug
	}
	return authContext, nil
}

func (s *Service) Login(ctx context.Context, tenantID string, email string, password string) (types.AuthContext, string, error) {
	email = strings.TrimSpace(email)
	tenantID = strings.TrimSpace(tenantID)
	if email == "" || password == "" {
		return types.AuthContext{}, "", ErrInvalidLogin
	}
	user, err := s.repo.FindUserByEmail(ctx, tenantID, email)
	if err != nil {
		return types.AuthContext{}, "", err
	}
	if user == nil || user.PasswordHash == nil || !auth.VerifyPassword(password, *user.PasswordHash) {
		return types.AuthContext{}, "", ErrInvalidLogin
	}
	secret, err := generateSessionSecret()
	if err != nil {
		return types.AuthContext{}, "", err
	}
	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	session, err := s.repo.CreateUserSession(ctx, types.UserSession{
		TenantID:   user.TenantID,
		UserID:     user.ID,
		SecretHash: hashSecret(secret),
		ExpiresAt:  expiresAt,
	})
	if err != nil {
		return types.AuthContext{}, "", err
	}
	authContext := authContextForUserSession(session, *user)
	if tenant, err := s.repo.GetTenant(ctx, user.TenantID); err != nil {
		return types.AuthContext{}, "", err
	} else if tenant != nil {
		authContext.TenantSlug = tenant.Slug
	}
	return authContext, secret, nil
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
	if tenant, err := s.repo.GetTenant(ctx, user.TenantID); err != nil {
		return nil, err
	} else if tenant != nil {
		authContext.TenantSlug = tenant.Slug
	}
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
		TenantID:    authContext.TenantID,
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
	tenant, err := s.repo.GetTenant(ctx, loginCode.TenantID)
	if err != nil {
		return CLILoginExchangeResult{}, err
	}
	if tenant == nil {
		return CLILoginExchangeResult{}, CodedError{Code: "TENANT_NOT_FOUND", Message: "Tenant not found."}
	}
	keyName = strings.TrimSpace(keyName)
	if keyName == "" {
		keyName = defaultCLIKeyName()
	}
	key, err := s.CreateAPIKeyWithScopes(ctx, types.AuthContext{
		TenantID:    tenant.ID,
		TenantSlug:  tenant.Slug,
		UserID:      user.ID,
		SubjectType: types.AuthSubjectUserSession,
		ActorName:   user.DisplayName,
		Role:        user.Role,
	}, keyName, cliAPIKeyScopes())
	if err != nil {
		return CLILoginExchangeResult{}, err
	}
	return CLILoginExchangeResult{
		APIKey:   key,
		Tenant:   *tenant,
		User:     *user,
		AuthType: "api_key",
	}, nil
}

func generateSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "agb_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func defaultAPIKeyScopes() []string {
	return []string{"threads:read", "threads:write", "assets:read", "assets:write", "mcp:use"}
}

func cliAPIKeyScopes() []string {
	return append(defaultAPIKeyScopes(), "keys:read", "keys:write")
}

func ConnectorAPIKeyScopes(purpose string) []string {
	switch strings.ToLower(strings.TrimSpace(purpose)) {
	case "chatgpt", "raycast":
		return defaultAPIKeyScopes()
	default:
		return defaultAPIKeyScopes()
	}
}

func normalizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return defaultAPIKeyScopes()
	}
	seen := map[string]bool{}
	normalized := []string{}
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		normalized = append(normalized, scope)
	}
	if len(normalized) == 0 {
		return defaultAPIKeyScopes()
	}
	return normalized
}

func generateSessionSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "ags_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func generateSetupToken() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "setup_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func generateCLILoginCode() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "cli_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func defaultCLIKeyName() string {
	return "cli"
}

func validateCLIRedirectURI(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "redirect_uri is invalid."}
	}
	if parsed.Scheme != "http" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "redirect_uri must use http."}
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "redirect_uri must point to localhost."}
	}
	if parsed.Port() == "" || parsed.Path != "/callback" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "redirect_uri must include a localhost callback port and /callback path."}
	}
	return nil
}

func authContextForUserSession(session types.UserSession, user types.User) types.AuthContext {
	return types.AuthContext{
		TenantID:    user.TenantID,
		UserID:      user.ID,
		SubjectType: types.AuthSubjectUserSession,
		ActorName:   user.DisplayName,
		SessionID:   session.ID,
		Role:        user.Role,
	}
}

func hashSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func tokenPrefix(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func normalizeTenantSlug(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "tenant_slug is required."}
	}
	if len(value) > 80 {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "tenant_slug must be at most 80 characters."}
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "tenant_slug may contain only lowercase letters, numbers, hyphens, and underscores."}
	}
	return value, nil
}

func tenantIDForSlug(slug string) string {
	if slug == "default" {
		return types.DefaultTenantID
	}
	return "ten_" + slug
}

func requireAuthContext(auth types.AuthContext) error {
	if strings.TrimSpace(auth.TenantID) == "" {
		return CodedError{Code: "PERMISSION_DENIED", Message: "Authentication context is required."}
	}
	if strings.TrimSpace(auth.ActorName) == "" {
		return CodedError{Code: "PERMISSION_DENIED", Message: "Authentication context is required."}
	}
	return nil
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type PostMessageParams struct {
	ThreadID        string
	Body            string
	BodyContentType *string
	File            *assets.ChatGPTFileInput
	UploadedAssets  []types.UploadedAssetReference
}

type PostMessageWithAssetParams struct {
	ThreadID        string
	Body            string
	BodyContentType *string
	Bytes           []byte
	FileName        string
	MimeType        *string
}

type CodedError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

func (e CodedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func (e CodedError) Unwrap() error {
	return e.Err
}
