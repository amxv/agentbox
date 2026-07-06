package profiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type Profile struct {
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	TenantID   string `json:"tenant_id,omitempty"`
	TenantSlug string `json:"tenant_slug,omitempty"`
	TenantName string `json:"tenant_name,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	KeyName    string `json:"key_name,omitempty"`
	AuthType   string `json:"auth_type,omitempty"`
}

type Store struct {
	ActiveProfileName string
	Profiles          []Profile
}

type ResolvedProfile struct {
	Profile
	Source string `json:"source"`
}

func DefaultConfigDir() string {
	if value := strings.TrimSpace(os.Getenv("AGENTBOX_CONFIG_DIR")); value != "" {
		return value
	}
	if runtime.GOOS == "windows" {
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			return filepath.Join(appData, "agentbox")
		}
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "agentbox")
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "agentbox")
	}
	return filepath.Join(home, ".config", "agentbox")
}

func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "profiles.json")
}

func ParseProfilesConfig(raw string) ([]Profile, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &value); err != nil {
		return nil, err
	}
	return dedupeProfiles(parseProfilesRecord(value)), nil
}

func ReadStore() (Store, error) {
	bytes, err := os.ReadFile(DefaultConfigPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Store{}, nil
		}
		return Store{}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(bytes, &raw); err != nil {
		return Store{}, err
	}
	profiles := dedupeProfiles(parseProfilesRecord(raw["profiles"]))
	active := stringField(raw, "active_profile", "current_profile")
	if active != "" && !hasProfile(profiles, active) {
		active = ""
	}
	return Store{ActiveProfileName: active, Profiles: profiles}, nil
}

func SaveProfile(profile Profile, activate bool) (Store, error) {
	store, err := ReadStore()
	if err != nil {
		return Store{}, err
	}
	normalized, ok := normalizeProfile(profile.Name, map[string]any{
		"base_url":    profile.BaseURL,
		"api_key":     profile.APIKey,
		"tenant_id":   profile.TenantID,
		"tenant_slug": profile.TenantSlug,
		"tenant_name": profile.TenantName,
		"user_id":     profile.UserID,
		"key_name":    profile.KeyName,
		"auth_type":   profile.AuthType,
	})
	if !ok {
		normalized = profile
	}
	next := make([]Profile, 0, len(store.Profiles)+1)
	for _, existing := range store.Profiles {
		if existing.Name != normalized.Name {
			next = append(next, existing)
		}
	}
	next = append(next, normalized)
	active := store.ActiveProfileName
	if activate {
		active = normalized.Name
	}
	if err := writeStore(Store{ActiveProfileName: active, Profiles: next}); err != nil {
		return Store{}, err
	}
	return ReadStore()
}

func RemoveProfile(name string) (Store, error) {
	store, err := ReadStore()
	if err != nil {
		return Store{}, err
	}
	next := make([]Profile, 0, len(store.Profiles))
	for _, profile := range store.Profiles {
		if profile.Name != name {
			next = append(next, profile)
		}
	}
	if len(next) == len(store.Profiles) {
		return Store{}, fmt.Errorf("Unknown Agentbox profile %q.", name)
	}
	active := store.ActiveProfileName
	if active == name {
		active = ""
		if len(next) > 0 {
			active = next[0].Name
		}
	}
	if err := writeStore(Store{ActiveProfileName: active, Profiles: next}); err != nil {
		return Store{}, err
	}
	return ReadStore()
}

func SetActiveProfile(name string) (Store, error) {
	store, err := ReadStore()
	if err != nil {
		return Store{}, err
	}
	if !hasProfile(store.Profiles, name) {
		return Store{}, fmt.Errorf("Unknown Agentbox profile %q.", name)
	}
	if err := writeStore(Store{ActiveProfileName: name, Profiles: store.Profiles}); err != nil {
		return Store{}, err
	}
	return ReadStore()
}

func Resolve(selection string) (*ResolvedProfile, error) {
	envProfiles, err := ParseProfilesConfig(os.Getenv("AGENTBOX_PROFILES"))
	if err != nil {
		return nil, err
	}
	selected := selection
	if selected == "" {
		selected = os.Getenv("AGENTBOX_PROFILE")
	}
	if len(envProfiles) > 0 {
		if selected != "" {
			match := findProfile(envProfiles, selected)
			if match == nil {
				return nil, fmt.Errorf("Unknown Agentbox profile %q.", selected)
			}
			return &ResolvedProfile{Profile: *match, Source: "env"}, nil
		}
		return &ResolvedProfile{Profile: envProfiles[0], Source: "env"}, nil
	}
	store, err := ReadStore()
	if err != nil {
		return nil, err
	}
	if len(store.Profiles) > 0 {
		active := selected
		if active == "" {
			active = store.ActiveProfileName
		}
		if active == "" {
			active = store.Profiles[0].Name
		}
		match := findProfile(store.Profiles, active)
		if selected != "" && match == nil {
			return nil, fmt.Errorf("Unknown Agentbox profile %q.", selected)
		}
		if match != nil {
			return &ResolvedProfile{Profile: *match, Source: "config"}, nil
		}
	}
	legacyBaseURL := os.Getenv("AGENTBOX_BASE_URL")
	if legacyBaseURL == "" {
		legacyBaseURL = os.Getenv("AGENTBOX_URL")
	}
	legacyAPIKey := os.Getenv("AGENTBOX_API_KEY")
	if legacyBaseURL == "" || legacyAPIKey == "" {
		return nil, nil
	}
	if selected != "" && selected != "default" {
		return nil, fmt.Errorf("Unknown Agentbox profile %q.", selected)
	}
	return &ResolvedProfile{
		Profile: Profile{Name: defaultString(selected, "default"), BaseURL: trimURL(legacyBaseURL), APIKey: strings.TrimSpace(legacyAPIKey)},
		Source:  "legacy-env",
	}, nil
}

func MaskSecret(secret string) string {
	return MaskSecretWith(secret, 3, 2)
}

func MaskSecretWith(secret string, prefix int, suffix int) string {
	if len(secret) <= prefix+suffix {
		if len(secret) > 4 {
			return strings.Repeat("*", len(secret))
		}
		return "****"
	}
	return secret[:prefix] + strings.Repeat("*", len(secret)-prefix-suffix) + secret[len(secret)-suffix:]
}

func SanitizeURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	key := parsed.Query().Get("key")
	if key == "" {
		return parsed.String()
	}
	query := parsed.Query()
	query.Set("key", MaskSecret(key))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func writeStore(store Store) error {
	profiles := dedupeProfiles(store.Profiles)
	active := store.ActiveProfileName
	if active != "" && !hasProfile(profiles, active) {
		active = ""
	}
	if active == "" && len(profiles) > 0 {
		active = profiles[0].Name
	}
	var activeProfile *string
	if active != "" {
		activeProfile = &active
	}
	payload := struct {
		ActiveProfile *string                   `json:"active_profile"`
		Profiles      map[string]map[string]any `json:"profiles"`
	}{
		ActiveProfile: activeProfile,
		Profiles:      map[string]map[string]any{},
	}
	for _, profile := range profiles {
		record := map[string]any{
			"base_url": profile.BaseURL,
			"api_key":  profile.APIKey,
		}
		setOptionalField(record, "tenant_id", profile.TenantID)
		setOptionalField(record, "tenant_slug", profile.TenantSlug)
		setOptionalField(record, "tenant_name", profile.TenantName)
		setOptionalField(record, "user_id", profile.UserID)
		setOptionalField(record, "key_name", profile.KeyName)
		setOptionalField(record, "auth_type", profile.AuthType)
		payload.Profiles[profile.Name] = record
	}
	bytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	path := DefaultConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(bytes, '\n'), 0o600)
}

func parseProfilesRecord(value any) []Profile {
	switch typed := value.(type) {
	case []any:
		var profiles []Profile
		for _, entry := range typed {
			record, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			name, _ := record["name"].(string)
			if profile, ok := normalizeProfile(name, record); ok {
				profiles = append(profiles, profile)
			}
		}
		return profiles
	case map[string]any:
		var profiles []Profile
		for name, entry := range typed {
			record, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if profile, ok := normalizeProfile(name, record); ok {
				profiles = append(profiles, profile)
			}
		}
		return profiles
	default:
		return nil
	}
}

func normalizeProfile(name string, record map[string]any) (Profile, bool) {
	name = strings.TrimSpace(name)
	baseURL := stringField(record, "base_url", "baseUrl")
	apiKey := stringField(record, "api_key", "apiKey")
	if name == "" || baseURL == "" || apiKey == "" {
		return Profile{}, false
	}
	return Profile{
		Name:       name,
		BaseURL:    trimURL(baseURL),
		APIKey:     strings.TrimSpace(apiKey),
		TenantID:   strings.TrimSpace(stringField(record, "tenant_id", "tenantId")),
		TenantSlug: strings.TrimSpace(stringField(record, "tenant_slug", "tenantSlug")),
		TenantName: strings.TrimSpace(stringField(record, "tenant_name", "tenantName")),
		UserID:     strings.TrimSpace(stringField(record, "user_id", "userId")),
		KeyName:    strings.TrimSpace(stringField(record, "key_name", "keyName")),
		AuthType:   strings.TrimSpace(stringField(record, "auth_type", "authType")),
	}, true
}

func setOptionalField(record map[string]any, name string, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		record[name] = value
	}
}

func dedupeProfiles(profiles []Profile) []Profile {
	byName := map[string]Profile{}
	for _, profile := range profiles {
		byName[profile.Name] = profile
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]Profile, 0, len(names))
	for _, name := range names {
		result = append(result, byName[name])
	}
	return result
}

func stringField(record map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := record[name].(string); ok {
			return value
		}
	}
	return ""
}

func hasProfile(profiles []Profile, name string) bool {
	return findProfile(profiles, name) != nil
}

func findProfile(profiles []Profile, name string) *Profile {
	for i := range profiles {
		if profiles[i].Name == name {
			return &profiles[i]
		}
	}
	return nil
}

func trimURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
