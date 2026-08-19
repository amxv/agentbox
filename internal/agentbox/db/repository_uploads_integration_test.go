package db

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"agentbox/internal/agentbox/types"
)

func TestPendingUploadFinalizationIsAtomicAndReplaySafeInPostgreSQL(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "atomic-upload-owner@example.com", "Atomic Upload Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	authContext := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard"}
	thread, err := repository.CreateThread(ctx, owner.ID, "Atomic pending upload", authContext)
	if err != nil {
		t.Fatal(err)
	}
	mimeType := "text/plain"
	digest := strings.Repeat("a", 64)
	upload := types.PendingUpload{
		ID: "upl_atomic", ThreadID: thread.ID, StorageKey: "agentbox/staging/atomic.txt", FileName: "atomic.txt", MimeType: &mimeType, SizeBytes: 6,
		ExpectedSHA256: digest, ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), CreatedBy: authContext.ActorName,
		CreatedByUserID: optionalString(owner.ID), CreatedByUserDisplayName: optionalString(owner.DisplayName), CreatedByActorName: optionalString(authContext.ActorName),
	}
	if _, err := repository.CreatePendingUploads(ctx, owner.ID, []types.PendingUpload{upload}); err != nil {
		t.Fatal(err)
	}

	type result struct {
		token   string
		claimed []types.PendingUpload
		err     error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		index := index
		go func() {
			<-start
			token := fmt.Sprintf("fin_atomic_%d", index)
			claimed, err := repository.ClaimPendingUploadsForFinalization(context.Background(), owner.ID, thread.ID, authContext, token, []types.UploadFinalizationTarget{{UploadID: upload.ID, FinalStorageKey: fmt.Sprintf("agentbox/final/sha256/%s/%d-atomic.txt", digest, index)}})
			results <- result{token: token, claimed: claimed, err: err}
		}()
	}
	close(start)
	var winner result
	successes, finalizing := 0, 0
	for index := 0; index < 2; index++ {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			winner = result
		case errors.Is(result.err, types.ErrPendingUploadFinalizing):
			finalizing++
		default:
			t.Fatalf("unexpected claim error=%v", result.err)
		}
	}
	if successes != 1 || finalizing != 1 || len(winner.claimed) != 1 {
		t.Fatalf("successes=%d finalizing=%d winner=%#v", successes, finalizing, winner)
	}
	finalized := types.NewAsset{StorageKey: winner.claimed[0].FinalStorageKey, FileName: upload.FileName, MimeType: upload.MimeType, SizeBytes: upload.SizeBytes, ContentSHA256: digest}
	message, err := repository.PostMessageWithFinalizedUploads(ctx, owner.ID, thread.ID, authContext, "atomic finalize", nil, nil, []types.NewAsset{finalized}, []string{upload.ID}, winner.token)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Assets) != 1 || message.Assets[0].StorageKey != finalized.StorageKey {
		t.Fatalf("successful message=%#v", message)
	}
	if _, err := repository.PostMessageWithFinalizedUploads(ctx, owner.ID, thread.ID, authContext, "replay", nil, nil, []types.NewAsset{finalized}, []string{upload.ID}, winner.token); !errors.Is(err, types.ErrPendingUploadUnavailable) {
		t.Fatalf("finalization replay error=%v", err)
	}
	var messageCount, assetCount, consumedCount int
	if err := repository.pool.QueryRow(ctx, `select count(*) from messages where thread_id=$1`, thread.ID).Scan(&messageCount); err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, `select count(*) from assets where storage_key=$1`, finalized.StorageKey).Scan(&assetCount); err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, `select count(*) from pending_uploads where id=$1 and status='finalized' and consumed_at is not null`, upload.ID).Scan(&consumedCount); err != nil {
		t.Fatal(err)
	}
	if messageCount != 1 || assetCount != 1 || consumedCount != 1 {
		t.Fatalf("message_count=%d asset_count=%d consumed_count=%d", messageCount, assetCount, consumedCount)
	}
}

