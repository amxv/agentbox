package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/identity"
	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func listThreadTeamsTx(ctx context.Context, tx pgx.Tx, threadID string) ([]types.Team, error) {
	rows, err := tx.Query(ctx, `
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

func listUserTeamsTx(ctx context.Context, tx pgx.Tx, userID string) ([]types.Team, error) {
	rows, err := tx.Query(ctx, `
select t.id, t.slug, t.name, t.created_at, t.updated_at
from teams t
join team_memberships membership on membership.team_id = t.id
where membership.user_id = $1
order by lower(t.name), lower(t.slug), t.id
`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTeams(rows)
}

func requireUserTeamMembershipsTx(ctx context.Context, tx pgx.Tx, userID string, teamIDs []string) error {
	teamIDs = uniqueNonEmptyStrings(teamIDs)
	if len(teamIDs) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `
select membership.team_id
from team_memberships membership
join teams team on team.id = membership.team_id
where membership.user_id = $1
  and membership.team_id = any($2::text[])
for key share of membership, team
`, strings.TrimSpace(userID), teamIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != len(teamIDs) {
		return types.ErrThreadVisibilityTeamUnavailable
	}
	return nil
}

func (r *Repository) ManageThreadVisibility(ctx context.Context, userID string, threadID string, input types.ManageThreadVisibilityInput) (types.ManagedThreadVisibility, error) {
	input.AddTeams = uniqueNonEmptyStrings(input.AddTeams)
	input.RemoveTeams = uniqueNonEmptyStrings(input.RemoveTeams)
	mutation := len(input.AddTeams) > 0 || len(input.RemoveTeams) > 0 || input.Public != nil || input.RegeneratePublicLink

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	defer tx.Rollback(ctx)

	state := types.ManagedThreadVisibility{ThreadID: threadID}
	if mutation {
		if err := lockThreadAccessForMutation(ctx, tx, userID, threadID); err != nil {
			return types.ManagedThreadVisibility{}, err
		}
		if err := tx.QueryRow(ctx, `select owner_user_id from threads where id = $1`, threadID).Scan(&state.OwnerUserID); err != nil {
			return types.ManagedThreadVisibility{}, err
		}
	} else {
		if err := tx.QueryRow(ctx, `
select t.owner_user_id
from threads t
where `+normalThreadAccessPredicate+` and t.id = $2
`, userID, threadID).Scan(&state.OwnerUserID); errors.Is(err, pgx.ErrNoRows) {
			return types.ManagedThreadVisibility{}, types.ErrThreadNotFound
		} else if err != nil {
			return types.ManagedThreadVisibility{}, err
		}
	}

	availableTeams, err := listUserTeamsTx(ctx, tx, userID)
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	currentTeams, err := listThreadTeamsTx(ctx, tx, threadID)
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	addTeamIDs, err := resolveTeamReferences(input.AddTeams, availableTeams, true)
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	removeTeamIDs, err := resolveTeamReferences(input.RemoveTeams, currentTeams, false)
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	if stringSetsOverlap(addTeamIDs, removeTeamIDs) {
		return types.ManagedThreadVisibility{}, types.ErrThreadVisibilityConflict
	}
	if err := requireUserTeamMembershipsTx(ctx, tx, userID, addTeamIDs); err != nil {
		return types.ManagedThreadVisibility{}, err
	}

	if len(removeTeamIDs) > 0 {
		if _, err := tx.Exec(ctx, `
delete from thread_team_shares
where thread_id = $1 and team_id = any($2::text[])
`, threadID, removeTeamIDs); err != nil {
			return types.ManagedThreadVisibility{}, err
		}
	}
	for _, teamID := range addTeamIDs {
		if _, err := tx.Exec(ctx, `
insert into thread_team_shares (thread_id, team_id, created_by_user_id)
values ($1, $2, $3)
on conflict (thread_id, team_id) do nothing
`, threadID, teamID, userID); err != nil {
			return types.ManagedThreadVisibility{}, err
		}
	}

	activeLink, err := getActiveThreadPublicLinkTx(ctx, tx, threadID)
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	if input.Public != nil && !*input.Public {
		if input.RegeneratePublicLink {
			return types.ManagedThreadVisibility{}, types.ErrThreadVisibilityConflict
		}
		if activeLink != nil {
			if _, err := tx.Exec(ctx, `
update thread_public_links
set revoked_at = now(), updated_at = now()
where thread_id = $1 and revoked_at is null
`, threadID); err != nil {
				return types.ManagedThreadVisibility{}, err
			}
			activeLink = nil
		}
	} else {
		createOrRotate := false
		if input.RegeneratePublicLink {
			if activeLink == nil && input.Public == nil {
				return types.ManagedThreadVisibility{}, types.ErrThreadPublicLinkNotFound
			}
			createOrRotate = true
		} else if input.Public != nil && *input.Public && activeLink == nil {
			createOrRotate = true
		}
		if createOrRotate {
			if strings.TrimSpace(input.PublicToken) == "" || strings.TrimSpace(input.PublicTokenHash) == "" || strings.TrimSpace(input.PublicTokenPrefix) == "" {
				return types.ManagedThreadVisibility{}, errors.New("public token material is required")
			}
			link, err := upsertThreadPublicLinkTx(ctx, tx, threadID, userID, input.PublicToken, input.PublicTokenHash, input.PublicTokenPrefix)
			if err != nil {
				return types.ManagedThreadVisibility{}, err
			}
			activeLink = &link
		}
	}

	state.SharedTeams, err = listThreadTeamsTx(ctx, tx, threadID)
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	state.AvailableTeams = availableTeams
	state.PublicLink = activeLink
	state.Public = activeLink != nil
	if err := tx.Commit(ctx); err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	return state, nil
}

func getActiveThreadPublicLinkTx(ctx context.Context, tx pgx.Tx, threadID string) (*types.ThreadPublicLink, error) {
	link, err := scanThreadPublicLink(tx.QueryRow(ctx, `
select
  link.thread_id,
  coalesce(link.token_value, ''),
  link.token_hash,
  link.token_prefix,
  link.created_by_user_id,
  link.created_at,
  link.updated_at,
  link.revoked_at
from thread_public_links link
where link.thread_id = $1 and link.revoked_at is null
`, threadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func upsertThreadPublicLinkTx(ctx context.Context, tx pgx.Tx, threadID string, userID string, token string, tokenHash string, tokenPrefix string) (types.ThreadPublicLink, error) {
	return scanThreadPublicLink(tx.QueryRow(ctx, `
insert into thread_public_links (
  thread_id, token_value, token_hash, token_prefix, created_by_user_id
)
values ($1, $2, $3, $4, $5)
on conflict (thread_id) do update
set token_value = excluded.token_value,
    token_hash = excluded.token_hash,
    token_prefix = excluded.token_prefix,
    created_by_user_id = excluded.created_by_user_id,
    updated_at = now(),
    revoked_at = null
returning
  thread_id,
  coalesce(token_value, ''),
  token_hash,
  token_prefix,
  created_by_user_id,
  created_at,
  updated_at,
  revoked_at
`, threadID, token, tokenHash, tokenPrefix, userID))
}

func resolveTeamReferences(refs []string, teams []types.Team, requireAll bool) ([]string, error) {
	byID := make(map[string]string, len(teams))
	bySlug := make(map[string]string, len(teams))
	for _, team := range teams {
		byID[team.ID] = team.ID
		bySlug[strings.ToLower(team.Slug)] = team.ID
	}
	resolved := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, raw := range refs {
		ref := strings.TrimSpace(raw)
		teamID := byID[ref]
		if teamID == "" {
			teamID = bySlug[strings.ToLower(ref)]
		}
		if teamID == "" {
			if requireAll {
				return nil, types.ErrThreadVisibilityTeamUnavailable
			}
			continue
		}
		if _, exists := seen[teamID]; exists {
			continue
		}
		seen[teamID] = struct{}{}
		resolved = append(resolved, teamID)
	}
	return resolved, nil
}

func stringSetsOverlap(left []string, right []string) bool {
	values := make(map[string]struct{}, len(left))
	for _, value := range left {
		values[value] = struct{}{}
	}
	for _, value := range right {
		if _, exists := values[value]; exists {
			return true
		}
	}
	return false
}

func (r *Repository) AcquirePublicThreadLease(ctx context.Context, tokenHash string) (types.PublicThreadAuthorizationLease, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (types.PublicThreadAuthorizationLease, error) {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	thread, err := scanThread(tx.QueryRow(ctx, `
select
  t.id, t.owner_user_id, t.title, t.created_at, t.updated_at, t.created_by,
  t.created_by_user_id, t.created_by_key_id, t.created_by_user_display_name, t.created_by_actor_name
from thread_public_links link
join threads t on t.id = link.thread_id
where link.token_hash = $1 and link.revoked_at is null
for key share of link, t
`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return nil, nil
	}
	if err != nil {
		return fail(err)
	}
	messages, err := loadThreadMessagesTx(ctx, tx, thread.ID)
	if err != nil {
		return fail(err)
	}
	return &transactionPublicThreadLease{tx: tx, thread: types.ThreadWithMessages{
		Thread:     thread,
		Messages:   messages,
		Visibility: types.ThreadVisibility{ThreadID: thread.ID, OwnerUserID: thread.OwnerUserID},
	}}, nil
}

func (r *Repository) AcquirePublicAssetSigningLease(ctx context.Context, tokenHash string, assetID string) (types.AssetAuthorizationLease, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	asset, err := scanAsset(tx.QueryRow(ctx, `
select a.id, a.message_id, a.storage_key, a.file_name, a.mime_type, a.size_bytes,
       a.created_at, a.created_by, a.created_by_user_id, a.created_by_key_id,
       a.created_by_user_display_name, a.created_by_actor_name,
       a.purged_at, a.purged_by_user_id, a.purge_last_attempt_at, a.purge_error
from thread_public_links link
join threads t on t.id = link.thread_id
join messages m on m.thread_id = t.id
join assets a on a.message_id = m.id
where link.token_hash = $1 and link.revoked_at is null and a.id = $2
for key share of link, t
`, tokenHash, assetID))
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return nil, nil
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return &transactionAssetLease{tx: tx, asset: asset}, nil
}

func (r *Repository) AcquireAssetSigningLease(ctx context.Context, userID string, assetID string) (types.AssetAuthorizationLease, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (types.AssetAuthorizationLease, error) {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	var threadID string
	var ownerUserID string
	if err := tx.QueryRow(ctx, `
select t.id, t.owner_user_id
from assets a
join messages m on m.id = a.message_id
join threads t on t.id = m.thread_id
where a.id = $1
for key share of t
`, assetID).Scan(&threadID, &ownerUserID); errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return nil, nil
	} else if err != nil {
		return fail(err)
	}
	if ownerUserID != strings.TrimSpace(userID) {
		rows, err := tx.Query(ctx, `
select tm.team_id
from thread_team_shares tts
join team_memberships tm on tm.team_id = tts.team_id
where tts.thread_id = $1 and tm.user_id = $2
for key share of tts, tm
`, threadID, strings.TrimSpace(userID))
		if err != nil {
			return fail(err)
		}
		qualified := rows.Next()
		rows.Close()
		if !qualified {
			_ = tx.Rollback(ctx)
			return nil, nil
		}
	}
	asset, err := getAssetByIDTx(ctx, tx, assetID)
	if err != nil {
		return fail(err)
	}
	if asset == nil {
		_ = tx.Rollback(ctx)
		return nil, nil
	}
	return &transactionAssetLease{tx: tx, asset: *asset}, nil
}

func getAssetByIDTx(ctx context.Context, tx pgx.Tx, assetID string) (*types.Asset, error) {
	asset, err := scanAsset(tx.QueryRow(ctx, `
select a.id, a.message_id, a.storage_key, a.file_name, a.mime_type, a.size_bytes,
       a.created_at, a.created_by, a.created_by_user_id, a.created_by_key_id,
       a.created_by_user_display_name, a.created_by_actor_name,
       a.purged_at, a.purged_by_user_id, a.purge_last_attempt_at, a.purge_error
from assets a
where a.id = $1
`, assetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func loadThreadMessagesTx(ctx context.Context, tx pgx.Tx, threadID string) ([]types.Message, error) {
	rows, err := tx.Query(ctx, `
select id, thread_id, author, body, body_content_type, created_at,
       created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
from messages
where thread_id = $1
order by position
`, threadID)
	if err != nil {
		return nil, err
	}
	messages := []types.Message{}
	messageIndex := map[string]int{}
	for rows.Next() {
		message, err := scanMessage(rows, nil)
		if err != nil {
			rows.Close()
			return nil, err
		}
		message.Position = int64(len(messages) + 1)
		messageIndex[message.ID] = len(messages)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	assetRows, err := tx.Query(ctx, `
select a.id, a.message_id, a.storage_key, a.file_name, a.mime_type, a.size_bytes,
       a.created_at, a.created_by, a.created_by_user_id, a.created_by_key_id,
       a.created_by_user_display_name, a.created_by_actor_name,
       a.purged_at, a.purged_by_user_id, a.purge_last_attempt_at, a.purge_error
from assets a
join messages m on m.id = a.message_id
where m.thread_id = $1
order by m.position, a.position
`, threadID)
	if err != nil {
		return nil, err
	}
	for assetRows.Next() {
		asset, err := scanAsset(assetRows)
		if err != nil {
			assetRows.Close()
			return nil, err
		}
		if index, ok := messageIndex[asset.MessageID]; ok {
			asset.Position = int64(len(messages[index].Assets) + 1)
			messages[index].Assets = append(messages[index].Assets, asset)
		}
	}
	if err := assetRows.Err(); err != nil {
		assetRows.Close()
		return nil, err
	}
	assetRows.Close()
	return messages, nil
}

func (r *Repository) loadThreadMessages(ctx context.Context, threadID string) ([]types.Message, error) {
	rows, err := r.pool.Query(ctx, `
select
  id,
  thread_id,
  author,
  body,
  body_content_type,
  created_at,
  created_by_user_id,
  created_by_key_id,
  created_by_user_display_name,
  created_by_actor_name
from messages
where thread_id = $1
order by position
`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := []types.Message{}
	messageIndex := map[string]int{}
	for rows.Next() {
		message, err := scanMessage(rows, nil)
		if err != nil {
			return nil, err
		}
		message.Position = int64(len(messages) + 1)
		messageIndex[message.ID] = len(messages)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	assetRows, err := r.pool.Query(ctx, `
select
  a.id,
  a.message_id,
  a.storage_key,
  a.file_name,
  a.mime_type,
  a.size_bytes,
  a.created_at,
  a.created_by,
  a.created_by_user_id,
  a.created_by_key_id,
  a.created_by_user_display_name,
  a.created_by_actor_name,
  a.purged_at,
  a.purged_by_user_id,
  a.purge_last_attempt_at,
  a.purge_error
from assets a
join messages m on m.id = a.message_id
where m.thread_id = $1
order by m.position, a.position
`, threadID)
	if err != nil {
		return nil, err
	}
	defer assetRows.Close()
	for assetRows.Next() {
		asset, err := scanAsset(assetRows)
		if err != nil {
			return nil, err
		}
		if index, ok := messageIndex[asset.MessageID]; ok {
			asset.Position = int64(len(messages[index].Assets) + 1)
			messages[index].Assets = append(messages[index].Assets, asset)
		}
	}
	if err := assetRows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func (r *Repository) ListThreads(ctx context.Context, userID string, limit int) ([]types.Thread, error) {
	return r.ListThreadsFiltered(ctx, userID, types.ThreadListParams{Limit: limit, Filter: types.ThreadFilterAll})
}

func (r *Repository) ListThreadsFiltered(ctx context.Context, userID string, params types.ThreadListParams) ([]types.Thread, error) {
	page, err := r.ListThreadsPage(ctx, userID, params)
	return page.Threads, err
}

func (r *Repository) ListThreadsPage(ctx context.Context, userID string, params types.ThreadListParams) (types.ThreadPage, error) {
	if strings.TrimSpace(params.Filter) == "" {
		params.Filter = types.ThreadFilterAll
	}
	if params.Limit <= 0 {
		params.Limit = 50
	}
	var cursorUpdatedAt any
	cursorID := ""
	if params.Cursor != nil {
		cursorUpdatedAt = params.Cursor.UpdatedAt
		cursorID = params.Cursor.ID
	}
	rows, err := r.pool.Query(ctx, `
select
  t.id,
  t.owner_user_id,
  t.title,
  t.created_at,
  t.updated_at,
  t.created_by,
  t.created_by_user_id,
  t.created_by_key_id,
  t.created_by_user_display_name,
  t.created_by_actor_name,
  (select count(*)::int from messages summary_count where summary_count.thread_id = t.id) as message_count,
  coalesce((
    select left(summary_latest.body, 512)
    from messages summary_latest
    where summary_latest.thread_id = t.id
    order by summary_latest.position desc
    limit 1
  ), '') as last_message_body,
`+threadVisibilitySummarySelect+`
from threads t
where `+normalThreadAccessPredicate+`
  and `+threadFilterPredicate("$2", "$3")+`
  and ($4::timestamptz is null or t.updated_at < $4 or (t.updated_at = $4 and t.id > $5))
order by t.updated_at desc, t.id
limit $6
`, userID, params.Filter, params.TeamRef, cursorUpdatedAt, cursorID, params.Limit+1)
	if err != nil {
		return types.ThreadPage{}, err
	}
	defer rows.Close()

	threads := []types.Thread{}
	positions := []types.ThreadPageCursor{}
	for rows.Next() {
		thread, updatedAt, err := scanThreadSummaryWithVisibilityPosition(rows)
		if err != nil {
			return types.ThreadPage{}, err
		}
		threads = append(threads, thread)
		positions = append(positions, types.ThreadPageCursor{UpdatedAt: updatedAt, ID: thread.ID})
	}
	if err := rows.Err(); err != nil {
		return types.ThreadPage{}, err
	}
	visible := len(threads)
	hasMore := visible > params.Limit
	if hasMore {
		visible = params.Limit
	}
	pageInfo := types.ThreadPageInfo{Limit: params.Limit, HasMore: hasMore}
	if hasMore && visible > 0 {
		next, err := types.EncodeThreadPageCursor(positions[visible-1])
		if err != nil {
			return types.ThreadPage{}, err
		}
		pageInfo.NextCursor = &next
	}
	return types.ThreadPage{Threads: threads[:visible], Page: pageInfo}, nil
}

func (r *Repository) SearchThreads(ctx context.Context, userID string, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
	page, err := r.SearchThreadsPage(ctx, userID, params)
	return page.Threads, err
}

func (r *Repository) SearchThreadsPage(ctx context.Context, userID string, params types.SearchThreadParams) (types.SearchThreadPage, error) {
	if strings.TrimSpace(params.Filter) == "" {
		params.Filter = types.ThreadFilterAll
	}
	if params.Limit <= 0 {
		params.Limit = 20
	}
	var createdBy any
	if params.CreatedBy != nil && *params.CreatedBy != "" {
		createdBy = *params.CreatedBy
	}
	var updatedAfter any
	if params.UpdatedAfter != nil && *params.UpdatedAfter != "" {
		parsed, err := time.Parse(time.RFC3339, *params.UpdatedAfter)
		if err != nil {
			return types.SearchThreadPage{}, err
		}
		updatedAfter = parsed
	}
	pattern := "%" + params.Query + "%"
	var cursorUpdatedAt any
	cursorID := ""
	if params.Cursor != nil {
		cursorUpdatedAt = params.Cursor.UpdatedAt
		cursorID = params.Cursor.ID
	}
	rows, err := r.pool.Query(ctx, `
select
  t.id,
  t.owner_user_id,
  t.title,
  t.created_at,
  t.updated_at,
  t.created_by,
  t.created_by_user_display_name,
  t.created_by_actor_name,
  (select count(*)::int from messages counted_message where counted_message.thread_id = t.id) as message_count,
  coalesce((select lm.body from messages lm where lm.thread_id = t.id order by lm.position desc limit 1), '') as last_message_body,
  coalesce((select mm.body from messages mm where mm.thread_id = t.id and mm.body ilike $2 order by mm.position desc limit 1), '') as matched_message_body,
`+threadVisibilitySummarySelect+`
from threads t
where `+normalThreadAccessPredicate+`
  and ($3::text is null or t.created_by = $3)
  and ($4::timestamptz is null or t.updated_at > $4)
  and (
    t.title ilike $2
    or exists (select 1 from messages sm where sm.thread_id = t.id and sm.body ilike $2)
  )
  and `+threadFilterPredicate("$5", "$6")+`
  and ($7::timestamptz is null or t.updated_at < $7 or (t.updated_at = $7 and t.id > $8))
order by t.updated_at desc, t.id
limit $9
`, userID, pattern, createdBy, updatedAfter, params.Filter, params.TeamRef, cursorUpdatedAt, cursorID, params.Limit+1)
	if err != nil {
		return types.SearchThreadPage{}, err
	}
	defer rows.Close()

	results := []types.SearchThreadResult{}
	positions := []types.ThreadPageCursor{}
	for rows.Next() {
		var createdAt time.Time
		var updatedAt time.Time
		var lastBody string
		var matchedBody string
		var ownedByMe bool
		var sharedTeamsJSON []byte
		var matchedTeamsJSON []byte
		var isPublic bool
		result := types.SearchThreadResult{}
		if err := rows.Scan(
			&result.ID,
			&result.OwnerUserID,
			&result.Title,
			&createdAt,
			&updatedAt,
			&result.CreatedBy,
			&result.CreatedByUserDisplayName,
			&result.CreatedByActorName,
			&result.MessageCount,
			&lastBody,
			&matchedBody,
			&ownedByMe,
			&sharedTeamsJSON,
			&matchedTeamsJSON,
			&isPublic,
		); err != nil {
			return types.SearchThreadPage{}, err
		}
		result.VisibilitySummary, err = decodeThreadVisibilitySummary(ownedByMe, sharedTeamsJSON, matchedTeamsJSON, isPublic)
		if err != nil {
			return types.SearchThreadPage{}, err
		}
		result.CreatedAt = isoMillis(createdAt)
		result.UpdatedAt = isoMillis(updatedAt)
		result.LastMessagePreview = previewText(lastBody, 180)
		result.MatchedSnippets = matchedSnippets(params.Query, result.Title, matchedBody)
		results = append(results, result)
		positions = append(positions, types.ThreadPageCursor{UpdatedAt: updatedAt, ID: result.ID})
	}
	if err := rows.Err(); err != nil {
		return types.SearchThreadPage{}, err
	}
	visible := len(results)
	hasMore := visible > params.Limit
	if hasMore {
		visible = params.Limit
	}
	pageInfo := types.ThreadPageInfo{Limit: params.Limit, HasMore: hasMore}
	if hasMore && visible > 0 {
		next, err := types.EncodeThreadPageCursor(positions[visible-1])
		if err != nil {
			return types.SearchThreadPage{}, err
		}
		pageInfo.NextCursor = &next
	}
	return types.SearchThreadPage{Threads: results[:visible], Page: pageInfo}, nil
}

func (r *Repository) ListOwnerContentThreads(ctx context.Context, ownerUserID string, params types.OwnerContentListParams) ([]types.OwnerContentThreadSummary, error) {
	page, err := r.ListOwnerContentThreadsPage(ctx, ownerUserID, params)
	return page.Threads, err
}

func (r *Repository) SearchOwnerContentThreads(ctx context.Context, ownerUserID string, params types.OwnerContentSearchParams) ([]types.OwnerContentThreadSummary, error) {
	page, err := r.SearchOwnerContentThreadsPage(ctx, ownerUserID, params)
	return page.Threads, err
}

func (r *Repository) ListOwnerContentThreadsPage(ctx context.Context, ownerUserID string, params types.OwnerContentListParams) (types.OwnerContentThreadPage, error) {
	return r.queryOwnerContentThreadsPage(ctx, ownerUserID, "", params.UserID, params.TeamRef, types.PageRequest{Limit: params.Limit, Offset: params.Offset})
}

func (r *Repository) SearchOwnerContentThreadsPage(ctx context.Context, ownerUserID string, params types.OwnerContentSearchParams) (types.OwnerContentThreadPage, error) {
	return r.queryOwnerContentThreadsPage(ctx, ownerUserID, params.Query, params.UserID, params.TeamRef, types.PageRequest{Limit: params.Limit, Offset: params.Offset})
}

func (r *Repository) queryOwnerContentThreadsPage(ctx context.Context, ownerUserID string, query string, userID string, teamRef string, pageRequest types.PageRequest) (types.OwnerContentThreadPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	pattern := ""
	if strings.TrimSpace(query) != "" {
		pattern = "%" + strings.TrimSpace(query) + "%"
	}
	rows, err := r.pool.Query(ctx, `
select
  t.id,
  t.owner_user_id,
  t.title,
  t.created_at,
  t.updated_at,
  t.created_by,
  t.created_by_user_id,
  t.created_by_key_id,
  t.created_by_user_display_name,
  t.created_by_actor_name,
`+threadVisibilitySummarySelect+`,
  owner.id,
  owner.email,
  owner.display_name,
  owner.password_hash,
  owner.is_owner,
  owner.created_at,
  owner.updated_at,
  owner.disabled_at,
  (select count(*)::int from messages counted_message where counted_message.thread_id = t.id) as message_count,
  coalesce((select lm.body from messages lm where lm.thread_id = t.id order by lm.position desc limit 1), '') as last_message_body,
  coalesce((select mm.body from messages mm where $2 <> '' and mm.thread_id = t.id and mm.body ilike $2 order by mm.position desc limit 1), '') as matched_message_body
from threads t
join users owner on owner.id = t.owner_user_id
where ($2 = '' or t.title ilike $2 or exists (
  select 1 from messages search_message
  where search_message.thread_id = t.id and search_message.body ilike $2
))
  and ($3 = '' or t.owner_user_id = $3)
  and ($4 = '' or exists (
    select 1
    from thread_team_shares owner_filter_share
    join teams owner_filter_team on owner_filter_team.id = owner_filter_share.team_id
    where owner_filter_share.thread_id = t.id
      and (owner_filter_team.id = $4 or owner_filter_team.slug = lower($4))
  ))
order by t.updated_at desc, t.id
limit $5 offset $6
`, strings.TrimSpace(ownerUserID), pattern, strings.TrimSpace(userID), strings.TrimSpace(teamRef), pageRequest.Limit+1, pageRequest.Offset)
	if err != nil {
		return types.OwnerContentThreadPage{}, err
	}
	defer rows.Close()
	results := []types.OwnerContentThreadSummary{}
	for rows.Next() {
		result, err := scanOwnerContentThreadSummary(rows, query)
		if err != nil {
			return types.OwnerContentThreadPage{}, err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return types.OwnerContentThreadPage{}, err
	}
	visible, pageInfo := types.PageWindow(pageRequest, len(results))
	return types.OwnerContentThreadPage{Threads: results[:visible], Page: pageInfo}, nil
}

func (r *Repository) GetOwnerContentThread(ctx context.Context, ownerUserID string, threadID string) (*types.OwnerContentThreadDetail, error) {
	summary, err := scanOwnerContentThreadSummary(r.pool.QueryRow(ctx, `
select
  t.id,
  t.owner_user_id,
  t.title,
  t.created_at,
  t.updated_at,
  t.created_by,
  t.created_by_user_id,
  t.created_by_key_id,
  t.created_by_user_display_name,
  t.created_by_actor_name,
`+threadVisibilitySummarySelect+`,
  owner.id,
  owner.email,
  owner.display_name,
  owner.password_hash,
  owner.is_owner,
  owner.created_at,
  owner.updated_at,
  owner.disabled_at,
  (select count(*)::int from messages counted_message where counted_message.thread_id = t.id) as message_count,
  coalesce((select lm.body from messages lm where lm.thread_id = t.id order by lm.position desc limit 1), '') as last_message_body,
  ''::text as matched_message_body
from threads t
join users owner on owner.id = t.owner_user_id
where t.id = $2
`, strings.TrimSpace(ownerUserID), strings.TrimSpace(threadID)), "")
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	messages, err := r.loadThreadMessages(ctx, summary.ID)
	if err != nil {
		return nil, err
	}
	teams, err := r.listThreadTeams(ctx, summary.ID)
	if err != nil {
		return nil, err
	}
	return &types.OwnerContentThreadDetail{
		Thread:     summary.Thread,
		Owner:      summary.Owner,
		Messages:   messages,
		Visibility: types.ThreadVisibility{ThreadID: summary.ID, OwnerUserID: summary.OwnerUserID, SharedTeams: teams},
	}, nil
}

func (r *Repository) GetOwnerContentAsset(ctx context.Context, assetID string) (*types.Asset, error) {
	asset, err := scanAsset(r.pool.QueryRow(ctx, `
select
  a.id,
  a.message_id,
  a.storage_key,
  a.file_name,
  a.mime_type,
  a.size_bytes,
  a.created_at,
  a.created_by,
  a.created_by_user_id,
  a.created_by_key_id,
  a.created_by_user_display_name,
  a.created_by_actor_name,
  a.purged_at,
  a.purged_by_user_id,
  a.purge_last_attempt_at,
  a.purge_error
from assets a
where a.id = $1
`, strings.TrimSpace(assetID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func scanOwnerContentThreadSummary(row threadScanner, query string) (types.OwnerContentThreadSummary, error) {
	var threadCreatedAt time.Time
	var threadUpdatedAt time.Time
	var ownedByMe bool
	var sharedTeamsJSON []byte
	var matchedTeamsJSON []byte
	var isPublic bool
	var ownerCreatedAt time.Time
	var ownerUpdatedAt time.Time
	var ownerDisabledAt *time.Time
	var lastBody string
	var matchedBody string
	result := types.OwnerContentThreadSummary{}
	err := row.Scan(
		&result.ID,
		&result.OwnerUserID,
		&result.Title,
		&threadCreatedAt,
		&threadUpdatedAt,
		&result.CreatedBy,
		&result.CreatedByUserID,
		&result.CreatedByKeyID,
		&result.CreatedByUserDisplayName,
		&result.CreatedByActorName,
		&ownedByMe,
		&sharedTeamsJSON,
		&matchedTeamsJSON,
		&isPublic,
		&result.Owner.ID,
		&result.Owner.Email,
		&result.Owner.DisplayName,
		&result.Owner.PasswordHash,
		&result.Owner.IsOwner,
		&ownerCreatedAt,
		&ownerUpdatedAt,
		&ownerDisabledAt,
		&result.MessageCount,
		&lastBody,
		&matchedBody,
	)
	if err != nil {
		return types.OwnerContentThreadSummary{}, err
	}
	result.CreatedAt = isoMillis(threadCreatedAt)
	result.UpdatedAt = isoMillis(threadUpdatedAt)
	result.Owner.CreatedAt = isoMillis(ownerCreatedAt)
	result.Owner.UpdatedAt = isoMillis(ownerUpdatedAt)
	result.Owner.DisabledAt = optionalISOTime(ownerDisabledAt)
	result.VisibilitySummary, err = decodeThreadVisibilitySummary(ownedByMe, sharedTeamsJSON, matchedTeamsJSON, isPublic)
	if err != nil {
		return types.OwnerContentThreadSummary{}, err
	}
	// Owner-content summaries describe the thread's global visibility rather
	// than whether the permanent owner is the thread owner.
	result.VisibilitySummary.Private = len(result.VisibilitySummary.SharedTeams) == 0 && !result.VisibilitySummary.Public
	result.LastMessagePreview = previewText(lastBody, 280)
	result.MatchedSnippets = matchedSnippets(query, result.Title, matchedBody)
	return result, nil
}

func (r *Repository) CreateThread(ctx context.Context, userID string, title string, auth types.AuthContext) (types.Thread, error) {
	id := "thr_" + uuid.NewString()
	thread, err := scanThread(r.pool.QueryRow(ctx, `
insert into threads (
  id, owner_user_id, title, created_by, created_by_user_id, created_by_key_id,
  created_by_user_display_name, created_by_actor_name
)
values ($1, $2, $3, $4, $5, $6, $7, $8)
returning id, owner_user_id, title, created_at, updated_at, created_by,
          created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
`, id, userID, title, auth.ActorName, userID, optionalString(auth.KeyID), optionalString(auth.UserDisplayName), optionalString(auth.ActorName)))
	if err != nil {
		return types.Thread{}, err
	}
	thread.VisibilitySummary = newPrivateThreadVisibilitySummary()
	return thread, nil
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
  id, owner_user_id, title, created_by, created_by_user_id, created_by_key_id,
  created_by_user_display_name, created_by_actor_name
)
values ($1, $2, $3, $4, $5, $6, $7, $8)
returning id, owner_user_id, title, created_at, updated_at, created_by,
          created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
`, threadID, userID, title, auth.ActorName, userID, optionalString(auth.KeyID), optionalString(auth.UserDisplayName), optionalString(auth.ActorName)))
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}

	messageID := "msg_" + uuid.NewString()
	message, err := scanMessage(tx.QueryRow(ctx, `
insert into messages (
  id, thread_id, position, author, body, body_content_type, created_by_user_id, created_by_key_id,
  created_by_user_display_name, created_by_actor_name
)
values ($1, $2, 1, $3, $4, $5, $6, $7, $8, $9)
returning id, thread_id, author, body, body_content_type, created_at,
          created_by_user_id, created_by_key_id, created_by_user_display_name, created_by_actor_name
`, messageID, thread.ID, auth.ActorName, body, bodyContentType, userID, optionalString(auth.KeyID), optionalString(auth.UserDisplayName), optionalString(auth.ActorName)), nil)
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	message.Position = 1
	if _, err := tx.Exec(ctx, `update threads t set updated_at = now() where `+normalThreadAccessPredicate+` and t.id = $2`, userID, thread.ID); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	thread.VisibilitySummary = newPrivateThreadVisibilitySummary()
	return thread, message, nil
}

