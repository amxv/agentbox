package service

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/types"
)

func TestReadAttachmentReturnsRawMarkdownAndAcceptsGenericText(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	auth := types.AuthContext{UserID: "usr_text", UserDisplayName: "Text User", SubjectType: types.AuthSubjectUserSession, SessionID: "sess_text", ActorName: "Web dashboard"}
	thread, err := svc.CreateThread(t.Context(), auth, "Attachment text")
	if err != nil {
		t.Fatal(err)
	}

	markdown := []byte("# Review\n\n- exact markdown\n")
	markdownMessage, err := svc.PostMessageWithAsset(t.Context(), auth, PostMessageWithAssetParams{ThreadID: thread.ID, Body: "markdown", Bytes: markdown, FileName: "review.md"})
	if err != nil {
		t.Fatal(err)
	}
	read, err := svc.ReadAttachment(t.Context(), auth, markdownMessage.Assets[0].ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if read.Text != string(markdown) || read.Encoding != "utf-8" || read.Range.StartByte != 0 || read.Range.EndByte != int64(len(markdown)) || read.Range.TotalBytes != int64(len(markdown)) || read.Range.HasMore || read.Range.NextOffset != nil {
		t.Fatalf("markdown read = %#v", read)
	}
	if read.Asset.ID != markdownMessage.Assets[0].ID || read.Asset.FileName != "review.md" || !strings.Contains(read.Asset.MimeType, "markdown") {
		t.Fatalf("markdown asset = %#v", read.Asset)
	}

	genericMime := "application/octet-stream"
	generic := []byte("#!/usr/bin/env bash\nprintf 'still text\\n'\n")
	genericMessage, err := svc.PostMessageWithAsset(t.Context(), auth, PostMessageWithAssetParams{ThreadID: thread.ID, Body: "generic", Bytes: generic, FileName: "script.data", MimeType: &genericMime})
	if err != nil {
		t.Fatal(err)
	}
	read, err = svc.ReadAttachment(t.Context(), auth, genericMessage.Assets[0].ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if read.Text != string(generic) || read.Asset.MimeType != genericMime {
		t.Fatalf("generic read = %#v", read)
	}
}

func TestReadAttachmentChunksUTF8WithoutSplittingRunes(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	auth := types.AuthContext{UserID: "usr_chunks", UserDisplayName: "Chunk User", SubjectType: types.AuthSubjectUserSession, SessionID: "sess_chunks", ActorName: "Web dashboard"}
	thread, err := svc.CreateThread(t.Context(), auth, "Chunked attachment")
	if err != nil {
		t.Fatal(err)
	}
	prefix := strings.Repeat("a", int(DefaultAttachmentReadBytes)-2)
	contents := []byte(prefix + "🙂" + strings.Repeat("b", 4096))
	message, err := svc.PostMessageWithAsset(t.Context(), auth, PostMessageWithAssetParams{ThreadID: thread.ID, Body: "large", Bytes: contents, FileName: "large.txt"})
	if err != nil {
		t.Fatal(err)
	}

	var reconstructed bytes.Buffer
	offset := int64(0)
	reads := 0
	for {
		read, err := svc.ReadAttachment(t.Context(), auth, message.Assets[0].ID, offset, 0)
		if err != nil {
			t.Fatal(err)
		}
		reads++
		reconstructed.WriteString(read.Text)
		if !read.Range.HasMore {
			break
		}
		if read.Range.NextOffset == nil || *read.Range.NextOffset <= offset {
			t.Fatalf("non-progressing range = %#v", read.Range)
		}
		offset = *read.Range.NextOffset
	}
	if !bytes.Equal(reconstructed.Bytes(), contents) {
		t.Fatalf("reconstructed length=%d want=%d", reconstructed.Len(), len(contents))
	}
	if reads < 2 {
		t.Fatalf("reads=%d, want chunking", reads)
	}
	first, err := svc.ReadAttachment(t.Context(), auth, message.Assets[0].ID, 0, DefaultAttachmentReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if first.Range.EndByte != DefaultAttachmentReadBytes-2 || first.Range.NextOffset == nil || *first.Range.NextOffset != DefaultAttachmentReadBytes-2 {
		t.Fatalf("first UTF-8 boundary = %#v", first.Range)
	}
	if _, err := svc.ReadAttachment(t.Context(), auth, message.Assets[0].ID, DefaultAttachmentReadBytes-1, DefaultAttachmentReadBytes); !hasCodedError(err, "INVALID_ARGUMENT") {
		t.Fatalf("mid-rune offset error = %v", err)
	}
}

func TestReadAttachmentHandlesUTF8BOMAndRejectsBinary(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	auth := types.AuthContext{UserID: "usr_encoding", UserDisplayName: "Encoding User", SubjectType: types.AuthSubjectUserSession, SessionID: "sess_encoding", ActorName: "Web dashboard"}
	thread, err := svc.CreateThread(t.Context(), auth, "Encoding")
	if err != nil {
		t.Fatal(err)
	}
	bomContents := append([]byte{0xef, 0xbb, 0xbf}, []byte("hello\n")...)
	bomMessage, err := svc.PostMessageWithAsset(t.Context(), auth, PostMessageWithAssetParams{ThreadID: thread.ID, Body: "bom", Bytes: bomContents, FileName: "bom.txt"})
	if err != nil {
		t.Fatal(err)
	}
	read, err := svc.ReadAttachment(t.Context(), auth, bomMessage.Assets[0].ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if read.Text != "hello\n" || read.Range.EndByte != int64(len(bomContents)) {
		t.Fatalf("BOM read = %#v", read)
	}

	binary := []byte{'P', 'K', 0, 3, 4, 0xff, 0xfe}
	binaryMessage, err := svc.PostMessageWithAsset(t.Context(), auth, PostMessageWithAssetParams{ThreadID: thread.ID, Body: "binary", Bytes: binary, FileName: "archive.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadAttachment(t.Context(), auth, binaryMessage.Assets[0].ID, 0, 0); !hasCodedError(err, "ATTACHMENT_NOT_TEXT") {
		t.Fatalf("binary read error = %v", err)
	}
}

func TestReadAttachmentReauthorizesAfterStorageIO(t *testing.T) {
	ctx := context.Background()
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	owner := types.User{ID: "usr_read_owner", Email: "read-owner@example.com", DisplayName: "Read Owner"}
	member := types.User{ID: "usr_read_member", Email: "read-member@example.com", DisplayName: "Read Member"}
	repo.Users = append(repo.Users, owner, member)
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_read_owner", ActorName: "Web dashboard"}
	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectAPIKey, KeyID: "key_read_member", ActorName: "ChatGPT", Scopes: []string{"threads:read", "assets:read"}}
	team, err := repo.CreateTeam(ctx, "attachment-readers", "Attachment Readers")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{owner.ID, member.ID} {
		if _, err := repo.AddTeamMember(ctx, team.ID, userID); err != nil {
			t.Fatal(err)
		}
	}
	thread, err := svc.CreateThread(ctx, ownerAuth, "Authorization race")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(ctx, repo, owner.ID, thread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	message, err := svc.PostMessageWithAsset(ctx, ownerAuth, PostMessageWithAssetParams{ThreadID: thread.ID, Body: "text", Bytes: []byte("secret shared context"), FileName: "context.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if download, err := svc.PrepareAttachmentDownload(ctx, memberAuth, message.Assets[0].ID, 300); err != nil || !strings.HasPrefix(download.URL, "https://r2.test/") {
		t.Fatalf("team-member download=%#v err=%v", download, err)
	}

	removed := false
	store.AfterRead = func(_ assets.ReadAssetRangeParams, _ []byte) {
		if removed {
			return
		}
		removed = true
		_, _ = repo.RemoveTeamMember(ctx, team.ID, member.ID)
	}
	if _, err := svc.ReadAttachment(ctx, memberAuth, message.Assets[0].ID, 0, 0); !hasCodedError(err, "ATTACHMENT_NOT_FOUND") {
		t.Fatalf("authorization-loss read error = %v", err)
	}
	if !removed || len(store.ReadCalls) == 0 {
		t.Fatalf("storage race was not exercised: removed=%v reads=%#v", removed, store.ReadCalls)
	}
	if _, err := svc.PrepareAttachmentDownload(ctx, memberAuth, message.Assets[0].ID, 300); !hasCodedError(err, "ATTACHMENT_NOT_FOUND") {
		t.Fatalf("removed member retained download access: %v", err)
	}
}

func TestAttachmentReadAndDownloadRequireAssetScopeAndHideMissingAssets(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	ownerAuth := types.AuthContext{UserID: "usr_scope", UserDisplayName: "Scope User", SubjectType: types.AuthSubjectUserSession, SessionID: "sess_scope", ActorName: "Web dashboard"}
	thread, err := svc.CreateThread(t.Context(), ownerAuth, "Scopes")
	if err != nil {
		t.Fatal(err)
	}
	message, err := svc.PostMessageWithAsset(t.Context(), ownerAuth, PostMessageWithAssetParams{ThreadID: thread.ID, Body: "text", Bytes: []byte("hello"), FileName: "hello.txt"})
	if err != nil {
		t.Fatal(err)
	}
	restricted := types.AuthContext{UserID: ownerAuth.UserID, UserDisplayName: ownerAuth.UserDisplayName, SubjectType: types.AuthSubjectAPIKey, KeyID: "key_scope", ActorName: "restricted", Scopes: []string{"threads:read"}}
	if _, err := svc.ReadAttachment(t.Context(), restricted, message.Assets[0].ID, 0, 0); !hasCodedError(err, "PERMISSION_DENIED") {
		t.Fatalf("restricted read error = %v", err)
	}
	if _, err := svc.PrepareAttachmentDownload(t.Context(), restricted, message.Assets[0].ID, 300); !hasCodedError(err, "PERMISSION_DENIED") {
		t.Fatalf("restricted download error = %v", err)
	}

	if _, err := svc.ReadAttachment(t.Context(), ownerAuth, "asset_missing", 0, 0); !hasCodedError(err, "ATTACHMENT_NOT_FOUND") {
		t.Fatalf("missing read error = %v", err)
	}
	if _, err := svc.PrepareAttachmentDownload(t.Context(), ownerAuth, "asset_missing", 300); !hasCodedError(err, "ATTACHMENT_NOT_FOUND") {
		t.Fatalf("missing download error = %v", err)
	}

	outsider := types.AuthContext{UserID: "usr_outsider", UserDisplayName: "Outsider", SubjectType: types.AuthSubjectAPIKey, KeyID: "key_outsider", ActorName: "Claude", Scopes: []string{"threads:read", "assets:read"}}
	if _, err := svc.ReadAttachment(t.Context(), outsider, message.Assets[0].ID, 0, 0); !hasCodedError(err, "ATTACHMENT_NOT_FOUND") {
		t.Fatalf("cross-user read error = %v", err)
	}
	if _, err := svc.PrepareAttachmentDownload(t.Context(), outsider, message.Assets[0].ID, 300); !hasCodedError(err, "ATTACHMENT_NOT_FOUND") {
		t.Fatalf("cross-user download error = %v", err)
	}
}

func TestAttachmentReadAndDownloadRejectPurgedOrMissingStorage(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	auth := types.AuthContext{UserID: "usr_unavailable", UserDisplayName: "Unavailable User", SubjectType: types.AuthSubjectUserSession, SessionID: "sess_unavailable", ActorName: "Web dashboard"}
	thread, err := svc.CreateThread(t.Context(), auth, "Unavailable attachments")
	if err != nil {
		t.Fatal(err)
	}

	missingMessage, err := svc.PostMessageWithAsset(t.Context(), auth, PostMessageWithAssetParams{ThreadID: thread.ID, Body: "missing", Bytes: []byte("gone"), FileName: "gone.txt"})
	if err != nil {
		t.Fatal(err)
	}
	missingAsset := missingMessage.Assets[0]
	if err := store.DeleteAssetObject(t.Context(), missingAsset.StorageKey); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadAttachment(t.Context(), auth, missingAsset.ID, 0, 0); !hasCodedError(err, "ATTACHMENT_UNAVAILABLE") {
		t.Fatalf("missing storage read error = %v", err)
	}
	if _, err := svc.PrepareAttachmentDownload(t.Context(), auth, missingAsset.ID, 300); !hasCodedError(err, "ATTACHMENT_UNAVAILABLE") {
		t.Fatalf("missing storage download error = %v", err)
	}

	purgedMessage, err := svc.PostMessageWithAsset(t.Context(), auth, PostMessageWithAssetParams{ThreadID: thread.ID, Body: "purged", Bytes: []byte("purged"), FileName: "purged.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if marked, err := repo.MarkAssetPurged(t.Context(), purgedMessage.Assets[0].ID, auth.UserID); err != nil || !marked {
		t.Fatalf("mark purged=%v err=%v", marked, err)
	}
	if _, err := svc.ReadAttachment(t.Context(), auth, purgedMessage.Assets[0].ID, 0, 0); !hasCodedError(err, "ATTACHMENT_PURGED") {
		t.Fatalf("purged read error = %v", err)
	}
	if _, err := svc.PrepareAttachmentDownload(t.Context(), auth, purgedMessage.Assets[0].ID, 300); !hasCodedError(err, "ATTACHMENT_PURGED") {
		t.Fatalf("purged download error = %v", err)
	}
}

func TestPrepareAttachmentDownloadReturnsDirectSignedMetadata(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	auth := types.AuthContext{UserID: "usr_download", UserDisplayName: "Download User", SubjectType: types.AuthSubjectUserSession, SessionID: "sess_download", ActorName: "Web dashboard"}
	thread, err := svc.CreateThread(t.Context(), auth, "Download")
	if err != nil {
		t.Fatal(err)
	}
	message, err := svc.PostMessageWithAsset(t.Context(), auth, PostMessageWithAssetParams{ThreadID: thread.ID, Body: "file", Bytes: []byte("download me"), FileName: "download.txt"})
	if err != nil {
		t.Fatal(err)
	}
	download, err := svc.PrepareAttachmentDownload(t.Context(), auth, message.Assets[0].ID, 300)
	if err != nil {
		t.Fatal(err)
	}
	if download.Asset.ID != message.Assets[0].ID || download.Asset.FileName != "download.txt" || download.Asset.SizeBytes != int64(len("download me")) || download.ExpiresIn != 300 || !strings.HasPrefix(download.URL, "https://r2.test/") {
		t.Fatalf("download = %#v", download)
	}
	second, err := svc.PrepareAttachmentDownload(t.Context(), auth, message.Assets[0].ID, 300)
	if err != nil || second.Asset.ID != download.Asset.ID {
		t.Fatalf("second download=%#v err=%v", second, err)
	}
	if len(store.SignedURLCalls) != 2 {
		t.Fatalf("signed URL calls = %#v, want a fresh signing operation per download request", store.SignedURLCalls)
	}
}