func TestPendingUploadQuotaSerializesAcrossThreadsInPostgreSQL(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "upload-quota-owner@example.com", "Upload Quota Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	authContext := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard"}
	threadA, err := repository.CreateThread(ctx, owner.ID, "Quota thread A", authContext)
	if err != nil {
		t.Fatal(err)
	}
	threadB, err := repository.CreateThread(ctx, owner.ID, "Quota thread B", authContext)
	if err != nil {
		t.Fatal(err)
	}

	makeUploads := func(threadID string, prefix string, count int) []types.PendingUpload {
		uploads := make([]types.PendingUpload, 0, count)
		for index := 0; index < count; index++ {
			uploads = append(uploads, types.PendingUpload{
				ID:                       fmt.Sprintf("upl_quota_%s_%03d", prefix, index),
				ThreadID:                 threadID,
				StorageKey:               fmt.Sprintf("agentbox/staging/quota/%s/%03d.bin", prefix, index),
				FileName:                 fmt.Sprintf("quota-%s-%03d.bin", prefix, index),
				SizeBytes:                1,
				ExpectedSHA256:           strings.Repeat(fmt.Sprintf("%x", (index%15)+1), 64),
				ExpiresAt:                time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
				CreatedBy:                authContext.ActorName,
				CreatedByUserID:          optionalString(owner.ID),
				CreatedByUserDisplayName: optionalString(owner.DisplayName),
				CreatedByActorName:       optionalString(authContext.ActorName),
			})
		}
		return uploads
	}

	// Exercise the single-row compatibility method too: it must use the same
	// quota and cleanup state machine rather than bypassing the batch path.
	single, err := repository.CreatePendingUpload(ctx, owner.ID, makeUploads(threadA.ID, "single", 1)[0])
	if err != nil || single.Status != "pending" {
		t.Fatalf("single pending upload=%#v err=%v", single, err)
	}
	if _, err := repository.CreatePendingUploads(ctx, owner.ID, makeUploads(threadA.ID, "baseline", 89)); err != nil {
		t.Fatal(err)
	}

	type quotaResult struct {
		created int
		err     error
	}
	results := make(chan quotaResult, 2)
	start := make(chan struct{})
	for _, batch := range []struct {
		threadID string
		prefix   string
	}{{threadA.ID, "race-a"}, {threadB.ID, "race-b"}} {
		batch := batch
		go func() {
			<-start
			created, err := repository.CreatePendingUploads(context.Background(), owner.ID, makeUploads(batch.threadID, batch.prefix, 10))
			results <- quotaResult{created: len(created), err: err}
		}()
	}
	close(start)

	successes, rejected := 0, 0
	for index := 0; index < 2; index++ {
		result := <-results
		switch {
		case result.err == nil && result.created == 10:
			successes++
		case errors.Is(result.err, types.ErrPendingUploadQuotaExceeded):
			rejected++
		default:
			t.Fatalf("unexpected quota result: %#v", result)
		}
	}
	if successes != 1 || rejected != 1 {
		t.Fatalf("successes=%d rejected=%d", successes, rejected)
	}

	var pendingCount, cleanupCount int
	if err := repository.pool.QueryRow(ctx, `
select
  (select count(*)::int from pending_uploads where created_by_user_id = $1 and consumed_at is null),
  (select count(*)::int from upload_cleanup_objects where object_kind = 'staging' and cleaned_at is null)
`, owner.ID).Scan(&pendingCount, &cleanupCount); err != nil {
		t.Fatal(err)
	}
	if pendingCount != 100 || cleanupCount != 100 {
		t.Fatalf("pending_count=%d cleanup_count=%d, want 100/100", pendingCount, cleanupCount)
	}
}

func TestStalePendingUploadFinalizationRecoversForRetryInPostgreSQL(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "stale-finalization-owner@example.com", "Stale Finalization Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	authContext := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard"}
	thread, err := repository.CreateThread(ctx, owner.ID, "Stale pending finalization", authContext)
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("b", 64)
	upload := types.PendingUpload{
		ID: "upl_stale_pg", ThreadID: thread.ID, StorageKey: "agentbox/staging/stale-pg.bin", FileName: "stale-pg.bin", SizeBytes: 3,
		ExpectedSHA256: digest, ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), CreatedBy: authContext.ActorName,
		CreatedByUserID: optionalString(owner.ID), CreatedByUserDisplayName: optionalString(owner.DisplayName), CreatedByActorName: optionalString(authContext.ActorName),
	}
	if _, err := repository.CreatePendingUpload(ctx, owner.ID, upload); err != nil {
		t.Fatal(err)
	}
	finalKey := "agentbox/final/sha256/" + digest + "/stale-pg.bin"
	claimed, err := repository.ClaimPendingUploadsForFinalization(ctx, owner.ID, thread.ID, authContext, "fin_stale_pg", []types.UploadFinalizationTarget{{
		UploadID: upload.ID, FinalStorageKey: finalKey,
	}})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%#v err=%v", claimed, err)
	}
	if _, err := repository.pool.Exec(ctx, `
update pending_uploads
set finalization_started_at = now() - interval '11 minutes'
where id = $1
`, upload.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `
update upload_cleanup_objects
set not_before = now() - interval '1 minute'
where upload_id = $1 and object_kind = 'final_candidate'
`, upload.ID); err != nil {
		t.Fatal(err)
	}

	candidates, err := repository.ListUploadCleanupCandidates(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	foundFinalCandidate := false
	for _, candidate := range candidates {
		if candidate.UploadID == upload.ID && candidate.StorageKey == finalKey && candidate.ObjectKind == "final_candidate" {
			foundFinalCandidate = true
		}
	}
	if !foundFinalCandidate {
		t.Fatalf("stale final candidate not returned for cleanup: %#v", candidates)
	}

	var status string
	var finalStorageKey, finalizationToken *string
	var finalizationStartedAt *time.Time
	if err := repository.pool.QueryRow(ctx, `
select status, final_storage_key, finalization_token, finalization_started_at
from pending_uploads
where id = $1
`, upload.ID).Scan(&status, &finalStorageKey, &finalizationToken, &finalizationStartedAt); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || finalStorageKey != nil || finalizationToken != nil || finalizationStartedAt != nil {
		t.Fatalf("stale claim state status=%q final_key=%v token=%v started=%v", status, finalStorageKey, finalizationToken, finalizationStartedAt)
	}

	retry, err := repository.ClaimPendingUploadsForFinalization(ctx, owner.ID, thread.ID, authContext, "fin_stale_pg_retry", []types.UploadFinalizationTarget{{
		UploadID: upload.ID, FinalStorageKey: finalKey + ".retry",
	}})
	if err != nil || len(retry) != 1 || retry[0].Status != "finalizing" {
		t.Fatalf("retry claim=%#v err=%v", retry, err)
	}
}

