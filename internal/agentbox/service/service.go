package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
	"github.com/google/uuid"
)

var ErrThreadNotFound = types.ErrThreadNotFound
var ErrAPIKeyNotFound = errors.New("API key not found.")
var ErrInvalidLogin = errors.New("Invalid email or password.")

type Repository interface {
	ResolveThreadAccess(ctx context.Context, userID string, threadID string) (*types.ThreadAccess, error)
	ListThreads(ctx context.Context, userID string, limit int) ([]types.Thread, error)
	SearchThreads(ctx context.Context, userID string, params types.SearchThreadParams) ([]types.SearchThreadResult, error)
	CreateThread(ctx context.Context, userID string, title string, auth types.AuthContext) (types.Thread, error)
	CreateThreadWithMessage(ctx context.Context, userID string, title string, auth types.AuthContext, body string, bodyContentType *string) (types.Thread, types.Message, error)
	GetThread(ctx context.Context, userID string, threadID string) (*types.ThreadWithMessages, error)
	GetAsset(ctx context.Context, userID string, assetID string) (*types.Asset, error)
	CreatePendingUpload(ctx context.Context, userID string, upload types.PendingUpload) (types.PendingUpload, error)
	GetPendingUploads(ctx context.Context, userID string, threadID string, uploadIDs []string, actor types.AuthContext) ([]types.PendingUpload, error)
	MarkPendingUploadsConsumed(ctx context.Context, userID string, threadID string, uploadIDs []string, actor types.AuthContext) error
	PostMessage(ctx context.Context, userID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, assets []types.NewAsset) (types.Message, error)
	CreateAPIKey(ctx context.Context, userID string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string) (types.APIKey, error)
	ListAPIKeys(ctx context.Context, userID string) ([]types.APIKey, error)
	RevokeAPIKey(ctx context.Context, userID string, name string) (bool, error)
	FindAPIKeyBySecret(ctx context.Context, key string) (*types.APIKey, *types.User, error)
	MarkAPIKeyUsed(ctx context.Context, keyID string) error
	UpsertTenant(ctx context.Context, tenant types.Tenant) (types.Tenant, error)
	GetTenant(ctx context.Context, idOrSlug string) (*types.Tenant, error)
	BootstrapOwner(ctx context.Context, email string, displayName string, passwordHash string) (types.User, error)
	UpsertProvisionedUser(ctx context.Context, tenantID string, email string, displayName string, passwordHash *string, role string) (types.User, error)
	FindUserByEmail(ctx context.Context, tenantID string, email string) (*types.User, error)
	CreateUserSession(ctx context.Context, session types.UserSession) (types.UserSession, error)
	FindUserSessionBySecretHash(ctx context.Context, secretHash string) (*types.UserSession, *types.User, error)
	MarkUserSessionUsed(ctx context.Context, sessionID string) error
	RevokeUserSession(ctx context.Context, sessionID string) error
	CreateCLILoginCode(ctx context.Context, code types.CLILoginCode) (types.CLILoginCode, error)
	ConsumeCLILoginCode(ctx context.Context, codeHash string, stateHash string, redirectURI string) (*types.CLILoginCode, *types.User, error)
	CreateOwnerSetupToken(ctx context.Context, tokenHash string, expiresAt time.Time) (types.OwnerSetupToken, error)
	UseOwnerSetupToken(ctx context.Context, tokenHash string, email string, displayName string, passwordHash string) (types.User, types.OwnerSetupToken, error)
	CreateSignupInvitation(ctx context.Context, createdByUserID string, tokenHash string, expiresAt time.Time, teamIDs []string) (types.SignupInvitation, error)
	ListSignupInvitations(ctx context.Context) ([]types.SignupInvitation, error)
	RevokeSignupInvitation(ctx context.Context, invitationID string) (bool, error)
	FindSignupInvitation(ctx context.Context, tokenHash string) (*types.SignupInvitation, error)
	RegisterWithSignupInvitation(ctx context.Context, tokenHash string, email string, displayName string, passwordHash string, sessionSecretHash string, sessionExpiresAt time.Time) (types.User, types.UserSession, types.SignupInvitation, error)
	ListUsers(ctx context.Context) ([]types.User, error)
	SetUserDisabled(ctx context.Context, userID string, disabled bool) (types.User, error)
	CreateTeam(ctx context.Context, slug string, name string) (types.Team, error)
	RenameTeam(ctx context.Context, teamID string, name string) (types.Team, error)
	ListTeams(ctx context.Context) ([]types.Team, error)
	ListUserTeams(ctx context.Context, userID string) ([]types.Team, error)
	ListTeamMembers(ctx context.Context, teamID string) ([]types.User, error)
	AddTeamMember(ctx context.Context, teamID string, userID string) (types.TeamMembership, error)
	RemoveTeamMember(ctx context.Context, teamID string, userID string) (bool, error)
}

var teamSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Service struct {
	repo   Repository
	assets assets.AssetStore
}

func New(repo Repository, assetStore assets.AssetStore) *Service {
	return &Service{repo: repo, assets: assetStore}
}

func (s *Service) ListThreads(ctx context.Context, auth types.AuthContext, limit int) ([]types.Thread, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 50
	}
	return s.repo.ListThreads(ctx, auth.UserID, limit)
}

func (s *Service) SearchThreads(ctx context.Context, auth types.AuthContext, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
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
	return s.repo.SearchThreads(ctx, auth.UserID, params)
}

func (s *Service) CreateThread(ctx context.Context, auth types.AuthContext, title string) (types.Thread, error) {
	if err := requireScope(auth, "threads:write"); err != nil {
		return types.Thread{}, err
	}
	if err := validate.CreateThreadTitle(title); err != nil {
		return types.Thread{}, err
	}
	return s.repo.CreateThread(ctx, auth.UserID, title, auth)
}

func (s *Service) CreateThreadWithMessage(ctx context.Context, auth types.AuthContext, title string, body string, bodyContentType *string) (types.Thread, types.Message, error) {
	if err := requireScope(auth, "threads:write"); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	if err := validate.CreateThreadTitle(title); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	resolvedContentType, err := messageformat.Resolve(bodyContentType, body, "")
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	return s.repo.CreateThreadWithMessage(ctx, auth.UserID, title, auth, body, &resolvedContentType)
}

func (s *Service) GetThread(ctx context.Context, auth types.AuthContext, threadID string) (*types.ThreadWithMessages, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return nil, err
	}
	thread, err := s.repo.GetThread(ctx, auth.UserID, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	return thread, nil
}

func (s *Service) GetAsset(ctx context.Context, auth types.AuthContext, assetID string) (*types.Asset, error) {
	if err := requireScope(auth, "assets:read"); err != nil {
		return nil, err
	}
	return s.repo.GetAsset(ctx, auth.UserID, assetID)
}

func (s *Service) PostMessage(ctx context.Context, auth types.AuthContext, params PostMessageParams) (types.Message, error) {
	if err := requireScope(auth, "threads:write"); err != nil {
		return types.Message{}, err
	}
	if params.File != nil || len(params.UploadedAssets) > 0 {
		if err := requireScope(auth, "assets:write"); err != nil {
			return types.Message{}, err
		}
	}
	if err := validate.PostMessage(params.ThreadID); err != nil {
		return types.Message{}, err
	}
	thread, err := s.repo.GetThread(ctx, auth.UserID, params.ThreadID)
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
		asset, err := s.assets.UploadChatGPTFile(ctx, auth.UserID, params.ThreadID, *params.File)
		if err != nil {
			return types.Message{}, err
		}
		newAssets = append(newAssets, asset)
	}
	if len(params.UploadedAssets) > 0 {
		assets, err := s.pendingUploadsToAssets(ctx, auth, params.ThreadID, params.UploadedAssets)
		if err != nil {
			return types.Message{}, err
		}
		newAssets = append(newAssets, assets...)
	}
	message, err := s.repo.PostMessage(ctx, auth.UserID, params.ThreadID, auth, params.Body, &bodyContentType, newAssets)
	if err != nil {
		return types.Message{}, err
	}
	if len(params.UploadedAssets) > 0 {
		ids := make([]string, 0, len(params.UploadedAssets))
		for _, uploaded := range params.UploadedAssets {
			ids = append(ids, strings.TrimSpace(uploaded.UploadID))
		}
		if err := s.repo.MarkPendingUploadsConsumed(ctx, auth.UserID, params.ThreadID, ids, auth); err != nil {
			return types.Message{}, err
		}
	}
	return message, nil
}

