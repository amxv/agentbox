package db

import (
	"context"
	"errors"
	"time"

	"agentbox/internal/agentbox/types"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) GetThreadActivity(ctx context.Context, userID string, threadID string) (string, error) {
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx, `
select t.updated_at from threads t
where `+normalThreadAccessPredicate+` and t.id = $2
`, userID, threadID).Scan(&updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", types.ErrThreadNotFound
	}
	if err != nil {
		return "", err
	}
	return isoMillis(updatedAt), nil
}
