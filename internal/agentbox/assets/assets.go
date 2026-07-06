package assets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"agentbox/internal/agentbox/config"
	agenttypes "agentbox/internal/agentbox/types"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

type UploadBytesParams struct {
	TenantID    string
	ThreadID    string
	MessageHint string
	Bytes       []byte
	FileName    string
	MimeType    *string
}

type SignedURLParams struct {
	StorageKey       string
	FileName         string
	MimeType         *string
	ExpiresInSeconds int
}

type PresignedUploadParams struct {
	TenantID         string
	ThreadID         string
	UploadID         string
	FileName         string
	MimeType         *string
	SizeBytes        int64
	ExpiresInSeconds int
}

type AssetStore interface {
	UploadAssetBytes(ctx context.Context, params UploadBytesParams) (agenttypes.NewAsset, error)
	CreatePresignedAssetUploadURL(ctx context.Context, params PresignedUploadParams) (agenttypes.PresignedUpload, error)
	CreateSignedAssetDownloadURL(ctx context.Context, params SignedURLParams) (string, error)
	UploadChatGPTFile(ctx context.Context, tenantID string, threadID string, input ChatGPTFileInput) (agenttypes.NewAsset, error)
}

type ChatGPTFileInput struct {
	DownloadURL string
	FileID      string
	MimeType    *string
	FileName    *string
	RawString   string
}

type R2Store struct {
	cfg        config.Config
	client     *s3.Client
	presigner  *s3.PresignClient
	httpClient *http.Client
}

func NewR2Store(ctx context.Context, cfg config.Config) (*R2Store, error) {
	if cfg.R2AccountID == "" || cfg.R2AccessKeyID == "" || cfg.R2SecretAccessKey == "" {
		return nil, errors.New("R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, and R2_SECRET_ACCESS_KEY are required for asset uploads.")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.R2AccessKeyID,
			cfg.R2SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.R2AccountID))
		o.UsePathStyle = true
	})
	return &R2Store{
		cfg:        cfg,
		client:     client,
		presigner:  s3.NewPresignClient(client),
		httpClient: http.DefaultClient,
	}, nil
}

func (s *R2Store) UploadAssetBytes(ctx context.Context, params UploadBytesParams) (agenttypes.NewAsset, error) {
	if int64(len(params.Bytes)) > s.cfg.MaxFileSizeBytes {
		return agenttypes.NewAsset{}, fmt.Errorf("File is too large. Max size is %d bytes.", s.cfg.MaxFileSizeBytes)
	}
	if s.cfg.R2Bucket == "" {
		return agenttypes.NewAsset{}, errors.New("R2_BUCKET is required for asset uploads.")
	}

	fileName := SanitizeFilename(params.FileName)
	mimeType := InferMimeType(fileName, params.MimeType)
	storageKey := MakeStorageKey(params.TenantID, params.ThreadID, defaultString(params.MessageHint, "message"), fileName)
	contentType := "application/octet-stream"
	if mimeType != nil {
		contentType = *mimeType
	}

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.R2Bucket),
		Key:         aws.String(storageKey),
		Body:        bytes.NewReader(params.Bytes),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return agenttypes.NewAsset{}, err
	}

	return agenttypes.NewAsset{
		StorageKey: storageKey,
		FileName:   fileName,
		MimeType:   mimeType,
		SizeBytes:  int64(len(params.Bytes)),
		PublicURL:  PublicURLForKey(s.cfg.R2PublicBaseURL, storageKey),
	}, nil
}

func (s *R2Store) CreatePresignedAssetUploadURL(ctx context.Context, params PresignedUploadParams) (agenttypes.PresignedUpload, error) {
	if s.cfg.R2Bucket == "" {
		return agenttypes.PresignedUpload{}, errors.New("R2_BUCKET is required for asset uploads.")
	}
	if params.SizeBytes > s.cfg.MaxFileSizeBytes {
		return agenttypes.PresignedUpload{}, fmt.Errorf("File is too large. Max size is %d bytes.", s.cfg.MaxFileSizeBytes)
	}
	expires := params.ExpiresInSeconds
	if expires == 0 {
		expires = 900
	}
	if expires < 60 {
		expires = 60
	}
	if expires > 3600 {
		expires = 3600
	}
	fileName := SanitizeFilename(params.FileName)
	mimeType := InferMimeType(fileName, params.MimeType)
	storageKey := MakeStorageKey(params.TenantID, params.ThreadID, defaultString(params.UploadID, "upload"), fileName)
	contentType := "application/octet-stream"
	if mimeType != nil {
		contentType = *mimeType
	}
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.cfg.R2Bucket),
		Key:         aws.String(storageKey),
		ContentType: aws.String(contentType),
	}
	out, err := s.presigner.PresignPutObject(ctx, input, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(expires) * time.Second
	})
	if err != nil {
		return agenttypes.PresignedUpload{}, err
	}
	return agenttypes.PresignedUpload{
		UploadID:   params.UploadID,
		StorageKey: storageKey,
		FileName:   fileName,
		MimeType:   mimeType,
		SizeBytes:  params.SizeBytes,
		PublicURL:  PublicURLForKey(s.cfg.R2PublicBaseURL, storageKey),
		UploadURL:  out.URL,
		ExpiresIn:  expires,
		RequiredHeaders: map[string]string{
			"content-type": contentType,
		},
	}, nil
}

