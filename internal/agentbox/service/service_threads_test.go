package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/types"
)

func TestServiceThreadAndMessageFlow(t *testing.T) {
	repo := &db.MemoryRepository{}
	store := &assets.FakeStore{}
	svc := New(repo, store)
	auth := testAuth("ten_a", "author")

	thread, err := svc.CreateThread(context.Background(), auth, "Thread flow")
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

	gotMessage, err := svc.GetMessage(context.Background(), auth, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotMessage.ID != message.ID || gotMessage.ThreadID != thread.ID || gotMessage.Body != "hello" {
		t.Fatalf("unexpected message: %#v", gotMessage)
	}

	_, err = svc.GetMessage(context.Background(), auth, "msg_missing")
	var missingMessage CodedError
	if !errors.As(err, &missingMessage) || missingMessage.Code != "MESSAGE_NOT_FOUND" {
		t.Fatalf("expected MESSAGE_NOT_FOUND, got %#v", err)
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
