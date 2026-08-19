package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"agentbox/internal/agentbox/types"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestOwnerAdministrationCollectionsAreBoundedAndContinuable(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "page-owner@example.com", "Page Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	createdUsers := []types.User{owner}
	for index := 0; index < 27; index++ {
		user, err := repository.CreateUser(ctx, fmt.Sprintf("page-user-%02d@example.com", index), fmt.Sprintf("Page User %02d", index), nil)
		if err != nil {
			t.Fatal(err)
		}
		createdUsers = append(createdUsers, user)
		if _, err := repository.CreateAPIKey(ctx, user.ID, fmt.Sprintf("credential-%02d", index), "test", fmt.Sprintf("hash-%02d", index), fmt.Sprintf("prefix-%02d", index), []string{"threads:read"}); err != nil {
			t.Fatal(err)
		}
	}
	teamIDs := []string{}
	for index := 0; index < 13; index++ {
		team, err := repository.CreateTeam(ctx, fmt.Sprintf("page-team-%02d", index), fmt.Sprintf("Page Team %02d", index))
		if err != nil {
			t.Fatal(err)
		}
		teamIDs = append(teamIDs, team.ID)
		for memberIndex := 0; memberIndex < 12; memberIndex++ {
			if _, err := repository.AddTeamMember(ctx, team.ID, createdUsers[(index+memberIndex)%len(createdUsers)].ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	for index := 0; index < 23; index++ {
		if _, err := repository.CreateSignupInvitation(ctx, owner.ID, fmt.Sprintf("invite-hash-%02d", index), time.Now().UTC().Add(time.Hour), []string{teamIDs[index%len(teamIDs)]}); err != nil {
			t.Fatal(err)
		}
	}

	usersPage, err := repository.ListUsersPage(ctx, types.PageRequest{Limit: 10})
	if err != nil || len(usersPage.Users) != 10 || usersPage.Page.NextCursor == nil {
		t.Fatalf("users page=%#v err=%v", usersPage, err)
	}
	usersNext, err := repository.ListUsersPage(ctx, types.PageRequest{Limit: 10, Offset: 10})
	if err != nil || len(usersNext.Users) != 10 {
		t.Fatalf("users next=%#v err=%v", usersNext, err)
	}
	credentialsPage, err := repository.ListAllAPIKeysPage(ctx, types.PageRequest{Limit: 10})
	if err != nil || len(credentialsPage.Credentials) != 10 || credentialsPage.Page.NextCursor == nil {
		t.Fatalf("credentials page=%#v err=%v", credentialsPage, err)
	}
	invitationsPage, err := repository.ListSignupInvitationsPage(ctx, types.PageRequest{Limit: 10})
	if err != nil || len(invitationsPage.Invitations) != 10 || invitationsPage.Page.NextCursor == nil {
		t.Fatalf("invitations page=%#v err=%v", invitationsPage, err)
	}
	for _, invitation := range invitationsPage.Invitations {
		if len(invitation.Teams) != 1 {
			t.Fatalf("batched invitation teams missing: %#v", invitation)
		}
	}
	teamsPage, err := repository.ListTeamsPage(ctx, types.PageRequest{Limit: 5}, 4)
	if err != nil || len(teamsPage.Teams) != 5 || teamsPage.Page.NextCursor == nil {
		t.Fatalf("teams page=%#v err=%v", teamsPage, err)
	}
	for _, team := range teamsPage.Teams {
		if len(team.Members) != 4 || team.MemberCount != 12 || team.MembersPage.NextCursor == nil {
			t.Fatalf("bounded team members=%#v", team)
		}
	}
	membersPage, err := repository.ListTeamMembersPage(ctx, teamIDs[0], types.PageRequest{Limit: 5})
	if err != nil || len(membersPage.Members) != 5 || membersPage.Page.NextCursor == nil {
		t.Fatalf("members page=%#v err=%v", membersPage, err)
	}
}

func TestOwnerContentPaginationTraversesEntireDeployment(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "content-page-owner@example.com", "Content Page Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}
	authContext := types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, SubjectType: types.AuthSubjectUserSession, SessionID: "sess_content_page", ActorName: "Web dashboard"}
	for index := 0; index < 31; index++ {
		if _, err := repository.CreateThread(ctx, owner.ID, fmt.Sprintf("Paged content %02d", index), authContext); err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]bool{}
	offset := 0
	for {
		page, err := repository.ListOwnerContentThreadsPage(ctx, owner.ID, types.OwnerContentListParams{Limit: 10, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Threads) == 0 {
			t.Fatal("owner content pagination returned an empty intermediate page")
		}
		for _, thread := range page.Threads {
			if seen[thread.ID] {
				t.Fatalf("thread %s appeared on multiple owner-content pages", thread.ID)
			}
			seen[thread.ID] = true
		}
		if page.Page.NextCursor == nil {
			break
		}
		next, err := strconv.Atoi(*page.Page.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		offset = next
	}
	if len(seen) != 31 {
		t.Fatalf("owner content pagination reached %d/31 threads", len(seen))
	}

	searchFirst, err := repository.SearchOwnerContentThreadsPage(ctx, owner.ID, types.OwnerContentSearchParams{Query: "Paged content", Limit: 12})
	if err != nil {
		t.Fatal(err)
	}
	if len(searchFirst.Threads) != 12 || searchFirst.Page.NextCursor == nil {
		t.Fatalf("first owner search page=%#v", searchFirst)
	}
	searchSecond, err := repository.SearchOwnerContentThreadsPage(ctx, owner.ID, types.OwnerContentSearchParams{Query: "Paged content", Limit: 12, Offset: 12})
	if err != nil {
		t.Fatal(err)
	}
	if len(searchSecond.Threads) != 12 || searchSecond.Page.PreviousCursor == nil {
		t.Fatalf("second owner search page=%#v", searchSecond)
	}
}

func TestOwnerContentRepositoryBypassesOnlyTheExplicitOwnerPath(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "content-owner@example.com", "Content Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	member, err := repository.CreateUser(ctx, "content-member@example.com", "Content Member", nil)
	if err != nil {
		t.Fatal(err)
	}
	teammate, err := repository.CreateUser(ctx, "content-teammate@example.com", "Content Teammate", nil)
	if err != nil {
		t.Fatal(err)
	}
	team, err := repository.CreateTeam(ctx, "content-audit", "Content Audit")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{member.ID, teammate.ID} {
		if _, err := repository.AddTeamMember(ctx, team.ID, userID); err != nil {
			t.Fatal(err)
		}
	}
	memberAuth := types.AuthContext{
		UserID:          member.ID,
		UserDisplayName: member.DisplayName,
		SubjectType:     types.AuthSubjectUserSession,
		ActorName:       "Web dashboard",
	}
	privateThread, err := repository.CreateThread(ctx, member.ID, "Private owner audit marker", memberAuth)
	if err != nil {
		t.Fatal(err)
	}
	sharedThread, err := repository.CreateThread(ctx, member.ID, "Shared owner audit marker", memberAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(ctx, repository, member.ID, sharedThread.ID, []string{team.ID}); err != nil {
		t.Fatal(err)
	}
	privateMessage, err := repository.PostMessage(ctx, member.ID, privateThread.ID, memberAuth, "private searchable owner evidence", nil, []types.NewAsset{{
		StorageKey: "agentbox/owner-content/private-evidence.txt",
		FileName:   "private-evidence.txt",
		SizeBytes:  23,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PostMessage(ctx, member.ID, sharedThread.ID, memberAuth, "shared content", nil, nil); err != nil {
		t.Fatal(err)
	}

	normalOwnerThread, err := repository.GetThread(ctx, owner.ID, privateThread.ID)
	if err != nil || normalOwnerThread != nil {
		t.Fatalf("normal owner access bypassed private thread: thread=%#v err=%v", normalOwnerThread, err)
	}
	normalOwnerAsset, err := repository.GetAsset(ctx, owner.ID, privateMessage.Assets[0].ID)
	if err != nil || normalOwnerAsset != nil {
		t.Fatalf("normal owner access bypassed private asset: asset=%#v err=%v", normalOwnerAsset, err)
	}
	teammateShared, err := repository.GetThread(ctx, teammate.ID, sharedThread.ID)
	if err != nil || teammateShared == nil {
		t.Fatalf("normal qualified member lost shared access: thread=%#v err=%v", teammateShared, err)
	}
	teammatePrivate, err := repository.GetThread(ctx, teammate.ID, privateThread.ID)
	if err != nil || teammatePrivate != nil {
		t.Fatalf("normal qualified member saw private content: thread=%#v err=%v", teammatePrivate, err)
	}

	all, err := repository.ListOwnerContentThreads(ctx, owner.ID, types.OwnerContentListParams{Limit: 50})
	if err != nil || len(all) != 2 {
		t.Fatalf("owner content list=%#v err=%v", all, err)
	}
	byID := map[string]types.OwnerContentThreadSummary{}
	for _, thread := range all {
		byID[thread.ID] = thread
	}
	if summary, ok := byID[privateThread.ID]; !ok || summary.Owner.ID != member.ID || !summary.VisibilitySummary.Private || summary.VisibilitySummary.Public || len(summary.VisibilitySummary.SharedTeams) != 0 || summary.MessageCount != 1 {
		t.Fatalf("private owner summary=%#v", summary)
	}
	if summary, ok := byID[sharedThread.ID]; !ok || summary.Owner.ID != member.ID || summary.VisibilitySummary.Private || len(summary.VisibilitySummary.SharedTeams) != 1 || summary.VisibilitySummary.SharedTeams[0].ID != team.ID || summary.MessageCount != 1 {
		t.Fatalf("shared owner summary=%#v", summary)
	}
	byUser, err := repository.ListOwnerContentThreads(ctx, owner.ID, types.OwnerContentListParams{Limit: 50, UserID: member.ID})
	if err != nil || len(byUser) != 2 {
		t.Fatalf("owner user filter=%#v err=%v", byUser, err)
	}
	byTeam, err := repository.ListOwnerContentThreads(ctx, owner.ID, types.OwnerContentListParams{Limit: 50, TeamRef: team.Slug})
	if err != nil || len(byTeam) != 1 || byTeam[0].ID != sharedThread.ID {
		t.Fatalf("owner team filter=%#v err=%v", byTeam, err)
	}
	searched, err := repository.SearchOwnerContentThreads(ctx, owner.ID, types.OwnerContentSearchParams{
		Query: "searchable owner evidence",
		Limit: 50,
	})
	if err != nil || len(searched) != 1 || searched[0].ID != privateThread.ID || len(searched[0].MatchedSnippets) == 0 {
		t.Fatalf("owner content search=%#v err=%v", searched, err)
	}
	detail, err := repository.GetOwnerContentThread(ctx, owner.ID, privateThread.ID)
	if err != nil || detail == nil || detail.Owner.ID != member.ID || detail.ID != privateThread.ID || !detail.VisibilitySummary.Private || len(detail.Messages) != 1 || detail.Messages[0].ID != privateMessage.ID || len(detail.Messages[0].Assets) != 1 {
		t.Fatalf("owner content detail=%#v err=%v", detail, err)
	}
	asset, err := repository.GetOwnerContentAsset(ctx, privateMessage.Assets[0].ID)
	if err != nil || asset == nil || asset.ID != privateMessage.Assets[0].ID || asset.StorageKey != "agentbox/owner-content/private-evidence.txt" || asset.CreatedByUserID == nil || *asset.CreatedByUserID != member.ID {
		t.Fatalf("owner content asset=%#v err=%v", asset, err)
	}
}

func TestOwnerSetupTokensAreHashedSingleUseAndTransactional(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	firstSecret := "agos_first_secret"
	first, err := repository.CreateOwnerSetupToken(ctx, hashSecret(firstSecret), time.Now().UTC().Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if first.Purpose != "bootstrap" {
		t.Fatalf("first purpose = %q", first.Purpose)
	}
	var storedHash string
	if err := repository.pool.QueryRow(ctx, `select token_hash from owner_setup_tokens where id = $1`, first.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash != hashSecret(firstSecret) || storedHash == firstSecret {
		t.Fatalf("setup token storage is not hash-only: %q", storedHash)
	}

	secondSecret := "agos_second_secret"
	second, err := repository.CreateOwnerSetupToken(ctx, hashSecret(secondSecret), time.Now().UTC().Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.Purpose != "bootstrap" || second.ID == first.ID {
		t.Fatalf("unexpected replacement token: first=%#v second=%#v", first, second)
	}
	var firstRevoked bool
	if err := repository.pool.QueryRow(ctx, `select revoked_at is not null from owner_setup_tokens where id = $1`, first.ID).Scan(&firstRevoked); err != nil {
		t.Fatal(err)
	}
	if !firstRevoked {
		t.Fatal("issuing a replacement token did not revoke the prior active token")
	}
	if _, _, err := repository.UseOwnerSetupToken(ctx, hashSecret(firstSecret), "owner@example.com", "Owner", "hash-one"); !errors.Is(err, ErrOwnerSetupTokenInvalid) {
		t.Fatalf("revoked token error = %v", err)
	}

	owner, consumed, err := repository.UseOwnerSetupToken(ctx, hashSecret(secondSecret), "owner@example.com", "Owner", "hash-one")
	if err != nil {
		t.Fatal(err)
	}
	if !owner.IsOwner || consumed.ConsumedAt == nil {
		t.Fatalf("bootstrap result owner=%#v token=%#v", owner, consumed)
	}
	if _, _, err := repository.UseOwnerSetupToken(ctx, hashSecret(secondSecret), "owner@example.com", "Owner", "hash-one"); !errors.Is(err, ErrOwnerSetupTokenInvalid) {
		t.Fatalf("replayed token error = %v", err)
	}

	recoverySecret := "agos_recovery_secret"
	recovery, err := repository.CreateOwnerSetupToken(ctx, hashSecret(recoverySecret), time.Now().UTC().Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Purpose != "recovery" {
		t.Fatalf("recovery purpose = %q", recovery.Purpose)
	}
	if _, _, err := repository.UseOwnerSetupToken(ctx, hashSecret(recoverySecret), "other@example.com", "Wrong", "hash-two"); !errors.Is(err, ErrOwnerAlreadyExists) {
		t.Fatalf("wrong-email recovery error = %v", err)
	}
	var recoveryConsumed bool
	if err := repository.pool.QueryRow(ctx, `select consumed_at is not null from owner_setup_tokens where id = $1`, recovery.ID).Scan(&recoveryConsumed); err != nil {
		t.Fatal(err)
	}
	if recoveryConsumed {
		t.Fatal("failed recovery consumed the one-time token despite transaction rollback")
	}
	recoveredOwner, _, err := repository.UseOwnerSetupToken(ctx, hashSecret(recoverySecret), "OWNER@example.com", "Recovered", "hash-two")
	if err != nil {
		t.Fatal(err)
	}
	if recoveredOwner.ID != owner.ID || recoveredOwner.DisplayName != "Recovered" {
		t.Fatalf("recovery changed owner identity: before=%#v after=%#v", owner, recoveredOwner)
	}

	expiredSecret := "agos_expired_secret"
	expired, err := repository.CreateOwnerSetupToken(ctx, hashSecret(expiredSecret), time.Now().UTC().Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.pool.Exec(ctx, `update owner_setup_tokens set expires_at = now() - interval '1 minute' where id = $1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.UseOwnerSetupToken(ctx, hashSecret(expiredSecret), "owner@example.com", "Owner", "hash-three"); !errors.Is(err, ErrOwnerSetupTokenInvalid) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestSignupInvitationsRegisterTransactionallyAndDisableUsers(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "owner-hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateUser(ctx, "existing@example.com", "Existing", nil); err != nil {
		t.Fatal(err)
	}

	engineering, err := repository.CreateTeam(ctx, "engineering", "Engineering")
	if err != nil {
		t.Fatal(err)
	}
	operations, err := repository.CreateTeam(ctx, "operations", "Operations")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateTeam(ctx, "ENGINEERING", "Duplicate"); !errors.Is(err, types.ErrTeamSlugConflict) {
		t.Fatalf("duplicate team slug error=%v", err)
	}

	invitationSecret := "aginv_transactional"
	invitation, err := repository.CreateSignupInvitation(ctx, owner.ID, hashSecret(invitationSecret), time.Now().UTC().Add(time.Hour), []string{engineering.ID, operations.ID, engineering.ID})
	if err != nil {
		t.Fatal(err)
	}
	if invitation.CreatedByUserID != owner.ID || len(invitation.Teams) != 2 {
		t.Fatalf("unexpected invitation=%#v", invitation)
	}
	if _, err := repository.pool.Exec(ctx, `delete from teams where id = $1`, engineering.ID); err == nil {
		t.Fatal("team referenced by an active invitation was deleted")
	} else {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
			t.Fatalf("active-invitation team deletion error=%v", err)
		}
	}
	if _, _, _, err := repository.RegisterWithSignupInvitation(ctx, hashSecret(invitationSecret), "EXISTING@example.com", "Duplicate", "password-hash", "session-duplicate", time.Now().UTC().Add(time.Hour)); !errors.Is(err, types.ErrEmailAlreadyRegistered) {
		t.Fatalf("duplicate registration error=%v", err)
	}
	if active, err := repository.FindSignupInvitation(ctx, hashSecret(invitationSecret)); err != nil || active == nil {
		t.Fatalf("duplicate registration consumed invitation: active=%#v err=%v", active, err)
	}
	var existingMemberships int
	if err := repository.pool.QueryRow(ctx, `
select count(*)
from team_memberships tm
join users u on u.id = tm.user_id
where lower(u.email) = lower('existing@example.com')
`).Scan(&existingMemberships); err != nil {
		t.Fatal(err)
	}
	if existingMemberships != 0 {
		t.Fatalf("failed duplicate registration created memberships=%d", existingMemberships)
	}

	memberSessionHash := "member-session-hash"
	member, session, consumed, err := repository.RegisterWithSignupInvitation(ctx, hashSecret(invitationSecret), "member@example.com", "Member", "member-password-hash", memberSessionHash, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if member.IsOwner || session.UserID != member.ID || consumed.ConsumedAt == nil || consumed.ConsumedByUserID == nil || *consumed.ConsumedByUserID != member.ID {
		t.Fatalf("unexpected registration member=%#v session=%#v invitation=%#v", member, session, consumed)
	}
	memberTeams, err := repository.ListUserTeams(ctx, member.ID)
	if err != nil || len(memberTeams) != 2 || memberTeams[0].ID != engineering.ID || memberTeams[1].ID != operations.ID {
		t.Fatalf("transactional invitation memberships=%#v err=%v", memberTeams, err)
	}
	if _, err := repository.AddTeamMember(ctx, engineering.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	firstOwnerMembership, err := repository.AddTeamMember(ctx, engineering.ID, owner.ID)
	if err != nil {
		t.Fatalf("duplicate membership add failed: %v", err)
	}
	if firstOwnerMembership.TeamID != engineering.ID || firstOwnerMembership.UserID != owner.ID {
		t.Fatalf("duplicate membership result=%#v", firstOwnerMembership)
	}
	renamedEngineering, err := repository.RenameTeam(ctx, engineering.ID, "Product Engineering")
	if err != nil || renamedEngineering.Slug != engineering.Slug || renamedEngineering.Name != "Product Engineering" {
		t.Fatalf("rename team=%#v err=%v", renamedEngineering, err)
	}
	if removed, err := repository.RemoveTeamMember(ctx, operations.ID, member.ID); err != nil || !removed {
		t.Fatalf("remove membership removed=%t err=%v", removed, err)
	}
	if removed, err := repository.RemoveTeamMember(ctx, operations.ID, member.ID); err != nil || removed {
		t.Fatalf("idempotent remove removed=%t err=%v", removed, err)
	}
	memberTeams, err = repository.ListUserTeams(ctx, member.ID)
	if err != nil || len(memberTeams) != 1 || memberTeams[0].ID != engineering.ID {
		t.Fatalf("membership removal teams=%#v err=%v", memberTeams, err)
	}
	ownerThread, err := repository.CreateThread(ctx, owner.ID, "membership does not share", types.AuthContext{UserID: owner.ID, UserDisplayName: owner.DisplayName, ActorName: "Web dashboard"})
	if err != nil {
		t.Fatal(err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, member.ID, ownerThread.ID); err != nil || access != nil {
		t.Fatalf("team membership changed private thread access: access=%#v err=%v", access, err)
	}
	if active, err := repository.FindSignupInvitation(ctx, hashSecret(invitationSecret)); err != nil || active != nil {
		t.Fatalf("consumed invitation remained active: active=%#v err=%v", active, err)
	}

	memberKeySecret := "agb_member_disable"
	if _, err := repository.CreateAPIKey(ctx, member.ID, "local", "local", hashSecret(memberKeySecret), "agb_member", []string{"threads:read"}); err != nil {
		t.Fatal(err)
	}
	oldKey, err := repository.CreateAPIKey(ctx, member.ID, "old", "custom", hashSecret("agb_member_old"), "agb_old", []string{"threads:read"})
	if err != nil {
		t.Fatal(err)
	}
	if revoked, err := repository.RevokeAPIKeyByID(ctx, oldKey.ID); err != nil || !revoked {
		t.Fatalf("pre-revoke old credential revoked=%t err=%v", revoked, err)
	}
	if revoked, err := repository.RevokeAPIKeyByID(ctx, oldKey.ID); err != nil || !revoked {
		t.Fatalf("idempotent old credential revoke=%t err=%v", revoked, err)
	}
	allKeys, err := repository.ListAllAPIKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(allKeys) != 2 || allKeys[0].UserID != member.ID || allKeys[1].UserID != member.ID || allKeys[0].TokenHash == "" || allKeys[0].Key != "" {
		t.Fatalf("owner credential metadata fixture=%#v", allKeys)
	}

	memberAuth := types.AuthContext{UserID: member.ID, UserDisplayName: member.DisplayName, ActorName: "Web dashboard", SubjectType: types.AuthSubjectUserSession}
	memberPrivateThread, err := repository.CreateThread(ctx, member.ID, "disabled member private", memberAuth)
	if err != nil {
		t.Fatal(err)
	}
	memberSharedThread, err := repository.CreateThread(ctx, member.ID, "disabled member shared", memberAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(ctx, repository, member.ID, memberSharedThread.ID, []string{engineering.ID}); err != nil {
		t.Fatal(err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, owner.ID, memberSharedThread.ID); err != nil || access == nil {
		t.Fatalf("qualified owner lacked shared thread before disable: access=%#v err=%v", access, err)
	}
	cliCodeHash := "member-cli-code"
	cliStateHash := "member-cli-state"
	if _, err := repository.CreateCLILoginCode(ctx, types.CLILoginCode{
		UserID:      member.ID,
		CodeHash:    cliCodeHash,
		StateHash:   cliStateHash,
		RedirectURI: "http://127.0.0.1:8080/callback",
		ExpiresAt:   isoMillis(time.Now().UTC().Add(time.Hour)),
	}); err != nil {
		t.Fatal(err)
	}
	disabled, err := repository.SetUserDisabled(ctx, member.ID, true)
	if err != nil || disabled.DisabledAt == nil {
		t.Fatalf("disable member=%#v err=%v", disabled, err)
	}
	if foundSession, foundUser, err := repository.FindUserSessionBySecretHash(ctx, memberSessionHash); err != nil || foundSession != nil || foundUser != nil {
		t.Fatalf("disabled session resolved: session=%#v user=%#v err=%v", foundSession, foundUser, err)
	}
	if foundKey, foundUser, err := repository.FindAPIKeyBySecret(ctx, memberKeySecret); err != nil || foundKey != nil || foundUser != nil {
		t.Fatalf("disabled key resolved: key=%#v user=%#v err=%v", foundKey, foundUser, err)
	}
	if teams, err := repository.ListUserTeams(ctx, member.ID); err != nil || len(teams) != 0 {
		t.Fatalf("disabled memberships=%#v err=%v", teams, err)
	}
	if _, err := repository.AddTeamMember(ctx, engineering.ID, member.ID); !errors.Is(err, types.ErrUserDisabled) {
		t.Fatalf("disabled user re-added to team: %v", err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, owner.ID, memberSharedThread.ID); err != nil || access == nil {
		t.Fatalf("disabled owner's shared thread unavailable to qualified member: access=%#v err=%v", access, err)
	}
	if access, err := repository.ResolveThreadAccess(ctx, owner.ID, memberPrivateThread.ID); err != nil || access != nil {
		t.Fatalf("disabled owner's private thread leaked: access=%#v err=%v", access, err)
	}
	allKeys, err = repository.ListAllAPIKeys(ctx)
	if err != nil || len(allKeys) != 2 {
		t.Fatalf("disabled credential metadata=%#v err=%v", allKeys, err)
	}
	for _, key := range allKeys {
		if key.RevokedAt == nil {
			t.Fatalf("credential remained active after disable: %#v", key)
		}
	}
	enabled, err := repository.SetUserDisabled(ctx, member.ID, false)
	if err != nil || enabled.DisabledAt != nil {
		t.Fatalf("enable member=%#v err=%v", enabled, err)
	}
	if code, user, err := repository.ConsumeCLILoginCode(ctx, cliCodeHash, cliStateHash, "http://127.0.0.1:8080/callback"); err != nil || code != nil || user != nil {
		t.Fatalf("disabled user's old CLI login code became usable after enablement: code=%#v user=%#v err=%v", code, user, err)
	}
	if teams, err := repository.ListUserTeams(ctx, member.ID); err != nil || len(teams) != 0 {
		t.Fatalf("enable restored memberships=%#v err=%v", teams, err)
	}
	if foundKey, foundUser, err := repository.FindAPIKeyBySecret(ctx, memberKeySecret); err != nil || foundKey != nil || foundUser != nil {
		t.Fatalf("enable restored old credential: key=%#v user=%#v err=%v", foundKey, foundUser, err)
	}
	if _, err := repository.SetUserDisabled(ctx, owner.ID, true); !errors.Is(err, types.ErrOwnerCannotBeDisabled) {
		t.Fatalf("owner disable error=%v", err)
	}

	concurrentSecret := "aginv_concurrent"
	if _, err := repository.CreateSignupInvitation(ctx, owner.ID, hashSecret(concurrentSecret), time.Now().UTC().Add(time.Hour), []string{engineering.ID}); err != nil {
		t.Fatal(err)
	}
	type registrationResult struct {
		user types.User
		err  error
	}
	results := make(chan registrationResult, 2)
	var waitGroup sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			user, _, _, err := repository.RegisterWithSignupInvitation(
				ctx,
				hashSecret(concurrentSecret),
				fmt.Sprintf("concurrent-%d@example.com", index),
				fmt.Sprintf("Concurrent %d", index),
				"password-hash",
				fmt.Sprintf("session-%d", index),
				time.Now().UTC().Add(time.Hour),
			)
			results <- registrationResult{user: user, err: err}
		}()
	}
	waitGroup.Wait()
	close(results)
	successes := 0
	invalids := 0
	successfulUserID := ""
	for result := range results {
		if result.err == nil {
			successes++
			if result.user.ID == "" {
				t.Fatal("successful concurrent registration had empty user ID")
			}
			successfulUserID = result.user.ID
		} else if errors.Is(result.err, types.ErrSignupInvitationInvalid) {
			invalids++
		} else {
			t.Fatalf("unexpected concurrent registration error=%v", result.err)
		}
	}
	if successes != 1 || invalids != 1 {
		t.Fatalf("concurrent registration results: successes=%d invalids=%d", successes, invalids)
	}
	if teams, err := repository.ListUserTeams(ctx, successfulUserID); err != nil || len(teams) != 1 || teams[0].ID != engineering.ID {
		t.Fatalf("concurrent winner memberships=%#v err=%v", teams, err)
	}

	zeroSecret := "aginv_zero_team"
	if _, err := repository.CreateSignupInvitation(ctx, owner.ID, hashSecret(zeroSecret), time.Now().UTC().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	zeroUser, _, _, err := repository.RegisterWithSignupInvitation(ctx, hashSecret(zeroSecret), "zero@example.com", "Zero", "hash", "zero-session", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if teams, err := repository.ListUserTeams(ctx, zeroUser.ID); err != nil || len(teams) != 0 {
		t.Fatalf("zero-team registration teams=%#v err=%v", teams, err)
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
from team_memberships tm
join teams t on t.id = tm.team_id
where tm.user_id = $1
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
	if !strings.Contains(plan.String(), "team_memberships_user_team_idx") {
		t.Fatalf("team list plan did not use membership index:\n%s", plan.String())
	}
}

func TestUserOnboardingCredentialsAreExplicitResumableAndSerialized(t *testing.T) {
	repository, ctx := openPostgresTestRepository(t)
	if err := repository.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := repository.BootstrapOwner(ctx, "owner@example.com", "Owner", "hash")
	if err != nil {
		t.Fatal(err)
	}

	initial, err := repository.GetOnboardingState(ctx, owner.ID)
	if err != nil || len(initial.Steps) != 0 {
		t.Fatalf("initial onboarding=%#v err=%v", initial, err)
	}
	keys, err := repository.ListAPIKeys(ctx, owner.ID)
	if err != nil || len(keys) != 0 {
		t.Fatalf("onboarding read created keys=%#v err=%v", keys, err)
	}
	dismissed, err := repository.DismissOnboarding(ctx, owner.ID)
	if err != nil || dismissed.DismissedAt == nil {
		t.Fatalf("dismissed onboarding=%#v err=%v", dismissed, err)
	}

	chatSecret := "agb_onboarding_chat"
	chat, state, err := repository.CreateOnboardingCredential(ctx, owner.ID, "chatgpt", "ChatGPT", "chatgpt", hashSecret(chatSecret), "agb_chat", []string{"threads:read", "mcp:use"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Name != "ChatGPT" || state.DismissedAt != nil || len(state.Steps) != 1 || state.Steps[0].Credential == nil || state.Steps[0].Credential.ID != chat.ID || state.Steps[0].Credential.Key != "" {
		t.Fatalf("chat onboarding key=%#v state=%#v", chat, state)
	}
	if _, _, err := repository.CreateOnboardingCredential(ctx, owner.ID, "chatgpt", "ChatGPT", "chatgpt", hashSecret("duplicate"), "agb_dupe", []string{"threads:read"}, "", false); !errors.Is(err, types.ErrOnboardingCredentialExists) {
		t.Fatalf("duplicate onboarding credential error=%v", err)
	}

	raycast, _, err := repository.CreateOnboardingCredential(ctx, owner.ID, "raycast", "Raycast", "raycast", hashSecret("agb_onboarding_raycast"), "agb_raycast", []string{"threads:read", "threads:write", "assets:read", "assets:write"}, "https://dashboard.example", false)
	if err != nil {
		t.Fatal(err)
	}
	claude, _, err := repository.CreateOnboardingCredential(ctx, owner.ID, "claude", "Claude", "claude", hashSecret("agb_onboarding_claude"), "agb_claude", []string{"threads:read", "mcp:use"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	local, state, err := repository.CreateOnboardingCredential(ctx, owner.ID, "local", "Local CLI", "local", hashSecret("agb_onboarding_local"), "agb_local", []string{"threads:read", "threads:write", "keys:read", "keys:write"}, "", false)
	if err != nil || len(state.Steps) != 4 {
		t.Fatalf("local onboarding key=%#v state=%#v err=%v", local, state, err)
	}
	if got := []string{state.Steps[0].Connector, state.Steps[1].Connector, state.Steps[2].Connector, state.Steps[3].Connector}; strings.Join(got, ",") != "chatgpt,claude,local,raycast" {
		t.Fatalf("onboarding connector order=%v", got)
	}
	if chat.ID == claude.ID || chat.ID == local.ID || chat.ID == raycast.ID || claude.ID == local.ID || claude.ID == raycast.ID || local.ID == raycast.ID {
		t.Fatalf("connectors collapsed credentials: chat=%s claude=%s local=%s raycast=%s", chat.ID, claude.ID, local.ID, raycast.ID)
	}

	rotatedSecret := "agb_onboarding_chat_rotated"
	rotated, _, err := repository.CreateOnboardingCredential(ctx, owner.ID, "chatgpt", "ChatGPT", "chatgpt", hashSecret(rotatedSecret), "agb_rotate", []string{"threads:read", "mcp:use"}, "", true)
	if err != nil || rotated.ID != chat.ID {
		t.Fatalf("rotation=%#v original=%#v err=%v", rotated, chat, err)
	}
	if oldKey, _, err := repository.FindAPIKeyBySecret(ctx, chatSecret); err != nil || oldKey != nil {
		t.Fatalf("old rotated secret active: key=%#v err=%v", oldKey, err)
	}
	if newKey, user, err := repository.FindAPIKeyBySecret(ctx, rotatedSecret); err != nil || newKey == nil || user == nil || newKey.ID != chat.ID || user.ID != owner.ID {
		t.Fatalf("rotated secret lookup key=%#v user=%#v err=%v", newKey, user, err)
	}
	if claudeKey, _, err := repository.FindAPIKeyBySecret(ctx, "agb_onboarding_claude"); err != nil || claudeKey == nil || claudeKey.ID != claude.ID {
		t.Fatalf("chat rotation affected Claude: key=%#v err=%v", claudeKey, err)
	}

	if revoked, err := repository.RevokeAPIKeyForUserByID(ctx, owner.ID, local.ID); err != nil || !revoked {
		t.Fatalf("revoke local revoked=%t err=%v", revoked, err)
	}
	state, err = repository.GetOnboardingState(ctx, owner.ID)
	if err != nil || state.Steps[2].Connector != "local" || state.Steps[2].CompletedAt == nil || state.Steps[2].Credential != nil {
		t.Fatalf("revoked local onboarding state=%#v err=%v", state, err)
	}
	recreated, _, err := repository.CreateOnboardingCredential(ctx, owner.ID, "local", "Local CLI", "local", hashSecret("agb_onboarding_local_new"), "agb_local_new", []string{"threads:read"}, "", false)
	if err != nil || recreated.ID == local.ID {
		t.Fatalf("recreated local=%#v original=%#v err=%v", recreated, local, err)
	}

	concurrentUser, err := repository.CreateUser(ctx, "concurrent-onboarding@example.com", "Concurrent", nil)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		key types.APIKey
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		index := index
		go func() {
			<-start
			key, _, err := repository.CreateOnboardingCredential(context.Background(), concurrentUser.ID, "chatgpt", "ChatGPT", "chatgpt", hashSecret(fmt.Sprintf("concurrent-%d", index)), fmt.Sprintf("agb_%d", index), []string{"threads:read"}, "", false)
			results <- result{key: key, err: err}
		}()
	}
	close(start)
	successes := 0
	conflicts := 0
	for index := 0; index < 2; index++ {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			if result.key.ID == "" {
				t.Fatal("concurrent onboarding success had empty key ID")
			}
		case errors.Is(result.err, types.ErrOnboardingCredentialExists):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent onboarding error=%v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent onboarding successes=%d conflicts=%d", successes, conflicts)
	}
	if keys, err := repository.ListAPIKeys(ctx, concurrentUser.ID); err != nil || len(keys) != 1 || keys[0].Name != "ChatGPT" {
		t.Fatalf("concurrent onboarding keys=%#v err=%v", keys, err)
	}
}
