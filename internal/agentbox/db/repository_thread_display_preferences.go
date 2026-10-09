package db

import (
	"context"
	"encoding/json"
	"errors"

	"agentbox/internal/agentbox/types"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) GetThreadDisplayPreference(ctx context.Context, userID string, threadID string) (types.ThreadDisplayPreference, error) {
	preference := types.ThreadDisplayPreference{MessagePreferences: map[string]bool{}}
	var raw []byte
	err := r.pool.QueryRow(ctx, `
select coalesce(p.all_plain_as_markdown, false), coalesce(p.message_preferences, '{}'::jsonb)
from threads t
left join thread_display_preferences p on p.thread_id = t.id and p.user_id = $1
where `+normalThreadAccessPredicate+` and t.id = $2
`, userID, threadID).Scan(&preference.AllPlainAsMarkdown, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return types.ThreadDisplayPreference{}, types.ErrThreadNotFound
	}
	if err != nil {
		return types.ThreadDisplayPreference{}, err
	}
	if err := json.Unmarshal(raw, &preference.MessagePreferences); err != nil {
		return types.ThreadDisplayPreference{}, err
	}
	return preference, nil
}

func (r *Repository) SaveThreadDisplayPreference(ctx context.Context, userID string, threadID string, preference types.ThreadDisplayPreference) error {
	payload, err := json.Marshal(preference.MessagePreferences)
	if err != nil {
		return err
	}
	result, err := r.pool.Exec(ctx, `
insert into thread_display_preferences (user_id, thread_id, all_plain_as_markdown, message_preferences)
select $1, t.id, $3, $4::jsonb
from threads t
where `+normalThreadAccessPredicate+` and t.id = $2
on conflict (user_id, thread_id) do update
set all_plain_as_markdown = excluded.all_plain_as_markdown,
    message_preferences = excluded.message_preferences,
    updated_at = now()
`, userID, threadID, preference.AllPlainAsMarkdown, string(payload))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return types.ErrThreadNotFound
	}
	return nil
}
