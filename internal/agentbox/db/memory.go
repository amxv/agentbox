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
	Users    []types.User
	Sessions []types.UserSession
}

func (m *MemoryRepository) EnsureSchema(context.Context) error {
	return nil
}

func (m *MemoryRepository) ListThreads(_ context.Context, tenantID string, limit int) ([]types.Thread, error) {
	threads := []types.Thread{}
	for _, thread := range m.Threads {
		if tenantOf(thread.TenantID) == tenantOf(tenantID) {
			threads = append(threads, thread)
		}
	}
	sort.Slice(threads, func(i, j int) bool {
		return threads[i].UpdatedAt > threads[j].UpdatedAt
	})
	if limit < len(threads) {
		threads = threads[:limit]
	}
	return threads, nil
}

func (m *MemoryRepository) SearchThreads(_ context.Context, tenantID string, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
	query := strings.ToLower(strings.TrimSpace(params.Query))
	results := []types.SearchThreadResult{}
	threads := append([]types.Thread(nil), m.Threads...)
	sort.Slice(threads, func(i, j int) bool {
		return threads[i].UpdatedAt > threads[j].UpdatedAt
	})
	for _, thread := range threads {
		if tenantOf(thread.TenantID) != tenantOf(tenantID) {
			continue
		}
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
			if tenantOf(message.TenantID) != tenantOf(tenantID) || message.ThreadID != thread.ID {
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
			TenantID:           firstNonEmptyString(thread.TenantID, types.DefaultTenantID),
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

func (m *MemoryRepository) CreateThread(_ context.Context, tenantID string, title string, auth types.AuthContext) (types.Thread, error) {
	now := isoMillis(time.Now())
	thread := types.Thread{
		ID:              "thr_" + uuid.NewString(),
		TenantID:        tenantOf(tenantID),
		Title:           title,
		CreatedAt:       now,
		UpdatedAt:       now,
		CreatedBy:       auth.ActorName,
		CreatedByUserID: optionalString(auth.UserID),
		CreatedByKeyID:  optionalString(auth.KeyID),
	}
	m.Threads = append(m.Threads, thread)
	return thread, nil
}

func (m *MemoryRepository) CreateThreadWithMessage(_ context.Context, tenantID string, title string, auth types.AuthContext, body string, bodyContentType *string) (types.Thread, types.Message, error) {
	now := isoMillis(time.Now())
	thread := types.Thread{
		ID:              "thr_" + uuid.NewString(),
		TenantID:        tenantOf(tenantID),
		Title:           title,
		CreatedAt:       now,
		UpdatedAt:       now,
		CreatedBy:       auth.ActorName,
		CreatedByUserID: optionalString(auth.UserID),
		CreatedByKeyID:  optionalString(auth.KeyID),
	}
	message := types.Message{
		ID:              "msg_" + uuid.NewString(),
		TenantID:        thread.TenantID,
		ThreadID:        thread.ID,
		Author:          auth.ActorName,
		Body:            body,
		BodyContentType: bodyContentType,
		CreatedAt:       now,
		Assets:          []types.Asset{},
		CreatedByUserID: optionalString(auth.UserID),
		CreatedByKeyID:  optionalString(auth.KeyID),
	}
	m.Threads = append(m.Threads, thread)
	m.Messages = append(m.Messages, message)
	return thread, message, nil
}

func (m *MemoryRepository) GetThread(_ context.Context, tenantID string, threadID string) (*types.ThreadWithMessages, error) {
	for _, thread := range m.Threads {
		if tenantOf(thread.TenantID) != tenantOf(tenantID) || thread.ID != threadID {
			continue
		}
		messages := []types.Message{}
		for _, message := range m.Messages {
			if tenantOf(message.TenantID) != tenantOf(tenantID) || message.ThreadID != threadID {
				continue
			}
			assets := []types.Asset{}
			for _, asset := range m.Assets {
				if tenantOf(asset.TenantID) == tenantOf(tenantID) && asset.MessageID == message.ID {
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

func (m *MemoryRepository) GetAsset(_ context.Context, tenantID string, assetID string) (*types.Asset, error) {
	for _, asset := range m.Assets {
		if tenantOf(asset.TenantID) == tenantOf(tenantID) && asset.ID == assetID {
			return &asset, nil
		}
	}
	return nil, nil
}

func (m *MemoryRepository) CreatePendingUpload(_ context.Context, upload types.PendingUpload) (types.PendingUpload, error) {
	now := isoMillis(time.Now())
	if upload.TenantID == "" {
		upload.TenantID = types.DefaultTenantID
	}
	upload.CreatedAt = now
	if upload.ExpiresAt == "" {
		upload.ExpiresAt = isoMillis(time.Now().Add(15 * time.Minute))
	}
	m.Pending = append(m.Pending, upload)
	return upload, nil
}

func (m *MemoryRepository) GetPendingUploads(_ context.Context, tenantID string, threadID string, uploadIDs []string, owner types.AuthContext) ([]types.PendingUpload, error) {
	wanted := map[string]bool{}
	for _, id := range uploadIDs {
		wanted[id] = true
	}
	uploads := []types.PendingUpload{}
	for _, upload := range m.Pending {
		if tenantOf(upload.TenantID) == tenantOf(tenantID) && upload.ThreadID == threadID && pendingUploadOwnedBy(upload, owner) && wanted[upload.ID] {
			uploads = append(uploads, upload)
		}
	}
	return uploads, nil
}

func (m *MemoryRepository) MarkPendingUploadsConsumed(_ context.Context, tenantID string, threadID string, uploadIDs []string, owner types.AuthContext) error {
	wanted := map[string]bool{}
	for _, id := range uploadIDs {
		wanted[id] = true
	}
	now := isoMillis(time.Now())
	for i := range m.Pending {
		if tenantOf(m.Pending[i].TenantID) == tenantOf(tenantID) && m.Pending[i].ThreadID == threadID && pendingUploadOwnedBy(m.Pending[i], owner) && wanted[m.Pending[i].ID] {
			m.Pending[i].ConsumedAt = &now
		}
	}
	return nil
}

func (m *MemoryRepository) PostMessage(_ context.Context, tenantID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, newAssets []types.NewAsset) (types.Message, error) {
	var threadIndex = -1
	for i, thread := range m.Threads {
		if tenantOf(thread.TenantID) == tenantOf(tenantID) && thread.ID == threadID {
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
		TenantID:        firstNonEmptyString(m.Threads[threadIndex].TenantID, types.DefaultTenantID),
		ThreadID:        threadID,
		Author:          auth.ActorName,
		Body:            body,
		BodyContentType: bodyContentType,
		CreatedAt:       now,
		Assets:          []types.Asset{},
		CreatedByUserID: optionalString(auth.UserID),
		CreatedByKeyID:  optionalString(auth.KeyID),
	}
	m.Messages = append(m.Messages, message)
	m.Threads[threadIndex].UpdatedAt = isoMillis(time.Now())

	for _, asset := range newAssets {
		createdAsset := types.Asset{
			ID:              "asset_" + uuid.NewString(),
			TenantID:        firstNonEmptyString(asset.TenantID, message.TenantID),
			MessageID:       message.ID,
			StorageKey:      asset.StorageKey,
			FileName:        asset.FileName,
			Filename:        asset.FileName,
			MimeType:        asset.MimeType,
			SizeBytes:       asset.SizeBytes,
			PublicURL:       asset.PublicURL,
			DownloadURL:     asset.PublicURL,
			CreatedAt:       now,
			CreatedBy:       auth.ActorName,
			CreatedByUserID: optionalString(auth.UserID),
			CreatedByKeyID:  optionalString(auth.KeyID),
		}
		m.Assets = append(m.Assets, createdAsset)
		message.Assets = append(message.Assets, createdAsset)
	}
	return message, nil
}

func (m *MemoryRepository) CreateAPIKey(_ context.Context, tenantID string, name string, key string, tokenHash string, tokenPrefix string) (types.APIKey, error) {
	now := isoMillis(time.Now())
	created := types.APIKey{
		ID:          "key_" + uuid.NewString(),
		TenantID:    tenantOf(tenantID),
		Name:        name,
		Key:         key,
		KeyMasked:   maskSecret(key),
		TokenPrefix: tokenPrefix,
		TokenHash:   tokenHash,
		Scopes:      []string{"threads:read", "threads:write", "assets:read", "assets:write", "mcp:use"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	for i := range m.APIKeys {
		if tenantOf(m.APIKeys[i].TenantID) == tenantOf(tenantID) && strings.EqualFold(m.APIKeys[i].Name, name) && m.APIKeys[i].RevokedAt == nil {
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

func (m *MemoryRepository) ListAPIKeys(_ context.Context, tenantID string) ([]types.APIKey, error) {
	keys := []types.APIKey{}
	for _, key := range m.APIKeys {
		if tenantOf(key.TenantID) == tenantOf(tenantID) && key.RevokedAt == nil {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Name < keys[j].Name
	})
	return keys, nil
}

func (m *MemoryRepository) RevokeAPIKey(_ context.Context, tenantID string, name string) (bool, error) {
	now := isoMillis(time.Now())
	for i, key := range m.APIKeys {
		if tenantOf(key.TenantID) == tenantOf(tenantID) && strings.EqualFold(key.Name, name) && key.RevokedAt == nil {
			m.APIKeys[i].RevokedAt = &now
			m.APIKeys[i].UpdatedAt = now
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryRepository) FindAPIKeyBySecret(_ context.Context, secret string) (*types.APIKey, error) {
	for _, key := range m.APIKeys {
		if key.RevokedAt == nil && (key.Key == secret || (key.TokenHash != "" && key.TokenHash == hashSecret(secret))) {
			found := key
			return &found, nil
		}
	}
	return nil, nil
}

func (m *MemoryRepository) MarkAPIKeyUsed(_ context.Context, keyID string) error {
	now := isoMillis(time.Now())
	for i := range m.APIKeys {
		if m.APIKeys[i].ID == keyID && m.APIKeys[i].RevokedAt == nil {
			m.APIKeys[i].LastUsedAt = &now
			return nil
		}
	}
	return nil
}

func (m *MemoryRepository) FindUserByEmail(_ context.Context, tenantID string, email string) (*types.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var found *types.User
	for _, user := range m.Users {
		if user.DisabledAt != nil || strings.ToLower(strings.TrimSpace(user.Email)) != email {
			continue
		}
		if strings.TrimSpace(tenantID) != "" && tenantOf(user.TenantID) != tenantOf(tenantID) {
			continue
		}
		if found != nil {
			return nil, errors.New("Multiple users match that email. Specify a tenant.")
		}
		copy := user
		found = &copy
	}
	return found, nil
}

func (m *MemoryRepository) CreateUserSession(_ context.Context, session types.UserSession) (types.UserSession, error) {
	now := isoMillis(time.Now())
	if session.ID == "" {
		session.ID = "sess_" + uuid.NewString()
	}
	if session.TenantID == "" {
		session.TenantID = types.DefaultTenantID
	}
	session.CreatedAt = now
	if session.ExpiresAt == "" {
		session.ExpiresAt = isoMillis(time.Now().Add(30 * 24 * time.Hour))
	}
	m.Sessions = append(m.Sessions, session)
	return session, nil
}

func (m *MemoryRepository) FindUserSessionBySecretHash(_ context.Context, secretHash string) (*types.UserSession, *types.User, error) {
	now := time.Now().UTC()
	for _, session := range m.Sessions {
		if session.SecretHash != secretHash || session.RevokedAt != nil {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339, session.ExpiresAt)
		if err == nil && now.After(expiresAt) {
			continue
		}
		for _, user := range m.Users {
			if tenantOf(user.TenantID) == tenantOf(session.TenantID) && user.ID == session.UserID && user.DisabledAt == nil {
				sessionCopy := session
				userCopy := user
				return &sessionCopy, &userCopy, nil
			}
		}
	}
	return nil, nil, nil
}

func (m *MemoryRepository) MarkUserSessionUsed(_ context.Context, sessionID string) error {
	now := isoMillis(time.Now())
	for i := range m.Sessions {
		if m.Sessions[i].ID == sessionID && m.Sessions[i].RevokedAt == nil {
			m.Sessions[i].LastUsedAt = &now
			return nil
		}
	}
	return nil
}

func (m *MemoryRepository) RevokeUserSession(_ context.Context, sessionID string) error {
	now := isoMillis(time.Now())
	for i := range m.Sessions {
		if m.Sessions[i].ID == sessionID && m.Sessions[i].RevokedAt == nil {
			m.Sessions[i].RevokedAt = &now
			return nil
		}
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func tenantOf(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return types.DefaultTenantID
	}
	return value
}

func pendingUploadOwnedBy(upload types.PendingUpload, owner types.AuthContext) bool {
	if upload.CreatedBy != owner.ActorName {
		return false
	}
	if owner.UserID != "" && (upload.CreatedByUserID == nil || *upload.CreatedByUserID != owner.UserID) {
		return false
	}
	if owner.KeyID != "" && (upload.CreatedByKeyID == nil || *upload.CreatedByKeyID != owner.KeyID) {
		return false
	}
	return true
}
