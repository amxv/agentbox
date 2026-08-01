package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/types"
	migrationfiles "agentbox/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrateLegacySchemaIsIdempotentAndPreservesContent(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	migrations, err := migrationfiles.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:4] {
		if _, err := repository.pool.Exec(ctx, migration.SQL); err != nil {
			t.Fatalf("apply legacy migration %s: %v", migration.Name, err)
		}
	}

	legacyStatements := []string{
		`insert into threads (id, title, created_by) values ('thr_legacy', 'Legacy thread', 'Legacy owner')`,
		`insert into messages (id, thread_id, author, body, body_content_type) values ('msg_legacy', 'thr_legacy', 'Legacy agent', 'Preserve this body', 'text/markdown')`,
		`insert into assets (id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_by) values ('ast_legacy', 'msg_legacy', 'agentbox/legacy/key.bin', 'key.bin', 'application/octet-stream', 9, null, 'Legacy agent')`,
		`insert into pending_uploads (id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url, expires_at, created_by) values ('upl_legacy', 'thr_legacy', 'agentbox/legacy/pending.bin', 'pending.bin', 'application/octet-stream', 11, null, now() + interval '1 hour', 'Legacy agent')`,
		`insert into api_keys (name, key_value) values ('legacy', 'legacy-secret')`,
	}
	for _, statement := range legacyStatements {
		if _, err := repository.pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}
	if _, err := repository.pool.Exec(ctx, migrations[4].SQL); err != nil {
		t.Fatalf("apply legacy auth migration %s: %v", migrations[4].Name, err)
	}
	legacyAuthStatements := []string{
		`insert into users (id, tenant_id, email, display_name, password_hash, role) values ('usr_legacy', 'ten_default', 'legacy@example.com', 'Legacy User', 'hash', 'admin')`,
		`insert into user_sessions (id, tenant_id, user_id, secret_hash, expires_at) values ('sess_legacy', 'ten_default', 'usr_legacy', 'session-hash', now() + interval '1 hour')`,
		`insert into cli_login_codes (id, tenant_id, user_id, code_hash, state_hash, redirect_uri, expires_at) values ('code_legacy', 'ten_default', 'usr_legacy', 'code-hash', 'state-hash', 'http://127.0.0.1:8080/callback', now() + interval '1 hour')`,
	}
	for _, statement := range legacyAuthStatements {
		if _, err := repository.pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seed disposable legacy auth: %v", err)
		}
	}

	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var migrationCount int
	var firstLedgerSnapshot string
	if err := repository.pool.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != len(migrations) {
		t.Fatalf("schema_migrations count = %d, want %d", migrationCount, len(migrations))
	}
	if err := repository.pool.QueryRow(ctx, `
select string_agg(version || ':' || name || ':' || checksum || ':' || applied_at::text, ',' order by version)
from schema_migrations
`).Scan(&firstLedgerSnapshot); err != nil {
		t.Fatal(err)
	}

	var threadTitle string
	var threadTenant string
	if err := repository.pool.QueryRow(ctx, `select title, tenant_id from threads where id = 'thr_legacy'`).Scan(&threadTitle, &threadTenant); err != nil {
		t.Fatal(err)
	}
	if threadTitle != "Legacy thread" || threadTenant != "ten_default" {
		t.Fatalf("legacy thread changed: title=%q tenant=%q", threadTitle, threadTenant)
	}

	var messageBody string
	var storageKey string
	var pendingStorageKey string
	if err := repository.pool.QueryRow(ctx, `select body from messages where id = 'msg_legacy'`).Scan(&messageBody); err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, `select storage_key from assets where id = 'ast_legacy'`).Scan(&storageKey); err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, `select storage_key from pending_uploads where id = 'upl_legacy'`).Scan(&pendingStorageKey); err != nil {
		t.Fatal(err)
	}
	if messageBody != "Preserve this body" || storageKey != "agentbox/legacy/key.bin" || pendingStorageKey != "agentbox/legacy/pending.bin" {
		t.Fatalf("legacy content changed: body=%q asset=%q pending=%q", messageBody, storageKey, pendingStorageKey)
	}

	var usersCount int
	var sessionsCount int
	var codesCount int
	var activeKeyCount int
	if err := repository.pool.QueryRow(ctx, `
select
  (select count(*) from users),
  (select count(*) from user_sessions),
  (select count(*) from cli_login_codes),
  (select count(*) from api_keys)
`).Scan(&usersCount, &sessionsCount, &codesCount, &activeKeyCount); err != nil {
		t.Fatal(err)
	}
	if usersCount != 0 || sessionsCount != 0 || codesCount != 0 || activeKeyCount != 0 {
		t.Fatalf("legacy authentication data was retained: users=%d sessions=%d codes=%d api_keys=%d", usersCount, sessionsCount, codesCount, activeKeyCount)
	}

	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Deployment Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	var legacyOwnerUserID string
	var legacyCreatedBy string
	if err := repository.pool.QueryRow(ctx, `
select owner_user_id, created_by
from threads
where id = 'thr_legacy'
`).Scan(&legacyOwnerUserID, &legacyCreatedBy); err != nil {
		t.Fatal(err)
	}
	if legacyOwnerUserID != owner.ID || legacyCreatedBy != "Legacy owner" {
		t.Fatalf("legacy ownership backfill changed attribution: owner=%q want=%q created_by=%q", legacyOwnerUserID, owner.ID, legacyCreatedBy)
	}

	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var secondLedgerSnapshot string
	if err := repository.pool.QueryRow(ctx, `
select string_agg(version || ':' || name || ':' || checksum || ':' || applied_at::text, ',' order by version)
from schema_migrations
`).Scan(&secondLedgerSnapshot); err != nil {
		t.Fatal(err)
	}
	if secondLedgerSnapshot != firstLedgerSnapshot {
		t.Fatalf("migration retry changed ledger\nfirst:  %s\nsecond: %s", firstLedgerSnapshot, secondLedgerSnapshot)
	}
}

