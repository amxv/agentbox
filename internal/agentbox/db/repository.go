package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrOwnerAlreadyExists = types.ErrOwnerAlreadyExists
var ErrOwnerSetupTokenInvalid = types.ErrOwnerSetupTokenInvalid

const ownerBootstrapAdvisoryLockID int64 = 0x4167656e744f776e

type Repository struct {
	pool *pgxpool.Pool
}

// normalThreadAccessPredicate is the single normal-user authorization boundary.
// It is intentionally evaluated at query time so membership and visibility
// changes revoke or grant access immediately across every content path.
const normalThreadAccessPredicate = `(
  t.owner_user_id = $1
  or exists (
    select 1
    from thread_team_shares access_share
    join team_memberships access_membership
      on access_membership.team_id = access_share.team_id
    where access_share.thread_id = t.id
      and access_membership.user_id = $1
  )
)`

func Open(ctx context.Context, cfg config.Config) (*Repository, error) {
	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required.")
	}
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	poolConfig.MaxConns = cfg.DBPoolSize
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}

func (r *Repository) ResolveThreadAccess(ctx context.Context, userID string, threadID string) (*types.ThreadAccess, error) {
	var access types.ThreadAccess
	err := r.pool.QueryRow(ctx, `
select
  t.id,
  t.owner_user_id,
  coalesce(array(
    select access_share.team_id
    from thread_team_shares access_share
    join team_memberships access_membership
      on access_membership.team_id = access_share.team_id
    where access_share.thread_id = t.id
      and access_membership.user_id = $1
    order by access_share.team_id
  ), '{}'::text[])
from threads t
where `+normalThreadAccessPredicate+` and t.id = $2
`, userID, threadID).Scan(&access.ThreadID, &access.OwnerUserID, &access.MatchedTeamIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	access.UserID = userID
	access.IsOwner = access.OwnerUserID == userID
	return &access, nil
}

func (r *Repository) GetThreadVisibility(ctx context.Context, userID string, threadID string) (*types.ThreadVisibility, error) {
	var visibility types.ThreadVisibility
	err := r.pool.QueryRow(ctx, `
select t.id, t.owner_user_id
from threads t
where `+normalThreadAccessPredicate+` and t.id = $2
`, userID, threadID).Scan(&visibility.ThreadID, &visibility.OwnerUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	teams, err := r.listThreadTeams(ctx, threadID)
	if err != nil {
		return nil, err
	}
	visibility.SharedTeams = teams
	return &visibility, nil
}

func (r *Repository) SetThreadVisibility(ctx context.Context, userID string, threadID string, teamIDs []string) (types.ThreadVisibility, error) {
	teamIDs = uniqueNonEmptyStrings(teamIDs)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	defer tx.Rollback(ctx)

	visibility := types.ThreadVisibility{ThreadID: threadID}
	err = tx.QueryRow(ctx, `
select t.owner_user_id
from threads t
where `+normalThreadAccessPredicate+` and t.id = $2
for update
`, userID, threadID).Scan(&visibility.OwnerUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.ThreadVisibility{}, types.ErrThreadNotFound
	}
	if err != nil {
		return types.ThreadVisibility{}, err
	}

	teams, err := listTeamsByIDsTx(ctx, tx, teamIDs)
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	if _, err := tx.Exec(ctx, `
delete from thread_team_shares
where thread_id = $1
  and not (team_id = any($2::text[]))
`, threadID, teamIDs); err != nil {
		return types.ThreadVisibility{}, err
	}
	for _, team := range teams {
		if _, err := tx.Exec(ctx, `
insert into thread_team_shares (thread_id, team_id, created_by_user_id)
values ($1, $2, $3)
on conflict (thread_id, team_id) do nothing
`, threadID, team.ID, userID); err != nil {
			return types.ThreadVisibility{}, err
		}
	}
	visibility.SharedTeams = teams
	if err := tx.Commit(ctx); err != nil {
		return types.ThreadVisibility{}, err
	}
	return visibility, nil
}

func (r *Repository) listThreadTeams(ctx context.Context, threadID string) ([]types.Team, error) {
	rows, err := r.pool.Query(ctx, `
select t.id, t.slug, t.name, t.created_at, t.updated_at
from teams t
join thread_team_shares share on share.team_id = t.id
where share.thread_id = $1
order by lower(t.name), lower(t.slug), t.id
`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTeams(rows)
}

func (r *Repository) ListThreads(ctx context.Context, userID string, limit int) ([]types.Thread, error) {
	rows, err := r.pool.Query(ctx, `
select id, tenant_id, owner_user_id, title, created_at, updated_at, created_by,
       created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
from threads t
where `+normalThreadAccessPredicate+`
order by updated_at desc
limit $2
`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	threads := []types.Thread{}
	for rows.Next() {
		thread, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, thread)
	}
	return threads, rows.Err()
}

func (r *Repository) SearchThreads(ctx context.Context, userID string, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
	var createdBy any
	if params.CreatedBy != nil && *params.CreatedBy != "" {
		createdBy = *params.CreatedBy
	}
	var updatedAfter any
	if params.UpdatedAfter != nil && *params.UpdatedAfter != "" {
		parsed, err := time.Parse(time.RFC3339, *params.UpdatedAfter)
		if err != nil {
			return nil, err
		}
		updatedAfter = parsed
	}
	pattern := "%" + params.Query + "%"
	rows, err := r.pool.Query(ctx, `
select
  t.id,
  t.tenant_id,
  t.owner_user_id,
  t.title,
  t.created_at,
  t.updated_at,
  t.created_by,
  count(m.id)::int as message_count,
  coalesce((select lm.body from messages lm where lm.thread_id = t.id order by lm.created_at desc limit 1), '') as last_message_body,
  coalesce((select mm.body from messages mm where mm.thread_id = t.id and mm.body ilike $2 order by mm.created_at desc limit 1), '') as matched_message_body
from threads t
left join messages m on m.thread_id = t.id
where `+normalThreadAccessPredicate+`
  and ($3::text is null or t.created_by = $3)
  and ($4::timestamptz is null or t.updated_at > $4)
  and (
    t.title ilike $2
    or exists (select 1 from messages sm where sm.thread_id = t.id and sm.body ilike $2)
  )
group by t.id, t.tenant_id, t.owner_user_id, t.title, t.created_at, t.updated_at, t.created_by
order by t.updated_at desc
limit $5
`, userID, pattern, createdBy, updatedAfter, params.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []types.SearchThreadResult{}
	for rows.Next() {
		var createdAt time.Time
		var updatedAt time.Time
		var lastBody string
		var matchedBody string
		result := types.SearchThreadResult{}
		if err := rows.Scan(&result.ID, &result.TenantID, &result.OwnerUserID, &result.Title, &createdAt, &updatedAt, &result.CreatedBy, &result.MessageCount, &lastBody, &matchedBody); err != nil {
			return nil, err
		}
		result.CreatedAt = isoMillis(createdAt)
		result.UpdatedAt = isoMillis(updatedAt)
		result.LastMessagePreview = previewText(lastBody, 180)
		result.MatchedSnippets = matchedSnippets(params.Query, result.Title, matchedBody)
		results = append(results, result)
	}
	return results, rows.Err()
}

func (r *Repository) CreateThread(ctx context.Context, userID string, title string, auth types.AuthContext) (types.Thread, error) {
	id := "thr_" + uuid.NewString()
	return scanThread(r.pool.QueryRow(ctx, `
insert into threads (
  id, tenant_id, owner_user_id, title, created_by, created_by_user_id, created_by_key_id,
  created_by_user_display_name, created_by_actor_name
)
values ($1, 'ten_default', $2, $3, $4, $5, $6, $7, $8)
returning id, tenant_id, owner_user_id, title, created_at, updated_at, created_by,
          created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
`, id, userID, title, auth.ActorName, userID, optionalString(auth.KeyID), optionalString(auth.UserDisplayName), optionalString(auth.ActorName)))
}

func (r *Repository) CreateThreadWithMessage(ctx context.Context, userID string, title string, auth types.AuthContext, body string, bodyContentType *string) (types.Thread, types.Message, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	threadID := "thr_" + uuid.NewString()
	thread, err := scanThread(tx.QueryRow(ctx, `
insert into threads (
  id, tenant_id, owner_user_id, title, created_by, created_by_user_id, created_by_key_id,
  created_by_user_display_name, created_by_actor_name
)
values ($1, 'ten_default', $2, $3, $4, $5, $6, $7, $8)
returning id, tenant_id, owner_user_id, title, created_at, updated_at, created_by,
          created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
`, threadID, userID, title, auth.ActorName, userID, optionalString(auth.KeyID), optionalString(auth.UserDisplayName), optionalString(auth.ActorName)))
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}

	messageID := "msg_" + uuid.NewString()
	message, err := scanMessage(tx.QueryRow(ctx, `
insert into messages (
  id, tenant_id, thread_id, author, body, body_content_type, created_by_user_id, created_by_key_id,
  created_by_user_display_name, created_by_actor_name
)
values ($1, 'ten_default', $2, $3, $4, $5, $6, $7, $8, $9)
returning id, tenant_id, thread_id, author, body, body_content_type, created_at,
          created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
`, messageID, thread.ID, auth.ActorName, body, bodyContentType, userID, optionalString(auth.KeyID), optionalString(auth.UserDisplayName), optionalString(auth.ActorName)), nil)
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	if _, err := tx.Exec(ctx, `update threads t set updated_at = now() where `+normalThreadAccessPredicate+` and t.id = $2`, userID, thread.ID); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	return thread, message, nil
}

func (r *Repository) GetThread(ctx context.Context, userID string, threadID string) (*types.ThreadWithMessages, error) {
	thread, err := scanThread(r.pool.QueryRow(ctx, `
select t.id, t.tenant_id, t.owner_user_id, t.title, t.created_at, t.updated_at, t.created_by,
       t.created_by_user_id, t.created_by_key_id, t.created_by_user_display_name, t.created_by_actor_name
from threads t
where `+normalThreadAccessPredicate+` and t.id = $2
`, userID, threadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	messageRows, err := r.pool.Query(ctx, `
select id, tenant_id, thread_id, author, body, body_content_type, created_at,
       created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
from messages
where thread_id = $1
order by created_at asc
`, threadID)
	if err != nil {
		return nil, err
	}
	defer messageRows.Close()

	messages := []types.Message{}
	messageIDs := []string{}
	for messageRows.Next() {
		message, err := scanMessage(messageRows, nil)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
		messageIDs = append(messageIDs, message.ID)
	}
	if err := messageRows.Err(); err != nil {
		return nil, err
	}

	if len(messageIDs) > 0 {
		assetRows, err := r.pool.Query(ctx, `
select id, tenant_id, message_id, storage_key, file_name, mime_type, size_bytes, public_url,
       created_at, created_by, created_by_user_id, created_by_key_id,
       created_by_user_display_name, created_by_actor_name
from assets
where message_id = any($1)
order by created_at asc
`, messageIDs)
		if err != nil {
			return nil, err
		}
		defer assetRows.Close()

		assetsByMessage := map[string][]types.Asset{}
		for assetRows.Next() {
			asset, err := scanAsset(assetRows)
			if err != nil {
				return nil, err
			}
			assetsByMessage[asset.MessageID] = append(assetsByMessage[asset.MessageID], asset)
		}
		if err := assetRows.Err(); err != nil {
			return nil, err
		}
		for i := range messages {
			messages[i].Assets = assetsByMessage[messages[i].ID]
			if messages[i].Assets == nil {
				messages[i].Assets = []types.Asset{}
			}
		}
	}

	visibility, err := r.GetThreadVisibility(ctx, userID, threadID)
	if err != nil {
		return nil, err
	}
	if visibility == nil {
		return nil, nil
	}
	return &types.ThreadWithMessages{Thread: thread, Messages: messages, Visibility: *visibility}, nil
}

func (r *Repository) GetAsset(ctx context.Context, userID string, assetID string) (*types.Asset, error) {
	asset, err := scanAsset(r.pool.QueryRow(ctx, `
select a.id, a.tenant_id, a.message_id, a.storage_key, a.file_name, a.mime_type, a.size_bytes, a.public_url,
       a.created_at, a.created_by, a.created_by_user_id, a.created_by_key_id,
       a.created_by_user_display_name, a.created_by_actor_name
from assets a
join messages m on m.id = a.message_id
join threads t on t.id = m.thread_id
where `+normalThreadAccessPredicate+` and a.id = $2
`, userID, assetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *Repository) CreatePendingUpload(ctx context.Context, userID string, upload types.PendingUpload) (types.PendingUpload, error) {
	created, err := scanPendingUpload(r.pool.QueryRow(ctx, `
insert into pending_uploads (
  id, tenant_id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url, expires_at,
  created_by, created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
)
select $2, t.tenant_id, t.id, $4, $5, $6, $7, null, $8, $9, $1, $10, $11, $12
from threads t
where `+normalThreadAccessPredicate+` and t.id = $3
returning id, tenant_id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url,
          created_at, expires_at, created_by, created_by_user_id, created_by_key_id,
          created_by_user_display_name, created_by_actor_name, consumed_at
`, userID, upload.ID, upload.ThreadID, upload.StorageKey, upload.FileName, upload.MimeType, upload.SizeBytes, upload.ExpiresAt, upload.CreatedBy, upload.CreatedByKeyID, upload.CreatedByUserDisplayName, upload.CreatedByActorName))
	if errors.Is(err, pgx.ErrNoRows) {
		return types.PendingUpload{}, types.ErrThreadNotFound
	}
	return created, err
}

func (r *Repository) GetPendingUploads(ctx context.Context, userID string, threadID string, uploadIDs []string, actor types.AuthContext) ([]types.PendingUpload, error) {
	if len(uploadIDs) == 0 {
		return []types.PendingUpload{}, nil
	}
	rows, err := r.pool.Query(ctx, `
select p.id, p.tenant_id, p.thread_id, p.storage_key, p.file_name, p.mime_type, p.size_bytes, p.public_url,
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

func (r *Repository) PostMessage(ctx context.Context, userID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, newAssets []types.NewAsset) (types.Message, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tenantID string
	if err := tx.QueryRow(ctx, `
select t.tenant_id
from threads t
where `+normalThreadAccessPredicate+` and t.id = $2
for update
`, userID, threadID).Scan(&tenantID); errors.Is(err, pgx.ErrNoRows) {
		return types.Message{}, types.ErrThreadNotFound
	} else if err != nil {
		return types.Message{}, err
	}

	messageID := "msg_" + uuid.NewString()
	message, err := scanMessage(tx.QueryRow(ctx, `
insert into messages (
  id, tenant_id, thread_id, author, body, body_content_type, created_by_user_id, created_by_key_id,
  created_by_user_display_name, created_by_actor_name
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
returning id, tenant_id, thread_id, author, body, body_content_type, created_at,
          created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
`, messageID, tenantID, threadID, auth.ActorName, body, bodyContentType, userID, optionalString(auth.KeyID), optionalString(auth.UserDisplayName), optionalString(auth.ActorName)), nil)
	if err != nil {
		return types.Message{}, err
	}

	if _, err := tx.Exec(ctx, `update threads t set updated_at = now() where `+normalThreadAccessPredicate+` and t.id = $2`, userID, threadID); err != nil {
		return types.Message{}, err
	}

	message.Assets = []types.Asset{}
	for _, asset := range newAssets {
		assetID := "asset_" + uuid.NewString()
		created, err := scanAsset(tx.QueryRow(ctx, `
insert into assets (
  id, tenant_id, message_id, storage_key, file_name, mime_type, size_bytes, public_url,
  created_by, created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
)
values ($1, $2, $3, $4, $5, $6, $7, null, $8, $9, $10, $11, $12)
returning id, tenant_id, message_id, storage_key, file_name, mime_type, size_bytes, public_url,
          created_at, created_by, created_by_user_id, created_by_key_id,
          created_by_user_display_name, created_by_actor_name
`, assetID, tenantID, messageID, asset.StorageKey, asset.FileName, asset.MimeType, asset.SizeBytes, auth.ActorName, userID, optionalString(auth.KeyID), optionalString(auth.UserDisplayName), optionalString(auth.ActorName)))
		if err != nil {
			return types.Message{}, err
		}
		message.Assets = append(message.Assets, created)
	}

	if err := tx.Commit(ctx); err != nil {
		return types.Message{}, err
	}
	return message, nil
}

func (r *Repository) CreateAPIKey(ctx context.Context, userID string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string) (types.APIKey, error) {
	id := "key_" + uuid.NewString()
	created, err := scanAPIKey(r.pool.QueryRow(ctx, `
insert into api_keys (id, user_id, name, purpose, token_prefix, token_hash, scopes)
values ($1, $2, $3, $4, $5, $6, $7)
on conflict (user_id, lower(name)) where revoked_at is null do update
set purpose = excluded.purpose,
    token_prefix = excluded.token_prefix,
    token_hash = excluded.token_hash,
    scopes = excluded.scopes,
    updated_at = now(),
    last_used_at = null
returning id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
`, id, userID, name, purpose, tokenPrefix, tokenHash, scopes))
	if err != nil {
		return types.APIKey{}, err
	}
	return created, nil
}

func (r *Repository) CreateOnboardingCredential(ctx context.Context, userID string, connector string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string, rotate bool) (types.APIKey, types.OnboardingState, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
insert into user_onboarding (user_id)
values ($1)
on conflict (user_id) do nothing
`, userID); err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}

	var lockedUserID string
	if err := tx.QueryRow(ctx, `
select user_id
from user_onboarding
where user_id = $1
for update
`, userID).Scan(&lockedUserID); err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}
	if !rotate {
		var activeExists bool
		if err := tx.QueryRow(ctx, `
select exists (
  select 1
  from user_onboarding_steps s
  join api_keys k on k.id = s.credential_id
  where s.user_id = $1
    and s.connector = $2
    and k.revoked_at is null
)
`, userID, connector).Scan(&activeExists); err != nil {
			return types.APIKey{}, types.OnboardingState{}, err
		}
		if activeExists {
			return types.APIKey{}, types.OnboardingState{}, types.ErrOnboardingCredentialExists
		}
	}
	if _, err := tx.Exec(ctx, `
update user_onboarding
set dismissed_at = null,
    updated_at = now()
where user_id = $1
`, userID); err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}

	created, err := scanAPIKey(tx.QueryRow(ctx, `
insert into api_keys (id, user_id, name, purpose, token_prefix, token_hash, scopes)
values ($1, $2, $3, $4, $5, $6, $7)
on conflict (user_id, lower(name)) where revoked_at is null do update
set purpose = excluded.purpose,
    token_prefix = excluded.token_prefix,
    token_hash = excluded.token_hash,
    scopes = excluded.scopes,
    updated_at = now(),
    last_used_at = null
returning id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
`, "key_"+uuid.NewString(), userID, name, purpose, tokenPrefix, tokenHash, scopes))
	if err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}

	if _, err := tx.Exec(ctx, `
insert into user_onboarding_steps (user_id, connector, credential_id)
values ($1, $2, $3)
on conflict (user_id, connector) do update
set credential_id = excluded.credential_id,
    updated_at = now()
`, userID, connector, created.ID); err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}
	state, err := r.GetOnboardingState(ctx, userID)
	if err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}
	return created, state, nil
}

func (r *Repository) GetOnboardingState(ctx context.Context, userID string) (types.OnboardingState, error) {
	state := types.OnboardingState{UserID: userID, Steps: []types.OnboardingStep{}}
	var dismissedAt *time.Time
	var createdAt time.Time
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx, `
select dismissed_at, created_at, updated_at
from user_onboarding
where user_id = $1
`, userID).Scan(&dismissedAt, &createdAt, &updatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return types.OnboardingState{}, err
	}
	if err == nil {
		state.DismissedAt = optionalISOTime(dismissedAt)
		created := isoMillis(createdAt)
		updated := isoMillis(updatedAt)
		state.CreatedAt = &created
		state.UpdatedAt = &updated
	}

	rows, err := r.pool.Query(ctx, `
select connector, credential_id, completed_at, updated_at
from user_onboarding_steps
where user_id = $1
order by case connector when 'chatgpt' then 1 when 'claude' then 2 else 3 end
`, userID)
	if err != nil {
		return types.OnboardingState{}, err
	}
	defer rows.Close()
	type storedStep struct {
		step         types.OnboardingStep
		credentialID *string
	}
	stored := []storedStep{}
	for rows.Next() {
		var connector string
		var credentialID *string
		var completedAt time.Time
		var stepUpdatedAt time.Time
		if err := rows.Scan(&connector, &credentialID, &completedAt, &stepUpdatedAt); err != nil {
			return types.OnboardingState{}, err
		}
		completed := isoMillis(completedAt)
		updated := isoMillis(stepUpdatedAt)
		stored = append(stored, storedStep{
			step:         types.OnboardingStep{Connector: connector, CompletedAt: &completed, UpdatedAt: &updated},
			credentialID: credentialID,
		})
	}
	if err := rows.Err(); err != nil {
		return types.OnboardingState{}, err
	}

	keys, err := r.ListAPIKeys(ctx, userID)
	if err != nil {
		return types.OnboardingState{}, err
	}
	keysByID := make(map[string]types.APIKey, len(keys))
	for _, key := range keys {
		keysByID[key.ID] = key
	}
	for _, item := range stored {
		step := item.step
		if item.credentialID != nil {
			if key, ok := keysByID[*item.credentialID]; ok {
				keyCopy := key
				step.Credential = &keyCopy
			}
		}
		state.Steps = append(state.Steps, step)
	}
	return state, nil
}

func (r *Repository) DismissOnboarding(ctx context.Context, userID string) (types.OnboardingState, error) {
	if _, err := r.pool.Exec(ctx, `
insert into user_onboarding (user_id, dismissed_at)
values ($1, now())
on conflict (user_id) do update
set dismissed_at = now(),
    updated_at = now()
`, userID); err != nil {
		return types.OnboardingState{}, err
	}
	return r.GetOnboardingState(ctx, userID)
}

func (r *Repository) ListAPIKeys(ctx context.Context, userID string) ([]types.APIKey, error) {
	rows, err := r.pool.Query(ctx, `
select id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
from api_keys
where user_id = $1 and revoked_at is null
order by name asc
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := []types.APIKey{}
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (r *Repository) RevokeAPIKey(ctx context.Context, userID string, name string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `update api_keys set revoked_at = now(), updated_at = now() where user_id = $1 and lower(name) = lower($2) and revoked_at is null`, userID, name)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) FindAPIKeyBySecret(ctx context.Context, key string) (*types.APIKey, *types.User, error) {
	found, user, err := scanAPIKeyAndUser(r.pool.QueryRow(ctx, `
select
  k.id, k.user_id, k.name, k.purpose, k.token_prefix, k.token_hash, k.scopes,
  k.created_at, k.updated_at, k.last_used_at, k.revoked_at,
  u.id, u.tenant_id, u.email, u.display_name, u.password_hash, u.role, u.is_owner,
  u.created_at, u.updated_at, u.disabled_at
from api_keys k
join users u on u.id = k.user_id
where k.revoked_at is null
  and k.token_hash = $1
  and u.disabled_at is null
`, hashSecret(key)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &found, &user, nil
}

func (r *Repository) MarkAPIKeyUsed(ctx context.Context, keyID string) error {
	if keyID == "" {
		return nil
	}
	_, err := r.pool.Exec(ctx, `update api_keys set last_used_at = now() where id = $1 and revoked_at is null`, keyID)
	return err
}

func (r *Repository) UpsertTenant(ctx context.Context, tenant types.Tenant) (types.Tenant, error) {
	row := r.pool.QueryRow(ctx, `
insert into tenants (id, slug, name)
values ($1, $2, $3)
on conflict (slug) do update
set name = excluded.name, updated_at = now()
returning id, slug, name, created_at, updated_at
`, tenant.ID, tenant.Slug, tenant.Name)
	return scanTenant(row)
}

func (r *Repository) GetTenant(ctx context.Context, idOrSlug string) (*types.Tenant, error) {
	tenant, err := scanTenant(r.pool.QueryRow(ctx, `
select id, slug, name, created_at, updated_at
from tenants
where id = $1 or slug = $1
`, strings.TrimSpace(idOrSlug)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tenant, nil
}

func (r *Repository) BootstrapOwner(ctx context.Context, email string, displayName string, passwordHash string) (types.User, error) {
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if email == "" || displayName == "" || passwordHash == "" {
		return types.User{}, errors.New("owner email, display name, and password hash are required")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, ownerBootstrapAdvisoryLockID); err != nil {
		return types.User{}, fmt.Errorf("lock owner bootstrap: %w", err)
	}
	owner, err := bootstrapOwnerTx(ctx, tx, email, displayName, passwordHash, "")
	if err != nil {
		return types.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.User{}, err
	}
	return owner, nil
}

func (r *Repository) CreateOwnerSetupToken(ctx context.Context, tokenHash string, expiresAt time.Time) (types.OwnerSetupToken, error) {
	if strings.TrimSpace(tokenHash) == "" || !expiresAt.After(time.Now().UTC()) {
		return types.OwnerSetupToken{}, errors.New("owner setup token hash and future expiry are required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.OwnerSetupToken{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, ownerBootstrapAdvisoryLockID); err != nil {
		return types.OwnerSetupToken{}, fmt.Errorf("lock owner setup token: %w", err)
	}
	var ownerExists bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from users where is_owner)`).Scan(&ownerExists); err != nil {
		return types.OwnerSetupToken{}, err
	}
	purpose := "bootstrap"
	if ownerExists {
		purpose = "recovery"
	}
	if _, err := tx.Exec(ctx, `
update owner_setup_tokens
set revoked_at = now()
where consumed_at is null and revoked_at is null
`); err != nil {
		return types.OwnerSetupToken{}, err
	}
	token, err := scanOwnerSetupToken(tx.QueryRow(ctx, `
insert into owner_setup_tokens (id, token_hash, purpose, expires_at)
values ($1, $2, $3, $4)
returning id, purpose, created_at, expires_at, consumed_at, revoked_at
`, "ost_"+uuid.NewString(), tokenHash, purpose, expiresAt))
	if err != nil {
		return types.OwnerSetupToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.OwnerSetupToken{}, err
	}
	return token, nil
}

func (r *Repository) UseOwnerSetupToken(ctx context.Context, tokenHash string, email string, displayName string, passwordHash string) (types.User, types.OwnerSetupToken, error) {
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if tokenHash == "" || email == "" || displayName == "" || passwordHash == "" {
		return types.User{}, types.OwnerSetupToken{}, ErrOwnerSetupTokenInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.User{}, types.OwnerSetupToken{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, ownerBootstrapAdvisoryLockID); err != nil {
		return types.User{}, types.OwnerSetupToken{}, fmt.Errorf("lock owner setup: %w", err)
	}
	token, err := scanOwnerSetupToken(tx.QueryRow(ctx, `
update owner_setup_tokens
set consumed_at = now()
where token_hash = $1
  and consumed_at is null
  and revoked_at is null
  and expires_at > now()
returning id, purpose, created_at, expires_at, consumed_at, revoked_at
`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return types.User{}, types.OwnerSetupToken{}, ErrOwnerSetupTokenInvalid
	}
	if err != nil {
		return types.User{}, types.OwnerSetupToken{}, err
	}
	owner, err := bootstrapOwnerTx(ctx, tx, email, displayName, passwordHash, token.Purpose)
	if err != nil {
		return types.User{}, types.OwnerSetupToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.User{}, types.OwnerSetupToken{}, err
	}
	return owner, token, nil
}

func bootstrapOwnerTx(ctx context.Context, tx pgx.Tx, email string, displayName string, passwordHash string, requiredPurpose string) (types.User, error) {
	owner, err := scanUser(tx.QueryRow(ctx, `
select id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
from users
where is_owner
for update
`))
	if err == nil {
		if requiredPurpose == "bootstrap" {
			return types.User{}, ErrOwnerSetupTokenInvalid
		}
		if !strings.EqualFold(owner.Email, email) {
			return types.User{}, ErrOwnerAlreadyExists
		}
		return scanUser(tx.QueryRow(ctx, `
update users
set display_name = $1,
    password_hash = $2,
    role = 'admin',
    disabled_at = null,
    updated_at = now()
where id = $3
returning id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
`, displayName, passwordHash, owner.ID))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return types.User{}, err
	}
	if requiredPurpose == "recovery" {
		return types.User{}, ErrOwnerSetupTokenInvalid
	}

	existing, err := scanUser(tx.QueryRow(ctx, `
select id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
from users
where lower(email) = lower($1)
for update
`, email))
	if err == nil {
		return scanUser(tx.QueryRow(ctx, `
update users
set display_name = $1,
    password_hash = $2,
    role = 'admin',
    is_owner = true,
    disabled_at = null,
    updated_at = now()
where id = $3
returning id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
`, displayName, passwordHash, existing.ID))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return types.User{}, err
	}
	return scanUser(tx.QueryRow(ctx, `
insert into users (id, tenant_id, email, display_name, password_hash, role, is_owner)
values ($1, $2, $3, $4, $5, 'admin', true)
returning id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
`, "usr_"+uuid.NewString(), types.DefaultTenantID, email, displayName, passwordHash))
}

func (r *Repository) CreateSignupInvitation(ctx context.Context, createdByUserID string, tokenHash string, expiresAt time.Time, teamIDs []string) (types.SignupInvitation, error) {
	if strings.TrimSpace(createdByUserID) == "" || strings.TrimSpace(tokenHash) == "" || !expiresAt.After(time.Now().UTC()) {
		return types.SignupInvitation{}, errors.New("invitation creator, token hash, and future expiry are required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.SignupInvitation{}, err
	}
	defer tx.Rollback(ctx)
	teams, err := listTeamsByIDsTx(ctx, tx, teamIDs)
	if err != nil {
		return types.SignupInvitation{}, err
	}
	invitation, err := scanSignupInvitation(tx.QueryRow(ctx, `
insert into signup_invitations (id, token_hash, created_by_user_id, expires_at)
values ($1, $2, $3, $4)
returning id, created_by_user_id, created_at, expires_at, consumed_at, consumed_by_user_id, revoked_at
`, "inv_"+uuid.NewString(), tokenHash, createdByUserID, expiresAt))
	if err != nil {
		return types.SignupInvitation{}, err
	}
	for _, team := range teams {
		if _, err := tx.Exec(ctx, `
insert into signup_invitation_teams (invitation_id, team_id)
values ($1, $2)
`, invitation.ID, team.ID); err != nil {
			return types.SignupInvitation{}, err
		}
	}
	invitation.Teams = teams
	if err := tx.Commit(ctx); err != nil {
		return types.SignupInvitation{}, err
	}
	return invitation, nil
}

func (r *Repository) ListSignupInvitations(ctx context.Context) ([]types.SignupInvitation, error) {
	rows, err := r.pool.Query(ctx, `
select id, created_by_user_id, created_at, expires_at, consumed_at, consumed_by_user_id, revoked_at
from signup_invitations
order by created_at desc, id desc
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	invitations := []types.SignupInvitation{}
	for rows.Next() {
		invitation, err := scanSignupInvitation(rows)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for index := range invitations {
		teams, err := r.listInvitationTeams(ctx, invitations[index].ID)
		if err != nil {
			return nil, err
		}
		invitations[index].Teams = teams
	}
	return invitations, nil
}

func (r *Repository) RevokeSignupInvitation(ctx context.Context, invitationID string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
update signup_invitations
set revoked_at = now()
where id = $1 and consumed_at is null and revoked_at is null
`, strings.TrimSpace(invitationID))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) FindSignupInvitation(ctx context.Context, tokenHash string) (*types.SignupInvitation, error) {
	invitation, err := scanSignupInvitation(r.pool.QueryRow(ctx, `
select id, created_by_user_id, created_at, expires_at, consumed_at, consumed_by_user_id, revoked_at
from signup_invitations
where token_hash = $1
  and consumed_at is null
  and revoked_at is null
  and expires_at > now()
`, strings.TrimSpace(tokenHash)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	teams, err := r.listInvitationTeams(ctx, invitation.ID)
	if err != nil {
		return nil, err
	}
	invitation.Teams = teams
	return &invitation, nil
}

func (r *Repository) RegisterWithSignupInvitation(ctx context.Context, tokenHash string, email string, displayName string, passwordHash string, sessionSecretHash string, sessionExpiresAt time.Time) (types.User, types.UserSession, types.SignupInvitation, error) {
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if tokenHash == "" || email == "" || displayName == "" || passwordHash == "" || sessionSecretHash == "" || !sessionExpiresAt.After(time.Now().UTC()) {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, types.ErrSignupInvitationInvalid
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, err
	}
	defer tx.Rollback(ctx)

	invitation, err := scanSignupInvitation(tx.QueryRow(ctx, `
select id, created_by_user_id, created_at, expires_at, consumed_at, consumed_by_user_id, revoked_at
from signup_invitations
where token_hash = $1
  and consumed_at is null
  and revoked_at is null
  and expires_at > now()
for update
`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, types.ErrSignupInvitationInvalid
	}
	if err != nil {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, err
	}

	teams, err := listInvitationTeamsTx(ctx, tx, invitation.ID)
	if err != nil {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, err
	}

	user, err := scanUser(tx.QueryRow(ctx, `
insert into users (id, tenant_id, email, display_name, password_hash, role, is_owner)
values ($1, $2, $3, $4, $5, 'member', false)
returning id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
`, "usr_"+uuid.NewString(), types.DefaultTenantID, email, displayName, passwordHash))
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return types.User{}, types.UserSession{}, types.SignupInvitation{}, types.ErrEmailAlreadyRegistered
		}
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, err
	}

	for _, team := range teams {
		if _, err := tx.Exec(ctx, `
insert into team_memberships (team_id, user_id)
values ($1, $2)
`, team.ID, user.ID); err != nil {
			return types.User{}, types.UserSession{}, types.SignupInvitation{}, err
		}
	}

	session, err := scanUserSession(tx.QueryRow(ctx, `
insert into user_sessions (id, user_id, secret_hash, expires_at)
values ($1, $2, $3, $4)
returning id, user_id, secret_hash, created_at, last_used_at, expires_at, revoked_at
`, "sess_"+uuid.NewString(), user.ID, sessionSecretHash, sessionExpiresAt))
	if err != nil {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, err
	}

	invitation, err = scanSignupInvitation(tx.QueryRow(ctx, `
update signup_invitations
set consumed_at = now(), consumed_by_user_id = $1
where id = $2
returning id, created_by_user_id, created_at, expires_at, consumed_at, consumed_by_user_id, revoked_at
`, user.ID, invitation.ID))
	if err != nil {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, err
	}
	invitation.Teams = teams
	if err := tx.Commit(ctx); err != nil {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, err
	}
	return user, session, invitation, nil
}

func (r *Repository) CreateTeam(ctx context.Context, slug string, name string) (types.Team, error) {
	team, err := scanTeam(r.pool.QueryRow(ctx, `
insert into teams (id, slug, name)
values ($1, $2, $3)
returning id, slug, name, created_at, updated_at
`, "team_"+uuid.NewString(), strings.TrimSpace(slug), strings.TrimSpace(name)))
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return types.Team{}, types.ErrTeamSlugConflict
		}
		return types.Team{}, err
	}
	return team, nil
}

func (r *Repository) RenameTeam(ctx context.Context, teamID string, name string) (types.Team, error) {
	team, err := scanTeam(r.pool.QueryRow(ctx, `
update teams
set name = $2, updated_at = now()
where id = $1
returning id, slug, name, created_at, updated_at
`, strings.TrimSpace(teamID), strings.TrimSpace(name)))
	if errors.Is(err, pgx.ErrNoRows) {
		return types.Team{}, types.ErrTeamNotFound
	}
	return team, err
}

func (r *Repository) ListTeams(ctx context.Context) ([]types.Team, error) {
	rows, err := r.pool.Query(ctx, `
select id, slug, name, created_at, updated_at
from teams
order by lower(name), lower(slug), id
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	teams := []types.Team{}
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func (r *Repository) ListUserTeams(ctx context.Context, userID string) ([]types.Team, error) {
	rows, err := r.pool.Query(ctx, `
select t.id, t.slug, t.name, t.created_at, t.updated_at
from teams t
join team_memberships tm on tm.team_id = t.id
where tm.user_id = $1
order by lower(t.name), lower(t.slug), t.id
`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	teams := []types.Team{}
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func (r *Repository) ListTeamMembers(ctx context.Context, teamID string) ([]types.User, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `select exists (select 1 from teams where id = $1)`, strings.TrimSpace(teamID)).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, types.ErrTeamNotFound
	}
	rows, err := r.pool.Query(ctx, `
select u.id, u.tenant_id, u.email, u.display_name, u.password_hash, u.role, u.is_owner,
       u.created_at, u.updated_at, u.disabled_at
from users u
join team_memberships tm on tm.user_id = u.id
where tm.team_id = $1
order by u.is_owner desc, lower(u.display_name), lower(u.email), u.id
`, strings.TrimSpace(teamID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []types.User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (r *Repository) AddTeamMember(ctx context.Context, teamID string, userID string) (types.TeamMembership, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.TeamMembership{}, err
	}
	defer tx.Rollback(ctx)
	if err := requireTeamAndUserTx(ctx, tx, teamID, userID); err != nil {
		return types.TeamMembership{}, err
	}
	if _, err := tx.Exec(ctx, `
insert into team_memberships (team_id, user_id)
values ($1, $2)
on conflict (team_id, user_id) do nothing
`, strings.TrimSpace(teamID), strings.TrimSpace(userID)); err != nil {
		return types.TeamMembership{}, err
	}
	membership, err := scanTeamMembership(tx.QueryRow(ctx, `
select team_id, user_id, created_at
from team_memberships
where team_id = $1 and user_id = $2
`, strings.TrimSpace(teamID), strings.TrimSpace(userID)))
	if err != nil {
		return types.TeamMembership{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.TeamMembership{}, err
	}
	return membership, nil
}

func (r *Repository) RemoveTeamMember(ctx context.Context, teamID string, userID string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err := requireTeamAndUserTx(ctx, tx, teamID, userID); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `delete from team_memberships where team_id = $1 and user_id = $2`, strings.TrimSpace(teamID), strings.TrimSpace(userID))
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) listInvitationTeams(ctx context.Context, invitationID string) ([]types.Team, error) {
	rows, err := r.pool.Query(ctx, `
select t.id, t.slug, t.name, t.created_at, t.updated_at
from teams t
join signup_invitation_teams sit on sit.team_id = t.id
where sit.invitation_id = $1
order by lower(t.name), lower(t.slug), t.id
`, invitationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTeams(rows)
}

func listTeamsByIDsTx(ctx context.Context, tx pgx.Tx, teamIDs []string) ([]types.Team, error) {
	teamIDs = uniqueNonEmptyStrings(teamIDs)
	if len(teamIDs) == 0 {
		return []types.Team{}, nil
	}
	rows, err := tx.Query(ctx, `
select id, slug, name, created_at, updated_at
from teams
where id = any($1)
order by lower(name), lower(slug), id
for key share
`, teamIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	teams, err := scanTeams(rows)
	if err != nil {
		return nil, err
	}
	if len(teams) != len(teamIDs) {
		return nil, types.ErrTeamNotFound
	}
	return teams, nil
}

func listInvitationTeamsTx(ctx context.Context, tx pgx.Tx, invitationID string) ([]types.Team, error) {
	rows, err := tx.Query(ctx, `
select t.id, t.slug, t.name, t.created_at, t.updated_at
from teams t
join signup_invitation_teams sit on sit.team_id = t.id
where sit.invitation_id = $1
order by lower(t.name), lower(t.slug), t.id
for key share of t
`, invitationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTeams(rows)
}

func requireTeamAndUserTx(ctx context.Context, tx pgx.Tx, teamID string, userID string) error {
	var lockedTeamID string
	if err := tx.QueryRow(ctx, `select id from teams where id = $1 for key share`, strings.TrimSpace(teamID)).Scan(&lockedTeamID); errors.Is(err, pgx.ErrNoRows) {
		return types.ErrTeamNotFound
	} else if err != nil {
		return err
	}
	var lockedUserID string
	if err := tx.QueryRow(ctx, `select id from users where id = $1 for key share`, strings.TrimSpace(userID)).Scan(&lockedUserID); errors.Is(err, pgx.ErrNoRows) {
		return types.ErrUserNotFound
	} else if err != nil {
		return err
	}
	return nil
}

func (r *Repository) ListUsers(ctx context.Context) ([]types.User, error) {
	rows, err := r.pool.Query(ctx, `
select id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
from users
order by is_owner desc, created_at asc, id asc
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []types.User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func (r *Repository) SetUserDisabled(ctx context.Context, userID string, disabled bool) (types.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.User{}, err
	}
	defer tx.Rollback(ctx)
	user, err := scanUser(tx.QueryRow(ctx, `
select id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
from users
where id = $1
for update
`, strings.TrimSpace(userID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return types.User{}, types.ErrUserNotFound
	}
	if err != nil {
		return types.User{}, err
	}
	if disabled && user.IsOwner {
		return types.User{}, types.ErrOwnerCannotBeDisabled
	}
	if disabled {
		user, err = scanUser(tx.QueryRow(ctx, `
update users
set disabled_at = coalesce(disabled_at, now()), updated_at = now()
where id = $1
returning id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
`, user.ID))
		if err != nil {
			return types.User{}, err
		}
		if _, err := tx.Exec(ctx, `update user_sessions set revoked_at = coalesce(revoked_at, now()) where user_id = $1`, user.ID); err != nil {
			return types.User{}, err
		}
		if _, err := tx.Exec(ctx, `update api_keys set revoked_at = coalesce(revoked_at, now()), updated_at = now() where user_id = $1`, user.ID); err != nil {
			return types.User{}, err
		}
		if _, err := tx.Exec(ctx, `update cli_login_codes set consumed_at = coalesce(consumed_at, now()) where user_id = $1`, user.ID); err != nil {
			return types.User{}, err
		}
	} else {
		user, err = scanUser(tx.QueryRow(ctx, `
update users
set disabled_at = null, updated_at = now()
where id = $1
returning id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
`, user.ID))
		if err != nil {
			return types.User{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return types.User{}, err
	}
	return user, nil
}

func (r *Repository) UpsertProvisionedUser(ctx context.Context, tenantID string, email string, displayName string, passwordHash *string, role string) (types.User, error) {
	row := r.pool.QueryRow(ctx, `
insert into users (id, tenant_id, email, display_name, password_hash, role)
values ($1, $2, $3, $4, $5, $6)
on conflict (lower(email)) do update
set
  display_name = excluded.display_name,
  password_hash = coalesce(excluded.password_hash, users.password_hash),
  role = excluded.role,
  updated_at = now(),
  disabled_at = null
returning id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
`, "usr_"+uuid.NewString(), tenantID, strings.TrimSpace(email), strings.TrimSpace(displayName), passwordHash, role)
	return scanUser(row)
}

func (r *Repository) FindUserByEmail(ctx context.Context, _ string, email string) (*types.User, error) {
	user, err := scanUser(r.pool.QueryRow(ctx, `
select id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
from users
where disabled_at is null
  and lower(email) = lower($1)
`, strings.TrimSpace(email)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) CreateUserSession(ctx context.Context, session types.UserSession) (types.UserSession, error) {
	id := session.ID
	if id == "" {
		id = "sess_" + uuid.NewString()
	}
	return scanUserSession(r.pool.QueryRow(ctx, `
insert into user_sessions (id, user_id, secret_hash, expires_at)
values ($1, $2, $3, $4)
returning id, user_id, secret_hash, created_at, last_used_at, expires_at, revoked_at
`, id, session.UserID, session.SecretHash, session.ExpiresAt))
}

func (r *Repository) FindUserSessionBySecretHash(ctx context.Context, secretHash string) (*types.UserSession, *types.User, error) {
	row := r.pool.QueryRow(ctx, `
select
  s.id,
  s.user_id,
  s.secret_hash,
  s.created_at,
  s.last_used_at,
  s.expires_at,
  s.revoked_at,
  u.id,
  u.tenant_id,
  u.email,
  u.display_name,
  u.password_hash,
  u.role,
  u.is_owner,
  u.created_at,
  u.updated_at,
  u.disabled_at
from user_sessions s
join users u on u.id = s.user_id
where s.secret_hash = $1
  and s.revoked_at is null
  and s.expires_at > now()
  and u.disabled_at is null
`, secretHash)
	session, user, err := scanUserSessionAndUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &session, &user, nil
}

func (r *Repository) MarkUserSessionUsed(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	_, err := r.pool.Exec(ctx, `update user_sessions set last_used_at = now() where id = $1 and revoked_at is null`, sessionID)
	return err
}

func (r *Repository) RevokeUserSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	_, err := r.pool.Exec(ctx, `update user_sessions set revoked_at = now() where id = $1 and revoked_at is null`, sessionID)
	return err
}

func (r *Repository) CreateCLILoginCode(ctx context.Context, code types.CLILoginCode) (types.CLILoginCode, error) {
	id := code.ID
	if id == "" {
		id = "clicode_" + uuid.NewString()
	}
	return scanCLILoginCode(r.pool.QueryRow(ctx, `
insert into cli_login_codes (id, user_id, code_hash, state_hash, redirect_uri, expires_at)
values ($1, $2, $3, $4, $5, $6)
returning id, user_id, code_hash, state_hash, redirect_uri, created_at, expires_at, consumed_at
`, id, code.UserID, code.CodeHash, code.StateHash, code.RedirectURI, code.ExpiresAt))
}

func (r *Repository) ConsumeCLILoginCode(ctx context.Context, codeHash string, stateHash string, redirectURI string) (*types.CLILoginCode, *types.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
update cli_login_codes
set consumed_at = now()
where code_hash = $1
  and state_hash = $2
  and redirect_uri = $3
  and consumed_at is null
  and expires_at > now()
returning id, user_id, code_hash, state_hash, redirect_uri, created_at, expires_at, consumed_at
`, codeHash, stateHash, redirectURI)
	code, err := scanCLILoginCode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	user, err := scanUser(tx.QueryRow(ctx, `
select id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
from users
where id = $1 and disabled_at is null
`, code.UserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return &code, &user, nil
}

type threadScanner interface {
	Scan(dest ...any) error
}

func scanThread(row threadScanner) (types.Thread, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var thread types.Thread
	err := row.Scan(
		&thread.ID,
		&thread.TenantID,
		&thread.OwnerUserID,
		&thread.Title,
		&createdAt,
		&updatedAt,
		&thread.CreatedBy,
		&thread.CreatedByUserID,
		&thread.CreatedByKeyID,
		&thread.CreatedByUserDisplayName,
		&thread.CreatedByActorName,
	)
	thread.CreatedAt = isoMillis(createdAt)
	thread.UpdatedAt = isoMillis(updatedAt)
	return thread, err
}

func scanMessage(row threadScanner, assets []types.Asset) (types.Message, error) {
	var createdAt time.Time
	var bodyContentType *string
	var message types.Message
	err := row.Scan(
		&message.ID,
		&message.TenantID,
		&message.ThreadID,
		&message.Author,
		&message.Body,
		&bodyContentType,
		&createdAt,
		&message.CreatedByUserID,
		&message.CreatedByKeyID,
		&message.CreatedByUserDisplayName,
		&message.CreatedByActorName,
	)
	message.BodyContentType = bodyContentType
	message.CreatedAt = isoMillis(createdAt)
	if assets == nil {
		message.Assets = []types.Asset{}
	} else {
		message.Assets = assets
	}
	return message, err
}

func scanAsset(row threadScanner) (types.Asset, error) {
	var createdAt time.Time
	var mimeType *string
	var ignoredPublicURL *string
	var asset types.Asset
	err := row.Scan(
		&asset.ID,
		&asset.TenantID,
		&asset.MessageID,
		&asset.StorageKey,
		&asset.FileName,
		&mimeType,
		&asset.SizeBytes,
		&ignoredPublicURL,
		&createdAt,
		&asset.CreatedBy,
		&asset.CreatedByUserID,
		&asset.CreatedByKeyID,
		&asset.CreatedByUserDisplayName,
		&asset.CreatedByActorName,
	)
	asset.MimeType = mimeType
	asset.PublicURL = nil
	asset.Filename = asset.FileName
	asset.DownloadURL = nil
	asset.CreatedAt = isoMillis(createdAt)
	return asset, err
}

func scanPendingUpload(row threadScanner) (types.PendingUpload, error) {
	var createdAt time.Time
	var expiresAt time.Time
	var consumedAt *time.Time
	var mimeType *string
	var ignoredPublicURL *string
	upload := types.PendingUpload{}
	err := row.Scan(
		&upload.ID,
		&upload.TenantID,
		&upload.ThreadID,
		&upload.StorageKey,
		&upload.FileName,
		&mimeType,
		&upload.SizeBytes,
		&ignoredPublicURL,
		&createdAt,
		&expiresAt,
		&upload.CreatedBy,
		&upload.CreatedByUserID,
		&upload.CreatedByKeyID,
		&upload.CreatedByUserDisplayName,
		&upload.CreatedByActorName,
		&consumedAt,
	)
	upload.MimeType = mimeType
	upload.PublicURL = nil
	upload.CreatedAt = isoMillis(createdAt)
	upload.ExpiresAt = isoMillis(expiresAt)
	if consumedAt != nil {
		value := isoMillis(*consumedAt)
		upload.ConsumedAt = &value
	}
	return upload, err
}

func scanAPIKey(row threadScanner) (types.APIKey, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var lastUsedAt *time.Time
	var revokedAt *time.Time
	key := types.APIKey{}
	err := row.Scan(&key.ID, &key.UserID, &key.Name, &key.Purpose, &key.TokenPrefix, &key.TokenHash, &key.Scopes, &createdAt, &updatedAt, &lastUsedAt, &revokedAt)
	key.KeyMasked = maskSecret(key.TokenPrefix)
	key.CreatedAt = isoMillis(createdAt)
	key.UpdatedAt = isoMillis(updatedAt)
	if lastUsedAt != nil {
		value := isoMillis(*lastUsedAt)
		key.LastUsedAt = &value
	}
	if revokedAt != nil {
		value := isoMillis(*revokedAt)
		key.RevokedAt = &value
	}
	return key, err
}

func scanAPIKeyAndUser(row threadScanner) (types.APIKey, types.User, error) {
	var keyCreatedAt time.Time
	var keyUpdatedAt time.Time
	var keyLastUsedAt *time.Time
	var keyRevokedAt *time.Time
	var userCreatedAt time.Time
	var userUpdatedAt time.Time
	var userDisabledAt *time.Time
	key := types.APIKey{}
	user := types.User{}
	err := row.Scan(
		&key.ID,
		&key.UserID,
		&key.Name,
		&key.Purpose,
		&key.TokenPrefix,
		&key.TokenHash,
		&key.Scopes,
		&keyCreatedAt,
		&keyUpdatedAt,
		&keyLastUsedAt,
		&keyRevokedAt,
		&user.ID,
		&user.TenantID,
		&user.Email,
		&user.DisplayName,
		&user.PasswordHash,
		&user.Role,
		&user.IsOwner,
		&userCreatedAt,
		&userUpdatedAt,
		&userDisabledAt,
	)
	key.KeyMasked = maskSecret(key.TokenPrefix)
	key.CreatedAt = isoMillis(keyCreatedAt)
	key.UpdatedAt = isoMillis(keyUpdatedAt)
	if keyLastUsedAt != nil {
		value := isoMillis(*keyLastUsedAt)
		key.LastUsedAt = &value
	}
	if keyRevokedAt != nil {
		value := isoMillis(*keyRevokedAt)
		key.RevokedAt = &value
	}
	user.CreatedAt = isoMillis(userCreatedAt)
	user.UpdatedAt = isoMillis(userUpdatedAt)
	if userDisabledAt != nil {
		value := isoMillis(*userDisabledAt)
		user.DisabledAt = &value
	}
	return key, user, err
}

func scanTenant(row threadScanner) (types.Tenant, error) {
	var createdAt time.Time
	var updatedAt time.Time
	tenant := types.Tenant{}
	err := row.Scan(&tenant.ID, &tenant.Slug, &tenant.Name, &createdAt, &updatedAt)
	tenant.CreatedAt = isoMillis(createdAt)
	tenant.UpdatedAt = isoMillis(updatedAt)
	return tenant, err
}

func scanUser(row threadScanner) (types.User, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var disabledAt *time.Time
	user := types.User{}
	err := row.Scan(&user.ID, &user.TenantID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.Role, &user.IsOwner, &createdAt, &updatedAt, &disabledAt)
	user.CreatedAt = isoMillis(createdAt)
	user.UpdatedAt = isoMillis(updatedAt)
	if disabledAt != nil {
		value := isoMillis(*disabledAt)
		user.DisabledAt = &value
	}
	return user, err
}

func scanUserSession(row threadScanner) (types.UserSession, error) {
	var createdAt time.Time
	var lastUsedAt *time.Time
	var expiresAt time.Time
	var revokedAt *time.Time
	session := types.UserSession{}
	err := row.Scan(&session.ID, &session.UserID, &session.SecretHash, &createdAt, &lastUsedAt, &expiresAt, &revokedAt)
	session.CreatedAt = isoMillis(createdAt)
	session.ExpiresAt = isoMillis(expiresAt)
	if lastUsedAt != nil {
		value := isoMillis(*lastUsedAt)
		session.LastUsedAt = &value
	}
	if revokedAt != nil {
		value := isoMillis(*revokedAt)
		session.RevokedAt = &value
	}
	return session, err
}

func scanUserSessionAndUser(row threadScanner) (types.UserSession, types.User, error) {
	var sessionCreatedAt time.Time
	var sessionLastUsedAt *time.Time
	var expiresAt time.Time
	var revokedAt *time.Time
	var userCreatedAt time.Time
	var userUpdatedAt time.Time
	var disabledAt *time.Time
	session := types.UserSession{}
	user := types.User{}
	err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.SecretHash,
		&sessionCreatedAt,
		&sessionLastUsedAt,
		&expiresAt,
		&revokedAt,
		&user.ID,
		&user.TenantID,
		&user.Email,
		&user.DisplayName,
		&user.PasswordHash,
		&user.Role,
		&user.IsOwner,
		&userCreatedAt,
		&userUpdatedAt,
		&disabledAt,
	)
	session.CreatedAt = isoMillis(sessionCreatedAt)
	session.ExpiresAt = isoMillis(expiresAt)
	if sessionLastUsedAt != nil {
		value := isoMillis(*sessionLastUsedAt)
		session.LastUsedAt = &value
	}
	if revokedAt != nil {
		value := isoMillis(*revokedAt)
		session.RevokedAt = &value
	}
	user.CreatedAt = isoMillis(userCreatedAt)
	user.UpdatedAt = isoMillis(userUpdatedAt)
	if disabledAt != nil {
		value := isoMillis(*disabledAt)
		user.DisabledAt = &value
	}
	return session, user, err
}

func scanCLILoginCode(row threadScanner) (types.CLILoginCode, error) {
	var createdAt time.Time
	var expiresAt time.Time
	var consumedAt *time.Time
	code := types.CLILoginCode{}
	err := row.Scan(&code.ID, &code.UserID, &code.CodeHash, &code.StateHash, &code.RedirectURI, &createdAt, &expiresAt, &consumedAt)
	code.CreatedAt = isoMillis(createdAt)
	code.ExpiresAt = isoMillis(expiresAt)
	if consumedAt != nil {
		value := isoMillis(*consumedAt)
		code.ConsumedAt = &value
	}
	return code, err
}

func scanOwnerSetupToken(row threadScanner) (types.OwnerSetupToken, error) {
	var createdAt time.Time
	var expiresAt time.Time
	var consumedAt *time.Time
	var revokedAt *time.Time
	token := types.OwnerSetupToken{}
	err := row.Scan(&token.ID, &token.Purpose, &createdAt, &expiresAt, &consumedAt, &revokedAt)
	token.CreatedAt = isoMillis(createdAt)
	token.ExpiresAt = isoMillis(expiresAt)
	if consumedAt != nil {
		value := isoMillis(*consumedAt)
		token.ConsumedAt = &value
	}
	if revokedAt != nil {
		value := isoMillis(*revokedAt)
		token.RevokedAt = &value
	}
	return token, err
}

func scanSignupInvitation(row threadScanner) (types.SignupInvitation, error) {
	var createdAt time.Time
	var expiresAt time.Time
	var consumedAt *time.Time
	var revokedAt *time.Time
	invitation := types.SignupInvitation{}
	err := row.Scan(
		&invitation.ID,
		&invitation.CreatedByUserID,
		&createdAt,
		&expiresAt,
		&consumedAt,
		&invitation.ConsumedByUserID,
		&revokedAt,
	)
	invitation.CreatedAt = isoMillis(createdAt)
	invitation.ExpiresAt = isoMillis(expiresAt)
	if consumedAt != nil {
		value := isoMillis(*consumedAt)
		invitation.ConsumedAt = &value
	}
	if revokedAt != nil {
		value := isoMillis(*revokedAt)
		invitation.RevokedAt = &value
	}
	return invitation, err
}

func scanTeam(row threadScanner) (types.Team, error) {
	var createdAt time.Time
	var updatedAt time.Time
	team := types.Team{}
	err := row.Scan(&team.ID, &team.Slug, &team.Name, &createdAt, &updatedAt)
	team.CreatedAt = isoMillis(createdAt)
	team.UpdatedAt = isoMillis(updatedAt)
	return team, err
}

func scanTeams(rows pgx.Rows) ([]types.Team, error) {
	teams := []types.Team{}
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func scanTeamMembership(row threadScanner) (types.TeamMembership, error) {
	var createdAt time.Time
	membership := types.TeamMembership{}
	err := row.Scan(&membership.TeamID, &membership.UserID, &createdAt)
	membership.CreatedAt = isoMillis(createdAt)
	return membership, err
}

func hashSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func tokenPrefix(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func maskSecret(value string) string {
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalISOTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := isoMillis(*value)
	return &formatted
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func StorageKey(threadID string, messageHint string, fileName string) string {
	return fmt.Sprintf("agentbox/%s/%s/%s", threadID, messageHint, fileName)
}

func isoMillis(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}

func previewText(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func matchedSnippets(query string, title string, body string) []string {
	snippets := []string{}
	if strings.Contains(strings.ToLower(title), strings.ToLower(query)) {
		snippets = append(snippets, previewText(title, 180))
	}
	if body != "" {
		snippets = append(snippets, previewText(body, 240))
	}
	return snippets
}
