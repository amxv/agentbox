package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/backup"
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
	GetThreadVisibility(ctx context.Context, userID string, threadID string) (*types.ThreadVisibility, error)
	ManageThreadVisibility(ctx context.Context, userID string, threadID string, input types.ManageThreadVisibilityInput) (types.ManagedThreadVisibility, error)
	AcquirePublicThreadLease(ctx context.Context, tokenHash string) (types.PublicThreadAuthorizationLease, error)
	AcquirePublicAssetSigningLease(ctx context.Context, tokenHash string, assetID string) (types.AssetAuthorizationLease, error)
	AcquireAssetSigningLease(ctx context.Context, userID string, assetID string) (types.AssetAuthorizationLease, error)
	ListThreadsPage(ctx context.Context, userID string, params types.ThreadListParams) (types.ThreadPage, error)
	SearchThreadsPage(ctx context.Context, userID string, params types.SearchThreadParams) (types.SearchThreadPage, error)
	CreateThread(ctx context.Context, userID string, title string, auth types.AuthContext) (types.Thread, error)
	CreateThreadWithMessage(ctx context.Context, userID string, title string, auth types.AuthContext, body string, bodyContentType *string) (types.Thread, types.Message, error)
	GetThread(ctx context.Context, userID string, threadID string) (*types.ThreadWithMessages, error)
	ListOwnerContentThreads(ctx context.Context, ownerUserID string, params types.OwnerContentListParams) ([]types.OwnerContentThreadSummary, error)
	ListOwnerContentThreadsPage(ctx context.Context, ownerUserID string, params types.OwnerContentListParams) (types.OwnerContentThreadPage, error)
	SearchOwnerContentThreads(ctx context.Context, ownerUserID string, params types.OwnerContentSearchParams) ([]types.OwnerContentThreadSummary, error)
	SearchOwnerContentThreadsPage(ctx context.Context, ownerUserID string, params types.OwnerContentSearchParams) (types.OwnerContentThreadPage, error)
	GetOwnerContentThread(ctx context.Context, ownerUserID string, threadID string) (*types.OwnerContentThreadDetail, error)
	GetOwnerContentAsset(ctx context.Context, assetID string) (*types.Asset, error)
	GetAsset(ctx context.Context, userID string, assetID string) (*types.Asset, error)
	AcquireAttachmentPurgeLease(ctx context.Context, uploaderUserID string) (types.AttachmentPurgeLease, error)
	ListAssetPurgeCandidates(ctx context.Context, uploaderUserID string, limit int) ([]types.AssetPurgeCandidate, error)
	MarkAssetPurged(ctx context.Context, assetID string, ownerUserID string) (bool, error)
	MarkAssetPurgeFailure(ctx context.Context, assetID string, message string) error
	CountUnpurgedAssetsByUploader(ctx context.Context, uploaderUserID string) (int, error)
	CreatePendingUpload(ctx context.Context, userID string, upload types.PendingUpload) (types.PendingUpload, error)
	CreatePendingUploads(ctx context.Context, userID string, uploads []types.PendingUpload) ([]types.PendingUpload, error)
	GetPendingUploads(ctx context.Context, userID string, threadID string, uploadIDs []string, actor types.AuthContext) ([]types.PendingUpload, error)
	MarkPendingUploadsConsumed(ctx context.Context, userID string, threadID string, uploadIDs []string, actor types.AuthContext) error
	ClaimPendingUploadsForFinalization(ctx context.Context, userID string, threadID string, actor types.AuthContext, token string, targets []types.UploadFinalizationTarget) ([]types.PendingUpload, error)
	ReleasePendingUploadsFinalization(ctx context.Context, userID string, threadID string, actor types.AuthContext, token string, uploadIDs []string, rejectReason string) error
	PostMessage(ctx context.Context, userID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, assets []types.NewAsset) (types.Message, error)
	PostMessageWithFinalizedUploads(ctx context.Context, userID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, assets []types.NewAsset, finalizedUploads []types.NewAsset, pendingUploadIDs []string, token string) (types.Message, error)
	ListUploadCleanupCandidates(ctx context.Context, limit int) ([]types.UploadCleanupCandidate, error)
	MarkUploadCleanupSuccess(ctx context.Context, cleanupID string) error
	MarkUploadCleanupFailure(ctx context.Context, cleanupID string, message string) error
	CreateAPIKey(ctx context.Context, userID string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string) (types.APIKey, error)
	CreateRaycastAPIKey(ctx context.Context, userID string, name string, tokenHash string, tokenPrefix string, scopes []string, setupBaseURL string) (types.APIKey, error)
	CreateOnboardingCredential(ctx context.Context, userID string, connector string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string, setupBaseURL string, rotate bool) (types.APIKey, types.OnboardingState, error)
	GetOnboardingState(ctx context.Context, userID string) (types.OnboardingState, error)
	DismissOnboarding(ctx context.Context, userID string) (types.OnboardingState, error)
	ListAPIKeys(ctx context.Context, userID string) ([]types.APIKey, error)
	ListAPIKeysPage(ctx context.Context, userID string, page types.PageRequest) (types.APIKeyPage, error)
	ListAllAPIKeys(ctx context.Context) ([]types.APIKey, error)
	ListAllAPIKeysPage(ctx context.Context, page types.PageRequest) (types.APIKeyPage, error)
	RevokeAPIKey(ctx context.Context, userID string, name string) (bool, error)
	RevokeAPIKeyForUserByID(ctx context.Context, userID string, keyID string) (bool, error)
	RevokeAPIKeyByID(ctx context.Context, keyID string) (bool, error)
	RotateAPIKeyForUserByID(ctx context.Context, userID string, keyID string, tokenHash string, tokenPrefix string, setupBaseURL string) (*types.APIKey, string, error)
	GetAPIKeySetup(ctx context.Context, userID string, keyID string, setupBaseURL string) (*types.APIKey, string, error)
	FindAPIKeyBySecret(ctx context.Context, key string) (*types.APIKey, *types.User, error)
	MarkAPIKeyUsed(ctx context.Context, keyID string) error
	BootstrapOwner(ctx context.Context, email string, displayName string, passwordHash string) (types.User, error)
	CreateUser(ctx context.Context, email string, displayName string, passwordHash *string) (types.User, error)
	FindUserByEmail(ctx context.Context, email string) (*types.User, error)
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
	ListSignupInvitationsPage(ctx context.Context, page types.PageRequest) (types.SignupInvitationPage, error)
	RevokeSignupInvitation(ctx context.Context, invitationID string) (bool, error)
	FindSignupInvitation(ctx context.Context, tokenHash string) (*types.SignupInvitation, error)
	RegisterWithSignupInvitation(ctx context.Context, tokenHash string, email string, displayName string, passwordHash string, sessionSecretHash string, sessionExpiresAt time.Time) (types.User, types.UserSession, types.SignupInvitation, error)
	ListUsers(ctx context.Context) ([]types.User, error)
	ListUsersPage(ctx context.Context, page types.PageRequest) (types.UserPage, error)
	GetUserByID(ctx context.Context, userID string) (*types.User, error)
	SetUserDisabled(ctx context.Context, userID string, disabled bool) (types.User, error)
	CreateTeam(ctx context.Context, slug string, name string) (types.Team, error)
	RenameTeam(ctx context.Context, teamID string, name string) (types.Team, error)
	ListTeams(ctx context.Context) ([]types.Team, error)
	ListTeamsPage(ctx context.Context, page types.PageRequest, memberLimit int) (types.TeamPage, error)
	ListUserTeams(ctx context.Context, userID string) ([]types.Team, error)
	ListUserTeamsPage(ctx context.Context, userID string, page types.PageRequest) (types.UserTeamPage, error)
	ListTeamMembers(ctx context.Context, teamID string) ([]types.User, error)
	ListTeamMembersPage(ctx context.Context, teamID string, page types.PageRequest) (types.TeamMemberPage, error)
	AddTeamMember(ctx context.Context, teamID string, userID string) (types.TeamMembership, error)
	RemoveTeamMember(ctx context.Context, teamID string, userID string) (bool, error)
}

var teamSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var sha256HexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Service struct {
	repo   Repository
	assets assets.AssetStore
}

