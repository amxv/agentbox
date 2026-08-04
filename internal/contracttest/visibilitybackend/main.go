package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/httpapi"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
)

type fixture struct {
	BackendURL string `json:"backend_url"`
	APIKey     string `json:"api_key"`
	ThreadID   string `json:"thread_id"`
	TeamA      string `json:"team_a"`
	TeamB      string `json:"team_b"`
}

func main() {
	ctx := context.Background()
	repo := &db.MemoryRepository{}
	svc := service.New(repo, &assets.FakeStore{})

	now := time.Now().UTC().Format(time.RFC3339Nano)
	owner := types.User{
		ID:          "usr_visibility_proxy",
		Email:       "visibility-proxy@example.invalid",
		DisplayName: "Visibility Proxy",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	repo.Users = append(repo.Users, owner)
	auth := types.AuthContext{
		UserID:          owner.ID,
		UserDisplayName: owner.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		ActorID:         "session_visibility_proxy",
		ActorName:       "Web dashboard",
		SessionID:       "session_visibility_proxy",
	}

	teamA, err := repo.CreateTeam(ctx, "proxy-alpha", "Proxy Alpha")
	check(err)
	teamB, err := repo.CreateTeam(ctx, "proxy-beta", "Proxy Beta")
	check(err)
	_, err = repo.AddTeamMember(ctx, teamA.ID, owner.ID)
	check(err)
	_, err = repo.AddTeamMember(ctx, teamB.ID, owner.ID)
	check(err)
	thread, err := svc.CreateThread(ctx, auth, "Visibility proxy contract")
	check(err)
	credential, err := svc.CreateAPIKeyWithPurposeAndScopes(ctx, auth, "visibility-proxy", "custom", []string{"threads:read", "threads:write"})
	check(err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	check(err)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Forwarded-Host") != "dashboard.example" || r.Header.Get("X-Forwarded-Proto") != "https" || r.Header.Get("Forwarded") != "" {
				http.Error(w, "dashboard proxy did not replace forwarded headers", http.StatusBadRequest)
				return
			}
			httpapi.NewServer(config.Config{AppPublicURL: "https://dashboard.example"}, svc).ServeHTTP(w, r)
		}),
	}
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, serveErr)
			os.Exit(1)
		}
	}()

	check(json.NewEncoder(os.Stdout).Encode(fixture{
		BackendURL: "http://" + listener.Addr().String(),
		APIKey:     credential.Key,
		ThreadID:   thread.ID,
		TeamA:      teamA.ID,
		TeamB:      teamB.ID,
	}))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	check(server.Shutdown(shutdownCtx))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
