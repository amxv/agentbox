package db

import (
	"context"
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
create table if not exists threads (
  id text primary key,
  title text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  created_by text not null
);

create table if not exists messages (
  id text primary key,
  thread_id text not null references threads(id) on delete cascade,
  author text not null,
  body text not null,
  body_content_type text,
  created_at timestamptz not null default now()
);

alter table messages add column if not exists body_content_type text;

create table if not exists assets (
  id text primary key,
  message_id text not null references messages(id) on delete cascade,
  storage_key text not null,
  file_name text not null,
  mime_type text,
  size_bytes integer not null,
  public_url text,
  created_at timestamptz not null default now(),
  created_by text not null
);

create table if not exists api_keys (
  name text primary key,
  key_value text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists pending_uploads (
  id text primary key,
  thread_id text not null references threads(id) on delete cascade,
  storage_key text not null unique,
  file_name text not null,
  mime_type text,
  size_bytes integer not null,
  public_url text,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  created_by text not null,
  consumed_at timestamptz
);

create index if not exists threads_updated_at_idx on threads(updated_at desc);
create index if not exists messages_thread_created_idx on messages(thread_id, created_at asc);
create index if not exists assets_message_id_idx on assets(message_id);
create index if not exists pending_uploads_thread_idx on pending_uploads(thread_id, created_at desc);
`)
	return err
}

func (r *Repository) ListThreads(ctx context.Context, limit int) ([]types.Thread, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
select id, title, created_at, updated_at, created_by
from threads
order by updated_at desc
limit $1
`, limit)
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

func (r *Repository) SearchThreads(ctx context.Context, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
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
  t.title,
  t.created_at,
  t.updated_at,
  t.created_by,
  count(m.id)::int as message_count,
  coalesce((select lm.body from messages lm where lm.thread_id = t.id order by lm.created_at desc limit 1), '') as last_message_body,
  coalesce((select mm.body from messages mm where mm.thread_id = t.id and mm.body ilike $1 order by mm.created_at desc limit 1), '') as matched_message_body
from threads t
left join messages m on m.thread_id = t.id
where ($2::text is null or t.created_by = $2)
  and ($3::timestamptz is null or t.updated_at > $3)
  and (
    t.title ilike $1
    or exists (select 1 from messages sm where sm.thread_id = t.id and sm.body ilike $1)
  )
group by t.id, t.title, t.created_at, t.updated_at, t.created_by
order by t.updated_at desc
limit $4
`, pattern, createdBy, updatedAfter, params.Limit)
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
		if err := rows.Scan(&result.ID, &result.Title, &createdAt, &updatedAt, &result.CreatedBy, &result.MessageCount, &lastBody, &matchedBody); err != nil {
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

func (r *Repository) CreateThread(ctx context.Context, title string, author string) (types.Thread, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return types.Thread{}, err
	}
	id := "thr_" + uuid.NewString()
	row := r.pool.QueryRow(ctx, `
insert into threads (id, title, created_by)
values ($1, $2, $3)
returning id, title, created_at, updated_at, created_by
`, id, title, author)
	return scanThread(row)
}

func (r *Repository) CreateThreadWithMessage(ctx context.Context, title string, author string, body string, bodyContentType *string) (types.Thread, types.Message, error) {
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
insert into threads (id, title, created_by)
values ($1, $2, $3)
returning id, title, created_at, updated_at, created_by
`, threadID, title, author))
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	messageID := "msg_" + uuid.NewString()
	message, err := scanMessage(tx.QueryRow(ctx, `
insert into messages (id, thread_id, author, body, body_content_type)
values ($1, $2, $3, $4, $5)
returning id, thread_id, author, body, body_content_type, created_at
`, messageID, thread.ID, author, body, bodyContentType), nil)
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	if _, err := tx.Exec(ctx, `update threads set updated_at = now() where id = $1`, thread.ID); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	return thread, message, nil
}