// OwnerWebContext is intentionally opaque outside this package. It can be
// created only from a permanent-owner browser session and is required by every
// deployment-wide content method.
type OwnerWebContext struct {
	userID    string
	sessionID string
}

func New(repo Repository, assetStore assets.AssetStore) *Service {
	return &Service{repo: repo, assets: assetStore}
}

func (s *Service) ResolveOwnerWebContext(authContext types.AuthContext) (OwnerWebContext, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return OwnerWebContext{}, err
	}
	return OwnerWebContext{userID: authContext.UserID, sessionID: authContext.SessionID}, nil
}

func requireOwnerWebContext(ownerContext OwnerWebContext) error {
	if strings.TrimSpace(ownerContext.userID) == "" || strings.TrimSpace(ownerContext.sessionID) == "" {
		return CodedError{Code: "OWNER_BROWSER_REQUIRED", Message: "This operation requires the permanent owner's browser session."}
	}
	return nil
}

func (s *Service) ListThreads(ctx context.Context, auth types.AuthContext, limit int) ([]types.Thread, error) {
	return s.ListThreadsFiltered(ctx, auth, types.ThreadListParams{Limit: limit})
}

func (s *Service) ListThreadsFiltered(ctx context.Context, auth types.AuthContext, params types.ThreadListParams) ([]types.Thread, error) {
	page, err := s.ListThreadsPage(ctx, auth, params)
	return page.Threads, err
}

func (s *Service) ListThreadsPage(ctx context.Context, auth types.AuthContext, params types.ThreadListParams) (types.ThreadPage, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return types.ThreadPage{}, err
	}
	if params.Limit == 0 {
		params.Limit = 50
	}
	if params.Limit < 1 {
		params.Limit = 1
	}
	if params.Limit > 200 {
		params.Limit = 200
	}
	if err := normalizeThreadFilter(&params.Filter, &params.TeamRef); err != nil {
		return types.ThreadPage{}, err
	}
	if err := validateThreadPageCursor(params.Cursor); err != nil {
		return types.ThreadPage{}, err
	}
	return s.repo.ListThreadsPage(ctx, auth.UserID, params)
}

func (s *Service) SearchThreads(ctx context.Context, auth types.AuthContext, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
	page, err := s.SearchThreadsPage(ctx, auth, params)
	return page.Threads, err
}

func (s *Service) SearchThreadsPage(ctx context.Context, auth types.AuthContext, params types.SearchThreadParams) (types.SearchThreadPage, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return types.SearchThreadPage{}, err
	}
	params.Query = strings.TrimSpace(params.Query)
	if params.Query == "" {
		return types.SearchThreadPage{}, CodedError{Code: "INVALID_ARGUMENT", Message: "query is required."}
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
			return types.SearchThreadPage{}, CodedError{Code: "INVALID_ARGUMENT", Message: "updated_after must be an RFC3339 timestamp."}
		}
	}
	if err := normalizeThreadFilter(&params.Filter, &params.TeamRef); err != nil {
		return types.SearchThreadPage{}, err
	}
	if err := validateThreadPageCursor(params.Cursor); err != nil {
		return types.SearchThreadPage{}, err
	}
	return s.repo.SearchThreadsPage(ctx, auth.UserID, params)
}

func validateThreadPageCursor(cursor *types.ThreadPageCursor) error {
	if cursor == nil {
		return nil
	}
	if cursor.UpdatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "cursor is invalid."}
	}
	return nil
}

func normalizeThreadFilter(filter *string, teamRef *string) error {
	*filter = strings.ToLower(strings.TrimSpace(*filter))
	*teamRef = strings.TrimSpace(*teamRef)
	if *filter == "" {
		*filter = types.ThreadFilterAll
	}
	switch *filter {
	case types.ThreadFilterAll, types.ThreadFilterPrivate, types.ThreadFilterShared, types.ThreadFilterPublic:
		if *teamRef != "" {
			return CodedError{Code: "INVALID_ARGUMENT", Message: "team is valid only with filter=team."}
		}
	case types.ThreadFilterTeam:
		if *teamRef == "" {
			return CodedError{Code: "INVALID_ARGUMENT", Message: "team is required when filter=team."}
		}
	default:
		return CodedError{Code: "INVALID_ARGUMENT", Message: "filter must be all, private, shared, team, or public."}
	}
	return nil
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

func (s *Service) GetThreadVisibility(ctx context.Context, auth types.AuthContext, threadID string) (*types.ThreadVisibility, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return nil, err
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "thread_id is required."}
	}
	visibility, err := s.repo.GetThreadVisibility(ctx, auth.UserID, threadID)
	if err != nil {
		return nil, err
	}
	if visibility == nil {
		return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	return visibility, nil
}

func (s *Service) ManageThreadVisibility(ctx context.Context, auth types.AuthContext, threadID string, baseURL string, input types.ManageThreadVisibilityInput) (types.ManagedThreadVisibility, error) {
	mutation := len(input.AddTeams) > 0 || len(input.RemoveTeams) > 0 || input.Public != nil || input.RegeneratePublicLink
	if mutation {
		if err := requireScope(auth, "threads:write"); err != nil {
			return types.ManagedThreadVisibility{}, err
		}
	} else if err := requireScope(auth, "threads:read"); err != nil {
		return types.ManagedThreadVisibility{}, err
	}

	threadID = strings.TrimSpace(threadID)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if threadID == "" {
		return types.ManagedThreadVisibility{}, CodedError{Code: "INVALID_ARGUMENT", Message: "thread_id is required."}
	}
	input.AddTeams = uniqueTrimmedStrings(input.AddTeams)
	input.RemoveTeams = uniqueTrimmedStrings(input.RemoveTeams)
	if input.Public != nil && !*input.Public && input.RegeneratePublicLink {
		return types.ManagedThreadVisibility{}, CodedError{Code: "INVALID_ARGUMENT", Message: "regenerate_public_link cannot be combined with public=false."}
	}
	if (input.Public != nil && *input.Public) || input.RegeneratePublicLink {
		token, err := generatePublicToken()
		if err != nil {
			return types.ManagedThreadVisibility{}, err
		}
		input.PublicToken = token
		input.PublicTokenHash = hashSecret(token)
		input.PublicTokenPrefix = tokenPrefix(token)
	}

	state, err := s.repo.ManageThreadVisibility(ctx, auth.UserID, threadID, input)
	if errors.Is(err, types.ErrThreadNotFound) {
		return types.ManagedThreadVisibility{}, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	if errors.Is(err, types.ErrThreadVisibilityTeamUnavailable) {
		return types.ManagedThreadVisibility{}, CodedError{Code: "TEAM_NOT_AVAILABLE", Message: "A thread can be shared only with teams the acting user currently belongs to.", Err: err}
	}
	if errors.Is(err, types.ErrThreadVisibilityConflict) {
		return types.ManagedThreadVisibility{}, CodedError{Code: "INVALID_ARGUMENT", Message: "A team cannot be both added and removed, and a public link cannot be regenerated while unpublishing.", Err: err}
	}
	if errors.Is(err, types.ErrThreadPublicLinkNotFound) {
		return types.ManagedThreadVisibility{}, CodedError{Code: "PUBLIC_LINK_NOT_FOUND", Message: "No active public link exists to regenerate.", Err: err}
	}
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	if state.PublicLink != nil && strings.TrimSpace(state.PublicLink.Token) != "" && baseURL != "" {
		state.PublicURL = publicThreadURL(baseURL, state.PublicLink.Token)
	}
	return state, nil
}

func publicThreadURL(baseURL string, token string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(token) == "" {
		return ""
	}
	return baseURL + "/share/" + url.PathEscape(token)
}

func (s *Service) GetPublicThread(ctx context.Context, token string) (*types.PublicThreadView, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, CodedError{Code: "PUBLIC_LINK_NOT_FOUND", Message: "Shared thread not found."}
	}
	lease, err := s.repo.AcquirePublicThreadLease(ctx, hashSecret(token))
	if err != nil {
		return nil, err
	}
	if lease == nil {
		return nil, CodedError{Code: "PUBLIC_LINK_NOT_FOUND", Message: "Shared thread not found."}
	}
	thread := lease.Thread()
	if err := lease.Close(ctx); err != nil {
		return nil, fmt.Errorf("close public thread authorization snapshot: %w", err)
	}
	view := s.sanitizePublicThread(token, thread)
	return &view, nil
}

