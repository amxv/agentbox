package db

import (
	"context"
	"sort"
	"strings"
	"time"

	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
)

func (m *MemoryRepository) CreateTeam(_ context.Context, slug string, name string) (types.Team, error) {
	slug = strings.TrimSpace(slug)
	name = strings.TrimSpace(name)
	for _, team := range m.Teams {
		if strings.EqualFold(team.Slug, slug) {
			return types.Team{}, types.ErrTeamSlugConflict
		}
	}
	now := isoMillis(time.Now().UTC())
	team := types.Team{ID: "team_" + uuid.NewString(), Slug: slug, Name: name, CreatedAt: now, UpdatedAt: now}
	m.Teams = append(m.Teams, team)
	return team, nil
}

func (m *MemoryRepository) RenameTeam(_ context.Context, teamID string, name string) (types.Team, error) {
	teamID = strings.TrimSpace(teamID)
	for index := range m.Teams {
		if m.Teams[index].ID != teamID {
			continue
		}
		m.Teams[index].Name = strings.TrimSpace(name)
		m.Teams[index].UpdatedAt = isoMillis(time.Now().UTC())
		return m.Teams[index], nil
	}
	return types.Team{}, types.ErrTeamNotFound
}

func (m *MemoryRepository) ListTeams(ctx context.Context) ([]types.Team, error) {
	page, err := m.ListTeamsPage(ctx, types.PageRequest{}, 10)
	if err != nil {
		return nil, err
	}
	teams := make([]types.Team, 0, len(page.Teams))
	for _, item := range page.Teams {
		teams = append(teams, item.Team)
	}
	return teams, nil
}

