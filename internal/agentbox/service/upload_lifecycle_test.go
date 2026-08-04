package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/backup"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/types"
)

type failOnceCopyStore struct {
	*assets.FakeStore
	remainingFailures int
}

func (s *failOnceCopyStore) CopyAssetObject(ctx context.Context, sourceStorageKey string, destinationStorageKey string, expectedETag string) (backup.ObjectMetadata, error) {
	if s.remainingFailures > 0 {
		s.remainingFailures--
		return backup.ObjectMetadata{}, errors.New("simulated copy outage")
	}
	return s.FakeStore.CopyAssetObject(ctx, sourceStorageKey, destinationStorageKey, expectedETag)
}

type finalizedUploadFixture struct {
	upload  types.PresignedUpload
	message types.Message
	digest  string
	mime    string
}

func finalizeDirectUploadForTest(t *testing.T, svc *Service, store *assets.FakeStore, auth types.AuthContext, threadID string, name string, contents []byte) finalizedUploadFixture {
	t.Helper()
	digestBytes := sha256.Sum256(contents)
	digest := hex.EncodeToString(digestBytes[:])
	mimeType := "text/plain"
	uploads, err := svc.CreatePresignedUploads(t.Context(), auth, threadID, []types.UploadIntentFile{{
		FileName: name, MimeType: &mimeType, SizeBytes: int64(len(contents)), SHA256: digest,
	}})
	if err != nil || len(uploads) != 1 {
		t.Fatalf("create upload intent: uploads=%#v err=%v", uploads, err)
	}
	store.PutAssetObjectWithSHA(uploads[0].StorageKey, int64(len(contents)), &mimeType, digest)
	message, err := svc.PostMessage(t.Context(), auth, PostMessageParams{
		ThreadID:       threadID,
		Body:           "finalize " + name,
		UploadedAssets: []types.UploadedAssetReference{{UploadID: uploads[0].UploadID}},
	})
	if err != nil || len(message.Assets) != 1 {
		t.Fatalf("finalize upload: message=%#v err=%v", message, err)
	}
	if message.Assets[0].StorageKey == uploads[0].StorageKey || assets.SHA256FromFinalStorageKey(message.Assets[0].StorageKey) != digest {
		t.Fatalf("upload did not promote to immutable content-addressed key: upload=%#v asset=%#v", uploads[0], message.Assets[0])
	}
	return finalizedUploadFixture{upload: uploads[0], message: message, digest: digest, mime: mimeType}
}

func replayStagingAndAssertFinalUnchanged(t *testing.T, store *assets.FakeStore, fixture finalizedUploadFixture) {
	t.Helper()
	finalKey := fixture.message.Assets[0].StorageKey
	before, err := store.HeadAssetObject(t.Context(), finalKey)
	if err != nil {
		t.Fatalf("head canonical object before replay: %v", err)
	}
	replayDigest := strings.Repeat("f", 64)
	replayType := "application/octet-stream"
	store.PutAssetObjectWithSHA(fixture.upload.StorageKey, fixture.upload.SizeBytes+7, &replayType, replayDigest)
	after, err := store.HeadAssetObject(t.Context(), finalKey)
	if err != nil {
		t.Fatalf("head canonical object after replay: %v", err)
	}
	if before.SizeBytes != after.SizeBytes || before.ETag != after.ETag || !reflect.DeepEqual(before.Metadata, after.Metadata) || after.Metadata["agentbox-sha256"] != fixture.digest {
		t.Fatalf("staging replay mutated canonical object: before=%#v after=%#v", before, after)
	}
}

