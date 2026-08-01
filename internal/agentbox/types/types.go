package types

import "errors"

var ErrOwnerAlreadyExists = errors.New("deployment owner already exists")
var ErrOwnerSetupTokenInvalid = errors.New("owner setup token is invalid or expired")
var ErrSignupInvitationInvalid = errors.New("signup invitation is invalid or expired")
var ErrEmailAlreadyRegistered = errors.New("email is already registered")
var ErrUserNotFound = errors.New("user not found")
var ErrOwnerCannotBeDisabled = errors.New("deployment owner cannot be disabled")
var ErrThreadNotFound = errors.New("Thread not found.")
var ErrTeamNotFound = errors.New("team not found")
var ErrTeamSlugConflict = errors.New("team slug is already in use")

const DefaultTenantID = "ten_default"

type Actor struct {
	Name    string `json:"name"`
	KeyName string `json:"keyName"`
}

type AuthSubjectType string

const (
	AuthSubjectUserSession AuthSubjectType = "user_session"
	AuthSubjectAPIKey      AuthSubjectType = "api_key"
	AuthSubjectAdmin       AuthSubjectType = "admin"
)

type AuthContext struct {
	TenantID        string          `json:"-"`
	TenantSlug      string          `json:"-"`
	UserID          string          `json:"user_id,omitempty"`
	UserDisplayName string          `json:"user_display_name,omitempty"`
	SubjectType     AuthSubjectType `json:"subject_type"`
	ActorID         string          `json:"actor_id,omitempty"`
	ActorName       string          `json:"actor_name"`
	KeyID           string          `json:"key_id,omitempty"`
	SessionID       string          `json:"session_id,omitempty"`
	Scopes          []string        `json:"scopes,omitempty"`
	Role            string          `json:"role,omitempty"`
	IsOwner         bool            `json:"is_owner,omitempty"`
}

type Tenant struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type User struct {
	ID           string  `json:"id"`
	TenantID     string  `json:"-"`
	Email        string  `json:"email"`
	DisplayName  string  `json:"display_name"`
	PasswordHash *string `json:"-"`
	Role         string  `json:"role"`
	IsOwner      bool    `json:"is_owner"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	DisabledAt   *string `json:"disabled_at,omitempty"`
}

type UserSession struct {
	ID         string  `json:"id"`
	UserID     string  `json:"user_id"`
	SecretHash string  `json:"-"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
	ExpiresAt  string  `json:"expires_at"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
}

type CLILoginCode struct {
	ID          string  `json:"id"`
	UserID      string  `json:"user_id"`
	CodeHash    string  `json:"-"`
	StateHash   string  `json:"-"`
	RedirectURI string  `json:"redirect_uri"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   string  `json:"expires_at"`
	ConsumedAt  *string `json:"consumed_at,omitempty"`
}

type OwnerSetupToken struct {
	ID         string  `json:"id"`
	Purpose    string  `json:"purpose"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  string  `json:"expires_at"`
	ConsumedAt *string `json:"consumed_at,omitempty"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
}

type SignupInvitation struct {
	ID               string  `json:"id"`
	CreatedByUserID  string  `json:"created_by_user_id"`
	CreatedAt        string  `json:"created_at"`
	ExpiresAt        string  `json:"expires_at"`
	ConsumedAt       *string `json:"consumed_at,omitempty"`
	ConsumedByUserID *string `json:"consumed_by_user_id,omitempty"`
	RevokedAt        *string `json:"revoked_at,omitempty"`
	Teams            []Team  `json:"teams"`
}

type Team struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type TeamMembership struct {
	TeamID    string `json:"team_id"`
	UserID    string `json:"user_id"`
	CreatedAt string `json:"created_at"`
}

type TeamWithMembers struct {
	Team
	Members []User `json:"members"`
}

type Thread struct {
	ID                       string  `json:"id"`
	TenantID                 string  `json:"-"`
	OwnerUserID              string  `json:"owner_user_id"`
	Title                    string  `json:"title"`
	CreatedAt                string  `json:"created_at"`
	UpdatedAt                string  `json:"updated_at"`
	CreatedBy                string  `json:"created_by"`
	CreatedByUserID          *string `json:"created_by_user_id,omitempty"`
	CreatedByKeyID           *string `json:"created_by_key_id,omitempty"`
	CreatedByUserDisplayName *string `json:"created_by_user_display_name,omitempty"`
	CreatedByActorName       *string `json:"created_by_actor_name,omitempty"`
}

type ThreadAccess struct {
	ThreadID    string `json:"thread_id"`
	OwnerUserID string `json:"owner_user_id"`
	UserID      string `json:"user_id"`
	IsOwner     bool   `json:"is_owner"`
}

