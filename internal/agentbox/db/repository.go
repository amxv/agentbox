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
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrOwnerAlreadyExists = errors.New("deployment owner already exists")

const ownerBootstrapAdvisoryLockID int64 = 0x4167656e744f776e

type Repository struct {
	pool *pgxpool.Pool
}

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

func (r *Repository) ListThreads(ctx context.Context, tenantID string, limit int) ([]types.Thread, error) {
	rows, err := r.pool.Query(ctx, `
select id, tenant_id, title, created_at, updated_at, created_by, created_by_user_id, created_by_key_id
from threads
where tenant_id = $1
order by updated_at desc
limit $2
`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var threads []types.Thread
	for rows.Next() {
		thread, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, thread)
	}
	return threads, rows.Err()
}

func (r *Repository) SearchThreads(ctx context.Context, tenantID string, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
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
  t.title,
  t.created_at,
  t.updated_at,
  t.created_by,
  count(m.id)::int as message_count,
  coalesce((select lm.body from messages lm where lm.tenant_id = t.tenant_id and lm.thread_id = t.id order by lm.created_at desc limit 1), '') as last_message_body,
  coalesce((select mm.body from messages mm where mm.tenant_id = t.tenant_id and mm.thread_id = t.id and mm.body ilike $1 order by mm.created_at desc limit 1), '') as matched_message_body
from threads t
left join messages m on m.tenant_id = t.tenant_id and m.thread_id = t.id
where t.tenant_id = $2
  and ($3::text is null or t.created_by = $3)
  and ($4::timestamptz is null or t.updated_at > $4)
  and (
    t.title ilike $1
    or exists (select 1 from messages sm where sm.tenant_id = t.tenant_id and sm.thread_id = t.id and sm.body ilike $1)
  )
group by t.id, t.tenant_id, t.title, t.created_at, t.updated_at, t.created_by
order by t.updated_at desc
limit $5
`, pattern, tenantID, createdBy, updatedAfter, params.Limit)
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
		var tenantID string
		if err := rows.Scan(&result.ID, &tenantID, &result.Title, &createdAt, &updatedAt, &result.CreatedBy, &result.MessageCount, &lastBody, &matchedBody); err != nil {
			return nil, err
		}
		result.TenantID = tenantID
		result.CreatedAt = isoMillis(createdAt)
		result.UpdatedAt = isoMillis(updatedAt)
		result.LastMessagePreview = previewText(lastBody, 180)
		result.MatchedSnippets = matchedSnippets(params.Query, result.Title, matchedBody)
		results = append(results, result)
	}
	return results, rows.Err()
}

func (r *Repository) CreateThread(ctx context.Context, tenantID string, title string, auth types.AuthContext) (types.Thread, error) {
	id := "thr_" + uuid.NewString()
	row := r.pool.QueryRow(ctx, `
insert into threads (id, tenant_id, title, created_by, created_by_user_id, created_by_key_id)
values ($1, $2, $3, $4, $5, $6)
returning id, tenant_id, title, created_at, updated_at, created_by, created_by_user_id, created_by_key_id
`, id, tenantID, title, auth.ActorName, optionalString(auth.UserID), optionalString(auth.KeyID))
	return scanThread(row)
}

func (r *Repository) CreateThreadWithMessage(ctx context.Context, tenantID string, title string, auth types.AuthContext, body string, bodyContentType *string) (types.Thread, types.Message, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	threadID := "thr_" + uuid.NewString()
	thread, err := scanThread(tx.QueryRow(ctx, `
insert into threads (id, tenant_id, title, created_by, created_by_user_id, created_by_key_id)
values ($1, $2, $3, $4, $5, $6)
returning id, tenant_id, title, created_at, updated_at, created_by, created_by_user_id, created_by_key_id
`, threadID, tenantID, title, auth.ActorName, optionalString(auth.UserID), optionalString(auth.KeyID)))
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	messageID := "msg_" + uuid.NewString()
	message, err := scanMessage(tx.QueryRow(ctx, `
insert into messages (id, tenant_id, thread_id, author, body, body_content_type, created_by_user_id, created_by_key_id)
values ($1, $2, $3, $4, $5, $6, $7, $8)
returning id, tenant_id, thread_id, author, body, body_content_type, created_at, created_by_user_id, created_by_key_id
`, messageID, tenantID, thread.ID, auth.ActorName, body, bodyContentType, optionalString(auth.UserID), optionalString(auth.KeyID)), nil)
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	if _, err := tx.Exec(ctx, `update threads set updated_at = now() where tenant_id = $1 and id = $2`, tenantID, thread.ID); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	return thread, message, nil
}

func (r *Repository) GetThread(ctx context.Context, tenantID string, threadID string) (*types.ThreadWithMessages, error) {
	thread, err := scanThread(r.pool.QueryRow(ctx, `
select id, tenant_id, title, created_at, updated_at, created_by, created_by_user_id, created_by_key_id
from threads
where tenant_id = $1 and id = $2
`, tenantID, threadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	messageRows, err := r.pool.Query(ctx, `
select id, tenant_id, thread_id, author, body, body_content_type, created_at, created_by_user_id, created_by_key_id
from messages
where tenant_id = $1 and thread_id = $2
order by created_at asc
`, tenantID, threadID)
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
select id, tenant_id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, created_by, created_by_user_id, created_by_key_id
from assets
where tenant_id = $1 and message_id = any($2)
order by created_at asc
`, tenantID, messageIDs)
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

	return &types.ThreadWithMessages{Thread: thread, Messages: messages}, nil
}

