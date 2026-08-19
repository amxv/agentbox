package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) AcquireAttachmentPurgeLease(ctx context.Context, userID string) (types.AttachmentPurgeLease, error) {
	userID = strings.TrimSpace(userID)
	connection, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	release := func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, unlockErr := connection.Exec(cleanupContext, `select pg_advisory_unlock(hashtextextended($1, $2))`, userID, attachmentPurgeAdvisoryNamespace)
		if unlockErr != nil {
			conn := connection.Hijack()
			_ = conn.Close(cleanupContext)
			return
		}
		connection.Release()
	}
	if _, err := connection.Exec(ctx, `select pg_advisory_lock(hashtextextended($1, $2))`, userID, attachmentPurgeAdvisoryNamespace); err != nil {
		connection.Release()
		return nil, err
	}
	var isOwner bool
	var disabledAt *time.Time
	if err := connection.QueryRow(ctx, `select is_owner, disabled_at from users where id = $1`, userID).Scan(&isOwner, &disabledAt); errors.Is(err, pgx.ErrNoRows) {
		release()
		return nil, types.ErrUserNotFound
	} else if err != nil {
		release()
		return nil, err
	}
	if isOwner {
		release()
		return nil, types.ErrOwnerCannotBeDisabled
	}
	if disabledAt == nil {
		release()
		return nil, types.ErrUserMustBeDisabled
	}
	return &postgresAttachmentPurgeLease{conn: connection, userID: userID}, nil
}