func (r *Repository) GetThread(ctx context.Context, threadID string) (*types.ThreadWithMessages, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	thread, err := scanThread(r.pool.QueryRow(ctx, `
select id, title, created_at, updated_at, created_by
from threads
where id = $1
`, threadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	messageRows, err := r.pool.Query(ctx, `
select id, thread_id, author, body, body_content_type, created_at
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
select id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, created_by
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

	return &types.ThreadWithMessages{Thread: thread, Messages: messages}, nil
}

func (r *Repository) GetAsset(ctx context.Context, assetID string) (*types.Asset, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	asset, err := scanAsset(r.pool.QueryRow(ctx, `
select id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, created_by
from assets
where id = $1
`, assetID))
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
insert into pending_uploads (id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url, expires_at, created_by)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
returning id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, expires_at, created_by, consumed_at
`, upload.ID, upload.ThreadID, upload.StorageKey, upload.FileName, upload.MimeType, upload.SizeBytes, upload.PublicURL, upload.ExpiresAt, upload.CreatedBy))
}

func (r *Repository) GetPendingUploads(ctx context.Context, threadID string, uploadIDs []string, author string) ([]types.PendingUpload, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	if len(uploadIDs) == 0 {
		return []types.PendingUpload{}, nil
	}
	rows, err := r.pool.Query(ctx, `
select id, thread_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, expires_at, created_by, consumed_at
from pending_uploads
where thread_id = $1 and created_by = $2 and id = any($3)
`, threadID, author, uploadIDs)
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

func (r *Repository) MarkPendingUploadsConsumed(ctx context.Context, uploadIDs []string) error {
	if len(uploadIDs) == 0 {
		return nil
	}
	if err := r.EnsureSchema(ctx); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `update pending_uploads set consumed_at = now() where id = any($1)`, uploadIDs)
	return err
}

func (r *Repository) PostMessage(ctx context.Context, threadID string, author string, body string, bodyContentType *string, newAssets []types.NewAsset) (types.Message, error) {
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
insert into messages (id, thread_id, author, body, body_content_type)
values ($1, $2, $3, $4, $5)
returning id, thread_id, author, body, body_content_type, created_at
`, messageID, threadID, author, body, bodyContentType), nil)
	if err != nil {
		return types.Message{}, err
	}

	if _, err := tx.Exec(ctx, `update threads set updated_at = now() where id = $1`, threadID); err != nil {
		return types.Message{}, err
	}

	message.Assets = []types.Asset{}
	for _, asset := range newAssets {
		assetID := "asset_" + uuid.NewString()
		created, err := scanAsset(tx.QueryRow(ctx, `
insert into assets (id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_by)
values ($1, $2, $3, $4, $5, $6, $7, $8)
returning id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, created_by
`, assetID, messageID, asset.StorageKey, asset.FileName, asset.MimeType, asset.SizeBytes, asset.PublicURL, author))
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

func (r *Repository) CreateAPIKey(ctx context.Context, name string, key string) (types.APIKey, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return types.APIKey{}, err
	}
	row := r.pool.QueryRow(ctx, `
insert into api_keys (name, key_value)
values ($1, $2)
on conflict (name) do update set key_value = excluded.key_value, updated_at = now()
returning name, key_value, created_at, updated_at
`, name, key)
	return scanAPIKey(row)
}

func (r *Repository) ListAPIKeys(ctx context.Context) ([]types.APIKey, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
select name, key_value, created_at, updated_at
from api_keys
order by name asc
`)
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

func (r *Repository) RevokeAPIKey(ctx context.Context, name string) (bool, error) {
	if err := r.EnsureSchema(ctx); err != nil {
		return false, err
	}
	tag, err := r.pool.Exec(ctx, `delete from api_keys where name = $1`, name)
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
select name, key_value, created_at, updated_at
from api_keys
where key_value = $1
`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &found, nil
}

type threadScanner interface {
	Scan(dest ...any) error
}

func scanThread(row threadScanner) (types.Thread, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var thread types.Thread
	err := row.Scan(&thread.ID, &thread.Title, &createdAt, &updatedAt, &thread.CreatedBy)
	thread.CreatedAt = isoMillis(createdAt)
	thread.UpdatedAt = isoMillis(updatedAt)
	return thread, err
}

func scanMessage(row threadScanner, assets []types.Asset) (types.Message, error) {
	var createdAt time.Time
	var bodyContentType *string
	var message types.Message
	err := row.Scan(&message.ID, &message.ThreadID, &message.Author, &message.Body, &bodyContentType, &createdAt)
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
		&asset.MessageID,
		&asset.StorageKey,
		&asset.FileName,
		&mimeType,
		&asset.SizeBytes,
		&publicURL,
		&createdAt,
		&asset.CreatedBy,
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
		&upload.ThreadID,
		&upload.StorageKey,
		&upload.FileName,
		&mimeType,
		&upload.SizeBytes,
		&publicURL,
		&createdAt,
		&expiresAt,
		&upload.CreatedBy,
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
	key := types.APIKey{}
	err := row.Scan(&key.Name, &key.Key, &createdAt, &updatedAt)
	key.KeyMasked = maskSecret(key.Key)
	key.CreatedAt = isoMillis(createdAt)
	key.UpdatedAt = isoMillis(updatedAt)
	return key, err
}

func maskSecret(value string) string {
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "..." + value[len(value)-4:]
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
