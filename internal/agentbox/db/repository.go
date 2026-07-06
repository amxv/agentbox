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

func (r *Repository) EnsureSchema(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `
create extension if not exists pgcrypto;

create table if not exists tenants (
  id text primary key,
  slug text not null unique,
  name text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

insert into tenants (id, slug, name)
values ('ten_default', 'default', 'Default')
on conflict (id) do nothing;

create table if not exists users (
  id text primary key,
  tenant_id text not null references tenants(id) on delete cascade,
  email text not null,
  display_name text not null,
  password_hash text,
  role text not null default 'member',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  disabled_at timestamptz
);

create table if not exists user_sessions (
  id text primary key,
  tenant_id text not null references tenants(id) on delete cascade,
  user_id text not null references users(id) on delete cascade,
  secret_hash text not null unique,
  created_at timestamptz not null default now(),
  last_used_at timestamptz,
  expires_at timestamptz not null,
  revoked_at timestamptz
);

create table if not exists cli_login_codes (
  id text primary key,
  tenant_id text not null references tenants(id) on delete cascade,
  user_id text not null references users(id) on delete cascade,
  code_hash text not null unique,
  state_hash text not null,
  redirect_uri text not null,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  consumed_at timestamptz
);

create table if not exists threads (
  id text primary key,
  tenant_id text not null default 'ten_default' references tenants(id),
  title text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  created_by text not null,
  created_by_user_id text,
  created_by_key_id text
);

create table if not exists messages (
  id text primary key,
  tenant_id text not null default 'ten_default' references tenants(id),
  thread_id text not null references threads(id) on delete cascade,
  author text not null,
  body text not null,
  body_content_type text,
  created_at timestamptz not null default now(),
  created_by_user_id text,
  created_by_key_id text
);

alter table messages add column if not exists body_content_type text;

create table if not exists assets (
  id text primary key,
  tenant_id text not null default 'ten_default' references tenants(id),
  message_id text not null references messages(id) on delete cascade,
  storage_key text not null,
  file_name text not null,
  mime_type text,
  size_bytes integer not null,
  public_url text,
  created_at timestamptz not null default now(),
  created_by text not null,
  created_by_user_id text,
  created_by_key_id text
);

create table if not exists api_keys (
  id text primary key,
  tenant_id text not null default 'ten_default' references tenants(id),
  user_id text references users(id),
  name text not null,
  key_value text,
  token_prefix text not null,
  token_hash text not null unique,
  scopes text[] not null default array['threads:read','threads:write','assets:read','assets:write','mcp:use'],
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  last_used_at timestamptz,
  revoked_at timestamptz
);

create table if not exists pending_uploads (
  id text primary key,
  tenant_id text not null default 'ten_default' references tenants(id),
  thread_id text not null references threads(id) on delete cascade,
  storage_key text not null unique,
  file_name text not null,
  mime_type text,
  size_bytes integer not null,
  public_url text,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  created_by text not null,
  created_by_user_id text,
  created_by_key_id text,
  consumed_at timestamptz
);

alter table threads add column if not exists tenant_id text;
alter table threads add column if not exists created_by_user_id text;
alter table threads add column if not exists created_by_key_id text;
update threads set tenant_id = 'ten_default' where tenant_id is null;
alter table threads alter column tenant_id set default 'ten_default';
alter table threads alter column tenant_id set not null;

alter table messages add column if not exists tenant_id text;
alter table messages add column if not exists created_by_user_id text;
alter table messages add column if not exists created_by_key_id text;
update messages m set tenant_id = coalesce(t.tenant_id, 'ten_default') from threads t where m.thread_id = t.id and m.tenant_id is null;
update messages set tenant_id = 'ten_default' where tenant_id is null;
alter table messages alter column tenant_id set default 'ten_default';
alter table messages alter column tenant_id set not null;

alter table assets add column if not exists tenant_id text;
alter table assets add column if not exists created_by_user_id text;
alter table assets add column if not exists created_by_key_id text;
update assets a set tenant_id = coalesce(m.tenant_id, 'ten_default') from messages m where a.message_id = m.id and a.tenant_id is null;
update assets set tenant_id = 'ten_default' where tenant_id is null;
alter table assets alter column tenant_id set default 'ten_default';
alter table assets alter column tenant_id set not null;

alter table pending_uploads add column if not exists tenant_id text;
alter table pending_uploads add column if not exists created_by_user_id text;
alter table pending_uploads add column if not exists created_by_key_id text;
update pending_uploads p set tenant_id = coalesce(t.tenant_id, 'ten_default') from threads t where p.thread_id = t.id and p.tenant_id is null;
update pending_uploads set tenant_id = 'ten_default' where tenant_id is null;
alter table pending_uploads alter column tenant_id set default 'ten_default';
alter table pending_uploads alter column tenant_id set not null;

alter table api_keys add column if not exists id text;
alter table api_keys add column if not exists tenant_id text;
alter table api_keys add column if not exists user_id text;
alter table api_keys add column if not exists key_value text;
alter table api_keys add column if not exists token_prefix text;
alter table api_keys add column if not exists token_hash text;
alter table api_keys add column if not exists scopes text[] not null default array['threads:read','threads:write','assets:read','assets:write','mcp:use'];
alter table api_keys add column if not exists last_used_at timestamptz;
alter table api_keys add column if not exists revoked_at timestamptz;
update api_keys
set
  id = coalesce(id, 'key_' || replace(gen_random_uuid()::text, '-', '')),
  tenant_id = coalesce(tenant_id, 'ten_default'),
  token_prefix = coalesce(token_prefix, left(key_value, 8)),
  token_hash = coalesce(token_hash, encode(digest(key_value, 'sha256'), 'hex'))
where id is null or tenant_id is null or token_prefix is null or token_hash is null;
alter table api_keys alter column id set not null;
alter table api_keys alter column tenant_id set default 'ten_default';
alter table api_keys alter column tenant_id set not null;
alter table api_keys alter column token_prefix set not null;
alter table api_keys alter column token_hash set not null;
alter table api_keys alter column key_value drop not null;

do $$
declare
  pk_name text;
begin
  select conname into pk_name
  from pg_constraint
  where conrelid = 'api_keys'::regclass and contype = 'p';

  if pk_name is not null then
    if not exists (
      select 1
      from pg_attribute a
      join pg_constraint c on c.conrelid = a.attrelid and a.attnum = any(c.conkey)
      where c.conrelid = 'api_keys'::regclass
        and c.contype = 'p'
        and a.attname = 'id'
    ) then
      execute format('alter table api_keys drop constraint %I', pk_name);
    end if;
  end if;

  if not exists (
    select 1 from pg_constraint
    where conrelid = 'api_keys'::regclass and contype = 'p'
  ) then
    alter table api_keys add constraint api_keys_pkey primary key (id);
  end if;
end $$;

create index if not exists threads_updated_at_idx on threads(updated_at desc);
create index if not exists messages_thread_created_idx on messages(thread_id, created_at asc);
create index if not exists assets_message_id_idx on assets(message_id);
create index if not exists pending_uploads_thread_idx on pending_uploads(thread_id, created_at desc);
create unique index if not exists users_tenant_email_idx on users (tenant_id, lower(email));
create index if not exists users_tenant_id_idx on users (tenant_id);
create index if not exists user_sessions_tenant_user_idx on user_sessions (tenant_id, user_id);
create index if not exists user_sessions_expires_idx on user_sessions (expires_at);
create index if not exists cli_login_codes_tenant_user_idx on cli_login_codes (tenant_id, user_id);
create index if not exists cli_login_codes_expires_idx on cli_login_codes (expires_at);
create unique index if not exists api_keys_token_hash_idx on api_keys (token_hash);
create unique index if not exists api_keys_tenant_active_name_idx on api_keys (tenant_id, lower(name)) where revoked_at is null;
create index if not exists api_keys_tenant_id_idx on api_keys (tenant_id);
create index if not exists api_keys_user_id_idx on api_keys (user_id);
create index if not exists threads_tenant_updated_idx on threads (tenant_id, updated_at desc);
create index if not exists messages_tenant_thread_created_idx on messages (tenant_id, thread_id, created_at asc);
create index if not exists assets_tenant_message_id_idx on assets (tenant_id, message_id);
create index if not exists pending_uploads_tenant_thread_idx on pending_uploads (tenant_id, thread_id, created_at desc);
`)
	return err
}

