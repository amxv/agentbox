package service

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"agentbox/internal/agentbox/assets"
	authpkg "agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/types"
)

func TestSessionAndCredentialResolveSameUserWithDistinctActors(t *testing.T) {
	passwordHash, err := authpkg.HashPassword("secret-password")
	if err != nil {
		t.Fatal(err)
	}
	repo := &db.MemoryRepository{
		Users: []types.User{{
			ID:           "usr_owner",
			Email:        "owner@example.com",
			DisplayName:  "Owner Person",
			PasswordHash: &passwordHash,
			IsOwner:      true,
		}},
	}
	svc := New(repo, &assets.FakeStore{})

	sessionAuth, sessionSecret, err := svc.Login(context.Background(), "ten_wrong", "owner@example.com", "secret-password")
	if err != nil {
		t.Fatal(err)
	}
	if sessionAuth.UserID != "usr_owner" || sessionAuth.UserDisplayName != "Owner Person" || !sessionAuth.IsOwner || sessionAuth.ActorID == "" || sessionAuth.ActorID != sessionAuth.SessionID {
		t.Fatalf("unexpected browser auth context: %#v", sessionAuth)
	}
	credential, err := svc.CreateAPIKeyWithPurposeAndScopes(context.Background(), sessionAuth, "ChatGPT", "chatgpt", []string{"threads:read", "threads:write"})
	if err != nil {
		t.Fatal(err)
	}
	if credential.UserID != sessionAuth.UserID || credential.Purpose != "chatgpt" {
		t.Fatalf("unexpected credential: %#v", credential)
	}
	credentialAuth, err := svc.AuthenticateAPIKey(context.Background(), credential.Key)
	if err != nil {
		t.Fatal(err)
	}
	if credentialAuth == nil || credentialAuth.UserID != sessionAuth.UserID || credentialAuth.UserDisplayName != sessionAuth.UserDisplayName {
		t.Fatalf("credential did not resolve the browser user: session=%#v key=%#v", sessionAuth, credentialAuth)
	}
	if credentialAuth.ActorID != credential.ID || credentialAuth.KeyID != credential.ID || credentialAuth.ActorName != "ChatGPT" || credentialAuth.IsOwner {
		t.Fatalf("credential actor or owner authority is wrong: %#v", credentialAuth)
	}
	if credentialAuth.ActorID == sessionAuth.ActorID {
		t.Fatalf("browser and credential actors collapsed: session=%#v key=%#v", sessionAuth, credentialAuth)
	}

	thread, err := svc.CreateThread(context.Background(), sessionAuth, "Shared user identity")
	if err != nil {
		t.Fatal(err)
	}
	if thread.OwnerUserID != sessionAuth.UserID || thread.CreatedByUserDisplayName == nil || *thread.CreatedByUserDisplayName != "Owner Person" || thread.CreatedByActorName == nil || *thread.CreatedByActorName != "Web dashboard" {
		t.Fatalf("unexpected thread ownership or snapshots: %#v", thread)
	}
	message, err := svc.PostMessage(context.Background(), *credentialAuth, PostMessageParams{ThreadID: thread.ID, Body: "from connector"})
	if err != nil {
		t.Fatal(err)
	}
	if message.CreatedByUserID == nil || *message.CreatedByUserID != sessionAuth.UserID || message.CreatedByKeyID == nil || *message.CreatedByKeyID != credential.ID || message.CreatedByUserDisplayName == nil || *message.CreatedByUserDisplayName != "Owner Person" || message.CreatedByActorName == nil || *message.CreatedByActorName != "ChatGPT" {
		t.Fatalf("unexpected connector attribution: %#v", message)
	}
	if _, err := svc.PostMessage(context.Background(), sessionAuth, PostMessageParams{ThreadID: thread.ID, Body: "from browser"}); err != nil {
		t.Fatal(err)
	}
	for _, actorName := range []string{"Claude", "Local CLI"} {
		actorCredential, err := svc.CreateAPIKeyWithPurposeAndScopes(context.Background(), sessionAuth, actorName, strings.ToLower(strings.ReplaceAll(actorName, " ", "-")), []string{"threads:read", "threads:write"})
		if err != nil {
			t.Fatal(err)
		}
		actorAuth, err := svc.AuthenticateAPIKey(context.Background(), actorCredential.Key)
		if err != nil || actorAuth == nil {
			t.Fatalf("authenticate %s: auth=%#v err=%v", actorName, actorAuth, err)
		}
		if _, err := svc.PostMessage(context.Background(), *actorAuth, PostMessageParams{ThreadID: thread.ID, Body: "from " + actorName}); err != nil {
			t.Fatal(err)
		}
	}
	shared, err := svc.GetThread(context.Background(), sessionAuth, thread.ID)
	if err != nil || len(shared.Messages) != 4 || shared.Messages[0].ID != message.ID {
		t.Fatalf("same user actors did not share thread access: thread=%#v err=%v", shared, err)
	}
	actorSnapshots := make([]string, 0, len(shared.Messages))
	for _, sharedMessage := range shared.Messages {
		if sharedMessage.CreatedByUserDisplayName == nil || *sharedMessage.CreatedByUserDisplayName != "Owner Person" || sharedMessage.CreatedByActorName == nil {
			t.Fatalf("missing attribution snapshot: %#v", sharedMessage)
		}
		actorSnapshots = append(actorSnapshots, *sharedMessage.CreatedByActorName)
	}
	sort.Strings(actorSnapshots)
	if !reflect.DeepEqual(actorSnapshots, []string{"ChatGPT", "Claude", "Local CLI", "Web dashboard"}) {
		t.Fatalf("actor snapshots=%v", actorSnapshots)
	}

	disabledAt := "2026-08-01T00:00:00.000Z"
	repo.Users[0].DisabledAt = &disabledAt
	if authenticated, err := svc.AuthenticateSession(context.Background(), sessionSecret); err != nil || authenticated != nil {
		t.Fatalf("disabled user browser session authenticated: auth=%#v err=%v", authenticated, err)
	}
	if authenticated, err := svc.AuthenticateAPIKey(context.Background(), credential.Key); err != nil || authenticated != nil {
		t.Fatalf("disabled user credential authenticated: auth=%#v err=%v", authenticated, err)
	}
}