func (s *R2Store) CreateSignedAssetDownloadURL(ctx context.Context, params SignedURLParams) (string, error) {
	if s.cfg.R2Bucket == "" {
		return "", errors.New("R2_BUCKET is required for asset downloads.")
	}
	fallback := regexp.MustCompile(`[^a-zA-Z0-9._-]+`).ReplaceAllString(params.FileName, "_")
	if fallback == "" {
		fallback = "download.bin"
	}
	input := &s3.GetObjectInput{
		Bucket:                     aws.String(s.cfg.R2Bucket),
		Key:                        aws.String(params.StorageKey),
		ResponseContentDisposition: aws.String(fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fallback, url.PathEscape(params.FileName))),
	}
	if params.MimeType != nil {
		input.ResponseContentType = params.MimeType
	}
	expires := params.ExpiresInSeconds
	if expires == 0 {
		expires = 300
	}
	out, err := s.presigner.PresignGetObject(ctx, input, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(expires) * time.Second
	})
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func (s *R2Store) UploadChatGPTFile(ctx context.Context, tenantID string, threadID string, input ChatGPTFileInput) (agenttypes.NewAsset, error) {
	file, err := NormalizeChatGPTFileInput(input)
	if err != nil {
		return agenttypes.NewAsset{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, file.DownloadURL, nil)
	if err != nil {
		return agenttypes.NewAsset{}, err
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return agenttypes.NewAsset{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return agenttypes.NewAsset{}, fmt.Errorf("Failed to download ChatGPT file: %d %s", response.StatusCode, http.StatusText(response.StatusCode))
	}
	if response.ContentLength > s.cfg.MaxFileSizeBytes {
		return agenttypes.NewAsset{}, fmt.Errorf("File is too large. Max size is %d bytes.", s.cfg.MaxFileSizeBytes)
	}

	bytes, err := io.ReadAll(io.LimitReader(response.Body, s.cfg.MaxFileSizeBytes+1))
	if err != nil {
		return agenttypes.NewAsset{}, err
	}
	if int64(len(bytes)) > s.cfg.MaxFileSizeBytes {
		return agenttypes.NewAsset{}, fmt.Errorf("File is too large. Max size is %d bytes.", s.cfg.MaxFileSizeBytes)
	}

	fileName := file.FileID + ".bin"
	if file.FileName != nil {
		fileName = *file.FileName
	}
	return s.UploadAssetBytes(ctx, UploadBytesParams{
		TenantID:    tenantID,
		ThreadID:    threadID,
		MessageHint: file.FileID,
		Bytes:       bytes,
		FileName:    fileName,
		MimeType:    file.MimeType,
	})
}

func SanitizeFilename(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	sanitized := strings.Trim(re.ReplaceAllString(name, "-"), "-")
	if len(sanitized) > 150 {
		sanitized = sanitized[:150]
	}
	if sanitized == "" {
		return "file.bin"
	}
	return sanitized
}

func InferMimeType(fileName string, fallback *string) *string {
	if fallback != nil {
		return fallback
	}
	value := mime.TypeByExtension(path.Ext(fileName))
	if value == "" {
		return nil
	}
	return &value
}

func MakeStorageKey(tenantID string, threadID string, messageHint string, fileName string) string {
	return strings.Join([]string{
		"agentbox",
		tenantID,
		threadID,
		messageHint,
		uuid.NewString() + "-" + SanitizeFilename(fileName),
	}, "/")
}

func PublicURLForKey(base string, key string) *string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		return nil
	}
	value := base + "/" + key
	return &value
}

func NormalizeChatGPTFileInput(input ChatGPTFileInput) (ChatGPTFileInput, error) {
	if input.RawString == "" {
		if input.DownloadURL == "" || input.FileID == "" {
			return ChatGPTFileInput{}, errors.New("download_url and file_id are required")
		}
		return input, nil
	}
	value := strings.TrimSpace(input.RawString)
	parsed, err := url.Parse(value)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		fileName := path.Base(parsed.Path)
		if fileName == "." || fileName == "/" || fileName == "" {
			fileName = "download.bin"
		}
		return ChatGPTFileInput{
			DownloadURL: value,
			FileID:      "url-" + uuid.NewString(),
			FileName:    &fileName,
		}, nil
	}
	return ChatGPTFileInput{}, errors.New("File was received as a plain string. Pass a ChatGPT uploaded file ID like file_... to the MCP tool so ChatGPT expands it into { download_url, file_id, mime_type?, file_name? }. Local filesystem paths and plain filenames cannot be fetched by the remote Agentbox server.")
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