func (s *Service) PublicAssetDownloadURL(ctx context.Context, token string, assetID string) (string, error) {
	return s.publicAssetURL(ctx, token, assetID, false)
}

func (s *Service) PublicAssetPreviewURL(ctx context.Context, token string, assetID string) (string, error) {
	return s.publicAssetURL(ctx, token, assetID, true)
}

func (s *Service) publicAssetURL(ctx context.Context, token string, assetID string, inline bool) (string, error) {
	token = strings.TrimSpace(token)
	assetID = strings.TrimSpace(assetID)
	if token == "" || assetID == "" {
		return "", CodedError{Code: "PUBLIC_ASSET_NOT_FOUND", Message: "Public attachment not found."}
	}
	tokenHash := hashSecret(token)
	lease, err := s.repo.AcquirePublicAssetSigningLease(ctx, tokenHash, assetID)
	if err != nil {
		return "", err
	}
	if lease == nil {
		return "", CodedError{Code: "PUBLIC_ASSET_NOT_FOUND", Message: "Public attachment not found."}
	}
	asset := lease.Asset()
	if err := lease.Close(ctx); err != nil {
		return "", fmt.Errorf("close public attachment authorization snapshot: %w", err)
	}
	if asset.PurgedAt != nil {
		return "", CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	if inline && (asset.MimeType == nil || !strings.HasPrefix(strings.ToLower(*asset.MimeType), "image/")) {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "This attachment type does not support inline preview."}
	}
	if err := s.inspectAvailableAsset(ctx, asset); err != nil {
		return "", err
	}

	// Reauthorize immediately before signing so a token revocation or purge that
	// races the external storage inspection cannot turn a stale snapshot into a
	// fresh signed URL. URL generation itself is local and does not wait on R2.
	signingLease, err := s.repo.AcquirePublicAssetSigningLease(ctx, tokenHash, assetID)
	if err != nil {
		return "", err
	}
	if signingLease == nil {
		return "", CodedError{Code: "PUBLIC_ASSET_NOT_FOUND", Message: "Public attachment not found."}
	}
	signingAsset := signingLease.Asset()
	if !sameAssetIdentity(asset, signingAsset) {
		if err := signingLease.Close(ctx); err != nil {
			return "", fmt.Errorf("close changed public attachment signing authorization: %w", err)
		}
		return "", CodedError{Code: "PUBLIC_ASSET_NOT_FOUND", Message: "Public attachment changed before signing."}
	}
	if signingAsset.PurgedAt != nil {
		if err := signingLease.Close(ctx); err != nil {
			return "", fmt.Errorf("close purged public attachment signing authorization: %w", err)
		}
		return "", CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	signedURL, err := s.createSignedAssetURL(ctx, signingAsset, 300, inline)
	if err != nil {
		if closeErr := signingLease.Close(ctx); closeErr != nil {
			return "", fmt.Errorf("sign public attachment: %v; close signing authorization: %w", err, closeErr)
		}
		return "", err
	}
	if err := signingLease.Close(ctx); err != nil {
		return "", fmt.Errorf("close public attachment signing authorization: %w", err)
	}
	return signedURL, nil
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
	cleanupAndReturn := func(cause error) (types.Message, error) {
		return types.Message{}, s.compensateUploadedAssets(ctx, newAssets, cause)
	}

	if len(params.UploadedAssets) == 0 {
		message, err := s.repo.PostMessage(ctx, auth.UserID, params.ThreadID, auth, params.Body, &bodyContentType, newAssets)
		if err != nil {
			return cleanupAndReturn(err)
		}
		return message, nil
	}

	claimed, finalized, pendingUploadIDs, token, err := s.finalizePendingUploads(ctx, auth, params.ThreadID, params.UploadedAssets)
	if err != nil {
		return cleanupAndReturn(err)
	}
	message, err := s.repo.PostMessageWithFinalizedUploads(ctx, auth.UserID, params.ThreadID, auth, params.Body, &bodyContentType, newAssets, finalized, pendingUploadIDs, token)
	if err != nil {
		// Commit failures are deliberately not followed by eager final-object
		// deletion: the commit outcome can be ambiguous. The pre-registered
		// final-candidate cleanup rows delete only keys not referenced by an
		// active asset, so a committed canonical object is never removed.
		_ = s.repo.ReleasePendingUploadsFinalization(ctx, auth.UserID, params.ThreadID, auth, token, pendingUploadIDs, "")
		if errors.Is(err, types.ErrPendingUploadUnavailable) || errors.Is(err, types.ErrThreadNotFound) {
			return cleanupAndReturn(CodedError{Code: "UPLOAD_UNAVAILABLE", Message: "One or more uploads lost authorization or changed before finalization completed.", Err: err})
		}
		return cleanupAndReturn(err)
	}
	for _, upload := range claimed {
		// Delete staging immediately, but retain the due-at-expiry cleanup row.
		// If the still-valid presigned URL is replayed, the final expiry pass
		// deletes the recreated staging object without touching the final key.
		_ = s.assets.DeleteAssetObject(ctx, upload.StorageKey)
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
	message, err := s.repo.PostMessage(ctx, auth.UserID, params.ThreadID, auth, params.Body, &bodyContentType, newAssets)
	if err != nil {
		return types.Message{}, s.compensateUploadedAssets(ctx, newAssets, err)
	}
	return message, nil
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
	_, _ = s.cleanupPendingUploads(ctx, 10)

	validated := make([]types.UploadIntentFile, len(files))
	for index, file := range files {
		file.FileName = strings.TrimSpace(file.FileName)
		file.SHA256 = strings.ToLower(strings.TrimSpace(file.SHA256))
		if file.FileName == "" {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "file_name is required."}
		}
		if file.SizeBytes < 0 {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "size_bytes must be >= 0."}
		}
		if !sha256HexPattern.MatchString(file.SHA256) {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "sha256 must be exactly 64 hexadecimal characters."}
		}
		validated[index] = file
	}

	uploads := make([]types.PresignedUpload, 0, len(validated))
	pending := make([]types.PendingUpload, 0, len(validated))
	for _, file := range validated {
		uploadID := "upl_" + uuid.NewString()
		presigned, err := s.assets.CreatePresignedAssetUploadURL(ctx, assets.PresignedUploadParams{
			UserID:           auth.UserID,
			ThreadID:         threadID,
			UploadID:         uploadID,
			FileName:         file.FileName,
			MimeType:         file.MimeType,
			SizeBytes:        file.SizeBytes,
			SHA256:           file.SHA256,
			ExpiresInSeconds: 900,
		})
		if err != nil {
			return nil, err
		}
		expiresAt := time.Now().UTC().Add(time.Duration(presigned.ExpiresIn) * time.Second).Format(time.RFC3339)
		pending = append(pending, types.PendingUpload{
			ID:                       presigned.UploadID,
			ThreadID:                 threadID,
			StorageKey:               presigned.StorageKey,
			FileName:                 presigned.FileName,
			MimeType:                 presigned.MimeType,
			SizeBytes:                presigned.SizeBytes,
			ExpectedSHA256:           presigned.SHA256,
			Status:                   "pending",
			ExpiresAt:                expiresAt,
			CreatedBy:                auth.ActorName,
			CreatedByUserID:          optionalString(auth.UserID),
			CreatedByKeyID:           optionalString(auth.KeyID),
			CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
			CreatedByActorName:       optionalString(auth.ActorName),
		})
		uploads = append(uploads, presigned)
	}
	if _, err := s.repo.CreatePendingUploads(ctx, auth.UserID, pending); err != nil {
		if errors.Is(err, types.ErrThreadNotFound) {
			return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
		}
		if errors.Is(err, types.ErrPendingUploadQuotaExceeded) {
			return nil, CodedError{Code: "UPLOAD_QUOTA_EXCEEDED", Message: "Too many unconsumed uploads are active. Wait for expiry or finalize existing uploads.", Err: err}
		}
		return nil, err
	}
	return uploads, nil
}

