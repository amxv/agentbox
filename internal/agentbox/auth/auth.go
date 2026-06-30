package auth

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"agentbox/internal/agentbox/config"
)

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
	adminKey := strings.TrimSpace(cfg.AdminKey)
	if adminKey == "" {
		return errors.New("AGENTBOX_ADMIN_KEY is required for admin API requests.")
	}
	if provided != "" && safeEqual(adminKey, provided) {
		return nil
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
