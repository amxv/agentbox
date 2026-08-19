package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
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
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
)

type UploadBytesParams struct {
	UserID      string
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
	Inline           bool
}

type ReadAssetRangeParams struct {
	StorageKey   string
	OffsetBytes  int64
	MaxBytes     int64
	ExpectedETag string
}

type PresignedUploadParams struct {
	UserID           string
	ThreadID         string
	UploadID         string
	FileName         string
	MimeType         *string
	SizeBytes        int64
	SHA256           string
	ExpiresInSeconds int
}

type AssetStore interface {
	UploadAssetBytes(ctx context.Context, params UploadBytesParams) (agenttypes.NewAsset, error)
	CreatePresignedAssetUploadURL(ctx context.Context, params PresignedUploadParams) (agenttypes.PresignedUpload, error)
	CreateSignedAssetDownloadURL(ctx context.Context, params SignedURLParams) (string, error)
	HeadAssetObject(ctx context.Context, storageKey string) (ObjectMetadata, error)
	ReadAssetObjectRange(ctx context.Context, params ReadAssetRangeParams) ([]byte, error)
	CopyAssetObject(ctx context.Context, sourceStorageKey string, destinationStorageKey string, expectedETag string) (ObjectMetadata, error)
	DeleteAssetObject(ctx context.Context, storageKey string) error
	UploadChatGPTFile(ctx context.Context, userID string, threadID string, input ChatGPTFileInput) (agenttypes.NewAsset, error)
}

var ErrObjectChanged = errors.New("asset object changed")

func (s *R2Store) headObject(ctx context.Context, bucket string, key string) (ObjectMetadata, error) {
	if strings.TrimSpace(bucket) == "" {
		return ObjectMetadata{}, errors.New("R2 bucket is required")
	}
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(key),
		ChecksumMode: s3types.ChecksumModeEnabled,
	})
	if err != nil {
		if isObjectNotFound(err) {
			return ObjectMetadata{}, fmt.Errorf("%w: r2://%s/%s", ErrObjectNotFound, bucket, key)
		}
		return ObjectMetadata{}, err
	}
	return ObjectMetadata{
		Bucket:         bucket,
		Key:            key,
		SizeBytes:      aws.ToInt64(output.ContentLength),
		ETag:           normalizeETag(aws.ToString(output.ETag)),
		ContentType:    output.ContentType,
		ChecksumSHA256: aws.ToString(output.ChecksumSHA256),
		Metadata:       cloneMetadata(output.Metadata),
		LastModified:   output.LastModified,
	}, nil
}

func (s *R2Store) copyObject(ctx context.Context, request copyObjectRequest) (ObjectMetadata, error) {
	if strings.TrimSpace(request.SourceBucket) == "" || strings.TrimSpace(request.DestinationBucket) == "" {
		return ObjectMetadata{}, errors.New("source and destination R2 buckets are required")
	}
	input := &s3.CopyObjectInput{
		Bucket:     aws.String(request.DestinationBucket),
		Key:        aws.String(request.DestinationKey),
		CopySource: aws.String(url.PathEscape(request.SourceBucket + "/" + request.SourceKey)),
	}
	if request.ExpectedETag != "" {
		input.CopySourceIfMatch = aws.String(`"` + normalizeETag(request.ExpectedETag) + `"`)
	}
	if _, err := s.client.CopyObject(ctx, input); err != nil {
		return ObjectMetadata{}, err
	}
	return s.headObject(ctx, request.DestinationBucket, request.DestinationKey)
}

type ChatGPTFileInput struct {
	DownloadURL string
	FileID      string
	MimeType    *string
	FileName    *string
}

type R2Store struct {
	cfg         config.Config
	client      *s3.Client
	presigner   *s3.PresignClient
	fileFetcher RemoteFileFetcher
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
		cfg:         cfg,
		client:      client,
		presigner:   s3.NewPresignClient(client),
		fileFetcher: NewSecureRemoteFileFetcher(),
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
	digest := sha256.Sum256(params.Bytes)
	digestHex := hex.EncodeToString(digest[:])
	digestBase64 := base64.StdEncoding.EncodeToString(digest[:])
	storageKey := MakeFinalStorageKey(params.UserID, params.ThreadID, defaultString(params.MessageHint, "message"), fileName, digestHex)
	contentType := "application/octet-stream"
	if mimeType != nil {
		contentType = *mimeType
	}

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:         aws.String(s.cfg.R2Bucket),
		Key:            aws.String(storageKey),
		Body:           bytes.NewReader(params.Bytes),
		ContentType:    aws.String(contentType),
		ContentLength:  aws.Int64(int64(len(params.Bytes))),
		ChecksumSHA256: aws.String(digestBase64),
		Metadata:       map[string]string{"agentbox-sha256": digestHex},
	})
	if err != nil {
		return agenttypes.NewAsset{}, err
	}

	return agenttypes.NewAsset{
		StorageKey:    storageKey,
		FileName:      fileName,
		MimeType:      mimeType,
		SizeBytes:     int64(len(params.Bytes)),
		ContentSHA256: digestHex,
	}, nil
}