func (r *Repository) GetThread(ctx context.Context, userID string, threadID string) (*types.ThreadWithMessages, error) {
	thread, err := scanThreadWithVisibility(r.pool.QueryRow(ctx, `
select
  t.id,
  t.owner_user_id,
  t.title,
  t.created_at,
  t.updated_at,
  t.created_by,
  t.created_by_user_id,
  t.created_by_key_id,
  t.created_by_user_display_name,
  t.created_by_actor_name,
`+threadVisibilitySummarySelect+`
from threads t
where `+normalThreadAccessPredicate+` and t.id = $2
`, userID, threadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	messages, err := r.loadThreadMessages(ctx, threadID)
	if err != nil {
		return nil, err
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
select a.id, a.message_id, a.storage_key, a.file_name, a.mime_type, a.size_bytes,
       a.created_at, a.created_by, a.created_by_user_id, a.created_by_key_id,
       a.created_by_user_display_name, a.created_by_actor_name,
       a.purged_at, a.purged_by_user_id, a.purge_last_attempt_at, a.purge_error
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

func (r *Repository) CreateAPIKey(ctx context.Context, userID string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string) (types.APIKey, error) {
	id := "key_" + uuid.NewString()
	created, err := scanAPIKey(r.pool.QueryRow(ctx, `
insert into api_keys (id, user_id, name, purpose, token_prefix, token_hash, scopes)
values ($1, $2, $3, $4, $5, $6, $7)
returning id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
`, id, userID, name, purpose, tokenPrefix, tokenHash, scopes))
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return types.APIKey{}, types.ErrCredentialLabelConflict
		}
		return types.APIKey{}, err
	}
	return created, nil
}

func (r *Repository) CreateRaycastAPIKey(ctx context.Context, userID string, name string, tokenHash string, tokenPrefix string, scopes []string, setupBaseURL string) (types.APIKey, error) {
	id := "key_" + uuid.NewString()
	created, err := scanAPIKey(r.pool.QueryRow(ctx, `
insert into api_keys (id, user_id, name, purpose, token_prefix, token_hash, scopes, setup_base_url)
values ($1, $2, $3, 'raycast', $4, $5, $6, $7)
returning id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
`, id, strings.TrimSpace(userID), strings.TrimSpace(name), tokenPrefix, tokenHash, scopes, strings.TrimRight(strings.TrimSpace(setupBaseURL), "/")))
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return types.APIKey{}, types.ErrCredentialLabelConflict
		}
		return types.APIKey{}, err
	}
	return created, nil
}