func TestPresignedUploadReplayCannotMutateCanonicalAttachmentAcrossLifecycleTransitions(t *testing.T) {
	t.Run("successful finalization", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		store := &assets.FakeStore{}
		svc := New(repo, store)
		user := types.User{ID: "usr_replay_success", Email: "success@example.invalid", DisplayName: "Success"}
		repo.Users = append(repo.Users, user)
		auth := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_replay_success", ActorName: "Web dashboard"}
		thread, err := svc.CreateThread(t.Context(), auth, "Replay after finalize")
		if err != nil {
			t.Fatal(err)
		}
		fixture := finalizeDirectUploadForTest(t, svc, store, auth, thread.ID, "success.txt", []byte("canonical success"))
		replayStagingAndAssertFinalUnchanged(t, store, fixture)
		if len(repo.Assets) != 1 || repo.Assets[0].StorageKey != fixture.message.Assets[0].StorageKey {
			t.Fatalf("staging replay changed canonical asset rows: %#v", repo.Assets)
		}
	})

	t.Run("loss of team access", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		store := &assets.FakeStore{}
		svc := New(repo, store)
		owner := types.User{ID: "usr_replay_owner", Email: "owner@example.invalid", DisplayName: "Owner"}
		member := types.User{ID: "usr_replay_member", Email: "member@example.invalid", DisplayName: "Member"}
		repo.Users = append(repo.Users, owner, member)
		ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_replay_owner", ActorName: "Web dashboard"}
		memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_replay_member", ActorName: "Web dashboard"}
		team, err := repo.CreateTeam(t.Context(), "replay-team", "Replay Team")
		if err != nil {
			t.Fatal(err)
		}
		for _, userID := range []string{owner.ID, member.ID} {
			if _, err := repo.AddTeamMember(t.Context(), team.ID, userID); err != nil {
				t.Fatal(err)
			}
		}
		thread, err := svc.CreateThread(t.Context(), ownerAuth, "Shared replay")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := setThreadVisibilityForTest(t.Context(), repo, owner.ID, thread.ID, []string{team.ID}); err != nil {
			t.Fatal(err)
		}
		fixture := finalizeDirectUploadForTest(t, svc, store, memberAuth, thread.ID, "team.txt", []byte("team canonical"))
		if removed, err := repo.RemoveTeamMember(t.Context(), team.ID, member.ID); err != nil || !removed {
			t.Fatalf("remove member: removed=%t err=%v", removed, err)
		}
		replayStagingAndAssertFinalUnchanged(t, store, fixture)
		if _, err := svc.PostMessage(t.Context(), memberAuth, PostMessageParams{ThreadID: thread.ID, Body: "no access"}); !errors.Is(err, types.ErrThreadNotFound) {
			t.Fatalf("removed member post error=%v", err)
		}
	})

	t.Run("credential revocation", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		store := &assets.FakeStore{}
		svc := New(repo, store)
		user := types.User{ID: "usr_replay_key", Email: "key@example.invalid", DisplayName: "Key User"}
		repo.Users = append(repo.Users, user)
		browserAuth := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_replay_key", ActorName: "Web dashboard"}
		credential, err := svc.CreateAPIKeyWithPurposeAndScopes(t.Context(), browserAuth, "Replay CLI", "cli", []string{"threads:read", "threads:write", "assets:read", "assets:write"})
		if err != nil {
			t.Fatal(err)
		}
		keyAuth := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectAPIKey, KeyID: credential.ID, ActorID: credential.ID, ActorName: credential.Name, Scopes: credential.Scopes}
		thread, err := svc.CreateThread(t.Context(), browserAuth, "Credential replay")
		if err != nil {
			t.Fatal(err)
		}
		fixture := finalizeDirectUploadForTest(t, svc, store, keyAuth, thread.ID, "key.txt", []byte("key canonical"))
		if err := svc.RevokeAPIKeyByID(t.Context(), browserAuth, credential.ID); err != nil {
			t.Fatal(err)
		}
		replayStagingAndAssertFinalUnchanged(t, store, fixture)
	})

	t.Run("user disablement", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		store := &assets.FakeStore{}
		svc := New(repo, store)
		owner := types.User{ID: "usr_replay_disable_owner", Email: "disable-owner@example.invalid", DisplayName: "Owner", IsOwner: true}
		target := types.User{ID: "usr_replay_disable_target", Email: "disable-target@example.invalid", DisplayName: "Target"}
		repo.Users = append(repo.Users, owner, target)
		ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_replay_disable_owner", ActorName: "Web dashboard", IsOwner: true}
		targetAuth := types.AuthContext{UserID: target.ID, UserDisplayName: target.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_replay_disable_target", ActorName: "Web dashboard"}
		thread, err := svc.CreateThread(t.Context(), targetAuth, "Disable replay")
		if err != nil {
			t.Fatal(err)
		}
		fixture := finalizeDirectUploadForTest(t, svc, store, targetAuth, thread.ID, "disabled.txt", []byte("disabled canonical"))
		if _, err := svc.SetUserDisabled(t.Context(), ownerAuth, target.ID, true); err != nil {
			t.Fatal(err)
		}
		replayStagingAndAssertFinalUnchanged(t, store, fixture)
	})

	t.Run("attachment purge", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		store := &assets.FakeStore{}
		svc := New(repo, store)
		owner := types.User{ID: "usr_replay_purge_owner", Email: "purge-owner@example.invalid", DisplayName: "Owner", IsOwner: true}
		target := types.User{ID: "usr_replay_purge_target", Email: "purge-target@example.invalid", DisplayName: "Target"}
		repo.Users = append(repo.Users, owner, target)
		ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_replay_purge_owner", ActorName: "Web dashboard", IsOwner: true}
		targetAuth := types.AuthContext{UserID: target.ID, UserDisplayName: target.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_replay_purge_target", ActorName: "Web dashboard"}
		thread, err := svc.CreateThread(t.Context(), targetAuth, "Purge replay")
		if err != nil {
			t.Fatal(err)
		}
		fixture := finalizeDirectUploadForTest(t, svc, store, targetAuth, thread.ID, "purged.txt", []byte("purged canonical"))
		finalKey := fixture.message.Assets[0].StorageKey
		if _, err := svc.SetUserDisabled(t.Context(), ownerAuth, target.ID, true); err != nil {
			t.Fatal(err)
		}
		purge, err := svc.PurgeUserAttachments(t.Context(), ownerAuth, target.ID, 10)
		if err != nil || purge.Purged != 1 || !purge.Complete {
			t.Fatalf("purge=%#v err=%v", purge, err)
		}
		if _, err := store.HeadAssetObject(t.Context(), finalKey); !errors.Is(err, backup.ErrObjectNotFound) {
			t.Fatalf("purged canonical object still exists: %v", err)
		}
		replayType := "application/octet-stream"
		store.PutAssetObjectWithSHA(fixture.upload.StorageKey, fixture.upload.SizeBytes+9, &replayType, strings.Repeat("e", 64))
		if _, err := store.HeadAssetObject(t.Context(), finalKey); !errors.Is(err, backup.ErrObjectNotFound) {
			t.Fatalf("staging replay recreated purged canonical object: %v", err)
		}
		if len(repo.Assets) != 1 || repo.Assets[0].PurgedAt == nil || repo.Assets[0].StorageKey != finalKey {
			t.Fatalf("purged canonical row changed after replay: %#v", repo.Assets)
		}
	})
}

