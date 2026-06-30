package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
)

var ErrThreadNotFound = errors.New("Thread not found.")
var ErrAPIKeyNotFound = errors.New("API key not found.")

type Repository interface {
	EnsureSchema(ctx context.Context) error
	ListThreads(ctx context.Context, limit int) ([]types.Thread, error)
	SearchThreads(ctx context.Context, params types.SearchThreadParams) ([]types.SearchThreadResult, error)
	CreateThread(ctx context.Context, title string, author string) (types.Thread, error)
	CreateThreadWithMessage(ctx context.Context, title string, author string, body string, bodyContentType *string) (types.Thread, types.Message, error)
	GetThread(ctx context.Context, threadID string) (*types.ThreadWithMessages, error)
	GetAsset(ctx context.Context, assetID string) (*types.Asset, error)
	PostMessage(ctx context.Context, threadID string, author string, body string, bodyContentType *string, asset *types.NewAsset) (types.Message, error)
	CreateAPIKey(ctx context.Context, name string, key string) (types.APIKey, error)
	ListAPIKeys(ctx context.Context) ([]types.APIKey, error)
	RevokeAPIKey(ctx context.Context, name string) (bool, error)
	FindAPIKeyBySecret(ctx context.Context, key string) (*types.APIKey, error)
}

type Service struct {
	repo   Repository
	assets assets.AssetStore
}

func New(repo Repository, assetStore assets.AssetStore) *Service {
	return &Service{repo: repo, assets: assetStore}
}

func (s *Service) ListThreads(ctx context.Context, limit int) ([]types.Thread, error) {
	if limit == 0 {
		limit = 50
	}
	return s.repo.ListThreads(ctx, limit)
}

func (s *Service) SearchThreads(ctx context.Context, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
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
	return s.repo.SearchThreads(ctx, params)
}

func (s *Service) CreateThread(ctx context.Context, actor types.Actor, title string) (types.Thread, error) {
	if err := validate.CreateThreadTitle(title); err != nil {
		return types.Thread{}, err
	}
	return s.repo.CreateThread(ctx, title, actor.Name)
}

func (s *Service) CreateThreadWithMessage(ctx context.Context, actor types.Actor, title string, body string, bodyContentType *string) (types.Thread, types.Message, error) {
	if err := validate.CreateThreadTitle(title); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	resolvedContentType, err := messageformat.Resolve(bodyContentType, body, "")
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	return s.repo.CreateThreadWithMessage(ctx, title, actor.Name, body, &resolvedContentType)
}

func (s *Service) GetThread(ctx context.Context, threadID string) (*types.ThreadWithMessages, error) {
	thread, err := s.repo.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	return thread, nil
}

func (s *Service) GetAsset(ctx context.Context, assetID string) (*types.Asset, error) {
	return s.repo.GetAsset(ctx, assetID)
}

func (s *Service) PostMessage(ctx context.Context, actor types.Actor, params PostMessageParams) (types.Message, error) {
	if err := validate.PostMessage(params.ThreadID); err != nil {
		return types.Message{}, err
	}
	thread, err := s.repo.GetThread(ctx, params.ThreadID)
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
	var newAsset *types.NewAsset
	if params.File != nil {
		asset, err := s.assets.UploadChatGPTFile(ctx, params.ThreadID, *params.File)
		if err != nil {
			return types.Message{}, err
		}
		newAsset = &asset
	}
	return s.repo.PostMessage(ctx, params.ThreadID, actor.Name, params.Body, &bodyContentType, newAsset)
}

func (s *Service) PostMessageWithAsset(ctx context.Context, actor types.Actor, params PostMessageWithAssetParams) (types.Message, error) {
	if err := validate.PostMessage(params.ThreadID); err != nil {
		return types.Message{}, err
	}
	thread, err := s.repo.GetThread(ctx, params.ThreadID)
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
	var newAsset *types.NewAsset
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
		newAsset = &asset
	}
	return s.repo.PostMessage(ctx, params.ThreadID, actor.Name, params.Body, &bodyContentType, newAsset)
}

func (s *Service) SignedAssetDownloadURL(ctx context.Context, asset types.Asset, expiresInSeconds int) (string, error) {
	return s.assets.CreateSignedAssetDownloadURL(ctx, assets.SignedURLParams{
		StorageKey:       asset.StorageKey,
		FileName:         asset.FileName,
		MimeType:         asset.MimeType,
		ExpiresInSeconds: validate.ClampSignedURLExpiry(expiresInSeconds),
	})
}

func (s *Service) CreateAPIKey(ctx context.Context, name string) (types.APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return types.APIKey{}, errors.New("API key name is required.")
	}
	secret, err := generateSecret()
	if err != nil {
		return types.APIKey{}, err
	}
	return s.repo.CreateAPIKey(ctx, name, secret)
}

func (s *Service) ListAPIKeys(ctx context.Context) ([]types.APIKey, error) {
	return s.repo.ListAPIKeys(ctx)
}

func (s *Service) RevokeAPIKey(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("API key name is required.")
	}
	removed, err := s.repo.RevokeAPIKey(ctx, name)
	if err != nil {
		return err
	}
	if !removed {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (s *Service) AuthenticateAPIKey(ctx context.Context, secret string) (*types.Actor, error) {
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
	return &types.Actor{Name: key.Name, KeyName: key.Name}, nil
}

func generateSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

type PostMessageParams struct {
	ThreadID        string
	Body            string
	BodyContentType *string
	File            *assets.ChatGPTFileInput
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
