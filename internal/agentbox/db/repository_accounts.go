package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"agentbox/internal/agentbox/identity"
	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

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
`, identity.OwnerIDForEmail(email), email, displayName, passwordHash))
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
