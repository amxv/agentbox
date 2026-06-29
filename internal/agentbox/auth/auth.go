package auth

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/types"
)

type KeyConfig struct {
	Name   string `json:"name"`
	Key    string `json:"key"`
	Author string `json:"author"`
}

type AdminKeyConfig struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

func ParseAPIKeys(raw string) ([]KeyConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	if strings.HasPrefix(raw, "[") {
		var parsed []KeyConfig
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return nil, err
		}
		keys := parsed[:0]
		for _, entry := range parsed {
			if entry.Name != "" && entry.Key != "" && entry.Author != "" {
				keys = append(keys, entry)
			}
		}
		return keys, nil
	}

	parts := strings.Split(raw, ",")
	keys := make([]KeyConfig, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, ":")
		name := firstField(fields, 0)
		key := firstField(fields, 1)
		author := firstField(fields, 2)
		if author == "" {
			author = name
		}
		if name != "" && key != "" && author != "" {
			keys = append(keys, KeyConfig{Name: name, Key: key, Author: author})
		}
	}
	return keys, nil
}

func ParseAdminKeys(raw string, legacy string) ([]AdminKeyConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if legacy == "" {
			return nil, nil
		}
		return []AdminKeyConfig{{Name: "default", Key: legacy}}, nil
	}

	if strings.HasPrefix(raw, "[") {
		var parsed []AdminKeyConfig
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return nil, err
		}
		keys := parsed[:0]
		for _, entry := range parsed {
			if entry.Name != "" && entry.Key != "" {
				keys = append(keys, entry)
			}
		}
		return keys, nil
	}

	parts := strings.Split(raw, ",")
	keys := make([]AdminKeyConfig, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, ":")
		name := firstField(fields, 0)
		key := firstField(fields, 1)
		if name != "" && key != "" {
			keys = append(keys, AdminKeyConfig{Name: name, Key: key})
		}
	}
	return keys, nil
}

func AuthenticateRequest(r *http.Request, cfg config.Config) (*types.Actor, error) {
	keys, err := ParseAPIKeys(cfg.APIKeys)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 && !cfg.IsProduction() {
		return &types.Actor{Name: "local-dev", KeyName: "local-dev"}, nil
	}

	token := r.URL.Query().Get("key")
	if token == "" {
		return nil, nil
	}
	for _, entry := range keys {
		if safeEqual(entry.Key, token) {
			return &types.Actor{Name: entry.Author, KeyName: entry.Name}, nil
		}
	}
	return nil, nil
}

func RequireAdminRequest(r *http.Request, cfg config.Config) error {
	headerKey := r.Header.Get("x-agentbox-admin-key")
	bearer := strings.TrimSpace(r.Header.Get("authorization"))
	if strings.HasPrefix(strings.ToLower(bearer), "bearer ") {
		bearer = strings.TrimSpace(bearer[len("Bearer "):])
	} else {
		bearer = ""
	}
	if headerKey != "" {
		return RequireAdminKey(headerKey, cfg)
	}
	return RequireAdminKey(bearer, cfg)
}

func RequireAdminKey(provided string, cfg config.Config) error {
	keys, err := ParseAdminKeys(cfg.AdminKeys, cfg.AdminKey)
	if err != nil {
		return err
	}
	if len(keys) == 0 && !cfg.IsProduction() {
		return nil
	}
	if len(keys) == 0 {
		return errors.New("AGENTBOX_ADMIN_KEY or AGENTBOX_ADMIN_KEYS is required for the web thread viewer.")
	}
	for _, entry := range keys {
		if provided != "" && safeEqual(entry.Key, provided) {
			return nil
		}
	}
	return errors.New("Unauthorized")
}

func ValidateOrigin(r *http.Request, cfg config.Config) bool {
	if len(cfg.AllowedOrigins) == 0 {
		return true
	}
	origin := r.Header.Get("origin")
	if origin == "" {
		return true
	}
	for _, allowed := range cfg.AllowedOrigins {
		if allowed == origin {
			return true
		}
	}
	return false
}

func safeEqual(a string, b string) bool {
	left := []byte(a)
	right := []byte(b)
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare(left, right) == 1
}

func firstField(fields []string, index int) string {
	if index >= len(fields) {
		return ""
	}
	return fields[index]
}