func TestImmutableUploadBoundaryCleanupRetryQuotaAndIdentity(t *testing.T) {
	t.Run("oversized intent rejected before pending state", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		store := &assets.FakeStore{MaxFileSizeBytes: 4}
		svc := New(repo, store)
		user := types.User{ID: "usr_upload_oversize", Email: "oversize@example.invalid", DisplayName: "Oversize"}
		repo.Users = append(repo.Users, user)
		auth := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_upload_oversize", ActorName: "Web dashboard"}
		thread, err := svc.CreateThread(t.Context(), auth, "Oversize")
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.CreatePresignedUploads(t.Context(), auth, thread.ID, []types.UploadIntentFile{{FileName: "too-large.bin", SizeBytes: 5, SHA256: strings.Repeat("a", 64)}})
		if err == nil || !strings.Contains(err.Error(), "too large") || len(repo.Pending) != 0 {
			t.Fatalf("oversized intent err=%v pending=%#v", err, repo.Pending)
		}
	})

	t.Run("rejected mismatch is exact-key cleaned and retries idempotently", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		store := &assets.FakeStore{DeleteFailures: map[string]error{}}
		svc := New(repo, store)
		user := types.User{ID: "usr_upload_cleanup", Email: "cleanup@example.invalid", DisplayName: "Cleanup"}
		repo.Users = append(repo.Users, user)
		auth := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_upload_cleanup", ActorName: "Web dashboard"}
		thread, err := svc.CreateThread(t.Context(), auth, "Cleanup")
		if err != nil {
			t.Fatal(err)
		}
		mimeType := "text/plain"
		digest := strings.Repeat("a", 64)
		uploads, err := svc.CreatePresignedUploads(t.Context(), auth, thread.ID, []types.UploadIntentFile{{FileName: "mismatch.txt", MimeType: &mimeType, SizeBytes: 4, SHA256: digest}})
		if err != nil {
			t.Fatal(err)
		}
		stagingKey := uploads[0].StorageKey
		store.PutAssetObjectWithSHA(stagingKey, 5, &mimeType, digest)
		store.DeleteFailures[stagingKey] = errors.New("simulated cleanup outage")
		_, err = svc.PostMessage(t.Context(), auth, PostMessageParams{ThreadID: thread.ID, Body: "reject", UploadedAssets: []types.UploadedAssetReference{{UploadID: uploads[0].UploadID}}})
		if !hasCodedError(err, "UPLOAD_METADATA_MISMATCH") || len(repo.Messages) != 0 || repo.Pending[0].Status != "rejected" {
			t.Fatalf("rejected finalization err=%v messages=%#v pending=%#v", err, repo.Messages, repo.Pending)
		}
		if _, err := store.HeadAssetObject(t.Context(), stagingKey); err != nil {
			t.Fatalf("failed cleanup unexpectedly removed staging object: %v", err)
		}
		delete(store.DeleteFailures, stagingKey)
		cleanup, err := svc.CleanupPendingUploads(t.Context(), 100)
		if err != nil || cleanup.Cleaned != 1 || cleanup.Failed != 0 {
			t.Fatalf("cleanup retry=%#v err=%v", cleanup, err)
		}
		if _, err := store.HeadAssetObject(t.Context(), stagingKey); !errors.Is(err, backup.ErrObjectNotFound) {
			t.Fatalf("cleanup did not remove exact staging key: %v", err)
		}
		repeated, err := svc.CleanupPendingUploads(t.Context(), 100)
		if err != nil || repeated.Attempted != 0 {
			t.Fatalf("repeated cleanup was not idempotent: %#v err=%v", repeated, err)
		}
	})

	t.Run("abandoned expired staging object is bounded and idempotent", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		store := &assets.FakeStore{}
		svc := New(repo, store)
		user := types.User{ID: "usr_upload_abandoned", Email: "abandoned@example.invalid", DisplayName: "Abandoned"}
		repo.Users = append(repo.Users, user)
		auth := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_upload_abandoned", ActorName: "Web dashboard"}
		thread, err := svc.CreateThread(t.Context(), auth, "Abandoned")
		if err != nil {
			t.Fatal(err)
		}
		digest := strings.Repeat("b", 64)
		uploads, err := svc.CreatePresignedUploads(t.Context(), auth, thread.ID, []types.UploadIntentFile{{FileName: "abandoned.bin", SizeBytes: 3, SHA256: digest}})
		if err != nil {
			t.Fatal(err)
		}
		store.PutAssetObjectWithSHA(uploads[0].StorageKey, 3, nil, digest)
		if len(repo.UploadCleanup) != 1 {
			t.Fatalf("cleanup inventory=%#v", repo.UploadCleanup)
		}
		repo.UploadCleanup[0].NotBefore = time.Now().UTC().Add(-time.Minute)
		cleanup, err := svc.CleanupPendingUploads(t.Context(), 1)
		if err != nil || cleanup.Attempted != 1 || cleanup.Cleaned != 1 {
			t.Fatalf("abandoned cleanup=%#v err=%v", cleanup, err)
		}
		if _, err := store.HeadAssetObject(t.Context(), uploads[0].StorageKey); !errors.Is(err, backup.ErrObjectNotFound) {
			t.Fatalf("abandoned staging object survived cleanup: %v", err)
		}
		repeated, err := svc.CleanupPendingUploads(t.Context(), 1)
		if err != nil || repeated.Attempted != 0 {
			t.Fatalf("abandoned cleanup repeat=%#v err=%v", repeated, err)
		}
	})

	t.Run("copy failure releases claim and retry succeeds", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		baseStore := &assets.FakeStore{}
		store := &failOnceCopyStore{FakeStore: baseStore, remainingFailures: 1}
		svc := New(repo, store)
		user := types.User{ID: "usr_upload_copy_retry", Email: "copy-retry@example.invalid", DisplayName: "Copy Retry"}
		repo.Users = append(repo.Users, user)
		auth := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_upload_copy_retry", ActorName: "Web dashboard"}
		thread, err := svc.CreateThread(t.Context(), auth, "Copy retry")
		if err != nil {
			t.Fatal(err)
		}
		mimeType := "text/plain"
		digest := strings.Repeat("c", 64)
		uploads, err := svc.CreatePresignedUploads(t.Context(), auth, thread.ID, []types.UploadIntentFile{{FileName: "retry.txt", MimeType: &mimeType, SizeBytes: 4, SHA256: digest}})
		if err != nil {
			t.Fatal(err)
		}
		baseStore.PutAssetObjectWithSHA(uploads[0].StorageKey, 4, &mimeType, digest)
		params := PostMessageParams{ThreadID: thread.ID, Body: "retry", UploadedAssets: []types.UploadedAssetReference{{UploadID: uploads[0].UploadID}}}
		if _, err := svc.PostMessage(t.Context(), auth, params); err == nil || !strings.Contains(err.Error(), "simulated copy outage") {
			t.Fatalf("copy outage error=%v", err)
		}
		if len(repo.Messages) != 0 || repo.Pending[0].Status != "pending" || repo.Pending[0].FinalizationToken != "" {
			t.Fatalf("copy failure left partial publish state: messages=%#v pending=%#v", repo.Messages, repo.Pending)
		}
		message, err := svc.PostMessage(t.Context(), auth, params)
		if err != nil || len(message.Assets) != 1 || repo.Pending[0].Status != "finalized" {
			t.Fatalf("copy retry message=%#v pending=%#v err=%v", message, repo.Pending, err)
		}
	})

	t.Run("stale finalization claim is recovered and retryable", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		store := &assets.FakeStore{}
		svc := New(repo, store)
		user := types.User{ID: "usr_upload_stale_claim", Email: "stale-claim@example.invalid", DisplayName: "Stale Claim"}
		repo.Users = append(repo.Users, user)
		auth := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_upload_stale_claim", ActorName: "Web dashboard"}
		thread, err := svc.CreateThread(t.Context(), auth, "Stale finalization")
		if err != nil {
			t.Fatal(err)
		}
		mimeType := "text/plain"
		contents := []byte("retry after crashed worker")
		sum := sha256.Sum256(contents)
		digest := hex.EncodeToString(sum[:])
		uploads, err := svc.CreatePresignedUploads(t.Context(), auth, thread.ID, []types.UploadIntentFile{{
			FileName: "stale.txt", MimeType: &mimeType, SizeBytes: int64(len(contents)), SHA256: digest,
		}})
		if err != nil || len(uploads) != 1 {
			t.Fatalf("create upload intent: uploads=%#v err=%v", uploads, err)
		}
		store.PutAssetObjectWithSHA(uploads[0].StorageKey, int64(len(contents)), &mimeType, digest)
		staleFinalKey := assets.MakeFinalStorageKey(user.ID, thread.ID, uploads[0].UploadID, uploads[0].FileName, digest)
		claimed, err := repo.ClaimPendingUploadsForFinalization(t.Context(), user.ID, thread.ID, auth, "fin_stale", []types.UploadFinalizationTarget{{
			UploadID: uploads[0].UploadID, FinalStorageKey: staleFinalKey,
		}})
		if err != nil || len(claimed) != 1 {
			t.Fatalf("claim upload: claimed=%#v err=%v", claimed, err)
		}
		store.PutAssetObjectWithSHA(staleFinalKey, int64(len(contents)), &mimeType, digest)
		staleStartedAt := time.Now().UTC().Add(-11 * time.Minute).Format(time.RFC3339)
		repo.Pending[0].FinalizationStartedAt = &staleStartedAt
		for index := range repo.UploadCleanup {
			if repo.UploadCleanup[index].Candidate.ObjectKind == "final_candidate" {
				repo.UploadCleanup[index].NotBefore = time.Now().UTC().Add(-time.Minute)
			}
		}

		cleanup, err := svc.CleanupPendingUploads(t.Context(), 10)
		if err != nil || cleanup.Cleaned != 1 || cleanup.Failed != 0 {
			t.Fatalf("stale cleanup=%#v err=%v", cleanup, err)
		}
		if repo.Pending[0].Status != "pending" || repo.Pending[0].FinalizationToken != "" || repo.Pending[0].FinalStorageKey != "" || repo.Pending[0].FinalizationStartedAt != nil {
			t.Fatalf("stale claim was not released: %#v", repo.Pending[0])
		}
		if _, err := store.HeadAssetObject(t.Context(), staleFinalKey); !errors.Is(err, backup.ErrObjectNotFound) {
			t.Fatalf("stale final candidate survived cleanup: %v", err)
		}
		if _, err := store.HeadAssetObject(t.Context(), uploads[0].StorageKey); err != nil {
			t.Fatalf("retryable staging object was removed: %v", err)
		}

		message, err := svc.PostMessage(t.Context(), auth, PostMessageParams{
			ThreadID: thread.ID, Body: "retry stale claim", UploadedAssets: []types.UploadedAssetReference{{UploadID: uploads[0].UploadID}},
		})
		if err != nil || len(message.Assets) != 1 || repo.Pending[0].Status != "finalized" {
			t.Fatalf("retry after stale recovery: message=%#v pending=%#v err=%v", message, repo.Pending[0], err)
		}
	})

	t.Run("active pending quota resists repeated abuse", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		store := &assets.FakeStore{}
		svc := New(repo, store)
		user := types.User{ID: "usr_upload_quota", Email: "quota@example.invalid", DisplayName: "Quota"}
		repo.Users = append(repo.Users, user)
		auth := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_upload_quota", ActorName: "Web dashboard"}
		thread, err := svc.CreateThread(t.Context(), auth, "Quota")
		if err != nil {
			t.Fatal(err)
		}
		for batch := 0; batch < 10; batch++ {
			files := make([]types.UploadIntentFile, 0, 10)
			for index := 0; index < 10; index++ {
				files = append(files, types.UploadIntentFile{FileName: fmt.Sprintf("quota-%02d-%02d.bin", batch, index), SizeBytes: 1, SHA256: strings.Repeat(fmt.Sprintf("%x", batch%16), 64)})
			}
			if _, err := svc.CreatePresignedUploads(t.Context(), auth, thread.ID, files); err != nil {
				t.Fatalf("quota batch %d: %v", batch, err)
			}
		}
		if len(repo.Pending) != 100 {
			t.Fatalf("pending count=%d, want 100", len(repo.Pending))
		}
		_, err = svc.CreatePresignedUploads(t.Context(), auth, thread.ID, []types.UploadIntentFile{{FileName: "quota-overflow.bin", SizeBytes: 1, SHA256: strings.Repeat("d", 64)}})
		if !hasCodedError(err, "UPLOAD_QUOTA_EXCEEDED") || len(repo.Pending) != 100 {
			t.Fatalf("quota overflow err=%v pending=%d", err, len(repo.Pending))
		}
	})

	t.Run("download signing rejects same-length identity substitution", func(t *testing.T) {
		repo := &db.MemoryRepository{}
		store := &assets.FakeStore{}
		svc := New(repo, store)
		user := types.User{ID: "usr_upload_identity", Email: "identity@example.invalid", DisplayName: "Identity"}
		repo.Users = append(repo.Users, user)
		auth := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_upload_identity", ActorName: "Web dashboard"}
		thread, err := svc.CreateThread(t.Context(), auth, "Identity")
		if err != nil {
			t.Fatal(err)
		}
		fixture := finalizeDirectUploadForTest(t, svc, store, auth, thread.ID, "identity.txt", []byte("same-length-data"))
		finalKey := fixture.message.Assets[0].StorageKey
		store.PutAssetObjectWithSHA(finalKey, fixture.upload.SizeBytes, &fixture.mime, strings.Repeat("0", 64))
		if _, err := svc.SignedAssetDownloadURL(t.Context(), auth, fixture.message.Assets[0].ID, 60); !hasCodedError(err, "ATTACHMENT_UNAVAILABLE") {
			t.Fatalf("same-length identity substitution signed: %v", err)
		}
	})
}
