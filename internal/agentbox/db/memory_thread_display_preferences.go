package db

import (
	"context"

	"agentbox/internal/agentbox/types"
)

func (m *MemoryRepository) canReadThread(userID string, threadID string) bool {
	for _, thread := range m.Threads {
		if thread.ID == threadID && m.normalThreadAccess(thread, userID) != nil {
			return true
		}
	}
	return false
}

func (m *MemoryRepository) GetThreadActivity(_ context.Context, userID string, threadID string) (string, error) {
	for _, thread := range m.Threads {
		if thread.ID == threadID && m.normalThreadAccess(thread, userID) != nil {
			return thread.UpdatedAt, nil
		}
	}
	return "", types.ErrThreadNotFound
}

func (m *MemoryRepository) GetThreadDisplayPreference(_ context.Context, userID string, threadID string) (types.ThreadDisplayPreference, error) {
	if !m.canReadThread(userID, threadID) {
		return types.ThreadDisplayPreference{}, types.ErrThreadNotFound
	}
	preference, exists := m.ThreadDisplayPreferences[userID+"\x00"+threadID]
	if !exists {
		return types.ThreadDisplayPreference{MessagePreferences: map[string]bool{}}, nil
	}
	clone := make(map[string]bool, len(preference.MessagePreferences))
	for id, enabled := range preference.MessagePreferences {
		clone[id] = enabled
	}
	preference.MessagePreferences = clone
	return preference, nil
}

func (m *MemoryRepository) SaveThreadDisplayPreference(_ context.Context, userID string, threadID string, preference types.ThreadDisplayPreference) error {
	if !m.canReadThread(userID, threadID) {
		return types.ErrThreadNotFound
	}
	if m.ThreadDisplayPreferences == nil {
		m.ThreadDisplayPreferences = make(map[string]types.ThreadDisplayPreference)
	}
	clone := make(map[string]bool, len(preference.MessagePreferences))
	for id, enabled := range preference.MessagePreferences {
		clone[id] = enabled
	}
	preference.MessagePreferences = clone
	m.ThreadDisplayPreferences[userID+"\x00"+threadID] = preference
	return nil
}
