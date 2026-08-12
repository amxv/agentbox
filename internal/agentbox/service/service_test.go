package service

import (
	"context"
	"encoding/json"
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

func setThreadVisibilityForTest(ctx context.Context, repository interface {
	ManageThreadVisibility(context.Context, string, string, types.ManageThreadVisibilityInput) (types.ManagedThreadVisibility, error)
}, userID string, threadID string, desiredTeamIDs []string) (types.ThreadVisibility, error) {
	current, err := repository.ManageThreadVisibility(ctx, userID, threadID, types.ManageThreadVisibilityInput{})
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	desired := map[string]bool{}
	for _, teamID := range desiredTeamIDs {
		desired[teamID] = true
	}
	currentIDs := map[string]bool{}
	input := types.ManageThreadVisibilityInput{}
	for _, team := range current.SharedTeams {
		currentIDs[team.ID] = true
		if !desired[team.ID] {
			input.RemoveTeams = append(input.RemoveTeams, team.ID)
		}
	}
	for _, teamID := range desiredTeamIDs {
		if !currentIDs[teamID] {
			input.AddTeams = append(input.AddTeams, teamID)
		}
	}
	state, err := repository.ManageThreadVisibility(ctx, userID, threadID, input)
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	return types.ThreadVisibility{ThreadID: state.ThreadID, OwnerUserID: state.OwnerUserID, SharedTeams: state.SharedTeams}, nil
}

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

func TestDirectUploadFinalizationValidatesBatchMetadataAndReplay(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	user := types.User{ID: "usr_upload_integrity", Email: "upload@example.com", DisplayName: "Uploader"}
	repo.Users = append(repo.Users, user)
	authContext := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_upload", ActorName: "Web dashboard"}
	thread, err := svc.CreateThread(context.Background(), authContext, "Upload integrity")
	if err != nil {
		t.Fatal(err)
	}
	mimeType := "text/plain"
	digest := strings.Repeat("a", 64)
	if _, err := svc.CreatePresignedUploads(context.Background(), authContext, thread.ID, []types.UploadIntentFile{{FileName: "first.txt", MimeType: &mimeType, SizeBytes: 4, SHA256: digest}, {FileName: "", SizeBytes: 1, SHA256: digest}}); !hasCodedError(err, "INVALID_ARGUMENT") {
		t.Fatalf("invalid batch error=%v", err)
	}
	if len(repo.Pending) != 0 {
		t.Fatalf("invalid batch left hidden pending rows: %#v", repo.Pending)
	}
	create := func(name string) types.PresignedUpload {
		t.Helper()
		uploads, err := svc.CreatePresignedUploads(context.Background(), authContext, thread.ID, []types.UploadIntentFile{{FileName: name, MimeType: &mimeType, SizeBytes: 4, SHA256: digest}})
		if err != nil || len(uploads) != 1 {
			t.Fatalf("uploads=%#v err=%v", uploads, err)
		}
		return uploads[0]
	}
	post := func(upload types.PresignedUpload) (types.Message, error) {
		return svc.PostMessage(context.Background(), authContext, PostMessageParams{ThreadID: thread.ID, Body: "finalize", UploadedAssets: []types.UploadedAssetReference{{UploadID: upload.UploadID}}})
	}

	missing := create("missing.txt")
	if _, err := post(missing); !hasCodedError(err, "UPLOAD_NOT_MATERIALIZED") {
		t.Fatalf("missing object finalization error=%v", err)
	}
	if len(repo.Messages) != 0 {
		t.Fatalf("missing object mutated messages=%#v", repo.Messages)
	}

	wrongSize := create("wrong-size.txt")
	store.PutAssetObjectWithSHA(wrongSize.StorageKey, 5, &mimeType, digest)
	if _, err := post(wrongSize); !hasCodedError(err, "UPLOAD_METADATA_MISMATCH") {
		t.Fatalf("size mismatch error=%v", err)
	}

	wrongTypeUpload := create("wrong-type.txt")
	wrongType := "application/octet-stream"
	store.PutAssetObjectWithSHA(wrongTypeUpload.StorageKey, 4, &wrongType, digest)
	if _, err := post(wrongTypeUpload); !hasCodedError(err, "UPLOAD_METADATA_MISMATCH") {
		t.Fatalf("type mismatch error=%v", err)
	}

	wrongChecksum := create("wrong-checksum.txt")
	store.PutAssetObjectWithSHA(wrongChecksum.StorageKey, 4, &mimeType, strings.Repeat("b", 64))
	if _, err := post(wrongChecksum); !hasCodedError(err, "UPLOAD_METADATA_MISMATCH") {
		t.Fatalf("checksum mismatch error=%v", err)
	}

	ready := create("ready.txt")
	store.PutAssetObjectWithSHA(ready.StorageKey, 4, &mimeType, digest)
	message, err := post(ready)
	if err != nil || len(message.Assets) != 1 {
		t.Fatalf("finalized message=%#v err=%v", message, err)
	}
	if message.Assets[0].StorageKey == ready.StorageKey || !strings.Contains(message.Assets[0].StorageKey, "/final/sha256/"+digest+"/") {
		t.Fatalf("canonical asset did not use an immutable final key: %#v", message.Assets[0])
	}
	if _, err := post(ready); !hasCodedError(err, "UPLOAD_UNAVAILABLE") {
		t.Fatalf("replay error=%v", err)
	}
	if len(repo.Messages) != 1 || len(repo.Assets) != 1 {
		t.Fatalf("replay duplicated rows messages=%d assets=%d", len(repo.Messages), len(repo.Assets))
	}
}

func TestServerUploadCompensatesWhenAccessIsLostAfterObjectWrite(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	owner := types.User{ID: "usr_comp_owner", Email: "owner-comp@example.com", DisplayName: "Owner"}
	member := types.User{ID: "usr_comp_member", Email: "member-comp@example.com", DisplayName: "Member"}
	repo.Users = append(repo.Users, owner, member)
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_comp_owner", ActorName: "Web dashboard"}
	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_comp_member", ActorName: "Web dashboard"}
	team, err := repo.CreateTeam(context.Background(), "comp-team", "Comp Team")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{owner.ID, member.ID} {
		if _, err := repo.AddTeamMember(context.Background(), team.ID, userID); err != nil {
			t.Fatal(err)
		}
	}
	thread, err := svc.CreateThread(context.Background(), ownerAuth, "Compensation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(context.Background(), repo, owner.ID, thread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	store.AfterUpload = func(types.NewAsset) { _, _ = repo.RemoveTeamMember(context.Background(), team.ID, member.ID) }
	_, err = svc.PostMessageWithAsset(context.Background(), memberAuth, PostMessageWithAssetParams{ThreadID: thread.ID, Body: "must fail", Bytes: []byte("orphan me not"), FileName: "comp.txt"})
	if !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("post after access loss error=%v", err)
	}
	if len(store.Uploads) != 1 || len(store.DeleteCalls) != 1 || store.DeleteCalls[0] != store.Uploads[0].StorageKey {
		t.Fatalf("upload compensation uploads=%#v deletes=%#v", store.Uploads, store.DeleteCalls)
	}
	if len(repo.Messages) != 0 || len(repo.Assets) != 0 {
		t.Fatalf("failed post committed rows messages=%#v assets=%#v", repo.Messages, repo.Assets)
	}
	if _, err := store.HeadAssetObject(context.Background(), store.Uploads[0].StorageKey); err == nil {
		t.Fatal("compensated object still exists")
	}
}

func TestChatGPTFileFailureAndAccessLossLeaveNoPartialState(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	owner := types.User{ID: "usr_chatgpt_owner", Email: "owner-chatgpt@example.com", DisplayName: "ChatGPT Owner"}
	member := types.User{ID: "usr_chatgpt_member", Email: "member-chatgpt@example.com", DisplayName: "ChatGPT Member"}
	repo.Users = append(repo.Users, owner, member)
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_chatgpt_owner", ActorName: "Web dashboard"}
	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectAPIKey, KeyID: "key_chatgpt_member", ActorName: "ChatGPT"}
	team, err := repo.CreateTeam(context.Background(), "chatgpt-team", "ChatGPT Team")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{owner.ID, member.ID} {
		if _, err := repo.AddTeamMember(context.Background(), team.ID, userID); err != nil {
			t.Fatal(err)
		}
	}
	thread, err := svc.CreateThread(context.Background(), ownerAuth, "ChatGPT compensation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(context.Background(), repo, owner.ID, thread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	fileName := "handoff.md"
	mimeType := "text/markdown"
	file := &assets.ChatGPTFileInput{
		DownloadURL: "https://files.openai.example/download/token",
		FileID:      "file_compensation",
		FileName:    &fileName,
		MimeType:    &mimeType,
	}

	store.ChatGPTFailure = errors.New("simulated R2 write failure")
	if _, err := svc.PostMessage(context.Background(), memberAuth, PostMessageParams{ThreadID: thread.ID, Body: "must not persist", File: file}); err == nil || !strings.Contains(err.Error(), "simulated R2 write failure") {
		t.Fatalf("R2 failure error=%v", err)
	}
	if len(store.ChatGPTInputs) != 1 || len(store.Uploads) != 0 || len(store.DeleteCalls) != 0 || len(repo.Messages) != 0 || len(repo.Assets) != 0 {
		t.Fatalf("R2 failure left state inputs=%#v uploads=%#v deletes=%#v messages=%#v assets=%#v", store.ChatGPTInputs, store.Uploads, store.DeleteCalls, repo.Messages, repo.Assets)
	}

	store.ChatGPTFailure = nil
	store.AfterUpload = func(types.NewAsset) { _, _ = repo.RemoveTeamMember(context.Background(), team.ID, member.ID) }
	_, err = svc.PostMessage(context.Background(), memberAuth, PostMessageParams{ThreadID: thread.ID, Body: "must compensate", File: file})
	if !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("post after access loss error=%v", err)
	}
	if len(store.ChatGPTInputs) != 2 || len(store.Uploads) != 1 || len(store.DeleteCalls) != 1 || store.DeleteCalls[0] != store.Uploads[0].StorageKey {
		t.Fatalf("ChatGPT compensation inputs=%#v uploads=%#v deletes=%#v", store.ChatGPTInputs, store.Uploads, store.DeleteCalls)
	}
	if len(repo.Messages) != 0 || len(repo.Assets) != 0 {
		t.Fatalf("failed ChatGPT post committed rows messages=%#v assets=%#v", repo.Messages, repo.Assets)
	}
	if _, err := store.HeadAssetObject(context.Background(), store.Uploads[0].StorageKey); err == nil {
		t.Fatal("compensated ChatGPT object still exists")
	}
}