func (s *Service) finalizePendingUploads(ctx context.Context, auth types.AuthContext, threadID string, refs []types.UploadedAssetReference) ([]types.PendingUpload, []types.NewAsset, []string, string, error) {
	ids := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		id := strings.TrimSpace(ref.UploadID)
		if id == "" {
			return nil, nil, nil, "", CodedError{Code: "INVALID_ARGUMENT", Message: "upload_id is required."}
		}
		if seen[id] {
			return nil, nil, nil, "", CodedError{Code: "INVALID_ARGUMENT", Message: "upload_id values must be unique."}
		}
		seen[id] = true
		ids = append(ids, id)
	}
	pending, err := s.repo.GetPendingUploads(ctx, auth.UserID, threadID, ids, auth)
	if err != nil {
		return nil, nil, nil, "", err
	}
	byID := map[string]types.PendingUpload{}
	for _, upload := range pending {
		byID[upload.ID] = upload
	}
	targets := make([]types.UploadFinalizationTarget, 0, len(ids))
	for _, id := range ids {
		upload, ok := byID[id]
		if !ok || upload.ConsumedAt != nil || upload.Status != "pending" || !sha256HexPattern.MatchString(upload.ExpectedSHA256) {
			return nil, nil, nil, "", CodedError{Code: "UPLOAD_UNAVAILABLE", Message: "Upload was not found, is not pending, or has already been used."}
		}
		targets = append(targets, types.UploadFinalizationTarget{
			UploadID:        id,
			FinalStorageKey: assets.MakeFinalStorageKey(auth.UserID, threadID, id, upload.FileName, upload.ExpectedSHA256),
		})
	}
	token := "fin_" + uuid.NewString()
	claimed, err := s.repo.ClaimPendingUploadsForFinalization(ctx, auth.UserID, threadID, auth, token, targets)
	if errors.Is(err, types.ErrPendingUploadFinalizing) {
		return nil, nil, nil, "", CodedError{Code: "UPLOAD_FINALIZING", Message: "One or more uploads are already being finalized.", Err: err}
	}
	if errors.Is(err, types.ErrPendingUploadUnavailable) || errors.Is(err, types.ErrThreadNotFound) {
		return nil, nil, nil, "", CodedError{Code: "UPLOAD_UNAVAILABLE", Message: "One or more uploads expired, changed, or lost authorization before finalization.", Err: err}
	}
	if err != nil {
		return nil, nil, nil, "", err
	}
	copied := []types.NewAsset{}
	fail := func(cause error, reject bool) ([]types.PendingUpload, []types.NewAsset, []string, string, error) {
		for _, asset := range copied {
			_ = s.assets.DeleteAssetObject(ctx, asset.StorageKey)
		}
		reason := ""
		if reject {
			reason = cause.Error()
		}
		_ = s.repo.ReleasePendingUploadsFinalization(ctx, auth.UserID, threadID, auth, token, ids, reason)
		if reject {
			_, _ = s.cleanupPendingUploads(ctx, 10)
		}
		return nil, nil, nil, "", cause
	}
	for _, upload := range claimed {
		metadata, err := s.assets.HeadAssetObject(ctx, upload.StorageKey)
		if errors.Is(err, backup.ErrObjectNotFound) {
			return fail(CodedError{Code: "UPLOAD_NOT_MATERIALIZED", Message: "The staging object does not exist yet. Retry the upload before finalizing.", Err: err}, false)
		}
		if err != nil {
			return fail(fmt.Errorf("inspect staging upload %s: %w", upload.ID, err), false)
		}
		if err := verifyUploadObject(upload, metadata); err != nil {
			return fail(CodedError{Code: "UPLOAD_METADATA_MISMATCH", Message: err.Error(), Err: err}, true)
		}
		if strings.TrimSpace(metadata.ETag) == "" {
			return fail(CodedError{Code: "UPLOAD_METADATA_MISMATCH", Message: "Uploaded staging object has no stable ETag for conditional promotion."}, true)
		}
		finalMetadata, err := s.assets.CopyAssetObject(ctx, upload.StorageKey, upload.FinalStorageKey, metadata.ETag)
		if err != nil {
			return fail(fmt.Errorf("promote staging upload %s: %w", upload.ID, err), false)
		}
		if err := verifyUploadObject(upload, finalMetadata); err != nil {
			return fail(CodedError{Code: "UPLOAD_METADATA_MISMATCH", Message: "Promoted object failed identity verification: " + err.Error(), Err: err}, true)
		}
		if assets.SHA256FromFinalStorageKey(upload.FinalStorageKey) != upload.ExpectedSHA256 {
			return fail(CodedError{Code: "UPLOAD_METADATA_MISMATCH", Message: "Promoted object key does not encode the expected SHA-256 identity."}, true)
		}
		asset := types.NewAsset{
			StorageKey:    upload.FinalStorageKey,
			FileName:      upload.FileName,
			MimeType:      upload.MimeType,
			SizeBytes:     upload.SizeBytes,
			ContentSHA256: upload.ExpectedSHA256,
		}
		copied = append(copied, asset)
	}
	return claimed, copied, ids, token, nil
}

func verifyUploadObject(upload types.PendingUpload, metadata backup.ObjectMetadata) error {
	if metadata.SizeBytes != upload.SizeBytes {
		return fmt.Errorf("uploaded object size %d does not match the signed size %d", metadata.SizeBytes, upload.SizeBytes)
	}
	if upload.MimeType != nil && (metadata.ContentType == nil || normalizeContentType(*metadata.ContentType) != normalizeContentType(*upload.MimeType)) {
		return errors.New("uploaded object content type does not match the signed content type")
	}
	actualSHA256 := strings.ToLower(strings.TrimSpace(metadata.Metadata["agentbox-sha256"]))
	if actualSHA256 != upload.ExpectedSHA256 {
		return errors.New("uploaded object SHA-256 identity does not match the signed checksum")
	}
	if metadata.ChecksumSHA256 != "" {
		expectedBytes, _ := hex.DecodeString(upload.ExpectedSHA256)
		expectedBase64 := base64.StdEncoding.EncodeToString(expectedBytes)
		if strings.TrimSpace(metadata.ChecksumSHA256) != expectedBase64 {
			return errors.New("storage-reported SHA-256 checksum does not match the signed checksum")
		}
	}
	return nil
}

func (s *Service) CleanupPendingUploads(ctx context.Context, limit int) (types.UploadCleanupResult, error) {
	return s.cleanupPendingUploads(ctx, limit)
}

func (s *Service) cleanupPendingUploads(ctx context.Context, limit int) (types.UploadCleanupResult, error) {
	candidates, err := s.repo.ListUploadCleanupCandidates(ctx, limit)
	if err != nil {
		return types.UploadCleanupResult{}, err
	}
	result := types.UploadCleanupResult{Failures: []string{}}
	for _, candidate := range candidates {
		result.Attempted++
		if err := s.assets.DeleteAssetObject(ctx, candidate.StorageKey); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, candidate.ID+": "+err.Error())
			_ = s.repo.MarkUploadCleanupFailure(ctx, candidate.ID, err.Error())
			continue
		}
		if err := s.repo.MarkUploadCleanupSuccess(ctx, candidate.ID); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, candidate.ID+": "+err.Error())
			continue
		}
		result.Cleaned++
	}
	return result, nil
}

func normalizeContentType(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if index := strings.IndexByte(value, ';'); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	return value
}

func (s *Service) compensateUploadedAssets(ctx context.Context, uploaded []types.NewAsset, cause error) error {
	if len(uploaded) == 0 {
		return cause
	}
	cleanupErrors := []string{}
	for _, asset := range uploaded {
		if err := s.assets.DeleteAssetObject(ctx, asset.StorageKey); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("%s: %v", asset.StorageKey, err))
		}
	}
	if len(cleanupErrors) == 0 {
		return cause
	}
	return fmt.Errorf("%w; uploaded-object cleanup also failed: %s", cause, strings.Join(cleanupErrors, "; "))
}

