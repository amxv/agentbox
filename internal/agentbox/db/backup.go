package db

import (
	"context"
	"fmt"
	"sync"

	"agentbox/internal/agentbox/backup"
	"github.com/jackc/pgx/v5"
)

type contentSnapshot struct {
	transaction pgx.Tx
	data        backup.SnapshotData
	mutex       sync.Mutex
	closed      bool
}

func (r *Repository) OpenContentSnapshot(ctx context.Context) (backup.ContentSnapshot, error) {
	transaction, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("begin content snapshot: %w", err)
	}
	fail := func(err error) (backup.ContentSnapshot, error) {
		_ = transaction.Rollback(ctx)
		return nil, err
	}

	data := backup.SnapshotData{}
	if err := transaction.QueryRow(ctx, `select pg_export_snapshot()`).Scan(&data.SnapshotID); err != nil {
		return fail(fmt.Errorf("export PostgreSQL snapshot: %w", err))
	}
	if err := transaction.QueryRow(ctx, `
select
  (select count(*) from threads),
  (select count(*) from messages),
  (select count(*) from assets),
  (select count(*) from pending_uploads)
`).Scan(
		&data.Counts.Threads,
		&data.Counts.Messages,
		&data.Counts.Assets,
		&data.Counts.PendingUploads,
	); err != nil {
		return fail(fmt.Errorf("count content rows: %w", err))
	}
	if err := transaction.QueryRow(ctx, `
select
  (select count(*) from messages m left join threads t on t.id = m.thread_id where t.id is null),
  (select count(*) from assets a left join messages m on m.id = a.message_id where m.id is null),
  (select count(*) from pending_uploads p left join threads t on t.id = p.thread_id where t.id is null)
`).Scan(
		&data.Orphans.MessagesWithoutThread,
		&data.Orphans.AssetsWithoutMessage,
		&data.Orphans.PendingUploadsWithoutThread,
	); err != nil {
		return fail(fmt.Errorf("count orphaned content rows: %w", err))
	}

	rows, err := transaction.Query(ctx, `
select 'asset' as kind, id, storage_key, size_bytes::bigint
from assets
where purged_at is null
union all
select 'pending_upload' as kind, id, storage_key, size_bytes::bigint
from pending_uploads
order by kind, id
`)
	if err != nil {
		return fail(fmt.Errorf("list content object references: %w", err))
	}
	defer rows.Close()
	for rows.Next() {
		var reference backup.ObjectReference
		if err := rows.Scan(&reference.Kind, &reference.RecordID, &reference.StorageKey, &reference.SizeBytes); err != nil {
			return fail(fmt.Errorf("scan content object reference: %w", err))
		}
		data.References = append(data.References, reference)
	}
	if err := rows.Err(); err != nil {
		return fail(fmt.Errorf("iterate content object references: %w", err))
	}

	return &contentSnapshot{transaction: transaction, data: data}, nil
}

func (s *contentSnapshot) Data() backup.SnapshotData {
	return s.data
}

func (s *contentSnapshot) Close(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.transaction.Commit(ctx); err != nil {
		return fmt.Errorf("close content snapshot: %w", err)
	}
	return nil
}
