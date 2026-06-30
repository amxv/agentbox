package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

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
	CreateThread(ctx context.Context, title string, author string) (types.Thread, error)
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

func (s *Service) CreateThread(ctx context.Context, actor types.Actor, title string) (types.Thread, error) {
	if err := validate.CreateThreadTitle(title); err != nil {
		return types.Thread{}, err
	}
	return s.repo.CreateThread(ctx, title, actor.Name)
}

func (s *Service) GetThread(ctx context.Context, threadID string) (*types.ThreadWithMessages, error) {
	thread, err := s.repo.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, ErrThreadNotFound
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