func (s *Service) SignedAssetDownloadURL(ctx context.Context, auth types.AuthContext, assetID string, expiresInSeconds int) (string, error) {
	return s.signedAssetURL(ctx, auth, assetID, expiresInSeconds, false)
}

func (s *Service) SignedAssetPreviewURL(ctx context.Context, auth types.AuthContext, assetID string, expiresInSeconds int) (string, error) {
	return s.signedAssetURL(ctx, auth, assetID, expiresInSeconds, true)
}

func (s *Service) signedAssetURL(ctx context.Context, auth types.AuthContext, assetID string, expiresInSeconds int, inline bool) (string, error) {
	if err := requireScope(auth, "assets:read"); err != nil {
		return "", err
	}
	lease, err := s.repo.AcquireAssetSigningLease(ctx, auth.UserID, assetID)
	if err != nil {
		return "", err
	}
	if lease == nil {
		return "", CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset not found."}
	}
	asset := lease.Asset()
	if err := lease.Close(ctx); err != nil {
		return "", fmt.Errorf("close attachment authorization snapshot: %w", err)
	}
	if asset.PurgedAt != nil {
		return "", CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	if err := s.inspectAvailableAsset(ctx, asset); err != nil {
		return "", err
	}

	signingLease, err := s.repo.AcquireAssetSigningLease(ctx, auth.UserID, assetID)
	if err != nil {
		return "", err
	}
	if signingLease == nil {
		return "", CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset not found."}
	}
	signingAsset := signingLease.Asset()
	if !sameAssetIdentity(asset, signingAsset) {
		if err := signingLease.Close(ctx); err != nil {
			return "", fmt.Errorf("close changed attachment signing authorization: %w", err)
		}
		return "", CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset changed before signing."}
	}
	if signingAsset.PurgedAt != nil {
		if err := signingLease.Close(ctx); err != nil {
			return "", fmt.Errorf("close purged attachment signing authorization: %w", err)
		}
		return "", CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	signedURL, err := s.createSignedAssetURL(ctx, signingAsset, validate.ClampSignedURLExpiry(expiresInSeconds), inline)
	if err != nil {
		if closeErr := signingLease.Close(ctx); closeErr != nil {
			return "", fmt.Errorf("sign attachment: %v; close signing authorization: %w", err, closeErr)
		}
		return "", err
	}
	if err := signingLease.Close(ctx); err != nil {
		return "", fmt.Errorf("close attachment signing authorization: %w", err)
	}
	return signedURL, nil
}

func (s *Service) inspectAvailableAsset(ctx context.Context, asset types.Asset) error {
	metadata, err := s.assets.HeadAssetObject(ctx, asset.StorageKey)
	if errors.Is(err, backup.ErrObjectNotFound) {
		return CodedError{Code: "ATTACHMENT_UNAVAILABLE", Message: "Attachment unavailable because its stored object is missing.", Err: err}
	}
	if err != nil {
		return fmt.Errorf("inspect attachment object: %w", err)
	}
	if metadata.SizeBytes != asset.SizeBytes {
		return CodedError{Code: "ATTACHMENT_UNAVAILABLE", Message: "Attachment unavailable because its stored object does not match the recorded metadata."}
	}
	if expectedSHA256 := assets.SHA256FromFinalStorageKey(asset.StorageKey); expectedSHA256 != "" {
		actualSHA256 := strings.ToLower(strings.TrimSpace(metadata.Metadata["agentbox-sha256"]))
		if actualSHA256 != expectedSHA256 {
			return CodedError{Code: "ATTACHMENT_UNAVAILABLE", Message: "Attachment unavailable because its stored object failed SHA-256 identity verification."}
		}
		if metadata.ChecksumSHA256 != "" {
			expectedBytes, _ := hex.DecodeString(expectedSHA256)
			if strings.TrimSpace(metadata.ChecksumSHA256) != base64.StdEncoding.EncodeToString(expectedBytes) {
				return CodedError{Code: "ATTACHMENT_UNAVAILABLE", Message: "Attachment unavailable because its storage checksum failed identity verification."}
			}
		}
	}
	return nil
}

func (s *Service) createSignedAssetURL(ctx context.Context, asset types.Asset, expiresInSeconds int, inline bool) (string, error) {
	return s.assets.CreateSignedAssetDownloadURL(ctx, assets.SignedURLParams{
		StorageKey:       asset.StorageKey,
		FileName:         asset.FileName,
		MimeType:         asset.MimeType,
		ExpiresInSeconds: expiresInSeconds,
		Inline:           inline,
	})
}

func sameAssetIdentity(left types.Asset, right types.Asset) bool {
	return left.ID == right.ID &&
		left.MessageID == right.MessageID &&
		left.StorageKey == right.StorageKey &&
		left.FileName == right.FileName &&
		left.SizeBytes == right.SizeBytes &&
		sameOptionalText(left.MimeType, right.MimeType)
}

func sameOptionalText(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return strings.TrimSpace(*left) == strings.TrimSpace(*right)
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
	if errors.Is(err, types.ErrCredentialLabelConflict) {
		return types.APIKey{}, CodedError{Code: "CREDENTIAL_LABEL_CONFLICT", Message: "An active credential already uses that label. Choose a distinct label or rotate that exact credential.", Err: err}
	}
	if err != nil {
		return types.APIKey{}, err
	}
	created.Key = secret
	return created, nil
}

func (s *Service) GetOnboardingState(ctx context.Context, auth types.AuthContext) (types.OnboardingState, error) {
	if err := requireBrowserSession(auth); err != nil {
		return types.OnboardingState{}, err
	}
	return s.repo.GetOnboardingState(ctx, auth.UserID)
}

func (s *Service) DismissOnboarding(ctx context.Context, auth types.AuthContext) (types.OnboardingState, error) {
	if err := requireBrowserSession(auth); err != nil {
		return types.OnboardingState{}, err
	}
	return s.repo.DismissOnboarding(ctx, auth.UserID)
}

func (s *Service) CreateOnboardingConnection(ctx context.Context, auth types.AuthContext, connector string, baseURL string, rotate bool) (OnboardingConnectionResult, error) {
	if err := requireBrowserSession(auth); err != nil {
		return OnboardingConnectionResult{}, err
	}
	connector, name, purpose, scopes, err := onboardingCredentialSpec(connector)
	if err != nil {
		return OnboardingConnectionResult{}, err
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return OnboardingConnectionResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "deployment base URL is required."}
	}
	secret, err := generateSecret()
	if err != nil {
		return OnboardingConnectionResult{}, err
	}
	setupBaseURL := ""
	if connector == "raycast" {
		setupBaseURL = baseURL
	}
	credential, state, err := s.repo.CreateOnboardingCredential(ctx, auth.UserID, connector, name, purpose, hashSecret(secret), tokenPrefix(secret), scopes, setupBaseURL, rotate)
	if errors.Is(err, types.ErrOnboardingCredentialExists) {
		return OnboardingConnectionResult{}, CodedError{Code: "ONBOARDING_CREDENTIAL_EXISTS", Message: "This connector already has an active credential. Choose rotate to replace it.", Err: err}
	}
	if errors.Is(err, types.ErrOnboardingCredentialNotFound) {
		return OnboardingConnectionResult{}, CodedError{Code: "ONBOARDING_CREDENTIAL_NOT_FOUND", Message: "This connector does not have an active credential to rotate. Reconnect it instead.", Err: err}
	}
	if errors.Is(err, types.ErrCredentialLabelConflict) {
		return OnboardingConnectionResult{}, CodedError{Code: "CREDENTIAL_LABEL_CONFLICT", Message: "An unrelated active credential already uses this connector label. Rename or revoke that credential before reconnecting onboarding.", Err: err}
	}
	if err != nil {
		return OnboardingConnectionResult{}, err
	}
	credential.Key = secret
	result := OnboardingConnectionResult{Connector: connector, Credential: credential, State: state}
	switch connector {
	case "chatgpt":
		result.MCPURL, err = mcpURLWithSecret(baseURL, secret)
		result.Instructions = []string{
			"Open ChatGPT settings and go to Apps & Connectors, then Advanced settings.",
			"Create a custom remote MCP connector and paste the complete URL below.",
			"Choose no authentication. The credential is already carried in the URL query string.",
			"Save the connector, then ask ChatGPT to list your AgentBox threads as a connection test.",
			"After connector schema changes, refresh or recreate the connector and run Scan Tools when available.",
			"To test attachments, create a ChatGPT file artifact and ask it to attach the file you just created; do not supply a file ID, path, or URL.",
		}
	case "claude":
		result.MCPURL, err = mcpURLWithSecret(baseURL, secret)
		result.Instructions = []string{
			"Open Claude's connector or integrations settings and add a custom remote MCP server.",
			"Paste the complete URL below without adding a separate authorization header.",
			"Name the connector AgentBox and finish the connection flow.",
			"Ask Claude to list your AgentBox threads as a connection test.",
		}
	case "local":
		result.ProfileCommand = localProfileCommand(baseURL, secret, auth.UserID, credential.Name)
		result.SetupPrompt = localAgentSetupPrompt(baseURL, secret, auth.UserID, credential.Name)
		result.Instructions = []string{
			"Copy the generated prompt and paste it into one local coding-agent session on the machine you want to connect.",
			"The agent will install the public npm package, save an active local profile, and run agentbox list.",
			"Use a separate credential later for each additional machine.",
		}
	case "raycast":
		setup := raycastSetupMaterial(baseURL, secret, credential.ID, credential.Name)
		result.RaycastSetup = &setup
		result.Instructions = []string{
			"Clone or update the AgentBox repository on the Mac where Raycast is installed.",
			"Install the extension dependencies and run it in Raycast developer mode with the commands below.",
			"Enter the generated Agentbox URL and Agentbox API Key in the required extension preferences.",
			"Run Browse Threads in Raycast and confirm it shows only the threads currently accessible to your user.",
			"Create a separate Raycast credential for every additional Mac or local Raycast installation.",
		}
	}
	if err != nil {
		return OnboardingConnectionResult{}, err
	}
	return result, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, auth types.AuthContext) ([]types.APIKey, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return nil, err
	}
	return s.repo.ListAPIKeys(ctx, auth.UserID)
}

