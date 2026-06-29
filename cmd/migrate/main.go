package main

import (
	"context"
	"log"
	"time"

	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := db.Open(ctx, config.LoadFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()

	if err := repo.EnsureSchema(ctx); err != nil {
		log.Fatal(err)
	}
	log.Println("Agentbox schema is ready.")
}
