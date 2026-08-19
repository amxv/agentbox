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
	"agentbox/internal/agentbox/types"
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
	GetMessage(ctx context.Context, userID string, messageID string) (*types.Message, error)
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
	const extensionPath = "apps/raycast"
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
			"npm ci",
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