func (r *Repository) CreateOnboardingCredential(ctx context.Context, userID string, connector string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string, setupBaseURL string, rotate bool) (types.APIKey, types.OnboardingState, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

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
	var linkedCredentialID *string
	var linkedRevokedAt *time.Time
	err = tx.QueryRow(ctx, `
select s.credential_id, k.revoked_at
from user_onboarding_steps s
left join api_keys k on k.id = s.credential_id and k.user_id = s.user_id
where s.user_id = $1 and s.connector = $2
`, userID, connector).Scan(&linkedCredentialID, &linkedRevokedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return types.APIKey{}, types.OnboardingState{}, err
	}
	activeLinkedCredential := err == nil && linkedCredentialID != nil && linkedRevokedAt == nil
	if rotate && !activeLinkedCredential {
		return types.APIKey{}, types.OnboardingState{}, types.ErrOnboardingCredentialNotFound
	}
	if !rotate && activeLinkedCredential {
		return types.APIKey{}, types.OnboardingState{}, types.ErrOnboardingCredentialExists
	}
	if _, err := tx.Exec(ctx, `
update user_onboarding
set dismissed_at = null,
    updated_at = now()
where user_id = $1
`, userID); err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}

	normalizedSetupBaseURL := strings.TrimRight(strings.TrimSpace(setupBaseURL), "/")
	var created types.APIKey
	if rotate {
		created, err = scanAPIKey(tx.QueryRow(ctx, `
update api_keys
set name = $3,
    purpose = $4,
    token_prefix = $5,
    token_hash = $6,
    scopes = $7,
    setup_base_url = case when $4 = 'raycast' then nullif($8, '') else setup_base_url end,
    updated_at = now(),
    last_used_at = null
where id = $1 and user_id = $2 and revoked_at is null
returning id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
`, *linkedCredentialID, userID, name, purpose, tokenPrefix, tokenHash, scopes, normalizedSetupBaseURL))
	} else {
		created, err = scanAPIKey(tx.QueryRow(ctx, `
insert into api_keys (id, user_id, name, purpose, token_prefix, token_hash, scopes, setup_base_url)
values ($1, $2, $3, $4, $5, $6, $7, case when $4 = 'raycast' then nullif($8, '') else null end)
returning id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
`, "key_"+uuid.NewString(), userID, name, purpose, tokenPrefix, tokenHash, scopes, normalizedSetupBaseURL))
	}
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return types.APIKey{}, types.OnboardingState{}, types.ErrCredentialLabelConflict
		}
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
order by case connector
  when 'chatgpt' then 1
  when 'claude' then 2
  when 'local' then 3
  when 'raycast' then 4
  else 5