func (s *Service) PostMessageWithAsset(ctx context.Context, auth types.AuthContext, params PostMessageWithAssetParams) (types.Message, error) {
	if err := requireScope(auth, "threads:write"); err != nil {
		return types.Message{}, err
	}
	if len(params.Bytes) > 0 || params.FileName != "" {
		if err := requireScope(auth, "assets:write"); err != nil {
			return types.Message{}, err
		}
	}
	if err := validate.PostMessage(params.ThreadID); err != nil {
		return types.Message{}, err
	}
	thread, err := s.repo.GetThread(ctx, auth.UserID, params.ThreadID)
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
			UserID:   auth.UserID,
			ThreadID: params.ThreadID,
			Bytes:    params.Bytes,
			FileName: params.FileName,
			MimeType: params.MimeType,
		})
		if err != nil {
			return types.Message{}, err
		}
		newAssets = append(newAssets, asset)
	}
	return s.repo.PostMessage(ctx, auth.UserID, params.ThreadID, auth, params.Body, &bodyContentType, newAssets)
}

func (s *Service) CreatePresignedUploads(ctx context.Context, auth types.AuthContext, threadID string, files []types.UploadIntentFile) ([]types.PresignedUpload, error) {
	if err := requireScope(auth, "threads:write"); err != nil {
		return nil, err
	}
	if err := requireScope(auth, "assets:write"); err != nil {
		return nil, err
	}
	if err := validate.PostMessage(threadID); err != nil {
		return nil, err
	}
	thread, err := s.repo.GetThread(ctx, auth.UserID, threadID)
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
			UserID:           auth.UserID,
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
		if _, err := s.repo.CreatePendingUpload(ctx, auth.UserID, types.PendingUpload{
			ID:                       presigned.UploadID,
			ThreadID:                 threadID,
			StorageKey:               presigned.StorageKey,
			FileName:                 presigned.FileName,
			MimeType:                 presigned.MimeType,
			SizeBytes:                presigned.SizeBytes,
			PublicURL:                presigned.PublicURL,
			ExpiresAt:                expiresAt,
			CreatedBy:                auth.ActorName,
			CreatedByUserID:          optionalString(auth.UserID),
			CreatedByKeyID:           optionalString(auth.KeyID),
			CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
			CreatedByActorName:       optionalString(auth.ActorName),
		}); err != nil {
			if errors.Is(err, types.ErrThreadNotFound) {
				return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
			}
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
	pending, err := s.repo.GetPendingUploads(ctx, auth.UserID, threadID, ids, auth)
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
			StorageKey: upload.StorageKey,
			FileName:   upload.FileName,
			MimeType:   upload.MimeType,
			SizeBytes:  upload.SizeBytes,
			PublicURL:  upload.PublicURL,
		})
	}
	return assets, nil
}

func (s *Service) SignedAssetDownloadURL(ctx context.Context, auth types.AuthContext, assetID string, expiresInSeconds int) (string, error) {
	if err := requireScope(auth, "assets:read"); err != nil {
		return "", err
	}
	asset, err := s.repo.GetAsset(ctx, auth.UserID, assetID)
	if err != nil {
		return "", err
	}
	if asset == nil {
		return "", CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset not found."}
	}
	return s.assets.CreateSignedAssetDownloadURL(ctx, assets.SignedURLParams{
		StorageKey:       asset.StorageKey,
		FileName:         asset.FileName,
		MimeType:         asset.MimeType,
		ExpiresInSeconds: validate.ClampSignedURLExpiry(expiresInSeconds),
	})
}

func (s *Service) CreateAPIKey(ctx context.Context, auth types.AuthContext, name string) (types.APIKey, error) {
	return s.CreateAPIKeyWithPurposeAndScopes(ctx, auth, name, "custom", defaultAPIKeyScopes())
}

func (s *Service) CreateAPIKeyWithScopes(ctx context.Context, auth types.AuthContext, name string, scopes []string) (types.APIKey, error) {
	return s.CreateAPIKeyWithPurposeAndScopes(ctx, auth, name, "custom", scopes)
}

