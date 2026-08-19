package db

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrOwnerAlreadyExists = types.ErrOwnerAlreadyExists
var ErrOwnerSetupTokenInvalid = types.ErrOwnerSetupTokenInvalid

const ownerBootstrapAdvisoryLockID int64 = 0x4167656e744f776e
const attachmentPurgeAdvisoryNamespace int64 = 0x41505055524745

func lockThreadAccessForMutation(ctx context.Context, tx pgx.Tx, userID string, threadID string) error {
	var ownerUserID string
	if err := tx.QueryRow(ctx, `
select owner_user_id
from threads
where id = $1
for update
`, strings.TrimSpace(threadID)).Scan(&ownerUserID); errors.Is(err, pgx.ErrNoRows) {
		return types.ErrThreadNotFound
	} else if err != nil {
		return err
	}
	if ownerUserID == strings.TrimSpace(userID) {
		return nil
	}

	rows, err := tx.Query(ctx, `
select tm.team_id
from thread_team_shares tts
join team_memberships tm on tm.team_id = tts.team_id
where tts.thread_id = $1 and tm.user_id = $2
for key share of tts, tm
`, strings.TrimSpace(threadID), strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return types.ErrThreadNotFound
	}
	return nil
}

func lockLiveActorForMutation(ctx context.Context, tx pgx.Tx, auth types.AuthContext) error {
	var allowed bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from users where id = $1 and disabled_at is null)`, strings.TrimSpace(auth.UserID)).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return types.ErrThreadNotFound
	}
	if strings.TrimSpace(auth.KeyID) != "" {
		if err := tx.QueryRow(ctx, `select exists(select 1 from api_keys where id = $1 and user_id = $2 and revoked_at is null)`, strings.TrimSpace(auth.KeyID), strings.TrimSpace(auth.UserID)).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return types.ErrThreadNotFound
		}
	}
	if strings.TrimSpace(auth.SessionID) != "" {
		if err := tx.QueryRow(ctx, `select exists(select 1 from user_sessions where id = $1 and user_id = $2 and revoked_at is null and expires_at > now())`, strings.TrimSpace(auth.SessionID), strings.TrimSpace(auth.UserID)).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return types.ErrThreadNotFound
		}
	}
	return nil
}

type transactionAssetLease struct {
	tx       pgx.Tx
	asset    types.Asset
	once     sync.Once
	closeErr error
}

func (l *transactionAssetLease) Asset() types.Asset { return l.asset }
func (l *transactionAssetLease) Close(ctx context.Context) error {
	l.once.Do(func() { l.closeErr = l.tx.Commit(ctx) })
	return l.closeErr
}

type transactionPublicThreadLease struct {
	tx       pgx.Tx
	thread   types.ThreadWithMessages
	once     sync.Once
	closeErr error
}

func (l *transactionPublicThreadLease) Thread() types.ThreadWithMessages { return l.thread }
func (l *transactionPublicThreadLease) Close(ctx context.Context) error {
	l.once.Do(func() { l.closeErr = l.tx.Commit(ctx) })
	return l.closeErr
}

type postgresAttachmentPurgeLease struct {
	conn     *pgxpool.Conn
	userID   string
	once     sync.Once
	closeErr error
}

func (l *postgresAttachmentPurgeLease) Close(_ context.Context) error {
	l.once.Do(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, l.closeErr = l.conn.Exec(cleanupContext, `select pg_advisory_unlock(hashtextextended($1, $2))`, l.userID, attachmentPurgeAdvisoryNamespace)
		if l.closeErr != nil {
			connection := l.conn.Hijack()
			_ = connection.Close(cleanupContext)
			return
		}
		l.conn.Release()
	})
	return l.closeErr
}

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

const threadVisibilitySummarySelect = `
  (t.owner_user_id = $1) as owned_by_me,
  coalesce((
    select jsonb_agg(
      jsonb_build_object('id', summary_team.id, 'slug', summary_team.slug, 'name', summary_team.name)
      order by lower(summary_team.name), lower(summary_team.slug), summary_team.id
    )
    from thread_team_shares summary_share
    join teams summary_team on summary_team.id = summary_share.team_id
    where summary_share.thread_id = t.id
  ), '[]'::jsonb) as shared_teams,
  coalesce((
    select jsonb_agg(
      jsonb_build_object('id', matched_team.id, 'slug', matched_team.slug, 'name', matched_team.name)
      order by lower(matched_team.name), lower(matched_team.slug), matched_team.id
    )
    from thread_team_shares matched_share
    join teams matched_team on matched_team.id = matched_share.team_id
    join team_memberships matched_membership
      on matched_membership.team_id = matched_share.team_id
     and matched_membership.user_id = $1
    where matched_share.thread_id = t.id
  ), '[]'::jsonb) as matched_teams,
  exists (
    select 1
    from thread_public_links summary_public
    where summary_public.thread_id = t.id
      and summary_public.revoked_at is null
  ) as is_public`

func threadFilterPredicate(filterPlaceholder string, teamPlaceholder string) string {
	return `(
  ` + filterPlaceholder + ` = 'all'
  or (
    ` + filterPlaceholder + ` = 'private'
    and t.owner_user_id = $1
    and not exists (select 1 from thread_team_shares private_share where private_share.thread_id = t.id)
    and not exists (
      select 1 from thread_public_links private_public
      where private_public.thread_id = t.id and private_public.revoked_at is null
    )
  )
  or (
    ` + filterPlaceholder + ` = 'shared'
    and t.owner_user_id <> $1
    and exists (
      select 1
      from thread_team_shares shared_filter
      join team_memberships shared_membership
        on shared_membership.team_id = shared_filter.team_id
      where shared_filter.thread_id = t.id
        and shared_membership.user_id = $1
    )
  )
  or (
    ` + filterPlaceholder + ` = 'team'
    and exists (
      select 1
      from thread_team_shares team_filter
      join teams filter_team on filter_team.id = team_filter.team_id
      join team_memberships filter_membership
        on filter_membership.team_id = team_filter.team_id
      where team_filter.thread_id = t.id
        and filter_membership.user_id = $1
        and (filter_team.id = ` + teamPlaceholder + ` or filter_team.slug = lower(` + teamPlaceholder + `))
    )
  )
  or (
    ` + filterPlaceholder + ` = 'public'
    and exists (
      select 1 from thread_public_links public_filter
      where public_filter.thread_id = t.id and public_filter.revoked_at is null
    )
  )
)`
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