func (r *Repository) ListThreads(ctx context.Context, tenantID string, limit int) ([]types.Thread, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
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
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
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
	if err := r.EnsureSchema(ctx); err != nil {
		return types.Thread{}, err
	}
	id := "thr_" + uuid.NewString()
	row := r.pool.QueryRow(ctx, `
insert into threads (id, tenant_id, title, created_by, created_by_user_id, created_by_key_id)
values ($1, $2, $3, $4, $5, $6)
returning id, tenant_id, title, created_at, updated_at, created_by, created_by_user_id, created_by_key_id
`, id, tenantID, title, auth.ActorName, optionalString(auth.UserID), optionalString(auth.KeyID))
	return scanThread(row)
}

func (r *Repository) CreateThreadWithMessage(ctx context.Context, tenantID string, title string, auth types.AuthContext, body string, bodyContentType *string) (types.Thread, types.Message, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return types.Thread{}, types.Message{}, err
	}
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
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
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
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
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
	if err := r.EnsureSchema(ctx); err != nil {
		return types.PendingUpload{}, err
	}
	return scanPendingUpload(r.pool.QueryRow(ctx, `
insert into pending_uploads (id, tenant_id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url, expires_at, created_by, created_by_user_id, created_by_key_id)
values ($1, coalesce(nullif($2, ''), 'ten_default'), $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
returning id, tenant_id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, expires_at, created_by, created_by_user_id, created_by_key_id, consumed_at
`, upload.ID, upload.TenantID, upload.ThreadID, upload.StorageKey, upload.FileName, upload.MimeType, upload.SizeBytes, upload.PublicURL, upload.ExpiresAt, upload.CreatedBy, upload.CreatedByUserID, upload.CreatedByKeyID))
}