func (s *Service) ListAPIKeysPage(ctx context.Context, auth types.AuthContext, pageRequest types.PageRequest) (types.APIKeyPage, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return types.APIKeyPage{}, err
	}
	return s.repo.ListAPIKeysPage(ctx, auth.UserID, pageRequest)
}

type RaycastInstallationResult struct {
	Credential types.APIKey               `json:"credential"`
	Setup      types.RaycastSetupMaterial `json:"raycast_setup"`
}

func (s *Service) CreateRaycastInstallation(ctx context.Context, auth types.AuthContext, label string, baseURL string) (RaycastInstallationResult, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return RaycastInstallationResult{}, err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return RaycastInstallationResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "A distinct Raycast installation label is required."}
	}
	if len(label) > 120 {
		return RaycastInstallationResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "Raycast installation labels must be at most 120 characters."}
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return RaycastInstallationResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "deployment base URL is required."}
	}
	secret, err := generateSecret()
	if err != nil {
		return RaycastInstallationResult{}, err
	}
	credential, err := s.repo.CreateRaycastAPIKey(ctx, auth.UserID, label, hashSecret(secret), tokenPrefix(secret), ConnectorAPIKeyScopes("raycast"), baseURL)
	if errors.Is(err, types.ErrCredentialLabelConflict) {
		return RaycastInstallationResult{}, CodedError{Code: "CREDENTIAL_LABEL_CONFLICT", Message: "An active credential already uses that label. Choose a distinct installation label or rotate that exact credential.", Err: err}
	}
	if err != nil {
		return RaycastInstallationResult{}, err
	}
	credential.Key = secret
	return RaycastInstallationResult{
		Credential: credential,
		Setup:      raycastSetupMaterial(baseURL, secret, credential.ID, credential.Name),
	}, nil
}

func (s *Service) RotateAPIKeyByID(ctx context.Context, auth types.AuthContext, keyID string, baseURL string) (types.APIKey, *types.RaycastSetupMaterial, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return types.APIKey{}, nil, err
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return types.APIKey{}, nil, CodedError{Code: "INVALID_ARGUMENT", Message: "credential_id is required."}
	}
	secret, err := generateSecret()
	if err != nil {
		return types.APIKey{}, nil, err
	}
	rotated, setupBaseURL, err := s.repo.RotateAPIKeyForUserByID(ctx, auth.UserID, keyID, hashSecret(secret), tokenPrefix(secret), strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if errors.Is(err, types.ErrRaycastSetupUnavailable) {
		return types.APIKey{}, nil, CodedError{Code: "RAYCAST_SETUP_UNAVAILABLE", Message: "Raycast setup metadata is unavailable for this credential, so its secret was not rotated.", Err: err}
	}
	if err != nil {
		return types.APIKey{}, nil, err
	}
	if rotated == nil {
		return types.APIKey{}, nil, CodedError{Code: "CREDENTIAL_NOT_FOUND", Message: "Credential not found.", Err: ErrAPIKeyNotFound}
	}
	rotated.Key = secret
	var setup *types.RaycastSetupMaterial
	if rotated.Purpose == "raycast" {
		material := raycastSetupMaterial(setupBaseURL, secret, rotated.ID, rotated.Name)
		setup = &material
	}
	return *rotated, setup, nil
}

func (s *Service) RevokeAPIKeyByID(ctx context.Context, auth types.AuthContext, keyID string) error {
	if err := requireUserAuthContext(auth); err != nil {
		return err
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "credential_id is required."}
	}
	revoked, err := s.repo.RevokeAPIKeyForUserByID(ctx, auth.UserID, keyID)
	if err != nil {
		return err
	}
	if !revoked {
		return CodedError{Code: "CREDENTIAL_NOT_FOUND", Message: "Credential not found.", Err: ErrAPIKeyNotFound}
	}
	return nil
}

func (s *Service) RaycastInstallationSetup(ctx context.Context, auth types.AuthContext, keyID string, baseURL string) (types.RaycastSetupMaterial, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return types.RaycastSetupMaterial{}, err
	}
	key, setupBaseURL, err := s.repo.GetAPIKeySetup(ctx, auth.UserID, strings.TrimSpace(keyID), strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil {
		return types.RaycastSetupMaterial{}, err
	}
	if key == nil || key.Purpose != "raycast" {
		return types.RaycastSetupMaterial{}, CodedError{Code: "CREDENTIAL_NOT_FOUND", Message: "Raycast installation credential not found.", Err: ErrAPIKeyNotFound}
	}
	if strings.TrimSpace(setupBaseURL) == "" {
		return types.RaycastSetupMaterial{}, CodedError{Code: "RAYCAST_SETUP_UNAVAILABLE", Message: "Raycast setup metadata is unavailable for this credential."}
	}
	return raycastSetupMaterial(setupBaseURL, "", key.ID, key.Name), nil
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

type OnboardingConnectionResult struct {
	Connector      string                      `json:"connector"`
	Credential     types.APIKey                `json:"credential"`
	State          types.OnboardingState       `json:"state"`
	MCPURL         string                      `json:"mcp_url,omitempty"`
	ProfileCommand string                      `json:"profile_command,omitempty"`
	SetupPrompt    string                      `json:"setup_prompt,omitempty"`
	RaycastSetup   *types.RaycastSetupMaterial `json:"raycast_setup,omitempty"`
	Instructions   []string                    `json:"instructions"`
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
	page, err := s.ListSignupInvitationsPage(ctx, authContext, types.PageRequest{})
	return page.Invitations, err
}

func (s *Service) ListSignupInvitationsPage(ctx context.Context, authContext types.AuthContext, pageRequest types.PageRequest) (types.SignupInvitationPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.SignupInvitationPage{}, err
	}
	return s.repo.ListSignupInvitationsPage(ctx, pageRequest)
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
	return authContextForUserSession(session, user), sessionSecret, user, nil
}

func (s *Service) ListUsers(ctx context.Context, authContext types.AuthContext) ([]types.User, error) {
	page, err := s.ListUsersPage(ctx, authContext, types.PageRequest{})
	return page.Users, err
}