end
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

func (r *Repository) ListAPIKeysPage(ctx context.Context, userID string, pageRequest types.PageRequest) (types.APIKeyPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	rows, err := r.pool.Query(ctx, `
select id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
from api_keys
where user_id = $1
order by created_at desc, id desc
limit $2 offset $3
`, strings.TrimSpace(userID), pageRequest.Limit+1, pageRequest.Offset)
	if err != nil {
		return types.APIKeyPage{}, err
	}
	defer rows.Close()
	keys := []types.APIKey{}
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return types.APIKeyPage{}, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return types.APIKeyPage{}, err
	}
	visible, pageInfo := types.PageWindow(pageRequest, len(keys))
	return types.APIKeyPage{Credentials: keys[:visible], Page: pageInfo}, nil
}

func (r *Repository) ListAllAPIKeys(ctx context.Context) ([]types.APIKey, error) {
	page, err := r.ListAllAPIKeysPage(ctx, types.PageRequest{})
	return page.Credentials, err
}

func (r *Repository) ListAllAPIKeysPage(ctx context.Context, pageRequest types.PageRequest) (types.APIKeyPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	rows, err := r.pool.Query(ctx, `
select id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
from api_keys
order by created_at desc, id desc
limit $1 offset $2
`, pageRequest.Limit+1, pageRequest.Offset)
	if err != nil {
		return types.APIKeyPage{}, err
	}
	defer rows.Close()
	keys := []types.APIKey{}
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return types.APIKeyPage{}, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return types.APIKeyPage{}, err
	}
	visible, pageInfo := types.PageWindow(pageRequest, len(keys))
	return types.APIKeyPage{Credentials: keys[:visible], Page: pageInfo}, nil
}