func (r *Repository) GetAsset(ctx context.Context, tenantID string, assetID string) (*types.Asset, error) {
	asset, err := scanAsset(r.pool.QueryRow(ctx, `
select id, tenant_id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, created_by, created_by_user_id, created_by_key_id
from assets
where tenant_id = $1 and id = $2
`, tenantID, assetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *Repository) CreatePendingUpload(ctx context.Context, upload types.PendingUpload) (types.PendingUpload, error) {
	return scanPendingUpload(r.pool.QueryRow(ctx, `
insert into pending_uploads (id, tenant_id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url, expires_at, created_by, created_by_user_id, created_by_key_id)
values ($1, coalesce(nullif($2, ''), 'ten_default'), $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
returning id, tenant_id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, expires_at, created_by, created_by_user_id, created_by_key_id, consumed_at
`, upload.ID, upload.TenantID, upload.ThreadID, upload.StorageKey, upload.FileName, upload.MimeType, upload.SizeBytes, upload.PublicURL, upload.ExpiresAt, upload.CreatedBy, upload.CreatedByUserID, upload.CreatedByKeyID))
}

func (r *Repository) GetPendingUploads(ctx context.Context, tenantID string, threadID string, uploadIDs []string, owner types.AuthContext) ([]types.PendingUpload, error) {
	if len(uploadIDs) == 0 {
		return []types.PendingUpload{}, nil
	}
	rows, err := r.pool.Query(ctx, `
select id, tenant_id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, expires_at, created_by, created_by_user_id, created_by_key_id, consumed_at
from pending_uploads
where tenant_id = $1
  and thread_id = $2
  and id = any($3)
  and created_by = $4
  and ($5::text is null or created_by_user_id = $5)
  and ($6::text is null or created_by_key_id = $6)
`, tenantID, threadID, uploadIDs, owner.ActorName, optionalString(owner.UserID), optionalString(owner.KeyID))
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

func (r *Repository) MarkPendingUploadsConsumed(ctx context.Context, tenantID string, threadID string, uploadIDs []string, owner types.AuthContext) error {
	if len(uploadIDs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
update pending_uploads
set consumed_at = now()
where tenant_id = $1
  and thread_id = $2
  and id = any($3)
  and created_by = $4
  and ($5::text is null or created_by_user_id = $5)
  and ($6::text is null or created_by_key_id = $6)
`, tenantID, threadID, uploadIDs, owner.ActorName, optionalString(owner.UserID), optionalString(owner.KeyID))
	return err
}

func (r *Repository) PostMessage(ctx context.Context, tenantID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, newAssets []types.NewAsset) (types.Message, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.Message{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	messageID := "msg_" + uuid.NewString()
	message, err := scanMessage(tx.QueryRow(ctx, `
insert into messages (id, tenant_id, thread_id, author, body, body_content_type, created_by_user_id, created_by_key_id)
values ($1, $2, $3, $4, $5, $6, $7, $8)
returning id, tenant_id, thread_id, author, body, body_content_type, created_at, created_by_user_id, created_by_key_id
`, messageID, tenantID, threadID, auth.ActorName, body, bodyContentType, optionalString(auth.UserID), optionalString(auth.KeyID)), nil)
	if err != nil {
		return types.Message{}, err
	}

	if _, err := tx.Exec(ctx, `update threads set updated_at = now() where tenant_id = $1 and id = $2`, tenantID, threadID); err != nil {
		return types.Message{}, err
	}

	message.Assets = []types.Asset{}
	for _, asset := range newAssets {
		assetID := "asset_" + uuid.NewString()
		created, err := scanAsset(tx.QueryRow(ctx, `
insert into assets (id, tenant_id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_by, created_by_user_id, created_by_key_id)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
returning id, tenant_id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, created_by, created_by_user_id, created_by_key_id
`, assetID, tenantID, messageID, asset.StorageKey, asset.FileName, asset.MimeType, asset.SizeBytes, asset.PublicURL, auth.ActorName, optionalString(auth.UserID), optionalString(auth.KeyID)))
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

	owner, err := scanUser(tx.QueryRow(ctx, `
select id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
from users
where is_owner
for update
`))
	if err == nil {
		if !strings.EqualFold(owner.Email, email) {
			return types.User{}, ErrOwnerAlreadyExists
		}
		owner, err = scanUser(tx.QueryRow(ctx, `
update users
set display_name = $1,
    password_hash = $2,
    role = 'admin',
    disabled_at = null,
    updated_at = now()
where id = $3
returning id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
`, displayName, passwordHash, owner.ID))
		if err != nil {
			return types.User{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return types.User{}, err
		}
		return owner, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return types.User{}, err
	}

	existing, err := scanUser(tx.QueryRow(ctx, `
select id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
from users
where lower(email) = lower($1)
for update
`, email))
	if err == nil {
		owner, err = scanUser(tx.QueryRow(ctx, `
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
		if err != nil {
			return types.User{}, err
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		owner, err = scanUser(tx.QueryRow(ctx, `
insert into users (id, tenant_id, email, display_name, password_hash, role, is_owner)
values ($1, $2, $3, $4, $5, 'admin', true)
returning id, tenant_id, email, display_name, password_hash, role, is_owner, created_at, updated_at, disabled_at
`, "usr_"+uuid.NewString(), types.DefaultTenantID, email, displayName, passwordHash))
		if err != nil {
			return types.User{}, err
		}
	} else {
		return types.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.User{}, err
	}
	return owner, nil
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
	err := row.Scan(&thread.ID, &thread.TenantID, &thread.Title, &createdAt, &updatedAt, &thread.CreatedBy, &thread.CreatedByUserID, &thread.CreatedByKeyID)
	thread.CreatedAt = isoMillis(createdAt)
	thread.UpdatedAt = isoMillis(updatedAt)
	return thread, err
}

func scanMessage(row threadScanner, assets []types.Asset) (types.Message, error) {
	var createdAt time.Time
	var bodyContentType *string
	var message types.Message
	err := row.Scan(&message.ID, &message.TenantID, &message.ThreadID, &message.Author, &message.Body, &bodyContentType, &createdAt, &message.CreatedByUserID, &message.CreatedByKeyID)
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
	var publicURL *string
	var asset types.Asset
	err := row.Scan(
		&asset.ID,
		&asset.TenantID,
		&asset.MessageID,
		&asset.StorageKey,
		&asset.FileName,
		&mimeType,
		&asset.SizeBytes,
		&publicURL,
		&createdAt,
		&asset.CreatedBy,
		&asset.CreatedByUserID,
		&asset.CreatedByKeyID,
	)
	asset.MimeType = mimeType
	asset.PublicURL = publicURL
	asset.Filename = asset.FileName
	asset.DownloadURL = publicURL
	asset.CreatedAt = isoMillis(createdAt)
	return asset, err
}

func scanPendingUpload(row threadScanner) (types.PendingUpload, error) {
	var createdAt time.Time
	var expiresAt time.Time
	var consumedAt *time.Time
	var mimeType *string
	var publicURL *string
	upload := types.PendingUpload{}
	err := row.Scan(
		&upload.ID,
		&upload.TenantID,
		&upload.ThreadID,
		&upload.StorageKey,
		&upload.FileName,
		&mimeType,
		&upload.SizeBytes,
		&publicURL,
		&createdAt,
		&expiresAt,
		&upload.CreatedBy,
		&upload.CreatedByUserID,
		&upload.CreatedByKeyID,
		&consumedAt,
	)
	upload.MimeType = mimeType
	upload.PublicURL = publicURL
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
