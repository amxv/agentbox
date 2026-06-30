package types

type Actor struct {
	Name    string `json:"name"`
	KeyName string `json:"keyName"`
}

type Thread struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	CreatedBy string `json:"created_by"`
}

type Asset struct {
	ID         string  `json:"id"`
	MessageID  string  `json:"message_id"`
	StorageKey string  `json:"storage_key"`
	FileName   string  `json:"file_name"`
	MimeType   *string `json:"mime_type"`
	SizeBytes  int64   `json:"size_bytes"`
	PublicURL  *string `json:"public_url"`
	CreatedAt  string  `json:"created_at"`
	CreatedBy  string  `json:"created_by"`
}

type Message struct {
	ID              string  `json:"id"`
	ThreadID        string  `json:"thread_id"`
	Author          string  `json:"author"`
	Body            string  `json:"body"`
	BodyContentType *string `json:"body_content_type"`
	CreatedAt       string  `json:"created_at"`
	Assets          []Asset `json:"assets"`
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
