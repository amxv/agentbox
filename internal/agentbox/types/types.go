package types

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
	TenantID    string          `json:"tenant_id"`
	TenantSlug  string          `json:"tenant_slug,omitempty"`
	UserID      string          `json:"user_id,omitempty"`
	SubjectType AuthSubjectType `json:"subject_type"`
	ActorName   string          `json:"actor_name"`
	KeyID       string          `json:"key_id,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	Scopes      []string        `json:"scopes,omitempty"`
	Role        string          `json:"role,omitempty"`
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
	TenantID     string  `json:"tenant_id"`
	Email        string  `json:"email"`
	DisplayName  string  `json:"display_name"`
	PasswordHash *string `json:"-"`
	Role         string  `json:"role"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	DisabledAt   *string `json:"disabled_at,omitempty"`
}

type UserSession struct {
	ID         string  `json:"id"`
	TenantID   string  `json:"tenant_id"`
	UserID     string  `json:"user_id"`
	SecretHash string  `json:"-"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
	ExpiresAt  string  `json:"expires_at"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
}

type CLILoginCode struct {
	ID          string  `json:"id"`
	TenantID    string  `json:"tenant_id"`
	UserID      string  `json:"user_id"`
	CodeHash    string  `json:"-"`
	StateHash   string  `json:"-"`
	RedirectURI string  `json:"redirect_uri"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   string  `json:"expires_at"`
	ConsumedAt  *string `json:"consumed_at,omitempty"`
}

type Thread struct {
	ID              string  `json:"id"`
	TenantID        string  `json:"tenant_id,omitempty"`
	Title           string  `json:"title"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	CreatedBy       string  `json:"created_by"`
	CreatedByUserID *string `json:"created_by_user_id,omitempty"`
	CreatedByKeyID  *string `json:"created_by_key_id,omitempty"`
}

type Asset struct {
	ID              string  `json:"id"`
	TenantID        string  `json:"tenant_id,omitempty"`
	MessageID       string  `json:"message_id"`
	StorageKey      string  `json:"storage_key"`
	FileName        string  `json:"file_name"`
	Filename        string  `json:"filename"`
	MimeType        *string `json:"mime_type"`
	SizeBytes       int64   `json:"size_bytes"`
	PublicURL       *string `json:"public_url"`
	DownloadURL     *string `json:"download_url,omitempty"`
	CreatedAt       string  `json:"created_at"`
	CreatedBy       string  `json:"created_by"`
	CreatedByUserID *string `json:"created_by_user_id,omitempty"`
	CreatedByKeyID  *string `json:"created_by_key_id,omitempty"`
}

type Message struct {
	ID              string  `json:"id"`
	TenantID        string  `json:"tenant_id,omitempty"`
	ThreadID        string  `json:"thread_id"`
	Author          string  `json:"author"`
	Body            string  `json:"body"`
	BodyContentType *string `json:"body_content_type"`
	CreatedAt       string  `json:"created_at"`
	Assets          []Asset `json:"assets"`
	CreatedByUserID *string `json:"created_by_user_id,omitempty"`
	CreatedByKeyID  *string `json:"created_by_key_id,omitempty"`
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
	TenantID   string
	StorageKey string
	FileName   string
	MimeType   *string
	SizeBytes  int64
	PublicURL  *string
}

type PendingUpload struct {
	ID              string  `json:"id"`
	TenantID        string  `json:"tenant_id,omitempty"`
	ThreadID        string  `json:"thread_id"`
	StorageKey      string  `json:"storage_key"`
	FileName        string  `json:"file_name"`
	MimeType        *string `json:"mime_type"`
	SizeBytes       int64   `json:"size_bytes"`
	PublicURL       *string `json:"public_url"`
	CreatedAt       string  `json:"created_at"`
	ExpiresAt       string  `json:"expires_at"`
	CreatedBy       string  `json:"created_by"`
	CreatedByUserID *string `json:"created_by_user_id,omitempty"`
	CreatedByKeyID  *string `json:"created_by_key_id,omitempty"`
	ConsumedAt      *string `json:"consumed_at,omitempty"`
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
	PublicURL       *string           `json:"public_url"`
	UploadURL       string            `json:"upload_url"`
	ExpiresIn       int               `json:"expires_in"`
	RequiredHeaders map[string]string `json:"required_headers"`
}

type UploadedAssetReference struct {
	UploadID string `json:"upload_id"`
}

type SearchThreadResult struct {
	ID                 string   `json:"id"`
	TenantID           string   `json:"tenant_id,omitempty"`
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
	TenantID    string   `json:"tenant_id,omitempty"`
	UserID      *string  `json:"user_id,omitempty"`
	Name        string   `json:"name"`
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