func TestAttachmentPurgeCandidatesAndTombstonesAreUploaderScopedAndIndexed(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "purge-owner@example.com", "Purge Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	target, err := repository.CreateUser(ctx, "purge-target@example.com", "Purge Target", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := repository.CreateUser(ctx, "purge-other@example.com", "Purge Other", nil)
	if err != nil {
		t.Fatal(err)
	}
	team, err := repository.CreateTeam(ctx, "purge-fixture", "Purge Fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{owner.ID, target.ID, other.ID} {
		if _, err := repository.AddTeamMember(ctx, team.ID, userID); err != nil {
			t.Fatal(err)
		}
	}
	targetAuth := types.AuthContext{UserID: target.ID, UserDisplayName: target.DisplayName, ActorName: "Web dashboard", SubjectType: types.AuthSubjectUserSession}
	otherAuth := types.AuthContext{UserID: other.ID, UserDisplayName: other.DisplayName, ActorName: "Web dashboard", SubjectType: types.AuthSubjectUserSession}
	targetThread, err := repository.CreateThread(ctx, target.ID, "Target purge fixture", targetAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(ctx, repository, target.ID, targetThread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	otherThread, err := repository.CreateThread(ctx, other.ID, "Other purge fixture", otherAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(ctx, repository, other.ID, otherThread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}

	targetOwnedKey := "agentbox/purge-db/target-owned.bin"
	targetCrossThreadKey := "agentbox/purge-db/target-cross-thread.bin"
	otherOwnedKey := "agentbox/purge-db/other-owned.bin"
	targetOwnedMessage, err := repository.PostMessage(ctx, target.ID, targetThread.ID, targetAuth, "target owned", nil, []types.NewAsset{{StorageKey: targetOwnedKey, FileName: "target-owned.bin", SizeBytes: 10}})
	if err != nil {
		t.Fatal(err)
	}
	targetCrossThreadMessage, err := repository.PostMessage(ctx, target.ID, otherThread.ID, targetAuth, "target cross thread", nil, []types.NewAsset{{StorageKey: targetCrossThreadKey, FileName: "target-cross-thread.bin", SizeBytes: 20}})
	if err != nil {
		t.Fatal(err)
	}
	otherOwnedMessage, err := repository.PostMessage(ctx, other.ID, targetThread.ID, otherAuth, "other owned", nil, []types.NewAsset{{StorageKey: otherOwnedKey, FileName: "other-owned.bin", SizeBytes: 30}})
	if err != nil {
		t.Fatal(err)
	}

	candidates, err := repository.ListAssetPurgeCandidates(ctx, target.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Fatalf("target purge candidates=%#v", candidates)
	}
	candidateKeys := []string{candidates[0].StorageKey, candidates[1].StorageKey}
	sort.Strings(candidateKeys)
	if !reflect.DeepEqual(candidateKeys, []string{targetCrossThreadKey, targetOwnedKey}) {
		t.Fatalf("candidate keys=%v", candidateKeys)
	}
	otherCandidates, err := repository.ListAssetPurgeCandidates(ctx, other.ID, 50)
	if err != nil || len(otherCandidates) != 1 || otherCandidates[0].StorageKey != otherOwnedKey {
		t.Fatalf("other purge candidates=%#v err=%v", otherCandidates, err)
	}

	targetOwnedAssetID := targetOwnedMessage.Assets[0].ID
	targetCrossThreadAssetID := targetCrossThreadMessage.Assets[0].ID
	otherOwnedAssetID := otherOwnedMessage.Assets[0].ID
	if err := repository.MarkAssetPurgeFailure(ctx, targetCrossThreadAssetID, "simulated exact-key delete failure"); err != nil {
		t.Fatal(err)
	}
	failedAsset, err := repository.GetAsset(ctx, owner.ID, targetCrossThreadAssetID)
	if err != nil || failedAsset == nil || failedAsset.PurgedAt != nil || failedAsset.PurgeLastAttemptAt == nil || failedAsset.PurgeError == nil || *failedAsset.PurgeError != "simulated exact-key delete failure" {
		t.Fatalf("failed purge state=%#v err=%v", failedAsset, err)
	}
	marked, err := repository.MarkAssetPurged(ctx, targetOwnedAssetID, owner.ID)
	if err != nil || !marked {
		t.Fatalf("mark first asset purged=%t err=%v", marked, err)
	}
	marked, err = repository.MarkAssetPurged(ctx, targetOwnedAssetID, owner.ID)
	if err != nil || !marked {
		t.Fatalf("idempotent tombstone=%t err=%v", marked, err)
	}
	remaining, err := repository.CountUnpurgedAssetsByUploader(ctx, target.ID)
	if err != nil || remaining != 1 {
		t.Fatalf("remaining target assets=%d err=%v", remaining, err)
	}
	if marked, err := repository.MarkAssetPurged(ctx, targetCrossThreadAssetID, owner.ID); err != nil || !marked {
		t.Fatalf("mark retry asset purged=%t err=%v", marked, err)
	}
	remaining, err = repository.CountUnpurgedAssetsByUploader(ctx, target.ID)
	if err != nil || remaining != 0 {
		t.Fatalf("completed target purge remaining=%d err=%v", remaining, err)
	}
	completed, err := repository.ListAssetPurgeCandidates(ctx, target.ID, 50)
	if err != nil || len(completed) != 0 {
		t.Fatalf("completed purge candidates=%#v err=%v", completed, err)
	}

	purgedOwned, err := repository.GetAsset(ctx, owner.ID, targetOwnedAssetID)
	if err != nil || purgedOwned == nil || purgedOwned.PurgedAt == nil || purgedOwned.PurgedByUserID == nil || *purgedOwned.PurgedByUserID != owner.ID || purgedOwned.PurgeError != nil || purgedOwned.StorageKey != targetOwnedKey || purgedOwned.FileName != "target-owned.bin" || purgedOwned.CreatedByUserID == nil || *purgedOwned.CreatedByUserID != target.ID {
		t.Fatalf("purged owned asset=%#v err=%v", purgedOwned, err)
	}
	purgedCrossThread, err := repository.GetAsset(ctx, owner.ID, targetCrossThreadAssetID)
	if err != nil || purgedCrossThread == nil || purgedCrossThread.PurgedAt == nil || purgedCrossThread.PurgeError != nil || purgedCrossThread.CreatedByUserID == nil || *purgedCrossThread.CreatedByUserID != target.ID {
		t.Fatalf("purged cross-thread asset=%#v err=%v", purgedCrossThread, err)
	}
	retainedOther, err := repository.GetAsset(ctx, owner.ID, otherOwnedAssetID)
	if err != nil || retainedOther == nil || retainedOther.PurgedAt != nil || retainedOther.StorageKey != otherOwnedKey || retainedOther.CreatedByUserID == nil || *retainedOther.CreatedByUserID != other.ID {
		t.Fatalf("retained other asset=%#v err=%v", retainedOther, err)
	}

	var indexPredicate string
	if err := repository.pool.QueryRow(ctx, `
select pg_get_expr(indpred, indrelid)
from pg_index
where indexrelid = 'assets_uploader_unpurged_idx'::regclass
`).Scan(&indexPredicate); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(indexPredicate, "created_by_user_id") || !strings.Contains(indexPredicate, "purged_at IS NULL") {
		t.Fatalf("unexpected purge index predicate=%q", indexPredicate)
	}
	planTx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer planTx.Rollback(ctx)
	if _, err := planTx.Exec(ctx, `set local enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	planRows, err := planTx.Query(ctx, `explain (costs off)
select id, storage_key
from assets
where created_by_user_id = $1 and purged_at is null
order by created_at, id
limit $2
`, other.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.Builder{}
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			planRows.Close()
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	planRows.Close()
	if err := planRows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "assets_uploader_unpurged_idx") {
		t.Fatalf("purge candidate plan did not use uploader index:\n%s", plan.String())
	}
}
