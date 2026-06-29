package db

import (
	"context"
	"errors"
	"fmt"
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
  created_at timestamptz not null default now()
);

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

create index if not exists threads_updated_at_idx on threads(updated_at desc);
create index if not exists messages_thread_created_idx on messages(thread_id, created_at asc);
create index if not exists assets_message_id_idx on assets(message_id);
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
select id, thread_id, author, body, created_at
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

func (r *Repository) PostMessage(ctx context.Context, threadID string, author string, body string, asset *types.NewAsset) (types.Message, error) {
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
insert into messages (id, thread_id, author, body)
values ($1, $2, $3, $4)
returning id, thread_id, author, body, created_at
`, messageID, threadID, author, body), nil)
	if err != nil {
		return types.Message{}, err
	}

	if _, err := tx.Exec(ctx, `update threads set updated_at = now() where id = $1`, threadID); err != nil {
		return types.Message{}, err
	}

	if asset != nil {
		assetID := "asset_" + uuid.NewString()
		created, err := scanAsset(tx.QueryRow(ctx, `
insert into assets (id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_by)
values ($1, $2, $3, $4, $5, $6, $7, $8)
returning id, message_id, storage_key, file_name, mime_type, size_bytes, public_url, created_at, created_by
`, assetID, messageID, asset.StorageKey, asset.FileName, asset.MimeType, asset.SizeBytes, asset.PublicURL, author))
		if err != nil {
			return types.Message{}, err
		}
		message.Assets = []types.Asset{created}
	}

	if err := tx.Commit(ctx); err != nil {
		return types.Message{}, err
	}
	return message, nil
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
	var message types.Message
	err := row.Scan(&message.ID, &message.ThreadID, &message.Author, &message.Body, &createdAt)
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
	asset.CreatedAt = isoMillis(createdAt)
	return asset, err
}

func StorageKey(threadID string, messageHint string, fileName string) string {
	return fmt.Sprintf("agentbox/%s/%s/%s", threadID, messageHint, fileName)
}

func isoMillis(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}
