package service

import (
	"context"
	"encoding/json"
	"errors"
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
		Tenants: []types.Tenant{{ID: types.DefaultTenantID, Slug: "default", Name: "Default"}},
		Users: []types.User{{
			ID:           "usr_owner",
			TenantID:     types.DefaultTenantID,
			Email:        "owner@example.com",
			DisplayName:  "Owner Person",
			PasswordHash: &passwordHash,
			Role:         "admin",
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
	if sessionAuth.TenantID != types.DefaultTenantID {
		t.Fatalf("tenant selector changed account choice: %#v", sessionAuth)
	}

	credential, err := svc.CreateAPIKeyWithPurposeAndScopes(context.Background(), sessionAuth, "chatgpt", "chatgpt", []string{"threads:read", "threads:write"})
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
	if credentialAuth.ActorID != credential.ID || credentialAuth.KeyID != credential.ID || credentialAuth.ActorName != "chatgpt" || credentialAuth.IsOwner {
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
	if message.CreatedByUserID == nil || *message.CreatedByUserID != sessionAuth.UserID || message.CreatedByKeyID == nil || *message.CreatedByKeyID != credential.ID || message.CreatedByUserDisplayName == nil || *message.CreatedByUserDisplayName != "Owner Person" || message.CreatedByActorName == nil || *message.CreatedByActorName != "chatgpt" {
		t.Fatalf("unexpected connector attribution: %#v", message)
	}
	shared, err := svc.GetThread(context.Background(), sessionAuth, thread.ID)
	if err != nil || len(shared.Messages) != 1 || shared.Messages[0].ID != message.ID {
		t.Fatalf("same user actors did not share thread access: thread=%#v err=%v", shared, err)
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
	repo := &db.MemoryRepository{
		Tenants: []types.Tenant{{ID: types.DefaultTenantID, Slug: "default", Name: "Default"}},
	}
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
		Tenants: []types.Tenant{{ID: types.DefaultTenantID, Slug: "default", Name: "Default"}},
		Users: []types.User{{
			ID:           "usr_owner",
			TenantID:     types.DefaultTenantID,
			Email:        "owner@example.com",
			DisplayName:  "Owner",
			PasswordHash: &ownerPasswordHash,
			Role:         "admin",
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
	if member.ID == "" || member.IsOwner || member.Role != "member" || memberAuth.UserID != member.ID || memberSessionSecret == "" {
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
		t.Fatalf("Phase 5 membership changed private thread access: %v", err)
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

func hasCodedError(err error, code string) bool {
	var coded CodedError
	return errors.As(err, &coded) && coded.Code == code
}

func TestServiceThreadAndMessageFlow(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	auth := testAuth("ten_a", "author")

	thread, err := svc.CreateThread(context.Background(), auth, "Phase 2")
	if err != nil {
		t.Fatal(err)
	}
	message, err := svc.PostMessage(context.Background(), auth, PostMessageParams{
		ThreadID: thread.ID,
		Body:     "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Author != "author" || len(message.Assets) != 0 {
		t.Fatalf("unexpected message: %#v", message)
	}
	if message.BodyContentType == nil || *message.BodyContentType != "text/plain" {
		t.Fatalf("message content type = %#v", message.BodyContentType)
	}

	got, err := svc.GetThread(context.Background(), auth, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Body != "hello" {
		t.Fatalf("unexpected thread: %#v", got)
	}

	results, err := svc.SearchThreads(context.Background(), auth, types.SearchThreadParams{Query: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != thread.ID || results[0].MessageCount != 1 || results[0].LastMessagePreview != "hello" {
		t.Fatalf("search results = %#v", results)
	}

	_, err = svc.GetThread(context.Background(), auth, "thr_missing")
	if !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("expected ErrThreadNotFound, got %v", err)
	}
	var coded CodedError
	if !errors.As(err, &coded) || coded.Code != "THREAD_NOT_FOUND" {
		t.Fatalf("expected THREAD_NOT_FOUND, got %#v", err)
	}

	_, err = svc.PostMessage(context.Background(), auth, PostMessageParams{
		ThreadID: "thr_missing",
		Body:     "bad",
	})
	if !errors.As(err, &coded) || coded.Code != "THREAD_NOT_FOUND" {
		t.Fatalf("expected coded missing-thread post error, got %#v", err)
	}

	threadWithMessage, firstMessage, err := svc.CreateThreadWithMessage(context.Background(), auth, "Initial", "first body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if firstMessage.ThreadID != threadWithMessage.ID || firstMessage.Body != "first body" {
		t.Fatalf("threadWithMessage=%#v firstMessage=%#v", threadWithMessage, firstMessage)
	}
}

func TestServiceUserPrivateIsolationAndAPIKeys(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	tenantA := testAuth(types.DefaultTenantID, "shared")
	tenantA.UserID = "usr_a"
	tenantA.UserDisplayName = "User A"
	tenantB := testAuth(types.DefaultTenantID, "shared")
	tenantB.UserID = "usr_b"
	tenantB.UserDisplayName = "User B"
	repo.Users = append(repo.Users,
		types.User{ID: tenantA.UserID, TenantID: types.DefaultTenantID, Email: "a@example.com", DisplayName: "User A", Role: "member"},
		types.User{ID: tenantB.UserID, TenantID: types.DefaultTenantID, Email: "b@example.com", DisplayName: "User B", Role: "member"},
	)

	keyA, err := svc.CreateAPIKey(context.Background(), tenantA, "shared")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := svc.CreateAPIKey(context.Background(), tenantB, "shared")
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
	if authA == nil || authA.UserID != tenantA.UserID || authA.ActorName != "shared" || authB == nil || authB.UserID != tenantB.UserID {
		t.Fatalf("auth contexts: %#v %#v", authA, authB)
	}

	threadA, err := svc.CreateThread(context.Background(), *authA, "Tenant A")
	if err != nil {
		t.Fatal(err)
	}
	threadB, err := svc.CreateThread(context.Background(), *authB, "Tenant B")
	if err != nil {
		t.Fatal(err)
	}

	threadsA, err := svc.ListThreads(context.Background(), *authA, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(threadsA) != 1 || threadsA[0].ID != threadA.ID {
		t.Fatalf("tenant A list leaked or missed data: %#v", threadsA)
	}
	if _, err := svc.GetThread(context.Background(), *authA, threadB.ID); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("tenant A get tenant B err = %v", err)
	}
	if _, err := svc.PostMessage(context.Background(), *authA, PostMessageParams{ThreadID: threadB.ID, Body: "nope"}); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("tenant A post tenant B err = %v", err)
	}

	if err := svc.RevokeAPIKey(context.Background(), tenantA, "shared"); err != nil {
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
	if revokedA != nil || stillB == nil || stillB.UserID != tenantB.UserID {
		t.Fatalf("revoke result revokedA=%#v stillB=%#v", revokedA, stillB)
	}
}

func TestOnboardingConnectionsAreExplicitResumableAndActorIsolated(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	user := types.User{
		ID:          "usr_onboarding",
		TenantID:    types.DefaultTenantID,
		Email:       "onboarding@example.com",
		DisplayName: "Onboarding User",
		Role:        "member",
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

	claude, err := svc.CreateOnboardingConnection(context.Background(), browser, "claude", "https://agentbox.example", false)
	if err != nil {
		t.Fatal(err)
	}
	local, err := svc.CreateOnboardingConnection(context.Background(), browser, "local", "https://agentbox.example", false)
	if err != nil {
		t.Fatal(err)
	}
	if chatgpt.Credential.Key == claude.Credential.Key || chatgpt.Credential.Key == local.Credential.Key || claude.Credential.Key == local.Credential.Key {
		t.Fatal("onboarding connectors reused credential material")
	}
	if !strings.Contains(local.ProfileCommand, "agentbox profiles add local") || !strings.Contains(local.ProfileCommand, "--user-id '"+user.ID+"'") || !strings.Contains(local.ProfileCommand, "--key-name 'Local CLI'") || !strings.Contains(local.SetupPrompt, "npm install -g @amxv/agentbox") || !strings.Contains(local.SetupPrompt, "agentbox list") {
		t.Fatalf("local setup output=%#v", local)
	}

	thread, err := svc.CreateThread(context.Background(), browser, "same user, separate actors")
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{chatgpt.Credential.Key, claude.Credential.Key, local.Credential.Key}
	expectedActors := []string{"ChatGPT", "Claude", "Local CLI"}
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

	if err := svc.RevokeAPIKey(context.Background(), browser, "Local CLI"); err != nil {
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
}

func TestPublicThreadLinksAreHashedRevocableAndTokenScoped(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	owner := types.User{ID: "usr_public_owner", TenantID: types.DefaultTenantID, Email: "public-owner@example.com", DisplayName: "Public Owner", Role: "member"}
	member := types.User{ID: "usr_public_member", TenantID: types.DefaultTenantID, Email: "public-member@example.com", DisplayName: "Public Member", Role: "member"}
	outsider := types.User{ID: "usr_public_outsider", TenantID: types.DefaultTenantID, Email: "public-outsider@example.com", DisplayName: "Public Outsider", Role: "member"}
	repo.Users = append(repo.Users, owner, member, outsider)
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_public_owner", ActorName: "Web dashboard", Scopes: defaultAPIKeyScopes()}
	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectAPIKey, KeyID: "key_public_member", ActorName: "Member agent", Scopes: defaultAPIKeyScopes()}
	outsiderAuth := types.AuthContext{UserID: outsider.ID, UserDisplayName: outsider.DisplayName, SubjectType: types.AuthSubjectAPIKey, KeyID: "key_public_outsider", ActorName: "Outsider agent", Scopes: defaultAPIKeyScopes()}

	team, err := repo.CreateTeam(context.Background(), "public-team", "Public Team")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddTeamMember(context.Background(), team.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	thread, err := repo.CreateThread(context.Background(), owner.ID, "Public marker", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetThreadVisibility(context.Background(), owner.ID, thread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	mimeType := "text/plain"
	message, err := repo.PostMessage(context.Background(), owner.ID, thread.ID, ownerAuth, "Public body", nil, []types.NewAsset{{
		StorageKey: "agentbox/" + owner.ID + "/" + thread.ID + "/public.txt",
		FileName:   "public.txt",
		MimeType:   &mimeType,
		SizeBytes:  6,
	}})
	if err != nil || len(message.Assets) != 1 {
		t.Fatalf("public fixture message=%#v err=%v", message, err)
	}
	otherThread, err := repo.CreateThread(context.Background(), owner.ID, "Other private", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	otherMessage, err := repo.PostMessage(context.Background(), owner.ID, otherThread.ID, ownerAuth, "Other body", nil, []types.NewAsset{{
		StorageKey: "agentbox/" + owner.ID + "/" + otherThread.ID + "/other.txt",
		FileName:   "other.txt",
		MimeType:   &mimeType,
		SizeBytes:  5,
	}})
	if err != nil || len(otherMessage.Assets) != 1 {
		t.Fatalf("other fixture message=%#v err=%v", otherMessage, err)
	}

	if link, err := svc.GetThreadPublicLink(context.Background(), memberAuth, thread.ID); err != nil || link != nil {
		t.Fatalf("initial public link=%#v err=%v", link, err)
	}
	created, err := svc.CreateThreadPublicLink(context.Background(), memberAuth, thread.ID, "https://agentbox.example", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Token, "agpub_") || created.PublicURL != "https://agentbox.example/public/"+created.Token || created.Link.TokenHash == "" || created.Link.TokenHash == created.Token {
		t.Fatalf("created public link=%#v", created)
	}
	if _, err := svc.CreateThreadPublicLink(context.Background(), memberAuth, thread.ID, "https://agentbox.example", false); !hasCodedError(err, "PUBLIC_LINK_EXISTS") {
		t.Fatalf("duplicate public link error=%v", err)
	}
	metadata, err := svc.GetThreadPublicLink(context.Background(), ownerAuth, thread.ID)
	if err != nil || metadata == nil || metadata.TokenPrefix == "" {
		t.Fatalf("public metadata=%#v err=%v", metadata, err)
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(metadataJSON), created.Token) || strings.Contains(string(metadataJSON), created.Link.TokenHash) || strings.Contains(string(metadataJSON), "token_hash") {
		t.Fatalf("public metadata leaked token material: %s", metadataJSON)
	}

	publicView, err := svc.GetPublicThread(context.Background(), created.Token)
	if err != nil || publicView == nil || publicView.ID != thread.ID || len(publicView.Messages) != 1 || len(publicView.Messages[0].Assets) != 1 {
		t.Fatalf("public view=%#v err=%v", publicView, err)
	}
	publicJSON, err := json.Marshal(publicView)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(publicJSON)
	for _, forbidden := range []string{"tenant_id", "owner_user_id", "created_by_user_id", "created_by_key_id", "storage_key", "token_hash"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("public payload leaked %q: %s", forbidden, serialized)
		}
	}
	if !strings.Contains(publicView.Messages[0].Assets[0].DownloadPath, "/api/public/threads/") || !strings.Contains(publicView.Messages[0].Assets[0].DownloadPath, message.Assets[0].ID) {
		t.Fatalf("public asset path=%q", publicView.Messages[0].Assets[0].DownloadPath)
	}
	if downloadURL, err := svc.PublicAssetDownloadURL(context.Background(), created.Token, message.Assets[0].ID); err != nil || downloadURL == "" {
		t.Fatalf("public asset signing url=%q err=%v", downloadURL, err)
	}
	if _, err := svc.PublicAssetDownloadURL(context.Background(), created.Token, otherMessage.Assets[0].ID); !hasCodedError(err, "PUBLIC_ASSET_NOT_FOUND") {
		t.Fatalf("cross-thread public asset signing error=%v", err)
	}

	if _, err := svc.CreateThreadPublicLink(context.Background(), outsiderAuth, thread.ID, "https://agentbox.example", true); !hasCodedError(err, "THREAD_NOT_FOUND") {
		t.Fatalf("outsider rotated public link: %v", err)
	}
	rotated, err := svc.CreateThreadPublicLink(context.Background(), memberAuth, thread.ID, "https://agentbox.example", true)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Token == created.Token || rotated.Link.ThreadID != thread.ID {
		t.Fatalf("rotated public link=%#v original=%#v", rotated, created)
	}
	if _, err := svc.GetPublicThread(context.Background(), created.Token); !hasCodedError(err, "PUBLIC_LINK_NOT_FOUND") {
		t.Fatalf("old public token remained active: %v", err)
	}
	if view, err := svc.GetPublicThread(context.Background(), rotated.Token); err != nil || view == nil || view.ID != thread.ID {
		t.Fatalf("rotated public token view=%#v err=%v", view, err)
	}
	if err := svc.RevokeThreadPublicLink(context.Background(), memberAuth, thread.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublicThread(context.Background(), rotated.Token); !hasCodedError(err, "PUBLIC_LINK_NOT_FOUND") {
		t.Fatalf("revoked public token remained active: %v", err)
	}
	if _, err := svc.PublicAssetDownloadURL(context.Background(), rotated.Token, message.Assets[0].ID); !hasCodedError(err, "PUBLIC_ASSET_NOT_FOUND") {
		t.Fatalf("revoked public token signed asset: %v", err)
	}
	if link, err := svc.GetThreadPublicLink(context.Background(), ownerAuth, thread.ID); err != nil || link != nil {
		t.Fatalf("revoked public metadata=%#v err=%v", link, err)
	}
	recreated, err := svc.CreateThreadPublicLink(context.Background(), ownerAuth, thread.ID, "https://agentbox.example", false)
	if err != nil || recreated.Token == rotated.Token {
		t.Fatalf("recreated public link=%#v err=%v", recreated, err)
	}
}

func TestServiceEnforcesAPIKeyScopes(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	adminAuth := types.AuthContext{TenantID: "ten_a", UserID: "usr_admin", SubjectType: types.AuthSubjectUserSession, ActorName: "admin", Role: "admin"}
	repo.Users = append(repo.Users, types.User{ID: adminAuth.UserID, TenantID: "ten_a", Email: "admin@example.com", DisplayName: "Admin", Role: "admin"})
	thread, err := svc.CreateThread(context.Background(), adminAuth, "Scoped")
	if err != nil {
		t.Fatal(err)
	}
	message, err := svc.PostMessageWithAsset(context.Background(), adminAuth, PostMessageWithAssetParams{
		ThreadID: thread.ID,
		Body:     "with asset",
		Bytes:    []byte("asset"),
		FileName: "asset.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Assets) != 1 {
		t.Fatalf("expected one asset, got %#v", message.Assets)
	}

	restrictedKey, err := svc.CreateAPIKeyWithScopes(context.Background(), adminAuth, "keys-only", []string{"keys:read"})
	if err != nil {
		t.Fatal(err)
	}
	restrictedAuth, err := svc.AuthenticateAPIKey(context.Background(), restrictedKey.Key)
	if err != nil {
		t.Fatal(err)
	}
	if restrictedAuth == nil {
		t.Fatal("restricted key did not authenticate")
	}
	assertScopeDenied := func(label string, err error) {
		t.Helper()
		var coded CodedError
		if !errors.As(err, &coded) || coded.Code != "PERMISSION_DENIED" {
			t.Fatalf("%s expected PERMISSION_DENIED, got %#v", label, err)
		}
	}
	_, err = svc.ListThreads(context.Background(), *restrictedAuth, 10)
	assertScopeDenied("list", err)
	_, err = svc.GetThread(context.Background(), *restrictedAuth, thread.ID)
	assertScopeDenied("get thread", err)
	_, err = svc.CreateThread(context.Background(), *restrictedAuth, "Nope")
	assertScopeDenied("create thread", err)
	_, err = svc.PostMessage(context.Background(), *restrictedAuth, PostMessageParams{ThreadID: thread.ID, Body: "nope"})
	assertScopeDenied("post message", err)
	_, err = svc.CreatePresignedUploads(context.Background(), *restrictedAuth, thread.ID, []types.UploadIntentFile{{FileName: "asset.txt"}})
	assertScopeDenied("upload intent", err)
	_, err = svc.GetAsset(context.Background(), *restrictedAuth, message.Assets[0].ID)
	assertScopeDenied("get asset", err)
	_, err = svc.SignedAssetDownloadURL(context.Background(), *restrictedAuth, message.Assets[0].ID, 300)
	assertScopeDenied("sign asset", err)

	scopedKey, err := svc.CreateAPIKeyWithScopes(context.Background(), adminAuth, "worker", []string{"threads:read", "threads:write", "assets:read", "assets:write"})
	if err != nil {
		t.Fatal(err)
	}
	scopedAuth, err := svc.AuthenticateAPIKey(context.Background(), scopedKey.Key)
	if err != nil {
		t.Fatal(err)
	}
	if scopedAuth == nil {
		t.Fatal("scoped key did not authenticate")
	}
	if _, err := svc.ListThreads(context.Background(), *scopedAuth, 10); err != nil {
		t.Fatalf("scoped list failed: %v", err)
	}
	if _, err := svc.PostMessage(context.Background(), *scopedAuth, PostMessageParams{ThreadID: thread.ID, Body: "ok"}); err != nil {
		t.Fatalf("scoped post failed: %v", err)
	}
	if _, err := svc.GetAsset(context.Background(), *scopedAuth, message.Assets[0].ID); err != nil {
		t.Fatalf("scoped get asset failed: %v", err)
	}
	if _, err := svc.SignedAssetDownloadURL(context.Background(), *scopedAuth, message.Assets[0].ID, 300); err != nil {
		t.Fatalf("scoped sign asset failed: %v", err)
	}
	if _, err := svc.CreatePresignedUploads(context.Background(), *scopedAuth, thread.ID, []types.UploadIntentFile{{FileName: "next.txt"}}); err != nil {
		t.Fatalf("scoped upload intent failed: %v", err)
	}
}

func testAuth(tenantID string, actorName string) types.AuthContext {
	return types.AuthContext{
		TenantID:    tenantID,
		UserID:      "usr_" + tenantID,
		SubjectType: types.AuthSubjectAPIKey,
		ActorName:   actorName,
		KeyID:       "key_" + tenantID,
	}
}

func TestLegacyTenantProvisioningIsDisabled(t *testing.T) {
	svc := New(&db.MemoryRepository{}, &assets.FakeStore{})
	if _, err := svc.ProvisionTenant(context.Background(), ProvisionTenantParams{}); !hasCodedError(err, "LEGACY_TENANT_PROVISIONING_DISABLED") {
		t.Fatalf("legacy tenant provisioning error=%v", err)
	}
}