func (s *Service) CreateAPIKeyWithPurposeAndScopes(ctx context.Context, auth types.AuthContext, name string, purpose string, scopes []string) (types.APIKey, error) {
	if err := requireUserAuthContext(auth); err != nil {
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
	created, err := s.repo.CreateAPIKey(ctx, auth.UserID, name, normalizeCredentialPurpose(purpose), hashSecret(secret), tokenPrefix(secret), normalizeScopes(scopes))
	if err != nil {
		return types.APIKey{}, err
	}
	created.Key = secret
	return created, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, auth types.AuthContext) ([]types.APIKey, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return nil, err
	}
	return s.repo.ListAPIKeys(ctx, auth.UserID)
}

func (s *Service) RevokeAPIKey(ctx context.Context, auth types.AuthContext, name string) error {
	if err := requireUserAuthContext(auth); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("API key name is required.")
	}
	removed, err := s.repo.RevokeAPIKey(ctx, auth.UserID, name)
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
	User        types.User   `json:"user"`
	AuthType    string       `json:"auth_type"`
}

type OwnerSetupTokenResult struct {
	Token     string `json:"token"`
	Purpose   string `json:"purpose"`
	ExpiresAt string `json:"expires_at"`
}

type SignupInvitationTokenResult struct {
	Invitation types.SignupInvitation `json:"invitation"`
	Token      string                 `json:"token"`
}