func (s *Service) ListUsersPage(ctx context.Context, authContext types.AuthContext, pageRequest types.PageRequest) (types.UserPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.UserPage{}, err
	}
	return s.repo.ListUsersPage(ctx, pageRequest)
}

func (s *Service) ListOwnerContentThreads(ctx context.Context, ownerContext OwnerWebContext, params types.OwnerContentListParams) ([]types.OwnerContentThreadSummary, error) {
	page, err := s.ListOwnerContentThreadsPage(ctx, ownerContext, params)
	return page.Threads, err
}

func (s *Service) ListOwnerContentThreadsPage(ctx context.Context, ownerContext OwnerWebContext, params types.OwnerContentListParams) (types.OwnerContentThreadPage, error) {
	if err := requireOwnerWebContext(ownerContext); err != nil {
		return types.OwnerContentThreadPage{}, err
	}
	params.UserID = strings.TrimSpace(params.UserID)
	params.TeamRef = strings.TrimSpace(params.TeamRef)
	request := types.NormalizePageRequest(types.PageRequest{Limit: params.Limit, Offset: params.Offset})
	params.Limit, params.Offset = request.Limit, request.Offset
	return s.repo.ListOwnerContentThreadsPage(ctx, ownerContext.userID, params)
}

func (s *Service) SearchOwnerContentThreads(ctx context.Context, ownerContext OwnerWebContext, params types.OwnerContentSearchParams) ([]types.OwnerContentThreadSummary, error) {
	page, err := s.SearchOwnerContentThreadsPage(ctx, ownerContext, params)
	return page.Threads, err
}

func (s *Service) SearchOwnerContentThreadsPage(ctx context.Context, ownerContext OwnerWebContext, params types.OwnerContentSearchParams) (types.OwnerContentThreadPage, error) {
	if err := requireOwnerWebContext(ownerContext); err != nil {
		return types.OwnerContentThreadPage{}, err
	}
	params.Query = strings.TrimSpace(params.Query)
	if params.Query == "" {
		return types.OwnerContentThreadPage{}, CodedError{Code: "INVALID_ARGUMENT", Message: "q is required."}
	}
	params.UserID = strings.TrimSpace(params.UserID)
	params.TeamRef = strings.TrimSpace(params.TeamRef)
	request := types.NormalizePageRequest(types.PageRequest{Limit: params.Limit, Offset: params.Offset})
	params.Limit, params.Offset = request.Limit, request.Offset
	return s.repo.SearchOwnerContentThreadsPage(ctx, ownerContext.userID, params)
}

func (s *Service) GetOwnerContentThread(ctx context.Context, ownerContext OwnerWebContext, threadID string) (*types.OwnerContentThreadDetail, error) {
	if err := requireOwnerWebContext(ownerContext); err != nil {
		return nil, err
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "thread_id is required."}
	}
	thread, err := s.repo.GetOwnerContentThread(ctx, ownerContext.userID, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	return thread, nil
}

func (s *Service) SignedOwnerContentAssetDownloadURL(ctx context.Context, ownerContext OwnerWebContext, assetID string, expiresSeconds int) (string, error) {
	if err := requireOwnerWebContext(ownerContext); err != nil {
		return "", err
	}
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "asset_id is required."}
	}
	asset, err := s.repo.GetOwnerContentAsset(ctx, assetID)
	if err != nil {
		return "", err
	}
	if asset == nil {
		return "", CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset not found."}
	}
	if asset.PurgedAt != nil {
		return "", CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	if err := s.inspectAvailableAsset(ctx, *asset); err != nil {
		return "", err
	}
	return s.createSignedAssetURL(ctx, *asset, validate.ClampSignedURLExpiry(expiresSeconds), false)
}

func (s *Service) ListOwnerAPIKeys(ctx context.Context, authContext types.AuthContext) ([]types.APIKey, error) {
	page, err := s.ListOwnerAPIKeysPage(ctx, authContext, types.PageRequest{})
	return page.Credentials, err
}

func (s *Service) ListOwnerAPIKeysPage(ctx context.Context, authContext types.AuthContext, pageRequest types.PageRequest) (types.APIKeyPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.APIKeyPage{}, err
	}
	return s.repo.ListAllAPIKeysPage(ctx, pageRequest)
}

func (s *Service) RevokeOwnerAPIKey(ctx context.Context, authContext types.AuthContext, keyID string) error {
	if err := requireOwnerBrowser(authContext); err != nil {
		return err
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "credential_id is required."}
	}
	revoked, err := s.repo.RevokeAPIKeyByID(ctx, keyID)
	if err != nil {
		return err
	}
	if !revoked {
		return CodedError{Code: "CREDENTIAL_NOT_FOUND", Message: "Credential not found.", Err: ErrAPIKeyNotFound}
	}
	return nil
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

func (s *Service) PurgeUserAttachments(ctx context.Context, authContext types.AuthContext, userID string, limit int) (types.AttachmentPurgeResult, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.AttachmentPurgeResult{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return types.AttachmentPurgeResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "user_id is required."}
	}
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return types.AttachmentPurgeResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "limit must be between 1 and 100."}
	}
	lease, err := s.repo.AcquireAttachmentPurgeLease(ctx, userID)
	if errors.Is(err, types.ErrUserNotFound) {
		return types.AttachmentPurgeResult{}, CodedError{Code: "USER_NOT_FOUND", Message: "User not found.", Err: err}
	}
	if errors.Is(err, types.ErrOwnerCannotBeDisabled) {
		return types.AttachmentPurgeResult{}, CodedError{Code: "OWNER_IMMUTABLE", Message: "The permanent deployment owner's attachments cannot be purged.", Err: err}
	}
	if errors.Is(err, types.ErrUserMustBeDisabled) {
		return types.AttachmentPurgeResult{}, CodedError{Code: "USER_ACTIVE", Message: "Attachments can be purged only after the user is disabled.", Err: err}
	}
	if err != nil {
		return types.AttachmentPurgeResult{}, err
	}
	defer func() { _ = lease.Close(ctx) }()

	candidates, err := s.repo.ListAssetPurgeCandidates(ctx, userID, limit)
	if err != nil {
		return types.AttachmentPurgeResult{}, err
	}
	result := types.AttachmentPurgeResult{UserID: userID, Failures: []types.AttachmentPurgeFailure{}}
	for _, candidate := range candidates {
		result.Attempted++
		if err := s.assets.DeleteAssetObject(ctx, candidate.StorageKey); err != nil {
			message := boundedPurgeError(err)
			_ = s.repo.MarkAssetPurgeFailure(ctx, candidate.AssetID, message)
			result.Failed++
			result.Failures = append(result.Failures, types.AttachmentPurgeFailure{AssetID: candidate.AssetID, Error: message})
			continue
		}
		marked, err := s.repo.MarkAssetPurged(ctx, candidate.AssetID, authContext.UserID)
		if err != nil || !marked {
			message := "failed to record attachment tombstone"
			if err != nil {
				message = boundedPurgeError(err)
			}
			_ = s.repo.MarkAssetPurgeFailure(ctx, candidate.AssetID, message)
			result.Failed++
			result.Failures = append(result.Failures, types.AttachmentPurgeFailure{AssetID: candidate.AssetID, Error: message})
			continue
		}
		result.Purged++
	}
	result.Remaining, err = s.repo.CountUnpurgedAssetsByUploader(ctx, userID)
	if err != nil {
		return types.AttachmentPurgeResult{}, err
	}
	result.Complete = result.Remaining == 0
	return result, nil
}

func boundedPurgeError(err error) string {
	message := err.Error()
	if len(message) > 500 {
		return message[:500]
	}
	return message
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
	page, err := s.ListOwnerTeamsPage(ctx, authContext, types.PageRequest{})
	return page.Teams, err
}

func (s *Service) ListOwnerTeamsPage(ctx context.Context, authContext types.AuthContext, pageRequest types.PageRequest) (types.TeamPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.TeamPage{}, err
	}
	return s.repo.ListTeamsPage(ctx, pageRequest, 10)
}