func (r *Repository) ListAssetPurgeCandidates(ctx context.Context, uploaderUserID string, limit int) ([]types.AssetPurgeCandidate, error) {
	if limit < 1 {
		limit = 25
	}
	rows, err := r.pool.Query(ctx, `
select id, storage_key
from assets
where created_by_user_id = $1 and purged_at is null
order by created_at, id
limit $2
`, strings.TrimSpace(uploaderUserID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := []types.AssetPurgeCandidate{}
	for rows.Next() {
		var candidate types.AssetPurgeCandidate
		if err := rows.Scan(&candidate.AssetID, &candidate.StorageKey); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func (r *Repository) MarkAssetPurged(ctx context.Context, assetID string, ownerUserID string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
update assets
set purged_at = coalesce(purged_at, now()),
    purged_by_user_id = coalesce(purged_by_user_id, $2),
    purge_last_attempt_at = now(),
    purge_error = null
where id = $1
`, strings.TrimSpace(assetID), strings.TrimSpace(ownerUserID))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) MarkAssetPurgeFailure(ctx context.Context, assetID string, message string) error {
	_, err := r.pool.Exec(ctx, `
update assets
set purge_last_attempt_at = now(), purge_error = $2
where id = $1 and purged_at is null
`, strings.TrimSpace(assetID), strings.TrimSpace(message))
	return err
}

func (r *Repository) CountUnpurgedAssetsByUploader(ctx context.Context, uploaderUserID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
select count(*)::int
from assets
where created_by_user_id = $1 and purged_at is null
`, strings.TrimSpace(uploaderUserID)).Scan(&count)
	return count, err
}

func (r *Repository) CreatePendingUpload(ctx context.Context, userID string, upload types.PendingUpload) (types.PendingUpload, error) {
	created, err := r.CreatePendingUploads(ctx, userID, []types.PendingUpload{upload})
	if err != nil {
		return types.PendingUpload{}, err
	}
	return created[0], nil
}

func (r *Repository) CreatePendingUploads(ctx context.Context, userID string, uploads []types.PendingUpload) ([]types.PendingUpload, error) {
	if len(uploads) == 0 {
		return []types.PendingUpload{}, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialize the quota boundary per user, not merely per target thread. Without
	// this row lock, concurrent intent batches for different threads can both see
	// the same active count and exceed the deployment-wide per-user limit.
	var lockedUserID string
	if err := tx.QueryRow(ctx, `
select id
from users
where id = $1 and disabled_at is null
for update
`, strings.TrimSpace(userID)).Scan(&lockedUserID); errors.Is(err, pgx.ErrNoRows) {
		return nil, types.ErrThreadNotFound
	} else if err != nil {
		return nil, err
	}

	threadID := uploads[0].ThreadID
	if err := lockThreadAccessForMutation(ctx, tx, userID, threadID); err != nil {
		return nil, err
	}
	var activeCount int
	if err := tx.QueryRow(ctx, `
select count(*)::int
from pending_uploads
where created_by_user_id = $1
  and consumed_at is null
  and status in ('pending', 'finalizing')
  and expires_at > now()
`, strings.TrimSpace(userID)).Scan(&activeCount); err != nil {
		return nil, err
	}
	if activeCount+len(uploads) > 100 {
		return nil, types.ErrPendingUploadQuotaExceeded
	}

	created := make([]types.PendingUpload, 0, len(uploads))
	for _, upload := range uploads {
		if upload.ThreadID != threadID {
			return nil, errors.New("pending upload batch must target one thread")
		}
		item, err := scanPendingUpload(tx.QueryRow(ctx, `
insert into pending_uploads (
  id, thread_id, storage_key, file_name, mime_type, size_bytes, expected_sha256, status, expires_at,
  created_by, created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
)
values ($1, $2, $3, $4, $5, $6, $7, 'pending', $8, $9, $10, $11, $12, $13)
returning id, thread_id, storage_key, file_name, mime_type, size_bytes,
          expected_sha256, status, final_storage_key, finalization_token, finalization_started_at, rejected_at, rejection_reason,
          created_at, expires_at, created_by, created_by_user_id, created_by_key_id,
          created_by_user_display_name, created_by_actor_name, consumed_at
`, upload.ID, upload.ThreadID, upload.StorageKey, upload.FileName, upload.MimeType, upload.SizeBytes, optionalString(upload.ExpectedSHA256), upload.ExpiresAt, upload.CreatedBy, upload.CreatedByUserID, upload.CreatedByKeyID, upload.CreatedByUserDisplayName, upload.CreatedByActorName))
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
insert into upload_cleanup_objects (id, upload_id, storage_key, object_kind, not_before)
values ($1, $2, $3, 'staging', $4)
on conflict (storage_key) do nothing
`, "ucl_"+uuid.NewString(), upload.ID, upload.StorageKey, upload.ExpiresAt); err != nil {
			return nil, err
		}
		created = append(created, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return created, nil
}

func (r *Repository) GetPendingUploads(ctx context.Context, userID string, threadID string, uploadIDs []string, actor types.AuthContext) ([]types.PendingUpload, error) {
	if len(uploadIDs) == 0 {
		return []types.PendingUpload{}, nil
	}
	rows, err := r.pool.Query(ctx, `
select p.id, p.thread_id, p.storage_key, p.file_name, p.mime_type, p.size_bytes,
       p.expected_sha256, p.status, p.final_storage_key, p.finalization_token, p.finalization_started_at, p.rejected_at, p.rejection_reason,
       p.created_at, p.expires_at, p.created_by, p.created_by_user_id, p.created_by_key_id,
       p.created_by_user_display_name, p.created_by_actor_name, p.consumed_at
from pending_uploads p
join threads t on t.id = p.thread_id
where `+normalThreadAccessPredicate+`
  and p.thread_id = $2
  and p.id = any($3)
  and p.created_by_user_id = $1
  and p.created_by_key_id is not distinct from $4::text
`, userID, threadID, uploadIDs, optionalString(actor.KeyID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	uploads := []types.PendingUpload{}
	for rows.Next() {
		upload, err := scanPendingUpload(rows)
		if err != nil {
			return nil, err
		}
		uploads = append(uploads, upload)
	}
	return uploads, rows.Err()
}

func (r *Repository) MarkPendingUploadsConsumed(ctx context.Context, userID string, threadID string, uploadIDs []string, actor types.AuthContext) error {
	if len(uploadIDs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
update pending_uploads p
set consumed_at = now()
where p.thread_id = $2
  and p.id = any($3)
  and p.created_by_user_id = $1
  and p.created_by_key_id is not distinct from $4::text
  and exists (
    select 1 from threads t
    where t.id = p.thread_id and `+normalThreadAccessPredicate+`
  )
`, userID, threadID, uploadIDs, optionalString(actor.KeyID))
	return err
}

func (r *Repository) ClaimPendingUploadsForFinalization(ctx context.Context, userID string, threadID string, actor types.AuthContext, token string, targets []types.UploadFinalizationTarget) ([]types.PendingUpload, error) {
	if len(targets) == 0 {
		return []types.PendingUpload{}, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockLiveActorForMutation(ctx, tx, actor); err != nil {
		return nil, err
	}
	if err := lockThreadAccessForMutation(ctx, tx, userID, threadID); err != nil {
		return nil, err
	}
	claimed := make([]types.PendingUpload, 0, len(targets))
	for _, target := range targets {
		upload, err := scanPendingUpload(tx.QueryRow(ctx, `
select p.id, p.thread_id, p.storage_key, p.file_name, p.mime_type, p.size_bytes,
       p.expected_sha256, p.status, p.final_storage_key, p.finalization_token, p.finalization_started_at, p.rejected_at, p.rejection_reason,
       p.created_at, p.expires_at, p.created_by, p.created_by_user_id, p.created_by_key_id,
       p.created_by_user_display_name, p.created_by_actor_name, p.consumed_at
from pending_uploads p
where p.id = $1
  and p.thread_id = $2
  and p.created_by_user_id = $3
  and p.created_by_key_id is not distinct from $4::text
for update
`, strings.TrimSpace(target.UploadID), strings.TrimSpace(threadID), strings.TrimSpace(userID), optionalString(actor.KeyID)))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, types.ErrPendingUploadUnavailable
		}
		if err != nil {
			return nil, err
		}
		if upload.Status == "finalizing" {
			return nil, types.ErrPendingUploadFinalizing
		}
		if upload.Status != "pending" || upload.ConsumedAt != nil || upload.ExpectedSHA256 == "" {
			return nil, types.ErrPendingUploadUnavailable
		}
		expiresAt, err := time.Parse(time.RFC3339, upload.ExpiresAt)
		if err != nil || !expiresAt.After(time.Now().UTC()) {
			return nil, types.ErrPendingUploadUnavailable
		}
		if _, err := tx.Exec(ctx, `
update pending_uploads
set status = 'finalizing', final_storage_key = $2, finalization_token = $3,
    finalization_started_at = now(), rejection_reason = null, rejected_at = null
where id = $1
`, upload.ID, strings.TrimSpace(target.FinalStorageKey), strings.TrimSpace(token)); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
insert into upload_cleanup_objects (id, upload_id, storage_key, object_kind, not_before)
values ($1, $2, $3, 'final_candidate', now() + interval '10 minutes')
on conflict (storage_key) do nothing
`, "ucl_"+uuid.NewString(), upload.ID, strings.TrimSpace(target.FinalStorageKey)); err != nil {
			return nil, err
		}
		upload.Status = "finalizing"
		upload.FinalStorageKey = strings.TrimSpace(target.FinalStorageKey)
		upload.FinalizationToken = strings.TrimSpace(token)
		claimed = append(claimed, upload)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return claimed, nil
}

func (r *Repository) ReleasePendingUploadsFinalization(ctx context.Context, userID string, threadID string, actor types.AuthContext, token string, uploadIDs []string, rejectReason string) error {
	if len(uploadIDs) == 0 {
		return nil
	}
	status := "pending"
	rejectedAt := "null"
	if strings.TrimSpace(rejectReason) != "" {
		status = "rejected"
		rejectedAt = "now()"
	}
	query := `
update pending_uploads
set status = $6,
    final_storage_key = null,
    finalization_token = null,
    finalization_started_at = null,
    rejected_at = ` + rejectedAt + `,
    rejection_reason = nullif($5, '')
where thread_id = $2
  and id = any($3)
  and created_by_user_id = $1
  and created_by_key_id is not distinct from $4::text
  and finalization_token = $7
  and status = 'finalizing'
`
	if _, err := r.pool.Exec(ctx, query, strings.TrimSpace(userID), strings.TrimSpace(threadID), uploadIDs, optionalString(actor.KeyID), strings.TrimSpace(rejectReason), status, strings.TrimSpace(token)); err != nil {
		return err
	}
	if strings.TrimSpace(rejectReason) != "" {
		_, err := r.pool.Exec(ctx, `
update upload_cleanup_objects
set not_before = least(not_before, now())
where upload_id = any($1) and object_kind = 'staging' and cleaned_at is null
`, uploadIDs)
		return err
	}
	return nil
}

func (r *Repository) ListUploadCleanupCandidates(ctx context.Context, limit int) ([]types.UploadCleanupCandidate, error) {
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// A worker may disappear after it atomically claims an upload but before it
	// publishes the message. Once the final-candidate grace period has elapsed,
	// release that stale claim so an unexpired staging object can be retried. If
	// the intent itself has expired, reject it and let the exact-key cleanup rows
	// drain both staging and any ambiguous final candidate.
	if _, err := tx.Exec(ctx, `
with stale as (
  select id
  from pending_uploads
  where status = 'finalizing'
    and consumed_at is null
    and finalization_started_at <= now() - interval '10 minutes'
  order by finalization_started_at, id
  limit $1
  for update skip locked
)
update pending_uploads p
set status = case when expires_at > now() then 'pending' else 'rejected' end,
    final_storage_key = null,
    finalization_token = null,
    finalization_started_at = null,
    rejected_at = case when expires_at > now() then rejected_at else coalesce(rejected_at, now()) end,
    rejection_reason = case
      when expires_at > now() then rejection_reason
      else coalesce(nullif(rejection_reason, ''), 'Finalization expired before completion.')
    end
from stale
where p.id = stale.id
`, limit); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
select c.id, coalesce(c.upload_id, ''), c.storage_key, c.object_kind
from upload_cleanup_objects c
where c.cleaned_at is null
  and c.not_before <= now()
  and not exists (select 1 from assets a where a.storage_key = c.storage_key and a.purged_at is null)
order by c.not_before, c.created_at, c.id
limit $1
`, limit)
	if err != nil {
		return nil, err
	}
	result := []types.UploadCleanupCandidate{}
	for rows.Next() {
		var item types.UploadCleanupCandidate
		if err := rows.Scan(&item.ID, &item.UploadID, &item.StorageKey, &item.ObjectKind); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) MarkUploadCleanupSuccess(ctx context.Context, cleanupID string) error {
	_, err := r.pool.Exec(ctx, `
update upload_cleanup_objects
set cleaned_at = coalesce(cleaned_at, now()), attempt_count = attempt_count + 1,
    last_attempt_at = now(), last_error = null
where id = $1
`, strings.TrimSpace(cleanupID))
	return err
}

func (r *Repository) MarkUploadCleanupFailure(ctx context.Context, cleanupID string, message string) error {
	_, err := r.pool.Exec(ctx, `
update upload_cleanup_objects
set attempt_count = attempt_count + 1, last_attempt_at = now(), last_error = $2
where id = $1 and cleaned_at is null
`, strings.TrimSpace(cleanupID), strings.TrimSpace(message))
	return err
}

func (r *Repository) PostMessage(ctx context.Context, userID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, newAssets []types.NewAsset) (types.Message, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockThreadAccessForMutation(ctx, tx, userID, threadID); err != nil {
		return types.Message{}, err
	}
	message, err := postMessageTx(ctx, tx, userID, threadID, auth, body, bodyContentType, newAssets)
	if err != nil {
		return types.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.Message{}, err
	}
	return message, nil
}

func (r *Repository) PostMessageWithFinalizedUploads(ctx context.Context, userID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, newAssets []types.NewAsset, finalizedUploads []types.NewAsset, pendingUploadIDs []string, token string) (types.Message, error) {
	if len(finalizedUploads) != len(pendingUploadIDs) {
		return types.Message{}, types.ErrPendingUploadUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockLiveActorForMutation(ctx, tx, auth); err != nil {
		return types.Message{}, err
	}
	if err := lockThreadAccessForMutation(ctx, tx, userID, threadID); err != nil {
		return types.Message{}, err
	}
	for index, uploadID := range pendingUploadIDs {
		upload, err := scanPendingUpload(tx.QueryRow(ctx, `
select p.id, p.thread_id, p.storage_key, p.file_name, p.mime_type, p.size_bytes,
       p.expected_sha256, p.status, p.final_storage_key, p.finalization_token, p.finalization_started_at, p.rejected_at, p.rejection_reason,
       p.created_at, p.expires_at, p.created_by, p.created_by_user_id, p.created_by_key_id,
       p.created_by_user_display_name, p.created_by_actor_name, p.consumed_at
from pending_uploads p
where p.id = $1
  and p.thread_id = $2
  and p.created_by_user_id = $3
  and p.created_by_key_id is not distinct from $4::text
for update
`, strings.TrimSpace(uploadID), strings.TrimSpace(threadID), strings.TrimSpace(userID), optionalString(auth.KeyID)))
		if errors.Is(err, pgx.ErrNoRows) {
			return types.Message{}, types.ErrPendingUploadUnavailable
		}
		if err != nil {
			return types.Message{}, err
		}
		asset := finalizedUploads[index]
		if upload.Status != "finalizing" || upload.ConsumedAt != nil || upload.FinalizationToken != strings.TrimSpace(token) || upload.FinalStorageKey != asset.StorageKey || upload.FileName != asset.FileName || upload.SizeBytes != asset.SizeBytes || upload.ExpectedSHA256 != asset.ContentSHA256 || !sameOptionalString(upload.MimeType, asset.MimeType) {
			return types.Message{}, types.ErrPendingUploadUnavailable
		}
	}
	allAssets := append(append([]types.NewAsset(nil), newAssets...), finalizedUploads...)
	message, err := postMessageTx(ctx, tx, userID, threadID, auth, body, bodyContentType, allAssets)
	if err != nil {
		return types.Message{}, err
	}
	tag, err := tx.Exec(ctx, `
update pending_uploads
set status = 'finalized', consumed_at = now(), finalization_token = null
where thread_id = $1
  and id = any($2)
  and created_by_user_id = $3
  and created_by_key_id is not distinct from $4::text
  and status = 'finalizing'
  and finalization_token = $5
`, strings.TrimSpace(threadID), pendingUploadIDs, strings.TrimSpace(userID), optionalString(auth.KeyID), strings.TrimSpace(token))
	if err != nil {
		return types.Message{}, err
	}
	if tag.RowsAffected() != int64(len(pendingUploadIDs)) {
		return types.Message{}, types.ErrPendingUploadUnavailable
	}
	if len(finalizedUploads) > 0 {
		keys := make([]string, 0, len(finalizedUploads))
		for _, asset := range finalizedUploads {
			keys = append(keys, asset.StorageKey)
		}
		if _, err := tx.Exec(ctx, `
update upload_cleanup_objects
set cleaned_at = coalesce(cleaned_at, now()), last_error = null
where storage_key = any($1) and object_kind = 'final_candidate'
`, keys); err != nil {
			return types.Message{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return types.Message{}, err
	}
	return message, nil
}

func postMessageTx(ctx context.Context, tx pgx.Tx, userID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, newAssets []types.NewAsset) (types.Message, error) {
	var nextPosition int64
	if err := tx.QueryRow(ctx, `
select coalesce(max(position), 0) + 1
from messages
where thread_id = $1
`, threadID).Scan(&nextPosition); err != nil {
		return types.Message{}, err
	}
	messageID := "msg_" + uuid.NewString()
	message, err := scanMessage(tx.QueryRow(ctx, `
insert into messages (
  id, thread_id, position, author, body, body_content_type, created_by_user_id, created_by_key_id,
  created_by_user_display_name, created_by_actor_name
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
returning id, thread_id, author, body, body_content_type, created_at,
          created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
`, messageID, threadID, nextPosition, auth.ActorName, body, bodyContentType, userID, optionalString(auth.KeyID), optionalString(auth.UserDisplayName), optionalString(auth.ActorName)), nil)
	if err != nil {
		return types.Message{}, err
	}
	message.Position = nextPosition
	if _, err := tx.Exec(ctx, `update threads set updated_at = now() where id = $1`, threadID); err != nil {
		return types.Message{}, err
	}
	message.Assets = []types.Asset{}
	for index, asset := range newAssets {
		assetID := "asset_" + uuid.NewString()
		created, err := scanAsset(tx.QueryRow(ctx, `
insert into assets (
  id, message_id, position, storage_key, file_name, mime_type, size_bytes,
  created_by, created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
returning id, message_id, storage_key, file_name, mime_type, size_bytes,
          created_at, created_by, created_by_user_id, created_by_key_id,
          created_by_user_display_name, created_by_actor_name,
          purged_at, purged_by_user_id, purge_last_attempt_at, purge_error
`, assetID, messageID, int64(index+1), asset.StorageKey, asset.FileName, asset.MimeType, asset.SizeBytes, auth.ActorName, userID, optionalString(auth.KeyID), optionalString(auth.UserDisplayName), optionalString(auth.ActorName)))
		if err != nil {
			return types.Message{}, err
		}
		created.Position = int64(index + 1)
		message.Assets = append(message.Assets, created)
	}
	return message, nil
}

func sameOptionalString(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return strings.TrimSpace(*left) == strings.TrimSpace(*right)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
