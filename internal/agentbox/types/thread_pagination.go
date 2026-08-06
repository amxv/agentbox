package types

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidThreadPageCursor = errors.New("invalid thread continuation cursor")

type ThreadPageCursor struct {
	UpdatedAt time.Time
	ID        string
}

type ThreadPageInfo struct {
	Limit      int     `json:"limit"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor,omitempty"`
}

type ThreadPage struct {
	Threads []Thread       `json:"threads"`
	Page    ThreadPageInfo `json:"page"`
}

type SearchThreadPage struct {
	Threads []SearchThreadResult `json:"threads"`
	Page    ThreadPageInfo       `json:"page"`
}

type threadPageCursorPayload struct {
	Version   int    `json:"v"`
	UpdatedAt string `json:"updated_at"`
	ID        string `json:"id"`
}

func EncodeThreadPageCursor(cursor ThreadPageCursor) (string, error) {
	if cursor.UpdatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return "", ErrInvalidThreadPageCursor
	}
	payload, err := json.Marshal(threadPageCursorPayload{
		Version:   1,
		UpdatedAt: cursor.UpdatedAt.UTC().Format(time.RFC3339Nano),
		ID:        strings.TrimSpace(cursor.ID),
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeThreadPageCursor(value string) (*ThreadPageCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, ErrInvalidThreadPageCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	decoder.DisallowUnknownFields()
	var payload threadPageCursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, ErrInvalidThreadPageCursor
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidThreadPageCursor
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, payload.UpdatedAt)
	if err != nil || payload.Version != 1 || strings.TrimSpace(payload.ID) == "" {
		return nil, ErrInvalidThreadPageCursor
	}
	return &ThreadPageCursor{UpdatedAt: updatedAt.UTC(), ID: strings.TrimSpace(payload.ID)}, nil
}
