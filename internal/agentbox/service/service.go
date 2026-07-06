package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
	"github.com/google/uuid"
)

var ErrThreadNotFound = errors.New("Thread not found.")
var ErrAPIKeyNotFound = errors.New("API key not found.")

type Repository interface {
	EnsureSchema(ctx context.Context) error
	ListThreads(ctx context.Context, tenantID string, limit int) ([]types.Thread, error)
	SearchThreads(ctx context.Context, tenantID string, params types.SearchThreadParams) ([]types.SearchThreadResult, error)
	CreateThread(ctx context.Context, tenantID string, title string, auth types.AuthContext) (types.Thread, error)
	CreateThreadWithMessage(ctx context.Context, tenantID string, title string, auth types.AuthContext, body string, bodyContentType *string) (types.Thread, types.Message, error)
	GetThread(ctx context.Context, tenantID string, threadID string) (*types.ThreadWithMessages, error)
	GetAsset(ctx context.Context, tenantID string, assetID string) (*types.Asset, error)
	CreatePendingUpload(ctx context.Context, upload types.PendingUpload) (types.PendingUpload, error)
	GetPendingUploads(ctx context.Context, tenantID string, threadID string, uploadIDs []string, author string) ([]types.PendingUpload, error)
	MarkPendingUploadsConsumed(ctx context.Context, tenantID string, uploadIDs []string) error
	PostMessage(ctx context.Context, tenantID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, assets []types.NewAsset) (types.Message, error)
	CreateAPIKey(ctx context.Context, tenantID string, name string, key string, tokenHash string, tokenPrefix string) (types.APIKey, error)
	ListAPIKeys(ctx context.Context, tenantID string) ([]types.APIKey, error)
	RevokeAPIKey(ctx context.Context, tenantID string, name string) (bool, error)
	FindAPIKeyBySecret(ctx context.Context, key string) (*types.APIKey, error)
	MarkAPIKeyUsed(ctx context.Context, keyID string) error
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
		asset, err := s.assets.UploadChatGPTFile(ctx, params.ThreadID, *params.File)
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
		if err := s.repo.MarkPendingUploadsConsumed(ctx, auth.TenantID, ids); err != nil {
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
	pending, err := s.repo.GetPendingUploads(ctx, auth.TenantID, threadID, ids, auth.ActorName)
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
	return s.repo.CreateAPIKey(ctx, auth.TenantID, name, secret, hashSecret(secret), tokenPrefix(secret))
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
	return &types.AuthContext{
		TenantID:    tenantID,
		UserID:      stringValue(key.UserID),
		SubjectType: types.AuthSubjectAPIKey,
		ActorName:   key.Name,
		KeyID:       key.ID,
		Scopes:      append([]string(nil), key.Scopes...),
	}, nil
}

func generateSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "agb_" + base64.RawURLEncoding.EncodeToString(bytes), nil
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
