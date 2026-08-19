package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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

func (r *Repository) GetMessage(ctx context.Context, userID string, messageID string) (*types.Message, error) {
	message, err := scanMessage(r.pool.QueryRow(ctx, `
select
  m.id,
  m.thread_id,
  m.author,
  m.body,
  m.body_content_type,
  m.created_at,
  m.created_by_user_id,
  m.created_by_key_id,
  m.created_by_user_display_name,
  m.created_by_actor_name
from messages m
join threads t on t.id = m.thread_id
where `+normalThreadAccessPredicate+` and m.id = $2
`, userID, messageID), nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
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
join threads t on t.id = m.thread_id
where `+normalThreadAccessPredicate+` and m.id = $2
order by a.position
`, userID, messageID)
	if err != nil {
		return nil, err
	}
	defer assetRows.Close()
	for assetRows.Next() {
		asset, err := scanAsset(assetRows)
		if err != nil {
			return nil, err
		}
		asset.Position = int64(len(message.Assets) + 1)
		message.Assets = append(message.Assets, asset)
	}
	if err := assetRows.Err(); err != nil {
		return nil, err
	}
	return &message, nil
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