func (r *Repository) RevokeAPIKey(ctx context.Context, userID string, name string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `update api_keys set revoked_at = now(), updated_at = now() where user_id = $1 and lower(name) = lower($2) and revoked_at is null`, userID, name)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) RevokeAPIKeyForUserByID(ctx context.Context, userID string, keyID string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
update api_keys
set revoked_at = coalesce(revoked_at, now()), updated_at = now()
where user_id = $1 and id = $2
`, strings.TrimSpace(userID), strings.TrimSpace(keyID))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) RevokeAPIKeyByID(ctx context.Context, keyID string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
update api_keys
set revoked_at = coalesce(revoked_at, now()), updated_at = now()
where id = $1
`, strings.TrimSpace(keyID))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) RotateAPIKeyForUserByID(ctx context.Context, userID string, keyID string, tokenHash string, tokenPrefix string, setupBaseURL string) (*types.APIKey, string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	key, persistedBaseURL, err := scanAPIKeyWithSetup(tx.QueryRow(ctx, `
select id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at,
       coalesce(setup_base_url, '')
from api_keys
where user_id = $1 and id = $2 and revoked_at is null
for update
`, strings.TrimSpace(userID), strings.TrimSpace(keyID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	resolvedBaseURL := strings.TrimRight(strings.TrimSpace(persistedBaseURL), "/")
	if key.Purpose == "raycast" && resolvedBaseURL == "" {
		resolvedBaseURL = strings.TrimRight(strings.TrimSpace(setupBaseURL), "/")
		if resolvedBaseURL == "" {
			return nil, "", types.ErrRaycastSetupUnavailable
		}
	}
	rotated, err := scanAPIKey(tx.QueryRow(ctx, `
update api_keys
set token_hash = $3,
    token_prefix = $4,
    setup_base_url = case when purpose = 'raycast' then nullif($5, '') else setup_base_url end,
    updated_at = now(),
    last_used_at = null
where user_id = $1 and id = $2 and revoked_at is null
returning id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
`, strings.TrimSpace(userID), strings.TrimSpace(keyID), tokenHash, tokenPrefix, resolvedBaseURL))
	if err != nil {
		return nil, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}
	return &rotated, resolvedBaseURL, nil
}

func (r *Repository) GetAPIKeySetup(ctx context.Context, userID string, keyID string, setupBaseURL string) (*types.APIKey, string, error) {
	key, resolvedBaseURL, err := scanAPIKeyWithSetup(r.pool.QueryRow(ctx, `
update api_keys
set setup_base_url = case
      when purpose = 'raycast' and coalesce(setup_base_url, '') = '' then nullif($3, '')
      else setup_base_url
    end,
    updated_at = case
      when purpose = 'raycast' and coalesce(setup_base_url, '') = '' and nullif($3, '') is not null then now()
      else updated_at
    end
where user_id = $1 and id = $2
returning id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at,
          coalesce(setup_base_url, '')
`, strings.TrimSpace(userID), strings.TrimSpace(keyID), strings.TrimRight(strings.TrimSpace(setupBaseURL), "/")))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return &key, resolvedBaseURL, nil
}

func (r *Repository) FindAPIKeyBySecret(ctx context.Context, key string) (*types.APIKey, *types.User, error) {
	found, user, err := scanAPIKeyAndUser(r.pool.QueryRow(ctx, `
select
  k.id, k.user_id, k.name, k.purpose, k.token_prefix, k.token_hash, k.scopes,
  k.created_at, k.updated_at, k.last_used_at, k.revoked_at,
  u.id, u.email, u.display_name, u.password_hash, u.is_owner,
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
select id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
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
    disabled_at = null,
    updated_at = now()
where id = $3
returning id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
`, displayName, passwordHash, owner.ID))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return types.User{}, err
	}
	if requiredPurpose == "recovery" {
		return types.User{}, ErrOwnerSetupTokenInvalid
	}

	existing, err := scanUser(tx.QueryRow(ctx, `
select id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
from users
where lower(email) = lower($1)
for update
`, email))
	if err == nil {
		return scanUser(tx.QueryRow(ctx, `
update users
set display_name = $1,
    password_hash = $2,
    is_owner = true,
    disabled_at = null,
    updated_at = now()
where id = $3
returning id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
`, displayName, passwordHash, existing.ID))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return types.User{}, err
	}
	return scanUser(tx.QueryRow(ctx, `
insert into users (id, email, display_name, password_hash, is_owner)
values ($1, $2, $3, $4, true)
returning id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
`, identity.ProposedOwnerID(email), email, displayName, passwordHash))
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
	page, err := r.ListSignupInvitationsPage(ctx, types.PageRequest{})
	return page.Invitations, err
}

func (r *Repository) ListSignupInvitationsPage(ctx context.Context, pageRequest types.PageRequest) (types.SignupInvitationPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	rows, err := r.pool.Query(ctx, `
select id, created_by_user_id, created_at, expires_at, consumed_at, consumed_by_user_id, revoked_at
from signup_invitations
order by created_at desc, id desc
limit $1 offset $2
`, pageRequest.Limit+1, pageRequest.Offset)
	if err != nil {
		return types.SignupInvitationPage{}, err
	}
	invitations := []types.SignupInvitation{}
	for rows.Next() {
		invitation, err := scanSignupInvitation(rows)
		if err != nil {
			rows.Close()
			return types.SignupInvitationPage{}, err
		}
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return types.SignupInvitationPage{}, err
	}
	rows.Close()
	visible, pageInfo := types.PageWindow(pageRequest, len(invitations))
	invitations = invitations[:visible]
	if len(invitations) > 0 {
		invitationIDs := make([]string, 0, len(invitations))
		indexByID := make(map[string]int, len(invitations))
		for index, invitation := range invitations {
			invitationIDs = append(invitationIDs, invitation.ID)
			indexByID[invitation.ID] = index
			invitations[index].Teams = []types.Team{}
		}
		teamRows, err := r.pool.Query(ctx, `
select sit.invitation_id, t.id, t.slug, t.name, t.created_at, t.updated_at
from signup_invitation_teams sit
join teams t on t.id = sit.team_id
where sit.invitation_id = any($1)
order by sit.invitation_id, lower(t.name), lower(t.slug), t.id
`, invitationIDs)
		if err != nil {
			return types.SignupInvitationPage{}, err
		}
		for teamRows.Next() {
			var invitationID string
			var createdAt, updatedAt time.Time
			var team types.Team
			if err := teamRows.Scan(&invitationID, &team.ID, &team.Slug, &team.Name, &createdAt, &updatedAt); err != nil {
				teamRows.Close()
				return types.SignupInvitationPage{}, err
			}
			team.CreatedAt = isoMillis(createdAt)
			team.UpdatedAt = isoMillis(updatedAt)
			if index, ok := indexByID[invitationID]; ok {
				invitations[index].Teams = append(invitations[index].Teams, team)
			}
		}
		if err := teamRows.Err(); err != nil {
			teamRows.Close()
			return types.SignupInvitationPage{}, err
		}
		teamRows.Close()
	}
	return types.SignupInvitationPage{Invitations: invitations, Page: pageInfo}, nil
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
insert into users (id, email, display_name, password_hash, is_owner)
values ($1, $2, $3, $4, false)
returning id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
`, "usr_"+uuid.NewString(), email, displayName, passwordHash))
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
	page, err := r.ListTeamsPage(ctx, types.PageRequest{}, 10)
	if err != nil {
		return nil, err
	}
	teams := make([]types.Team, 0, len(page.Teams))
	for _, item := range page.Teams {
		teams = append(teams, item.Team)
	}
	return teams, nil
}

func (r *Repository) ListTeamsPage(ctx context.Context, pageRequest types.PageRequest, memberLimit int) (types.TeamPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	if memberLimit < 1 {
		memberLimit = 10
	}
	if memberLimit > 50 {
		memberLimit = 50
	}
	rows, err := r.pool.Query(ctx, `
select id, slug, name, created_at, updated_at
from teams
order by lower(name), lower(slug), id
limit $1 offset $2
`, pageRequest.Limit+1, pageRequest.Offset)
	if err != nil {
		return types.TeamPage{}, err
	}
	teams := []types.TeamWithMembers{}
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			rows.Close()
			return types.TeamPage{}, err
		}
		teams = append(teams, types.TeamWithMembers{Team: team, Members: []types.User{}})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return types.TeamPage{}, err
	}
	rows.Close()
	visible, pageInfo := types.PageWindow(pageRequest, len(teams))
	teams = teams[:visible]
	if len(teams) > 0 {
		teamIDs := make([]string, 0, len(teams))
		indexByID := make(map[string]int, len(teams))
		for index, team := range teams {
			teamIDs = append(teamIDs, team.ID)
			indexByID[team.ID] = index
		}
		memberRows, err := r.pool.Query(ctx, `
with ranked_members as (
  select
    tm.team_id,
    u.id,
    u.email,
    u.display_name,
    u.password_hash,
    u.is_owner,
    u.created_at,
    u.updated_at,
    u.disabled_at,
    row_number() over (partition by tm.team_id order by u.is_owner desc, lower(u.display_name), lower(u.email), u.id) as member_position,
    count(*) over (partition by tm.team_id)::int as member_count
  from team_memberships tm
  join users u on u.id = tm.user_id
  where tm.team_id = any($1)
)
select team_id, id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at, member_count
from ranked_members
where member_position <= $2
order by team_id, member_position
`, teamIDs, memberLimit)
		if err != nil {
			return types.TeamPage{}, err
		}
		for memberRows.Next() {
			var teamID string
			var user types.User
			var createdAt, updatedAt time.Time
			var disabledAt *time.Time
			var memberCount int
			if err := memberRows.Scan(&teamID, &user.ID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.IsOwner, &createdAt, &updatedAt, &disabledAt, &memberCount); err != nil {
				memberRows.Close()
				return types.TeamPage{}, err
			}
			user.CreatedAt = isoMillis(createdAt)
			user.UpdatedAt = isoMillis(updatedAt)
			user.DisabledAt = optionalISOTime(disabledAt)
			if index, ok := indexByID[teamID]; ok {
				teams[index].Members = append(teams[index].Members, user)
				teams[index].MemberCount = memberCount
			}
		}
		if err := memberRows.Err(); err != nil {
			memberRows.Close()
			return types.TeamPage{}, err
		}
		memberRows.Close()
		for index := range teams {
			fetched := teams[index].MemberCount
			if fetched > memberLimit {
				fetched = memberLimit + 1
			}
			_, teams[index].MembersPage = types.PageWindow(types.PageRequest{Limit: memberLimit}, fetched)
		}
	}
	return types.TeamPage{Teams: teams, Page: pageInfo}, nil
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
	return scanTeams(rows)
}

func (r *Repository) ListUserTeamsPage(ctx context.Context, userID string, pageRequest types.PageRequest) (types.UserTeamPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	rows, err := r.pool.Query(ctx, `
select t.id, t.slug, t.name, t.created_at, t.updated_at
from teams t
join team_memberships tm on tm.team_id = t.id
where tm.user_id = $1
order by lower(t.name), lower(t.slug), t.id
limit $2 offset $3
`, strings.TrimSpace(userID), pageRequest.Limit+1, pageRequest.Offset)
	if err != nil {
		return types.UserTeamPage{}, err
	}
	defer rows.Close()
	teams, err := scanTeams(rows)
	if err != nil {
		return types.UserTeamPage{}, err
	}
	visible, pageInfo := types.PageWindow(pageRequest, len(teams))
	return types.UserTeamPage{Teams: teams[:visible], Page: pageInfo}, nil
}

func (r *Repository) ListTeamMembers(ctx context.Context, teamID string) ([]types.User, error) {
	page, err := r.ListTeamMembersPage(ctx, teamID, types.PageRequest{})
	return page.Members, err
}

func (r *Repository) ListTeamMembersPage(ctx context.Context, teamID string, pageRequest types.PageRequest) (types.TeamMemberPage, error) {
	teamID = strings.TrimSpace(teamID)
	pageRequest = types.NormalizePageRequest(pageRequest)
	var exists bool
	if err := r.pool.QueryRow(ctx, `select exists (select 1 from teams where id = $1)`, teamID).Scan(&exists); err != nil {
		return types.TeamMemberPage{}, err
	}
	if !exists {
		return types.TeamMemberPage{}, types.ErrTeamNotFound
	}
	rows, err := r.pool.Query(ctx, `
select u.id, u.email, u.display_name, u.password_hash, u.is_owner,
       u.created_at, u.updated_at, u.disabled_at
from users u
join team_memberships tm on tm.user_id = u.id
where tm.team_id = $1
order by u.is_owner desc, lower(u.display_name), lower(u.email), u.id
limit $2 offset $3
`, teamID, pageRequest.Limit+1, pageRequest.Offset)
	if err != nil {
		return types.TeamMemberPage{}, err
	}
	defer rows.Close()
	users := []types.User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return types.TeamMemberPage{}, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return types.TeamMemberPage{}, err
	}
	visible, pageInfo := types.PageWindow(pageRequest, len(users))
	return types.TeamMemberPage{Members: users[:visible], Page: pageInfo}, nil
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
	var disabledAt *time.Time
	if err := tx.QueryRow(ctx, `select id, disabled_at from users where id = $1 for key share`, strings.TrimSpace(userID)).Scan(&lockedUserID, &disabledAt); errors.Is(err, pgx.ErrNoRows) {
		return types.ErrUserNotFound
	} else if err != nil {
		return err
	}
	if disabledAt != nil {
		return types.ErrUserDisabled
	}
	return nil
}

func (r *Repository) ListUsers(ctx context.Context) ([]types.User, error) {
	page, err := r.ListUsersPage(ctx, types.PageRequest{})
	return page.Users, err
}

func (r *Repository) ListUsersPage(ctx context.Context, pageRequest types.PageRequest) (types.UserPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	rows, err := r.pool.Query(ctx, `
select id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
from users
order by is_owner desc, created_at asc, id asc
limit $1 offset $2
`, pageRequest.Limit+1, pageRequest.Offset)
	if err != nil {
		return types.UserPage{}, err
	}
	defer rows.Close()
	users := []types.User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return types.UserPage{}, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return types.UserPage{}, err
	}
	visible, pageInfo := types.PageWindow(pageRequest, len(users))
	return types.UserPage{Users: users[:visible], Page: pageInfo}, nil
}

func (r *Repository) GetUserByID(ctx context.Context, userID string) (*types.User, error) {
	user, err := scanUser(r.pool.QueryRow(ctx, `
select id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
from users
where id = $1
`, strings.TrimSpace(userID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) SetUserDisabled(ctx context.Context, userID string, disabled bool) (types.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, $2))`, strings.TrimSpace(userID), attachmentPurgeAdvisoryNamespace); err != nil {
		return types.User{}, err
	}
	user, err := scanUser(tx.QueryRow(ctx, `
select id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
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
returning id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
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
		if _, err := tx.Exec(ctx, `delete from team_memberships where user_id = $1`, user.ID); err != nil {
			return types.User{}, err
		}
	} else {
		user, err = scanUser(tx.QueryRow(ctx, `
update users
set disabled_at = null, updated_at = now()
where id = $1
returning id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
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

func (r *Repository) CreateUser(ctx context.Context, email string, displayName string, passwordHash *string) (types.User, error) {
	row := r.pool.QueryRow(ctx, `
insert into users (id, email, display_name, password_hash, is_owner)
values ($1, $2, $3, $4, false)
returning id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
`, "usr_"+uuid.NewString(), strings.TrimSpace(email), strings.TrimSpace(displayName), passwordHash)
	return scanUser(row)
}

func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*types.User, error) {
	user, err := scanUser(r.pool.QueryRow(ctx, `
select id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
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
  u.email,
  u.display_name,
  u.password_hash,
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
select id, email, display_name, password_hash, is_owner, created_at, updated_at, disabled_at
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
	err := row.Scan(threadScanDest(&thread, &createdAt, &updatedAt)...)
	thread.CreatedAt = isoMillis(createdAt)
	thread.UpdatedAt = isoMillis(updatedAt)
	return thread, err
}

func scanThreadWithVisibility(row threadScanner) (types.Thread, error) {
	thread, _, err := scanThreadWithVisibilityPosition(row)
	return thread, err
}

func scanThreadWithVisibilityPosition(row threadScanner) (types.Thread, time.Time, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var thread types.Thread
	var ownedByMe bool
	var sharedTeamsJSON []byte
	var matchedTeamsJSON []byte
	var isPublic bool
	dest := threadScanDest(&thread, &createdAt, &updatedAt)
	dest = append(dest, &ownedByMe, &sharedTeamsJSON, &matchedTeamsJSON, &isPublic)
	if err := row.Scan(dest...); err != nil {
		return types.Thread{}, time.Time{}, err
	}
	thread.CreatedAt = isoMillis(createdAt)
	thread.UpdatedAt = isoMillis(updatedAt)
	visibility, err := decodeThreadVisibilitySummary(ownedByMe, sharedTeamsJSON, matchedTeamsJSON, isPublic)
	if err != nil {
		return types.Thread{}, time.Time{}, err
	}
	thread.VisibilitySummary = visibility
	return thread, updatedAt, nil
}

func scanThreadSummaryWithVisibilityPosition(row threadScanner) (types.Thread, time.Time, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var thread types.Thread
	var messageCount int
	var lastMessageBody string
	var ownedByMe bool
	var sharedTeamsJSON []byte
	var matchedTeamsJSON []byte
	var isPublic bool
	dest := threadScanDest(&thread, &createdAt, &updatedAt)
	dest = append(dest, &messageCount, &lastMessageBody, &ownedByMe, &sharedTeamsJSON, &matchedTeamsJSON, &isPublic)
	if err := row.Scan(dest...); err != nil {
		return types.Thread{}, time.Time{}, err
	}
	thread.CreatedAt = isoMillis(createdAt)
	thread.UpdatedAt = isoMillis(updatedAt)
	thread.MessageCount = &messageCount
	preview := previewText(lastMessageBody, 180)
	thread.LastMessagePreview = &preview
	visibility, err := decodeThreadVisibilitySummary(ownedByMe, sharedTeamsJSON, matchedTeamsJSON, isPublic)
	if err != nil {
		return types.Thread{}, time.Time{}, err
	}
	thread.VisibilitySummary = visibility
	return thread, updatedAt, nil
}

func threadScanDest(thread *types.Thread, createdAt *time.Time, updatedAt *time.Time) []any {
	return []any{
		&thread.ID,
		&thread.OwnerUserID,
		&thread.Title,
		createdAt,
		updatedAt,
		&thread.CreatedBy,
		&thread.CreatedByUserID,
		&thread.CreatedByKeyID,
		&thread.CreatedByUserDisplayName,
		&thread.CreatedByActorName,
	}
}

func decodeThreadVisibilitySummary(ownedByMe bool, sharedTeamsJSON []byte, matchedTeamsJSON []byte, isPublic bool) (types.ThreadVisibilitySummary, error) {
	sharedTeams := []types.ThreadTeamSummary{}
	matchedTeams := []types.ThreadTeamSummary{}
	if len(sharedTeamsJSON) > 0 {
		if err := json.Unmarshal(sharedTeamsJSON, &sharedTeams); err != nil {
			return types.ThreadVisibilitySummary{}, err
		}
	}
	if len(matchedTeamsJSON) > 0 {
		if err := json.Unmarshal(matchedTeamsJSON, &matchedTeams); err != nil {
			return types.ThreadVisibilitySummary{}, err
		}
	}
	return types.ThreadVisibilitySummary{
		OwnedByMe:    ownedByMe,
		Private:      ownedByMe && len(sharedTeams) == 0 && !isPublic,
		SharedWithMe: !ownedByMe && len(matchedTeams) > 0,
		SharedTeams:  sharedTeams,
		MatchedTeams: matchedTeams,
		Public:       isPublic,
	}, nil
}

func newPrivateThreadVisibilitySummary() types.ThreadVisibilitySummary {
	return types.ThreadVisibilitySummary{
		OwnedByMe:    true,
		Private:      true,
		SharedTeams:  []types.ThreadTeamSummary{},
		MatchedTeams: []types.ThreadTeamSummary{},
	}
}

func scanMessage(row threadScanner, assets []types.Asset) (types.Message, error) {
	var createdAt time.Time
	var bodyContentType *string
	var message types.Message
	err := row.Scan(
		&message.ID,
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
	var purgedAt *time.Time
	var purgeLastAttemptAt *time.Time
	var mimeType *string
	var asset types.Asset
	err := row.Scan(
		&asset.ID,
		&asset.MessageID,
		&asset.StorageKey,
		&asset.FileName,
		&mimeType,
		&asset.SizeBytes,
		&createdAt,
		&asset.CreatedBy,
		&asset.CreatedByUserID,
		&asset.CreatedByKeyID,
		&asset.CreatedByUserDisplayName,
		&asset.CreatedByActorName,
		&purgedAt,
		&asset.PurgedByUserID,
		&purgeLastAttemptAt,
		&asset.PurgeError,
	)
	asset.MimeType = mimeType
	asset.Filename = asset.FileName
	asset.DownloadURL = nil
	asset.CreatedAt = isoMillis(createdAt)
	asset.PurgedAt = optionalISOTime(purgedAt)
	asset.PurgeLastAttemptAt = optionalISOTime(purgeLastAttemptAt)
	return asset, err
}

func scanPendingUpload(row threadScanner) (types.PendingUpload, error) {
	var createdAt time.Time
	var expiresAt time.Time
	var consumedAt *time.Time
	var finalizationStartedAt *time.Time
	var rejectedAt *time.Time
	var mimeType *string
	var expectedSHA256 *string
	var finalStorageKey *string
	var finalizationToken *string
	var rejectionReason *string
	upload := types.PendingUpload{}
	err := row.Scan(
		&upload.ID,
		&upload.ThreadID,
		&upload.StorageKey,
		&upload.FileName,
		&mimeType,
		&upload.SizeBytes,
		&expectedSHA256,
		&upload.Status,
		&finalStorageKey,
		&finalizationToken,
		&finalizationStartedAt,
		&rejectedAt,
		&rejectionReason,
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
	upload.ExpectedSHA256 = valueOrEmpty(expectedSHA256)
	upload.FinalStorageKey = valueOrEmpty(finalStorageKey)
	upload.FinalizationToken = valueOrEmpty(finalizationToken)
	upload.FinalizationStartedAt = optionalISOTime(finalizationStartedAt)
	upload.RejectedAt = optionalISOTime(rejectedAt)
	upload.RejectionReason = valueOrEmpty(rejectionReason)
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

func scanAPIKeyWithSetup(row threadScanner) (types.APIKey, string, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var lastUsedAt *time.Time
	var revokedAt *time.Time
	var setupBaseURL string
	key := types.APIKey{}
	err := row.Scan(
		&key.ID,
		&key.UserID,
		&key.Name,
		&key.Purpose,
		&key.TokenPrefix,
		&key.TokenHash,
		&key.Scopes,
		&createdAt,
		&updatedAt,
		&lastUsedAt,
		&revokedAt,
		&setupBaseURL,
	)
	key.KeyMasked = maskSecret(key.TokenPrefix)
	key.CreatedAt = isoMillis(createdAt)
	key.UpdatedAt = isoMillis(updatedAt)
	key.LastUsedAt = optionalISOTime(lastUsedAt)
	key.RevokedAt = optionalISOTime(revokedAt)
	return key, setupBaseURL, err
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
		&user.Email,
		&user.DisplayName,
		&user.PasswordHash,
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

func scanUser(row threadScanner) (types.User, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var disabledAt *time.Time
	user := types.User{}
	err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.IsOwner, &createdAt, &updatedAt, &disabledAt)
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
		&user.Email,
		&user.DisplayName,
		&user.PasswordHash,
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

func scanThreadPublicLink(row threadScanner) (types.ThreadPublicLink, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var revokedAt *time.Time
	link := types.ThreadPublicLink{}
	err := row.Scan(
		&link.ThreadID,
		&link.Token,
		&link.TokenHash,
		&link.TokenPrefix,
		&link.CreatedByUserID,
		&createdAt,
		&updatedAt,
		&revokedAt,
	)
	link.CreatedAt = isoMillis(createdAt)
	link.UpdatedAt = isoMillis(updatedAt)
	link.RevokedAt = optionalISOTime(revokedAt)
	return link, err
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
