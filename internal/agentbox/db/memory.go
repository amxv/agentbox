package db

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
)

type MemoryRepository struct {
	Threads  []types.Thread
	Messages []types.Message
	Assets   []types.Asset
	Pending  []types.PendingUpload
	APIKeys  []types.APIKey
}

func (m *MemoryRepository) EnsureSchema(context.Context) error {
	return nil
}

func (m *MemoryRepository) ListThreads(_ context.Context, limit int) ([]types.Thread, error) {
	threads := append([]types.Thread(nil), m.Threads...)
	sort.Slice(threads, func(i, j int) bool {
		return threads[i].UpdatedAt > threads[j].UpdatedAt
	})
	if limit < len(threads) {
		threads = threads[:limit]
	}
	return threads, nil
}

func (m *MemoryRepository) SearchThreads(_ context.Context, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
	query := strings.ToLower(strings.TrimSpace(params.Query))
	results := []types.SearchThreadResult{}
	threads := append([]types.Thread(nil), m.Threads...)
	sort.Slice(threads, func(i, j int) bool {
		return threads[i].UpdatedAt > threads[j].UpdatedAt
	})
	for _, thread := range threads {
		if params.CreatedBy != nil && *params.CreatedBy != "" && thread.CreatedBy != *params.CreatedBy {
			continue
		}
		if params.UpdatedAfter != nil && *params.UpdatedAfter != "" && thread.UpdatedAt <= *params.UpdatedAfter {
			continue
		}
		messageCount := 0
		lastBody := ""
		lastAt := ""
		matchedBody := ""
		titleMatches := strings.Contains(strings.ToLower(thread.Title), query)
		for _, message := range m.Messages {
			if message.ThreadID != thread.ID {
				continue
			}
			messageCount++
			if message.CreatedAt >= lastAt {
				lastBody = message.Body
				lastAt = message.CreatedAt
			}
			if matchedBody == "" && strings.Contains(strings.ToLower(message.Body), query) {
				matchedBody = message.Body
			}
		}
		if !titleMatches && matchedBody == "" {
			continue
		}
		results = append(results, types.SearchThreadResult{
			ID:                 thread.ID,
			Title:              thread.Title,
			CreatedAt:          thread.CreatedAt,
			UpdatedAt:          thread.UpdatedAt,
			CreatedBy:          thread.CreatedBy,
			MessageCount:       messageCount,
			LastMessagePreview: previewText(lastBody, 180),
			MatchedSnippets:    matchedSnippets(params.Query, thread.Title, matchedBody),
		})
		if len(results) >= params.Limit {
			break
		}
	}
	return results, nil
}

func (m *MemoryRepository) CreateThread(_ context.Context, title string, author string) (types.Thread, error) {
	now := isoMillis(time.Now())
	thread := types.Thread{
		ID:        "thr_" + uuid.NewString(),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: author,
	}
	m.Threads = append(m.Threads, thread)
	return thread, nil
}

func (m *MemoryRepository) CreateThreadWithMessage(_ context.Context, title string, author string, body string, bodyContentType *string) (types.Thread, types.Message, error) {
	now := isoMillis(time.Now())
	thread := types.Thread{
		ID:        "thr_" + uuid.NewString(),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: author,
	}
	message := types.Message{
		ID:              "msg_" + uuid.NewString(),
		ThreadID:        thread.ID,
		Author:          author,
		Body:            body,
		BodyContentType: bodyContentType,
		CreatedAt:       now,
		Assets:          []types.Asset{},
	}
	m.Threads = append(m.Threads, thread)
	m.Messages = append(m.Messages, message)
	return thread, message, nil
}

func (m *MemoryRepository) GetThread(_ context.Context, threadID string) (*types.ThreadWithMessages, error) {
	for _, thread := range m.Threads {
		if thread.ID != threadID {
			continue
		}
		messages := []types.Message{}
		for _, message := range m.Messages {
			if message.ThreadID != threadID {
				continue
			}
			assets := []types.Asset{}
			for _, asset := range m.Assets {
				if asset.MessageID == message.ID {
					assets = append(assets, asset)
				}
			}
			message.Assets = assets
			messages = append(messages, message)
		}
		sort.Slice(messages, func(i, j int) bool {
			return messages[i].CreatedAt < messages[j].CreatedAt
		})
		return &types.ThreadWithMessages{Thread: thread, Messages: messages}, nil
	}
	return nil, nil
}

