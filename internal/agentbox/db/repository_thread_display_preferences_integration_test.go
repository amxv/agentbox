package db

import (
	"errors"
	"testing"

	"agentbox/internal/agentbox/types"
)

func TestThreadDisplayPreferencesInPostgresRespectNormalReaderAccess(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := repository.CreateUser(ctx, "reader@example.com", "Reader", nil)
	if err != nil {
		t.Fatal(err)
	}

	ownerAuth := types.AuthContext{UserID: owner.ID, SubjectType: types.AuthSubjectUserSession, ActorName: "Owner"}
	thread, err := repository.CreateThread(ctx, owner.ID, "Reader display preferences", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	preference, err := repository.GetThreadDisplayPreference(ctx, owner.ID, thread.ID)
	if err != nil || preference.AllPlainAsMarkdown || len(preference.MessagePreferences) != 0 {
		t.Fatalf("initial owner preference=%#v err=%v", preference, err)
	}

	original := types.ThreadDisplayPreference{AllPlainAsMarkdown: true, MessagePreferences: map[string]bool{"msg_1": false, "msg_2": true}}
	if err := repository.SaveThreadDisplayPreference(ctx, owner.ID, thread.ID, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetThreadDisplayPreference(ctx, owner.ID, thread.ID)
	if err != nil || !loaded.AllPlainAsMarkdown || loaded.MessagePreferences["msg_1"] || !loaded.MessagePreferences["msg_2"] {
		t.Fatalf("owner preference failed roundtrip: %#v err=%v", loaded, err)
	}
	if _, err := repository.GetThreadDisplayPreference(ctx, reader.ID, thread.ID); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("private reader access error=%v", err)
	}
	if err := repository.SaveThreadDisplayPreference(ctx, reader.ID, thread.ID, original); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("private reader write error=%v", err)
	}

	team, err := repository.CreateTeam(ctx, "reader-team", "Reader team", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AddTeamMember(ctx, team.ID, reader.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ManageThreadVisibility(ctx, owner.ID, thread.ID, types.ManageThreadVisibilityInput{AddTeams: []string{team.ID}}); err != nil {
		t.Fatal(err)
	}

	second, err := repository.GetThreadDisplayPreference(ctx, reader.ID, thread.ID)
	if err != nil || second.AllPlainAsMarkdown || len(second.MessagePreferences) > 0 {
		t.Fatalf("second reader inherited owner's preference: %#v err=%v", second, err)
	}
	if err := repository.SaveThreadDisplayPreference(ctx, reader.ID, thread.ID, types.ThreadDisplayPreference{MessagePreferences: map[string]bool{"msg_1": true}}); err != nil {
		t.Fatal(err)
	}
	loaded, err = repository.GetThreadDisplayPreference(ctx, owner.ID, thread.ID)
	if err != nil || !loaded.AllPlainAsMarkdown || loaded.MessagePreferences["msg_1"] {
		t.Fatalf("second reader modified owner preference: %#v err=%v", loaded, err)
	}
	updatedAt, err := repository.GetThreadActivity(ctx, reader.ID, thread.ID)
	if err != nil || updatedAt == "" {
		t.Fatalf("cheap thread activity query failed: %q err=%v", updatedAt, err)
	}

	if _, err := repository.RemoveTeamMember(ctx, team.ID, reader.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetThreadDisplayPreference(ctx, reader.ID, thread.ID); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("revoked preference read error=%v", err)
	}
	if err := repository.SaveThreadDisplayPreference(ctx, reader.ID, thread.ID, original); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("revoked preference write error=%v", err)
	}
	if _, err := repository.GetThreadActivity(ctx, reader.ID, thread.ID); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("revoked activity probe error=%v", err)
	}
}
