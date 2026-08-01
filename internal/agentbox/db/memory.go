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
	Threads           []types.Thread
	Messages          []types.Message
	Assets            []types.Asset
	Pending           []types.PendingUpload
	APIKeys           []types.APIKey
	Tenants           []types.Tenant
	Users             []types.User
	Sessions          []types.UserSession
	CLICodes          []types.CLILoginCode
	OwnerSetupTokens  []memoryOwnerSetupToken
	SignupInvitations []memorySignupInvitation
	Teams             []types.Team
	TeamMemberships   []types.TeamMembership
	Onboarding        []types.OnboardingState
}

type memoryOwnerSetupToken struct {
	Token     types.OwnerSetupToken
	TokenHash string
}

type memorySignupInvitation struct {
	Invitation types.SignupInvitation
	TokenHash  string
	TeamIDs    []string
}

func (m *MemoryRepository) ResolveThreadAccess(_ context.Context, userID string, threadID string) (*types.ThreadAccess, error) {
	for _, thread := range m.Threads {
		if thread.ID == threadID {
			return normalMemoryThreadAccess(thread, userID), nil
		}
	}
	return nil, nil
}

func (m *MemoryRepository) ListThreads(_ context.Context, userID string, limit int) ([]types.Thread, error) {
	threads := []types.Thread{}
	for _, thread := range m.Threads {
		if normalMemoryThreadAccess(thread, userID) != nil {
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

func (m *MemoryRepository) SearchThreads(_ context.Context, userID string, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
	query := strings.ToLower(strings.TrimSpace(params.Query))
	results := []types.SearchThreadResult{}
	threads := append([]types.Thread(nil), m.Threads...)
	sort.Slice(threads, func(i, j int) bool {
		return threads[i].UpdatedAt > threads[j].UpdatedAt
	})
	for _, thread := range threads {
		if normalMemoryThreadAccess(thread, userID) == nil {
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
			TenantID:           firstNonEmptyString(thread.TenantID, types.DefaultTenantID),
			OwnerUserID:        thread.OwnerUserID,
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

func (m *MemoryRepository) CreateThread(_ context.Context, userID string, title string, auth types.AuthContext) (types.Thread, error) {
	now := isoMillis(time.Now())
	thread := types.Thread{
		ID:                       "thr_" + uuid.NewString(),
		TenantID:                 types.DefaultTenantID,
		OwnerUserID:              userID,
		Title:                    title,
		CreatedAt:                now,
		UpdatedAt:                now,
		CreatedBy:                auth.ActorName,
		CreatedByUserID:          optionalString(auth.UserID),
		CreatedByKeyID:           optionalString(auth.KeyID),
		CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
		CreatedByActorName:       optionalString(auth.ActorName),
	}
	m.Threads = append(m.Threads, thread)
	return thread, nil
}

func (m *MemoryRepository) CreateThreadWithMessage(_ context.Context, userID string, title string, auth types.AuthContext, body string, bodyContentType *string) (types.Thread, types.Message, error) {
	now := isoMillis(time.Now())
	thread := types.Thread{
		ID:                       "thr_" + uuid.NewString(),
		TenantID:                 types.DefaultTenantID,
		OwnerUserID:              userID,
		Title:                    title,
		CreatedAt:                now,
		UpdatedAt:                now,
		CreatedBy:                auth.ActorName,
		CreatedByUserID:          optionalString(auth.UserID),
		CreatedByKeyID:           optionalString(auth.KeyID),
		CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
		CreatedByActorName:       optionalString(auth.ActorName),
	}
	message := types.Message{
		ID:                       "msg_" + uuid.NewString(),
		TenantID:                 thread.TenantID,
		ThreadID:                 thread.ID,
		Author:                   auth.ActorName,
		Body:                     body,
		BodyContentType:          bodyContentType,
		CreatedAt:                now,
		Assets:                   []types.Asset{},
		CreatedByUserID:          optionalString(auth.UserID),
		CreatedByKeyID:           optionalString(auth.KeyID),
		CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
		CreatedByActorName:       optionalString(auth.ActorName),
	}
	m.Threads = append(m.Threads, thread)
	m.Messages = append(m.Messages, message)
	return thread, message, nil
}

func (m *MemoryRepository) GetThread(_ context.Context, userID string, threadID string) (*types.ThreadWithMessages, error) {
	for _, thread := range m.Threads {
		if thread.ID != threadID || normalMemoryThreadAccess(thread, userID) == nil {
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
					copy := asset
					copy.PublicURL = nil
					copy.DownloadURL = nil
					assets = append(assets, copy)
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

func (m *MemoryRepository) GetAsset(_ context.Context, userID string, assetID string) (*types.Asset, error) {
	for _, asset := range m.Assets {
		if asset.ID != assetID {
			continue
		}
		for _, message := range m.Messages {
			if message.ID != asset.MessageID {
				continue
			}
			for _, thread := range m.Threads {
				if thread.ID == message.ThreadID && normalMemoryThreadAccess(thread, userID) != nil {
					copy := asset
					copy.PublicURL = nil
					copy.DownloadURL = nil
					return &copy, nil
				}
			}
		}
	}
	return nil, nil
}

func (m *MemoryRepository) CreatePendingUpload(_ context.Context, userID string, upload types.PendingUpload) (types.PendingUpload, error) {
	access, _ := m.ResolveThreadAccess(context.Background(), userID, upload.ThreadID)
	if access == nil {
		return types.PendingUpload{}, types.ErrThreadNotFound
	}
	now := isoMillis(time.Now())
	if upload.TenantID == "" {
		upload.TenantID = types.DefaultTenantID
	}
	upload.PublicURL = nil
	upload.CreatedAt = now
	if upload.ExpiresAt == "" {
		upload.ExpiresAt = isoMillis(time.Now().Add(15 * time.Minute))
	}
	m.Pending = append(m.Pending, upload)
	return upload, nil
}

func (m *MemoryRepository) GetPendingUploads(_ context.Context, userID string, threadID string, uploadIDs []string, actor types.AuthContext) ([]types.PendingUpload, error) {
	wanted := map[string]bool{}
	for _, id := range uploadIDs {
		wanted[id] = true
	}
	uploads := []types.PendingUpload{}
	for _, upload := range m.Pending {
		if upload.ThreadID == threadID && pendingUploadOwnedBy(upload, actor) && wanted[upload.ID] {
			access, _ := m.ResolveThreadAccess(context.Background(), userID, threadID)
			if access == nil {
				continue
			}
			uploads = append(uploads, upload)
		}
	}
	return uploads, nil
}

func (m *MemoryRepository) MarkPendingUploadsConsumed(_ context.Context, userID string, threadID string, uploadIDs []string, actor types.AuthContext) error {
	wanted := map[string]bool{}
	for _, id := range uploadIDs {
		wanted[id] = true
	}
	now := isoMillis(time.Now())
	access, _ := m.ResolveThreadAccess(context.Background(), userID, threadID)
	if access == nil {
		return types.ErrThreadNotFound
	}
	for i := range m.Pending {
		if m.Pending[i].ThreadID == threadID && pendingUploadOwnedBy(m.Pending[i], actor) && wanted[m.Pending[i].ID] {
			m.Pending[i].ConsumedAt = &now
		}
	}
	return nil
}

func (m *MemoryRepository) PostMessage(_ context.Context, userID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, newAssets []types.NewAsset) (types.Message, error) {
	var threadIndex = -1
	for i, thread := range m.Threads {
		if thread.ID == threadID && normalMemoryThreadAccess(thread, userID) != nil {
			threadIndex = i
			break
		}
	}
	if threadIndex < 0 {
		return types.Message{}, types.ErrThreadNotFound
	}

	now := isoMillis(time.Now())
	message := types.Message{
		ID:                       "msg_" + uuid.NewString(),
		TenantID:                 firstNonEmptyString(m.Threads[threadIndex].TenantID, types.DefaultTenantID),
		ThreadID:                 threadID,
		Author:                   auth.ActorName,
		Body:                     body,
		BodyContentType:          bodyContentType,
		CreatedAt:                now,
		Assets:                   []types.Asset{},
		CreatedByUserID:          optionalString(auth.UserID),
		CreatedByKeyID:           optionalString(auth.KeyID),
		CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
		CreatedByActorName:       optionalString(auth.ActorName),
	}
	m.Messages = append(m.Messages, message)
	m.Threads[threadIndex].UpdatedAt = isoMillis(time.Now())

	for _, asset := range newAssets {
		createdAsset := types.Asset{
			ID:                       "asset_" + uuid.NewString(),
			TenantID:                 message.TenantID,
			MessageID:                message.ID,
			StorageKey:               asset.StorageKey,
			FileName:                 asset.FileName,
			Filename:                 asset.FileName,
			MimeType:                 asset.MimeType,
			SizeBytes:                asset.SizeBytes,
			PublicURL:                nil,
			DownloadURL:              nil,
			CreatedAt:                now,
			CreatedBy:                auth.ActorName,
			CreatedByUserID:          optionalString(auth.UserID),
			CreatedByKeyID:           optionalString(auth.KeyID),
			CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
			CreatedByActorName:       optionalString(auth.ActorName),
		}
		m.Assets = append(m.Assets, createdAsset)
		message.Assets = append(message.Assets, createdAsset)
	}
	return message, nil
}

func (m *MemoryRepository) CreateAPIKey(_ context.Context, userID string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string) (types.APIKey, error) {
	now := isoMillis(time.Now())
	if strings.TrimSpace(userID) == "" {
		return types.APIKey{}, errors.New("user ID is required")
	}
	userExists := false
	for _, user := range m.Users {
		if user.ID == userID {
			userExists = true
			break
		}
	}
	if !userExists {
		m.Users = append(m.Users, types.User{
			ID:          userID,
			TenantID:    types.DefaultTenantID,
			Email:       userID + "@example.invalid",
			DisplayName: userID,
			Role:        "member",
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	created := types.APIKey{
		ID:          "key_" + uuid.NewString(),
		UserID:      userID,
		Name:        name,
		Purpose:     purpose,
		KeyMasked:   maskSecret(tokenPrefix),
		TokenPrefix: tokenPrefix,
		TokenHash:   tokenHash,
		Scopes:      append([]string(nil), scopes...),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	for i := range m.APIKeys {
		if m.APIKeys[i].UserID == userID && strings.EqualFold(m.APIKeys[i].Name, name) && m.APIKeys[i].RevokedAt == nil {
			created.ID = m.APIKeys[i].ID
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

func (m *MemoryRepository) CreateOnboardingCredential(ctx context.Context, userID string, connector string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string, rotate bool) (types.APIKey, types.OnboardingState, error) {
	if !rotate {
		state, err := m.GetOnboardingState(ctx, userID)
		if err != nil {
			return types.APIKey{}, types.OnboardingState{}, err
		}
		for _, step := range state.Steps {
			if step.Connector == connector && step.Credential != nil {
				return types.APIKey{}, types.OnboardingState{}, types.ErrOnboardingCredentialExists
			}
		}
	}
	created, err := m.CreateAPIKey(ctx, userID, name, purpose, tokenHash, tokenPrefix, scopes)
	if err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}
	now := isoMillis(time.Now().UTC())
	index := m.onboardingIndex(userID)
	if index < 0 {
		m.Onboarding = append(m.Onboarding, types.OnboardingState{UserID: userID, CreatedAt: &now, UpdatedAt: &now, Steps: []types.OnboardingStep{}})
		index = len(m.Onboarding) - 1
	}
	state := &m.Onboarding[index]
	state.DismissedAt = nil
	state.UpdatedAt = &now
	updated := false
	for stepIndex := range state.Steps {
		if state.Steps[stepIndex].Connector != connector {
			continue
		}
		state.Steps[stepIndex].Credential = &created
		state.Steps[stepIndex].UpdatedAt = &now
		if state.Steps[stepIndex].CompletedAt == nil {
			state.Steps[stepIndex].CompletedAt = &now
		}
		updated = true
		break
	}
	if !updated {
		state.Steps = append(state.Steps, types.OnboardingStep{Connector: connector, CompletedAt: &now, UpdatedAt: &now, Credential: &created})
	}
	sort.SliceStable(state.Steps, func(i, j int) bool {
		return onboardingConnectorOrder(state.Steps[i].Connector) < onboardingConnectorOrder(state.Steps[j].Connector)
	})
	return created, m.onboardingStateCopy(*state), nil
}

func (m *MemoryRepository) GetOnboardingState(_ context.Context, userID string) (types.OnboardingState, error) {
	index := m.onboardingIndex(userID)
	if index < 0 {
		return types.OnboardingState{UserID: userID, Steps: []types.OnboardingStep{}}, nil
	}
	return m.onboardingStateCopy(m.Onboarding[index]), nil
}

func (m *MemoryRepository) DismissOnboarding(_ context.Context, userID string) (types.OnboardingState, error) {
	now := isoMillis(time.Now().UTC())
	index := m.onboardingIndex(userID)
	if index < 0 {
		m.Onboarding = append(m.Onboarding, types.OnboardingState{UserID: userID, CreatedAt: &now, UpdatedAt: &now, DismissedAt: &now, Steps: []types.OnboardingStep{}})
		index = len(m.Onboarding) - 1
	} else {
		m.Onboarding[index].DismissedAt = &now
		m.Onboarding[index].UpdatedAt = &now
	}
	return m.onboardingStateCopy(m.Onboarding[index]), nil
}

func (m *MemoryRepository) onboardingIndex(userID string) int {
	for index := range m.Onboarding {
		if m.Onboarding[index].UserID == userID {
			return index
		}
	}
	return -1
}

func (m *MemoryRepository) onboardingStateCopy(state types.OnboardingState) types.OnboardingState {
	copyState := state
	copyState.Steps = make([]types.OnboardingStep, 0, len(state.Steps))
	activeKeys := map[string]types.APIKey{}
	for _, key := range m.APIKeys {
		if key.UserID == state.UserID && key.RevokedAt == nil {
			activeKeys[key.ID] = key
		}
	}
	for _, step := range state.Steps {
		copyStep := step
		copyStep.Credential = nil
		if step.Credential != nil {
			if key, ok := activeKeys[step.Credential.ID]; ok {
				keyCopy := key
				copyStep.Credential = &keyCopy
			}
		}
		copyState.Steps = append(copyState.Steps, copyStep)
	}
	return copyState
}

func onboardingConnectorOrder(connector string) int {
	switch connector {
	case "chatgpt":
		return 1
	case "claude":
		return 2
	default:
		return 3
	}
}

func (m *MemoryRepository) ListAPIKeys(_ context.Context, userID string) ([]types.APIKey, error) {
	keys := []types.APIKey{}
	for _, key := range m.APIKeys {
		if key.UserID == userID && key.RevokedAt == nil {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Name < keys[j].Name
	})
	return keys, nil
}

func (m *MemoryRepository) RevokeAPIKey(_ context.Context, userID string, name string) (bool, error) {
	now := isoMillis(time.Now())
	for i, key := range m.APIKeys {
		if key.UserID == userID && strings.EqualFold(key.Name, name) && key.RevokedAt == nil {
			m.APIKeys[i].RevokedAt = &now
			m.APIKeys[i].UpdatedAt = now
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryRepository) FindAPIKeyBySecret(_ context.Context, secret string) (*types.APIKey, *types.User, error) {
	for _, key := range m.APIKeys {
		if key.RevokedAt != nil || (key.Key != secret && (key.TokenHash == "" || key.TokenHash != hashSecret(secret))) {
			continue
		}
		for _, user := range m.Users {
			if user.ID == key.UserID && user.DisabledAt == nil {
				found := key
				foundUser := user
				return &found, &foundUser, nil
			}
		}
	}
	return nil, nil, nil
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

func (m *MemoryRepository) UpsertTenant(_ context.Context, tenant types.Tenant) (types.Tenant, error) {
	now := isoMillis(time.Now())
	if tenant.ID == "" {
		tenant.ID = tenantOf(tenant.Slug)
	}
	tenant.Slug = strings.TrimSpace(tenant.Slug)
	tenant.Name = strings.TrimSpace(tenant.Name)
	for i := range m.Tenants {
		if strings.EqualFold(m.Tenants[i].Slug, tenant.Slug) {
			m.Tenants[i].Name = tenant.Name
			m.Tenants[i].UpdatedAt = now
			return m.Tenants[i], nil
		}
	}
	tenant.CreatedAt = now
	tenant.UpdatedAt = now
	m.Tenants = append(m.Tenants, tenant)
	return tenant, nil
}

func (m *MemoryRepository) GetTenant(_ context.Context, idOrSlug string) (*types.Tenant, error) {
	idOrSlug = strings.TrimSpace(idOrSlug)
	for _, tenant := range m.Tenants {
		if tenant.ID == idOrSlug || tenant.Slug == idOrSlug {
			copy := tenant
			return &copy, nil
		}
	}
	if idOrSlug == types.DefaultTenantID || idOrSlug == "default" {
		return &types.Tenant{
			ID:        types.DefaultTenantID,
			Slug:      "default",
			Name:      "Default",
			CreatedAt: isoMillis(time.Now()),
			UpdatedAt: isoMillis(time.Now()),
		}, nil
	}
	return nil, nil
}

func (m *MemoryRepository) BootstrapOwner(_ context.Context, email string, displayName string, passwordHash string) (types.User, error) {
	now := isoMillis(time.Now())
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if email == "" || displayName == "" || passwordHash == "" {
		return types.User{}, errors.New("owner email, display name, and password hash are required")
	}
	for i := range m.Users {
		if m.Users[i].IsOwner {
			if !strings.EqualFold(m.Users[i].Email, email) {
				return types.User{}, ErrOwnerAlreadyExists
			}
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].Role = "admin"
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignLegacyThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	for i := range m.Users {
		if strings.EqualFold(m.Users[i].Email, email) {
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].Role = "admin"
			m.Users[i].IsOwner = true
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignLegacyThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	owner := types.User{
		ID:           "usr_" + uuid.NewString(),
		TenantID:     types.DefaultTenantID,
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		Role:         "admin",
		IsOwner:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.Users = append(m.Users, owner)
	m.assignLegacyThreadsToOwner(owner.ID)
	return owner, nil
}

func (m *MemoryRepository) CreateOwnerSetupToken(_ context.Context, tokenHash string, expiresAt time.Time) (types.OwnerSetupToken, error) {
	if strings.TrimSpace(tokenHash) == "" || !expiresAt.After(time.Now().UTC()) {
		return types.OwnerSetupToken{}, errors.New("owner setup token hash and future expiry are required")
	}
	now := time.Now().UTC()
	nowValue := isoMillis(now)
	for index := range m.OwnerSetupTokens {
		if m.OwnerSetupTokens[index].Token.ConsumedAt == nil && m.OwnerSetupTokens[index].Token.RevokedAt == nil {
			m.OwnerSetupTokens[index].Token.RevokedAt = &nowValue
		}
	}
	purpose := "bootstrap"
	for _, user := range m.Users {
		if user.IsOwner {
			purpose = "recovery"
			break
		}
	}
	token := types.OwnerSetupToken{
		ID:        "ost_" + uuid.NewString(),
		Purpose:   purpose,
		CreatedAt: nowValue,
		ExpiresAt: isoMillis(expiresAt),
	}
	m.OwnerSetupTokens = append(m.OwnerSetupTokens, memoryOwnerSetupToken{Token: token, TokenHash: tokenHash})
	return token, nil
}

func (m *MemoryRepository) UseOwnerSetupToken(_ context.Context, tokenHash string, email string, displayName string, passwordHash string) (types.User, types.OwnerSetupToken, error) {
	now := time.Now().UTC()
	for index := range m.OwnerSetupTokens {
		entry := &m.OwnerSetupTokens[index]
		expiresAt, err := time.Parse(time.RFC3339Nano, entry.Token.ExpiresAt)
		if err != nil || entry.TokenHash != tokenHash || entry.Token.ConsumedAt != nil || entry.Token.RevokedAt != nil || !expiresAt.After(now) {
			continue
		}
		ownerIndex := -1
		for userIndex := range m.Users {
			if m.Users[userIndex].IsOwner {
				ownerIndex = userIndex
				break
			}
		}
		if entry.Token.Purpose == "bootstrap" && ownerIndex >= 0 {
			return types.User{}, types.OwnerSetupToken{}, ErrOwnerSetupTokenInvalid
		}
		if entry.Token.Purpose == "recovery" && ownerIndex < 0 {
			return types.User{}, types.OwnerSetupToken{}, ErrOwnerSetupTokenInvalid
		}
		if ownerIndex >= 0 && !strings.EqualFold(m.Users[ownerIndex].Email, strings.TrimSpace(email)) {
			return types.User{}, types.OwnerSetupToken{}, ErrOwnerAlreadyExists
		}
		owner, err := m.bootstrapOwner(strings.TrimSpace(email), strings.TrimSpace(displayName), passwordHash)
		if err != nil {
			return types.User{}, types.OwnerSetupToken{}, err
		}
		consumedAt := isoMillis(now)
		entry.Token.ConsumedAt = &consumedAt
		return owner, entry.Token, nil
	}
	return types.User{}, types.OwnerSetupToken{}, ErrOwnerSetupTokenInvalid
}

func (m *MemoryRepository) bootstrapOwner(email string, displayName string, passwordHash string) (types.User, error) {
	now := isoMillis(time.Now())
	if email == "" || displayName == "" || passwordHash == "" {
		return types.User{}, errors.New("owner email, display name, and password hash are required")
	}
	for i := range m.Users {
		if m.Users[i].IsOwner {
			if !strings.EqualFold(m.Users[i].Email, email) {
				return types.User{}, ErrOwnerAlreadyExists
			}
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].Role = "admin"
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignLegacyThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	for i := range m.Users {
		if strings.EqualFold(m.Users[i].Email, email) {
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].Role = "admin"
			m.Users[i].IsOwner = true
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignLegacyThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	owner := types.User{
		ID:           "usr_" + uuid.NewString(),
		TenantID:     types.DefaultTenantID,
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		Role:         "admin",
		IsOwner:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.Users = append(m.Users, owner)
	m.assignLegacyThreadsToOwner(owner.ID)
	return owner, nil
}

func (m *MemoryRepository) assignLegacyThreadsToOwner(ownerUserID string) {
	for i := range m.Threads {
		if strings.TrimSpace(m.Threads[i].OwnerUserID) == "" {
			m.Threads[i].OwnerUserID = ownerUserID
		}
	}
}

func (m *MemoryRepository) CreateSignupInvitation(_ context.Context, createdByUserID string, tokenHash string, expiresAt time.Time, teamIDs []string) (types.SignupInvitation, error) {
	if strings.TrimSpace(createdByUserID) == "" || strings.TrimSpace(tokenHash) == "" || !expiresAt.After(time.Now().UTC()) {
		return types.SignupInvitation{}, errors.New("invitation creator, token hash, and future expiry are required")
	}
	teamIDs = uniqueNonEmptyStrings(teamIDs)
	teams := make([]types.Team, 0, len(teamIDs))
	for _, teamID := range teamIDs {
		found := false
		for _, team := range m.Teams {
			if team.ID == teamID {
				teams = append(teams, team)
				found = true
				break
			}
		}
		if !found {
			return types.SignupInvitation{}, types.ErrTeamNotFound
		}
	}
	now := isoMillis(time.Now().UTC())
	invitation := types.SignupInvitation{
		ID:              "inv_" + uuid.NewString(),
		CreatedByUserID: createdByUserID,
		CreatedAt:       now,
		ExpiresAt:       isoMillis(expiresAt),
		Teams:           teams,
	}
	m.SignupInvitations = append(m.SignupInvitations, memorySignupInvitation{Invitation: invitation, TokenHash: tokenHash, TeamIDs: teamIDs})
	return invitation, nil
}

func (m *MemoryRepository) ListSignupInvitations(context.Context) ([]types.SignupInvitation, error) {
	invitations := make([]types.SignupInvitation, 0, len(m.SignupInvitations))
	for index := len(m.SignupInvitations) - 1; index >= 0; index-- {
		invitation, err := m.hydrateSignupInvitation(m.SignupInvitations[index])
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, invitation)
	}
	return invitations, nil
}

func (m *MemoryRepository) RevokeSignupInvitation(_ context.Context, invitationID string) (bool, error) {
	now := isoMillis(time.Now().UTC())
	for index := range m.SignupInvitations {
		invitation := &m.SignupInvitations[index].Invitation
		if invitation.ID == invitationID && invitation.ConsumedAt == nil && invitation.RevokedAt == nil {
			invitation.RevokedAt = &now
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryRepository) FindSignupInvitation(_ context.Context, tokenHash string) (*types.SignupInvitation, error) {
	now := time.Now().UTC()
	for _, entry := range m.SignupInvitations {
		expiresAt, err := time.Parse(time.RFC3339Nano, entry.Invitation.ExpiresAt)
		if err != nil || entry.TokenHash != tokenHash || entry.Invitation.ConsumedAt != nil || entry.Invitation.RevokedAt != nil || !expiresAt.After(now) {
			continue
		}
		invitation, err := m.hydrateSignupInvitation(entry)
		if err != nil {
			return nil, err
		}
		return &invitation, nil
	}
	return nil, nil
}

func (m *MemoryRepository) RegisterWithSignupInvitation(_ context.Context, tokenHash string, email string, displayName string, passwordHash string, sessionSecretHash string, sessionExpiresAt time.Time) (types.User, types.UserSession, types.SignupInvitation, error) {
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	now := time.Now().UTC()
	invitationIndex := -1
	for index, entry := range m.SignupInvitations {
		expiresAt, err := time.Parse(time.RFC3339Nano, entry.Invitation.ExpiresAt)
		if err == nil && entry.TokenHash == tokenHash && entry.Invitation.ConsumedAt == nil && entry.Invitation.RevokedAt == nil && expiresAt.After(now) {
			invitationIndex = index
			break
		}
	}
	if invitationIndex < 0 || email == "" || displayName == "" || passwordHash == "" || sessionSecretHash == "" || !sessionExpiresAt.After(now) {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, types.ErrSignupInvitationInvalid
	}
	for _, user := range m.Users {
		if strings.EqualFold(strings.TrimSpace(user.Email), email) {
			return types.User{}, types.UserSession{}, types.SignupInvitation{}, types.ErrEmailAlreadyRegistered
		}
	}
	nowValue := isoMillis(now)
	invitationValue, err := m.hydrateSignupInvitation(m.SignupInvitations[invitationIndex])
	if err != nil {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, err
	}
	user := types.User{
		ID:           "usr_" + uuid.NewString(),
		TenantID:     types.DefaultTenantID,
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		Role:         "member",
		CreatedAt:    nowValue,
		UpdatedAt:    nowValue,
	}
	memberships := make([]types.TeamMembership, 0, len(invitationValue.Teams))
	for _, team := range invitationValue.Teams {
		memberships = append(memberships, types.TeamMembership{TeamID: team.ID, UserID: user.ID, CreatedAt: nowValue})
	}
	session := types.UserSession{
		ID:         "sess_" + uuid.NewString(),
		UserID:     user.ID,
		SecretHash: sessionSecretHash,
		CreatedAt:  nowValue,
		ExpiresAt:  isoMillis(sessionExpiresAt),
	}
	consumedAt := nowValue
	invitation := &m.SignupInvitations[invitationIndex].Invitation
	invitation.ConsumedAt = &consumedAt
	invitation.ConsumedByUserID = &user.ID
	invitation.Teams = invitationValue.Teams
	m.Users = append(m.Users, user)
	m.Sessions = append(m.Sessions, session)
	m.TeamMemberships = append(m.TeamMemberships, memberships...)
	return user, session, *invitation, nil
}

func (m *MemoryRepository) hydrateSignupInvitation(entry memorySignupInvitation) (types.SignupInvitation, error) {
	invitation := entry.Invitation
	invitation.Teams = make([]types.Team, 0, len(entry.TeamIDs))
	for _, teamID := range entry.TeamIDs {
		found := false
		for _, team := range m.Teams {
			if team.ID == teamID {
				invitation.Teams = append(invitation.Teams, team)
				found = true
				break
			}
		}
		if !found {
			return types.SignupInvitation{}, types.ErrTeamNotFound
		}
	}
	sort.SliceStable(invitation.Teams, func(i, j int) bool {
		if !strings.EqualFold(invitation.Teams[i].Name, invitation.Teams[j].Name) {
			return strings.ToLower(invitation.Teams[i].Name) < strings.ToLower(invitation.Teams[j].Name)
		}
		return invitation.Teams[i].ID < invitation.Teams[j].ID
	})
	return invitation, nil
}

func (m *MemoryRepository) CreateTeam(_ context.Context, slug string, name string) (types.Team, error) {
	slug = strings.TrimSpace(slug)
	name = strings.TrimSpace(name)
	for _, team := range m.Teams {
		if strings.EqualFold(team.Slug, slug) {
			return types.Team{}, types.ErrTeamSlugConflict
		}
	}
	now := isoMillis(time.Now().UTC())
	team := types.Team{ID: "team_" + uuid.NewString(), Slug: slug, Name: name, CreatedAt: now, UpdatedAt: now}
	m.Teams = append(m.Teams, team)
	return team, nil
}

func (m *MemoryRepository) RenameTeam(_ context.Context, teamID string, name string) (types.Team, error) {
	teamID = strings.TrimSpace(teamID)
	for index := range m.Teams {
		if m.Teams[index].ID != teamID {
			continue
		}
		m.Teams[index].Name = strings.TrimSpace(name)
		m.Teams[index].UpdatedAt = isoMillis(time.Now().UTC())
		return m.Teams[index], nil
	}
	return types.Team{}, types.ErrTeamNotFound
}

func (m *MemoryRepository) ListTeams(context.Context) ([]types.Team, error) {
	teams := append([]types.Team(nil), m.Teams...)
	sort.SliceStable(teams, func(i, j int) bool {
		if !strings.EqualFold(teams[i].Name, teams[j].Name) {
			return strings.ToLower(teams[i].Name) < strings.ToLower(teams[j].Name)
		}
		if !strings.EqualFold(teams[i].Slug, teams[j].Slug) {
			return strings.ToLower(teams[i].Slug) < strings.ToLower(teams[j].Slug)
		}
		return teams[i].ID < teams[j].ID
	})
	return teams, nil
}

func (m *MemoryRepository) ListUserTeams(_ context.Context, userID string) ([]types.Team, error) {
	wanted := map[string]struct{}{}
	for _, membership := range m.TeamMemberships {
		if membership.UserID == strings.TrimSpace(userID) {
			wanted[membership.TeamID] = struct{}{}
		}
	}
	teams := []types.Team{}
	for _, team := range m.Teams {
		if _, exists := wanted[team.ID]; exists {
			teams = append(teams, team)
		}
	}
	sort.SliceStable(teams, func(i, j int) bool {
		if !strings.EqualFold(teams[i].Name, teams[j].Name) {
			return strings.ToLower(teams[i].Name) < strings.ToLower(teams[j].Name)
		}
		return teams[i].ID < teams[j].ID
	})
	return teams, nil
}

func (m *MemoryRepository) ListTeamMembers(_ context.Context, teamID string) ([]types.User, error) {
	teamID = strings.TrimSpace(teamID)
	teamFound := false
	for _, team := range m.Teams {
		if team.ID == teamID {
			teamFound = true
			break
		}
	}
	if !teamFound {
		return nil, types.ErrTeamNotFound
	}
	wanted := map[string]struct{}{}
	for _, membership := range m.TeamMemberships {
		if membership.TeamID == teamID {
			wanted[membership.UserID] = struct{}{}
		}
	}
	users := []types.User{}
	for _, user := range m.Users {
		if _, exists := wanted[user.ID]; exists {
			users = append(users, user)
		}
	}
	sort.SliceStable(users, func(i, j int) bool {
		if users[i].IsOwner != users[j].IsOwner {
			return users[i].IsOwner
		}
		if !strings.EqualFold(users[i].DisplayName, users[j].DisplayName) {
			return strings.ToLower(users[i].DisplayName) < strings.ToLower(users[j].DisplayName)
		}
		return users[i].ID < users[j].ID
	})
	return users, nil
}

func (m *MemoryRepository) AddTeamMember(_ context.Context, teamID string, userID string) (types.TeamMembership, error) {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	teamFound := false
	for _, team := range m.Teams {
		if team.ID == teamID {
			teamFound = true
			break
		}
	}
	if !teamFound {
		return types.TeamMembership{}, types.ErrTeamNotFound
	}
	userFound := false
	for _, user := range m.Users {
		if user.ID == userID {
			userFound = true
			break
		}
	}
	if !userFound {
		return types.TeamMembership{}, types.ErrUserNotFound
	}
	for _, membership := range m.TeamMemberships {
		if membership.TeamID == teamID && membership.UserID == userID {
			return membership, nil
		}
	}
	membership := types.TeamMembership{TeamID: teamID, UserID: userID, CreatedAt: isoMillis(time.Now().UTC())}
	m.TeamMemberships = append(m.TeamMemberships, membership)
	return membership, nil
}

func (m *MemoryRepository) RemoveTeamMember(_ context.Context, teamID string, userID string) (bool, error) {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	teamFound := false
	for _, team := range m.Teams {
		if team.ID == teamID {
			teamFound = true
			break
		}
	}
	if !teamFound {
		return false, types.ErrTeamNotFound
	}
	userFound := false
	for _, user := range m.Users {
		if user.ID == userID {
			userFound = true
			break
		}
	}
	if !userFound {
		return false, types.ErrUserNotFound
	}
	for index, membership := range m.TeamMemberships {
		if membership.TeamID == teamID && membership.UserID == userID {
			m.TeamMemberships = append(m.TeamMemberships[:index], m.TeamMemberships[index+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryRepository) ListUsers(context.Context) ([]types.User, error) {
	users := append([]types.User(nil), m.Users...)
	sort.SliceStable(users, func(i, j int) bool {
		if users[i].IsOwner != users[j].IsOwner {
			return users[i].IsOwner
		}
		if users[i].CreatedAt != users[j].CreatedAt {
			return users[i].CreatedAt < users[j].CreatedAt
		}
		return users[i].ID < users[j].ID
	})
	return users, nil
}

func (m *MemoryRepository) SetUserDisabled(_ context.Context, userID string, disabled bool) (types.User, error) {
	now := isoMillis(time.Now().UTC())
	for index := range m.Users {
		user := &m.Users[index]
		if user.ID != userID {
			continue
		}
		if disabled && user.IsOwner {
			return types.User{}, types.ErrOwnerCannotBeDisabled
		}
		if disabled {
			if user.DisabledAt == nil {
				user.DisabledAt = &now
			}
			for sessionIndex := range m.Sessions {
				if m.Sessions[sessionIndex].UserID == user.ID && m.Sessions[sessionIndex].RevokedAt == nil {
					m.Sessions[sessionIndex].RevokedAt = &now
				}
			}
			for keyIndex := range m.APIKeys {
				if m.APIKeys[keyIndex].UserID == user.ID && m.APIKeys[keyIndex].RevokedAt == nil {
					m.APIKeys[keyIndex].RevokedAt = &now
					m.APIKeys[keyIndex].UpdatedAt = now
				}
			}
			for codeIndex := range m.CLICodes {
				if m.CLICodes[codeIndex].UserID == user.ID && m.CLICodes[codeIndex].ConsumedAt == nil {
					m.CLICodes[codeIndex].ConsumedAt = &now
				}
			}
		} else {
			user.DisabledAt = nil
		}
		user.UpdatedAt = now
		return *user, nil
	}
	return types.User{}, types.ErrUserNotFound
}

func (m *MemoryRepository) UpsertProvisionedUser(_ context.Context, tenantID string, email string, displayName string, passwordHash *string, role string) (types.User, error) {
	now := isoMillis(time.Now())
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	for i := range m.Users {
		if strings.EqualFold(m.Users[i].Email, email) {
			m.Users[i].DisplayName = displayName
			if passwordHash != nil {
				m.Users[i].PasswordHash = passwordHash
			}
			m.Users[i].Role = role
			m.Users[i].UpdatedAt = now
			m.Users[i].DisabledAt = nil
			return m.Users[i], nil
		}
	}
	user := types.User{
		ID:           "usr_" + uuid.NewString(),
		TenantID:     tenantOf(tenantID),
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		Role:         role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.Users = append(m.Users, user)
	return user, nil
}

func (m *MemoryRepository) FindUserByEmail(_ context.Context, _ string, email string) (*types.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, user := range m.Users {
		if user.DisabledAt != nil || strings.ToLower(strings.TrimSpace(user.Email)) != email {
			continue
		}
		copy := user
		return &copy, nil
	}
	return nil, nil
}

func (m *MemoryRepository) CreateUserSession(_ context.Context, session types.UserSession) (types.UserSession, error) {
	now := isoMillis(time.Now())
	if session.ID == "" {
		session.ID = "sess_" + uuid.NewString()
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
			if user.ID == session.UserID && user.DisabledAt == nil {
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

func (m *MemoryRepository) CreateCLILoginCode(_ context.Context, code types.CLILoginCode) (types.CLILoginCode, error) {
	now := isoMillis(time.Now())
	if code.ID == "" {
		code.ID = "clicode_" + uuid.NewString()
	}
	code.CreatedAt = now
	if code.ExpiresAt == "" {
		code.ExpiresAt = isoMillis(time.Now().Add(5 * time.Minute))
	}
	m.CLICodes = append(m.CLICodes, code)
	return code, nil
}

func (m *MemoryRepository) ConsumeCLILoginCode(_ context.Context, codeHash string, stateHash string, redirectURI string) (*types.CLILoginCode, *types.User, error) {
	now := time.Now().UTC()
	consumedAt := isoMillis(now)
	for i := range m.CLICodes {
		code := m.CLICodes[i]
		if code.CodeHash != codeHash || code.StateHash != stateHash || code.RedirectURI != redirectURI || code.ConsumedAt != nil {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339, code.ExpiresAt)
		if err != nil || !expiresAt.After(now) {
			return nil, nil, nil
		}
		for _, user := range m.Users {
			if user.ID == code.UserID && user.DisabledAt == nil {
				m.CLICodes[i].ConsumedAt = &consumedAt
				code.ConsumedAt = &consumedAt
				userCopy := user
				return &code, &userCopy, nil
			}
		}
		return nil, nil, nil
	}
	return nil, nil, nil
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
	if strings.TrimSpace(owner.UserID) == "" || upload.CreatedByUserID == nil || *upload.CreatedByUserID != owner.UserID {
		return false
	}
	if strings.TrimSpace(owner.KeyID) == "" {
		return upload.CreatedByKeyID == nil || strings.TrimSpace(*upload.CreatedByKeyID) == ""
	}
	return upload.CreatedByKeyID != nil && *upload.CreatedByKeyID == owner.KeyID
}

// normalMemoryThreadAccess mirrors the SQL normalThreadAccessPredicate. Phase 7
// must widen both implementations together when team sharing is introduced.
func normalMemoryThreadAccess(thread types.Thread, userID string) *types.ThreadAccess {
	if strings.TrimSpace(userID) == "" || thread.OwnerUserID != userID {
		return nil
	}
	return &types.ThreadAccess{
		ThreadID:    thread.ID,
		OwnerUserID: thread.OwnerUserID,
		UserID:      userID,
		IsOwner:     true,
	}
}