func (m *MemoryRepository) ListTeamsPage(_ context.Context, pageRequest types.PageRequest, memberLimit int) (types.TeamPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	if memberLimit < 1 {
		memberLimit = 10
	}
	if memberLimit > 50 {
		memberLimit = 50
	}
	teams := append([]types.Team(nil), m.Teams...)
	sort.SliceStable(teams, func(i, j int) bool {
		if !strings.EqualFold(teams[i].Name, teams[j].Name) {
			return strings.ToLower(teams[i].Name) < strings.ToLower(teams[j].Name)
		}
		if !strings.EqualFold(teams[i].Slug, teams[j].Slug) {
			return strings.ToLower(teams[i].Slug) < strings.ToLower(teams[j].Slug)
		}
		return teams[i].ID < teams[j].ID
	})
	start := pageRequest.Offset
	if start > len(teams) {
		start = len(teams)
	}
	end := start + pageRequest.Limit + 1
	if end > len(teams) {
		end = len(teams)
	}
	window := teams[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	result := make([]types.TeamWithMembers, 0, visible)
	for _, team := range window[:visible] {
		members := []types.User{}
		wanted := map[string]bool{}
		for _, membership := range m.TeamMemberships {
			if membership.TeamID == team.ID {
				wanted[membership.UserID] = true
			}
		}
		for _, user := range m.Users {
			if wanted[user.ID] {
				members = append(members, user)
			}
		}
		sort.SliceStable(members, func(i, j int) bool {
			if members[i].IsOwner != members[j].IsOwner {
				return members[i].IsOwner
			}
			if !strings.EqualFold(members[i].DisplayName, members[j].DisplayName) {
				return strings.ToLower(members[i].DisplayName) < strings.ToLower(members[j].DisplayName)
			}
			return members[i].ID < members[j].ID
		})
		fetched := len(members)
		if fetched > memberLimit {
			fetched = memberLimit + 1
		}
		memberVisible, memberPage := types.PageWindow(types.PageRequest{Limit: memberLimit}, fetched)
		if memberVisible > len(members) {
			memberVisible = len(members)
		}
		result = append(result, types.TeamWithMembers{Team: team, Members: members[:memberVisible], MemberCount: len(members), MembersPage: memberPage})
	}
	return types.TeamPage{Teams: result, Page: pageInfo}, nil
}

func (m *MemoryRepository) ListUserTeams(_ context.Context, userID string) ([]types.Team, error) {
	wanted := map[string]bool{}
	for _, membership := range m.TeamMemberships {
		if membership.UserID == strings.TrimSpace(userID) {
			wanted[membership.TeamID] = true
		}
	}
	teams := []types.Team{}
	for _, team := range m.Teams {
		if wanted[team.ID] {
			teams = append(teams, team)
		}
	}
	sort.SliceStable(teams, func(i, j int) bool {
		if !strings.EqualFold(teams[i].Name, teams[j].Name) {
			return strings.ToLower(teams[i].Name) < strings.ToLower(teams[j].Name)
		}
		return teams[i].ID < teams[j].ID
	})
	return teams, nil
}

func (m *MemoryRepository) ListUserTeamsPage(_ context.Context, userID string, pageRequest types.PageRequest) (types.UserTeamPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	wanted := map[string]bool{}
	for _, membership := range m.TeamMemberships {
		if membership.UserID == strings.TrimSpace(userID) {
			wanted[membership.TeamID] = true
		}
	}
	teams := []types.Team{}
	for _, team := range m.Teams {
		if wanted[team.ID] {
			teams = append(teams, team)
		}
	}
	sort.SliceStable(teams, func(i, j int) bool {
		if !strings.EqualFold(teams[i].Name, teams[j].Name) {
			return strings.ToLower(teams[i].Name) < strings.ToLower(teams[j].Name)
		}
		return teams[i].ID < teams[j].ID
	})
	start := pageRequest.Offset
	if start > len(teams) {
		start = len(teams)
	}
	end := start + pageRequest.Limit + 1
	if end > len(teams) {
		end = len(teams)
	}
	window := teams[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.UserTeamPage{Teams: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) ListTeamMembers(ctx context.Context, teamID string) ([]types.User, error) {
	page, err := m.ListTeamMembersPage(ctx, teamID, types.PageRequest{})
	return page.Members, err
}

func (m *MemoryRepository) ListTeamMembersPage(_ context.Context, teamID string, pageRequest types.PageRequest) (types.TeamMemberPage, error) {
	teamID = strings.TrimSpace(teamID)
	pageRequest = types.NormalizePageRequest(pageRequest)
	teamFound := false
	for _, team := range m.Teams {
		if team.ID == teamID {
			teamFound = true
			break
		}
	}
	if !teamFound {
		return types.TeamMemberPage{}, types.ErrTeamNotFound
	}
	wanted := map[string]bool{}
	for _, membership := range m.TeamMemberships {
		if membership.TeamID == teamID {
			wanted[membership.UserID] = true
		}
	}
	users := []types.User{}
	for _, user := range m.Users {
		if wanted[user.ID] {
			users = append(users, user)
		}
	}
	sort.SliceStable(users, func(i, j int) bool {
		if users[i].IsOwner != users[j].IsOwner {
			return users[i].IsOwner
		}
		if !strings.EqualFold(users[i].DisplayName, users[j].DisplayName) {
			return strings.ToLower(users[i].DisplayName) < strings.ToLower(users[j].DisplayName)
		}
		return users[i].ID < users[j].ID
	})
	start := pageRequest.Offset
	if start > len(users) {
		start = len(users)
	}
	end := start + pageRequest.Limit + 1
	if end > len(users) {
		end = len(users)
	}
	window := users[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.TeamMemberPage{Members: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) AddTeamMember(_ context.Context, teamID string, userID string) (types.TeamMembership, error) {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	teamFound := false
	for _, team := range m.Teams {
		if team.ID == teamID {
			teamFound = true
			break
		}
	}
	if !teamFound {
		return types.TeamMembership{}, types.ErrTeamNotFound
	}
	userFound := false
	for _, user := range m.Users {
		if user.ID == userID {
			if user.DisabledAt != nil {
				return types.TeamMembership{}, types.ErrUserDisabled
			}
			userFound = true
			break
		}
	}
	if !userFound {
		return types.TeamMembership{}, types.ErrUserNotFound
	}
	for _, membership := range m.TeamMemberships {
		if membership.TeamID == teamID && membership.UserID == userID {
			return membership, nil
		}
	}
	membership := types.TeamMembership{TeamID: teamID, UserID: userID, CreatedAt: isoMillis(time.Now().UTC())}
	m.TeamMemberships = append(m.TeamMemberships, membership)
	return membership, nil
}

func (m *MemoryRepository) RemoveTeamMember(_ context.Context, teamID string, userID string) (bool, error) {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	teamFound := false
	for _, team := range m.Teams {
		if team.ID == teamID {
			teamFound = true
			break
		}
	}
	if !teamFound {
		return false, types.ErrTeamNotFound
	}
	userFound := false
	for _, user := range m.Users {
		if user.ID == userID {
			userFound = true
			break
		}
	}
	if !userFound {
		return false, types.ErrUserNotFound
	}
	for index, membership := range m.TeamMemberships {
		if membership.TeamID == teamID && membership.UserID == userID {
			m.TeamMemberships = append(m.TeamMemberships[:index], m.TeamMemberships[index+1:]...)
			return true, nil
		}
	}
	return false, nil
}
