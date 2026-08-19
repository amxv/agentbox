package db

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"agentbox/internal/agentbox/types"
)

func TestBootstrapOwnerIsUniqueIdempotentAndProtected(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	first, err := repository.BootstrapOwner(ctx, "owner@example.com", "Original Owner", "hash-one")
	if err != nil {
		t.Fatal(err)
	}
	if !first.IsOwner {
		t.Fatalf("unexpected owner: %#v", first)
	}

	second, err := repository.BootstrapOwner(ctx, "OWNER@example.com", "Updated Owner", "hash-two")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.DisplayName != "Updated Owner" || second.PasswordHash == nil || *second.PasswordHash != "hash-two" {
		t.Fatalf("owner bootstrap was not idempotent: first=%#v second=%#v", first, second)
	}

	if _, err := repository.BootstrapOwner(ctx, "other@example.com", "Other", "hash-three"); !errors.Is(err, ErrOwnerAlreadyExists) {
		t.Fatalf("second owner error = %v, want ErrOwnerAlreadyExists", err)
	}

	var ownerCount int
	if err := repository.pool.QueryRow(ctx, `select count(*) from users where is_owner`).Scan(&ownerCount); err != nil {
		t.Fatal(err)
	}
	if ownerCount != 1 {
		t.Fatalf("owner count = %d, want 1", ownerCount)
	}

	for name, statement := range map[string]string{
		"demote":  `update users set is_owner = false where id = $1`,
		"disable": `update users set disabled_at = now() where id = $1`,
		"delete":  `delete from users where id = $1`,
	} {
		if _, err := repository.pool.Exec(ctx, statement, first.ID); err == nil {
			t.Fatalf("%s owner mutation unexpectedly succeeded", name)
		}
	}

	if _, err := repository.CreateUser(ctx, "owner@example.com", "Duplicate", nil); err == nil {
		t.Fatal("deployment-global email uniqueness was not enforced")
	}
}

