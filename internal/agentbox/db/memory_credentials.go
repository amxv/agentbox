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
			Email:       userID + "@example.invalid",
			DisplayName: userID,
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
			return types.APIKey{}, types.ErrCredentialLabelConflict
		}
	}
	m.APIKeys = append(m.APIKeys, created)
	sort.Slice(m.APIKeys, func(i, j int) bool {
		return m.APIKeys[i].Name < m.APIKeys[j].Name
	})
	return created, nil
}

func (m *MemoryRepository) CreateRaycastAPIKey(ctx context.Context, userID string, name string, tokenHash string, tokenPrefix string, scopes []string, setupBaseURL string) (types.APIKey, error) {
	for _, key := range m.APIKeys {
		if key.UserID == strings.TrimSpace(userID) && strings.EqualFold(key.Name, strings.TrimSpace(name)) && key.RevokedAt == nil {
			return types.APIKey{}, types.ErrCredentialLabelConflict
		}
	}
	created, err := m.CreateAPIKey(ctx, userID, name, "raycast", tokenHash, tokenPrefix, scopes)
	if err != nil {
		return types.APIKey{}, err
	}
	if m.RaycastSetupURLs == nil {
		m.RaycastSetupURLs = map[string]string{}
	}
	m.RaycastSetupURLs[created.ID] = strings.TrimRight(strings.TrimSpace(setupBaseURL), "/")
	return created, nil
}

func (m *MemoryRepository) raycastSetupBaseURL(keyID string) string {
	if m.RaycastSetupURLs == nil {
		return ""
	}
	return m.RaycastSetupURLs[keyID]
}

