package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agentbox/internal/agentbox/assets"
	authpkg "agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/backup"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/types"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type deliberatelySlowAssetStore struct {
	*assets.FakeStore
	delay     time.Duration
	headCalls atomic.Int32
}

func (s *deliberatelySlowAssetStore) HeadAssetObject(ctx context.Context, storageKey string) (backup.ObjectMetadata, error) {
	s.headCalls.Add(1)
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return backup.ObjectMetadata{}, ctx.Err()
	}
	return s.FakeStore.HeadAssetObject(ctx, storageKey)
}

func TestAnonymousPublicReadsDoNotStarveSmallPostgresPoolOnSlowAssetStore(t *testing.T) {
	repository, cleanup := openSmallPoolServiceTestRepository(t, 2)
	defer cleanup()
	ctx := context.Background()
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	password := "public-pool-password"
	passwordHash, err := authpkg.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "public-pool-owner@example.invalid", "Public Pool Owner", passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	ownerAuth := types.AuthContext{
		UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession,
		SessionID: "sess_public_pool_owner", ActorID: "sess_public_pool_owner", ActorName: "Web dashboard", IsOwner: true,
	}
	store := &deliberatelySlowAssetStore{FakeStore: &assets.FakeStore{}, delay: 2 * time.Second}
	svc := New(repository, store)
	thread, err := svc.CreateThread(ctx, ownerAuth, "Public pool isolation")
	if err != nil {
		t.Fatal(err)
	}
	mimeType := "image/png"
	newAssets := make([]types.NewAsset, 0, 6)
	for index := 0; index < 6; index++ {
		digest := strings.Repeat(fmt.Sprintf("%x", index+1), 64)
		newAssets = append(newAssets, types.NewAsset{
			StorageKey: "agentbox/final/sha256/" + digest + "/" + owner.ID + "/" + thread.ID + "/msg/asset.png",
			FileName:   fmt.Sprintf("asset-%d.png", index), MimeType: &mimeType, SizeBytes: int64(index + 1), ContentSHA256: digest,
		})
	}
	if _, err := repository.PostMessage(ctx, owner.ID, thread.ID, ownerAuth, "many public assets", nil, newAssets); err != nil {
		t.Fatal(err)
	}
	publish := true
	visibility, err := svc.ManageThreadVisibility(ctx, ownerAuth, thread.ID, "https://dashboard.example", types.ManageThreadVisibilityInput{Public: &publish})
	if err != nil {
		t.Fatal(err)
	}
	if visibility.PublicLink == nil || strings.TrimSpace(visibility.PublicLink.Token) == "" {
		t.Fatalf("public visibility=%#v", visibility)
	}
	publicToken := visibility.PublicLink.Token

	const publicReaders = 8
	start := make(chan struct{})
	readerErrors := make(chan error, publicReaders)
	var readers sync.WaitGroup
	for index := 0; index < publicReaders; index++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			view, err := svc.GetPublicThread(readCtx, publicToken)
			if err == nil && (view == nil || len(view.Messages) != 1 || len(view.Messages[0].Assets) != len(newAssets)) {
				err = fmt.Errorf("unexpected public view: %#v", view)
			}
			readerErrors <- err
		}()
	}
	close(start)
	// On the reviewed implementation, two readers occupy the entire pool and
	// then block on the deliberately slow first HEAD. The fixed implementation
	// closes each snapshot and performs no eager HEAD before this point.
	time.Sleep(100 * time.Millisecond)

	type operation struct {
		name string
		run  func(context.Context) error
	}
	operations := []operation{
		{name: "login", run: func(opCtx context.Context) error {
			_, _, err := svc.Login(opCtx, "", owner.Email, password)
			return err
		}},
		{name: "inbox", run: func(opCtx context.Context) error {
			_, err := svc.ListThreads(opCtx, ownerAuth, 20)
			return err
		}},
		{name: "visibility", run: func(opCtx context.Context) error {
			_, err := svc.ManageThreadVisibility(opCtx, ownerAuth, thread.ID, "https://dashboard.example", types.ManageThreadVisibilityInput{})
			return err
		}},
		{name: "invitation", run: func(opCtx context.Context) error {
			_, err := svc.ListSignupInvitationsPage(opCtx, ownerAuth, types.PageRequest{Limit: 10})
			return err
		}},
		{name: "owner users", run: func(opCtx context.Context) error {
			_, err := svc.ListUsersPage(opCtx, ownerAuth, types.PageRequest{Limit: 10})
			return err
		}},
	}
	type operationResult struct {
		name string
		err  error
	}
	operationResults := make(chan operationResult, len(operations))
	for _, operation := range operations {
		operation := operation
		go func() {
			opCtx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			operationResults <- operationResult{name: operation.name, err: operation.run(opCtx)}
		}()
	}
	for range operations {
		result := <-operationResults
		if result.err != nil {
			t.Fatalf("%s operation starved behind anonymous public reads: %v", result.name, result.err)
		}
	}

	readers.Wait()
	close(readerErrors)
	for err := range readerErrors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if store.headCalls.Load() != 0 {
		t.Fatalf("anonymous public payload made %d eager R2 HEAD requests", store.headCalls.Load())
	}
}

