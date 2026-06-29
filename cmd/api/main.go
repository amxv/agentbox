package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/httpapi"
	"agentbox/internal/agentbox/service"
)

func main() {
	cfg := config.LoadFromEnv()
	if err := validateRuntimeConfig(cfg); err != nil {
		log.Fatal(err)
	}

	opened, err := openRepository(context.Background(), cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer opened.Close()

	assetStore, err := assets.NewR2Store(context.Background(), cfg)
	if err != nil {
		log.Fatal(err)
	}

	if cfg.AutoMigrate {
		if err := opened.EnsureSchema(context.Background()); err != nil {
			log.Fatal(err)
		}
	}

	svc := service.New(opened, assetStore)
	listen(httpapi.NewServer(cfg, svc))
}

func listen(handler http.Handler) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Fatal(http.ListenAndServe(":"+port, handler))
}

func validateRuntimeConfig(cfg config.Config) error {
	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.R2AccountID == "" {
		missing = append(missing, "R2_ACCOUNT_ID")
	}
	if cfg.R2AccessKeyID == "" {
		missing = append(missing, "R2_ACCESS_KEY_ID")
	}
	if cfg.R2SecretAccessKey == "" {
		missing = append(missing, "R2_SECRET_ACCESS_KEY")
	}
	if cfg.R2Bucket == "" {
		missing = append(missing, "R2_BUCKET")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing required backend configuration: %v", missing)
}

type repositoryWithClose interface {
	service.Repository
	Close()
	EnsureSchema(context.Context) error
}

func openRepository(ctx context.Context, cfg config.Config) (repositoryWithClose, error) {
	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	return db.Open(ctx, cfg)
}