func (r *Repository) GetPendingUploads(ctx context.Context, tenantID string, threadID string, uploadIDs []string, owner types.AuthContext) ([]types.PendingUpload, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
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
	if err := r.EnsureSchema(ctx); err != nil {
		return err
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
	if err := r.EnsureSchema(ctx); err != nil {
		return types.Message{}, err
	}
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

func (r *Repository) CreateAPIKey(ctx context.Context, tenantID string, name string, key string, tokenHash string, tokenPrefix string) (types.APIKey, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return types.APIKey{}, err
	}
	id := "key_" + uuid.NewString()
	tag, err := r.pool.Exec(ctx, `
update api_keys
set token_prefix = $1, token_hash = $2, updated_at = now(), revoked_at = null
where tenant_id = $3 and lower(name) = lower($4) and revoked_at is null
`, tokenPrefix, tokenHash, tenantID, name)
	if err != nil {
		return types.APIKey{}, err
	}
	if tag.RowsAffected() == 0 {
		_, err = r.pool.Exec(ctx, `
insert into api_keys (id, tenant_id, name, token_prefix, token_hash)
values ($1, $2, $3, $4, $5)
`, id, tenantID, name, tokenPrefix, tokenHash)
		if err != nil {
			return types.APIKey{}, err
		}
	}
	row := r.pool.QueryRow(ctx, `
select id, tenant_id, user_id, name, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
from api_keys
where tenant_id = $1 and lower(name) = lower($2) and revoked_at is null
`, tenantID, name)
	created, err := scanAPIKey(row)
	if err != nil {
		return types.APIKey{}, err
	}
	created.Key = key
	created.KeyMasked = maskSecret(key)
	return created, nil
}

func (r *Repository) ListAPIKeys(ctx context.Context, tenantID string) ([]types.APIKey, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
select id, tenant_id, user_id, name, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
from api_keys
where tenant_id = $1 and revoked_at is null
order by name asc
`, tenantID)
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

func (r *Repository) RevokeAPIKey(ctx context.Context, tenantID string, name string) (bool, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return false, err
	}
	tag, err := r.pool.Exec(ctx, `update api_keys set revoked_at = now(), updated_at = now() where tenant_id = $1 and lower(name) = lower($2) and revoked_at is null`, tenantID, name)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) FindAPIKeyBySecret(ctx context.Context, key string) (*types.APIKey, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	found, err := scanAPIKey(r.pool.QueryRow(ctx, `
select id, tenant_id, user_id, name, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
from api_keys
where revoked_at is null and (token_hash = $1 or key_value = $2)
`, hashSecret(key), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &found, nil
}

func (r *Repository) MarkAPIKeyUsed(ctx context.Context, keyID string) error {
	if keyID == "" {
		return nil
	}
	if err := r.EnsureSchema(ctx); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `update api_keys set last_used_at = now() where id = $1 and revoked_at is null`, keyID)
	return err
}

func (r *Repository) UpsertTenant(ctx context.Context, tenant types.Tenant) (types.Tenant, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return types.Tenant{}, err
	}
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
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
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

func (r *Repository) UpsertProvisionedUser(ctx context.Context, tenantID string, email string, displayName string, passwordHash *string, role string) (types.User, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return types.User{}, err
	}
	row := r.pool.QueryRow(ctx, `
insert into users (id, tenant_id, email, display_name, password_hash, role)
values ($1, $2, $3, $4, $5, $6)
on conflict (tenant_id, lower(email)) do update
set
  display_name = excluded.display_name,
  password_hash = coalesce(excluded.password_hash, users.password_hash),
  role = excluded.role,
  updated_at = now(),
  disabled_at = null
returning id, tenant_id, email, display_name, password_hash, role, created_at, updated_at, disabled_at
`, "usr_"+uuid.NewString(), tenantID, strings.TrimSpace(email), strings.TrimSpace(displayName), passwordHash, role)
	return scanUser(row)
}

func (r *Repository) FindUserByEmail(ctx context.Context, tenantID string, email string) (*types.User, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
select id, tenant_id, email, display_name, password_hash, role, created_at, updated_at, disabled_at
from users
where disabled_at is null
  and lower(email) = lower($1)
  and ($2::text = '' or tenant_id = $2)
order by created_at asc
limit 2
`, strings.TrimSpace(email), strings.TrimSpace(tenantID))
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
	if len(users) == 0 {
		return nil, nil
	}
	if len(users) > 1 {
		return nil, errors.New("Multiple users match that email. Specify a tenant.")
	}
	return &users[0], nil
}

