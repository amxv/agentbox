package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/types"
)

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