func openSmallPoolServiceTestRepository(t *testing.T, poolSize int32) (*db.Repository, func()) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		if strings.TrimSpace(os.Getenv("AGENTBOX_REQUIRE_POSTGRES_TESTS")) == "1" {
			t.Fatal("TEST_DATABASE_URL is required because AGENTBOX_REQUIRE_POSTGRES_TESTS=1")
		}
		t.Skip("TEST_DATABASE_URL is not set; PostgreSQL integration test runs in CI")
	}
	ctx := context.Background()
	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	extensionTx, err := adminPool.Begin(ctx)
	if err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	if _, err := extensionTx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('agentbox-test-pgcrypto', 0))`); err != nil {
		_ = extensionTx.Rollback(ctx)
		adminPool.Close()
		t.Fatal(err)
	}
	if _, err := extensionTx.Exec(ctx, `create extension if not exists pgcrypto with schema public`); err != nil {
		_ = extensionTx.Rollback(ctx)
		adminPool.Close()
		t.Fatal(err)
	}
	var pgcryptoSchema string
	err = extensionTx.QueryRow(ctx, `
select n.nspname
from pg_extension e
join pg_namespace n on n.oid = e.extnamespace
where e.extname = 'pgcrypto'
`).Scan(&pgcryptoSchema)
	switch {
	case err == pgx.ErrNoRows:
		_ = extensionTx.Rollback(ctx)
		adminPool.Close()
		t.Fatal("pgcrypto was not created")
	case err != nil:
		_ = extensionTx.Rollback(ctx)
		adminPool.Close()
		t.Fatal(err)
	case pgcryptoSchema != "public":
		if _, err := extensionTx.Exec(ctx, `alter extension pgcrypto set schema public`); err != nil {
			_ = extensionTx.Rollback(ctx)
			adminPool.Close()
			t.Fatal(err)
		}
	}
	if err := extensionTx.Commit(ctx); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	schemaName := "agentbox_public_pool_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminPool.Exec(ctx, `create schema `+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("options", "-csearch_path="+schemaName+",public")
	parsed.RawQuery = query.Encode()
	repository, err := db.Open(ctx, config.Config{DatabaseURL: parsed.String(), DBPoolSize: poolSize})
	if err != nil {
		_, _ = adminPool.Exec(ctx, `drop schema if exists `+pgx.Identifier{schemaName}.Sanitize()+` cascade`)
		adminPool.Close()
		t.Fatal(err)
	}
	cleanup := func() {
		repository.Close()
		_, _ = adminPool.Exec(context.Background(), `drop schema if exists `+pgx.Identifier{schemaName}.Sanitize()+` cascade`)
		adminPool.Close()
	}
	return repository, cleanup
}