type Asset struct {
	ID                       string  `json:"id"`
	TenantID                 string  `json:"-"`
	MessageID                string  `json:"message_id"`
	StorageKey               string  `json:"storage_key"`
	FileName                 string  `json:"file_name"`
	Filename                 string  `json:"filename"`
	MimeType                 *string `json:"mime_type"`
	SizeBytes                int64   `json:"size_bytes"`
	PublicURL                *string `json:"public_url,omitempty"`
	DownloadURL              *string `json:"download_url,omitempty"`
	CreatedAt                string  `json:"created_at"`
	CreatedBy                string  `json:"created_by"`
	CreatedByUserID          *string `json:"created_by_user_id,omitempty"`
	CreatedByKeyID           *string `json:"created_by_key_id,omitempty"`
	CreatedByUserDisplayName *string `json:"created_by_user_display_name,omitempty"`
	CreatedByActorName       *string `json:"created_by_actor_name,omitempty"`
}

type Message struct {
	ID                       string  `json:"id"`
	TenantID                 string  `json:"-"`
	ThreadID                 string  `json:"thread_id"`
	Author                   string  `json:"author"`
	Body                     string  `json:"body"`
	BodyContentType          *string `json:"body_content_type"`
	CreatedAt                string  `json:"created_at"`
	Assets                   []Asset `json:"assets"`
	CreatedByUserID          *string `json:"created_by_user_id,omitempty"`
	CreatedByKeyID           *string `json:"created_by_key_id,omitempty"`
	CreatedByUserDisplayName *string `json:"created_by_user_display_name,omitempty"`
	CreatedByActorName       *string `json:"created_by_actor_name,omitempty"`
}

type ThreadWithMessages struct {
	Thread
	Messages []Message `json:"messages"`
}

type ChatGPTFileReference struct {
	DownloadURL string  `json:"download_url"`
	FileID      string  `json:"file_id"`
	MimeType    *string `json:"mime_type,omitempty"`
	FileName    *string `json:"file_name,omitempty"`
}

type NewAsset struct {
	StorageKey string
	FileName   string
	MimeType   *string
	SizeBytes  int64
	PublicURL  *string
}

type PendingUpload struct {
	ID                       string  `json:"id"`
	TenantID                 string  `json:"-"`
	ThreadID                 string  `json:"thread_id"`
	StorageKey               string  `json:"storage_key"`
	FileName                 string  `json:"file_name"`
	MimeType                 *string `json:"mime_type"`
	SizeBytes                int64   `json:"size_bytes"`
	PublicURL                *string `json:"public_url,omitempty"`
	CreatedAt                string  `json:"created_at"`
	ExpiresAt                string  `json:"expires_at"`
	CreatedBy                string  `json:"created_by"`
	CreatedByUserID          *string `json:"created_by_user_id,omitempty"`
	CreatedByKeyID           *string `json:"created_by_key_id,omitempty"`
	CreatedByUserDisplayName *string `json:"created_by_user_display_name,omitempty"`
	CreatedByActorName       *string `json:"created_by_actor_name,omitempty"`
	ConsumedAt               *string `json:"consumed_at,omitempty"`
}

type UploadIntentFile struct {
	FileName  string  `json:"file_name"`
	MimeType  *string `json:"mime_type"`
	SizeBytes int64   `json:"size_bytes"`
}

type PresignedUpload struct {
	UploadID        string            `json:"upload_id"`
	StorageKey      string            `json:"storage_key"`
	FileName        string            `json:"file_name"`
	MimeType        *string           `json:"mime_type"`
	SizeBytes       int64             `json:"size_bytes"`
	PublicURL       *string           `json:"public_url,omitempty"`
	UploadURL       string            `json:"upload_url"`
	ExpiresIn       int               `json:"expires_in"`
	RequiredHeaders map[string]string `json:"required_headers"`
}

type UploadedAssetReference struct {
	UploadID string `json:"upload_id"`
}

type SearchThreadResult struct {
	ID                 string   `json:"id"`
	TenantID           string   `json:"-"`
	OwnerUserID        string   `json:"owner_user_id"`
	Title              string   `json:"title"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
	CreatedBy          string   `json:"created_by"`
	MessageCount       int      `json:"message_count"`
	LastMessagePreview string   `json:"last_message_preview"`
	MatchedSnippets    []string `json:"matched_snippets"`
}

type SearchThreadParams struct {
	Query        string
	Limit        int
	CreatedBy    *string
	UpdatedAfter *string
}

type APIKey struct {
	ID          string   `json:"id,omitempty"`
	UserID      string   `json:"user_id"`
	Name        string   `json:"name"`
	Purpose     string   `json:"purpose"`
	Key         string   `json:"-"`
	KeyMasked   string   `json:"key_masked"`
	TokenPrefix string   `json:"token_prefix,omitempty"`
	TokenHash   string   `json:"-"`
	Scopes      []string `json:"scopes,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	LastUsedAt  *string  `json:"last_used_at,omitempty"`
	RevokedAt   *string  `json:"revoked_at,omitempty"`
}