func (m *MemoryRepository) GetAsset(_ context.Context, assetID string) (*types.Asset, error) {
	for _, asset := range m.Assets {
		if asset.ID == assetID {
			return &asset, nil
		}
	}
	return nil, nil
}

func (m *MemoryRepository) CreatePendingUpload(_ context.Context, upload types.PendingUpload) (types.PendingUpload, error) {
	now := isoMillis(time.Now())
	upload.CreatedAt = now
	if upload.ExpiresAt == "" {
		upload.ExpiresAt = isoMillis(time.Now().Add(15 * time.Minute))
	}
	m.Pending = append(m.Pending, upload)
	return upload, nil
}

func (m *MemoryRepository) GetPendingUploads(_ context.Context, threadID string, uploadIDs []string, author string) ([]types.PendingUpload, error) {
	wanted := map[string]bool{}
	for _, id := range uploadIDs {
		wanted[id] = true
	}
	uploads := []types.PendingUpload{}
	for _, upload := range m.Pending {
		if upload.ThreadID == threadID && upload.CreatedBy == author && wanted[upload.ID] {
			uploads = append(uploads, upload)
		}
	}
	return uploads, nil
}

func (m *MemoryRepository) MarkPendingUploadsConsumed(_ context.Context, uploadIDs []string) error {
	wanted := map[string]bool{}
	for _, id := range uploadIDs {
		wanted[id] = true
	}
	now := isoMillis(time.Now())
	for i := range m.Pending {
		if wanted[m.Pending[i].ID] {
			m.Pending[i].ConsumedAt = &now
		}
	}
	return nil
}

func (m *MemoryRepository) PostMessage(_ context.Context, threadID string, author string, body string, bodyContentType *string, newAssets []types.NewAsset) (types.Message, error) {
	var threadIndex = -1
	for i, thread := range m.Threads {
		if thread.ID == threadID {
			threadIndex = i
			break
		}
	}
	if threadIndex < 0 {
		return types.Message{}, errors.New("Thread not found.")
	}

	now := isoMillis(time.Now())
	message := types.Message{
		ID:              "msg_" + uuid.NewString(),
		ThreadID:        threadID,
		Author:          author,
		Body:            body,
		BodyContentType: bodyContentType,
		CreatedAt:       now,
		Assets:          []types.Asset{},
	}
	m.Messages = append(m.Messages, message)
	m.Threads[threadIndex].UpdatedAt = isoMillis(time.Now())

	for _, asset := range newAssets {
		createdAsset := types.Asset{
			ID:          "asset_" + uuid.NewString(),
			MessageID:   message.ID,
			StorageKey:  asset.StorageKey,
			FileName:    asset.FileName,
			Filename:    asset.FileName,
			MimeType:    asset.MimeType,
			SizeBytes:   asset.SizeBytes,
			PublicURL:   asset.PublicURL,
			DownloadURL: asset.PublicURL,
			CreatedAt:   now,
			CreatedBy:   author,
		}
		m.Assets = append(m.Assets, createdAsset)
		message.Assets = append(message.Assets, createdAsset)
	}
	return message, nil
}

func (m *MemoryRepository) CreateAPIKey(_ context.Context, name string, key string) (types.APIKey, error) {
	now := isoMillis(time.Now())
	created := types.APIKey{
		Name:      name,
		Key:       key,
		KeyMasked: maskSecret(key),
		CreatedAt: now,
		UpdatedAt: now,
	}
	for i := range m.APIKeys {
		if m.APIKeys[i].Name == name {
			created.CreatedAt = m.APIKeys[i].CreatedAt
			m.APIKeys[i] = created
			return created, nil
		}
	}
	m.APIKeys = append(m.APIKeys, created)
	sort.Slice(m.APIKeys, func(i, j int) bool {
		return m.APIKeys[i].Name < m.APIKeys[j].Name
	})
	return created, nil
}

func (m *MemoryRepository) ListAPIKeys(context.Context) ([]types.APIKey, error) {
	keys := append([]types.APIKey(nil), m.APIKeys...)
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Name < keys[j].Name
	})
	return keys, nil
}

func (m *MemoryRepository) RevokeAPIKey(_ context.Context, name string) (bool, error) {
	for i, key := range m.APIKeys {
		if key.Name == name {
			m.APIKeys = append(m.APIKeys[:i], m.APIKeys[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryRepository) FindAPIKeyBySecret(_ context.Context, secret string) (*types.APIKey, error) {
	for _, key := range m.APIKeys {
		if key.Key == secret {
			found := key
			return &found, nil
		}
	}
	return nil, nil
}