func TestMissingAttachmentIsUnavailableAcrossAuthenticatedAndPublicSigning(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	user := types.User{ID: "usr_missing_asset", Email: "missing@example.com", DisplayName: "Missing"}
	repo.Users = append(repo.Users, user)
	authContext := types.AuthContext{UserID: user.ID, UserDisplayName: user.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_missing", ActorName: "Web dashboard"}
	thread, err := svc.CreateThread(context.Background(), authContext, "Missing object")
	if err != nil {
		t.Fatal(err)
	}
	mimeType := "image/png"
	message, err := repo.PostMessage(context.Background(), user.ID, thread.ID, authContext, "missing", nil, []types.NewAsset{{StorageKey: "missing/external.png", FileName: "external.png", MimeType: &mimeType, SizeBytes: 9}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SignedAssetDownloadURL(context.Background(), authContext, message.Assets[0].ID, 300); !hasCodedError(err, "ATTACHMENT_UNAVAILABLE") {
		t.Fatalf("authenticated missing object error=%v", err)
	}
	publish := true
	visibility, err := svc.ManageThreadVisibility(context.Background(), authContext, thread.ID, "https://agentbox.example", types.ManageThreadVisibilityInput{Public: &publish})
	if err != nil || visibility.PublicLink == nil {
		t.Fatalf("publish=%#v err=%v", visibility, err)
	}
	view, err := svc.GetPublicThread(context.Background(), visibility.PublicLink.Token)
	if err != nil || len(view.Messages) != 1 || len(view.Messages[0].Assets) != 1 {
		t.Fatalf("public view=%#v err=%v", view, err)
	}
	publicAsset := view.Messages[0].Assets[0]
	if publicAsset.Unavailable || publicAsset.DownloadPath == "" || publicAsset.PreviewPath == "" {
		t.Fatalf("missing public asset=%#v", publicAsset)
	}
	if _, err := svc.PublicAssetDownloadURL(context.Background(), visibility.PublicLink.Token, message.Assets[0].ID); !hasCodedError(err, "ATTACHMENT_UNAVAILABLE") {
		t.Fatalf("public missing object error=%v", err)
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

func TestUnifiedThreadFiltersAndVisibilitySummaries(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	owner := types.AuthContext{UserID: "usr_filter_owner", UserDisplayName: "Filter Owner", ActorName: "Web dashboard", SubjectType: types.AuthSubjectUserSession}
	member := types.AuthContext{UserID: "usr_filter_member", UserDisplayName: "Filter Member", ActorName: "Web dashboard", SubjectType: types.AuthSubjectUserSession}
	outsider := types.AuthContext{UserID: "usr_filter_outsider", UserDisplayName: "Filter Outsider", ActorName: "Web dashboard", SubjectType: types.AuthSubjectUserSession}
	for _, authContext := range []types.AuthContext{owner, member, outsider} {
		repo.Users = append(repo.Users, types.User{ID: authContext.UserID, Email: authContext.UserID + "@example.com", DisplayName: authContext.UserDisplayName})
	}
	engineering, err := repo.CreateTeam(context.Background(), "engineering", "Engineering")
	if err != nil {
		t.Fatal(err)
	}
	research, err := repo.CreateTeam(context.Background(), "research", "Research")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{owner.UserID, member.UserID} {
		for _, teamID := range []string{engineering.ID, research.ID} {
			if _, err := repo.AddTeamMember(context.Background(), teamID, userID); err != nil {
				t.Fatal(err)
			}
		}
	}

	privateThread, err := svc.CreateThread(context.Background(), member, "filter marker private")
	if err != nil {
		t.Fatal(err)
	}
	teamThread, err := svc.CreateThread(context.Background(), owner, "filter marker team")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(context.Background(), repo, owner.UserID, teamThread.ID, []string{engineering.ID}); err != nil {
		t.Fatal(err)
	}
	multiThread, err := svc.CreateThread(context.Background(), owner, "filter marker multi public")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(context.Background(), repo, owner.UserID, multiThread.ID, []string{engineering.ID, research.ID}); err != nil {
		t.Fatal(err)
	}
	repo.ThreadPublicLinks = append(repo.ThreadPublicLinks, types.ThreadPublicLink{ThreadID: multiThread.ID, Token: "agpub_filter_multi", TokenHash: "filter-multi-hash", TokenPrefix: "agpub_filter", CreatedAt: multiThread.CreatedAt, UpdatedAt: multiThread.UpdatedAt})
	memberPublicThread, err := svc.CreateThread(context.Background(), member, "filter marker owned public")
	if err != nil {
		t.Fatal(err)
	}
	repo.ThreadPublicLinks = append(repo.ThreadPublicLinks, types.ThreadPublicLink{ThreadID: memberPublicThread.ID, Token: "agpub_filter_owned", TokenHash: "filter-owned-hash", TokenPrefix: "agpub_filter", CreatedAt: memberPublicThread.CreatedAt, UpdatedAt: memberPublicThread.UpdatedAt})
	outsiderPublicThread, err := svc.CreateThread(context.Background(), outsider, "filter marker inaccessible public")
	if err != nil {
		t.Fatal(err)
	}
	repo.ThreadPublicLinks = append(repo.ThreadPublicLinks, types.ThreadPublicLink{ThreadID: outsiderPublicThread.ID, Token: "agpub_filter_outsider", TokenHash: "filter-outsider-hash", TokenPrefix: "agpub_filter", CreatedAt: outsiderPublicThread.CreatedAt, UpdatedAt: outsiderPublicThread.UpdatedAt})

	assertIDs := func(t *testing.T, got []types.Thread, want ...string) {
		t.Helper()
		gotIDs := make([]string, 0, len(got))
		for _, thread := range got {
			gotIDs = append(gotIDs, thread.ID)
		}
		sort.Strings(gotIDs)
		sort.Strings(want)
		if !reflect.DeepEqual(gotIDs, want) {
			t.Fatalf("thread IDs = %v, want %v", gotIDs, want)
		}
	}
	list := func(filter string, teamRef string) []types.Thread {
		threads, err := svc.ListThreadsFiltered(context.Background(), member, types.ThreadListParams{Limit: 50, Filter: filter, TeamRef: teamRef})
		if err != nil {
			t.Fatal(err)
		}
		return threads
	}

	all := list(types.ThreadFilterAll, "")
	assertIDs(t, all, privateThread.ID, teamThread.ID, multiThread.ID, memberPublicThread.ID)
	assertIDs(t, list(types.ThreadFilterPrivate, ""), privateThread.ID)
	assertIDs(t, list(types.ThreadFilterShared, ""), teamThread.ID, multiThread.ID)
	assertIDs(t, list(types.ThreadFilterTeam, engineering.Slug), teamThread.ID, multiThread.ID)
	assertIDs(t, list(types.ThreadFilterTeam, research.ID), multiThread.ID)
	assertIDs(t, list(types.ThreadFilterPublic, ""), multiThread.ID, memberPublicThread.ID)

	byID := map[string]types.Thread{}
	for _, thread := range all {
		byID[thread.ID] = thread
	}
	if summary := byID[privateThread.ID].VisibilitySummary; !summary.OwnedByMe || !summary.Private || summary.Public || len(summary.SharedTeams) != 0 {
		t.Fatalf("private summary = %#v", summary)
	}
	if summary := byID[multiThread.ID].VisibilitySummary; summary.OwnedByMe || !summary.SharedWithMe || !summary.Public || len(summary.SharedTeams) != 2 || len(summary.MatchedTeams) != 2 {
		t.Fatalf("multi-team summary = %#v", summary)
	}

	search, err := svc.SearchThreads(context.Background(), member, types.SearchThreadParams{Query: "filter marker", Limit: 50, Filter: types.ThreadFilterTeam, TeamRef: engineering.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(search) != 2 || !search[0].VisibilitySummary.SharedWithMe || !search[1].VisibilitySummary.SharedWithMe {
		t.Fatalf("filtered search = %#v", search)
	}
	seen := map[string]bool{}
	for _, result := range search {
		if seen[result.ID] {
			t.Fatalf("duplicate multi-team result: %#v", search)
		}
		seen[result.ID] = true
	}

	assertSearchIDs := func(t *testing.T, filter string, teamRef string, want ...string) {
		t.Helper()
		results, err := svc.SearchThreads(context.Background(), member, types.SearchThreadParams{
			Query: "filter marker", Limit: 50, Filter: filter, TeamRef: teamRef,
		})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, 0, len(results))
		for _, result := range results {
			got = append(got, result.ID)
		}
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("search filter=%q team=%q IDs = %v, want %v", filter, teamRef, got, want)
		}
	}
	assertSearchIDs(t, types.ThreadFilterAll, "", privateThread.ID, teamThread.ID, multiThread.ID, memberPublicThread.ID)
	assertSearchIDs(t, types.ThreadFilterPrivate, "", privateThread.ID)
	assertSearchIDs(t, types.ThreadFilterShared, "", teamThread.ID, multiThread.ID)
	assertSearchIDs(t, types.ThreadFilterTeam, engineering.ID, teamThread.ID, multiThread.ID)
	assertSearchIDs(t, types.ThreadFilterTeam, research.Slug, multiThread.ID)
	assertSearchIDs(t, types.ThreadFilterPublic, "", multiThread.ID, memberPublicThread.ID)

	for _, params := range []types.ThreadListParams{
		{Filter: "missing"},
		{Filter: types.ThreadFilterTeam},
		{Filter: types.ThreadFilterPrivate, TeamRef: engineering.ID},
	} {
		if _, err := svc.ListThreadsFiltered(context.Background(), member, params); err == nil {
			t.Fatalf("invalid filter unexpectedly succeeded: %#v", params)
		}
	}
}

func TestOwnerCredentialAdministrationAndDisablementPreserveSharedContent(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	owner := types.User{ID: "usr_phase11_owner", Email: "owner-phase11@example.com", DisplayName: "Owner", IsOwner: true}
	member := types.User{ID: "usr_phase11_member", Email: "member-phase11@example.com", DisplayName: "Member"}
	teammate := types.User{ID: "usr_phase11_teammate", Email: "teammate-phase11@example.com", DisplayName: "Teammate"}
	repo.Users = append(repo.Users, owner, member, teammate)
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_phase11_owner", ActorName: "Web dashboard", IsOwner: true}
	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_phase11_member", ActorName: "Web dashboard"}
	teammateAuth := types.AuthContext{UserID: teammate.ID, UserDisplayName: teammate.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_phase11_teammate", ActorName: "Web dashboard"}

	team, err := repo.CreateTeam(context.Background(), "phase11-team", "Phase 11 Team")
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
	if removed, err := repo.RevokeAPIKey(context.Background(), member.ID, revoked.Name); err != nil || !removed {
		t.Fatalf("pre-revoke credential removed=%t err=%v", removed, err)
	}
	credentials, err := svc.ListOwnerAPIKeys(context.Background(), ownerAuth)
	if err != nil || len(credentials) != 2 || credentials[0].TokenHash == "" || credentials[0].Key != "" {
		t.Fatalf("owner credential metadata=%#v err=%v", credentials, err)
	}
	ownerKeyAuth := ownerAuth
	ownerKeyAuth.SubjectType = types.AuthSubjectAPIKey
	ownerKeyAuth.SessionID = ""
	ownerKeyAuth.KeyID = "key_owner_phase11"
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

func TestOwnerAttachmentPurgeIsUploaderScopedResumableAndTombstoned(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{DeleteFailures: map[string]error{}}
	svc := New(repo, store)
	owner := types.User{ID: "usr_purge_owner", Email: "purge-owner@example.com", DisplayName: "Owner", IsOwner: true}
	target := types.User{ID: "usr_purge_target", Email: "purge-target@example.com", DisplayName: "Target"}
	other := types.User{ID: "usr_purge_other", Email: "purge-other@example.com", DisplayName: "Other"}
	repo.Users = append(repo.Users, owner, target, other)
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_purge_owner", ActorName: "Web dashboard", IsOwner: true}
	targetAuth := types.AuthContext{UserID: target.ID, UserDisplayName: target.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_purge_target", ActorName: "Web dashboard"}
	otherAuth := types.AuthContext{UserID: other.ID, UserDisplayName: other.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_purge_other", ActorName: "Web dashboard"}

	team, err := repo.CreateTeam(context.Background(), "purge-team", "Purge Team")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{owner.ID, target.ID, other.ID} {
		if _, err := repo.AddTeamMember(context.Background(), team.ID, userID); err != nil {
			t.Fatal(err)
		}
	}
	targetThread, err := svc.CreateThread(context.Background(), targetAuth, "Target-owned shared thread")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(context.Background(), repo, target.ID, targetThread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	otherThread, err := svc.CreateThread(context.Background(), otherAuth, "Other-owned shared thread")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(context.Background(), repo, other.ID, otherThread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	targetKey := "agentbox/purge/target-owned.txt"
	crossThreadTargetKey := "agentbox/purge/target-in-other-thread.txt"
	otherKey := "agentbox/purge/other-in-target-thread.txt"
	targetMessage, err := repo.PostMessage(context.Background(), target.ID, targetThread.ID, targetAuth, "target upload", nil, []types.NewAsset{{StorageKey: targetKey, FileName: "target-owned.txt", SizeBytes: 10}})
	if err != nil {
		t.Fatal(err)
	}
	otherMessage, err := repo.PostMessage(context.Background(), other.ID, targetThread.ID, otherAuth, "other upload", nil, []types.NewAsset{{StorageKey: otherKey, FileName: "other-owned.txt", SizeBytes: 20}})
	if err != nil {
		t.Fatal(err)
	}
	crossThreadMessage, err := repo.PostMessage(context.Background(), target.ID, otherThread.ID, targetAuth, "target upload elsewhere", nil, []types.NewAsset{{StorageKey: crossThreadTargetKey, FileName: "cross-thread.txt", SizeBytes: 30}})
	if err != nil {
		t.Fatal(err)
	}
	store.PutAssetObject(targetKey, 10, nil)
	store.PutAssetObject(crossThreadTargetKey, 30, nil)
	store.PutAssetObject(otherKey, 20, nil)
	publicToken := "agpub_purge_fixture"
	repo.ThreadPublicLinks = append(repo.ThreadPublicLinks, types.ThreadPublicLink{ThreadID: targetThread.ID, Token: publicToken, TokenHash: hashSecret(publicToken), TokenPrefix: tokenPrefix(publicToken), CreatedAt: targetThread.CreatedAt, UpdatedAt: targetThread.UpdatedAt})

	if _, err := svc.PurgeUserAttachments(context.Background(), ownerAuth, target.ID, 10); !hasCodedError(err, "USER_ACTIVE") {
		t.Fatalf("active user purge error=%v", err)
	}
	if _, err := svc.PurgeUserAttachments(context.Background(), ownerAuth, owner.ID, 10); !hasCodedError(err, "OWNER_IMMUTABLE") {
		t.Fatalf("owner purge error=%v", err)
	}
	if _, err := svc.PurgeUserAttachments(context.Background(), otherAuth, target.ID, 10); !hasCodedError(err, "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("ordinary user purge error=%v", err)
	}
	if _, err := svc.SetUserDisabled(context.Background(), ownerAuth, target.ID, true); err != nil {
		t.Fatal(err)
	}
	store.DeleteFailures[crossThreadTargetKey] = errors.New("simulated R2 outage")
	first, err := svc.PurgeUserAttachments(context.Background(), ownerAuth, target.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if first.Attempted != 2 || first.Purged != 1 || first.Failed != 1 || first.Remaining != 1 || first.Complete || len(first.Failures) != 1 {
		t.Fatalf("first purge=%#v", first)
	}
	if !reflect.DeepEqual(store.DeleteCalls, []string{targetKey, crossThreadTargetKey}) && !reflect.DeepEqual(store.DeleteCalls, []string{crossThreadTargetKey, targetKey}) {
		t.Fatalf("delete calls=%v", store.DeleteCalls)
	}
	for _, call := range store.DeleteCalls {
		if call == otherKey {
			t.Fatalf("purge deleted another user's object: %v", store.DeleteCalls)
		}
	}

	publicView, err := svc.GetPublicThread(context.Background(), publicToken)
	if err != nil {
		t.Fatal(err)
	}
	var purgedPublic *types.PublicAsset
	var retainedPublic *types.PublicAsset
	for messageIndex := range publicView.Messages {
		for assetIndex := range publicView.Messages[messageIndex].Assets {
			asset := &publicView.Messages[messageIndex].Assets[assetIndex]
			switch asset.ID {
			case targetMessage.Assets[0].ID:
				purgedPublic = asset
			case otherMessage.Assets[0].ID:
				retainedPublic = asset
			}
		}
	}
	if purgedPublic == nil || purgedPublic.PurgedAt == nil || purgedPublic.DownloadPath != "" {
		t.Fatalf("public purge tombstone=%#v", purgedPublic)
	}
	if retainedPublic == nil || retainedPublic.PurgedAt != nil || retainedPublic.DownloadPath == "" {
		t.Fatalf("retained public asset=%#v", retainedPublic)
	}
	if _, err := svc.PublicAssetDownloadURL(context.Background(), publicToken, targetMessage.Assets[0].ID); !hasCodedError(err, "ATTACHMENT_PURGED") {
		t.Fatalf("purged public asset signed: %v", err)
	}
	if _, err := svc.SignedAssetDownloadURL(context.Background(), ownerAuth, targetMessage.Assets[0].ID, 300); !hasCodedError(err, "ATTACHMENT_PURGED") {
		t.Fatalf("purged authenticated asset signed: %v", err)
	}
	if _, err := svc.PublicAssetDownloadURL(context.Background(), publicToken, otherMessage.Assets[0].ID); err != nil {
		t.Fatalf("retained public asset did not sign: %v", err)
	}

	delete(store.DeleteFailures, crossThreadTargetKey)
	second, err := svc.PurgeUserAttachments(context.Background(), ownerAuth, target.ID, 1)
	if err != nil || second.Purged != 1 || second.Failed != 0 || second.Remaining != 0 || !second.Complete {
		t.Fatalf("second purge=%#v err=%v", second, err)
	}
	if crossThreadMessage.Assets[0].CreatedByUserID == nil || *crossThreadMessage.Assets[0].CreatedByUserID != target.ID {
		t.Fatalf("cross-thread uploader attribution=%#v", crossThreadMessage.Assets[0])
	}
	deleteCount := len(store.DeleteCalls)
	completed, err := svc.PurgeUserAttachments(context.Background(), ownerAuth, target.ID, 10)
	if err != nil || !completed.Complete || completed.Attempted != 0 || completed.Purged != 0 || completed.Remaining != 0 {
		t.Fatalf("completed retry=%#v err=%v", completed, err)
	}
	if len(store.DeleteCalls) != deleteCount {
		t.Fatalf("completed retry issued more deletes: before=%d after=%v", deleteCount, store.DeleteCalls)
	}
}

func TestOwnerContentContextIsBrowserOnlyAndDoesNotBypassNormalAccess(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	owner := types.User{ID: "usr_owner_content_owner", Email: "owner-content@example.com", DisplayName: "Owner", IsOwner: true}
	member := types.User{ID: "usr_owner_content_member", Email: "member-content@example.com", DisplayName: "Member"}
	other := types.User{ID: "usr_owner_content_other", Email: "other-content@example.com", DisplayName: "Other"}
	repo.Users = append(repo.Users, owner, member, other)
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_owner_content", ActorName: "Web dashboard", IsOwner: true}
	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_member_content", ActorName: "Web dashboard"}
	team, err := repo.CreateTeam(context.Background(), "owner-content-team", "Owner Content Team")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{member.ID, other.ID} {
		if _, err := repo.AddTeamMember(context.Background(), team.ID, userID); err != nil {
			t.Fatal(err)
		}
	}
	ownerThread, err := svc.CreateThread(context.Background(), ownerAuth, "Owner private thread")
	if err != nil {
		t.Fatal(err)
	}
	memberPrivate, err := svc.CreateThread(context.Background(), memberAuth, "Member private audit marker")
	if err != nil {
		t.Fatal(err)
	}
	memberShared, err := svc.CreateThread(context.Background(), memberAuth, "Member shared thread")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(context.Background(), repo, member.ID, memberShared.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	message, err := repo.PostMessage(context.Background(), member.ID, memberPrivate.ID, memberAuth, "private searchable evidence", nil, []types.NewAsset{{StorageKey: "agentbox/owner-content/private.txt", FileName: "private.txt", SizeBytes: 17}})
	if err != nil {
		t.Fatal(err)
	}
	store.PutAssetObject(message.Assets[0].StorageKey, 17, nil)
	if _, err := svc.GetThread(context.Background(), ownerAuth, memberPrivate.ID); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("normal owner access bypassed private thread: %v", err)
	}
	ownerKeyAuth := ownerAuth
	ownerKeyAuth.SubjectType = types.AuthSubjectAPIKey
	ownerKeyAuth.SessionID = ""
	ownerKeyAuth.KeyID = "key_owner_content"
	if _, err := svc.ResolveOwnerWebContext(ownerKeyAuth); !hasCodedError(err, "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("owner API key resolved owner content context: %v", err)
	}
	if _, err := svc.ResolveOwnerWebContext(memberAuth); !hasCodedError(err, "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("ordinary browser resolved owner content context: %v", err)
	}
	ownerContext, err := svc.ResolveOwnerWebContext(ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListOwnerContentThreads(context.Background(), OwnerWebContext{}, types.OwnerContentListParams{}); !hasCodedError(err, "OWNER_BROWSER_REQUIRED") {
		t.Fatalf("zero owner content context succeeded: %v", err)
	}
	all, err := svc.ListOwnerContentThreads(context.Background(), ownerContext, types.OwnerContentListParams{Limit: 50})
	if err != nil || len(all) != 3 {
		t.Fatalf("owner content list=%#v err=%v", all, err)
	}
	memberOnly, err := svc.ListOwnerContentThreads(context.Background(), ownerContext, types.OwnerContentListParams{Limit: 50, UserID: member.ID})
	if err != nil || len(memberOnly) != 2 {
		t.Fatalf("owner user filter=%#v err=%v", memberOnly, err)
	}
	teamOnly, err := svc.ListOwnerContentThreads(context.Background(), ownerContext, types.OwnerContentListParams{Limit: 50, TeamRef: team.Slug})
	if err != nil || len(teamOnly) != 1 || teamOnly[0].ID != memberShared.ID {
		t.Fatalf("owner team filter=%#v err=%v", teamOnly, err)
	}
	search, err := svc.SearchOwnerContentThreads(context.Background(), ownerContext, types.OwnerContentSearchParams{Query: "searchable evidence", Limit: 50})
	if err != nil || len(search) != 1 || search[0].ID != memberPrivate.ID || len(search[0].MatchedSnippets) == 0 {
		t.Fatalf("owner content search=%#v err=%v", search, err)
	}
	detail, err := svc.GetOwnerContentThread(context.Background(), ownerContext, memberPrivate.ID)
	if err != nil || detail == nil || detail.Owner.ID != member.ID || len(detail.Messages) != 1 || detail.Messages[0].ID != message.ID || !detail.VisibilitySummary.Private {
		t.Fatalf("owner content detail=%#v err=%v", detail, err)
	}
	downloadURL, err := svc.SignedOwnerContentAssetDownloadURL(context.Background(), ownerContext, message.Assets[0].ID, 300)
	if err != nil || downloadURL == "" {
		t.Fatalf("owner content download=%q err=%v", downloadURL, err)
	}
	assetID := message.Assets[0].ID
	if _, err := repo.MarkAssetPurged(context.Background(), assetID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SignedOwnerContentAssetDownloadURL(context.Background(), ownerContext, assetID, 300); !hasCodedError(err, "ATTACHMENT_PURGED") {
		t.Fatalf("owner content signed purged asset: %v", err)
	}
	if ownerThread.ID == "" {
		t.Fatal("owner fixture thread missing")
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

	if err := svc.RevokeAPIKey(context.Background(), userA, "shared"); err != nil {
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
	if raycast.RaycastSetup == nil || raycast.RaycastSetup.CredentialID != raycast.Credential.ID || raycast.RaycastSetup.Label != raycast.Credential.Name || raycast.RaycastSetup.BaseURL != "https://agentbox.example" || raycast.RaycastSetup.APIKey != raycast.Credential.Key || raycast.RaycastSetup.RepositoryURL != "https://github.com/amxv/agentbox.git" || raycast.RaycastSetup.ExtensionPath != "raycast/agentbox" || strings.Join(raycast.RaycastSetup.InstallCommands, "\n") != "git clone https://github.com/amxv/agentbox.git\ncd agentbox/raycast/agentbox\nnpm install\nnpm run dev" || len(raycast.RaycastSetup.Preferences) != 2 || raycast.RaycastSetup.Preferences[0].Name != "baseUrl" || raycast.RaycastSetup.Preferences[1].Name != "apiKey" || !raycast.RaycastSetup.Preferences[1].Secret || !strings.Contains(raycast.RaycastSetup.FinalCheck, "Browse Threads") {
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

func TestPublicThreadLinksAreHashedRevocableAndTokenScoped(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	owner := types.User{ID: "usr_public_owner", Email: "public-owner@example.com", DisplayName: "Public Owner"}
	member := types.User{ID: "usr_public_member", Email: "public-member@example.com", DisplayName: "Public Member"}
	outsider := types.User{ID: "usr_public_outsider", Email: "public-outsider@example.com", DisplayName: "Public Outsider"}
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
	if _, err := repo.AddTeamMember(context.Background(), team.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	thread, err := repo.CreateThread(context.Background(), owner.ID, "Public marker", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(context.Background(), repo, owner.ID, thread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	mimeType := "image/png"
	message, err := repo.PostMessage(context.Background(), owner.ID, thread.ID, ownerAuth, "Public body", nil, []types.NewAsset{{StorageKey: "agentbox/" + owner.ID + "/" + thread.ID + "/public.png", FileName: "public.png", MimeType: &mimeType, SizeBytes: 6}})
	if err != nil || len(message.Assets) != 1 {
		t.Fatalf("public fixture message=%#v err=%v", message, err)
	}
	store.PutAssetObject(message.Assets[0].StorageKey, 6, &mimeType)
	otherThread, err := repo.CreateThread(context.Background(), owner.ID, "Other private", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	otherMessage, err := repo.PostMessage(context.Background(), owner.ID, otherThread.ID, ownerAuth, "Other body", nil, []types.NewAsset{{StorageKey: "agentbox/" + owner.ID + "/" + otherThread.ID + "/other.png", FileName: "other.png", MimeType: &mimeType, SizeBytes: 5}})
	if err != nil || len(otherMessage.Assets) != 1 {
		t.Fatalf("other fixture message=%#v err=%v", otherMessage, err)
	}
	store.PutAssetObject(otherMessage.Assets[0].StorageKey, 5, &mimeType)

	initial, err := svc.ManageThreadVisibility(context.Background(), memberAuth, thread.ID, "https://agentbox.example", types.ManageThreadVisibilityInput{})
	if err != nil || initial.Public || initial.PublicLink != nil {
		t.Fatalf("initial visibility=%#v err=%v", initial, err)
	}
	publish := true
	created, err := svc.ManageThreadVisibility(context.Background(), memberAuth, thread.ID, "https://agentbox.example", types.ManageThreadVisibilityInput{Public: &publish})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Public || created.PublicLink == nil || !strings.HasPrefix(created.PublicLink.Token, "agpub_") || created.PublicURL != "https://agentbox.example/share/"+created.PublicLink.Token || created.PublicLink.TokenHash == "" || created.PublicLink.TokenHash == created.PublicLink.Token {
		t.Fatalf("created visibility=%#v", created)
	}
	createdToken := created.PublicLink.Token
	idempotent, err := svc.ManageThreadVisibility(context.Background(), memberAuth, thread.ID, "https://agentbox.example", types.ManageThreadVisibilityInput{Public: &publish})
	if err != nil || idempotent.PublicLink == nil || idempotent.PublicLink.Token != createdToken {
		t.Fatalf("idempotent publish=%#v err=%v", idempotent, err)
	}
	metadata, err := svc.ManageThreadVisibility(context.Background(), ownerAuth, thread.ID, "https://agentbox.example", types.ManageThreadVisibilityInput{})
	if err != nil || metadata.PublicLink == nil || metadata.PublicLink.TokenPrefix == "" || metadata.PublicURL == "" {
		t.Fatalf("public metadata=%#v err=%v", metadata, err)
	}

	publicView, err := svc.GetPublicThread(context.Background(), createdToken)
	if err != nil || publicView == nil || publicView.ID != thread.ID || len(publicView.Messages) != 1 || len(publicView.Messages[0].Assets) != 1 {
		t.Fatalf("public view=%#v err=%v", publicView, err)
	}
	publicAsset := publicView.Messages[0].Assets[0]
	if publicAsset.DownloadPath == "" || publicAsset.PreviewPath == "" || publicAsset.Unavailable {
		t.Fatalf("public image asset=%#v", publicAsset)
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
	if downloadURL, err := svc.PublicAssetDownloadURL(context.Background(), createdToken, message.Assets[0].ID); err != nil || downloadURL == "" {
		t.Fatalf("public asset signing url=%q err=%v", downloadURL, err)
	}
	if previewURL, err := svc.PublicAssetPreviewURL(context.Background(), createdToken, message.Assets[0].ID); err != nil || !strings.Contains(previewURL, "inline") {
		t.Fatalf("public preview url=%q err=%v", previewURL, err)
	}
	if _, err := svc.PublicAssetDownloadURL(context.Background(), createdToken, otherMessage.Assets[0].ID); !hasCodedError(err, "PUBLIC_ASSET_NOT_FOUND") {
		t.Fatalf("cross-thread public asset signing error=%v", err)
	}

	if _, err := svc.ManageThreadVisibility(context.Background(), outsiderAuth, thread.ID, "https://agentbox.example", types.ManageThreadVisibilityInput{RegeneratePublicLink: true}); !hasCodedError(err, "THREAD_NOT_FOUND") {
		t.Fatalf("outsider rotated public link: %v", err)
	}
	rotated, err := svc.ManageThreadVisibility(context.Background(), memberAuth, thread.ID, "https://agentbox.example", types.ManageThreadVisibilityInput{RegeneratePublicLink: true})
	if err != nil || rotated.PublicLink == nil || rotated.PublicLink.Token == createdToken {
		t.Fatalf("rotated visibility=%#v err=%v", rotated, err)
	}
	rotatedToken := rotated.PublicLink.Token
	if _, err := svc.GetPublicThread(context.Background(), createdToken); !hasCodedError(err, "PUBLIC_LINK_NOT_FOUND") {
		t.Fatalf("old public token remained active: %v", err)
	}
	if view, err := svc.GetPublicThread(context.Background(), rotatedToken); err != nil || view == nil || view.ID != thread.ID {
		t.Fatalf("rotated public token view=%#v err=%v", view, err)
	}

	unpublish := false
	unpublished, err := svc.ManageThreadVisibility(context.Background(), memberAuth, thread.ID, "https://agentbox.example", types.ManageThreadVisibilityInput{Public: &unpublish})
	if err != nil || unpublished.Public || unpublished.PublicLink != nil {
		t.Fatalf("unpublish=%#v err=%v", unpublished, err)
	}
	if _, err := svc.GetPublicThread(context.Background(), rotatedToken); !hasCodedError(err, "PUBLIC_LINK_NOT_FOUND") {
		t.Fatalf("revoked public token remained active: %v", err)
	}
	if _, err := svc.PublicAssetDownloadURL(context.Background(), rotatedToken, message.Assets[0].ID); !hasCodedError(err, "PUBLIC_ASSET_NOT_FOUND") {
		t.Fatalf("revoked public token signed asset: %v", err)
	}
	recreated, err := svc.ManageThreadVisibility(context.Background(), ownerAuth, thread.ID, "https://agentbox.example", types.ManageThreadVisibilityInput{Public: &publish})
	if err != nil || recreated.PublicLink == nil || recreated.PublicLink.Token == rotatedToken {
		t.Fatalf("recreated visibility=%#v err=%v", recreated, err)
	}
}

func TestServiceEnforcesAPIKeyScopes(t *testing.T) {
	repo := &db.MemoryRepository{}
	svc := New(repo, &assets.FakeStore{})
	adminAuth := types.AuthContext{UserID: "usr_admin", SubjectType: types.AuthSubjectUserSession, ActorName: "admin"}
	repo.Users = append(repo.Users, types.User{ID: adminAuth.UserID, Email: "admin@example.com", DisplayName: "Admin"})
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
	_, err = svc.CreatePresignedUploads(context.Background(), *restrictedAuth, thread.ID, []types.UploadIntentFile{{FileName: "asset.txt", SHA256: strings.Repeat("a", 64)}})
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
	if _, err := svc.CreatePresignedUploads(context.Background(), *scopedAuth, thread.ID, []types.UploadIntentFile{{FileName: "next.txt", SHA256: strings.Repeat("a", 64)}}); err != nil {
		t.Fatalf("scoped upload intent failed: %v", err)
	}
}

func testAuth(userRef string, actorName string) types.AuthContext {
	return types.AuthContext{
		UserID:      "usr_" + userRef,
		SubjectType: types.AuthSubjectAPIKey,
		ActorName:   actorName,
		KeyID:       "key_" + userRef,
	}
}
