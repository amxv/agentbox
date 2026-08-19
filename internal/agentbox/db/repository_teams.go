package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

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
