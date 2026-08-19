package db

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"agentbox/internal/agentbox/identity"
	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
)

func (m *MemoryRepository) BootstrapOwner(_ context.Context, email string, displayName string, passwordHash string) (types.User, error) {
	now := isoMillis(time.Now())
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if email == "" || displayName == "" || passwordHash == "" {
		return types.User{}, errors.New("owner email, display name, and password hash are required")
	}
	for i := range m.Users {
		if m.Users[i].IsOwner {
			if !strings.EqualFold(m.Users[i].Email, email) {
				return types.User{}, ErrOwnerAlreadyExists
			}
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignUnownedThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	for i := range m.Users {
		if strings.EqualFold(m.Users[i].Email, email) {
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].IsOwner = true
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignUnownedThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	owner := types.User{
		ID:           identity.OwnerIDForEmail(email),
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		IsOwner:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.Users = append(m.Users, owner)
	m.assignUnownedThreadsToOwner(owner.ID)
	return owner, nil
}

func (m *MemoryRepository) CreateOwnerSetupToken(_ context.Context, tokenHash string, expiresAt time.Time) (types.OwnerSetupToken, error) {
	if strings.TrimSpace(tokenHash) == "" || !expiresAt.After(time.Now().UTC()) {
		return types.OwnerSetupToken{}, errors.New("owner setup token hash and future expiry are required")
	}
	now := time.Now().UTC()
	nowValue := isoMillis(now)
	for index := range m.OwnerSetupTokens {
		if m.OwnerSetupTokens[index].Token.ConsumedAt == nil && m.OwnerSetupTokens[index].Token.RevokedAt == nil {
			m.OwnerSetupTokens[index].Token.RevokedAt = &nowValue
		}
	}
	purpose := "bootstrap"
	for _, user := range m.Users {
		if user.IsOwner {
			purpose = "recovery"
			break
		}
	}
	token := types.OwnerSetupToken{
		ID:        "ost_" + uuid.NewString(),
		Purpose:   purpose,
		CreatedAt: nowValue,
		ExpiresAt: isoMillis(expiresAt),
	}
	m.OwnerSetupTokens = append(m.OwnerSetupTokens, memoryOwnerSetupToken{Token: token, TokenHash: tokenHash})
	return token, nil
}

func (m *MemoryRepository) UseOwnerSetupToken(_ context.Context, tokenHash string, email string, displayName string, passwordHash string) (types.User, types.OwnerSetupToken, error) {
	now := time.Now().UTC()
	for index := range m.OwnerSetupTokens {
		entry := &m.OwnerSetupTokens[index]
		expiresAt, err := time.Parse(time.RFC3339Nano, entry.Token.ExpiresAt)
		if err != nil || entry.TokenHash != tokenHash || entry.Token.ConsumedAt != nil || entry.Token.RevokedAt != nil || !expiresAt.After(now) {
			continue
		}
		ownerIndex := -1
		for userIndex := range m.Users {
			if m.Users[userIndex].IsOwner {
				ownerIndex = userIndex
				break
			}
		}
		if entry.Token.Purpose == "bootstrap" && ownerIndex >= 0 {
			return types.User{}, types.OwnerSetupToken{}, ErrOwnerSetupTokenInvalid
		}
		if entry.Token.Purpose == "recovery" && ownerIndex < 0 {
			return types.User{}, types.OwnerSetupToken{}, ErrOwnerSetupTokenInvalid
		}
		if ownerIndex >= 0 && !strings.EqualFold(m.Users[ownerIndex].Email, strings.TrimSpace(email)) {
			return types.User{}, types.OwnerSetupToken{}, ErrOwnerAlreadyExists
		}
		owner, err := m.bootstrapOwner(strings.TrimSpace(email), strings.TrimSpace(displayName), passwordHash)
		if err != nil {
			return types.User{}, types.OwnerSetupToken{}, err
		}
		consumedAt := isoMillis(now)
		entry.Token.ConsumedAt = &consumedAt
		return owner, entry.Token, nil
	}
	return types.User{}, types.OwnerSetupToken{}, ErrOwnerSetupTokenInvalid
}

func (m *MemoryRepository) bootstrapOwner(email string, displayName string, passwordHash string) (types.User, error) {
	now := isoMillis(time.Now())
	if email == "" || displayName == "" || passwordHash == "" {
		return types.User{}, errors.New("owner email, display name, and password hash are required")
	}
	for i := range m.Users {
		if m.Users[i].IsOwner {
			if !strings.EqualFold(m.Users[i].Email, email) {
				return types.User{}, ErrOwnerAlreadyExists
			}
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignUnownedThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	for i := range m.Users {
		if strings.EqualFold(m.Users[i].Email, email) {
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].IsOwner = true
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignUnownedThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	owner := types.User{
		ID:           identity.OwnerIDForEmail(email),
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		IsOwner:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.Users = append(m.Users, owner)
	m.assignUnownedThreadsToOwner(owner.ID)
	return owner, nil
}

// assignUnownedThreadsToOwner keeps the in-memory repository useful for tests
// that construct pre-owner fixtures. PostgreSQL enforces non-null thread ownership
// in the current schema.
func (m *MemoryRepository) assignUnownedThreadsToOwner(ownerUserID string) {
	for i := range m.Threads {
		if strings.TrimSpace(m.Threads[i].OwnerUserID) == "" {
			m.Threads[i].OwnerUserID = ownerUserID
		}
	}
}

func (m *MemoryRepository) CreateSignupInvitation(_ context.Context, createdByUserID string, tokenHash string, expiresAt time.Time, teamIDs []string) (types.SignupInvitation, error) {
	if strings.TrimSpace(createdByUserID) == "" || strings.TrimSpace(tokenHash) == "" || !expiresAt.After(time.Now().UTC()) {
		return types.SignupInvitation{}, errors.New("invitation creator, token hash, and future expiry are required")
	}
	teamIDs = uniqueNonEmptyStrings(teamIDs)
	teams := make([]types.Team, 0, len(teamIDs))
	for _, teamID := range teamIDs {
		found := false
		for _, team := range m.Teams {
			if team.ID == teamID {
				teams = append(teams, team)
				found = true
				break
			}
		}
		if !found {
			return types.SignupInvitation{}, types.ErrTeamNotFound
		}
	}
	now := isoMillis(time.Now().UTC())
	invitation := types.SignupInvitation{
		ID:              "inv_" + uuid.NewString(),
		CreatedByUserID: createdByUserID,
		CreatedAt:       now,
		ExpiresAt:       isoMillis(expiresAt),
		Teams:           teams,
	}
	m.SignupInvitations = append(m.SignupInvitations, memorySignupInvitation{Invitation: invitation, TokenHash: tokenHash, TeamIDs: teamIDs})
	return invitation, nil
}

func (m *MemoryRepository) ListSignupInvitations(ctx context.Context) ([]types.SignupInvitation, error) {
	page, err := m.ListSignupInvitationsPage(ctx, types.PageRequest{})
	return page.Invitations, err
}

func (m *MemoryRepository) ListSignupInvitationsPage(_ context.Context, pageRequest types.PageRequest) (types.SignupInvitationPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	invitations := make([]types.SignupInvitation, 0, len(m.SignupInvitations))
	for index := len(m.SignupInvitations) - 1; index >= 0; index-- {
		invitation, err := m.hydrateSignupInvitation(m.SignupInvitations[index])
		if err != nil {
			return types.SignupInvitationPage{}, err
		}
		invitations = append(invitations, invitation)
	}
	start := pageRequest.Offset
	if start > len(invitations) {
		start = len(invitations)
	}
	end := start + pageRequest.Limit + 1
	if end > len(invitations) {
		end = len(invitations)
	}
	window := invitations[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.SignupInvitationPage{Invitations: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) RevokeSignupInvitation(_ context.Context, invitationID string) (bool, error) {
	now := isoMillis(time.Now().UTC())
	for index := range m.SignupInvitations {
		invitation := &m.SignupInvitations[index].Invitation
		if invitation.ID == invitationID && invitation.ConsumedAt == nil && invitation.RevokedAt == nil {
			invitation.RevokedAt = &now
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryRepository) FindSignupInvitation(_ context.Context, tokenHash string) (*types.SignupInvitation, error) {
	now := time.Now().UTC()
	for _, entry := range m.SignupInvitations {
		expiresAt, err := time.Parse(time.RFC3339Nano, entry.Invitation.ExpiresAt)
		if err != nil || entry.TokenHash != tokenHash || entry.Invitation.ConsumedAt != nil || entry.Invitation.RevokedAt != nil || !expiresAt.After(now) {
			continue
		}
		invitation, err := m.hydrateSignupInvitation(entry)
		if err != nil {
			return nil, err
		}
		return &invitation, nil
	}
	return nil, nil
}

func (m *MemoryRepository) RegisterWithSignupInvitation(_ context.Context, tokenHash string, email string, displayName string, passwordHash string, sessionSecretHash string, sessionExpiresAt time.Time) (types.User, types.UserSession, types.SignupInvitation, error) {
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	now := time.Now().UTC()
	invitationIndex := -1
	for index, entry := range m.SignupInvitations {
		expiresAt, err := time.Parse(time.RFC3339Nano, entry.Invitation.ExpiresAt)
		if err == nil && entry.TokenHash == tokenHash && entry.Invitation.ConsumedAt == nil && entry.Invitation.RevokedAt == nil && expiresAt.After(now) {
			invitationIndex = index
			break
		}
	}
	if invitationIndex < 0 || email == "" || displayName == "" || passwordHash == "" || sessionSecretHash == "" || !sessionExpiresAt.After(now) {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, types.ErrSignupInvitationInvalid
	}
	for _, user := range m.Users {
		if strings.EqualFold(strings.TrimSpace(user.Email), email) {
			return types.User{}, types.UserSession{}, types.SignupInvitation{}, types.ErrEmailAlreadyRegistered
		}
	}
	nowValue := isoMillis(now)
	invitationValue, err := m.hydrateSignupInvitation(m.SignupInvitations[invitationIndex])
	if err != nil {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, err
	}
	user := types.User{
		ID:           "usr_" + uuid.NewString(),
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		CreatedAt:    nowValue,
		UpdatedAt:    nowValue,
	}
	memberships := make([]types.TeamMembership, 0, len(invitationValue.Teams))
	for _, team := range invitationValue.Teams {
		memberships = append(memberships, types.TeamMembership{TeamID: team.ID, UserID: user.ID, CreatedAt: nowValue})
	}
	session := types.UserSession{
		ID:         "sess_" + uuid.NewString(),
		UserID:     user.ID,
		SecretHash: sessionSecretHash,
		CreatedAt:  nowValue,
		ExpiresAt:  isoMillis(sessionExpiresAt),
	}
	consumedAt := nowValue
	invitation := &m.SignupInvitations[invitationIndex].Invitation
	invitation.ConsumedAt = &consumedAt
	invitation.ConsumedByUserID = &user.ID
	invitation.Teams = invitationValue.Teams
	m.Users = append(m.Users, user)
	m.Sessions = append(m.Sessions, session)
	m.TeamMemberships = append(m.TeamMemberships, memberships...)
	return user, session, *invitation, nil
}

func (m *MemoryRepository) hydrateSignupInvitation(entry memorySignupInvitation) (types.SignupInvitation, error) {
	invitation := entry.Invitation
	invitation.Teams = make([]types.Team, 0, len(entry.TeamIDs))
	for _, teamID := range entry.TeamIDs {
		found := false
		for _, team := range m.Teams {
			if team.ID == teamID {
				invitation.Teams = append(invitation.Teams, team)
				found = true
				break
			}
		}
		if !found {
			return types.SignupInvitation{}, types.ErrTeamNotFound
		}
	}
	sort.SliceStable(invitation.Teams, func(i, j int) bool {
		if !strings.EqualFold(invitation.Teams[i].Name, invitation.Teams[j].Name) {
			return strings.ToLower(invitation.Teams[i].Name) < strings.ToLower(invitation.Teams[j].Name)
		}
		return invitation.Teams[i].ID < invitation.Teams[j].ID
	})
	return invitation, nil
}
func (m *MemoryRepository) ListUsers(ctx context.Context) ([]types.User, error) {
	page, err := m.ListUsersPage(ctx, types.PageRequest{})
	return page.Users, err
}

func (m *MemoryRepository) ListUsersPage(_ context.Context, pageRequest types.PageRequest) (types.UserPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	users := append([]types.User(nil), m.Users...)
	sort.SliceStable(users, func(i, j int) bool {
		if users[i].IsOwner != users[j].IsOwner {
			return users[i].IsOwner
		}
		if users[i].CreatedAt != users[j].CreatedAt {
			return users[i].CreatedAt < users[j].CreatedAt
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
	return types.UserPage{Users: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) GetUserByID(_ context.Context, userID string) (*types.User, error) {
	for _, user := range m.Users {
		if user.ID == strings.TrimSpace(userID) {
			copyUser := user
			return &copyUser, nil
		}
	}
	return nil, nil
}

func (m *MemoryRepository) SetUserDisabled(_ context.Context, userID string, disabled bool) (types.User, error) {
	m.purgeMutex.Lock()
	defer m.purgeMutex.Unlock()
	now := isoMillis(time.Now().UTC())
	for index := range m.Users {
		user := &m.Users[index]
		if user.ID != userID {
			continue
		}
		if disabled && user.IsOwner {
			return types.User{}, types.ErrOwnerCannotBeDisabled
		}
		if disabled {
			if user.DisabledAt == nil {
				user.DisabledAt = &now
			}
			for sessionIndex := range m.Sessions {
				if m.Sessions[sessionIndex].UserID == user.ID && m.Sessions[sessionIndex].RevokedAt == nil {
					m.Sessions[sessionIndex].RevokedAt = &now
				}
			}
			for keyIndex := range m.APIKeys {
				if m.APIKeys[keyIndex].UserID == user.ID && m.APIKeys[keyIndex].RevokedAt == nil {
					m.APIKeys[keyIndex].RevokedAt = &now
					m.APIKeys[keyIndex].UpdatedAt = now
				}
			}
			for codeIndex := range m.CLICodes {
				if m.CLICodes[codeIndex].UserID == user.ID && m.CLICodes[codeIndex].ConsumedAt == nil {
					m.CLICodes[codeIndex].ConsumedAt = &now
				}
			}
			memberships := m.TeamMemberships[:0]
			for _, membership := range m.TeamMemberships {
				if membership.UserID != user.ID {
					memberships = append(memberships, membership)
				}
			}
			m.TeamMemberships = memberships
		} else {
			user.DisabledAt = nil
		}
		user.UpdatedAt = now
		return *user, nil
	}
	return types.User{}, types.ErrUserNotFound
}

func (m *MemoryRepository) CreateUser(_ context.Context, email string, displayName string, passwordHash *string) (types.User, error) {
	now := isoMillis(time.Now())
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	user := types.User{
		ID:           "usr_" + uuid.NewString(),
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.Users = append(m.Users, user)
	return user, nil
}

func (m *MemoryRepository) FindUserByEmail(_ context.Context, email string) (*types.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, user := range m.Users {
		if user.DisabledAt != nil || strings.ToLower(strings.TrimSpace(user.Email)) != email {
			continue
		}
		copy := user
		return &copy, nil
	}
	return nil, nil
}

func (m *MemoryRepository) CreateUserSession(_ context.Context, session types.UserSession) (types.UserSession, error) {
	now := isoMillis(time.Now())
	if session.ID == "" {
		session.ID = "sess_" + uuid.NewString()
	}
	session.CreatedAt = now
	if session.ExpiresAt == "" {
		session.ExpiresAt = isoMillis(time.Now().Add(30 * 24 * time.Hour))
	}
	m.Sessions = append(m.Sessions, session)
	return session, nil
}

func (m *MemoryRepository) FindUserSessionBySecretHash(_ context.Context, secretHash string) (*types.UserSession, *types.User, error) {
	now := time.Now().UTC()
	for _, session := range m.Sessions {
		if session.SecretHash != secretHash || session.RevokedAt != nil {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339, session.ExpiresAt)
		if err == nil && now.After(expiresAt) {
			continue
		}
		for _, user := range m.Users {
			if user.ID == session.UserID && user.DisabledAt == nil {
				sessionCopy := session
				userCopy := user
				return &sessionCopy, &userCopy, nil
			}
		}
	}
	return nil, nil, nil
}

func (m *MemoryRepository) MarkUserSessionUsed(_ context.Context, sessionID string) error {
	now := isoMillis(time.Now())
	for i := range m.Sessions {
		if m.Sessions[i].ID == sessionID && m.Sessions[i].RevokedAt == nil {
			m.Sessions[i].LastUsedAt = &now
			return nil
		}
	}
	return nil
}

func (m *MemoryRepository) RevokeUserSession(_ context.Context, sessionID string) error {
	now := isoMillis(time.Now())
	for i := range m.Sessions {
		if m.Sessions[i].ID == sessionID && m.Sessions[i].RevokedAt == nil {
			m.Sessions[i].RevokedAt = &now
			return nil
		}
	}
	return nil
}

func (m *MemoryRepository) CreateCLILoginCode(_ context.Context, code types.CLILoginCode) (types.CLILoginCode, error) {
	now := isoMillis(time.Now())
	if code.ID == "" {
		code.ID = "clicode_" + uuid.NewString()
	}
	code.CreatedAt = now
	if code.ExpiresAt == "" {
		code.ExpiresAt = isoMillis(time.Now().Add(5 * time.Minute))
	}
	m.CLICodes = append(m.CLICodes, code)
	return code, nil
}

func (m *MemoryRepository) ConsumeCLILoginCode(_ context.Context, codeHash string, stateHash string, redirectURI string) (*types.CLILoginCode, *types.User, error) {
	now := time.Now().UTC()
	consumedAt := isoMillis(now)
	for i := range m.CLICodes {
		code := m.CLICodes[i]
		if code.CodeHash != codeHash || code.StateHash != stateHash || code.RedirectURI != redirectURI || code.ConsumedAt != nil {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339, code.ExpiresAt)
		if err != nil || !expiresAt.After(now) {
			return nil, nil, nil
		}
		for _, user := range m.Users {
			if user.ID == code.UserID && user.DisabledAt == nil {
				m.CLICodes[i].ConsumedAt = &consumedAt
				code.ConsumedAt = &consumedAt
				userCopy := user
				return &code, &userCopy, nil
			}
		}
		return nil, nil, nil
	}
	return nil, nil, nil
}