func (m *MemoryRepository) CreateOnboardingCredential(ctx context.Context, userID string, connector string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string, setupBaseURL string, rotate bool) (types.APIKey, types.OnboardingState, error) {
	now := isoMillis(time.Now().UTC())
	index := m.onboardingIndex(userID)
	if index < 0 {
		m.Onboarding = append(m.Onboarding, types.OnboardingState{UserID: userID, CreatedAt: &now, UpdatedAt: &now, Steps: []types.OnboardingStep{}})
		index = len(m.Onboarding) - 1
	}
	state := &m.Onboarding[index]
	stepIndex := -1
	linkedCredentialID := ""
	for candidateIndex := range state.Steps {
		if state.Steps[candidateIndex].Connector != connector {
			continue
		}
		stepIndex = candidateIndex
		if state.Steps[candidateIndex].Credential != nil {
			linkedCredentialID = state.Steps[candidateIndex].Credential.ID
		}
		break
	}
	linkedKeyIndex := -1
	if linkedCredentialID != "" {
		for keyIndex := range m.APIKeys {
			if m.APIKeys[keyIndex].ID == linkedCredentialID && m.APIKeys[keyIndex].UserID == userID && m.APIKeys[keyIndex].RevokedAt == nil {
				linkedKeyIndex = keyIndex
				break
			}
		}
	}
	if rotate && linkedKeyIndex < 0 {
		return types.APIKey{}, types.OnboardingState{}, types.ErrOnboardingCredentialNotFound
	}
	if !rotate && linkedKeyIndex >= 0 {
		return types.APIKey{}, types.OnboardingState{}, types.ErrOnboardingCredentialExists
	}

	var created types.APIKey
	if rotate {
		for keyIndex := range m.APIKeys {
			if keyIndex != linkedKeyIndex && m.APIKeys[keyIndex].UserID == userID && strings.EqualFold(m.APIKeys[keyIndex].Name, name) && m.APIKeys[keyIndex].RevokedAt == nil {
				return types.APIKey{}, types.OnboardingState{}, types.ErrCredentialLabelConflict
			}
		}
		key := &m.APIKeys[linkedKeyIndex]
		key.Name = name
		key.Purpose = purpose
		key.TokenHash = tokenHash
		key.TokenPrefix = tokenPrefix
		key.KeyMasked = maskSecret(tokenPrefix)
		key.Scopes = append([]string(nil), scopes...)
		key.UpdatedAt = now
		key.LastUsedAt = nil
		created = *key
	} else {
		var err error
		created, err = m.CreateAPIKey(ctx, userID, name, purpose, tokenHash, tokenPrefix, scopes)
		if err != nil {
			return types.APIKey{}, types.OnboardingState{}, err
		}
	}
	if purpose == "raycast" {
		if m.RaycastSetupURLs == nil {
			m.RaycastSetupURLs = map[string]string{}
		}
		m.RaycastSetupURLs[created.ID] = strings.TrimRight(strings.TrimSpace(setupBaseURL), "/")
	}
	state.DismissedAt = nil
	state.UpdatedAt = &now
	if stepIndex >= 0 {
		state.Steps[stepIndex].Credential = &created
		state.Steps[stepIndex].UpdatedAt = &now
		if state.Steps[stepIndex].CompletedAt == nil {
			state.Steps[stepIndex].CompletedAt = &now
		}
	} else {
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
	case "local":
		return 3
	case "raycast":
		return 4
	default:
		return 5
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

func (m *MemoryRepository) ListAPIKeysPage(_ context.Context, userID string, pageRequest types.PageRequest) (types.APIKeyPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	keys := []types.APIKey{}
	for _, key := range m.APIKeys {
		if key.UserID == strings.TrimSpace(userID) {
			keys = append(keys, key)
		}
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].CreatedAt != keys[j].CreatedAt {
			return keys[i].CreatedAt > keys[j].CreatedAt
		}
		return keys[i].ID > keys[j].ID
	})
	start := pageRequest.Offset
	if start > len(keys) {
		start = len(keys)
	}
	end := start + pageRequest.Limit + 1
	if end > len(keys) {
		end = len(keys)
	}
	window := keys[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.APIKeyPage{Credentials: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) ListAllAPIKeys(ctx context.Context) ([]types.APIKey, error) {
	page, err := m.ListAllAPIKeysPage(ctx, types.PageRequest{})
	return page.Credentials, err
}

func (m *MemoryRepository) ListAllAPIKeysPage(_ context.Context, pageRequest types.PageRequest) (types.APIKeyPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	keys := append([]types.APIKey(nil), m.APIKeys...)
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].CreatedAt != keys[j].CreatedAt {
			return keys[i].CreatedAt > keys[j].CreatedAt
		}
		return keys[i].ID > keys[j].ID
	})
	start := pageRequest.Offset
	if start > len(keys) {
		start = len(keys)
	}
	end := start + pageRequest.Limit + 1
	if end > len(keys) {
		end = len(keys)
	}
	window := keys[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.APIKeyPage{Credentials: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) RevokeAPIKeyForUserByID(_ context.Context, userID string, keyID string) (bool, error) {
	now := isoMillis(time.Now().UTC())
	for index := range m.APIKeys {
		if m.APIKeys[index].UserID != strings.TrimSpace(userID) || m.APIKeys[index].ID != strings.TrimSpace(keyID) {
			continue
		}
		if m.APIKeys[index].RevokedAt == nil {
			m.APIKeys[index].RevokedAt = &now
		}
		m.APIKeys[index].UpdatedAt = now
		return true, nil
	}
	return false, nil
}

func (m *MemoryRepository) RevokeAPIKeyByID(_ context.Context, keyID string) (bool, error) {
	now := isoMillis(time.Now().UTC())
	for index := range m.APIKeys {
		if m.APIKeys[index].ID != strings.TrimSpace(keyID) {
			continue
		}
		if m.APIKeys[index].RevokedAt == nil {
			m.APIKeys[index].RevokedAt = &now
		}
		m.APIKeys[index].UpdatedAt = now
		return true, nil
	}
	return false, nil
}

func (m *MemoryRepository) RotateAPIKeyForUserByID(_ context.Context, userID string, keyID string, tokenHash string, tokenPrefix string, setupBaseURL string) (*types.APIKey, string, error) {
	now := isoMillis(time.Now().UTC())
	for index := range m.APIKeys {
		key := &m.APIKeys[index]
		if key.UserID != strings.TrimSpace(userID) || key.ID != strings.TrimSpace(keyID) || key.RevokedAt != nil {
			continue
		}
		resolvedBaseURL := m.raycastSetupBaseURL(key.ID)
		if key.Purpose == "raycast" && strings.TrimSpace(resolvedBaseURL) == "" {
			resolvedBaseURL = strings.TrimRight(strings.TrimSpace(setupBaseURL), "/")
			if resolvedBaseURL == "" {
				return nil, "", types.ErrRaycastSetupUnavailable
			}
			if m.RaycastSetupURLs == nil {
				m.RaycastSetupURLs = map[string]string{}
			}
			m.RaycastSetupURLs[key.ID] = resolvedBaseURL
		}
		key.TokenHash = tokenHash
		key.TokenPrefix = tokenPrefix
		key.KeyMasked = maskSecret(tokenPrefix)
		key.UpdatedAt = now
		key.LastUsedAt = nil
		copyKey := *key
		return &copyKey, resolvedBaseURL, nil
	}
	return nil, "", nil
}

func (m *MemoryRepository) GetAPIKeySetup(_ context.Context, userID string, keyID string, setupBaseURL string) (*types.APIKey, string, error) {
	for _, key := range m.APIKeys {
		if key.UserID == strings.TrimSpace(userID) && key.ID == strings.TrimSpace(keyID) {
			copyKey := key
			baseURL := m.raycastSetupBaseURL(key.ID)
			if key.Purpose == "raycast" && strings.TrimSpace(baseURL) == "" && strings.TrimSpace(setupBaseURL) != "" {
				baseURL = strings.TrimRight(strings.TrimSpace(setupBaseURL), "/")
				if m.RaycastSetupURLs == nil {
					m.RaycastSetupURLs = map[string]string{}
				}
				m.RaycastSetupURLs[key.ID] = baseURL
			}
			return &copyKey, baseURL, nil
		}
	}
	return nil, "", nil
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
