package db

import (
	"context"
	"fmt"
	"time"

	migrationfiles "agentbox/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationAdvisoryLockID int64 = 0x4167656e74426f78

type appliedMigration struct {
	Name     string
	Checksum string
}

// Migrate applies every checked-in SQL migration exactly once. Migration files
// are immutable after application; checksum drift is treated as an error.
func (r *Repository) Migrate(ctx context.Context) error {
	migrations, err := migrationfiles.Load()
	if err != nil {
		return err
	}
	return r.applyMigrations(ctx, migrations)
}

func (r *Repository) applyMigrations(ctx context.Context, migrations []migrationfiles.Migration) error {
	connection, err := r.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Release()

	if _, err := connection.Exec(ctx, `select pg_advisory_lock($1)`, migrationAdvisoryLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = connection.Exec(unlockContext, `select pg_advisory_unlock($1)`, migrationAdvisoryLockID)
	}()

	if _, err := connection.Exec(ctx, `
create table if not exists schema_migrations (
  version text primary key,
  name text not null,
  checksum text not null,
  applied_at timestamptz not null default now()
)
`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := loadAppliedMigrations(ctx, connection)
	if err != nil {
		return err
	}

	for _, migration := range migrations {
		if existing, ok := applied[migration.Version]; ok {
			if existing.Name != migration.Name || existing.Checksum != migration.Checksum {
				return fmt.Errorf(
					"migration %s drift detected: database has name=%q checksum=%s, source has name=%q checksum=%s",
					migration.Version,
					existing.Name,
					existing.Checksum,
					migration.Name,
					migration.Checksum,
				)
			}
			continue
		}

		transaction, err := connection.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", migration.Name, err)
		}
		if _, err := transaction.Exec(ctx, migration.SQL); err != nil {
			_ = transaction.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}
		if _, err := transaction.Exec(ctx, `
insert into schema_migrations (version, name, checksum)
values ($1, $2, $3)
`, migration.Version, migration.Name, migration.Checksum); err != nil {
			_ = transaction.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", migration.Name, err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", migration.Name, err)
		}
	}

	return nil
}

func loadAppliedMigrations(ctx context.Context, connection *pgxpool.Conn) (map[string]appliedMigration, error) {
	rows, err := connection.Query(ctx, `select version, name, checksum from schema_migrations order by version`)
	if err != nil {
		return nil, fmt.Errorf("list applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]appliedMigration)
	for rows.Next() {
		var version string
		var migration appliedMigration
		if err := rows.Scan(&version, &migration.Name, &migration.Checksum); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = migration
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}
	return applied, nil
}