func (s *Service) ListOwnerTeamMembersPage(ctx context.Context, authContext types.AuthContext, teamID string, pageRequest types.PageRequest) (types.TeamMemberPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.TeamMemberPage{}, err
	}
	page, err := s.repo.ListTeamMembersPage(ctx, strings.TrimSpace(teamID), pageRequest)
	if errors.Is(err, types.ErrTeamNotFound) {
		return types.TeamMemberPage{}, CodedError{Code: "TEAM_NOT_FOUND", Message: "Team not found.", Err: err}
	}
	return page, err
}

func (s *Service) ListOwnerUserTeamsPage(ctx context.Context, authContext types.AuthContext, userID string, pageRequest types.PageRequest) (types.UserTeamPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.UserTeamPage{}, err
	}
	user, err := s.repo.GetUserByID(ctx, strings.TrimSpace(userID))
	if err != nil {
		return types.UserTeamPage{}, err
	}
	if user == nil {
		return types.UserTeamPage{}, CodedError{Code: "USER_NOT_FOUND", Message: "User not found.", Err: types.ErrUserNotFound}
	}
	return s.repo.ListUserTeamsPage(ctx, user.ID, pageRequest)
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
	if errors.Is(err, types.ErrUserDisabled) {
		return types.TeamMembership{}, CodedError{Code: "USER_DISABLED", Message: "Disabled users cannot be added to teams.", Err: err}
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

func generateSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "agb_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func generatePublicToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "agpub_" + hex.EncodeToString(buffer), nil
}

func (s *Service) sanitizePublicThread(token string, thread types.ThreadWithMessages) types.PublicThreadView {
	view := types.PublicThreadView{
		ID:                       thread.ID,
		Title:                    thread.Title,
		CreatedAt:                thread.CreatedAt,
		UpdatedAt:                thread.UpdatedAt,
		CreatedBy:                thread.CreatedBy,
		CreatedByUserDisplayName: thread.CreatedByUserDisplayName,
		CreatedByActorName:       thread.CreatedByActorName,
		Messages:                 make([]types.PublicMessage, 0, len(thread.Messages)),
	}
	for _, message := range thread.Messages {
		publicMessage := types.PublicMessage{
			ID:                       message.ID,
			Author:                   message.Author,
			Body:                     message.Body,
			BodyContentType:          message.BodyContentType,
			CreatedAt:                message.CreatedAt,
			CreatedByUserDisplayName: message.CreatedByUserDisplayName,
			CreatedByActorName:       message.CreatedByActorName,
			Assets:                   make([]types.PublicAsset, 0, len(message.Assets)),
		}
		for _, asset := range message.Assets {
			publicAsset := types.PublicAsset{
				ID:                       asset.ID,
				FileName:                 asset.FileName,
				MimeType:                 asset.MimeType,
				SizeBytes:                asset.SizeBytes,
				CreatedAt:                asset.CreatedAt,
				CreatedBy:                asset.CreatedBy,
				CreatedByUserDisplayName: asset.CreatedByUserDisplayName,
				CreatedByActorName:       asset.CreatedByActorName,
				PurgedAt:                 asset.PurgedAt,
			}
			if asset.PurgedAt == nil {
				basePath := "/api/public/threads/" + url.PathEscape(token) + "/assets/" + url.PathEscape(asset.ID)
				publicAsset.DownloadPath = basePath + "/download"
				if asset.MimeType != nil && strings.HasPrefix(strings.ToLower(*asset.MimeType), "image/") {
					publicAsset.PreviewPath = basePath + "/preview"
				}
			}
			publicMessage.Assets = append(publicMessage.Assets, publicAsset)
		}
		view.Messages = append(view.Messages, publicMessage)
	}
	return view
}

func defaultAPIKeyScopes() []string {
	return []string{"threads:read", "threads:write", "assets:read", "assets:write", "mcp:use"}
}

func cliAPIKeyScopes() []string {
	return append(defaultAPIKeyScopes(), "keys:read", "keys:write")
}

func ConnectorAPIKeyScopes(purpose string) []string {
	switch strings.ToLower(strings.TrimSpace(purpose)) {
	case "chatgpt", "claude":
		return defaultAPIKeyScopes()
	case "raycast":
		return []string{"threads:read", "threads:write", "assets:read", "assets:write"}
	case "local", "cli":
		return cliAPIKeyScopes()
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
		UserID:          user.ID,
		UserDisplayName: user.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		ActorID:         session.ID,
		ActorName:       "Web dashboard",
		SessionID:       session.ID,
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

func requireBrowserSession(auth types.AuthContext) error {
	if auth.SubjectType != types.AuthSubjectUserSession || strings.TrimSpace(auth.UserID) == "" || strings.TrimSpace(auth.SessionID) == "" {
		return CodedError{Code: "BROWSER_SESSION_REQUIRED", Message: "An authenticated browser session is required."}
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

func onboardingCredentialSpec(value string) (connector string, name string, purpose string, scopes []string, err error) {
	connector = strings.ToLower(strings.TrimSpace(value))
	switch connector {
	case "chatgpt":
		return connector, "ChatGPT", "chatgpt", ConnectorAPIKeyScopes("chatgpt"), nil
	case "claude":
		return connector, "Claude", "claude", ConnectorAPIKeyScopes("claude"), nil
	case "local":
		return connector, "Local CLI", "local", ConnectorAPIKeyScopes("local"), nil
	case "raycast":
		return connector, "Raycast", "raycast", ConnectorAPIKeyScopes("raycast"), nil
	default:
		return "", "", "", nil, CodedError{Code: "INVALID_ONBOARDING_CONNECTOR", Message: "connector must be chatgpt, claude, local, or raycast.", Err: types.ErrInvalidOnboardingConnector}
	}
}

func raycastSetupMaterial(baseURL string, secret string, credentialID string, label string) types.RaycastSetupMaterial {
	const repositoryURL = "https://github.com/amxv/agentbox.git"
	const extensionPath = "raycast/agentbox"
	return types.RaycastSetupMaterial{
		CredentialID:  credentialID,
		Label:         label,
		BaseURL:       baseURL,
		APIKey:        secret,
		RepositoryURL: repositoryURL,
		ExtensionPath: extensionPath,
		InstallCommands: []string{
			"git clone " + repositoryURL,
			"cd agentbox/" + extensionPath,
			"npm install",
			"npm run dev",
		},
		Preferences: []types.RaycastSetupPreference{
			{Name: "baseUrl", Title: "Agentbox URL", Value: baseURL},
			{Name: "apiKey", Title: "Agentbox API Key", Value: secret, Secret: true},
		},
		FinalCheck: "In Raycast, run Agentbox: Browse Threads and confirm it lists only the threads currently accessible to your user.",
	}
}

func mcpURLWithSecret(baseURL string, secret string) (string, error) {
	endpoint, err := url.Parse(strings.TrimRight(baseURL, "/") + "/api/mcp")
	if err != nil {
		return "", err
	}
	query := endpoint.Query()
	query.Set("key", secret)
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func localProfileCommand(baseURL string, secret string, userID string, keyName string) string {
	return strings.Join([]string{
		"agentbox profiles add local",
		"--base-url " + shellQuote(baseURL),
		"--api-key " + shellQuote(secret),
		"--user-id " + shellQuote(userID),
		"--key-name " + shellQuote(keyName),
		"--auth-type api_key",
		"--activate",
	}, " ")
}

func localAgentSetupPrompt(baseURL string, secret string, userID string, keyName string) string {
	profileCommand := localProfileCommand(baseURL, secret, userID, keyName)
	return fmt.Sprintf(`Set up AgentBox on this machine for me.

AgentBox is a shared threaded inbox between me and my AI agents. Use the dedicated credential below only for this machine; do not reuse it for ChatGPT, Claude, or another computer.

Run these commands exactly:

1. Install the public CLI package:
   npm install -g @amxv/agentbox

2. Save and activate my local profile for this deployment:
   %s

3. Verify the connection by listing the threads I can access:
   agentbox list

Deployment: %s
User ID: %s

Do not ask for or use the deployment-owner secret. Do not print the credential again after saving it. Report whether installation, profile creation, and the final thread-list test each succeeded; include the exact error for any failed step.`, profileCommand, baseURL, userID)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
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