func TestBootstrapOwnerIsUniqueIdempotentAndProtected(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	first, err := repository.BootstrapOwner(ctx, "owner@example.com", "Original Owner", "hash-one")
	if err != nil {
		t.Fatal(err)
	}
	if !first.IsOwner || first.Role != "admin" || first.TenantID != types.DefaultTenantID {
		t.Fatalf("unexpected owner: %#v", first)
	}

	second, err := repository.BootstrapOwner(ctx, "OWNER@example.com", "Updated Owner", "hash-two")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.DisplayName != "Updated Owner" || second.PasswordHash == nil || *second.PasswordHash != "hash-two" {
		t.Fatalf("owner bootstrap was not idempotent: first=%#v second=%#v", first, second)
	}

	if _, err := repository.BootstrapOwner(ctx, "other@example.com", "Other", "hash-three"); !errors.Is(err, ErrOwnerAlreadyExists) {
		t.Fatalf("second owner error = %v, want ErrOwnerAlreadyExists", err)
	}

	var ownerCount int
	if err := repository.pool.QueryRow(ctx, `select count(*) from users where is_owner`).Scan(&ownerCount); err != nil {
		t.Fatal(err)
	}
	if ownerCount != 1 {
		t.Fatalf("owner count = %d, want 1", ownerCount)
	}

	for name, statement := range map[string]string{
		"demote":  `update users set is_owner = false where id = $1`,
		"disable": `update users set disabled_at = now() where id = $1`,
		"role":    `update users set role = 'member' where id = $1`,
		"delete":  `delete from users where id = $1`,
	} {
		if _, err := repository.pool.Exec(ctx, statement, first.ID); err == nil {
			t.Fatalf("%s owner mutation unexpectedly succeeded", name)
		}
	}

	if _, err := repository.UpsertTenant(ctx, types.Tenant{ID: "ten_other", Slug: "other", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpsertProvisionedUser(ctx, "ten_other", "owner@example.com", "Duplicate", nil, "member"); err == nil {
		t.Fatal("deployment-global email uniqueness was not enforced")
	}
}

func TestUserOwnedCredentialsAreIsolatedRotatableAndDisableAware(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repository.UpsertProvisionedUser(ctx, types.DefaultTenantID, "member@example.com", "Member", nil, "member")
	if err != nil {
		t.Fatal(err)
	}

	ownerFirstSecret := "agb_owner_first"
	ownerFirst, err := repository.CreateAPIKey(ctx, owner.ID, "chatgpt", "chatgpt", hashSecret(ownerFirstSecret), "agb_owner_f", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	memberSecret := "agb_member"
	memberKey, err := repository.CreateAPIKey(ctx, member.ID, "chatgpt", "chatgpt", hashSecret(memberSecret), "agb_member", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	if ownerFirst.ID == memberKey.ID || ownerFirst.UserID != owner.ID || memberKey.UserID != member.ID {
		t.Fatalf("same-label credentials were not user-scoped: owner=%#v member=%#v", ownerFirst, memberKey)
	}

	ownerSecondSecret := "agb_owner_second"
	ownerSecond, err := repository.CreateAPIKey(ctx, owner.ID, "CHATGPT", "chatgpt", hashSecret(ownerSecondSecret), "agb_owner_s", []string{"threads:read", "threads:write"})
	if err != nil {
		t.Fatal(err)
	}
	if ownerSecond.ID != ownerFirst.ID || ownerSecond.TokenHash == ownerFirst.TokenHash || ownerSecond.Purpose != "chatgpt" {
		t.Fatalf("owner rotation did not update the existing credential: first=%#v second=%#v", ownerFirst, ownerSecond)
	}

	ownerKeys, err := repository.ListAPIKeys(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	memberKeys, err := repository.ListAPIKeys(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerKeys) != 1 || ownerKeys[0].ID != ownerSecond.ID || len(memberKeys) != 1 || memberKeys[0].ID != memberKey.ID {
		t.Fatalf("credential lists crossed users: owner=%#v member=%#v", ownerKeys, memberKeys)
	}

	oldOwnerKey, oldOwnerUser, err := repository.FindAPIKeyBySecret(ctx, ownerFirstSecret)
	if err != nil {
		t.Fatal(err)
	}
	if oldOwnerKey != nil || oldOwnerUser != nil {
		t.Fatalf("rotated secret still authenticated: key=%#v user=%#v", oldOwnerKey, oldOwnerUser)
	}
	activeOwnerKey, activeOwnerUser, err := repository.FindAPIKeyBySecret(ctx, ownerSecondSecret)
	if err != nil {
		t.Fatal(err)
	}
	if activeOwnerKey == nil || activeOwnerUser == nil || activeOwnerUser.ID != owner.ID {
		t.Fatalf("rotated owner secret did not resolve owner: key=%#v user=%#v", activeOwnerKey, activeOwnerUser)
	}
	activeMemberKey, activeMemberUser, err := repository.FindAPIKeyBySecret(ctx, memberSecret)
	if err != nil {
		t.Fatal(err)
	}
	if activeMemberKey == nil || activeMemberUser == nil || activeMemberUser.ID != member.ID {
		t.Fatalf("member secret did not resolve member: key=%#v user=%#v", activeMemberKey, activeMemberUser)
	}

	if removed, err := repository.RevokeAPIKey(ctx, owner.ID, "chatgpt"); err != nil || !removed {
		t.Fatalf("revoke owner credential: removed=%t err=%v", removed, err)
	}
	if key, user, err := repository.FindAPIKeyBySecret(ctx, ownerSecondSecret); err != nil || key != nil || user != nil {
		t.Fatalf("revoked owner credential authenticated: key=%#v user=%#v err=%v", key, user, err)
	}
	if key, user, err := repository.FindAPIKeyBySecret(ctx, memberSecret); err != nil || key == nil || user == nil {
		t.Fatalf("owner revoke affected member credential: key=%#v user=%#v err=%v", key, user, err)
	}

	if _, err := repository.pool.Exec(ctx, `update users set disabled_at = now() where id = $1`, member.ID); err != nil {
		t.Fatal(err)
	}
	if key, user, err := repository.FindAPIKeyBySecret(ctx, memberSecret); err != nil || key != nil || user != nil {
		t.Fatalf("disabled user credential authenticated: key=%#v user=%#v err=%v", key, user, err)
	}
}

func TestUserOwnedPrivateThreadAccessUsesOneIndexedBoundary(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner Person", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repository.UpsertProvisionedUser(ctx, types.DefaultTenantID, "member@example.com", "Member Person", nil, "member")
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, err := repository.CreateAPIKey(ctx, owner.ID, "chatgpt", "chatgpt", hashSecret("owner-secret"), "agb_owner", []string{"threads:read", "threads:write", "assets:read", "assets:write"})
	if err != nil {
		t.Fatal(err)
	}

	ownerBrowser := types.AuthContext{
		UserID:          owner.ID,
		UserDisplayName: owner.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		ActorName:       "Web dashboard",
	}
	ownerConnector := types.AuthContext{
		UserID:          owner.ID,
		UserDisplayName: owner.DisplayName,
		SubjectType:     types.AuthSubjectAPIKey,
		ActorName:       ownerKey.Name,
		KeyID:           ownerKey.ID,
	}
	memberBrowser := types.AuthContext{
		UserID:          member.ID,
		UserDisplayName: member.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		ActorName:       "Web dashboard",
	}

	ownerThread, err := repository.CreateThread(ctx, owner.ID, "private marker owner", ownerBrowser)
	if err != nil {
		t.Fatal(err)
	}
	memberThread, err := repository.CreateThread(ctx, member.ID, "private marker member", memberBrowser)
	if err != nil {
		t.Fatal(err)
	}
	if ownerThread.OwnerUserID != owner.ID || ownerThread.CreatedByUserDisplayName == nil || *ownerThread.CreatedByUserDisplayName != owner.DisplayName || ownerThread.CreatedByActorName == nil || *ownerThread.CreatedByActorName != "Web dashboard" {
		t.Fatalf("owner thread metadata = %#v", ownerThread)
	}

	ownerThreads, err := repository.ListThreads(ctx, owner.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	memberThreads, err := repository.ListThreads(ctx, member.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerThreads) != 1 || ownerThreads[0].ID != ownerThread.ID || len(memberThreads) != 1 || memberThreads[0].ID != memberThread.ID {
		t.Fatalf("private lists crossed users: owner=%#v member=%#v", ownerThreads, memberThreads)
	}

	ownerSearch, err := repository.SearchThreads(ctx, owner.ID, types.SearchThreadParams{Query: "private marker", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerSearch) != 1 || ownerSearch[0].ID != ownerThread.ID || ownerSearch[0].OwnerUserID != owner.ID {
		t.Fatalf("private search crossed users: %#v", ownerSearch)
	}
	if access, err := repository.ResolveThreadAccess(ctx, member.ID, ownerThread.ID); err != nil || access != nil {
		t.Fatalf("member resolved owner access: access=%#v err=%v", access, err)
	}
	if thread, err := repository.GetThread(ctx, member.ID, ownerThread.ID); err != nil || thread != nil {
		t.Fatalf("member read owner thread: thread=%#v err=%v", thread, err)
	}
	if _, err := repository.PostMessage(ctx, member.ID, ownerThread.ID, memberBrowser, "blocked", nil, nil); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("member posted to owner thread: %v", err)
	}

	textType := "text/plain"
	message, err := repository.PostMessage(ctx, owner.ID, ownerThread.ID, ownerConnector, "connector contribution", nil, []types.NewAsset{{
		StorageKey: "agentbox/" + owner.ID + "/" + ownerThread.ID + "/message/existing.txt",
		FileName:   "existing.txt",
		MimeType:   &textType,
		SizeBytes:  8,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if message.CreatedByUserID == nil || *message.CreatedByUserID != owner.ID || message.CreatedByKeyID == nil || *message.CreatedByKeyID != ownerKey.ID || message.CreatedByUserDisplayName == nil || *message.CreatedByUserDisplayName != owner.DisplayName || message.CreatedByActorName == nil || *message.CreatedByActorName != ownerKey.Name || len(message.Assets) != 1 {
		t.Fatalf("connector attribution = %#v", message)
	}
	if message.Assets[0].PublicURL != nil || message.Assets[0].DownloadURL != nil {
		t.Fatalf("new private asset exposed a direct URL: %#v", message.Assets[0])
	}
	if asset, err := repository.GetAsset(ctx, member.ID, message.Assets[0].ID); err != nil || asset != nil {
		t.Fatalf("member read owner asset: asset=%#v err=%v", asset, err)
	}
	if asset, err := repository.GetAsset(ctx, owner.ID, message.Assets[0].ID); err != nil || asset == nil || asset.StorageKey != message.Assets[0].StorageKey {
		t.Fatalf("owner asset lookup failed: asset=%#v err=%v", asset, err)
	}

	expiresAt := time.Now().UTC().Add(15 * time.Minute)
	upload := types.PendingUpload{
		ID:                       "upl_owner_private",
		ThreadID:                 ownerThread.ID,
		StorageKey:               "agentbox/" + owner.ID + "/" + ownerThread.ID + "/upl_owner_private/file.txt",
		FileName:                 "file.txt",
		MimeType:                 &textType,
		SizeBytes:                4,
		ExpiresAt:                isoMillis(expiresAt),
		CreatedBy:                ownerKey.Name,
		CreatedByUserID:          &owner.ID,
		CreatedByKeyID:           &ownerKey.ID,
		CreatedByUserDisplayName: &owner.DisplayName,
		CreatedByActorName:       &ownerKey.Name,
	}
	createdUpload, err := repository.CreatePendingUpload(ctx, owner.ID, upload)
	if err != nil {
		t.Fatal(err)
	}
	if createdUpload.PublicURL != nil || !strings.HasPrefix(createdUpload.StorageKey, "agentbox/"+owner.ID+"/"+ownerThread.ID+"/") {
		t.Fatalf("pending upload metadata = %#v", createdUpload)
	}
	if _, err := repository.CreatePendingUpload(ctx, member.ID, types.PendingUpload{ID: "upl_cross_user", ThreadID: ownerThread.ID, StorageKey: "blocked", FileName: "blocked.txt", ExpiresAt: isoMillis(expiresAt), CreatedBy: memberBrowser.ActorName}); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("member created upload for owner thread: %v", err)
	}
	ownedUploads, err := repository.GetPendingUploads(ctx, owner.ID, ownerThread.ID, []string{upload.ID}, ownerConnector)
	if err != nil || len(ownedUploads) != 1 || ownedUploads[0].ID != upload.ID {
		t.Fatalf("owner pending upload lookup = %#v err=%v", ownedUploads, err)
	}
	wrongActor := ownerConnector
	wrongActor.KeyID = "key_other"
	if wrongUploads, err := repository.GetPendingUploads(ctx, owner.ID, ownerThread.ID, []string{upload.ID}, wrongActor); err != nil || len(wrongUploads) != 0 {
		t.Fatalf("pending upload crossed actors: uploads=%#v err=%v", wrongUploads, err)
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `set local enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	planRows, err := tx.Query(ctx, `
explain (format text)
select id, owner_user_id, title, updated_at
from threads
where owner_user_id = $1
order by updated_at desc
limit 50
`, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer planRows.Close()
	plan := strings.Builder{}
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := planRows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "threads_owner_updated_idx") {
		t.Fatalf("private list plan did not use owner index:\n%s", plan.String())
	}
}

func TestTeamSharedThreadAccessIsImmediateCompleteAndIndexed(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := repository.BootstrapOwner(ctx, "share-owner@example.com", "Share Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repository.UpsertProvisionedUser(ctx, types.DefaultTenantID, "share-member@example.com", "Share Member", nil, "member")
	if err != nil {
		t.Fatal(err)
	}
	secondMember, err := repository.UpsertProvisionedUser(ctx, types.DefaultTenantID, "share-second@example.com", "Second Member", nil, "member")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := repository.UpsertProvisionedUser(ctx, types.DefaultTenantID, "share-outsider@example.com", "Share Outsider", nil, "member")
	if err != nil {
		t.Fatal(err)
	}
	teamA, err := repository.CreateTeam(ctx, "team-a", "Team A")
	if err != nil {
		t.Fatal(err)
	}
	teamB, err := repository.CreateTeam(ctx, "team-b", "Team B")
	if err != nil {
		t.Fatal(err)
	}

	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard"}
	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectAPIKey, ActorName: "member-agent", KeyID: "key_member"}
	thread, err := repository.CreateThread(ctx, owner.ID, "team shared indexed marker", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}

	if visibility, err := repository.GetThreadVisibility(ctx, member.ID, thread.ID); err != nil || visibility != nil {
		t.Fatalf("private visibility leaked before share: visibility=%#v err=%v", visibility, err)
	}
	shared, err := repository.SetThreadVisibility(ctx, owner.ID, thread.ID, []string{teamA.ID, teamB.ID, teamA.ID})
	if err != nil || len(shared.SharedTeams) != 2 || shared.OwnerUserID != owner.ID {
		t.Fatalf("initial visibility=%#v err=%v", shared, err)
	}
	var shareCount int
	if err := repository.pool.QueryRow(ctx, `select count(*) from thread_team_shares where thread_id = $1`, thread.ID).Scan(&shareCount); err != nil {
		t.Fatal(err)
	}
	if shareCount != 2 {
		t.Fatalf("duplicate team share count=%d", shareCount)
	}

	if access, err := repository.ResolveThreadAccess(ctx, member.ID, thread.ID); err != nil || access != nil {
		t.Fatalf("share without membership granted access: access=%#v err=%v", access, err)
	}
	if _, err := repository.AddTeamMember(ctx, teamA.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	access, err := repository.ResolveThreadAccess(ctx, member.ID, thread.ID)
	if err != nil || access == nil || access.IsOwner || len(access.MatchedTeamIDs) != 1 || access.MatchedTeamIDs[0] != teamA.ID {
		t.Fatalf("team access=%#v err=%v", access, err)
	}

	threads, err := repository.ListThreads(ctx, member.ID, 50)
	if err != nil || len(threads) != 1 || threads[0].ID != thread.ID {
		t.Fatalf("team list=%#v err=%v", threads, err)
	}
	search, err := repository.SearchThreads(ctx, member.ID, types.SearchThreadParams{Query: "indexed marker", Limit: 20})
	if err != nil || len(search) != 1 || search[0].ID != thread.ID {
		t.Fatalf("team search=%#v err=%v", search, err)
	}
	detail, err := repository.GetThread(ctx, member.ID, thread.ID)
	if err != nil || detail == nil || len(detail.Visibility.SharedTeams) != 2 {
		t.Fatalf("team detail=%#v err=%v", detail, err)
	}

	textType := "text/plain"
	message, err := repository.PostMessage(ctx, member.ID, thread.ID, memberAuth, "team participant message", nil, []types.NewAsset{{
		StorageKey: "agentbox/" + member.ID + "/" + thread.ID + "/message/team.txt",
		FileName:   "team.txt",
		MimeType:   &textType,
		SizeBytes:  4,
	}})
	if err != nil || len(message.Assets) != 1 || message.CreatedByActorName == nil || *message.CreatedByActorName != memberAuth.ActorName {
		t.Fatalf("team post=%#v err=%v", message, err)
	}
	if asset, err := repository.GetAsset(ctx, member.ID, message.Assets[0].ID); err != nil || asset == nil || asset.ID != message.Assets[0].ID {
		t.Fatalf("team asset lookup=%#v err=%v", asset, err)
	}
	if asset, err := repository.GetAsset(ctx, outsider.ID, message.Assets[0].ID); err != nil || asset != nil {
		t.Fatalf("outsider asset lookup=%#v err=%v", asset, err)
	}

	expiresAt := time.Now().UTC().Add(15 * time.Minute)
	upload := types.PendingUpload{
		ID:                       "upl_team_shared",
		ThreadID:                 thread.ID,
		StorageKey:               "agentbox/" + member.ID + "/" + thread.ID + "/upl_team_shared/file.txt",
		FileName:                 "file.txt",
		MimeType:                 &textType,
		SizeBytes:                4,
		ExpiresAt:                isoMillis(expiresAt),
		CreatedBy:                memberAuth.ActorName,
		CreatedByUserID:          &member.ID,
		CreatedByKeyID:           &memberAuth.KeyID,
		CreatedByUserDisplayName: &member.DisplayName,
		CreatedByActorName:       &memberAuth.ActorName,
	}
	createdUpload, err := repository.CreatePendingUpload(ctx, member.ID, upload)
	if err != nil || createdUpload.ID != upload.ID {
		t.Fatalf("team upload creation=%#v err=%v", createdUpload, err)
	}
	pending, err := repository.GetPendingUploads(ctx, member.ID, thread.ID, []string{upload.ID}, memberAuth)
	if err != nil || len(pending) != 1 || pending[0].ID != upload.ID {
		t.Fatalf("team pending upload=%#v err=%v", pending, err)
	}
	if err := repository.MarkPendingUploadsConsumed(ctx, member.ID, thread.ID, []string{upload.ID}, memberAuth); err != nil {
		t.Fatal(err)
	}

	if _, err := repository.AddTeamMember(ctx, teamB.ID, secondMember.ID); err != nil {
		t.Fatal(err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, secondMember.ID, thread.ID); err != nil || access == nil || len(access.MatchedTeamIDs) != 1 || access.MatchedTeamIDs[0] != teamB.ID {
		t.Fatalf("second team access=%#v err=%v", access, err)
	}

	participantVisibility, err := repository.SetThreadVisibility(ctx, member.ID, thread.ID, []string{teamA.ID})
	if err != nil || len(participantVisibility.SharedTeams) != 1 || participantVisibility.SharedTeams[0].ID != teamA.ID {
		t.Fatalf("participant visibility mutation=%#v err=%v", participantVisibility, err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, secondMember.ID, thread.ID); err != nil || access != nil {
		t.Fatalf("removed team retained access: access=%#v err=%v", access, err)
	}

	if removed, err := repository.RemoveTeamMember(ctx, teamA.ID, member.ID); err != nil || !removed {
		t.Fatalf("membership removal removed=%t err=%v", removed, err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, member.ID, thread.ID); err != nil || access != nil {
		t.Fatalf("removed member retained access: access=%#v err=%v", access, err)
	}
	if _, err := repository.PostMessage(ctx, member.ID, thread.ID, memberAuth, "blocked", nil, nil); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("removed member posted: %v", err)
	}
	if _, err := repository.CreatePendingUpload(ctx, member.ID, types.PendingUpload{ID: "upl_blocked", ThreadID: thread.ID, StorageKey: "blocked", FileName: "blocked.txt", ExpiresAt: isoMillis(expiresAt), CreatedBy: memberAuth.ActorName}); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("removed member created upload: %v", err)
	}
	if asset, err := repository.GetAsset(ctx, member.ID, message.Assets[0].ID); err != nil || asset != nil {
		t.Fatalf("removed member retained asset access: asset=%#v err=%v", asset, err)
	}

	if _, err := repository.AddTeamMember(ctx, teamA.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, member.ID, thread.ID); err != nil || access == nil {
		t.Fatalf("re-added member did not regain access: access=%#v err=%v", access, err)
	}
	privateAgain, err := repository.SetThreadVisibility(ctx, member.ID, thread.ID, nil)
	if err != nil || len(privateAgain.SharedTeams) != 0 {
		t.Fatalf("participant made private visibility=%#v err=%v", privateAgain, err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, member.ID, thread.ID); err != nil || access != nil {
		t.Fatalf("participant retained access after making private: access=%#v err=%v", access, err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, owner.ID, thread.ID); err != nil || access == nil || !access.IsOwner {
		t.Fatalf("owner lost access after private mutation: access=%#v err=%v", access, err)
	}

	if _, err := repository.SetThreadVisibility(ctx, owner.ID, thread.ID, []string{"team_missing"}); !errors.Is(err, types.ErrTeamNotFound) {
		t.Fatalf("missing share team error=%v", err)
	}

	if _, err := repository.SetThreadVisibility(ctx, owner.ID, thread.ID, []string{teamA.ID}); err != nil {
		t.Fatal(err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `set local enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	planRows, err := tx.Query(ctx, `
explain (format text)
select t.id
from threads t
where `+normalThreadAccessPredicate+`
order by t.updated_at desc
limit 50
`, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer planRows.Close()
	plan := strings.Builder{}
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := planRows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "thread_team_shares_team_thread_idx") || !strings.Contains(plan.String(), "team_memberships_user_team_idx") {
		t.Fatalf("team access plan missed indexes:\n%s", plan.String())
	}
}

func TestCredentialMigrationRemovesTenantAndPlaintextSecretColumns(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	for table, forbiddenColumns := range map[string][]string{
		"api_keys":        {"tenant_id", "key_value"},
		"user_sessions":   {"tenant_id"},
		"cli_login_codes": {"tenant_id"},
	} {
		for _, column := range forbiddenColumns {
			var exists bool
			if err := repository.pool.QueryRow(ctx, `
select exists (
  select 1
  from information_schema.columns
  where table_schema = current_schema()
    and table_name = $1
    and column_name = $2
)
`, table, column).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if exists {
				t.Fatalf("legacy identity column %s.%s still exists", table, column)
			}
		}
	}

	var purposeNotNull bool
	var userIDNotNull bool
	if err := repository.pool.QueryRow(ctx, `
select
  (select is_nullable = 'NO' from information_schema.columns where table_schema = current_schema() and table_name = 'api_keys' and column_name = 'purpose'),
  (select is_nullable = 'NO' from information_schema.columns where table_schema = current_schema() and table_name = 'api_keys' and column_name = 'user_id')
`).Scan(&purposeNotNull, &userIDNotNull); err != nil {
		t.Fatal(err)
	}
	if !purposeNotNull || !userIDNotNull {
		t.Fatalf("credential ownership columns are nullable: purpose=%t user_id=%t", purposeNotNull, userIDNotNull)
	}
}

func TestOwnerSetupTokensAreHashedSingleUseAndTransactional(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	firstSecret := "agos_first_secret"
	first, err := repository.CreateOwnerSetupToken(ctx, hashSecret(firstSecret), time.Now().UTC().Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if first.Purpose != "bootstrap" {
		t.Fatalf("first purpose = %q", first.Purpose)
	}
	var storedHash string
	if err := repository.pool.QueryRow(ctx, `select token_hash from owner_setup_tokens where id = $1`, first.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash != hashSecret(firstSecret) || storedHash == firstSecret {
		t.Fatalf("setup token storage is not hash-only: %q", storedHash)
	}

	secondSecret := "agos_second_secret"
	second, err := repository.CreateOwnerSetupToken(ctx, hashSecret(secondSecret), time.Now().UTC().Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.Purpose != "bootstrap" || second.ID == first.ID {
		t.Fatalf("unexpected replacement token: first=%#v second=%#v", first, second)
	}
	var firstRevoked bool
	if err := repository.pool.QueryRow(ctx, `select revoked_at is not null from owner_setup_tokens where id = $1`, first.ID).Scan(&firstRevoked); err != nil {
		t.Fatal(err)
	}
	if !firstRevoked {
		t.Fatal("issuing a replacement token did not revoke the prior active token")
	}
	if _, _, err := repository.UseOwnerSetupToken(ctx, hashSecret(firstSecret), "owner@example.com", "Owner", "hash-one"); !errors.Is(err, ErrOwnerSetupTokenInvalid) {
		t.Fatalf("revoked token error = %v", err)
	}

	owner, consumed, err := repository.UseOwnerSetupToken(ctx, hashSecret(secondSecret), "owner@example.com", "Owner", "hash-one")
	if err != nil {
		t.Fatal(err)
	}
	if !owner.IsOwner || consumed.ConsumedAt == nil {
		t.Fatalf("bootstrap result owner=%#v token=%#v", owner, consumed)
	}
	if _, _, err := repository.UseOwnerSetupToken(ctx, hashSecret(secondSecret), "owner@example.com", "Owner", "hash-one"); !errors.Is(err, ErrOwnerSetupTokenInvalid) {
		t.Fatalf("replayed token error = %v", err)
	}

	recoverySecret := "agos_recovery_secret"
	recovery, err := repository.CreateOwnerSetupToken(ctx, hashSecret(recoverySecret), time.Now().UTC().Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Purpose != "recovery" {
		t.Fatalf("recovery purpose = %q", recovery.Purpose)
	}
	if _, _, err := repository.UseOwnerSetupToken(ctx, hashSecret(recoverySecret), "other@example.com", "Wrong", "hash-two"); !errors.Is(err, ErrOwnerAlreadyExists) {
		t.Fatalf("wrong-email recovery error = %v", err)
	}
	var recoveryConsumed bool
	if err := repository.pool.QueryRow(ctx, `select consumed_at is not null from owner_setup_tokens where id = $1`, recovery.ID).Scan(&recoveryConsumed); err != nil {
		t.Fatal(err)
	}
	if recoveryConsumed {
		t.Fatal("failed recovery consumed the one-time token despite transaction rollback")
	}
	recoveredOwner, _, err := repository.UseOwnerSetupToken(ctx, hashSecret(recoverySecret), "OWNER@example.com", "Recovered", "hash-two")
	if err != nil {
		t.Fatal(err)
	}
	if recoveredOwner.ID != owner.ID || recoveredOwner.DisplayName != "Recovered" {
		t.Fatalf("recovery changed owner identity: before=%#v after=%#v", owner, recoveredOwner)
	}

	expiredSecret := "agos_expired_secret"
	expired, err := repository.CreateOwnerSetupToken(ctx, hashSecret(expiredSecret), time.Now().UTC().Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `update owner_setup_tokens set expires_at = now() - interval '1 minute' where id = $1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.UseOwnerSetupToken(ctx, hashSecret(expiredSecret), "owner@example.com", "Owner", "hash-three"); !errors.Is(err, ErrOwnerSetupTokenInvalid) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestSignupInvitationsRegisterTransactionallyAndDisableUsers(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpsertProvisionedUser(ctx, types.DefaultTenantID, "existing@example.com", "Existing", nil, "member"); err != nil {
		t.Fatal(err)
	}

	engineering, err := repository.CreateTeam(ctx, "engineering", "Engineering")
	if err != nil {
		t.Fatal(err)
	}
	operations, err := repository.CreateTeam(ctx, "operations", "Operations")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateTeam(ctx, "ENGINEERING", "Duplicate"); !errors.Is(err, types.ErrTeamSlugConflict) {
		t.Fatalf("duplicate team slug error=%v", err)
	}

	invitationSecret := "aginv_transactional"
	invitation, err := repository.CreateSignupInvitation(ctx, owner.ID, hashSecret(invitationSecret), time.Now().UTC().Add(time.Hour), []string{engineering.ID, operations.ID, engineering.ID})
	if err != nil {
		t.Fatal(err)
	}
	if invitation.CreatedByUserID != owner.ID || len(invitation.Teams) != 2 {
		t.Fatalf("unexpected invitation=%#v", invitation)
	}
	if _, err := repository.pool.Exec(ctx, `delete from teams where id = $1`, engineering.ID); err == nil {
		t.Fatal("team referenced by an active invitation was deleted")
	} else {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
			t.Fatalf("active-invitation team deletion error=%v", err)
		}
	}
	if _, _, _, err := repository.RegisterWithSignupInvitation(ctx, hashSecret(invitationSecret), "EXISTING@example.com", "Duplicate", "password-hash", "session-duplicate", time.Now().UTC().Add(time.Hour)); !errors.Is(err, types.ErrEmailAlreadyRegistered) {
		t.Fatalf("duplicate registration error=%v", err)
	}
	if active, err := repository.FindSignupInvitation(ctx, hashSecret(invitationSecret)); err != nil || active == nil {
		t.Fatalf("duplicate registration consumed invitation: active=%#v err=%v", active, err)
	}
	var existingMemberships int
	if err := repository.pool.QueryRow(ctx, `
select count(*)
from team_memberships tm
join users u on u.id = tm.user_id
where lower(u.email) = lower('existing@example.com')
`).Scan(&existingMemberships); err != nil {
		t.Fatal(err)
	}
	if existingMemberships != 0 {
		t.Fatalf("failed duplicate registration created memberships=%d", existingMemberships)
	}

	memberSessionHash := "member-session-hash"
	member, session, consumed, err := repository.RegisterWithSignupInvitation(ctx, hashSecret(invitationSecret), "member@example.com", "Member", "member-password-hash", memberSessionHash, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if member.IsOwner || member.Role != "member" || session.UserID != member.ID || consumed.ConsumedAt == nil || consumed.ConsumedByUserID == nil || *consumed.ConsumedByUserID != member.ID {
		t.Fatalf("unexpected registration member=%#v session=%#v invitation=%#v", member, session, consumed)
	}
	memberTeams, err := repository.ListUserTeams(ctx, member.ID)
	if err != nil || len(memberTeams) != 2 || memberTeams[0].ID != engineering.ID || memberTeams[1].ID != operations.ID {
		t.Fatalf("transactional invitation memberships=%#v err=%v", memberTeams, err)
	}
	if _, err := repository.AddTeamMember(ctx, engineering.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	firstOwnerMembership, err := repository.AddTeamMember(ctx, engineering.ID, owner.ID)
	if err != nil {
		t.Fatalf("duplicate membership add failed: %v", err)
	}
	if firstOwnerMembership.TeamID != engineering.ID || firstOwnerMembership.UserID != owner.ID {
		t.Fatalf("duplicate membership result=%#v", firstOwnerMembership)
	}
	renamedEngineering, err := repository.RenameTeam(ctx, engineering.ID, "Product Engineering")
	if err != nil || renamedEngineering.Slug != engineering.Slug || renamedEngineering.Name != "Product Engineering" {
		t.Fatalf("rename team=%#v err=%v", renamedEngineering, err)
	}
	if removed, err := repository.RemoveTeamMember(ctx, operations.ID, member.ID); err != nil || !removed {
		t.Fatalf("remove membership removed=%t err=%v", removed, err)
	}
	if removed, err := repository.RemoveTeamMember(ctx, operations.ID, member.ID); err != nil || removed {
		t.Fatalf("idempotent remove removed=%t err=%v", removed, err)
	}
	memberTeams, err = repository.ListUserTeams(ctx, member.ID)
	if err != nil || len(memberTeams) != 1 || memberTeams[0].ID != engineering.ID {
		t.Fatalf("membership removal teams=%#v err=%v", memberTeams, err)
	}
	ownerThread, err := repository.CreateThread(ctx, owner.ID, "membership does not share", types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, ActorName: "Web dashboard"})
	if err != nil {
		t.Fatal(err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, member.ID, ownerThread.ID); err != nil || access != nil {
		t.Fatalf("team membership changed private thread access: access=%#v err=%v", access, err)
	}
	if active, err := repository.FindSignupInvitation(ctx, hashSecret(invitationSecret)); err != nil || active != nil {
		t.Fatalf("consumed invitation remained active: active=%#v err=%v", active, err)
	}

	memberKeySecret := "agb_member_disable"
	if _, err := repository.CreateAPIKey(ctx, member.ID, "local", "local", hashSecret(memberKeySecret), "agb_member", []string{"threads:read"}); err != nil {
		t.Fatal(err)
	}
	cliCodeHash := "member-cli-code"
	cliStateHash := "member-cli-state"
	if _, err := repository.CreateCLILoginCode(ctx, types.CLILoginCode{
		UserID:      member.ID,
		CodeHash:    cliCodeHash,
		StateHash:   cliStateHash,
		RedirectURI: "http://127.0.0.1:8080/callback",
		ExpiresAt:   isoMillis(time.Now().UTC().Add(time.Hour)),
	}); err != nil {
		t.Fatal(err)
	}
	disabled, err := repository.SetUserDisabled(ctx, member.ID, true)
	if err != nil || disabled.DisabledAt == nil {
		t.Fatalf("disable member=%#v err=%v", disabled, err)
	}
	if foundSession, foundUser, err := repository.FindUserSessionBySecretHash(ctx, memberSessionHash); err != nil || foundSession != nil || foundUser != nil {
		t.Fatalf("disabled session resolved: session=%#v user=%#v err=%v", foundSession, foundUser, err)
	}
	if foundKey, foundUser, err := repository.FindAPIKeyBySecret(ctx, memberKeySecret); err != nil || foundKey != nil || foundUser != nil {
		t.Fatalf("disabled key resolved: key=%#v user=%#v err=%v", foundKey, foundUser, err)
	}
	enabled, err := repository.SetUserDisabled(ctx, member.ID, false)
	if err != nil || enabled.DisabledAt != nil {
		t.Fatalf("enable member=%#v err=%v", enabled, err)
	}
	if code, user, err := repository.ConsumeCLILoginCode(ctx, cliCodeHash, cliStateHash, "http://127.0.0.1:8080/callback"); err != nil || code != nil || user != nil {
		t.Fatalf("disabled user's old CLI login code became usable after enablement: code=%#v user=%#v err=%v", code, user, err)
	}
	if _, err := repository.SetUserDisabled(ctx, owner.ID, true); !errors.Is(err, types.ErrOwnerCannotBeDisabled) {
		t.Fatalf("owner disable error=%v", err)
	}

	concurrentSecret := "aginv_concurrent"
	if _, err := repository.CreateSignupInvitation(ctx, owner.ID, hashSecret(concurrentSecret), time.Now().UTC().Add(time.Hour), []string{engineering.ID}); err != nil {
		t.Fatal(err)
	}
	type registrationResult struct {
		user types.User
		err  error
	}
	results := make(chan registrationResult, 2)
	var waitGroup sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			user, _, _, err := repository.RegisterWithSignupInvitation(
				ctx,
				hashSecret(concurrentSecret),
				fmt.Sprintf("concurrent-%d@example.com", index),
				fmt.Sprintf("Concurrent %d", index),
				"password-hash",
				fmt.Sprintf("session-%d", index),
				time.Now().UTC().Add(time.Hour),
			)
			results <- registrationResult{user: user, err: err}
		}()
	}
	waitGroup.Wait()
	close(results)
	successes := 0
	invalids := 0
	successfulUserID := ""
	for result := range results {
		if result.err == nil {
			successes++
			if result.user.ID == "" {
				t.Fatal("successful concurrent registration had empty user ID")
			}
			successfulUserID = result.user.ID
		} else if errors.Is(result.err, types.ErrSignupInvitationInvalid) {
			invalids++
		} else {
			t.Fatalf("unexpected concurrent registration error=%v", result.err)
		}
	}
	if successes != 1 || invalids != 1 {
		t.Fatalf("concurrent registration results: successes=%d invalids=%d", successes, invalids)
	}
	if teams, err := repository.ListUserTeams(ctx, successfulUserID); err != nil || len(teams) != 1 || teams[0].ID != engineering.ID {
		t.Fatalf("concurrent winner memberships=%#v err=%v", teams, err)
	}

	zeroSecret := "aginv_zero_team"
	if _, err := repository.CreateSignupInvitation(ctx, owner.ID, hashSecret(zeroSecret), time.Now().UTC().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	zeroUser, _, _, err := repository.RegisterWithSignupInvitation(ctx, hashSecret(zeroSecret), "zero@example.com", "Zero", "hash", "zero-session", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if teams, err := repository.ListUserTeams(ctx, zeroUser.ID); err != nil || len(teams) != 0 {
		t.Fatalf("zero-team registration teams=%#v err=%v", teams, err)
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `set local enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	planRows, err := tx.Query(ctx, `
explain (format text)
select t.id
from team_memberships tm
join teams t on t.id = tm.team_id
where tm.user_id = $1
`, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer planRows.Close()
	plan := strings.Builder{}
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := planRows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "team_memberships_user_team_idx") {
		t.Fatalf("team list plan did not use membership index:\n%s", plan.String())
	}
}

func TestMigrateRejectsAppliedMigrationDrift(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `update schema_migrations set checksum = 'changed' where version = '0001'`); err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(ctx); err == nil || !strings.Contains(err.Error(), "drift detected") {
		t.Fatalf("Migrate error = %v, want drift detection", err)
	}
}

func TestUserOnboardingCredentialsAreExplicitResumableAndSerialized(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}

	initial, err := repository.GetOnboardingState(ctx, owner.ID)
	if err != nil || len(initial.Steps) != 0 {
		t.Fatalf("initial onboarding=%#v err=%v", initial, err)
	}
	keys, err := repository.ListAPIKeys(ctx, owner.ID)
	if err != nil || len(keys) != 0 {
		t.Fatalf("onboarding read created keys=%#v err=%v", keys, err)
	}
	dismissed, err := repository.DismissOnboarding(ctx, owner.ID)
	if err != nil || dismissed.DismissedAt == nil {
		t.Fatalf("dismissed onboarding=%#v err=%v", dismissed, err)
	}

	chatSecret := "agb_onboarding_chat"
	chat, state, err := repository.CreateOnboardingCredential(ctx, owner.ID, "chatgpt", "ChatGPT", "chatgpt", hashSecret(chatSecret), "agb_chat", []string{"threads:read", "mcp:use"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Name != "ChatGPT" || state.DismissedAt != nil || len(state.Steps) != 1 || state.Steps[0].Credential == nil || state.Steps[0].Credential.ID != chat.ID || state.Steps[0].Credential.Key != "" {
		t.Fatalf("chat onboarding key=%#v state=%#v", chat, state)
	}
	if _, _, err := repository.CreateOnboardingCredential(ctx, owner.ID, "chatgpt", "ChatGPT", "chatgpt", hashSecret("duplicate"), "agb_dupe", []string{"threads:read"}, false); !errors.Is(err, types.ErrOnboardingCredentialExists) {
		t.Fatalf("duplicate onboarding credential error=%v", err)
	}

	claude, _, err := repository.CreateOnboardingCredential(ctx, owner.ID, "claude", "Claude", "claude", hashSecret("agb_onboarding_claude"), "agb_claude", []string{"threads:read", "mcp:use"}, false)
	if err != nil {
		t.Fatal(err)
	}
	local, state, err := repository.CreateOnboardingCredential(ctx, owner.ID, "local", "Local CLI", "local", hashSecret("agb_onboarding_local"), "agb_local", []string{"threads:read", "threads:write", "keys:read", "keys:write"}, false)
	if err != nil || len(state.Steps) != 3 {
		t.Fatalf("local onboarding key=%#v state=%#v err=%v", local, state, err)
	}
	if chat.ID == claude.ID || chat.ID == local.ID || claude.ID == local.ID {
		t.Fatalf("connectors collapsed credentials: chat=%s claude=%s local=%s", chat.ID, claude.ID, local.ID)
	}

	rotatedSecret := "agb_onboarding_chat_rotated"
	rotated, _, err := repository.CreateOnboardingCredential(ctx, owner.ID, "chatgpt", "ChatGPT", "chatgpt", hashSecret(rotatedSecret), "agb_rotate", []string{"threads:read", "mcp:use"}, true)
	if err != nil || rotated.ID != chat.ID {
		t.Fatalf("rotation=%#v original=%#v err=%v", rotated, chat, err)
	}
	if oldKey, _, err := repository.FindAPIKeyBySecret(ctx, chatSecret); err != nil || oldKey != nil {
		t.Fatalf("old rotated secret active: key=%#v err=%v", oldKey, err)
	}
	if newKey, user, err := repository.FindAPIKeyBySecret(ctx, rotatedSecret); err != nil || newKey == nil || user == nil || newKey.ID != chat.ID || user.ID != owner.ID {
		t.Fatalf("rotated secret lookup key=%#v user=%#v err=%v", newKey, user, err)
	}
	if claudeKey, _, err := repository.FindAPIKeyBySecret(ctx, "agb_onboarding_claude"); err != nil || claudeKey == nil || claudeKey.ID != claude.ID {
		t.Fatalf("chat rotation affected Claude: key=%#v err=%v", claudeKey, err)
	}

	if revoked, err := repository.RevokeAPIKey(ctx, owner.ID, "Local CLI"); err != nil || !revoked {
		t.Fatalf("revoke local revoked=%t err=%v", revoked, err)
	}
	state, err = repository.GetOnboardingState(ctx, owner.ID)
	if err != nil || state.Steps[2].Connector != "local" || state.Steps[2].CompletedAt == nil || state.Steps[2].Credential != nil {
		t.Fatalf("revoked local onboarding state=%#v err=%v", state, err)
	}
	recreated, _, err := repository.CreateOnboardingCredential(ctx, owner.ID, "local", "Local CLI", "local", hashSecret("agb_onboarding_local_new"), "agb_local_new", []string{"threads:read"}, false)
	if err != nil || recreated.ID == local.ID {
		t.Fatalf("recreated local=%#v original=%#v err=%v", recreated, local, err)
	}

	concurrentUser, err := repository.UpsertProvisionedUser(ctx, types.DefaultTenantID, "concurrent-onboarding@example.com", "Concurrent", nil, "member")
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		key types.APIKey
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		index := index
		go func() {
			<-start
			key, _, err := repository.CreateOnboardingCredential(context.Background(), concurrentUser.ID, "chatgpt", "ChatGPT", "chatgpt", hashSecret(fmt.Sprintf("concurrent-%d", index)), fmt.Sprintf("agb_%d", index), []string{"threads:read"}, false)
			results <- result{key: key, err: err}
		}()
	}
	close(start)
	successes := 0
	conflicts := 0
	for index := 0; index < 2; index++ {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			if result.key.ID == "" {
				t.Fatal("concurrent onboarding success had empty key ID")
			}
		case errors.Is(result.err, types.ErrOnboardingCredentialExists):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent onboarding error=%v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent onboarding successes=%d conflicts=%d", successes, conflicts)
	}
	if keys, err := repository.ListAPIKeys(ctx, concurrentUser.ID); err != nil || len(keys) != 1 || keys[0].Name != "ChatGPT" {
		t.Fatalf("concurrent onboarding keys=%#v err=%v", keys, err)
	}
}

func TestListThreadsDoesNotRunMigrations(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	auth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, ActorName: "Web dashboard"}
	thread, err := repository.CreateThread(ctx, owner.ID, "Hot path", auth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `drop table schema_migrations`); err != nil {
		t.Fatal(err)
	}

	threads, err := repository.ListThreads(ctx, owner.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].ID != thread.ID {
		t.Fatalf("threads = %#v", threads)
	}

	var ledgerExists bool
	if err := repository.pool.QueryRow(ctx, `
select exists (
  select 1
  from information_schema.tables
  where table_schema = current_schema()
    and table_name = 'schema_migrations'
)
`).Scan(&ledgerExists); err != nil {
		t.Fatal(err)
	}
	if ledgerExists {
		t.Fatal("ListThreads recreated schema_migrations; request-time DDL is still active")
	}
}

func openPostgresTestRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; PostgreSQL integration test runs in CI")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal(err)
	}

	schemaName := "agentbox_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}

	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	query := parsedURL.Query()
	query.Set("search_path", schemaName+",public")
	parsedURL.RawQuery = query.Encode()

	repository, err := Open(ctx, config.Config{
		DatabaseURL: parsedURL.String(),
		DBPoolSize:  4,
	})
	if err != nil {
		_, _ = adminPool.Exec(ctx, "drop schema "+quotedSchema+" cascade")
		adminPool.Close()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		repository.Close()
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupContext, "drop schema "+quotedSchema+" cascade"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		adminPool.Close()
	})

	if err := repository.pool.Ping(ctx); err != nil {
		t.Fatal(fmt.Errorf("ping isolated PostgreSQL schema: %w", err))
	}
	return repository, ctx
}
