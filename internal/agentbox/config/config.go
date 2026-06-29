package config

import (
	"os"
	"strconv"
	"strings"
)

const (
	DefaultDBPoolSize       = int32(3)
	DefaultMaxFileSizeBytes = int64(25 * 1024 * 1024)
	VercelMaxPayloadBytes   = int64(4_500_000)
)

type Config struct {
	DatabaseURL         string
	DBPoolSize          int32
	APIKeys             string
	AllowedOrigins      []string
	AdminKeys           string
	AdminKey            string
	R2AccountID         string
	R2AccessKeyID       string
	R2SecretAccessKey   string
	R2Bucket            string
	R2PublicBaseURL     string
	MaxFileSizeBytes    int64
	MultipartLimitBytes int64
	Environment         string
	VercelEnvironment   string
	AutoMigrate         bool
}

func LoadFromEnv() Config {
	maxFileSize := int64FromEnv("AGENTBOX_MAX_FILE_SIZE_BYTES", DefaultMaxFileSizeBytes)
	return Config{
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		DBPoolSize:          int32FromEnv("AGENTBOX_DB_POOL_SIZE", DefaultDBPoolSize),
		APIKeys:             os.Getenv("AGENTBOX_API_KEYS"),
		AllowedOrigins:      commaList(os.Getenv("AGENTBOX_ALLOWED_ORIGINS")),
		AdminKeys:           os.Getenv("AGENTBOX_ADMIN_KEYS"),
		AdminKey:            os.Getenv("AGENTBOX_ADMIN_KEY"),
		R2AccountID:         os.Getenv("R2_ACCOUNT_ID"),
		R2AccessKeyID:       os.Getenv("R2_ACCESS_KEY_ID"),
		R2SecretAccessKey:   os.Getenv("R2_SECRET_ACCESS_KEY"),
		R2Bucket:            os.Getenv("R2_BUCKET"),
		R2PublicBaseURL:     strings.TrimRight(os.Getenv("R2_PUBLIC_BASE_URL"), "/"),
		MaxFileSizeBytes:    maxFileSize,
		MultipartLimitBytes: multipartLimit(maxFileSize),
		Environment:         firstNonEmpty(os.Getenv("AGENTBOX_ENV"), os.Getenv("NODE_ENV")),
		VercelEnvironment:   os.Getenv("VERCEL_ENV"),
		AutoMigrate:         truthy(os.Getenv("AGENTBOX_AUTO_MIGRATE")),
	}
}

func (c Config) IsProduction() bool {
	return c.Environment == "production" || c.VercelEnvironment == "production"
}

func (c Config) IsVercel() bool {
	return os.Getenv("VERCEL") == "1" || c.VercelEnvironment != ""
}

func multipartLimit(maxFileSize int64) int64 {
	if os.Getenv("VERCEL") == "1" || os.Getenv("VERCEL_ENV") != "" {
		if maxFileSize > VercelMaxPayloadBytes {
			return VercelMaxPayloadBytes
		}
	}
	return maxFileSize
}

func commaList(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func int32FromEnv(name string, fallback int32) int32 {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return int32(value)
}

func int64FromEnv(name string, fallback int64) int64 {
	value, err := strconv.ParseInt(os.Getenv(name), 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