func (s *R2Store) CreatePresignedAssetUploadURL(ctx context.Context, params PresignedUploadParams) (agenttypes.PresignedUpload, error) {
	if s.cfg.R2Bucket == "" {
		return agenttypes.PresignedUpload{}, errors.New("R2_BUCKET is required for asset uploads.")
	}
	if params.SizeBytes > s.cfg.MaxFileSizeBytes {
		return agenttypes.PresignedUpload{}, fmt.Errorf("File is too large. Max size is %d bytes.", s.cfg.MaxFileSizeBytes)
	}
	digestHex, digestBase64, err := normalizeSHA256(params.SHA256)
	if err != nil {
		return agenttypes.PresignedUpload{}, err
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
	storageKey := MakeStagingStorageKey(params.UserID, params.ThreadID, defaultString(params.UploadID, "upload"), fileName)
	contentType := "application/octet-stream"
	if mimeType != nil {
		contentType = *mimeType
	}
	input := &s3.PutObjectInput{
		Bucket:         aws.String(s.cfg.R2Bucket),
		Key:            aws.String(storageKey),
		ContentType:    aws.String(contentType),
		ContentLength:  aws.Int64(params.SizeBytes),
		ChecksumSHA256: aws.String(digestBase64),
		Metadata:       map[string]string{"agentbox-sha256": digestHex},
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
		SHA256:     digestHex,
		UploadURL:  out.URL,
		ExpiresIn:  expires,
		RequiredHeaders: map[string]string{
			"content-type":               contentType,
			"x-amz-meta-agentbox-sha256": digestHex,
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
	disposition := "attachment"
	if params.Inline {
		disposition = "inline"
	}
	input := &s3.GetObjectInput{
		Bucket:                     aws.String(s.cfg.R2Bucket),
		Key:                        aws.String(params.StorageKey),
		ResponseContentDisposition: aws.String(fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, fallback, url.PathEscape(params.FileName))),
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

func (s *R2Store) HeadAssetObject(ctx context.Context, storageKey string) (ObjectMetadata, error) {
	if strings.TrimSpace(s.cfg.R2Bucket) == "" {
		return ObjectMetadata{}, errors.New("R2_BUCKET is required for asset inspection.")
	}
	return s.headObject(ctx, s.cfg.R2Bucket, strings.TrimSpace(storageKey))
}

func (s *R2Store) ReadAssetObjectRange(ctx context.Context, params ReadAssetRangeParams) ([]byte, error) {
	if strings.TrimSpace(s.cfg.R2Bucket) == "" {
		return nil, errors.New("R2_BUCKET is required for asset reads.")
	}
	storageKey := strings.TrimSpace(params.StorageKey)
	if storageKey == "" {
		return nil, errors.New("asset storage key is required")
	}
	if params.OffsetBytes < 0 {
		return nil, errors.New("asset read offset must be >= 0")
	}
	if params.MaxBytes <= 0 {
		return nil, errors.New("asset read size must be > 0")
	}
	end := params.OffsetBytes + params.MaxBytes - 1
	if end < params.OffsetBytes {
		return nil, errors.New("asset read range overflow")
	}
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.R2Bucket),
		Key:    aws.String(storageKey),
		Range:  aws.String(fmt.Sprintf("bytes=%d-%d", params.OffsetBytes, end)),
	}
	if expectedETag := normalizeETag(params.ExpectedETag); expectedETag != "" {
		input.IfMatch = aws.String(`"` + expectedETag + `"`)
	}
	output, err := s.client.GetObject(ctx, input)
	if err != nil {
		if isObjectNotFound(err) {
			return nil, fmt.Errorf("%w: r2://%s/%s", ErrObjectNotFound, s.cfg.R2Bucket, storageKey)
		}
		if isObjectChanged(err) {
			return nil, fmt.Errorf("%w: r2://%s/%s", ErrObjectChanged, s.cfg.R2Bucket, storageKey)
		}
		return nil, err
	}
	defer output.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(output.Body, params.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > params.MaxBytes {
		return nil, errors.New("asset range response exceeded requested size")
	}
	return contents, nil
}

func (s *R2Store) CopyAssetObject(ctx context.Context, sourceStorageKey string, destinationStorageKey string, expectedETag string) (ObjectMetadata, error) {
	if strings.TrimSpace(s.cfg.R2Bucket) == "" {
		return ObjectMetadata{}, errors.New("R2_BUCKET is required for asset copies.")
	}
	return s.copyObject(ctx, copyObjectRequest{
		SourceBucket:      s.cfg.R2Bucket,
		SourceKey:         strings.TrimSpace(sourceStorageKey),
		DestinationBucket: s.cfg.R2Bucket,
		DestinationKey:    strings.TrimSpace(destinationStorageKey),
		ExpectedETag:      strings.TrimSpace(expectedETag),
	})
}

func (s *R2Store) DeleteAssetObject(ctx context.Context, storageKey string) error {
	if s.cfg.R2Bucket == "" {
		return errors.New("R2_BUCKET is required for asset deletion.")
	}
	storageKey = strings.TrimSpace(storageKey)
	if storageKey == "" {
		return errors.New("asset storage key is required")
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.cfg.R2Bucket),
		Key:    aws.String(storageKey),
	})
	return err
}

func (s *R2Store) UploadChatGPTFile(ctx context.Context, userID string, threadID string, input ChatGPTFileInput) (agenttypes.NewAsset, error) {
	file, err := NormalizeChatGPTFileInput(input)
	if err != nil {
		return agenttypes.NewAsset{}, err
	}
	fetcher := s.fileFetcher
	if fetcher == nil {
		fetcher = NewSecureRemoteFileFetcher()
	}
	bytes, err := fetcher.Fetch(ctx, file.DownloadURL, s.cfg.MaxFileSizeBytes)
	if err != nil {
		return agenttypes.NewAsset{}, err
	}

	fileName := file.FileID + ".bin"
	if file.FileName != nil {
		fileName = *file.FileName
	}
	return s.UploadAssetBytes(ctx, UploadBytesParams{
		UserID:      userID,
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
	extension := strings.ToLower(path.Ext(fileName))
	switch extension {
	case ".md", ".markdown", ".mdown", ".mkd":
		value := "text/markdown; charset=utf-8"
		return &value
	}
	value := mime.TypeByExtension(extension)
	if value == "" {
		return nil
	}
	return &value
}

func MakeStorageKey(userID string, threadID string, messageHint string, fileName string) string {
	return strings.Join([]string{
		"agentbox",
		userID,
		threadID,
		messageHint,
		uuid.NewString() + "-" + SanitizeFilename(fileName),
	}, "/")
}

func MakeStagingStorageKey(userID string, threadID string, uploadID string, fileName string) string {
	return strings.Join([]string{
		"agentbox",
		"staging",
		userID,
		threadID,
		uploadID,
		uuid.NewString() + "-" + SanitizeFilename(fileName),
	}, "/")
}

func MakeFinalStorageKey(userID string, threadID string, messageHint string, fileName string, digestHex string) string {
	return strings.Join([]string{
		"agentbox",
		"final",
		"sha256",
		strings.ToLower(strings.TrimSpace(digestHex)),
		userID,
		threadID,
		messageHint,
		uuid.NewString() + "-" + SanitizeFilename(fileName),
	}, "/")
}

func SHA256FromFinalStorageKey(storageKey string) string {
	parts := strings.Split(strings.TrimSpace(storageKey), "/")
	if len(parts) < 5 || parts[0] != "agentbox" || parts[1] != "final" || parts[2] != "sha256" {
		return ""
	}
	digestHex, _, err := normalizeSHA256(parts[3])
	if err != nil {
		return ""
	}
	return digestHex
}

func NormalizeChatGPTFileInput(input ChatGPTFileInput) (ChatGPTFileInput, error) {
	input.DownloadURL = strings.TrimSpace(input.DownloadURL)
	input.FileID = strings.TrimSpace(input.FileID)
	if input.DownloadURL == "" || input.FileID == "" {
		return ChatGPTFileInput{}, errors.New("download_url and file_id are required")
	}
	parsed, err := url.Parse(input.DownloadURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return ChatGPTFileInput{}, errors.New("download_url must be an absolute HTTP or HTTPS URL without embedded credentials")
	}
	if input.MimeType != nil {
		value := strings.TrimSpace(*input.MimeType)
		if value == "" {
			input.MimeType = nil
		} else {
			input.MimeType = &value
		}
	}
	if input.FileName != nil {
		value := strings.TrimSpace(*input.FileName)
		if value == "" {
			input.FileName = nil
		} else {
			input.FileName = &value
		}
	}
	return input, nil
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func normalizeSHA256(value string) (string, string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return "", "", errors.New("sha256 must be exactly 64 hexadecimal characters")
	}
	return value, base64.StdEncoding.EncodeToString(decoded), nil
}

func cloneMetadata(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[strings.ToLower(key)] = value
	}
	return result
}

func normalizeETag(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"`)
}

func isObjectNotFound(err error) bool {
	var apiError smithy.APIError
	if !errors.As(err, &apiError) {
		return false
	}
	switch apiError.ErrorCode() {
	case "NoSuchKey", "NotFound", "404":
		return true
	default:
		return false
	}
}

func isObjectChanged(err error) bool {
	var apiError smithy.APIError
	if !errors.As(err, &apiError) {
		return false
	}
	switch apiError.ErrorCode() {
	case "PreconditionFailed", "412", "InvalidRange", "RequestedRangeNotSatisfiable", "416":
		return true
	default:
		return false
	}
}