func (r *Repository) CreateUserSession(ctx context.Context, session types.UserSession) (types.UserSession, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return types.UserSession{}, err
	}
	id := session.ID
	if id == "" {
		id = "sess_" + uuid.NewString()
	}
	return scanUserSession(r.pool.QueryRow(ctx, `
insert into user_sessions (id, tenant_id, user_id, secret_hash, expires_at)
values ($1, $2, $3, $4, $5)
returning id, tenant_id, user_id, secret_hash, created_at, last_used_at, expires_at, revoked_at
`, id, session.TenantID, session.UserID, session.SecretHash, session.ExpiresAt))
}

func (r *Repository) FindUserSessionBySecretHash(ctx context.Context, secretHash string) (*types.UserSession, *types.User, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, nil, err
	}
	row := r.pool.QueryRow(ctx, `
select
  s.id,
  s.tenant_id,
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
  u.created_at,
  u.updated_at,
  u.disabled_at
from user_sessions s
join users u on u.tenant_id = s.tenant_id and u.id = s.user_id
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
	if err := r.EnsureSchema(ctx); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `update user_sessions set last_used_at = now() where id = $1 and revoked_at is null`, sessionID)
	return err
}

func (r *Repository) RevokeUserSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	if err := r.EnsureSchema(ctx); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `update user_sessions set revoked_at = now() where id = $1 and revoked_at is null`, sessionID)
	return err
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
	err := row.Scan(&key.ID, &key.TenantID, &key.UserID, &key.Name, &key.TokenPrefix, &key.TokenHash, &key.Scopes, &createdAt, &updatedAt, &lastUsedAt, &revokedAt)
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
	err := row.Scan(&user.ID, &user.TenantID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.Role, &createdAt, &updatedAt, &disabledAt)
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
	err := row.Scan(&session.ID, &session.TenantID, &session.UserID, &session.SecretHash, &createdAt, &lastUsedAt, &expiresAt, &revokedAt)
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
		&session.TenantID,
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
