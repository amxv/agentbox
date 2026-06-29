package main

import (
	"context"
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
	repo := &db.MemoryRepository{}
	var assetStore assets.AssetStore = &assets.FakeStore{MaxFileSizeBytes: cfg.MaxFileSizeBytes, PublicBaseURL: cfg.R2PublicBaseURL}
	if cfg.R2AccountID != "" || cfg.R2AccessKeyID != "" || cfg.R2SecretAccessKey != "" {
		store, err := assets.NewR2Store(context.Background(), cfg)
		if err != nil {
			log.Fatal(err)
		}
		assetStore = store
	}

	if cfg.DatabaseURL != "" {
		opened, err := db.Open(context.Background(), cfg)
		if err != nil {
			log.Fatal(err)
		}
		defer opened.Close()
		if cfg.AutoMigrate {
			if err := opened.EnsureSchema(context.Background()); err != nil {
				log.Fatal(err)
			}
		}
		svc := service.New(opened, assetStore)
		listen(httpapi.NewServer(cfg, svc))
		return
	}

	svc := service.New(repo, assetStore)
	listen(httpapi.NewServer(cfg, svc))
}

func listen(handler http.Handler) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Fatal(http.ListenAndServe(":"+port, handler))
}
