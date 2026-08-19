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
