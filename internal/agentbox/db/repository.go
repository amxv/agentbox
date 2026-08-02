package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
for update of t
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
	currentTeams, err := listThreadTeamsTx(ctx, tx, threadID)
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	currentTeamIDs := make(map[string]struct{}, len(currentTeams))
	for _, team := range currentTeams {
		currentTeamIDs[team.ID] = struct{}{}
	}
	additionIDs := make([]string, 0, len(teamIDs))
	for _, teamID := range teamIDs {
		if _, exists := currentTeamIDs[teamID]; !exists {
			additionIDs = append(additionIDs, teamID)
		}
	}
	if err := requireUserTeamMembershipsTx(ctx, tx, userID, additionIDs); err != nil {
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
	threadQuery := `
select t.owner_user_id
from threads t
where ` + normalThreadAccessPredicate + ` and t.id = $2`
	if mutation {
		threadQuery += ` for update of t`
	}
	if err := tx.QueryRow(ctx, threadQuery, userID, threadID).Scan(&state.OwnerUserID); errors.Is(err, pgx.ErrNoRows) {
		return types.ManagedThreadVisibility{}, types.ErrThreadNotFound
	} else if err != nil {
		return types.ManagedThreadVisibility{}, err
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

func (r *Repository) GetThreadPublicLink(ctx context.Context, userID string, threadID string) (*types.ThreadPublicLink, error) {
	link, err := scanThreadPublicLink(r.pool.QueryRow(ctx, `
select
  link.thread_id,
  coalesce(link.token_value, ''),
  link.token_hash,
  link.token_prefix,
  link.created_by_user_id,
  link.created_at,
  link.updated_at,
  link.revoked_at
from threads t
join thread_public_links link on link.thread_id = t.id
where `+normalThreadAccessPredicate+`
  and t.id = $2
  and link.revoked_at is null
`, userID, threadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *Repository) CreateThreadPublicLink(ctx context.Context, userID string, threadID string, token string, tokenHash string, tokenPrefix string, rotate bool) (types.ThreadPublicLink, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return types.ThreadPublicLink{}, err
	}
	defer tx.Rollback(ctx)

	var lockedThreadID string
	if err := tx.QueryRow(ctx, `
select t.id
from threads t
where `+normalThreadAccessPredicate+` and t.id = $2
for update of t
`, userID, threadID).Scan(&lockedThreadID); errors.Is(err, pgx.ErrNoRows) {
		return types.ThreadPublicLink{}, types.ErrThreadNotFound
	} else if err != nil {
		return types.ThreadPublicLink{}, err
	}

	var activeExists bool
	if err := tx.QueryRow(ctx, `
select exists (
  select 1
  from thread_public_links
  where thread_id = $1 and revoked_at is null
)
`, threadID).Scan(&activeExists); err != nil {
		return types.ThreadPublicLink{}, err
	}
	if activeExists && !rotate {
		return types.ThreadPublicLink{}, types.ErrThreadPublicLinkExists
	}

	link, err := scanThreadPublicLink(tx.QueryRow(ctx, `
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
  token_value,
  token_hash,
  token_prefix,
  created_by_user_id,
  created_at,
  updated_at,
  revoked_at
`, threadID, token, tokenHash, tokenPrefix, userID))
	if err != nil {
		return types.ThreadPublicLink{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return types.ThreadPublicLink{}, err
	}
	return link, nil
}

func (r *Repository) RevokeThreadPublicLink(ctx context.Context, userID string, threadID string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var lockedThreadID string
	if err := tx.QueryRow(ctx, `
select t.id
from threads t
where `+normalThreadAccessPredicate+` and t.id = $2
for update of t
`, userID, threadID).Scan(&lockedThreadID); errors.Is(err, pgx.ErrNoRows) {
		return false, types.ErrThreadNotFound
	} else if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `
update thread_public_links
set revoked_at = now(), updated_at = now()
where thread_id = $1 and revoked_at is null
`, threadID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) GetThreadByPublicTokenHash(ctx context.Context, tokenHash string) (*types.ThreadWithMessages, error) {
	thread, err := scanThread(r.pool.QueryRow(ctx, `
select
  t.id,
  t.tenant_id,
  t.owner_user_id,
  t.title,
  t.created_at,
  t.updated_at,
  t.created_by,
  t.created_by_user_id,
  t.created_by_key_id,
  t.created_by_user_display_name,
  t.created_by_actor_name
from thread_public_links link
join threads t on t.id = link.thread_id
where link.token_hash = $1 and link.revoked_at is null
`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	messages, err := r.loadThreadMessages(ctx, thread.ID)
	if err != nil {
		return nil, err
	}
	return &types.ThreadWithMessages{
		Thread:     thread,
		Messages:   messages,
		Visibility: types.ThreadVisibility{ThreadID: thread.ID, OwnerUserID: thread.OwnerUserID},
	}, nil
}

func (r *Repository) GetAssetByPublicTokenHash(ctx context.Context, tokenHash string, assetID string) (*types.Asset, error) {
	asset, err := scanAsset(r.pool.QueryRow(ctx, `
select
  a.id,
  a.tenant_id,
  a.message_id,
  a.storage_key,
  a.file_name,
  a.mime_type,
  a.size_bytes,
  null::text as public_url,
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
from thread_public_links link
join messages m on m.thread_id = link.thread_id
join assets a on a.message_id = m.id
where link.token_hash = $1
  and link.revoked_at is null
  and a.id = $2
`, tokenHash, assetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *Repository) loadThreadMessages(ctx context.Context, threadID string) ([]types.Message, error) {
	rows, err := r.pool.Query(ctx, `
select
  id,
  tenant_id,
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
order by created_at, id
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
		messageIndex[message.ID] = len(messages)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	assetRows, err := r.pool.Query(ctx, `
select
  a.id,
  a.tenant_id,
  a.message_id,
  a.storage_key,
  a.file_name,
  a.mime_type,
  a.size_bytes,
  null::text as public_url,
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
order by a.created_at, a.id
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
	if strings.TrimSpace(params.Filter) == "" {
		params.Filter = types.ThreadFilterAll
	}
	if params.Limit <= 0 {
		params.Limit = 50
	}
	rows, err := r.pool.Query(ctx, `
select
  t.id,
  t.tenant_id,
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
where `+normalThreadAccessPredicate+`
  and `+threadFilterPredicate("$2", "$3")+`
order by t.updated_at desc, t.id
limit $4
`, userID, params.Filter, params.TeamRef, params.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	threads := []types.Thread{}
	for rows.Next() {
		thread, err := scanThreadWithVisibility(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, thread)
	}
	return threads, rows.Err()
}

func (r *Repository) SearchThreads(ctx context.Context, userID string, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
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
  t.created_by_user_display_name,
  t.created_by_actor_name,
  (select count(*)::int from messages counted_message where counted_message.thread_id = t.id) as message_count,
  coalesce((select lm.body from messages lm where lm.thread_id = t.id order by lm.created_at desc limit 1), '') as last_message_body,
  coalesce((select mm.body from messages mm where mm.thread_id = t.id and mm.body ilike $2 order by mm.created_at desc limit 1), '') as matched_message_body,
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
order by t.updated_at desc
limit $7
`, userID, pattern, createdBy, updatedAfter, params.Filter, params.TeamRef, params.Limit)
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
		var ownedByMe bool
		var sharedTeamsJSON []byte
		var matchedTeamsJSON []byte
		var isPublic bool
		result := types.SearchThreadResult{}
		if err := rows.Scan(
			&result.ID,
			&result.TenantID,
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
			return nil, err
		}
		result.VisibilitySummary, err = decodeThreadVisibilitySummary(ownedByMe, sharedTeamsJSON, matchedTeamsJSON, isPublic)
		if err != nil {
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

func (r *Repository) ListOwnerContentThreads(ctx context.Context, ownerUserID string, params types.OwnerContentListParams) ([]types.OwnerContentThreadSummary, error) {
	return r.queryOwnerContentThreads(ctx, ownerUserID, "", params.UserID, params.TeamRef, params.Limit)
}

func (r *Repository) SearchOwnerContentThreads(ctx context.Context, ownerUserID string, params types.OwnerContentSearchParams) ([]types.OwnerContentThreadSummary, error) {
	return r.queryOwnerContentThreads(ctx, ownerUserID, params.Query, params.UserID, params.TeamRef, params.Limit)
}

func (r *Repository) queryOwnerContentThreads(ctx context.Context, ownerUserID string, query string, userID string, teamRef string, limit int) ([]types.OwnerContentThreadSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	pattern := ""
	if strings.TrimSpace(query) != "" {
		pattern = "%" + strings.TrimSpace(query) + "%"
	}
	rows, err := r.pool.Query(ctx, `
select
  t.id,
  t.tenant_id,
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
  owner.tenant_id,
  owner.email,
  owner.display_name,
  owner.password_hash,
  owner.role,
  owner.is_owner,
  owner.created_at,
  owner.updated_at,
  owner.disabled_at,
  (select count(*)::int from messages counted_message where counted_message.thread_id = t.id) as message_count,
  coalesce((select lm.body from messages lm where lm.thread_id = t.id order by lm.created_at desc, lm.id desc limit 1), '') as last_message_body,
  coalesce((select mm.body from messages mm where $2 <> '' and mm.thread_id = t.id and mm.body ilike $2 order by mm.created_at desc, mm.id desc limit 1), '') as matched_message_body
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
limit $5
`, strings.TrimSpace(ownerUserID), pattern, strings.TrimSpace(userID), strings.TrimSpace(teamRef), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []types.OwnerContentThreadSummary{}
	for rows.Next() {
		result, err := scanOwnerContentThreadSummary(rows, query)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func (r *Repository) GetOwnerContentThread(ctx context.Context, ownerUserID string, threadID string) (*types.OwnerContentThreadDetail, error) {
	summary, err := scanOwnerContentThreadSummary(r.pool.QueryRow(ctx, `
select
  t.id,
  t.tenant_id,
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
  owner.tenant_id,
  owner.email,
  owner.display_name,
  owner.password_hash,
  owner.role,
  owner.is_owner,
  owner.created_at,
  owner.updated_at,
  owner.disabled_at,
  (select count(*)::int from messages counted_message where counted_message.thread_id = t.id) as message_count,
  coalesce((select lm.body from messages lm where lm.thread_id = t.id order by lm.created_at desc, lm.id desc limit 1), '') as last_message_body,
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
  a.tenant_id,
  a.message_id,
  a.storage_key,
  a.file_name,
  a.mime_type,
  a.size_bytes,
  null::text as public_url,
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
		&result.TenantID,
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
		&result.Owner.TenantID,
		&result.Owner.Email,
		&result.Owner.DisplayName,
		&result.Owner.PasswordHash,
		&result.Owner.Role,
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
  id, tenant_id, owner_user_id, title, created_by, created_by_user_id, created_by_key_id,
  created_by_user_display_name, created_by_actor_name
)
values ($1, 'ten_default', $2, $3, $4, $5, $6, $7, $8)
returning id, tenant_id, owner_user_id, title, created_at, updated_at, created_by,
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
	thread.VisibilitySummary = newPrivateThreadVisibilitySummary()
	return thread, message, nil
}

func (r *Repository) GetThread(ctx context.Context, userID string, threadID string) (*types.ThreadWithMessages, error) {
	thread, err := scanThreadWithVisibility(r.pool.QueryRow(ctx, `
select
  t.id,
  t.tenant_id,
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
       created_by_user_display_name, created_by_actor_name,
       purged_at, purged_by_user_id, purge_last_attempt_at, purge_error
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
    purge_error = null,
    public_url = null
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
          created_by_user_display_name, created_by_actor_name,
          purged_at, purged_by_user_id, purge_last_attempt_at, purge_error
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

func (r *Repository) ListAllAPIKeys(ctx context.Context) ([]types.APIKey, error) {
	rows, err := r.pool.Query(ctx, `
select id, user_id, name, purpose, token_prefix, token_hash, scopes, created_at, updated_at, last_used_at, revoked_at
from api_keys
order by user_id, lower(name), created_at, id
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

func (r *Repository) RevokeAPIKey(ctx context.Context, userID string, name string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `update api_keys set revoked_at = now(), updated_at = now() where user_id = $1 and lower(name) = lower($2) and revoked_at is null`, userID, name)
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

func (r *Repository) GetUserByID(ctx context.Context, userID string) (*types.User, error) {
	user, err := scanUser(r.pool.QueryRow(ctx, `
select id, tenant_id, email, display_name, password_hash, role, is_owner, disabled_at, created_at, updated_at
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
		if _, err := tx.Exec(ctx, `delete from team_memberships where user_id = $1`, user.ID); err != nil {
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
	err := row.Scan(threadScanDest(&thread, &createdAt, &updatedAt)...)
	thread.CreatedAt = isoMillis(createdAt)
	thread.UpdatedAt = isoMillis(updatedAt)
	return thread, err
}

func scanThreadWithVisibility(row threadScanner) (types.Thread, error) {
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
		return types.Thread{}, err
	}
	thread.CreatedAt = isoMillis(createdAt)
	thread.UpdatedAt = isoMillis(updatedAt)
	visibility, err := decodeThreadVisibilitySummary(ownedByMe, sharedTeamsJSON, matchedTeamsJSON, isPublic)
	if err != nil {
		return types.Thread{}, err
	}
	thread.VisibilitySummary = visibility
	return thread, nil
}

func threadScanDest(thread *types.Thread, createdAt *time.Time, updatedAt *time.Time) []any {
	return []any{
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
	var purgedAt *time.Time
	var purgeLastAttemptAt *time.Time
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
		&purgedAt,
		&asset.PurgedByUserID,
		&purgeLastAttemptAt,
		&asset.PurgeError,
	)
	asset.MimeType = mimeType
	asset.PublicURL = nil
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
