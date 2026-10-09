package service

import (
	"context"
	"strings"

	"agentbox/internal/agentbox/types"
)

const maxMessageDisplayOverrides = 1000

func (s *Service) GetThreadDisplayPreference(ctx context.Context, auth types.AuthContext, threadID string) (types.ThreadDisplayPreference, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return types.ThreadDisplayPreference{}, err
	}
	return s.repo.GetThreadDisplayPreference(ctx, auth.UserID, threadID)
}

func (s *Service) SaveThreadDisplayPreference(ctx context.Context, auth types.AuthContext, threadID string, preference types.ThreadDisplayPreference) (types.ThreadDisplayPreference, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return types.ThreadDisplayPreference{}, err
	}
	if len(preference.MessagePreferences) > maxMessageDisplayOverrides {
		return types.ThreadDisplayPreference{}, CodedError{Code: "INVALID_ARGUMENT", Message: "Too many message display preferences."}
	}
	for id := range preference.MessagePreferences {
		if len(id) == 0 || len(id) > 128 || strings.TrimSpace(id) != id {
			return types.ThreadDisplayPreference{}, CodedError{Code: "INVALID_ARGUMENT", Message: "Invalid message ID in display preferences."}
		}
	}
	if preference.MessagePreferences == nil {
		preference.MessagePreferences = map[string]bool{}
	}
	if err := s.repo.SaveThreadDisplayPreference(ctx, auth.UserID, threadID, preference); err != nil {
		return types.ThreadDisplayPreference{}, err
	}
	return preference, nil
}