func TestOwnerSetupTokensBootstrapRecoverRevokeAndRejectReplay(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})

	first, err := svc.IssueOwnerSetupToken(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.Purpose != "bootstrap" || !strings.HasPrefix(first.Token, "agos_") {
		t.Fatalf("unexpected first setup token: %#v", first)
	}
	second, err := svc.IssueOwnerSetupToken(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.Purpose != "bootstrap" || second.Token == first.Token {
		t.Fatalf("unexpected replacement token: first=%#v second=%#v", first, second)
	}
	if _, _, _, err := svc.CompleteOwnerSetup(context.Background(), first.Token, "owner@example.com", "Owner", "first-password"); !hasCodedError(err, "INVALID_OWNER_SETUP_TOKEN") {
		t.Fatalf("revoked token error = %v", err)
	}

	authContext, sessionSecret, owner, err := svc.CompleteOwnerSetup(context.Background(), second.Token, "owner@example.com", "Owner", "first-password")
	if err != nil {
		t.Fatal(err)
	}
	if owner.ID == "" || !owner.IsOwner || authContext.UserID != owner.ID || !authContext.IsOwner || authContext.SubjectType != types.AuthSubjectUserSession || sessionSecret == "" {
		t.Fatalf("unexpected owner completion: auth=%#v owner=%#v secret=%q", authContext, owner, sessionSecret)
	}
	if _, _, _, err := svc.CompleteOwnerSetup(context.Background(), second.Token, "owner@example.com", "Owner", "first-password"); !hasCodedError(err, "INVALID_OWNER_SETUP_TOKEN") {
		t.Fatalf("replayed token error = %v", err)
	}

	recovery, err := svc.IssueOwnerSetupToken(context.Background(), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Purpose != "recovery" {
		t.Fatalf("recovery purpose = %q", recovery.Purpose)
	}
	if _, _, _, err := svc.CompleteOwnerSetup(context.Background(), recovery.Token, "other@example.com", "Wrong", "second-password"); !hasCodedError(err, "OWNER_EMAIL_MISMATCH") {
		t.Fatalf("wrong-email recovery error = %v", err)
	}
	recoveredAuth, _, recoveredOwner, err := svc.CompleteOwnerSetup(context.Background(), recovery.Token, "OWNER@example.com", "Recovered Owner", "second-password")
	if err != nil {
		t.Fatal(err)
	}
	if recoveredOwner.ID != owner.ID || recoveredOwner.DisplayName != "Recovered Owner" || recoveredAuth.UserID != owner.ID {
		t.Fatalf("recovery changed owner identity: before=%#v after=%#v", owner, recoveredOwner)
	}
	if _, _, err := svc.Login(context.Background(), "ignored", "owner@example.com", "second-password"); err != nil {
		t.Fatalf("recovered password did not authenticate: %v", err)
	}
	if _, _, err := svc.Login(context.Background(), "ignored", "owner@example.com", "first-password"); !errors.Is(err, ErrInvalidLogin) {
		t.Fatalf("old password still authenticated: %v", err)
	}
}

func TestOwnerSetupTokenTTLIsBounded(t *testing.T) {
	svc := New(&db.MemoryRepository{}, &assets.FakeStore{})
	if _, err := svc.IssueOwnerSetupToken(context.Background(), 25*time.Hour); !hasCodedError(err, "INVALID_ARGUMENT") {
		t.Fatalf("oversized TTL error = %v", err)
	}
}

func TestInvitationRegistrationAndOwnerUserLifecycle(t *testing.T) {
	ownerPasswordHash, err := authpkg.HashPassword("owner-password")
	if err != nil {
		t.Fatal(err)
	}
	repo := &db.MemoryRepository{
		Users: []types.User{{
			ID:           "usr_owner",
			Email:        "owner@example.com",
			DisplayName:  "Owner",
			PasswordHash: &ownerPasswordHash,
			IsOwner:      true,
		}},
	}
	svc := New(repo, &assets.FakeStore{})
	ownerAuth, _, err := svc.Login(context.Background(), "ignored", "owner@example.com", "owner-password")
	if err != nil {
		t.Fatal(err)
	}

	engineering, err := svc.CreateTeam(context.Background(), ownerAuth, "engineering", "Engineering")
	if err != nil {
		t.Fatal(err)
	}
	operations, err := svc.CreateTeam(context.Background(), ownerAuth, "operations", "Operations")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTeam(context.Background(), ownerAuth, "Engineering", "Duplicate"); !hasCodedError(err, "TEAM_SLUG_CONFLICT") {
		t.Fatalf("duplicate team slug error=%v", err)
	}
	if _, err := svc.CreateSignupInvitation(context.Background(), ownerAuth, time.Hour, "team_missing"); !hasCodedError(err, "TEAM_NOT_FOUND") {
		t.Fatalf("missing invitation team error=%v", err)
	}

	invitationResult, err := svc.CreateSignupInvitation(context.Background(), ownerAuth, 2*time.Hour, engineering.ID, operations.ID, engineering.ID)
	if err != nil {
		t.Fatal(err)
	}
	if invitationResult.Invitation.CreatedByUserID != ownerAuth.UserID || !strings.HasPrefix(invitationResult.Token, "aginv_") || len(invitationResult.Invitation.Teams) != 2 {
		t.Fatalf("unexpected invitation: %#v", invitationResult)
	}
	inspection, err := svc.InspectSignupInvitation(context.Background(), invitationResult.Token)
	if err != nil || !inspection.Valid || inspection.ExpiresAt == "" {
		t.Fatalf("inspection=%#v err=%v", inspection, err)
	}

	memberAuth, memberSessionSecret, member, err := svc.RegisterWithSignupInvitation(context.Background(), invitationResult.Token, "member@example.com", "Member", "member-password")
	if err != nil {
		t.Fatal(err)
	}
	if member.ID == "" || member.IsOwner || memberAuth.UserID != member.ID || memberSessionSecret == "" {
		t.Fatalf("unexpected registration: auth=%#v user=%#v", memberAuth, member)
	}
	memberTeams, err := svc.ListMyTeams(context.Background(), memberAuth)
	if err != nil || len(memberTeams) != 2 || memberTeams[0].ID != engineering.ID || memberTeams[1].ID != operations.ID {
		t.Fatalf("invitation memberships=%#v err=%v", memberTeams, err)
	}
	ownerTeams, err := svc.ListOwnerTeams(context.Background(), ownerAuth)
	if err != nil || len(ownerTeams) != 2 || len(ownerTeams[0].Members) != 1 || ownerTeams[0].Members[0].ID != member.ID {
		t.Fatalf("owner team view=%#v err=%v", ownerTeams, err)
	}
	if _, err := svc.AddTeamMember(context.Background(), ownerAuth, engineering.ID, ownerAuth.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTeamMember(context.Background(), ownerAuth, engineering.ID, ownerAuth.UserID); err != nil {
		t.Fatalf("duplicate membership was not idempotent: %v", err)
	}
	renamed, err := svc.RenameTeam(context.Background(), ownerAuth, engineering.ID, "Product Engineering")
	if err != nil || renamed.Slug != "engineering" || renamed.Name != "Product Engineering" {
		t.Fatalf("renamed team=%#v err=%v", renamed, err)
	}

	ownerThread, err := svc.CreateThread(context.Background(), ownerAuth, "still private")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetThread(context.Background(), memberAuth, ownerThread.ID); !hasCodedError(err, "THREAD_NOT_FOUND") {
		t.Fatalf("team membership changed private thread access: %v", err)
	}
	if _, err := svc.InspectSignupInvitation(context.Background(), invitationResult.Token); !hasCodedError(err, "INVALID_INVITATION") {
		t.Fatalf("consumed invitation inspection error=%v", err)
	}
	if _, _, _, err := svc.RegisterWithSignupInvitation(context.Background(), invitationResult.Token, "other@example.com", "Other", "password"); !hasCodedError(err, "INVALID_INVITATION") {
		t.Fatalf("invitation replay error=%v", err)
	}

	duplicate, err := svc.CreateSignupInvitation(context.Background(), ownerAuth, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := svc.RegisterWithSignupInvitation(context.Background(), duplicate.Token, "OWNER@example.com", "Duplicate", "password"); !hasCodedError(err, "REGISTRATION_UNAVAILABLE") {
		t.Fatalf("duplicate registration error=%v", err)
	}
	if inspection, err := svc.InspectSignupInvitation(context.Background(), duplicate.Token); err != nil || !inspection.Valid {
		t.Fatalf("duplicate registration consumed invitation: inspection=%#v err=%v", inspection, err)
	}
	if err := svc.RevokeSignupInvitation(context.Background(), ownerAuth, duplicate.Invitation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InspectSignupInvitation(context.Background(), duplicate.Token); !hasCodedError(err, "INVALID_INVITATION") {
		t.Fatalf("revoked invitation inspection error=%v", err)
	}

	memberKey, err := svc.CreateAPIKey(context.Background(), memberAuth, "local")
	if err != nil {
		t.Fatal(err)
	}
	memberKeyAuth, err := svc.AuthenticateAPIKey(context.Background(), memberKey.Key)
	if err != nil || memberKeyAuth == nil {
		t.Fatalf("member key auth=%#v err=%v", memberKeyAuth, err)
	}
	if teams, err := svc.ListMyTeams(context.Background(), *memberKeyAuth); err != nil || len(teams) != 2 {
		t.Fatalf("member credential team list=%#v err=%v", teams, err)
	}
	if _, err := svc.CreateSignupInvitation(context.Background(), memberAuth, time.Hour); !hasCodedError(err, "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("member created invitation: %v", err)
	}
	if _, err := svc.CreateTeam(context.Background(), memberAuth, "blocked", "Blocked"); !hasCodedError(err, "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("member created team: %v", err)
	}
	ownerKey, err := svc.CreateAPIKey(context.Background(), ownerAuth, "owner-api")
	if err != nil {
		t.Fatal(err)
	}
	ownerKeyAuth, err := svc.AuthenticateAPIKey(context.Background(), ownerKey.Key)
	if err != nil || ownerKeyAuth == nil {
		t.Fatalf("owner key auth=%#v err=%v", ownerKeyAuth, err)
	}
	if _, err := svc.ListUsers(context.Background(), *ownerKeyAuth); !hasCodedError(err, "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("owner API key listed users: %v", err)
	}
	if _, err := svc.ListOwnerTeams(context.Background(), *ownerKeyAuth); !hasCodedError(err, "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("owner API key listed owner team view: %v", err)
	}
	if teams, err := svc.ListMyTeams(context.Background(), *ownerKeyAuth); err != nil || len(teams) != 1 || teams[0].ID != engineering.ID {
		t.Fatalf("owner credential own-team list=%#v err=%v", teams, err)
	}

	if err := svc.RemoveTeamMember(context.Background(), ownerAuth, operations.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveTeamMember(context.Background(), ownerAuth, operations.ID, member.ID); err != nil {
		t.Fatalf("duplicate membership removal was not idempotent: %v", err)
	}
	if teams, err := svc.ListMyTeams(context.Background(), memberAuth); err != nil || len(teams) != 1 || teams[0].ID != engineering.ID {
		t.Fatalf("membership removal result=%#v err=%v", teams, err)
	}

	zeroTeamInvitation, err := svc.CreateSignupInvitation(context.Background(), ownerAuth, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	zeroAuth, _, zeroUser, err := svc.RegisterWithSignupInvitation(context.Background(), zeroTeamInvitation.Token, "zero@example.com", "Zero Team", "password")
	if err != nil {
		t.Fatal(err)
	}
	if teams, err := svc.ListMyTeams(context.Background(), zeroAuth); err != nil || len(teams) != 0 {
		t.Fatalf("zero-team user %s has teams=%#v err=%v", zeroUser.ID, teams, err)
	}

	disabledMember, err := svc.SetUserDisabled(context.Background(), ownerAuth, member.ID, true)
	if err != nil || disabledMember.DisabledAt == nil {
		t.Fatalf("disable member=%#v err=%v", disabledMember, err)
	}
	if authenticated, err := svc.AuthenticateSession(context.Background(), memberSessionSecret); err != nil || authenticated != nil {
		t.Fatalf("disabled member session auth=%#v err=%v", authenticated, err)
	}
	if authenticated, err := svc.AuthenticateAPIKey(context.Background(), memberKey.Key); err != nil || authenticated != nil {
		t.Fatalf("disabled member key auth=%#v err=%v", authenticated, err)
	}
	if _, err := svc.SetUserDisabled(context.Background(), ownerAuth, ownerAuth.UserID, true); !hasCodedError(err, "OWNER_IMMUTABLE") {
		t.Fatalf("owner disable error=%v", err)
	}
	enabledMember, err := svc.SetUserDisabled(context.Background(), ownerAuth, member.ID, false)
	if err != nil || enabledMember.DisabledAt != nil {
		t.Fatalf("enable member=%#v err=%v", enabledMember, err)
	}
	if _, _, err := svc.Login(context.Background(), "ignored", "member@example.com", "member-password"); err != nil {
		t.Fatalf("enabled member could not log in: %v", err)
	}
}

func TestOwnerCredentialAdministrationAndDisablementPreserveSharedContent(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	owner := types.User{ID: "usr_owner_admin", Email: "owner-admin@example.com", DisplayName: "Owner", IsOwner: true}
	member := types.User{ID: "usr_owner_admin_member", Email: "member-admin@example.com", DisplayName: "Member"}
	teammate := types.User{ID: "usr_owner_admin_teammate", Email: "teammate-admin@example.com", DisplayName: "Teammate"}
	repo.Users = append(repo.Users, owner, member, teammate)
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_owner_admin", ActorName: "Web dashboard", IsOwner: true}
	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_owner_admin_member", ActorName: "Web dashboard"}
	teammateAuth := types.AuthContext{UserID: teammate.ID, UserDisplayName: teammate.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_owner_admin_teammate", ActorName: "Web dashboard"}

	team, err := repo.CreateTeam(context.Background(), "owner-admin-team", "Owner Admin Team")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{member.ID, teammate.ID} {
		if _, err := repo.AddTeamMember(context.Background(), team.ID, userID); err != nil {
			t.Fatal(err)
		}
	}
	privateThread, err := svc.CreateThread(context.Background(), memberAuth, "Disabled owner private")
	if err != nil {
		t.Fatal(err)
	}
	sharedThread, err := svc.CreateThread(context.Background(), memberAuth, "Disabled owner shared")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(context.Background(), repo, member.ID, sharedThread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	if thread, err := svc.GetThread(context.Background(), teammateAuth, sharedThread.ID); err != nil || thread == nil {
		t.Fatalf("shared thread unavailable before disable: thread=%#v err=%v", thread, err)
	}

	active, err := svc.CreateAPIKeyWithPurposeAndScopes(context.Background(), memberAuth, "ChatGPT", "chatgpt", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := svc.CreateAPIKeyWithPurposeAndScopes(context.Background(), memberAuth, "Old CLI", "local", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	if removed, err := repo.RevokeAPIKeyForUserByID(context.Background(), member.ID, revoked.ID); err != nil || !removed {
		t.Fatalf("pre-revoke credential removed=%t err=%v", removed, err)
	}
	credentials, err := svc.ListOwnerAPIKeys(context.Background(), ownerAuth)
	if err != nil || len(credentials) != 2 || credentials[0].TokenHash == "" || credentials[0].Key != "" {
		t.Fatalf("owner credential metadata=%#v err=%v", credentials, err)
	}
	ownerKeyAuth := ownerAuth
	ownerKeyAuth.SubjectType = types.AuthSubjectAPIKey
	ownerKeyAuth.SessionID = ""
	ownerKeyAuth.KeyID = "key_owner_admin"
	if _, err := svc.ListOwnerAPIKeys(context.Background(), ownerKeyAuth); !hasCodedError(err, "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("owner API key listed credentials: %v", err)
	}
	if _, err := svc.ListOwnerAPIKeys(context.Background(), memberAuth); !hasCodedError(err, "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("ordinary browser listed credentials: %v", err)
	}
	if err := svc.RevokeOwnerAPIKey(context.Background(), ownerAuth, active.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeOwnerAPIKey(context.Background(), ownerAuth, active.ID); err != nil {
		t.Fatalf("idempotent owner revoke failed: %v", err)
	}
	if authenticated, err := svc.AuthenticateAPIKey(context.Background(), active.Key); err != nil || authenticated != nil {
		t.Fatalf("owner-revoked credential authenticated: auth=%#v err=%v", authenticated, err)
	}

	disabled, err := svc.SetUserDisabled(context.Background(), ownerAuth, member.ID, true)
	if err != nil || disabled.DisabledAt == nil {
		t.Fatalf("disable user=%#v err=%v", disabled, err)
	}
	if teams, err := repo.ListUserTeams(context.Background(), member.ID); err != nil || len(teams) != 0 {
		t.Fatalf("disabled user memberships=%#v err=%v", teams, err)
	}
	if _, err := svc.AddTeamMember(context.Background(), ownerAuth, team.ID, member.ID); !hasCodedError(err, "USER_DISABLED") {
		t.Fatalf("disabled user was re-added to team: %v", err)
	}
	allCredentials, err := svc.ListOwnerAPIKeys(context.Background(), ownerAuth)
	if err != nil || len(allCredentials) != 2 {
		t.Fatalf("credentials after disable=%#v err=%v", allCredentials, err)
	}
	for _, credential := range allCredentials {
		if credential.RevokedAt == nil {
			t.Fatalf("credential remained active after disable: %#v", credential)
		}
	}
	if thread, err := svc.GetThread(context.Background(), teammateAuth, sharedThread.ID); err != nil || thread == nil {
		t.Fatalf("qualified teammate lost disabled owner's shared thread: thread=%#v err=%v", thread, err)
	}
	if _, err := svc.GetThread(context.Background(), teammateAuth, privateThread.ID); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("disabled owner's private thread leaked: %v", err)
	}
	if _, err := svc.SetUserDisabled(context.Background(), ownerAuth, member.ID, false); err != nil {
		t.Fatal(err)
	}
	if teams, err := repo.ListUserTeams(context.Background(), member.ID); err != nil || len(teams) != 0 {
		t.Fatalf("enable restored memberships=%#v err=%v", teams, err)
	}
	if authenticated, err := svc.AuthenticateAPIKey(context.Background(), active.Key); err != nil || authenticated != nil {
		t.Fatalf("enable restored revoked credential: auth=%#v err=%v", authenticated, err)
	}
}

func TestServiceUserPrivateIsolationAndAPIKeys(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	userA := testAuth("global", "shared")
	userA.UserID = "usr_a"
	userA.UserDisplayName = "User A"
	userB := testAuth("global", "shared")
	userB.UserID = "usr_b"
	userB.UserDisplayName = "User B"
	repo.Users = append(repo.Users,
		types.User{ID: userA.UserID, Email: "a@example.com", DisplayName: "User A"},
		types.User{ID: userB.UserID, Email: "b@example.com", DisplayName: "User B"},
	)

	keyA, err := svc.CreateAPIKey(context.Background(), userA, "shared")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := svc.CreateAPIKey(context.Background(), userB, "shared")
	if err != nil {
		t.Fatal(err)
	}
	if keyA.Key == "" || keyB.Key == "" || keyA.Key == keyB.Key {
		t.Fatalf("keys not unique: %#v %#v", keyA, keyB)
	}
	if keyA.TokenHash == "" || keyA.TokenPrefix == "" || keyA.KeyMasked == "" {
		t.Fatalf("key metadata missing: %#v", keyA)
	}

	authA, err := svc.AuthenticateAPIKey(context.Background(), keyA.Key)
	if err != nil {
		t.Fatal(err)
	}
	authB, err := svc.AuthenticateAPIKey(context.Background(), keyB.Key)
	if err != nil {
		t.Fatal(err)
	}
	if authA == nil || authA.UserID != userA.UserID || authA.ActorName != "shared" || authB == nil || authB.UserID != userB.UserID {
		t.Fatalf("auth contexts: %#v %#v", authA, authB)
	}

	threadA, err := svc.CreateThread(context.Background(), *authA, "User A private")
	if err != nil {
		t.Fatal(err)
	}
	threadB, err := svc.CreateThread(context.Background(), *authB, "User B private")
	if err != nil {
		t.Fatal(err)
	}

	threadsA, err := svc.ListThreads(context.Background(), *authA, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(threadsA) != 1 || threadsA[0].ID != threadA.ID {
		t.Fatalf("user A list leaked or missed data: %#v", threadsA)
	}
	if _, err := svc.GetThread(context.Background(), *authA, threadB.ID); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("user A get user B err = %v", err)
	}
	if _, err := svc.PostMessage(context.Background(), *authA, PostMessageParams{ThreadID: threadB.ID, Body: "nope"}); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("user A post user B err = %v", err)
	}

	if err := svc.RevokeAPIKeyByID(context.Background(), userA, keyA.ID); err != nil {
		t.Fatal(err)
	}
	revokedA, err := svc.AuthenticateAPIKey(context.Background(), keyA.Key)
	if err != nil {
		t.Fatal(err)
	}
	stillB, err := svc.AuthenticateAPIKey(context.Background(), keyB.Key)
	if err != nil {
		t.Fatal(err)
	}
	if revokedA != nil || stillB == nil || stillB.UserID != userB.UserID {
		t.Fatalf("revoke result revokedA=%#v stillB=%#v", revokedA, stillB)
	}
}

func TestOnboardingConnectionsAreExplicitResumableAndActorIsolated(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	user := types.User{
		ID:          "usr_onboarding",
		Email:       "onboarding@example.com",
		DisplayName: "Onboarding User",
		CreatedAt:   "2026-08-02T00:00:00.000Z",
		UpdatedAt:   "2026-08-02T00:00:00.000Z",
	}
	repo.Users = append(repo.Users, user)
	browser := types.AuthContext{
		UserID:          user.ID,
		UserDisplayName: user.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		SessionID:       "sess_onboarding",
		ActorID:         "sess_onboarding",
		ActorName:       "Web dashboard",
	}

	state, err := svc.GetOnboardingState(context.Background(), browser)
	if err != nil || len(state.Steps) != 0 {
		t.Fatalf("initial onboarding state=%#v err=%v", state, err)
	}
	if keys, err := svc.ListAPIKeys(context.Background(), browser); err != nil || len(keys) != 0 {
		t.Fatalf("onboarding read pre-created credentials: keys=%#v err=%v", keys, err)
	}
	if dismissed, err := svc.DismissOnboarding(context.Background(), browser); err != nil || dismissed.DismissedAt == nil {
		t.Fatalf("dismiss onboarding state=%#v err=%v", dismissed, err)
	}

	chatgpt, err := svc.CreateOnboardingConnection(context.Background(), browser, "chatgpt", "https://agentbox.example", false)
	if err != nil {
		t.Fatal(err)
	}
	if chatgpt.Credential.Name != "ChatGPT" || chatgpt.Credential.Key == "" || !strings.Contains(chatgpt.MCPURL, "/api/mcp?key=") || len(chatgpt.State.Steps) != 1 || chatgpt.State.DismissedAt != nil {
		t.Fatalf("chatgpt connection=%#v", chatgpt)
	}
	if chatgpt.State.Steps[0].Credential == nil || chatgpt.State.Steps[0].Credential.Key != "" {
		t.Fatalf("persisted onboarding state exposed secret: %#v", chatgpt.State)
	}
	if _, err := svc.CreateOnboardingConnection(context.Background(), browser, "chatgpt", "https://agentbox.example", false); !hasCodedError(err, "ONBOARDING_CREDENTIAL_EXISTS") {
		t.Fatalf("duplicate initial connector did not require rotation: %v", err)
	}

	raycast, err := svc.CreateOnboardingConnection(context.Background(), browser, "raycast", "https://agentbox.example", false)
	if err != nil {
		t.Fatal(err)
	}
	claude, err := svc.CreateOnboardingConnection(context.Background(), browser, "claude", "https://agentbox.example", false)
	if err != nil {
		t.Fatal(err)
	}
	local, err := svc.CreateOnboardingConnection(context.Background(), browser, "local", "https://agentbox.example", false)
	if err != nil {
		t.Fatal(err)
	}
	connectorSecrets := []string{chatgpt.Credential.Key, claude.Credential.Key, local.Credential.Key, raycast.Credential.Key}
	seenSecrets := map[string]bool{}
	for _, secret := range connectorSecrets {
		if secret == "" || seenSecrets[secret] {
			t.Fatal("onboarding connectors reused or omitted credential material")
		}
		seenSecrets[secret] = true
	}
	if !strings.Contains(local.ProfileCommand, "agentbox profiles add local") || !strings.Contains(local.ProfileCommand, "--user-id '"+user.ID+"'") || !strings.Contains(local.ProfileCommand, "--key-name 'Local CLI'") || !strings.Contains(local.SetupPrompt, "npm install -g @amxv/agentbox") || !strings.Contains(local.SetupPrompt, "agentbox list") {
		t.Fatalf("local setup output=%#v", local)
	}
	if raycast.RaycastSetup == nil || raycast.RaycastSetup.CredentialID != raycast.Credential.ID || raycast.RaycastSetup.Label != raycast.Credential.Name || raycast.RaycastSetup.BaseURL != "https://agentbox.example" || raycast.RaycastSetup.APIKey != raycast.Credential.Key || raycast.RaycastSetup.RepositoryURL != "https://github.com/amxv/agentbox.git" || raycast.RaycastSetup.ExtensionPath != "apps/raycast" || strings.Join(raycast.RaycastSetup.InstallCommands, "\n") != "git clone https://github.com/amxv/agentbox.git\ncd agentbox/apps/raycast\nnpm ci\nnpm run dev" || len(raycast.RaycastSetup.Preferences) != 2 || raycast.RaycastSetup.Preferences[0].Name != "baseUrl" || raycast.RaycastSetup.Preferences[1].Name != "apiKey" || !raycast.RaycastSetup.Preferences[1].Secret || !strings.Contains(raycast.RaycastSetup.FinalCheck, "Browse Threads") {
		t.Fatalf("raycast setup output=%#v", raycast.RaycastSetup)
	}
	if got := strings.Join(raycast.Credential.Scopes, ","); got != "threads:read,threads:write,assets:read,assets:write" || strings.Contains(got, "mcp:use") {
		t.Fatalf("raycast scopes=%q", got)
	}
	if got := []string{local.State.Steps[0].Connector, local.State.Steps[1].Connector, local.State.Steps[2].Connector, local.State.Steps[3].Connector}; strings.Join(got, ",") != "chatgpt,claude,local,raycast" {
		t.Fatalf("onboarding connector order=%v", got)
	}

	thread, err := svc.CreateThread(context.Background(), browser, "same user, separate actors")
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{chatgpt.Credential.Key, claude.Credential.Key, local.Credential.Key, raycast.Credential.Key}
	expectedActors := []string{"ChatGPT", "Claude", "Local CLI", "Raycast"}
	for index, secret := range secrets {
		authContext, err := svc.AuthenticateAPIKey(context.Background(), secret)
		if err != nil || authContext == nil {
			t.Fatalf("connector %s auth=%#v err=%v", expectedActors[index], authContext, err)
		}
		if authContext.UserID != user.ID || authContext.ActorName != expectedActors[index] {
			t.Fatalf("connector attribution=%#v", authContext)
		}
		message, err := svc.PostMessage(context.Background(), *authContext, PostMessageParams{ThreadID: thread.ID, Body: "from " + expectedActors[index]})
		if err != nil || message.CreatedByUserID == nil || *message.CreatedByUserID != user.ID || message.CreatedByActorName == nil || *message.CreatedByActorName != expectedActors[index] {
			t.Fatalf("connector post=%#v err=%v", message, err)
		}
	}

	raycastAuth, err := svc.AuthenticateAPIKey(context.Background(), raycast.Credential.Key)
	if err != nil || raycastAuth == nil {
		t.Fatalf("Raycast auth=%#v err=%v", raycastAuth, err)
	}
	raycastThread, err := svc.CreateThread(context.Background(), *raycastAuth, "Raycast scope matrix")
	if err != nil || raycastThread.CreatedByActorName == nil || *raycastThread.CreatedByActorName != "Raycast" {
		t.Fatalf("Raycast create thread=%#v err=%v", raycastThread, err)
	}
	listed, err := svc.ListThreads(context.Background(), *raycastAuth, 50)
	listedRaycastThread := false
	for _, item := range listed {
		if item.ID == raycastThread.ID {
			listedRaycastThread = true
			break
		}
	}
	if err != nil || !listedRaycastThread {
		t.Fatalf("Raycast list threads=%#v err=%v", listed, err)
	}
	searched, err := svc.SearchThreads(context.Background(), *raycastAuth, types.SearchThreadParams{Query: "scope matrix", Limit: 20})
	if err != nil || len(searched) != 1 || searched[0].ID != raycastThread.ID {
		t.Fatalf("Raycast search threads=%#v err=%v", searched, err)
	}
	loaded, err := svc.GetThread(context.Background(), *raycastAuth, raycastThread.ID)
	if err != nil || loaded == nil || loaded.ID != raycastThread.ID {
		t.Fatalf("Raycast get thread=%#v err=%v", loaded, err)
	}
	raycastAssetMessage, err := svc.PostMessageWithAsset(context.Background(), *raycastAuth, PostMessageWithAssetParams{
		ThreadID: raycastThread.ID,
		Body:     "Raycast attachment",
		Bytes:    []byte("raycast attachment bytes"),
		FileName: "raycast.txt",
	})
	if err != nil || len(raycastAssetMessage.Assets) != 1 {
		t.Fatalf("Raycast upload/post=%#v err=%v", raycastAssetMessage, err)
	}
	if _, err := svc.SignedAssetDownloadURL(context.Background(), *raycastAuth, raycastAssetMessage.Assets[0].ID, 300); err != nil {
		t.Fatalf("Raycast attachment download signing failed: %v", err)
	}
	publish := true
	managed, err := svc.ManageThreadVisibility(context.Background(), *raycastAuth, raycastThread.ID, "https://agentbox.example", types.ManageThreadVisibilityInput{Public: &publish})
	if err != nil || !managed.Public || managed.PublicURL == "" {
		t.Fatalf("Raycast visibility publish=%#v err=%v", managed, err)
	}
	if _, err := svc.ManageThreadVisibility(context.Background(), *raycastAuth, raycastThread.ID, "https://agentbox.example", types.ManageThreadVisibilityInput{}); err != nil {
		t.Fatalf("Raycast visibility read failed: %v", err)
	}

	rotated, err := svc.CreateOnboardingConnection(context.Background(), browser, "chatgpt", "https://agentbox.example", true)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Credential.ID != chatgpt.Credential.ID || rotated.Credential.Key == chatgpt.Credential.Key {
		t.Fatalf("chatgpt rotation=%#v original=%#v", rotated.Credential, chatgpt.Credential)
	}
	if oldAuth, err := svc.AuthenticateAPIKey(context.Background(), chatgpt.Credential.Key); err != nil || oldAuth != nil {
		t.Fatalf("rotated secret remained active: auth=%#v err=%v", oldAuth, err)
	}
	if claudeAuth, err := svc.AuthenticateAPIKey(context.Background(), claude.Credential.Key); err != nil || claudeAuth == nil || claudeAuth.ActorName != "Claude" {
		t.Fatalf("rotating ChatGPT affected Claude: auth=%#v err=%v", claudeAuth, err)
	}

	rotatedRaycast, err := svc.CreateOnboardingConnection(context.Background(), browser, "raycast", "https://agentbox.example", true)
	if err != nil {
		t.Fatal(err)
	}
	if rotatedRaycast.Credential.ID != raycast.Credential.ID || rotatedRaycast.Credential.Key == raycast.Credential.Key {
		t.Fatalf("raycast rotation=%#v original=%#v", rotatedRaycast.Credential, raycast.Credential)
	}
	if oldAuth, err := svc.AuthenticateAPIKey(context.Background(), raycast.Credential.Key); err != nil || oldAuth != nil {
		t.Fatalf("rotated Raycast secret remained active: auth=%#v err=%v", oldAuth, err)
	}
	if localAuth, err := svc.AuthenticateAPIKey(context.Background(), local.Credential.Key); err != nil || localAuth == nil || localAuth.ActorName != "Local CLI" {
		t.Fatalf("rotating Raycast affected Local CLI: auth=%#v err=%v", localAuth, err)
	}

	if err := svc.RevokeAPIKeyByID(context.Background(), browser, local.Credential.ID); err != nil {
		t.Fatal(err)
	}
	state, err = svc.GetOnboardingState(context.Background(), browser)
	if err != nil {
		t.Fatal(err)
	}
	localStep := state.Steps[2]
	if localStep.Connector != "local" || localStep.CompletedAt == nil || localStep.Credential != nil {
		t.Fatalf("revoked local step=%#v", localStep)
	}
	recreated, err := svc.CreateOnboardingConnection(context.Background(), browser, "local", "https://agentbox.example", false)
	if err != nil {
		t.Fatal(err)
	}
	if recreated.Credential.ID == local.Credential.ID || recreated.Credential.Key == local.Credential.Key {
		t.Fatalf("revoked local credential was not recreated independently: %#v", recreated.Credential)
	}

	apiAuth, err := svc.AuthenticateAPIKey(context.Background(), claude.Credential.Key)
	if err != nil || apiAuth == nil {
		t.Fatal("expected active Claude API auth")
	}
	if _, err := svc.GetOnboardingState(context.Background(), *apiAuth); !hasCodedError(err, "BROWSER_SESSION_REQUIRED") {
		t.Fatalf("API credential accessed onboarding state: %v", err)
	}

	secondUser := types.User{ID: "usr_onboarding_second", Email: "second@example.com", DisplayName: "Second User"}
	repo.Users = append(repo.Users, secondUser)
	secondBrowser := types.AuthContext{UserID: secondUser.ID, UserDisplayName: secondUser.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_second", ActorName: "Web dashboard"}
	secondRaycast, err := svc.CreateOnboardingConnection(context.Background(), secondBrowser, "raycast", "https://agentbox.example", false)
	if err != nil || secondRaycast.Credential.Name != "Raycast" || secondRaycast.Credential.Key == rotatedRaycast.Credential.Key {
		t.Fatalf("second-user Raycast=%#v err=%v", secondRaycast, err)
	}
	additionalRaycast, err := svc.CreateAPIKeyWithPurposeAndScopes(context.Background(), browser, "Raycast MacBook", "raycast", ConnectorAPIKeyScopes("raycast"))
	if err != nil || additionalRaycast.ID == rotatedRaycast.Credential.ID || strings.Join(additionalRaycast.Scopes, ",") != "threads:read,threads:write,assets:read,assets:write" {
		t.Fatalf("additional Raycast credential=%#v err=%v", additionalRaycast, err)
	}
}