func TestUserOwnedCredentialsAreIsolatedRotatableAndDisableAware(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repository.CreateUser(ctx, "member@example.com", "Member", nil)
	if err != nil {
		t.Fatal(err)
	}

	ownerFirstSecret := "agb_owner_first"
	ownerFirst, err := repository.CreateAPIKey(ctx, owner.ID, "chatgpt", "chatgpt", hashSecret(ownerFirstSecret), "agb_owner_f", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	memberSecret := "agb_member"
	memberKey, err := repository.CreateAPIKey(ctx, member.ID, "chatgpt", "chatgpt", hashSecret(memberSecret), "agb_member", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	if ownerFirst.ID == memberKey.ID || ownerFirst.UserID != owner.ID || memberKey.UserID != member.ID {
		t.Fatalf("same-label credentials were not user-scoped: owner=%#v member=%#v", ownerFirst, memberKey)
	}

	ownerSecondSecret := "agb_owner_second"
	if ownerSecond, err := repository.CreateAPIKey(ctx, owner.ID, "CHATGPT", "chatgpt", hashSecret(ownerSecondSecret), "agb_owner_s", []string{"threads:read", "threads:write"}); !errors.Is(err, types.ErrCredentialLabelConflict) || ownerSecond.ID != "" {
		t.Fatalf("duplicate create replaced an active credential: first=%#v second=%#v err=%v", ownerFirst, ownerSecond, err)
	}
	if key, user, err := repository.FindAPIKeyBySecret(ctx, ownerFirstSecret); err != nil || key == nil || user == nil || key.ID != ownerFirst.ID {
		t.Fatalf("original credential stopped authenticating after duplicate create: key=%#v user=%#v err=%v", key, user, err)
	}
	if key, user, err := repository.FindAPIKeyBySecret(ctx, ownerSecondSecret); err != nil || key != nil || user != nil {
		t.Fatalf("rejected duplicate secret authenticated: key=%#v user=%#v err=%v", key, user, err)
	}

	ownerKeys, err := repository.ListAPIKeys(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	memberKeys, err := repository.ListAPIKeys(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerKeys) != 1 || ownerKeys[0].ID != ownerFirst.ID || len(memberKeys) != 1 || memberKeys[0].ID != memberKey.ID {
		t.Fatalf("credential lists crossed users: owner=%#v member=%#v", ownerKeys, memberKeys)
	}

	oldOwnerKey, oldOwnerUser, err := repository.FindAPIKeyBySecret(ctx, ownerFirstSecret)
	if err != nil {
		t.Fatal(err)
	}
	if oldOwnerKey == nil || oldOwnerUser == nil || oldOwnerKey.ID != ownerFirst.ID || oldOwnerUser.ID != owner.ID {
		t.Fatalf("original secret stopped authenticating after rejected duplicate create: key=%#v user=%#v", oldOwnerKey, oldOwnerUser)
	}
	activeOwnerKey, activeOwnerUser, err := repository.FindAPIKeyBySecret(ctx, ownerSecondSecret)
	if err != nil {
		t.Fatal(err)
	}
	if activeOwnerKey != nil || activeOwnerUser != nil {
		t.Fatalf("rejected duplicate secret unexpectedly resolved: key=%#v user=%#v", activeOwnerKey, activeOwnerUser)
	}
	activeMemberKey, activeMemberUser, err := repository.FindAPIKeyBySecret(ctx, memberSecret)
	if err != nil {
		t.Fatal(err)
	}
	if activeMemberKey == nil || activeMemberUser == nil || activeMemberUser.ID != member.ID {
		t.Fatalf("member secret did not resolve member: key=%#v user=%#v", activeMemberKey, activeMemberUser)
	}

	if removed, err := repository.RevokeAPIKeyForUserByID(ctx, owner.ID, ownerFirst.ID); err != nil || !removed {
		t.Fatalf("revoke owner credential: removed=%t err=%v", removed, err)
	}
	if key, user, err := repository.FindAPIKeyBySecret(ctx, ownerFirstSecret); err != nil || key != nil || user != nil {
		t.Fatalf("revoked owner credential authenticated: key=%#v user=%#v err=%v", key, user, err)
	}
	if key, user, err := repository.FindAPIKeyBySecret(ctx, memberSecret); err != nil || key == nil || user == nil {
		t.Fatalf("owner revoke affected member credential: key=%#v user=%#v err=%v", key, user, err)
	}

	if _, err := repository.pool.Exec(ctx, `update users set disabled_at = now() where id = $1`, member.ID); err != nil {
		t.Fatal(err)
	}
	if key, user, err := repository.FindAPIKeyBySecret(ctx, memberSecret); err != nil || key != nil || user != nil {
		t.Fatalf("disabled user credential authenticated: key=%#v user=%#v err=%v", key, user, err)
	}
}

func TestUserOwnedPrivateThreadAccessUsesOneIndexedBoundary(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner Person", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repository.CreateUser(ctx, "member@example.com", "Member Person", nil)
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, err := repository.CreateAPIKey(ctx, owner.ID, "chatgpt", "chatgpt", hashSecret("owner-secret"), "agb_owner", []string{"threads:read", "threads:write", "assets:read", "assets:write"})
	if err != nil {
		t.Fatal(err)
	}

	ownerBrowser := types.AuthContext{
		UserID:          owner.ID,
		UserDisplayName: owner.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		ActorName:       "Web dashboard",
	}
	ownerConnector := types.AuthContext{
		UserID:          owner.ID,
		UserDisplayName: owner.DisplayName,
		SubjectType:     types.AuthSubjectAPIKey,
		ActorName:       ownerKey.Name,
		KeyID:           ownerKey.ID,
	}
	memberBrowser := types.AuthContext{
		UserID:          member.ID,
		UserDisplayName: member.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		ActorName:       "Web dashboard",
	}

	ownerThread, err := repository.CreateThread(ctx, owner.ID, "private marker owner", ownerBrowser)
	if err != nil {
		t.Fatal(err)
	}
	memberThread, err := repository.CreateThread(ctx, member.ID, "private marker member", memberBrowser)
	if err != nil {
		t.Fatal(err)
	}
	if ownerThread.OwnerUserID != owner.ID || ownerThread.CreatedByUserDisplayName == nil || *ownerThread.CreatedByUserDisplayName != owner.DisplayName || ownerThread.CreatedByActorName == nil || *ownerThread.CreatedByActorName != "Web dashboard" {
		t.Fatalf("owner thread metadata = %#v", ownerThread)
	}

	ownerThreads, err := repository.ListThreads(ctx, owner.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	memberThreads, err := repository.ListThreads(ctx, member.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerThreads) != 1 || ownerThreads[0].ID != ownerThread.ID || len(memberThreads) != 1 || memberThreads[0].ID != memberThread.ID {
		t.Fatalf("private lists crossed users: owner=%#v member=%#v", ownerThreads, memberThreads)
	}

	ownerSearch, err := repository.SearchThreads(ctx, owner.ID, types.SearchThreadParams{Query: "private marker", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerSearch) != 1 || ownerSearch[0].ID != ownerThread.ID || ownerSearch[0].OwnerUserID != owner.ID {
		t.Fatalf("private search crossed users: %#v", ownerSearch)
	}
	if access, err := repository.ResolveThreadAccess(ctx, member.ID, ownerThread.ID); err != nil || access != nil {
		t.Fatalf("member resolved owner access: access=%#v err=%v", access, err)
	}
	if thread, err := repository.GetThread(ctx, member.ID, ownerThread.ID); err != nil || thread != nil {
		t.Fatalf("member read owner thread: thread=%#v err=%v", thread, err)
	}
	if _, err := repository.PostMessage(ctx, member.ID, ownerThread.ID, memberBrowser, "blocked", nil, nil); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("member posted to owner thread: %v", err)
	}

	textType := "text/plain"
	message, err := repository.PostMessage(ctx, owner.ID, ownerThread.ID, ownerConnector, "connector contribution", nil, []types.NewAsset{{
		StorageKey: "agentbox/" + owner.ID + "/" + ownerThread.ID + "/message/existing.txt",
		FileName:   "existing.txt",
		MimeType:   &textType,
		SizeBytes:  8,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if message.CreatedByUserID == nil || *message.CreatedByUserID != owner.ID || message.CreatedByKeyID == nil || *message.CreatedByKeyID != ownerKey.ID || message.CreatedByUserDisplayName == nil || *message.CreatedByUserDisplayName != owner.DisplayName || message.CreatedByActorName == nil || *message.CreatedByActorName != ownerKey.Name || len(message.Assets) != 1 {
		t.Fatalf("connector attribution = %#v", message)
	}
	if message.Assets[0].DownloadURL != nil {
		t.Fatalf("new private asset exposed a direct URL: %#v", message.Assets[0])
	}
	if asset, err := repository.GetAsset(ctx, member.ID, message.Assets[0].ID); err != nil || asset != nil {
		t.Fatalf("member read owner asset: asset=%#v err=%v", asset, err)
	}
	if asset, err := repository.GetAsset(ctx, owner.ID, message.Assets[0].ID); err != nil || asset == nil || asset.StorageKey != message.Assets[0].StorageKey {
		t.Fatalf("owner asset lookup failed: asset=%#v err=%v", asset, err)
	}

	expiresAt := time.Now().UTC().Add(15 * time.Minute)
	upload := types.PendingUpload{
		ID:                       "upl_owner_private",
		ThreadID:                 ownerThread.ID,
		StorageKey:               "agentbox/" + owner.ID + "/" + ownerThread.ID + "/upl_owner_private/file.txt",
		FileName:                 "file.txt",
		MimeType:                 &textType,
		SizeBytes:                4,
		ExpiresAt:                isoMillis(expiresAt),
		CreatedBy:                ownerKey.Name,
		CreatedByUserID:          &owner.ID,
		CreatedByKeyID:           &ownerKey.ID,
		CreatedByUserDisplayName: &owner.DisplayName,
		CreatedByActorName:       &ownerKey.Name,
	}
	createdUpload, err := repository.CreatePendingUpload(ctx, owner.ID, upload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(createdUpload.StorageKey, "agentbox/"+owner.ID+"/"+ownerThread.ID+"/") {
		t.Fatalf("pending upload metadata = %#v", createdUpload)
	}
	if _, err := repository.CreatePendingUpload(ctx, member.ID, types.PendingUpload{ID: "upl_cross_user", ThreadID: ownerThread.ID, StorageKey: "blocked", FileName: "blocked.txt", ExpiresAt: isoMillis(expiresAt), CreatedBy: memberBrowser.ActorName}); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("member created upload for owner thread: %v", err)
	}
	ownedUploads, err := repository.GetPendingUploads(ctx, owner.ID, ownerThread.ID, []string{upload.ID}, ownerConnector)
	if err != nil || len(ownedUploads) != 1 || ownedUploads[0].ID != upload.ID {
		t.Fatalf("owner pending upload lookup = %#v err=%v", ownedUploads, err)
	}
	wrongActor := ownerConnector
	wrongActor.KeyID = "key_other"
	if wrongUploads, err := repository.GetPendingUploads(ctx, owner.ID, ownerThread.ID, []string{upload.ID}, wrongActor); err != nil || len(wrongUploads) != 0 {
		t.Fatalf("pending upload crossed actors: uploads=%#v err=%v", wrongUploads, err)
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `set local enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	planRows, err := tx.Query(ctx, `
explain (format text)
select id, owner_user_id, title, updated_at
from threads
where owner_user_id = $1
order by updated_at desc
limit 50
`, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer planRows.Close()
	plan := strings.Builder{}
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := planRows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "threads_owner_updated_idx") {
		t.Fatalf("private list plan did not use owner index:\n%s", plan.String())
	}
}

func TestTeamSharedThreadAccessIsImmediateCompleteAndIndexed(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err := repository.BootstrapOwner(ctx, "share-owner@example.com", "Share Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repository.CreateUser(ctx, "share-member@example.com", "Share Member", nil)
	if err != nil {
		t.Fatal(err)
	}
	secondMember, err := repository.CreateUser(ctx, "share-second@example.com", "Second Member", nil)
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := repository.CreateUser(ctx, "share-outsider@example.com", "Share Outsider", nil)
	if err != nil {
		t.Fatal(err)
	}
	teamA, err := repository.CreateTeam(ctx, "team-a", "Team A")
	if err != nil {
		t.Fatal(err)
	}
	teamB, err := repository.CreateTeam(ctx, "team-b", "Team B")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AddTeamMember(ctx, teamA.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AddTeamMember(ctx, teamB.ID, owner.ID); err != nil {
		t.Fatal(err)
	}

	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard"}
	memberKey, err := repository.CreateAPIKey(ctx, member.ID, "member-agent", "test", "member-key-hash", "member-key", []string{"threads:read", "threads:write", "assets:read", "assets:write"})
	if err != nil {
		t.Fatal(err)
	}
	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectAPIKey, ActorName: "member-agent", KeyID: memberKey.ID}
	thread, err := repository.CreateThread(ctx, owner.ID, "team shared indexed marker", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}

	if visibility, err := repository.GetThreadVisibility(ctx, member.ID, thread.ID); err != nil || visibility != nil {
		t.Fatalf("private visibility leaked before share: visibility=%#v err=%v", visibility, err)
	}
	shared, err := setThreadVisibilityForTest(ctx, repository, owner.ID, thread.ID, []string{teamA.ID, teamB.ID, teamA.ID})
	if err != nil || len(shared.SharedTeams) != 2 || shared.OwnerUserID != owner.ID {
		t.Fatalf("initial visibility=%#v err=%v", shared, err)
	}
	var shareCount int
	if err := repository.pool.QueryRow(ctx, `select count(*) from thread_team_shares where thread_id = $1`, thread.ID).Scan(&shareCount); err != nil {
		t.Fatal(err)
	}
	if shareCount != 2 {
		t.Fatalf("duplicate team share count=%d", shareCount)
	}

	if access, err := repository.ResolveThreadAccess(ctx, member.ID, thread.ID); err != nil || access != nil {
		t.Fatalf("share without membership granted access: access=%#v err=%v", access, err)
	}
	if _, err := repository.AddTeamMember(ctx, teamA.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	access, err := repository.ResolveThreadAccess(ctx, member.ID, thread.ID)
	if err != nil || access == nil || access.IsOwner || len(access.MatchedTeamIDs) != 1 || access.MatchedTeamIDs[0] != teamA.ID {
		t.Fatalf("team access=%#v err=%v", access, err)
	}

	threads, err := repository.ListThreads(ctx, member.ID, 50)
	if err != nil || len(threads) != 1 || threads[0].ID != thread.ID || !threads[0].VisibilitySummary.SharedWithMe || len(threads[0].VisibilitySummary.SharedTeams) != 2 || len(threads[0].VisibilitySummary.MatchedTeams) != 1 {
		t.Fatalf("team list=%#v err=%v", threads, err)
	}
	sharedThreads, err := repository.ListThreadsFiltered(ctx, member.ID, types.ThreadListParams{Limit: 50, Filter: types.ThreadFilterShared})
	if err != nil || len(sharedThreads) != 1 || sharedThreads[0].ID != thread.ID {
		t.Fatalf("shared filter=%#v err=%v", sharedThreads, err)
	}
	teamAThreads, err := repository.ListThreadsFiltered(ctx, member.ID, types.ThreadListParams{Limit: 50, Filter: types.ThreadFilterTeam, TeamRef: teamA.Slug})
	if err != nil || len(teamAThreads) != 1 || teamAThreads[0].ID != thread.ID {
		t.Fatalf("team A filter=%#v err=%v", teamAThreads, err)
	}
	teamBThreads, err := repository.ListThreadsFiltered(ctx, member.ID, types.ThreadListParams{Limit: 50, Filter: types.ThreadFilterTeam, TeamRef: teamB.ID})
	if err != nil || len(teamBThreads) != 0 {
		t.Fatalf("team B filter leaked non-membership=%#v err=%v", teamBThreads, err)
	}
	privateThreads, err := repository.ListThreadsFiltered(ctx, member.ID, types.ThreadListParams{Limit: 50, Filter: types.ThreadFilterPrivate})
	if err != nil || len(privateThreads) != 0 {
		t.Fatalf("private filter=%#v err=%v", privateThreads, err)
	}
	search, err := repository.SearchThreads(ctx, member.ID, types.SearchThreadParams{Query: "indexed marker", Limit: 20, Filter: types.ThreadFilterTeam, TeamRef: teamA.ID})
	if err != nil || len(search) != 1 || search[0].ID != thread.ID || !search[0].VisibilitySummary.SharedWithMe {
		t.Fatalf("team search=%#v err=%v", search, err)
	}
	detail, err := repository.GetThread(ctx, member.ID, thread.ID)
	if err != nil || detail == nil || len(detail.Visibility.SharedTeams) != 2 || !detail.VisibilitySummary.SharedWithMe {
		t.Fatalf("team detail=%#v err=%v", detail, err)
	}

	publicToken := "agpub_filter_integration"
	publish := true
	if _, err := repository.ManageThreadVisibility(ctx, member.ID, thread.ID, types.ManageThreadVisibilityInput{Public: &publish, PublicToken: publicToken, PublicTokenHash: hashSecret(publicToken), PublicTokenPrefix: "agpub_filter"}); err != nil {
		t.Fatal(err)
	}
	publicThreads, err := repository.ListThreadsFiltered(ctx, member.ID, types.ThreadListParams{Limit: 50, Filter: types.ThreadFilterPublic})
	if err != nil || len(publicThreads) != 1 || publicThreads[0].ID != thread.ID || !publicThreads[0].VisibilitySummary.Public {
		t.Fatalf("public filter=%#v err=%v", publicThreads, err)
	}

	planTx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer planTx.Rollback(ctx)
	if _, err := planTx.Exec(ctx, `set local enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	filterPlanRows, err := planTx.Query(ctx, `explain (costs off)
select t.id
from threads t
where `+normalThreadAccessPredicate+`
  and `+threadFilterPredicate("$2", "$3")+`
order by t.updated_at desc, t.id
limit $4
`, member.ID, types.ThreadFilterTeam, teamA.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	filterPlan := strings.Builder{}
	for filterPlanRows.Next() {
		var line string
		if err := filterPlanRows.Scan(&line); err != nil {
			filterPlanRows.Close()
			t.Fatal(err)
		}
		filterPlan.WriteString(line)
		filterPlan.WriteByte('\n')
	}
	filterPlanRows.Close()
	if err := filterPlanRows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filterPlan.String(), "thread_team_shares_team_thread_idx") || !strings.Contains(filterPlan.String(), "team_memberships_user_team_idx") {
		t.Fatalf("team filter plan did not use membership/share indexes:\n%s", filterPlan.String())
	}

	textType := "text/plain"
	message, err := repository.PostMessage(ctx, member.ID, thread.ID, memberAuth, "team participant message", nil, []types.NewAsset{{
		StorageKey: "agentbox/" + member.ID + "/" + thread.ID + "/message/team.txt",
		FileName:   "team.txt",
		MimeType:   &textType,
		SizeBytes:  4,
	}})
	if err != nil || len(message.Assets) != 1 || message.CreatedByActorName == nil || *message.CreatedByActorName != memberAuth.ActorName {
		t.Fatalf("team post=%#v err=%v", message, err)
	}
	if asset, err := repository.GetAsset(ctx, member.ID, message.Assets[0].ID); err != nil || asset == nil || asset.ID != message.Assets[0].ID {
		t.Fatalf("team asset lookup=%#v err=%v", asset, err)
	}
	if asset, err := repository.GetAsset(ctx, outsider.ID, message.Assets[0].ID); err != nil || asset != nil {
		t.Fatalf("outsider asset lookup=%#v err=%v", asset, err)
	}

	expiresAt := time.Now().UTC().Add(15 * time.Minute)
	upload := types.PendingUpload{
		ID:                       "upl_team_shared",
		ThreadID:                 thread.ID,
		StorageKey:               "agentbox/" + member.ID + "/" + thread.ID + "/upl_team_shared/file.txt",
		FileName:                 "file.txt",
		MimeType:                 &textType,
		SizeBytes:                4,
		ExpiresAt:                isoMillis(expiresAt),
		CreatedBy:                memberAuth.ActorName,
		CreatedByUserID:          &member.ID,
		CreatedByKeyID:           &memberAuth.KeyID,
		CreatedByUserDisplayName: &member.DisplayName,
		CreatedByActorName:       &memberAuth.ActorName,
	}
	createdUpload, err := repository.CreatePendingUpload(ctx, member.ID, upload)
	if err != nil || createdUpload.ID != upload.ID {
		t.Fatalf("team upload creation=%#v err=%v", createdUpload, err)
	}
	pending, err := repository.GetPendingUploads(ctx, member.ID, thread.ID, []string{upload.ID}, memberAuth)
	if err != nil || len(pending) != 1 || pending[0].ID != upload.ID {
		t.Fatalf("team pending upload=%#v err=%v", pending, err)
	}
	if err := repository.MarkPendingUploadsConsumed(ctx, member.ID, thread.ID, []string{upload.ID}, memberAuth); err != nil {
		t.Fatal(err)
	}

	if _, err := repository.AddTeamMember(ctx, teamB.ID, secondMember.ID); err != nil {
		t.Fatal(err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, secondMember.ID, thread.ID); err != nil || access == nil || len(access.MatchedTeamIDs) != 1 || access.MatchedTeamIDs[0] != teamB.ID {
		t.Fatalf("second team access=%#v err=%v", access, err)
	}

	participantVisibility, err := setThreadVisibilityForTest(ctx, repository, member.ID, thread.ID, []string{teamA.ID})
	if err != nil || len(participantVisibility.SharedTeams) != 1 || participantVisibility.SharedTeams[0].ID != teamA.ID {
		t.Fatalf("participant visibility mutation=%#v err=%v", participantVisibility, err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, secondMember.ID, thread.ID); err != nil || access != nil {
		t.Fatalf("removed team retained access: access=%#v err=%v", access, err)
	}

	if removed, err := repository.RemoveTeamMember(ctx, teamA.ID, member.ID); err != nil || !removed {
		t.Fatalf("membership removal removed=%t err=%v", removed, err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, member.ID, thread.ID); err != nil || access != nil {
		t.Fatalf("removed member retained access: access=%#v err=%v", access, err)
	}
	if _, err := repository.PostMessage(ctx, member.ID, thread.ID, memberAuth, "blocked", nil, nil); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("removed member posted: %v", err)
	}
	if _, err := repository.CreatePendingUpload(ctx, member.ID, types.PendingUpload{ID: "upl_blocked", ThreadID: thread.ID, StorageKey: "blocked", FileName: "blocked.txt", ExpiresAt: isoMillis(expiresAt), CreatedBy: memberAuth.ActorName}); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("removed member created upload: %v", err)
	}
	if asset, err := repository.GetAsset(ctx, member.ID, message.Assets[0].ID); err != nil || asset != nil {
		t.Fatalf("removed member retained asset access: asset=%#v err=%v", asset, err)
	}

	if _, err := repository.AddTeamMember(ctx, teamA.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, member.ID, thread.ID); err != nil || access == nil {
		t.Fatalf("re-added member did not regain access: access=%#v err=%v", access, err)
	}
	privateAgain, err := setThreadVisibilityForTest(ctx, repository, member.ID, thread.ID, nil)
	if err != nil || len(privateAgain.SharedTeams) != 0 {
		t.Fatalf("participant made private visibility=%#v err=%v", privateAgain, err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, member.ID, thread.ID); err != nil || access != nil {
		t.Fatalf("participant retained access after making private: access=%#v err=%v", access, err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, owner.ID, thread.ID); err != nil || access == nil || !access.IsOwner {
		t.Fatalf("owner lost access after private mutation: access=%#v err=%v", access, err)
	}

	if _, err := setThreadVisibilityForTest(ctx, repository, owner.ID, thread.ID, []string{"team_missing"}); !errors.Is(err, types.ErrThreadVisibilityTeamUnavailable) {
		t.Fatalf("missing share team error=%v", err)
	}

	if _, err := setThreadVisibilityForTest(ctx, repository, owner.ID, thread.ID, []string{teamA.ID}); err != nil {
		t.Fatal(err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `set local enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	planRows, err := tx.Query(ctx, `
explain (format text)
select t.id
from threads t
where `+normalThreadAccessPredicate+`
order by t.updated_at desc, t.id
limit 50
`, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer planRows.Close()
	plan := strings.Builder{}
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := planRows.Err(); err != nil {
		t.Fatal(err)
	}
	shareIndexUsed := strings.Contains(plan.String(), "thread_team_shares_team_thread_idx") || strings.Contains(plan.String(), "thread_team_shares_pkey")
	threadOrderIndexUsed := strings.Contains(plan.String(), "threads_updated_id_idx") || strings.Contains(plan.String(), "threads_updated_at_idx")
	if !shareIndexUsed || !strings.Contains(plan.String(), "team_memberships_user_team_idx") || !threadOrderIndexUsed {
		t.Fatalf("team access plan missed indexes:\n%s", plan.String())
	}
}

func TestThreadPublicLinksAreSingleRedisplayableRevocableAndIndexed(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "public-owner@example.com", "Public Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repository.CreateUser(ctx, "public-member@example.com", "Public Member", nil)
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := repository.CreateUser(ctx, "public-outsider@example.com", "Public Outsider", nil)
	if err != nil {
		t.Fatal(err)
	}
	team, err := repository.CreateTeam(ctx, "public-team", "Public Team")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AddTeamMember(ctx, team.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AddTeamMember(ctx, team.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard"}
	thread, err := repository.CreateThread(ctx, owner.ID, "PostgreSQL public marker", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(ctx, repository, owner.ID, thread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	mimeType := "text/plain"
	message, err := repository.PostMessage(ctx, owner.ID, thread.ID, ownerAuth, "PostgreSQL public body", nil, []types.NewAsset{{StorageKey: "agentbox/" + owner.ID + "/" + thread.ID + "/postgres-public.txt", FileName: "postgres-public.txt", MimeType: &mimeType, SizeBytes: 8}})
	if err != nil || len(message.Assets) != 1 {
		t.Fatalf("fixture=%#v err=%v", message, err)
	}
	otherThread, err := repository.CreateThread(ctx, owner.ID, "PostgreSQL other marker", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	otherMessage, err := repository.PostMessage(ctx, owner.ID, otherThread.ID, ownerAuth, "Other public body", nil, []types.NewAsset{{StorageKey: "agentbox/" + owner.ID + "/" + otherThread.ID + "/postgres-other.txt", FileName: "postgres-other.txt", MimeType: &mimeType, SizeBytes: 5}})
	if err != nil || len(otherMessage.Assets) != 1 {
		t.Fatalf("other fixture=%#v err=%v", otherMessage, err)
	}

	initial, err := repository.ManageThreadVisibility(ctx, member.ID, thread.ID, types.ManageThreadVisibilityInput{})
	if err != nil || initial.Public || initial.PublicLink != nil {
		t.Fatalf("initial=%#v err=%v", initial, err)
	}
	publish := true
	firstToken := "agpub_postgres_first"
	firstHash := hashSecret(firstToken)
	created, err := repository.ManageThreadVisibility(ctx, member.ID, thread.ID, types.ManageThreadVisibilityInput{Public: &publish, PublicToken: firstToken, PublicTokenHash: firstHash, PublicTokenPrefix: "agpub_postgr"})
	if err != nil || created.PublicLink == nil || created.PublicLink.Token != firstToken || created.PublicLink.CreatedByUserID == nil || *created.PublicLink.CreatedByUserID != member.ID {
		t.Fatalf("created=%#v err=%v", created, err)
	}
	var rowCount int
	var storedToken, storedHash string
	if err := repository.pool.QueryRow(ctx, `select count(*), max(token_value), max(token_hash) from thread_public_links where thread_id = $1`, thread.ID).Scan(&rowCount, &storedToken, &storedHash); err != nil {
		t.Fatal(err)
	}
	if rowCount != 1 || storedToken != firstToken || storedHash != firstHash || storedHash == firstToken {
		t.Fatalf("stored row count=%d token=%q hash=%q", rowCount, storedToken, storedHash)
	}
	idempotent, err := repository.ManageThreadVisibility(ctx, owner.ID, thread.ID, types.ManageThreadVisibilityInput{Public: &publish, PublicToken: "ignored", PublicTokenHash: hashSecret("ignored"), PublicTokenPrefix: "ignored"})
	if err != nil || idempotent.PublicLink == nil || idempotent.PublicLink.Token != firstToken {
		t.Fatalf("idempotent publish=%#v err=%v", idempotent, err)
	}
	if _, err := repository.ManageThreadVisibility(ctx, outsider.ID, thread.ID, types.ManageThreadVisibilityInput{RegeneratePublicLink: true, PublicToken: "outsider", PublicTokenHash: hashSecret("outsider"), PublicTokenPrefix: "outsider"}); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("outsider mutation=%v", err)
	}

	threadLease, err := repository.AcquirePublicThreadLease(ctx, firstHash)
	if err != nil || threadLease == nil || threadLease.Thread().ID != thread.ID || len(threadLease.Thread().Messages) != 1 {
		t.Fatalf("thread lease=%#v err=%v", threadLease, err)
	}
	if err := threadLease.Close(ctx); err != nil {
		t.Fatal(err)
	}
	assetLease, err := repository.AcquirePublicAssetSigningLease(ctx, firstHash, message.Assets[0].ID)
	if err != nil || assetLease == nil || assetLease.Asset().ID != message.Assets[0].ID {
		t.Fatalf("asset lease=%#v err=%v", assetLease, err)
	}
	if err := assetLease.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if lease, err := repository.AcquirePublicAssetSigningLease(ctx, firstHash, otherMessage.Assets[0].ID); err != nil || lease != nil {
		t.Fatalf("cross-thread lease=%#v err=%v", lease, err)
	}

	secondToken := "agpub_postgres_rotated"
	secondHash := hashSecret(secondToken)
	rotated, err := repository.ManageThreadVisibility(ctx, owner.ID, thread.ID, types.ManageThreadVisibilityInput{RegeneratePublicLink: true, PublicToken: secondToken, PublicTokenHash: secondHash, PublicTokenPrefix: "agpub_rotate"})
	if err != nil || rotated.PublicLink == nil || rotated.PublicLink.Token != secondToken {
		t.Fatalf("rotated=%#v err=%v", rotated, err)
	}
	if lease, err := repository.AcquirePublicThreadLease(ctx, firstHash); err != nil || lease != nil {
		t.Fatalf("old token lease=%#v err=%v", lease, err)
	}
	if lease, err := repository.AcquirePublicThreadLease(ctx, secondHash); err != nil || lease == nil {
		t.Fatalf("new token lease=%#v err=%v", lease, err)
	} else if err := lease.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repository.pool.QueryRow(ctx, `select count(*) from thread_public_links where thread_id = $1`, thread.ID).Scan(&rowCount); err != nil {
		t.Fatal(err)
	}
	if rowCount != 1 {
		t.Fatalf("rotation created %d rows", rowCount)
	}

	unpublish := false
	unpublished, err := repository.ManageThreadVisibility(ctx, member.ID, thread.ID, types.ManageThreadVisibilityInput{Public: &unpublish})
	if err != nil || unpublished.Public || unpublished.PublicLink != nil {
		t.Fatalf("unpublished=%#v err=%v", unpublished, err)
	}
	unpublishedAgain, err := repository.ManageThreadVisibility(ctx, member.ID, thread.ID, types.ManageThreadVisibilityInput{Public: &unpublish})
	if err != nil || unpublishedAgain.Public {
		t.Fatalf("idempotent unpublish=%#v err=%v", unpublishedAgain, err)
	}
	if lease, err := repository.AcquirePublicThreadLease(ctx, secondHash); err != nil || lease != nil {
		t.Fatalf("revoked token lease=%#v err=%v", lease, err)
	}
	thirdToken := "agpub_postgres_recreated"
	thirdHash := hashSecret(thirdToken)
	if _, err := repository.ManageThreadVisibility(ctx, owner.ID, thread.ID, types.ManageThreadVisibilityInput{Public: &publish, PublicToken: thirdToken, PublicTokenHash: thirdHash, PublicTokenPrefix: "agpub_recre"}); err != nil {
		t.Fatal(err)
	}

	concurrentThread, err := repository.CreateThread(ctx, owner.ID, "Concurrent public link", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	type createResult struct {
		state types.ManagedThreadVisibility
		err   error
	}
	results := make(chan createResult, 2)
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		index := index
		go func() {
			<-start
			token := fmt.Sprintf("concurrent-public-%d", index)
			state, err := repository.ManageThreadVisibility(context.Background(), owner.ID, concurrentThread.ID, types.ManageThreadVisibilityInput{Public: &publish, PublicToken: token, PublicTokenHash: hashSecret(token), PublicTokenPrefix: fmt.Sprintf("agpub_%d", index)})
			results <- createResult{state: state, err: err}
		}()
	}
	close(start)
	returnedTokens := map[string]bool{}
	for index := 0; index < 2; index++ {
		result := <-results
		if result.err != nil || result.state.PublicLink == nil {
			t.Fatalf("concurrent result=%#v err=%v", result.state, result.err)
		}
		returnedTokens[result.state.PublicLink.Token] = true
	}
	if len(returnedTokens) != 1 {
		t.Fatalf("concurrent publish returned tokens=%#v", returnedTokens)
	}
	if err := repository.pool.QueryRow(ctx, `select count(*) from thread_public_links where thread_id = $1`, concurrentThread.ID).Scan(&rowCount); err != nil {
		t.Fatal(err)
	}
	if rowCount != 1 {
		t.Fatalf("concurrent publish created %d rows", rowCount)
	}

	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `set local enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	planRows, err := tx.Query(ctx, `explain (format text) select t.id from thread_public_links link join threads t on t.id = link.thread_id where link.token_hash = $1 and link.revoked_at is null`, thirdHash)
	if err != nil {
		t.Fatal(err)
	}
	defer planRows.Close()
	plan := strings.Builder{}
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := planRows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "thread_public_links_active_token_idx") && !strings.Contains(plan.String(), "thread_public_links_token_hash_unique") {
		t.Fatalf("public token lookup plan missed indexes:\n%s", plan.String())
	}
}

func TestManageThreadVisibilityIsAtomicMembershipBoundAndSelfRevoking(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "visibility-owner@example.com", "Visibility Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repository.CreateUser(ctx, "visibility-member@example.com", "Visibility Member", nil)
	if err != nil {
		t.Fatal(err)
	}
	teamA, err := repository.CreateTeam(ctx, "visibility-a", "Visibility A")
	if err != nil {
		t.Fatal(err)
	}
	teamB, err := repository.CreateTeam(ctx, "visibility-b", "Visibility B")
	if err != nil {
		t.Fatal(err)
	}
	unavailable, err := repository.CreateTeam(ctx, "visibility-unavailable", "Visibility Unavailable")
	if err != nil {
		t.Fatal(err)
	}
	for _, teamID := range []string{teamA.ID, teamB.ID} {
		if _, err := repository.AddTeamMember(ctx, teamID, owner.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.AddTeamMember(ctx, teamB.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard"}
	thread, err := repository.CreateThread(ctx, owner.ID, "Unified visibility marker", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(ctx, repository, owner.ID, thread.ID, []string{teamA.ID}); err != nil {
		t.Fatal(err)
	}

	publish := true
	firstToken := "agpub_unified_first"
	state, err := repository.ManageThreadVisibility(ctx, owner.ID, thread.ID, types.ManageThreadVisibilityInput{
		AddTeams:          []string{teamB.Slug, teamB.ID},
		RemoveTeams:       []string{teamA.ID},
		Public:            &publish,
		PublicToken:       firstToken,
		PublicTokenHash:   hashSecret(firstToken),
		PublicTokenPrefix: "agpub_unifie",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.SharedTeams) != 1 || state.SharedTeams[0].ID != teamB.ID || !state.Public || state.PublicLink == nil || state.PublicLink.Token != firstToken || len(state.AvailableTeams) != 2 {
		t.Fatalf("combined visibility state=%#v", state)
	}
	var storedToken string
	var storedHash string
	if err := repository.pool.QueryRow(ctx, `select token_value, token_hash from thread_public_links where thread_id = $1 and revoked_at is null`, thread.ID).Scan(&storedToken, &storedHash); err != nil {
		t.Fatal(err)
	}
	if storedToken != firstToken || storedHash != hashSecret(firstToken) {
		t.Fatalf("stored public token=%q hash=%q", storedToken, storedHash)
	}

	secondToken := "agpub_unified_unused"
	repeated, err := repository.ManageThreadVisibility(ctx, owner.ID, thread.ID, types.ManageThreadVisibilityInput{
		AddTeams:          []string{teamB.Slug},
		RemoveTeams:       []string{teamA.Slug},
		Public:            &publish,
		PublicToken:       secondToken,
		PublicTokenHash:   hashSecret(secondToken),
		PublicTokenPrefix: "agpub_unifie",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.PublicLink == nil || repeated.PublicLink.Token != firstToken || len(repeated.SharedTeams) != 1 || repeated.SharedTeams[0].ID != teamB.ID {
		t.Fatalf("idempotent visibility state=%#v", repeated)
	}

	unpublish := false
	if _, err := repository.ManageThreadVisibility(ctx, owner.ID, thread.ID, types.ManageThreadVisibilityInput{
		AddTeams: []string{unavailable.Slug},
		Public:   &unpublish,
	}); !errors.Is(err, types.ErrThreadVisibilityTeamUnavailable) {
		t.Fatalf("unavailable team error=%v", err)
	}
	unchanged, err := repository.ManageThreadVisibility(ctx, owner.ID, thread.ID, types.ManageThreadVisibilityInput{})
	if err != nil || len(unchanged.SharedTeams) != 1 || unchanged.SharedTeams[0].ID != teamB.ID || !unchanged.Public || unchanged.PublicLink == nil || unchanged.PublicLink.Token != firstToken {
		t.Fatalf("failed mutation was not atomic state=%#v err=%v", unchanged, err)
	}

	selfRevoked, err := repository.ManageThreadVisibility(ctx, member.ID, thread.ID, types.ManageThreadVisibilityInput{
		RemoveTeams: []string{teamB.Slug},
		Public:      &unpublish,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(selfRevoked.SharedTeams) != 0 || selfRevoked.Public || selfRevoked.PublicLink != nil {
		t.Fatalf("self-revocation response=%#v", selfRevoked)
	}
	if access, err := repository.ResolveThreadAccess(ctx, member.ID, thread.ID); err != nil || access != nil {
		t.Fatalf("member retained access after self-revocation access=%#v err=%v", access, err)
	}
	if lease, err := repository.AcquirePublicThreadLease(ctx, hashSecret(firstToken)); err != nil || lease != nil {
		t.Fatalf("unpublished token remained active lease=%#v err=%v", lease, err)
	}
}

func TestAuthorizationLeasesSerializeRevocationMembershipRemovalAndReenable(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "lease-owner@example.com", "Lease Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repository.CreateUser(ctx, "lease-member@example.com", "Lease Member", nil)
	if err != nil {
		t.Fatal(err)
	}
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_lease_owner", ActorName: "Web dashboard"}
	thread, err := repository.CreateThread(ctx, owner.ID, "Lease thread", ownerAuth)
	if err != nil {
		t.Fatal(err)
	}
	message, err := repository.PostMessage(ctx, owner.ID, thread.ID, ownerAuth, "asset", nil, []types.NewAsset{{StorageKey: "agentbox/lease.txt", FileName: "lease.txt", SizeBytes: 5}})
	if err != nil {
		t.Fatal(err)
	}
	publish := true
	token := "agpub_lease"
	if _, err := repository.ManageThreadVisibility(ctx, owner.ID, thread.ID, types.ManageThreadVisibilityInput{Public: &publish, PublicToken: token, PublicTokenHash: hashSecret(token), PublicTokenPrefix: "agpub_lease"}); err != nil {
		t.Fatal(err)
	}
	publicLease, err := repository.AcquirePublicAssetSigningLease(ctx, hashSecret(token), message.Assets[0].ID)
	if err != nil || publicLease == nil {
		t.Fatalf("public lease=%#v err=%v", publicLease, err)
	}
	unpublishDone := make(chan error, 1)
	go func() {
		unpublish := false
		_, err := repository.ManageThreadVisibility(context.Background(), owner.ID, thread.ID, types.ManageThreadVisibilityInput{Public: &unpublish})
		unpublishDone <- err
	}()
	assertNotCompleted(t, unpublishDone, "public revocation committed while signing lease was active")
	if err := publicLease.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := waitForError(t, unpublishDone); err != nil {
		t.Fatal(err)
	}

	team, err := repository.CreateTeam(ctx, "lease-team", "Lease Team")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{owner.ID, member.ID} {
		if _, err := repository.AddTeamMember(ctx, team.ID, userID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := setThreadVisibilityForTest(ctx, repository, owner.ID, thread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockThreadAccessForMutation(ctx, tx, member.ID, thread.ID); err != nil {
		t.Fatal(err)
	}
	removeDone := make(chan error, 1)
	go func() {
		_, err := repository.RemoveTeamMember(context.Background(), team.ID, member.ID)
		removeDone <- err
	}()
	assertNotCompleted(t, removeDone, "membership removal committed while participant mutation lock was active")
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := waitForError(t, removeDone); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PostMessage(ctx, member.ID, thread.ID, types.AuthContext{UserID: member.ID, ActorName: "member"}, "denied", nil, nil); !errors.Is(err, types.ErrThreadNotFound) {
		t.Fatalf("removed member post error=%v", err)
	}

	if _, err := repository.SetUserDisabled(ctx, member.ID, true); err != nil {
		t.Fatal(err)
	}
	purgeLease, err := repository.AcquireAttachmentPurgeLease(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	reenableDone := make(chan error, 1)
	go func() {
		_, err := repository.SetUserDisabled(context.Background(), member.ID, false)
		reenableDone <- err
	}()
	assertNotCompleted(t, reenableDone, "user re-enable committed while purge lease was active")
	if err := purgeLease.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := waitForError(t, reenableDone); err != nil {
		t.Fatal(err)
	}
}

func TestThreadContinuationPagesTraverseEveryEffectiveAccessFilter(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "page-owner@example.com", "Page Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repository.CreateUser(ctx, "page-member@example.com", "Page Member", nil)
	if err != nil {
		t.Fatal(err)
	}
	team, err := repository.CreateTeam(ctx, "page-team", "Page Team")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{owner.ID, member.ID} {
		if _, err := repository.AddTeamMember(ctx, team.ID, userID); err != nil {
			t.Fatal(err)
		}
	}
	ownerAuth := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard"}
	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, SubjectType: types.AuthSubjectUserSession, ActorName: "Web dashboard"}

	createdIDs := []string{}
	for _, suffix := range []string{"private-a", "private-b", "private-c"} {
		thread, err := repository.CreateThread(ctx, member.ID, "pagination marker "+suffix, memberAuth)
		if err != nil {
			t.Fatal(err)
		}
		createdIDs = append(createdIDs, thread.ID)
	}
	for _, suffix := range []string{"shared-a", "shared-b", "shared-c"} {
		thread, err := repository.CreateThread(ctx, owner.ID, "pagination marker "+suffix, ownerAuth)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := setThreadVisibilityForTest(ctx, repository, owner.ID, thread.ID, []string{team.ID}); err != nil {
			t.Fatal(err)
		}
		createdIDs = append(createdIDs, thread.ID)
	}
	for _, suffix := range []string{"public-a", "public-b", "public-c"} {
		thread, err := repository.CreateThread(ctx, member.ID, "pagination marker "+suffix, memberAuth)
		if err != nil {
			t.Fatal(err)
		}
		publish := true
		if _, err := repository.ManageThreadVisibility(ctx, member.ID, thread.ID, types.ManageThreadVisibilityInput{
			Public:            &publish,
			PublicToken:       "agpub_" + thread.ID,
			PublicTokenHash:   "hash_" + thread.ID,
			PublicTokenPrefix: "agpub_page",
		}); err != nil {
			t.Fatal(err)
		}
		createdIDs = append(createdIDs, thread.ID)
	}
	summaryThreadID := createdIDs[0]
	for _, body := range []string{"first PostgreSQL summary", "second PostgreSQL summary"} {
		if _, err := repository.PostMessage(ctx, member.ID, summaryThreadID, memberAuth, body, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.pool.Exec(ctx, `
update messages
set created_at = case body
  when 'first PostgreSQL summary' then '2026-08-03T12:34:54Z'::timestamptz
  when 'second PostgreSQL summary' then '2026-08-03T12:34:55Z'::timestamptz
  else created_at
end
where thread_id = $1
`, summaryThreadID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `
update threads
set updated_at = '2026-08-03T12:34:56.123456Z'::timestamptz
where id = any($1)
`, createdIDs); err != nil {
		t.Fatal(err)
	}
	var indexDefinition string
	if err := repository.pool.QueryRow(ctx, `
select indexdef from pg_indexes
where schemaname = current_schema() and indexname = 'threads_updated_id_idx'
`).Scan(&indexDefinition); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(indexDefinition, "updated_at DESC") || !strings.Contains(indexDefinition, "id") {
		t.Fatalf("continuation index definition=%q", indexDefinition)
	}
	var messageIndexDefinition string
	if err := repository.pool.QueryRow(ctx, `
select indexdef from pg_indexes
where schemaname = current_schema() and indexname = 'messages_thread_latest_idx'
`).Scan(&messageIndexDefinition); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(messageIndexDefinition, "thread_id") || !strings.Contains(messageIndexDefinition, "created_at DESC") || !strings.Contains(messageIndexDefinition, "id DESC") {
		t.Fatalf("message summary index definition=%q", messageIndexDefinition)
	}

	type filterCase struct {
		name    string
		filter  string
		teamRef string
	}
	filters := []filterCase{
		{name: "all", filter: types.ThreadFilterAll},
		{name: "private", filter: types.ThreadFilterPrivate},
		{name: "shared", filter: types.ThreadFilterShared},
		{name: "team", filter: types.ThreadFilterTeam, teamRef: team.Slug},
		{name: "public", filter: types.ThreadFilterPublic},
	}
	threadIDs := func(threads []types.Thread) []string {
		ids := make([]string, 0, len(threads))
		for _, thread := range threads {
			ids = append(ids, thread.ID)
		}
		return ids
	}
	searchIDs := func(threads []types.SearchThreadResult) []string {
		ids := make([]string, 0, len(threads))
		for _, thread := range threads {
			ids = append(ids, thread.ID)
		}
		return ids
	}
	for _, testCase := range filters {
		t.Run(testCase.name, func(t *testing.T) {
			expectedList, err := repository.ListThreadsFiltered(ctx, member.ID, types.ThreadListParams{Limit: 100, Filter: testCase.filter, TeamRef: testCase.teamRef})
			if err != nil {
				t.Fatal(err)
			}
			expectedSearch, err := repository.SearchThreads(ctx, member.ID, types.SearchThreadParams{Query: "pagination marker", Limit: 100, Filter: testCase.filter, TeamRef: testCase.teamRef})
			if err != nil {
				t.Fatal(err)
			}

			listIDs := []string{}
			listSeen := map[string]bool{}
			var listCursor *types.ThreadPageCursor
			for pageNumber := 0; pageNumber < 20; pageNumber++ {
				page, err := repository.ListThreadsPage(ctx, member.ID, types.ThreadListParams{Limit: 2, Filter: testCase.filter, TeamRef: testCase.teamRef, Cursor: listCursor})
				if err != nil {
					t.Fatal(err)
				}
				for _, thread := range page.Threads {
					if thread.MessageCount == nil || thread.LastMessagePreview == nil {
						t.Fatalf("thread summary omitted for %s: %#v", thread.ID, thread)
					}
					if thread.ID == summaryThreadID && (*thread.MessageCount != 2 || *thread.LastMessagePreview != "second PostgreSQL summary") {
						t.Fatalf("thread summary=%d %q, want 2 and latest body", *thread.MessageCount, *thread.LastMessagePreview)
					}
					if listSeen[thread.ID] {
						t.Fatalf("duplicate list thread %s", thread.ID)
					}
					listSeen[thread.ID] = true
					listIDs = append(listIDs, thread.ID)
				}
				if !page.Page.HasMore {
					break
				}
				if page.Page.NextCursor == nil {
					t.Fatalf("list page omitted cursor: %#v", page.Page)
				}
				listCursor, err = types.DecodeThreadPageCursor(*page.Page.NextCursor)
				if err != nil {
					t.Fatal(err)
				}
			}
			if want := threadIDs(expectedList); !reflect.DeepEqual(listIDs, want) {
				t.Fatalf("list IDs=%v, want %v", listIDs, want)
			}

			pagedSearchIDs := []string{}
			searchSeen := map[string]bool{}
			var searchCursor *types.ThreadPageCursor
			for pageNumber := 0; pageNumber < 20; pageNumber++ {
				page, err := repository.SearchThreadsPage(ctx, member.ID, types.SearchThreadParams{Query: "pagination marker", Limit: 2, Filter: testCase.filter, TeamRef: testCase.teamRef, Cursor: searchCursor})
				if err != nil {
					t.Fatal(err)
				}
				for _, thread := range page.Threads {
					if searchSeen[thread.ID] {
						t.Fatalf("duplicate search thread %s", thread.ID)
					}
					searchSeen[thread.ID] = true
					pagedSearchIDs = append(pagedSearchIDs, thread.ID)
				}
				if !page.Page.HasMore {
					break
				}
				if page.Page.NextCursor == nil {
					t.Fatalf("search page omitted cursor: %#v", page.Page)
				}
				searchCursor, err = types.DecodeThreadPageCursor(*page.Page.NextCursor)
				if err != nil {
					t.Fatal(err)
				}
			}
			if want := searchIDs(expectedSearch); !reflect.DeepEqual(pagedSearchIDs, want) {
				t.Fatalf("search IDs=%v, want %v", pagedSearchIDs, want)
			}
		})
	}
}