type SignupInvitationInspection struct {
	Valid     bool   `json:"valid"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type ProvisionUserParams struct {
	TenantIDOrSlug string
	Email          string
	DisplayName    string
	Password       string
	Role           string
}

func (s *Service) ProvisionTenant(ctx context.Context, params ProvisionTenantParams) (ProvisionTenantResult, error) {
	return ProvisionTenantResult{}, CodedError{Code: "LEGACY_TENANT_PROVISIONING_DISABLED", Message: "Tenant provisioning is disabled. Create users only through owner invitations."}
}

func (s *Service) IssueOwnerSetupToken(ctx context.Context, ttl time.Duration) (OwnerSetupTokenResult, error) {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if ttl > 24*time.Hour {
		return OwnerSetupTokenResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "Owner setup token expiry may not exceed 24 hours."}
	}
	secret, err := generateOwnerSetupToken()
	if err != nil {
		return OwnerSetupTokenResult{}, err
	}
	token, err := s.repo.CreateOwnerSetupToken(ctx, hashSecret(secret), time.Now().UTC().Add(ttl))
	if err != nil {
		return OwnerSetupTokenResult{}, err
	}
	return OwnerSetupTokenResult{
		Token:     secret,
		Purpose:   token.Purpose,
		ExpiresAt: token.ExpiresAt,
	}, nil
}

func (s *Service) CompleteOwnerSetup(ctx context.Context, token string, email string, displayName string, password string) (types.AuthContext, string, types.User, error) {
	token = strings.TrimSpace(token)
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if token == "" || email == "" || displayName == "" || password == "" {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "INVALID_ARGUMENT", Message: "token, email, display_name, and password are required."}
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	owner, _, err := s.repo.UseOwnerSetupToken(ctx, hashSecret(token), email, displayName, passwordHash)
	if errors.Is(err, types.ErrOwnerSetupTokenInvalid) {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "INVALID_OWNER_SETUP_TOKEN", Message: "Owner setup token is invalid, expired, revoked, or already used.", Err: err}
	}
	if errors.Is(err, types.ErrOwnerAlreadyExists) {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "OWNER_EMAIL_MISMATCH", Message: "Recovery must use the permanent owner's existing email address.", Err: err}
	}
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	authContext, sessionSecret, err := s.createSessionForUser(ctx, owner)
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	return authContext, sessionSecret, owner, nil
}

func (s *Service) CreateSignupInvitation(ctx context.Context, authContext types.AuthContext, ttl time.Duration, requestedTeamIDs ...string) (SignupInvitationTokenResult, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return SignupInvitationTokenResult{}, err
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	secret, err := generateSignupInvitationToken()
	if err != nil {
		return SignupInvitationTokenResult{}, err
	}
	teamIDs := uniqueTrimmedStrings(requestedTeamIDs)
	invitation, err := s.repo.CreateSignupInvitation(ctx, authContext.UserID, hashSecret(secret), time.Now().UTC().Add(ttl), teamIDs)
	if errors.Is(err, types.ErrTeamNotFound) {
		return SignupInvitationTokenResult{}, CodedError{Code: "TEAM_NOT_FOUND", Message: "One or more selected teams no longer exist.", Err: err}
	}
	if err != nil {
		return SignupInvitationTokenResult{}, err
	}
	return SignupInvitationTokenResult{Invitation: invitation, Token: secret}, nil
}

func (s *Service) ListSignupInvitations(ctx context.Context, authContext types.AuthContext) ([]types.SignupInvitation, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return nil, err
	}
	return s.repo.ListSignupInvitations(ctx)
}

func (s *Service) RevokeSignupInvitation(ctx context.Context, authContext types.AuthContext, invitationID string) error {
	if err := requireOwnerBrowser(authContext); err != nil {
		return err
	}
	invitationID = strings.TrimSpace(invitationID)
	if invitationID == "" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "invitation_id is required."}
	}
	revoked, err := s.repo.RevokeSignupInvitation(ctx, invitationID)
	if err != nil {
		return err
	}
	if !revoked {
		return CodedError{Code: "INVITATION_NOT_FOUND", Message: "Active invitation not found."}
	}
	return nil
}

func (s *Service) InspectSignupInvitation(ctx context.Context, token string) (SignupInvitationInspection, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return SignupInvitationInspection{}, CodedError{Code: "INVALID_INVITATION", Message: "Invitation is invalid, expired, revoked, or already used."}
	}
	invitation, err := s.repo.FindSignupInvitation(ctx, hashSecret(token))
	if err != nil {
		return SignupInvitationInspection{}, err
	}
	if invitation == nil {
		return SignupInvitationInspection{}, CodedError{Code: "INVALID_INVITATION", Message: "Invitation is invalid, expired, revoked, or already used."}
	}
	return SignupInvitationInspection{Valid: true, ExpiresAt: invitation.ExpiresAt}, nil
}

func (s *Service) RegisterWithSignupInvitation(ctx context.Context, token string, email string, displayName string, password string) (types.AuthContext, string, types.User, error) {
	token = strings.TrimSpace(token)
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if token == "" || email == "" || displayName == "" || password == "" {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "INVALID_ARGUMENT", Message: "token, email, display_name, and password are required."}
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	sessionSecret, err := generateSessionSecret()
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	user, session, _, err := s.repo.RegisterWithSignupInvitation(
		ctx,
		hashSecret(token),
		email,
		displayName,
		passwordHash,
		hashSecret(sessionSecret),
		time.Now().UTC().Add(30*24*time.Hour),
	)
	if errors.Is(err, types.ErrSignupInvitationInvalid) {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "INVALID_INVITATION", Message: "Invitation is invalid, expired, revoked, or already used.", Err: err}
	}
	if errors.Is(err, types.ErrEmailAlreadyRegistered) {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "REGISTRATION_UNAVAILABLE", Message: "Registration could not be completed with those details.", Err: err}
	}
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	authContext := authContextForUserSession(session, user)
	if tenant, err := s.repo.GetTenant(ctx, user.TenantID); err != nil {
		return types.AuthContext{}, "", types.User{}, err
	} else if tenant != nil {
		authContext.TenantSlug = tenant.Slug
	}
	return authContext, sessionSecret, user, nil
}

func (s *Service) ListUsers(ctx context.Context, authContext types.AuthContext) ([]types.User, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return nil, err
	}
	return s.repo.ListUsers(ctx)
}

func (s *Service) SetUserDisabled(ctx context.Context, authContext types.AuthContext, userID string, disabled bool) (types.User, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.User{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return types.User{}, CodedError{Code: "INVALID_ARGUMENT", Message: "user_id is required."}
	}
	user, err := s.repo.SetUserDisabled(ctx, userID, disabled)
	if errors.Is(err, types.ErrUserNotFound) {
		return types.User{}, CodedError{Code: "USER_NOT_FOUND", Message: "User not found.", Err: err}
	}
	if errors.Is(err, types.ErrOwnerCannotBeDisabled) {
		return types.User{}, CodedError{Code: "OWNER_IMMUTABLE", Message: "The permanent deployment owner cannot be disabled.", Err: err}
	}
	if err != nil {
		return types.User{}, err
	}
	return user, nil
}

func (s *Service) CreateTeam(ctx context.Context, authContext types.AuthContext, slug string, name string) (types.Team, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.Team{}, err
	}
	slug, err := normalizeTeamSlug(slug)
	if err != nil {
		return types.Team{}, err
	}
	name, err = normalizeTeamName(name)
	if err != nil {
		return types.Team{}, err
	}
	team, err := s.repo.CreateTeam(ctx, slug, name)
	if errors.Is(err, types.ErrTeamSlugConflict) {
		return types.Team{}, CodedError{Code: "TEAM_SLUG_CONFLICT", Message: "That team slug is already in use.", Err: err}
	}
	if err != nil {
		return types.Team{}, err
	}
	return team, nil
}

func (s *Service) RenameTeam(ctx context.Context, authContext types.AuthContext, teamID string, name string) (types.Team, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.Team{}, err
	}
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return types.Team{}, CodedError{Code: "INVALID_ARGUMENT", Message: "team_id is required."}
	}
	name, err := normalizeTeamName(name)
	if err != nil {
		return types.Team{}, err
	}
	team, err := s.repo.RenameTeam(ctx, teamID, name)
	if errors.Is(err, types.ErrTeamNotFound) {
		return types.Team{}, CodedError{Code: "TEAM_NOT_FOUND", Message: "Team not found.", Err: err}
	}
	if err != nil {
		return types.Team{}, err
	}
	return team, nil
}

func (s *Service) ListOwnerTeams(ctx context.Context, authContext types.AuthContext) ([]types.TeamWithMembers, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return nil, err
	}
	teams, err := s.repo.ListTeams(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]types.TeamWithMembers, 0, len(teams))
	for _, team := range teams {
		members, err := s.repo.ListTeamMembers(ctx, team.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, types.TeamWithMembers{Team: team, Members: members})
	}
	return result, nil
}

func (s *Service) ListMyTeams(ctx context.Context, authContext types.AuthContext) ([]types.Team, error) {
	if err := requireUserAuthContext(authContext); err != nil {
		return nil, err
	}
	return s.repo.ListUserTeams(ctx, authContext.UserID)
}

func (s *Service) AddTeamMember(ctx context.Context, authContext types.AuthContext, teamID string, userID string) (types.TeamMembership, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.TeamMembership{}, err
	}
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	if teamID == "" || userID == "" {
		return types.TeamMembership{}, CodedError{Code: "INVALID_ARGUMENT", Message: "team_id and user_id are required."}
	}
	membership, err := s.repo.AddTeamMember(ctx, teamID, userID)
	if errors.Is(err, types.ErrTeamNotFound) {
		return types.TeamMembership{}, CodedError{Code: "TEAM_NOT_FOUND", Message: "Team not found.", Err: err}
	}
	if errors.Is(err, types.ErrUserNotFound) {
		return types.TeamMembership{}, CodedError{Code: "USER_NOT_FOUND", Message: "User not found.", Err: err}
	}
	if err != nil {
		return types.TeamMembership{}, err
	}
	return membership, nil
}

func (s *Service) RemoveTeamMember(ctx context.Context, authContext types.AuthContext, teamID string, userID string) error {
	if err := requireOwnerBrowser(authContext); err != nil {
		return err
	}
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	if teamID == "" || userID == "" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "team_id and user_id are required."}
	}
	_, err := s.repo.RemoveTeamMember(ctx, teamID, userID)
	if errors.Is(err, types.ErrTeamNotFound) {
		return CodedError{Code: "TEAM_NOT_FOUND", Message: "Team not found.", Err: err}
	}
	if errors.Is(err, types.ErrUserNotFound) {
		return CodedError{Code: "USER_NOT_FOUND", Message: "User not found.", Err: err}
	}
	return err
}

func (s *Service) ProvisionUser(ctx context.Context, params ProvisionUserParams) (types.User, string, error) {
	return types.User{}, "", CodedError{Code: "LEGACY_TENANT_PROVISIONING_DISABLED", Message: "Tenant provisioning is disabled. Create users only through owner invitations."}
}

func (s *Service) ProvisionTenantAPIKey(ctx context.Context, tenantIDOrSlug string, name string) (types.APIKey, error) {
	_ = ctx
	_ = tenantIDOrSlug
	_ = name
	return types.APIKey{}, CodedError{Code: "LEGACY_TENANT_KEY_DISABLED", Message: "Tenant-wide credentials are disabled. Create a credential from an authenticated user account."}
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
	tenantID := user.TenantID
	if tenantID == "" {
		tenantID = types.DefaultTenantID
	}
	authContext := &types.AuthContext{
		TenantID:        tenantID,
		UserID:          user.ID,
		UserDisplayName: user.DisplayName,
		SubjectType:     types.AuthSubjectAPIKey,
		ActorID:         key.ID,
		ActorName:       key.Name,
		KeyID:           key.ID,
		Scopes:          append([]string(nil), key.Scopes...),
	}
	if tenant, err := s.repo.GetTenant(ctx, tenantID); err != nil {
		return nil, err
	} else if tenant != nil {
		authContext.TenantSlug = tenant.Slug
	}
	return authContext, nil
}

func (s *Service) Login(ctx context.Context, _ string, email string, password string) (types.AuthContext, string, error) {
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return types.AuthContext{}, "", ErrInvalidLogin
	}
	user, err := s.repo.FindUserByEmail(ctx, "", email)
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
	authContext := authContextForUserSession(session, user)
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
	tenant, err := s.repo.GetTenant(ctx, user.TenantID)
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
	key, err := s.CreateAPIKeyWithPurposeAndScopes(ctx, types.AuthContext{
		TenantID:        tenant.ID,
		TenantSlug:      tenant.Slug,
		UserID:          user.ID,
		UserDisplayName: user.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		ActorID:         loginCode.ID,
		ActorName:       user.DisplayName,
		Role:            user.Role,
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

func generateOwnerSetupToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "agos_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func generateSignupInvitationToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "aginv_" + base64.RawURLEncoding.EncodeToString(bytes), nil
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
		TenantID:        user.TenantID,
		UserID:          user.ID,
		UserDisplayName: user.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		ActorID:         session.ID,
		ActorName:       "Web dashboard",
		SessionID:       session.ID,
		Role:            user.Role,
		IsOwner:         user.IsOwner,
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

func normalizeTeamSlug(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "team slug is required."}
	}
	if len(value) > 63 {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "team slug must be at most 63 characters."}
	}
	if !teamSlugPattern.MatchString(value) {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "team slug may contain lowercase letters, numbers, and single hyphens between words."}
	}
	return value, nil
}

func normalizeTeamName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "team name is required."}
	}
	if len(value) > 120 {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "team name must be at most 120 characters."}
	}
	return value, nil
}

func uniqueTrimmedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func tenantIDForSlug(slug string) string {
	if slug == "default" {
		return types.DefaultTenantID
	}
	return "ten_" + slug
}

func requireAuthContext(auth types.AuthContext) error {
	if strings.TrimSpace(auth.UserID) == "" {
		return CodedError{Code: "PERMISSION_DENIED", Message: "Authentication context is required."}
	}
	if strings.TrimSpace(auth.ActorName) == "" {
		return CodedError{Code: "PERMISSION_DENIED", Message: "Authentication context is required."}
	}
	return nil
}

func requireUserAuthContext(auth types.AuthContext) error {
	if strings.TrimSpace(auth.UserID) == "" || strings.TrimSpace(auth.ActorName) == "" {
		return CodedError{Code: "PERMISSION_DENIED", Message: "Authenticated user context is required."}
	}
	return nil
}

func requireOwnerBrowser(auth types.AuthContext) error {
	if auth.SubjectType != types.AuthSubjectUserSession || !auth.IsOwner || strings.TrimSpace(auth.UserID) == "" || strings.TrimSpace(auth.SessionID) == "" {
		return CodedError{Code: "OWNER_BROWSER_REQUIRED", Message: "A permanent-owner browser session is required."}
	}
	return nil
}

func normalizeCredentialPurpose(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "custom"
	}
	return value
}

func requireScope(auth types.AuthContext, scope string) error {
	if err := requireAuthContext(auth); err != nil {
		return err
	}
	if auth.SubjectType != types.AuthSubjectAPIKey || len(auth.Scopes) == 0 {
		return nil
	}
	for _, candidate := range auth.Scopes {
		if candidate == scope {
			return nil
		}
	}
	return CodedError{Code: "PERMISSION_DENIED", Message: scope + " scope is required."}
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
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
