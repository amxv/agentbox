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

	hasPurgedAt, err := snapshotColumnExists(ctx, transaction, "assets", "purged_at")
	if err != nil {
		return fail(err)
	}
	hasOwnerUserID, err := snapshotColumnExists(ctx, transaction, "threads", "owner_user_id")
	if err != nil {
		return fail(err)
	}
	hasUploadCleanupObjects, err := snapshotTableExists(ctx, transaction, "upload_cleanup_objects")
	if err != nil {
		return fail(err)
	}

	assetPredicate := "true"
	if hasPurgedAt {
		assetPredicate = "purged_at is null"
	}
	referenceQuery := fmt.Sprintf(`
select 'asset' as kind, id, storage_key, size_bytes::bigint, true as missing_blocks_readiness
from assets
where %s
union all
select 'pending_upload' as kind,
       p.id || ':' || c.object_kind as id,
       c.storage_key,
       p.size_bytes::bigint,
       false as missing_blocks_readiness
from upload_cleanup_objects c
join pending_uploads p on p.id = c.upload_id
where c.cleaned_at is null
  and not exists (select 1 from assets a where a.storage_key = c.storage_key and %s)
order by kind, id
`, assetPredicate, assetPredicate)
	if !hasUploadCleanupObjects {
		referenceQuery = fmt.Sprintf(`
select 'asset' as kind, id, storage_key, size_bytes::bigint, true as missing_blocks_readiness
from assets
where %s
union all
select 'pending_upload' as kind, p.id, p.storage_key, p.size_bytes::bigint, false as missing_blocks_readiness
from pending_uploads p
where p.consumed_at is null
  and p.expires_at > now()
  and not exists (select 1 from assets a where a.storage_key = p.storage_key and %s)
order by kind, id
`, assetPredicate, assetPredicate)
	}
	rows, err := transaction.Query(ctx, referenceQuery)
	if err != nil {
		return fail(fmt.Errorf("list content object references: %w", err))
	}
	for rows.Next() {
		var reference backup.ObjectReference
		if err := rows.Scan(&reference.Kind, &reference.RecordID, &reference.StorageKey, &reference.SizeBytes, &reference.MissingBlocks); err != nil {
			rows.Close()
			return fail(fmt.Errorf("scan content object reference: %w", err))
		}
		data.References = append(data.References, reference)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fail(fmt.Errorf("iterate content object references: %w", err))
	}
	rows.Close()

	ownershipQuery := `select id, '' from threads order by id`
	if hasOwnerUserID {
		ownershipQuery = `select id, coalesce(owner_user_id, '') from threads order by id`
	}
	ownershipRows, err := transaction.Query(ctx, ownershipQuery)
	if err != nil {
		return fail(fmt.Errorf("list thread ownership for proposed owner backfill: %w", err))
	}
	for ownershipRows.Next() {
		var ownership backup.ThreadOwnership
		if err := ownershipRows.Scan(&ownership.ThreadID, &ownership.OwnerUserID); err != nil {
			ownershipRows.Close()
			return fail(fmt.Errorf("scan thread ownership: %w", err))
		}
		data.ThreadOwnership = append(data.ThreadOwnership, ownership)
	}
	if err := ownershipRows.Err(); err != nil {
		ownershipRows.Close()
		return fail(fmt.Errorf("iterate thread ownership: %w", err))
	}
	ownershipRows.Close()

	return &contentSnapshot{transaction: transaction, data: data}, nil
}

func snapshotColumnExists(ctx context.Context, transaction pgx.Tx, tableName string, columnName string) (bool, error) {
	var exists bool
	if err := transaction.QueryRow(ctx, `
select exists (
  select 1
  from information_schema.columns
  where table_schema = current_schema()
    and table_name = $1
    and column_name = $2
)
`, tableName, columnName).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect %s.%s: %w", tableName, columnName, err)
	}
	return exists, nil
}

func snapshotTableExists(ctx context.Context, transaction pgx.Tx, tableName string) (bool, error) {
	var exists bool
	if err := transaction.QueryRow(ctx, `
select exists (
  select 1
  from information_schema.tables
  where table_schema = current_schema()
    and table_name = $1
)
`, tableName).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect table %s: %w", tableName, err)
	}
	return exists, nil
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
