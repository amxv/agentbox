package types

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestThreadPageCursorRoundTripsAndRejectsMalformedPayloads(t *testing.T) {
	want := ThreadPageCursor{UpdatedAt: time.Date(2026, 8, 3, 12, 34, 56, 123456789, time.UTC), ID: "thr_cursor"}
	encoded, err := EncodeThreadPageCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "" || encoded == want.ID {
		t.Fatalf("encoded cursor = %q", encoded)
	}
	got, err := DecodeThreadPageCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.UpdatedAt.Equal(want.UpdatedAt) || got.ID != want.ID {
		t.Fatalf("decoded cursor = %#v, want %#v", got, want)
	}
	if empty, err := DecodeThreadPageCursor("  "); err != nil || empty != nil {
		t.Fatalf("empty cursor = %#v, err=%v", empty, err)
	}

	invalidPayloads := []string{
		"not-base64!",
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":2,"updated_at":"2026-08-03T12:34:56Z","id":"thr_cursor"}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"updated_at":"not-a-time","id":"thr_cursor"}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"updated_at":"2026-08-03T12:34:56Z","id":""}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"updated_at":"2026-08-03T12:34:56Z","id":"thr_cursor","extra":true}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"updated_at":"2026-08-03T12:34:56Z","id":"thr_cursor"}{}`)),
	}
	for _, value := range invalidPayloads {
		if cursor, err := DecodeThreadPageCursor(value); err == nil || cursor != nil {
			t.Fatalf("malformed cursor %q decoded as %#v, err=%v", value, cursor, err)
		}
	}
	if _, err := EncodeThreadPageCursor(ThreadPageCursor{}); err == nil {
		t.Fatal("zero cursor encoded successfully")
	}
}
