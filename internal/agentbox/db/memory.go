package db

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"agentbox/internal/agentbox/identity"
	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
)

type memoryAssetLease struct{ asset types.Asset }

func (l memoryAssetLease) Asset() types.Asset          { return l.asset }
func (l memoryAssetLease) Close(context.Context) error { return nil }

type memoryPublicThreadLease struct{ thread types.ThreadWithMessages }

func (l memoryPublicThreadLease) Thread() types.ThreadWithMessages { return l.thread }
func (l memoryPublicThreadLease) Close(context.Context) error      { return nil }

type memoryAttachmentPurgeLease struct {
	mutex *sync.Mutex
	once  sync.Once
}

func (l *memoryAttachmentPurgeLease) Close(context.Context) error {
	l.once.Do(func() { l.mutex.Unlock() })
	return nil
}

type MemoryRepository struct {
	purgeMutex        sync.Mutex
	Threads           []types.Thread
	Messages          []types.Message
	Assets            []types.Asset
	Pending           []types.PendingUpload
	UploadCleanup     []memoryUploadCleanup
	APIKeys           []types.APIKey
	Users             []types.User
	Sessions          []types.UserSession
	CLICodes          []types.CLILoginCode
	OwnerSetupTokens  []memoryOwnerSetupToken
	SignupInvitations []memorySignupInvitation
	Teams             []types.Team
	TeamMemberships   []types.TeamMembership
	ThreadTeamShares  []types.ThreadTeamShare
	ThreadPublicLinks []types.ThreadPublicLink
	Onboarding        []types.OnboardingState
	RaycastSetupURLs  map[string]string
}

type memoryOwnerSetupToken struct {
	Token     types.OwnerSetupToken
	TokenHash string
}

type memorySignupInvitation struct {
	Invitation types.SignupInvitation
	TokenHash  string
	TeamIDs    []string
}

type memoryUploadCleanup struct {
	Candidate    types.UploadCleanupCandidate
	NotBefore    time.Time
	CleanedAt    *time.Time
	AttemptCount int
	LastError    string
}

func (m *MemoryRepository) ResolveThreadAccess(_ context.Context, userID string, threadID string) (*types.ThreadAccess, error) {
	for _, thread := range m.Threads {
		if thread.ID == threadID {
			return m.normalThreadAccess(thread, userID), nil
		}
	}
	return nil, nil
}

func (m *MemoryRepository) GetThreadVisibility(_ context.Context, userID string, threadID string) (*types.ThreadVisibility, error) {
	for _, thread := range m.Threads {
		if thread.ID != threadID || m.normalThreadAccess(thread, userID) == nil {
			continue
		}
		visibility := m.threadVisibility(thread)
		return &visibility, nil
	}
	return nil, nil
}

func (m *MemoryRepository) ManageThreadVisibility(_ context.Context, userID string, threadID string, input types.ManageThreadVisibilityInput) (types.ManagedThreadVisibility, error) {
	input.AddTeams = uniqueNonEmptyStrings(input.AddTeams)
	input.RemoveTeams = uniqueNonEmptyStrings(input.RemoveTeams)

	var thread *types.Thread
	for index := range m.Threads {
		if m.Threads[index].ID == threadID && m.normalThreadAccess(m.Threads[index], userID) != nil {
			thread = &m.Threads[index]
			break
		}
	}
	if thread == nil {
		return types.ManagedThreadVisibility{}, types.ErrThreadNotFound
	}

	availableTeams, err := m.ListUserTeams(context.Background(), userID)
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	currentTeams := m.threadVisibility(*thread).SharedTeams
	addTeamIDs, err := resolveTeamReferences(input.AddTeams, availableTeams, true)
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	removeTeamIDs, err := resolveTeamReferences(input.RemoveTeams, currentTeams, false)
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	if stringSetsOverlap(addTeamIDs, removeTeamIDs) || (input.Public != nil && !*input.Public && input.RegeneratePublicLink) {
		return types.ManagedThreadVisibility{}, types.ErrThreadVisibilityConflict
	}

	activeLinkIndex := -1
	for index := range m.ThreadPublicLinks {
		if m.ThreadPublicLinks[index].ThreadID == threadID && m.ThreadPublicLinks[index].RevokedAt == nil {
			activeLinkIndex = index
			break
		}
	}
	createOrRotate := false
	if input.Public == nil || *input.Public {
		if input.RegeneratePublicLink {
			if activeLinkIndex < 0 && input.Public == nil {
				return types.ManagedThreadVisibility{}, types.ErrThreadPublicLinkNotFound
			}
			createOrRotate = true
		} else if input.Public != nil && *input.Public && activeLinkIndex < 0 {
			createOrRotate = true
		}
	}
	if createOrRotate {
		if strings.TrimSpace(input.PublicToken) == "" || strings.TrimSpace(input.PublicTokenHash) == "" || strings.TrimSpace(input.PublicTokenPrefix) == "" {
			return types.ManagedThreadVisibility{}, errors.New("public token material is required")
		}
		for index, link := range m.ThreadPublicLinks {
			if index != activeLinkIndex && link.TokenHash == input.PublicTokenHash {
				return types.ManagedThreadVisibility{}, errors.New("public token hash already exists")
			}
		}
	}

	removeSet := make(map[string]struct{}, len(removeTeamIDs))
	for _, teamID := range removeTeamIDs {
		removeSet[teamID] = struct{}{}
	}
	existingSet := map[string]struct{}{}
	shares := make([]types.ThreadTeamShare, 0, len(m.ThreadTeamShares)+len(addTeamIDs))
	for _, share := range m.ThreadTeamShares {
		if share.ThreadID != threadID {
			shares = append(shares, share)
			continue
		}
		if _, remove := removeSet[share.TeamID]; remove {
			continue
		}
		existingSet[share.TeamID] = struct{}{}
		shares = append(shares, share)
	}
	now := isoMillis(time.Now().UTC())
	creator := userID
	for _, teamID := range addTeamIDs {
		if _, exists := existingSet[teamID]; exists {
			continue
		}
		shares = append(shares, types.ThreadTeamShare{
			ThreadID:        threadID,
			TeamID:          teamID,
			CreatedByUserID: &creator,
			CreatedAt:       now,
		})
	}
	m.ThreadTeamShares = shares

	if input.Public != nil && !*input.Public {
		if activeLinkIndex >= 0 {
			m.ThreadPublicLinks[activeLinkIndex].RevokedAt = &now
			m.ThreadPublicLinks[activeLinkIndex].UpdatedAt = now
			activeLinkIndex = -1
		}
	} else if createOrRotate {
		if activeLinkIndex >= 0 {
			link := &m.ThreadPublicLinks[activeLinkIndex]
			link.Token = input.PublicToken
			link.TokenHash = input.PublicTokenHash
			link.TokenPrefix = input.PublicTokenPrefix
			link.CreatedByUserID = &creator
			link.UpdatedAt = now
			link.RevokedAt = nil
		} else {
			m.ThreadPublicLinks = append(m.ThreadPublicLinks, types.ThreadPublicLink{
				ThreadID:        threadID,
				Token:           input.PublicToken,
				TokenHash:       input.PublicTokenHash,
				TokenPrefix:     input.PublicTokenPrefix,
				CreatedByUserID: &creator,
				CreatedAt:       now,
				UpdatedAt:       now,
			})
			activeLinkIndex = len(m.ThreadPublicLinks) - 1
		}
	}

	state := types.ManagedThreadVisibility{
		ThreadID:       threadID,
		OwnerUserID:    thread.OwnerUserID,
		SharedTeams:    m.threadVisibility(*thread).SharedTeams,
		AvailableTeams: availableTeams,
		Public:         activeLinkIndex >= 0,
	}
	if activeLinkIndex >= 0 {
		link := m.ThreadPublicLinks[activeLinkIndex]
		state.PublicLink = &link
	}
	return state, nil
}

func (m *MemoryRepository) AcquirePublicThreadLease(_ context.Context, tokenHash string) (types.PublicThreadAuthorizationLease, error) {
	threadID := ""
	for _, link := range m.ThreadPublicLinks {
		if link.TokenHash == tokenHash && link.RevokedAt == nil {
			threadID = link.ThreadID
			break
		}
	}
	if threadID == "" {
		return nil, nil
	}
	for _, thread := range m.Threads {
		if thread.ID != threadID {
			continue
		}
		messages := []types.Message{}
		for _, message := range m.Messages {
			if message.ThreadID != threadID {
				continue
			}
			copyMessage := message
			copyMessage.Assets = []types.Asset{}
			for _, asset := range m.Assets {
				if asset.MessageID == message.ID {
					copyMessage.Assets = append(copyMessage.Assets, asset)
				}
			}
			messages = append(messages, copyMessage)
		}
		sort.SliceStable(messages, func(i, j int) bool {
			if messages[i].CreatedAt != messages[j].CreatedAt {
				return messages[i].CreatedAt < messages[j].CreatedAt
			}
			return messages[i].ID < messages[j].ID
		})
		return memoryPublicThreadLease{thread: types.ThreadWithMessages{Thread: thread, Messages: messages, Visibility: types.ThreadVisibility{ThreadID: thread.ID, OwnerUserID: thread.OwnerUserID}}}, nil
	}
	return nil, nil
}

func (m *MemoryRepository) AcquirePublicAssetSigningLease(_ context.Context, tokenHash string, assetID string) (types.AssetAuthorizationLease, error) {
	threadID := ""
	for _, link := range m.ThreadPublicLinks {
		if link.TokenHash == tokenHash && link.RevokedAt == nil {
			threadID = link.ThreadID
			break
		}
	}
	if threadID == "" {
		return nil, nil
	}
	messageIDs := map[string]bool{}
	for _, message := range m.Messages {
		if message.ThreadID == threadID {
			messageIDs[message.ID] = true
		}
	}
	for _, asset := range m.Assets {
		if asset.ID == assetID && messageIDs[asset.MessageID] {
			return memoryAssetLease{asset: asset}, nil
		}
	}
	return nil, nil
}

func (m *MemoryRepository) AcquireAssetSigningLease(ctx context.Context, userID string, assetID string) (types.AssetAuthorizationLease, error) {
	asset, err := m.GetAsset(ctx, userID, assetID)
	if err != nil || asset == nil {
		return nil, err
	}
	return memoryAssetLease{asset: *asset}, nil
}

func (m *MemoryRepository) threadVisibility(thread types.Thread) types.ThreadVisibility {
	teamIDs := map[string]struct{}{}
	for _, share := range m.ThreadTeamShares {
		if share.ThreadID == thread.ID {
			teamIDs[share.TeamID] = struct{}{}
		}
	}
	teams := []types.Team{}
	for _, team := range m.Teams {
		if _, ok := teamIDs[team.ID]; ok {
			teams = append(teams, team)
		}
	}
	sort.SliceStable(teams, func(i, j int) bool {
		if !strings.EqualFold(teams[i].Name, teams[j].Name) {
			return strings.ToLower(teams[i].Name) < strings.ToLower(teams[j].Name)
		}
		if !strings.EqualFold(teams[i].Slug, teams[j].Slug) {
			return strings.ToLower(teams[i].Slug) < strings.ToLower(teams[j].Slug)
		}
		return teams[i].ID < teams[j].ID
	})
	return types.ThreadVisibility{ThreadID: thread.ID, OwnerUserID: thread.OwnerUserID, SharedTeams: teams}
}

func (m *MemoryRepository) ListThreads(_ context.Context, userID string, limit int) ([]types.Thread, error) {
	return m.ListThreadsFiltered(context.Background(), userID, types.ThreadListParams{Limit: limit, Filter: types.ThreadFilterAll})
}

func (m *MemoryRepository) ListThreadsFiltered(_ context.Context, userID string, params types.ThreadListParams) ([]types.Thread, error) {
	page, err := m.ListThreadsPage(context.Background(), userID, params)
	return page.Threads, err
}

func (m *MemoryRepository) ListThreadsPage(_ context.Context, userID string, params types.ThreadListParams) (types.ThreadPage, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}
	threads := []types.Thread{}
	for _, thread := range m.Threads {
		if m.normalThreadAccess(thread, userID) == nil {
			continue
		}
		thread.VisibilitySummary = m.threadVisibilitySummary(thread, userID)
		if matchesThreadFilter(thread.VisibilitySummary, params.Filter, params.TeamRef) {
			if !threadAfterPageCursor(thread, params.Cursor) {
				continue
			}
			thread = m.threadWithMessageSummary(thread)
			threads = append(threads, thread)
		}
	}
	sort.Slice(threads, func(i, j int) bool {
		if threads[i].UpdatedAt != threads[j].UpdatedAt {
			return threads[i].UpdatedAt > threads[j].UpdatedAt
		}
		return threads[i].ID < threads[j].ID
	})
	visible := len(threads)
	hasMore := visible > params.Limit
	if hasMore {
		visible = params.Limit
	}
	pageInfo := types.ThreadPageInfo{Limit: params.Limit, HasMore: hasMore}
	if hasMore && visible > 0 {
		updatedAt, err := time.Parse(time.RFC3339Nano, threads[visible-1].UpdatedAt)
		if err != nil {
			return types.ThreadPage{}, err
		}
		next, err := types.EncodeThreadPageCursor(types.ThreadPageCursor{UpdatedAt: updatedAt, ID: threads[visible-1].ID})
		if err != nil {
			return types.ThreadPage{}, err
		}
		pageInfo.NextCursor = &next
	}
	return types.ThreadPage{Threads: threads[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) threadWithMessageSummary(thread types.Thread) types.Thread {
	messageCount := 0
	lastMessageBody := ""
	lastMessageAt := ""
	lastMessageID := ""
	for _, message := range m.Messages {
		if message.ThreadID != thread.ID {
			continue
		}
		messageCount++
		if message.CreatedAt > lastMessageAt || (message.CreatedAt == lastMessageAt && message.ID > lastMessageID) {
			lastMessageAt = message.CreatedAt
			lastMessageID = message.ID
			lastMessageBody = message.Body
		}
	}
	thread.MessageCount = &messageCount
	preview := previewText(lastMessageBody, 180)
	thread.LastMessagePreview = &preview
	return thread
}

func (m *MemoryRepository) SearchThreads(_ context.Context, userID string, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
	page, err := m.SearchThreadsPage(context.Background(), userID, params)
	return page.Threads, err
}

func (m *MemoryRepository) SearchThreadsPage(_ context.Context, userID string, params types.SearchThreadParams) (types.SearchThreadPage, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := strings.ToLower(strings.TrimSpace(params.Query))
	results := []types.SearchThreadResult{}
	threads := append([]types.Thread(nil), m.Threads...)
	sort.Slice(threads, func(i, j int) bool {
		if threads[i].UpdatedAt != threads[j].UpdatedAt {
			return threads[i].UpdatedAt > threads[j].UpdatedAt
		}
		return threads[i].ID < threads[j].ID
	})
	for _, thread := range threads {
		if m.normalThreadAccess(thread, userID) == nil {
			continue
		}
		visibilitySummary := m.threadVisibilitySummary(thread, userID)
		if !matchesThreadFilter(visibilitySummary, params.Filter, params.TeamRef) {
			continue
		}
		if params.CreatedBy != nil && *params.CreatedBy != "" && thread.CreatedBy != *params.CreatedBy {
			continue
		}
		if params.UpdatedAfter != nil && *params.UpdatedAfter != "" && thread.UpdatedAt <= *params.UpdatedAfter {
			continue
		}
		if !threadAfterPageCursor(thread, params.Cursor) {
			continue
		}
		messageCount := 0
		lastBody := ""
		lastAt := ""
		matchedBody := ""
		titleMatches := strings.Contains(strings.ToLower(thread.Title), query)
		for _, message := range m.Messages {
			if message.ThreadID != thread.ID {
				continue
			}
			messageCount++
			if message.CreatedAt >= lastAt {
				lastBody = message.Body
				lastAt = message.CreatedAt
			}
			if matchedBody == "" && strings.Contains(strings.ToLower(message.Body), query) {
				matchedBody = message.Body
			}
		}
		if !titleMatches && matchedBody == "" {
			continue
		}
		results = append(results, types.SearchThreadResult{
			ID:                       thread.ID,
			OwnerUserID:              thread.OwnerUserID,
			Title:                    thread.Title,
			CreatedAt:                thread.CreatedAt,
			UpdatedAt:                thread.UpdatedAt,
			CreatedBy:                thread.CreatedBy,
			CreatedByUserDisplayName: thread.CreatedByUserDisplayName,
			CreatedByActorName:       thread.CreatedByActorName,
			MessageCount:             messageCount,
			LastMessagePreview:       previewText(lastBody, 180),
			MatchedSnippets:          matchedSnippets(params.Query, thread.Title, matchedBody),
			VisibilitySummary:        visibilitySummary,
		})
		if len(results) > params.Limit {
			break
		}
	}
	visible := len(results)
	hasMore := visible > params.Limit
	if hasMore {
		visible = params.Limit
	}
	pageInfo := types.ThreadPageInfo{Limit: params.Limit, HasMore: hasMore}
	if hasMore && visible > 0 {
		updatedAt, err := time.Parse(time.RFC3339Nano, results[visible-1].UpdatedAt)
		if err != nil {
			return types.SearchThreadPage{}, err
		}
		next, err := types.EncodeThreadPageCursor(types.ThreadPageCursor{UpdatedAt: updatedAt, ID: results[visible-1].ID})
		if err != nil {
			return types.SearchThreadPage{}, err
		}
		pageInfo.NextCursor = &next
	}
	return types.SearchThreadPage{Threads: results[:visible], Page: pageInfo}, nil
}

func threadAfterPageCursor(thread types.Thread, cursor *types.ThreadPageCursor) bool {
	if cursor == nil {
		return true
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, thread.UpdatedAt)
	if err != nil {
		return false
	}
	return updatedAt.Before(cursor.UpdatedAt) || (updatedAt.Equal(cursor.UpdatedAt) && thread.ID > cursor.ID)
}

func (m *MemoryRepository) ListOwnerContentThreads(ctx context.Context, ownerUserID string, params types.OwnerContentListParams) ([]types.OwnerContentThreadSummary, error) {
	page, err := m.ListOwnerContentThreadsPage(ctx, ownerUserID, params)
	return page.Threads, err
}

func (m *MemoryRepository) SearchOwnerContentThreads(ctx context.Context, ownerUserID string, params types.OwnerContentSearchParams) ([]types.OwnerContentThreadSummary, error) {
	page, err := m.SearchOwnerContentThreadsPage(ctx, ownerUserID, params)
	return page.Threads, err
}

func (m *MemoryRepository) ListOwnerContentThreadsPage(_ context.Context, ownerUserID string, params types.OwnerContentListParams) (types.OwnerContentThreadPage, error) {
	return m.ownerContentThreadsPage(ownerUserID, "", params.UserID, params.TeamRef, types.PageRequest{Limit: params.Limit, Offset: params.Offset}), nil
}

func (m *MemoryRepository) SearchOwnerContentThreadsPage(_ context.Context, ownerUserID string, params types.OwnerContentSearchParams) (types.OwnerContentThreadPage, error) {
	return m.ownerContentThreadsPage(ownerUserID, params.Query, params.UserID, params.TeamRef, types.PageRequest{Limit: params.Limit, Offset: params.Offset}), nil
}

func (m *MemoryRepository) ownerContentThreadsPage(ownerUserID string, query string, userID string, teamRef string, pageRequest types.PageRequest) types.OwnerContentThreadPage {
	pageRequest = types.NormalizePageRequest(pageRequest)
	queryLower := strings.ToLower(strings.TrimSpace(query))
	threads := append([]types.Thread(nil), m.Threads...)
	sort.SliceStable(threads, func(i, j int) bool {
		if threads[i].UpdatedAt != threads[j].UpdatedAt {
			return threads[i].UpdatedAt > threads[j].UpdatedAt
		}
		return threads[i].ID < threads[j].ID
	})
	all := []types.OwnerContentThreadSummary{}
	for _, thread := range threads {
		if strings.TrimSpace(userID) != "" && thread.OwnerUserID != strings.TrimSpace(userID) {
			continue
		}
		visibility := m.threadVisibilitySummary(thread, ownerUserID)
		visibility.Private = len(visibility.SharedTeams) == 0 && !visibility.Public
		if strings.TrimSpace(teamRef) != "" {
			matchedTeam := false
			for _, team := range visibility.SharedTeams {
				if team.ID == strings.TrimSpace(teamRef) || strings.EqualFold(team.Slug, strings.TrimSpace(teamRef)) {
					matchedTeam = true
					break
				}
			}
			if !matchedTeam {
				continue
			}
		}
		messageCount := 0
		lastBody, lastAt, matchedBody := "", "", ""
		titleMatches := queryLower == "" || strings.Contains(strings.ToLower(thread.Title), queryLower)
		for _, message := range m.Messages {
			if message.ThreadID != thread.ID {
				continue
			}
			messageCount++
			if message.CreatedAt >= lastAt {
				lastBody, lastAt = message.Body, message.CreatedAt
			}
			if queryLower != "" && matchedBody == "" && strings.Contains(strings.ToLower(message.Body), queryLower) {
				matchedBody = message.Body
			}
		}
		if !titleMatches && matchedBody == "" {
			continue
		}
		owner := types.User{}
		for _, candidate := range m.Users {
			if candidate.ID == thread.OwnerUserID {
				owner = candidate
				break
			}
		}
		thread.VisibilitySummary = visibility
		all = append(all, types.OwnerContentThreadSummary{Thread: thread, Owner: owner, MessageCount: messageCount, LastMessagePreview: previewText(lastBody, 180), MatchedSnippets: matchedSnippets(query, thread.Title, matchedBody)})
	}
	start := pageRequest.Offset
	if start > len(all) {
		start = len(all)
	}
	end := start + pageRequest.Limit + 1
	if end > len(all) {
		end = len(all)
	}
	window := all[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.OwnerContentThreadPage{Threads: window[:visible], Page: pageInfo}
}

func (m *MemoryRepository) GetOwnerContentThread(_ context.Context, ownerUserID string, threadID string) (*types.OwnerContentThreadDetail, error) {
	for _, thread := range m.Threads {
		if thread.ID != strings.TrimSpace(threadID) {
			continue
		}
		owner := types.User{}
		for _, candidate := range m.Users {
			if candidate.ID == thread.OwnerUserID {
				owner = candidate
				break
			}
		}
		messages := []types.Message{}
		for _, message := range m.Messages {
			if message.ThreadID != thread.ID {
				continue
			}
			assets := []types.Asset{}
			for _, asset := range m.Assets {
				if asset.MessageID == message.ID {
					copyAsset := asset
					copyAsset.DownloadURL = nil
					assets = append(assets, copyAsset)
				}
			}
			message.Assets = assets
			messages = append(messages, message)
		}
		sort.SliceStable(messages, func(i, j int) bool {
			if messages[i].CreatedAt != messages[j].CreatedAt {
				return messages[i].CreatedAt < messages[j].CreatedAt
			}
			return messages[i].ID < messages[j].ID
		})
		thread.VisibilitySummary = m.threadVisibilitySummary(thread, ownerUserID)
		thread.VisibilitySummary.Private = len(thread.VisibilitySummary.SharedTeams) == 0 && !thread.VisibilitySummary.Public
		return &types.OwnerContentThreadDetail{
			Thread:     thread,
			Owner:      owner,
			Messages:   messages,
			Visibility: m.threadVisibility(thread),
		}, nil
	}
	return nil, nil
}

func (m *MemoryRepository) GetOwnerContentAsset(_ context.Context, assetID string) (*types.Asset, error) {
	for _, asset := range m.Assets {
		if asset.ID == strings.TrimSpace(assetID) {
			copyAsset := asset
			copyAsset.DownloadURL = nil
			return &copyAsset, nil
		}
	}
	return nil, nil
}

func (m *MemoryRepository) CreateThread(_ context.Context, userID string, title string, auth types.AuthContext) (types.Thread, error) {
	now := isoMillis(time.Now())
	thread := types.Thread{
		ID:                       "thr_" + uuid.NewString(),
		OwnerUserID:              userID,
		Title:                    title,
		CreatedAt:                now,
		UpdatedAt:                now,
		CreatedBy:                auth.ActorName,
		CreatedByUserID:          optionalString(auth.UserID),
		CreatedByKeyID:           optionalString(auth.KeyID),
		CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
		CreatedByActorName:       optionalString(auth.ActorName),
		VisibilitySummary:        newPrivateThreadVisibilitySummary(),
	}
	m.Threads = append(m.Threads, thread)
	return thread, nil
}

func (m *MemoryRepository) CreateThreadWithMessage(_ context.Context, userID string, title string, auth types.AuthContext, body string, bodyContentType *string) (types.Thread, types.Message, error) {
	now := isoMillis(time.Now())
	thread := types.Thread{
		ID:                       "thr_" + uuid.NewString(),
		OwnerUserID:              userID,
		Title:                    title,
		CreatedAt:                now,
		UpdatedAt:                now,
		CreatedBy:                auth.ActorName,
		CreatedByUserID:          optionalString(auth.UserID),
		CreatedByKeyID:           optionalString(auth.KeyID),
		CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
		CreatedByActorName:       optionalString(auth.ActorName),
		VisibilitySummary:        newPrivateThreadVisibilitySummary(),
	}
	message := types.Message{
		ID:                       "msg_" + uuid.NewString(),
		ThreadID:                 thread.ID,
		Author:                   auth.ActorName,
		Body:                     body,
		BodyContentType:          bodyContentType,
		CreatedAt:                now,
		Assets:                   []types.Asset{},
		CreatedByUserID:          optionalString(auth.UserID),
		CreatedByKeyID:           optionalString(auth.KeyID),
		CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
		CreatedByActorName:       optionalString(auth.ActorName),
	}
	m.Threads = append(m.Threads, thread)
	m.Messages = append(m.Messages, message)
	return thread, message, nil
}

func (m *MemoryRepository) GetThread(_ context.Context, userID string, threadID string) (*types.ThreadWithMessages, error) {
	for _, thread := range m.Threads {
		if thread.ID != threadID || m.normalThreadAccess(thread, userID) == nil {
			continue
		}
		messages := []types.Message{}
		for _, message := range m.Messages {
			if message.ThreadID != threadID {
				continue
			}
			assets := []types.Asset{}
			for _, asset := range m.Assets {
				if asset.MessageID == message.ID {
					copy := asset
					copy.DownloadURL = nil
					assets = append(assets, copy)
				}
			}
			message.Assets = assets
			messages = append(messages, message)
		}
		sort.Slice(messages, func(i, j int) bool {
			return messages[i].CreatedAt < messages[j].CreatedAt
		})
		thread.VisibilitySummary = m.threadVisibilitySummary(thread, userID)
		return &types.ThreadWithMessages{Thread: thread, Messages: messages, Visibility: m.threadVisibility(thread)}, nil
	}
	return nil, nil
}

func (m *MemoryRepository) threadVisibilitySummary(thread types.Thread, userID string) types.ThreadVisibilitySummary {
	visibility := m.threadVisibility(thread)
	sharedTeams := make([]types.ThreadTeamSummary, 0, len(visibility.SharedTeams))
	matchedTeams := []types.ThreadTeamSummary{}
	membershipIDs := map[string]struct{}{}
	for _, membership := range m.TeamMemberships {
		if membership.UserID == userID {
			membershipIDs[membership.TeamID] = struct{}{}
		}
	}
	for _, team := range visibility.SharedTeams {
		summary := types.ThreadTeamSummary{ID: team.ID, Slug: team.Slug, Name: team.Name}
		sharedTeams = append(sharedTeams, summary)
		if _, matched := membershipIDs[team.ID]; matched {
			matchedTeams = append(matchedTeams, summary)
		}
	}
	isPublic := false
	for _, link := range m.ThreadPublicLinks {
		if link.ThreadID == thread.ID && link.RevokedAt == nil {
			isPublic = true
			break
		}
	}
	ownedByMe := thread.OwnerUserID == userID
	return types.ThreadVisibilitySummary{
		OwnedByMe:    ownedByMe,
		Private:      ownedByMe && len(sharedTeams) == 0 && !isPublic,
		SharedWithMe: !ownedByMe && len(matchedTeams) > 0,
		SharedTeams:  sharedTeams,
		MatchedTeams: matchedTeams,
		Public:       isPublic,
	}
}

func matchesThreadFilter(summary types.ThreadVisibilitySummary, filter string, teamRef string) bool {
	switch filter {
	case "", types.ThreadFilterAll:
		return true
	case types.ThreadFilterPrivate:
		return summary.Private
	case types.ThreadFilterShared:
		return summary.SharedWithMe
	case types.ThreadFilterPublic:
		return summary.Public
	case types.ThreadFilterTeam:
		for _, team := range summary.MatchedTeams {
			if team.ID == teamRef || strings.EqualFold(team.Slug, teamRef) {
				return true
			}
		}
	}
	return false
}

func (m *MemoryRepository) GetAsset(_ context.Context, userID string, assetID string) (*types.Asset, error) {
	for _, asset := range m.Assets {
		if asset.ID != assetID {
			continue
		}
		for _, message := range m.Messages {
			if message.ID != asset.MessageID {
				continue
			}
			for _, thread := range m.Threads {
				if thread.ID == message.ThreadID && m.normalThreadAccess(thread, userID) != nil {
					copy := asset
					copy.DownloadURL = nil
					return &copy, nil
				}
			}
		}
	}
	return nil, nil
}

func (m *MemoryRepository) AcquireAttachmentPurgeLease(_ context.Context, userID string) (types.AttachmentPurgeLease, error) {
	m.purgeMutex.Lock()
	for _, user := range m.Users {
		if user.ID != strings.TrimSpace(userID) {
			continue
		}
		if user.IsOwner {
			m.purgeMutex.Unlock()
			return nil, types.ErrOwnerCannotBeDisabled
		}
		if user.DisabledAt == nil {
			m.purgeMutex.Unlock()
			return nil, types.ErrUserMustBeDisabled
		}
		return &memoryAttachmentPurgeLease{mutex: &m.purgeMutex}, nil
	}
	m.purgeMutex.Unlock()
	return nil, types.ErrUserNotFound
}

func (m *MemoryRepository) ListAssetPurgeCandidates(_ context.Context, uploaderUserID string, limit int) ([]types.AssetPurgeCandidate, error) {
	if limit < 1 {
		limit = 25
	}
	candidates := []types.AssetPurgeCandidate{}
	for _, asset := range m.Assets {
		if asset.CreatedByUserID == nil || *asset.CreatedByUserID != strings.TrimSpace(uploaderUserID) || asset.PurgedAt != nil {
			continue
		}
		candidates = append(candidates, types.AssetPurgeCandidate{AssetID: asset.ID, StorageKey: asset.StorageKey})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].AssetID < candidates[j].AssetID
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

func (m *MemoryRepository) MarkAssetPurged(_ context.Context, assetID string, ownerUserID string) (bool, error) {
	now := isoMillis(time.Now().UTC())
	for index := range m.Assets {
		asset := &m.Assets[index]
		if asset.ID != strings.TrimSpace(assetID) {
			continue
		}
		if asset.PurgedAt == nil {
			asset.PurgedAt = &now
		}
		if asset.PurgedByUserID == nil {
			asset.PurgedByUserID = optionalString(ownerUserID)
		}
		asset.PurgeLastAttemptAt = &now
		asset.PurgeError = nil
		return true, nil
	}
	return false, nil
}

func (m *MemoryRepository) MarkAssetPurgeFailure(_ context.Context, assetID string, message string) error {
	now := isoMillis(time.Now().UTC())
	for index := range m.Assets {
		asset := &m.Assets[index]
		if asset.ID == strings.TrimSpace(assetID) && asset.PurgedAt == nil {
			asset.PurgeLastAttemptAt = &now
			asset.PurgeError = optionalString(strings.TrimSpace(message))
			return nil
		}
	}
	return nil
}

func (m *MemoryRepository) CountUnpurgedAssetsByUploader(_ context.Context, uploaderUserID string) (int, error) {
	count := 0
	for _, asset := range m.Assets {
		if asset.CreatedByUserID != nil && *asset.CreatedByUserID == strings.TrimSpace(uploaderUserID) && asset.PurgedAt == nil {
			count++
		}
	}
	return count, nil
}

func (m *MemoryRepository) CreatePendingUpload(ctx context.Context, userID string, upload types.PendingUpload) (types.PendingUpload, error) {
	created, err := m.CreatePendingUploads(ctx, userID, []types.PendingUpload{upload})
	if err != nil {
		return types.PendingUpload{}, err
	}
	return created[0], nil
}

func (m *MemoryRepository) CreatePendingUploads(ctx context.Context, userID string, uploads []types.PendingUpload) ([]types.PendingUpload, error) {
	if len(uploads) == 0 {
		return []types.PendingUpload{}, nil
	}
	access, _ := m.ResolveThreadAccess(ctx, userID, uploads[0].ThreadID)
	if access == nil {
		return nil, types.ErrThreadNotFound
	}
	activeCount := 0
	nowTime := time.Now().UTC()
	for _, existing := range m.Pending {
		expiresAt, _ := time.Parse(time.RFC3339, existing.ExpiresAt)
		if existing.CreatedByUserID != nil && *existing.CreatedByUserID == userID && existing.ConsumedAt == nil && (existing.Status == "pending" || existing.Status == "finalizing") && expiresAt.After(nowTime) {
			activeCount++
		}
	}
	if activeCount+len(uploads) > 100 {
		return nil, types.ErrPendingUploadQuotaExceeded
	}
	seenIDs := map[string]bool{}
	seenKeys := map[string]bool{}
	for _, existing := range m.Pending {
		seenIDs[existing.ID] = true
		seenKeys[existing.StorageKey] = true
	}
	for _, upload := range uploads {
		if upload.ThreadID != uploads[0].ThreadID || seenIDs[upload.ID] || seenKeys[upload.StorageKey] {
			return nil, errors.New("pending upload batch conflicts with existing state")
		}
		seenIDs[upload.ID] = true
		seenKeys[upload.StorageKey] = true
	}
	created := make([]types.PendingUpload, 0, len(uploads))
	now := isoMillis(nowTime)
	for _, upload := range uploads {
		upload.CreatedAt = now
		upload.Status = "pending"
		if upload.ExpiresAt == "" {
			upload.ExpiresAt = isoMillis(nowTime.Add(15 * time.Minute))
		}
		m.Pending = append(m.Pending, upload)
		expiresAt, _ := time.Parse(time.RFC3339, upload.ExpiresAt)
		m.UploadCleanup = append(m.UploadCleanup, memoryUploadCleanup{
			Candidate: types.UploadCleanupCandidate{ID: "ucl_" + uuid.NewString(), UploadID: upload.ID, StorageKey: upload.StorageKey, ObjectKind: "staging"},
			NotBefore: expiresAt,
		})
		created = append(created, upload)
	}
	return created, nil
}

func (m *MemoryRepository) GetPendingUploads(_ context.Context, userID string, threadID string, uploadIDs []string, actor types.AuthContext) ([]types.PendingUpload, error) {
	wanted := map[string]bool{}
	for _, id := range uploadIDs {
		wanted[id] = true
	}
	uploads := []types.PendingUpload{}
	for _, upload := range m.Pending {
		if upload.ThreadID == threadID && pendingUploadOwnedBy(upload, actor) && wanted[upload.ID] {
			access, _ := m.ResolveThreadAccess(context.Background(), userID, threadID)
			if access == nil {
				continue
			}
			uploads = append(uploads, upload)
		}
	}
	return uploads, nil
}

func (m *MemoryRepository) MarkPendingUploadsConsumed(_ context.Context, userID string, threadID string, uploadIDs []string, actor types.AuthContext) error {
	wanted := map[string]bool{}
	for _, id := range uploadIDs {
		wanted[id] = true
	}
	now := isoMillis(time.Now())
	access, _ := m.ResolveThreadAccess(context.Background(), userID, threadID)
	if access == nil {
		return types.ErrThreadNotFound
	}
	for i := range m.Pending {
		if m.Pending[i].ThreadID == threadID && pendingUploadOwnedBy(m.Pending[i], actor) && wanted[m.Pending[i].ID] {
			m.Pending[i].ConsumedAt = &now
		}
	}
	return nil
}

func (m *MemoryRepository) ClaimPendingUploadsForFinalization(_ context.Context, userID string, threadID string, actor types.AuthContext, token string, targets []types.UploadFinalizationTarget) ([]types.PendingUpload, error) {
	if !m.liveActor(actor) || m.normalThreadAccessByID(threadID, userID) == nil {
		return nil, types.ErrThreadNotFound
	}
	claimedIndexes := make([]int, 0, len(targets))
	claimed := make([]types.PendingUpload, 0, len(targets))
	for _, target := range targets {
		found := -1
		for index := range m.Pending {
			upload := m.Pending[index]
			if upload.ID == target.UploadID && upload.ThreadID == threadID && pendingUploadOwnedBy(upload, actor) {
				found = index
				break
			}
		}
		if found < 0 {
			return nil, types.ErrPendingUploadUnavailable
		}
		upload := m.Pending[found]
		if upload.Status == "finalizing" {
			return nil, types.ErrPendingUploadFinalizing
		}
		expiresAt, _ := time.Parse(time.RFC3339, upload.ExpiresAt)
		if upload.Status != "pending" || upload.ConsumedAt != nil || upload.ExpectedSHA256 == "" || !expiresAt.After(time.Now().UTC()) {
			return nil, types.ErrPendingUploadUnavailable
		}
		claimedIndexes = append(claimedIndexes, found)
		upload.Status = "finalizing"
		upload.FinalStorageKey = target.FinalStorageKey
		upload.FinalizationToken = token
		claimed = append(claimed, upload)
	}
	now := isoMillis(time.Now().UTC())
	for position, index := range claimedIndexes {
		m.Pending[index] = claimed[position]
		m.Pending[index].FinalizationStartedAt = &now
		m.UploadCleanup = append(m.UploadCleanup, memoryUploadCleanup{
			Candidate: types.UploadCleanupCandidate{ID: "ucl_" + uuid.NewString(), UploadID: claimed[position].ID, StorageKey: claimed[position].FinalStorageKey, ObjectKind: "final_candidate"},
			NotBefore: time.Now().UTC().Add(10 * time.Minute),
		})
	}
	return claimed, nil
}

func (m *MemoryRepository) ReleasePendingUploadsFinalization(_ context.Context, userID string, threadID string, actor types.AuthContext, token string, uploadIDs []string, rejectReason string) error {
	wanted := map[string]bool{}
	for _, id := range uploadIDs {
		wanted[id] = true
	}
	now := time.Now().UTC()
	nowText := isoMillis(now)
	for index := range m.Pending {
		upload := &m.Pending[index]
		if !wanted[upload.ID] || upload.ThreadID != threadID || !pendingUploadOwnedBy(*upload, actor) || upload.FinalizationToken != token || upload.Status != "finalizing" {
			continue
		}
		upload.Status = "pending"
		if strings.TrimSpace(rejectReason) != "" {
			upload.Status = "rejected"
			upload.RejectedAt = &nowText
			upload.RejectionReason = strings.TrimSpace(rejectReason)
		}
		upload.FinalStorageKey = ""
		upload.FinalizationToken = ""
		upload.FinalizationStartedAt = nil
	}
	if strings.TrimSpace(rejectReason) != "" {
		for index := range m.UploadCleanup {
			if wanted[m.UploadCleanup[index].Candidate.UploadID] && m.UploadCleanup[index].Candidate.ObjectKind == "staging" && m.UploadCleanup[index].CleanedAt == nil {
				m.UploadCleanup[index].NotBefore = now
			}
		}
	}
	return nil
}

func (m *MemoryRepository) PostMessage(ctx context.Context, userID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, newAssets []types.NewAsset) (types.Message, error) {
	return m.postMessageUnchecked(ctx, userID, threadID, auth, body, bodyContentType, newAssets)
}

func (m *MemoryRepository) PostMessageWithFinalizedUploads(ctx context.Context, userID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, newAssets []types.NewAsset, finalizedUploads []types.NewAsset, pendingUploadIDs []string, token string) (types.Message, error) {
	if !m.liveActor(auth) || len(finalizedUploads) != len(pendingUploadIDs) {
		return types.Message{}, types.ErrPendingUploadUnavailable
	}
	pendingIndexes := make([]int, 0, len(pendingUploadIDs))
	for position, uploadID := range pendingUploadIDs {
		found := -1
		for index := range m.Pending {
			upload := m.Pending[index]
			if upload.ID == uploadID && upload.ThreadID == threadID && pendingUploadOwnedBy(upload, auth) {
				found = index
				break
			}
		}
		if found < 0 {
			return types.Message{}, types.ErrPendingUploadUnavailable
		}
		upload := m.Pending[found]
		asset := finalizedUploads[position]
		if upload.Status != "finalizing" || upload.ConsumedAt != nil || upload.FinalizationToken != token || upload.FinalStorageKey != asset.StorageKey || upload.FileName != asset.FileName || upload.SizeBytes != asset.SizeBytes || upload.ExpectedSHA256 != asset.ContentSHA256 || !sameOptionalString(upload.MimeType, asset.MimeType) {
			return types.Message{}, types.ErrPendingUploadUnavailable
		}
		pendingIndexes = append(pendingIndexes, found)
	}
	allAssets := append(append([]types.NewAsset(nil), newAssets...), finalizedUploads...)
	message, err := m.postMessageUnchecked(ctx, userID, threadID, auth, body, bodyContentType, allAssets)
	if err != nil {
		return types.Message{}, err
	}
	consumedAt := isoMillis(time.Now().UTC())
	finalKeys := map[string]bool{}
	for position, index := range pendingIndexes {
		m.Pending[index].Status = "finalized"
		m.Pending[index].ConsumedAt = &consumedAt
		m.Pending[index].FinalizationToken = ""
		finalKeys[finalizedUploads[position].StorageKey] = true
	}
	for index := range m.UploadCleanup {
		if finalKeys[m.UploadCleanup[index].Candidate.StorageKey] && m.UploadCleanup[index].Candidate.ObjectKind == "final_candidate" {
			now := time.Now().UTC()
			m.UploadCleanup[index].CleanedAt = &now
		}
	}
	return message, nil
}

func (m *MemoryRepository) postMessageUnchecked(_ context.Context, userID string, threadID string, auth types.AuthContext, body string, bodyContentType *string, newAssets []types.NewAsset) (types.Message, error) {
	var threadIndex = -1
	for i, thread := range m.Threads {
		if thread.ID == threadID && m.normalThreadAccess(thread, userID) != nil {
			threadIndex = i
			break
		}
	}
	if threadIndex < 0 {
		return types.Message{}, types.ErrThreadNotFound
	}
	now := isoMillis(time.Now())
	message := types.Message{
		ID:                       "msg_" + uuid.NewString(),
		ThreadID:                 threadID,
		Author:                   auth.ActorName,
		Body:                     body,
		BodyContentType:          bodyContentType,
		CreatedAt:                now,
		Assets:                   []types.Asset{},
		CreatedByUserID:          optionalString(auth.UserID),
		CreatedByKeyID:           optionalString(auth.KeyID),
		CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
		CreatedByActorName:       optionalString(auth.ActorName),
	}
	m.Messages = append(m.Messages, message)
	m.Threads[threadIndex].UpdatedAt = isoMillis(time.Now())
	for _, asset := range newAssets {
		createdAsset := types.Asset{
			ID:                       "asset_" + uuid.NewString(),
			MessageID:                message.ID,
			StorageKey:               asset.StorageKey,
			FileName:                 asset.FileName,
			Filename:                 asset.FileName,
			MimeType:                 asset.MimeType,
			SizeBytes:                asset.SizeBytes,
			DownloadURL:              nil,
			CreatedAt:                now,
			CreatedBy:                auth.ActorName,
			CreatedByUserID:          optionalString(auth.UserID),
			CreatedByKeyID:           optionalString(auth.KeyID),
			CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
			CreatedByActorName:       optionalString(auth.ActorName),
		}
		m.Assets = append(m.Assets, createdAsset)
		message.Assets = append(message.Assets, createdAsset)
	}
	return message, nil
}

func (m *MemoryRepository) ListUploadCleanupCandidates(_ context.Context, limit int) ([]types.UploadCleanupCandidate, error) {
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	now := time.Now().UTC()

	// Mirror the PostgreSQL stale-claim recovery used by the bounded cleanup
	// worker. This keeps memory tests honest about crash recovery instead of
	// allowing a claimed upload to remain permanently unfinalizable.
	recovered := 0
	for index := range m.Pending {
		if recovered == limit {
			break
		}
		upload := &m.Pending[index]
		if upload.Status != "finalizing" || upload.ConsumedAt != nil || upload.FinalizationStartedAt == nil {
			continue
		}
		startedAt, err := time.Parse(time.RFC3339, *upload.FinalizationStartedAt)
		if err != nil || startedAt.After(now.Add(-10*time.Minute)) {
			continue
		}
		expiresAt, _ := time.Parse(time.RFC3339, upload.ExpiresAt)
		if expiresAt.After(now) {
			upload.Status = "pending"
		} else {
			upload.Status = "rejected"
			rejectedAt := isoMillis(now)
			upload.RejectedAt = &rejectedAt
			if strings.TrimSpace(upload.RejectionReason) == "" {
				upload.RejectionReason = "Finalization expired before completion."
			}
		}
		upload.FinalStorageKey = ""
		upload.FinalizationToken = ""
		upload.FinalizationStartedAt = nil
		recovered++
	}

	result := []types.UploadCleanupCandidate{}
	for _, cleanup := range m.UploadCleanup {
		if cleanup.CleanedAt != nil || cleanup.NotBefore.After(now) || m.assetUsesStorageKey(cleanup.Candidate.StorageKey) {
			continue
		}
		result = append(result, cleanup.Candidate)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (m *MemoryRepository) MarkUploadCleanupSuccess(_ context.Context, cleanupID string) error {
	for index := range m.UploadCleanup {
		if m.UploadCleanup[index].Candidate.ID == cleanupID {
			now := time.Now().UTC()
			m.UploadCleanup[index].CleanedAt = &now
			m.UploadCleanup[index].AttemptCount++
			m.UploadCleanup[index].LastError = ""
		}
	}
	return nil
}

func (m *MemoryRepository) MarkUploadCleanupFailure(_ context.Context, cleanupID string, message string) error {
	for index := range m.UploadCleanup {
		if m.UploadCleanup[index].Candidate.ID == cleanupID && m.UploadCleanup[index].CleanedAt == nil {
			m.UploadCleanup[index].AttemptCount++
			m.UploadCleanup[index].LastError = strings.TrimSpace(message)
		}
	}
	return nil
}

func (m *MemoryRepository) liveActor(auth types.AuthContext) bool {
	if len(m.Users) == 0 {
		return true
	}
	var user *types.User
	for index := range m.Users {
		if m.Users[index].ID == auth.UserID {
			user = &m.Users[index]
			break
		}
	}
	if user == nil || user.DisabledAt != nil {
		return false
	}
	if auth.KeyID != "" {
		for _, key := range m.APIKeys {
			if key.ID == auth.KeyID {
				return key.UserID == auth.UserID && key.RevokedAt == nil
			}
		}
		return true
	}
	if auth.SessionID != "" {
		for _, session := range m.Sessions {
			if session.ID == auth.SessionID {
				expiresAt, _ := time.Parse(time.RFC3339, session.ExpiresAt)
				return session.UserID == auth.UserID && session.RevokedAt == nil && expiresAt.After(time.Now().UTC())
			}
		}
		return true
	}
	return true
}

func (m *MemoryRepository) normalThreadAccessByID(threadID string, userID string) *types.ThreadAccess {
	for _, thread := range m.Threads {
		if thread.ID == threadID {
			return m.normalThreadAccess(thread, userID)
		}
	}
	return nil
}

func (m *MemoryRepository) assetUsesStorageKey(storageKey string) bool {
	for _, asset := range m.Assets {
		if asset.StorageKey == storageKey && asset.PurgedAt == nil {
			return true
		}
	}
	return false
}

func (m *MemoryRepository) CreateAPIKey(_ context.Context, userID string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string) (types.APIKey, error) {
	now := isoMillis(time.Now())
	if strings.TrimSpace(userID) == "" {
		return types.APIKey{}, errors.New("user ID is required")
	}
	userExists := false
	for _, user := range m.Users {
		if user.ID == userID {
			userExists = true
			break
		}
	}
	if !userExists {
		m.Users = append(m.Users, types.User{
			ID:          userID,
			Email:       userID + "@example.invalid",
			DisplayName: userID,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	created := types.APIKey{
		ID:          "key_" + uuid.NewString(),
		UserID:      userID,
		Name:        name,
		Purpose:     purpose,
		KeyMasked:   maskSecret(tokenPrefix),
		TokenPrefix: tokenPrefix,
		TokenHash:   tokenHash,
		Scopes:      append([]string(nil), scopes...),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	for i := range m.APIKeys {
		if m.APIKeys[i].UserID == userID && strings.EqualFold(m.APIKeys[i].Name, name) && m.APIKeys[i].RevokedAt == nil {
			created.ID = m.APIKeys[i].ID
			created.CreatedAt = m.APIKeys[i].CreatedAt
			m.APIKeys[i] = created
			return created, nil
		}
	}
	m.APIKeys = append(m.APIKeys, created)
	sort.Slice(m.APIKeys, func(i, j int) bool {
		return m.APIKeys[i].Name < m.APIKeys[j].Name
	})
	return created, nil
}

func (m *MemoryRepository) CreateRaycastAPIKey(ctx context.Context, userID string, name string, tokenHash string, tokenPrefix string, scopes []string, setupBaseURL string) (types.APIKey, error) {
	for _, key := range m.APIKeys {
		if key.UserID == strings.TrimSpace(userID) && strings.EqualFold(key.Name, strings.TrimSpace(name)) && key.RevokedAt == nil {
			return types.APIKey{}, types.ErrCredentialLabelConflict
		}
	}
	created, err := m.CreateAPIKey(ctx, userID, name, "raycast", tokenHash, tokenPrefix, scopes)
	if err != nil {
		return types.APIKey{}, err
	}
	if m.RaycastSetupURLs == nil {
		m.RaycastSetupURLs = map[string]string{}
	}
	m.RaycastSetupURLs[created.ID] = strings.TrimRight(strings.TrimSpace(setupBaseURL), "/")
	return created, nil
}

func (m *MemoryRepository) raycastSetupBaseURL(keyID string) string {
	if m.RaycastSetupURLs == nil {
		return ""
	}
	return m.RaycastSetupURLs[keyID]
}

func (m *MemoryRepository) CreateOnboardingCredential(ctx context.Context, userID string, connector string, name string, purpose string, tokenHash string, tokenPrefix string, scopes []string, rotate bool) (types.APIKey, types.OnboardingState, error) {
	if !rotate {
		state, err := m.GetOnboardingState(ctx, userID)
		if err != nil {
			return types.APIKey{}, types.OnboardingState{}, err
		}
		for _, step := range state.Steps {
			if step.Connector == connector && step.Credential != nil {
				return types.APIKey{}, types.OnboardingState{}, types.ErrOnboardingCredentialExists
			}
		}
	}
	created, err := m.CreateAPIKey(ctx, userID, name, purpose, tokenHash, tokenPrefix, scopes)
	if err != nil {
		return types.APIKey{}, types.OnboardingState{}, err
	}
	now := isoMillis(time.Now().UTC())
	index := m.onboardingIndex(userID)
	if index < 0 {
		m.Onboarding = append(m.Onboarding, types.OnboardingState{UserID: userID, CreatedAt: &now, UpdatedAt: &now, Steps: []types.OnboardingStep{}})
		index = len(m.Onboarding) - 1
	}
	state := &m.Onboarding[index]
	state.DismissedAt = nil
	state.UpdatedAt = &now
	updated := false
	for stepIndex := range state.Steps {
		if state.Steps[stepIndex].Connector != connector {
			continue
		}
		state.Steps[stepIndex].Credential = &created
		state.Steps[stepIndex].UpdatedAt = &now
		if state.Steps[stepIndex].CompletedAt == nil {
			state.Steps[stepIndex].CompletedAt = &now
		}
		updated = true
		break
	}
	if !updated {
		state.Steps = append(state.Steps, types.OnboardingStep{Connector: connector, CompletedAt: &now, UpdatedAt: &now, Credential: &created})
	}
	sort.SliceStable(state.Steps, func(i, j int) bool {
		return onboardingConnectorOrder(state.Steps[i].Connector) < onboardingConnectorOrder(state.Steps[j].Connector)
	})
	return created, m.onboardingStateCopy(*state), nil
}

func (m *MemoryRepository) GetOnboardingState(_ context.Context, userID string) (types.OnboardingState, error) {
	index := m.onboardingIndex(userID)
	if index < 0 {
		return types.OnboardingState{UserID: userID, Steps: []types.OnboardingStep{}}, nil
	}
	return m.onboardingStateCopy(m.Onboarding[index]), nil
}

func (m *MemoryRepository) DismissOnboarding(_ context.Context, userID string) (types.OnboardingState, error) {
	now := isoMillis(time.Now().UTC())
	index := m.onboardingIndex(userID)
	if index < 0 {
		m.Onboarding = append(m.Onboarding, types.OnboardingState{UserID: userID, CreatedAt: &now, UpdatedAt: &now, DismissedAt: &now, Steps: []types.OnboardingStep{}})
		index = len(m.Onboarding) - 1
	} else {
		m.Onboarding[index].DismissedAt = &now
		m.Onboarding[index].UpdatedAt = &now
	}
	return m.onboardingStateCopy(m.Onboarding[index]), nil
}

func (m *MemoryRepository) onboardingIndex(userID string) int {
	for index := range m.Onboarding {
		if m.Onboarding[index].UserID == userID {
			return index
		}
	}
	return -1
}

func (m *MemoryRepository) onboardingStateCopy(state types.OnboardingState) types.OnboardingState {
	copyState := state
	copyState.Steps = make([]types.OnboardingStep, 0, len(state.Steps))
	activeKeys := map[string]types.APIKey{}
	for _, key := range m.APIKeys {
		if key.UserID == state.UserID && key.RevokedAt == nil {
			activeKeys[key.ID] = key
		}
	}
	for _, step := range state.Steps {
		copyStep := step
		copyStep.Credential = nil
		if step.Credential != nil {
			if key, ok := activeKeys[step.Credential.ID]; ok {
				keyCopy := key
				copyStep.Credential = &keyCopy
			}
		}
		copyState.Steps = append(copyState.Steps, copyStep)
	}
	return copyState
}

func onboardingConnectorOrder(connector string) int {
	switch connector {
	case "chatgpt":
		return 1
	case "claude":
		return 2
	case "local":
		return 3
	case "raycast":
		return 4
	default:
		return 5
	}
}

func (m *MemoryRepository) ListAPIKeys(_ context.Context, userID string) ([]types.APIKey, error) {
	keys := []types.APIKey{}
	for _, key := range m.APIKeys {
		if key.UserID == userID && key.RevokedAt == nil {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Name < keys[j].Name
	})
	return keys, nil
}

func (m *MemoryRepository) ListAPIKeysPage(_ context.Context, userID string, pageRequest types.PageRequest) (types.APIKeyPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	keys := []types.APIKey{}
	for _, key := range m.APIKeys {
		if key.UserID == strings.TrimSpace(userID) {
			keys = append(keys, key)
		}
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].CreatedAt != keys[j].CreatedAt {
			return keys[i].CreatedAt > keys[j].CreatedAt
		}
		return keys[i].ID > keys[j].ID
	})
	start := pageRequest.Offset
	if start > len(keys) {
		start = len(keys)
	}
	end := start + pageRequest.Limit + 1
	if end > len(keys) {
		end = len(keys)
	}
	window := keys[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.APIKeyPage{Credentials: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) ListAllAPIKeys(ctx context.Context) ([]types.APIKey, error) {
	page, err := m.ListAllAPIKeysPage(ctx, types.PageRequest{})
	return page.Credentials, err
}

func (m *MemoryRepository) ListAllAPIKeysPage(_ context.Context, pageRequest types.PageRequest) (types.APIKeyPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	keys := append([]types.APIKey(nil), m.APIKeys...)
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].CreatedAt != keys[j].CreatedAt {
			return keys[i].CreatedAt > keys[j].CreatedAt
		}
		return keys[i].ID > keys[j].ID
	})
	start := pageRequest.Offset
	if start > len(keys) {
		start = len(keys)
	}
	end := start + pageRequest.Limit + 1
	if end > len(keys) {
		end = len(keys)
	}
	window := keys[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.APIKeyPage{Credentials: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) RevokeAPIKey(_ context.Context, userID string, name string) (bool, error) {
	now := isoMillis(time.Now())
	for i, key := range m.APIKeys {
		if key.UserID == userID && strings.EqualFold(key.Name, name) && key.RevokedAt == nil {
			m.APIKeys[i].RevokedAt = &now
			m.APIKeys[i].UpdatedAt = now
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryRepository) RevokeAPIKeyForUserByID(_ context.Context, userID string, keyID string) (bool, error) {
	now := isoMillis(time.Now().UTC())
	for index := range m.APIKeys {
		if m.APIKeys[index].UserID != strings.TrimSpace(userID) || m.APIKeys[index].ID != strings.TrimSpace(keyID) {
			continue
		}
		if m.APIKeys[index].RevokedAt == nil {
			m.APIKeys[index].RevokedAt = &now
		}
		m.APIKeys[index].UpdatedAt = now
		return true, nil
	}
	return false, nil
}

func (m *MemoryRepository) RevokeAPIKeyByID(_ context.Context, keyID string) (bool, error) {
	now := isoMillis(time.Now().UTC())
	for index := range m.APIKeys {
		if m.APIKeys[index].ID != strings.TrimSpace(keyID) {
			continue
		}
		if m.APIKeys[index].RevokedAt == nil {
			m.APIKeys[index].RevokedAt = &now
		}
		m.APIKeys[index].UpdatedAt = now
		return true, nil
	}
	return false, nil
}

func (m *MemoryRepository) RotateAPIKeyForUserByID(_ context.Context, userID string, keyID string, tokenHash string, tokenPrefix string) (*types.APIKey, error) {
	now := isoMillis(time.Now().UTC())
	for index := range m.APIKeys {
		key := &m.APIKeys[index]
		if key.UserID != strings.TrimSpace(userID) || key.ID != strings.TrimSpace(keyID) || key.RevokedAt != nil {
			continue
		}
		key.TokenHash = tokenHash
		key.TokenPrefix = tokenPrefix
		key.KeyMasked = maskSecret(tokenPrefix)
		key.UpdatedAt = now
		key.LastUsedAt = nil
		copyKey := *key
		return &copyKey, nil
	}
	return nil, nil
}

func (m *MemoryRepository) GetAPIKeySetup(_ context.Context, userID string, keyID string) (*types.APIKey, string, error) {
	for _, key := range m.APIKeys {
		if key.UserID == strings.TrimSpace(userID) && key.ID == strings.TrimSpace(keyID) {
			copyKey := key
			return &copyKey, m.raycastSetupBaseURL(key.ID), nil
		}
	}
	return nil, "", nil
}

func (m *MemoryRepository) FindAPIKeyBySecret(_ context.Context, secret string) (*types.APIKey, *types.User, error) {
	for _, key := range m.APIKeys {
		if key.RevokedAt != nil || (key.Key != secret && (key.TokenHash == "" || key.TokenHash != hashSecret(secret))) {
			continue
		}
		for _, user := range m.Users {
			if user.ID == key.UserID && user.DisabledAt == nil {
				found := key
				foundUser := user
				return &found, &foundUser, nil
			}
		}
	}
	return nil, nil, nil
}

func (m *MemoryRepository) MarkAPIKeyUsed(_ context.Context, keyID string) error {
	now := isoMillis(time.Now())
	for i := range m.APIKeys {
		if m.APIKeys[i].ID == keyID && m.APIKeys[i].RevokedAt == nil {
			m.APIKeys[i].LastUsedAt = &now
			return nil
		}
	}
	return nil
}

func (m *MemoryRepository) BootstrapOwner(_ context.Context, email string, displayName string, passwordHash string) (types.User, error) {
	now := isoMillis(time.Now())
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if email == "" || displayName == "" || passwordHash == "" {
		return types.User{}, errors.New("owner email, display name, and password hash are required")
	}
	for i := range m.Users {
		if m.Users[i].IsOwner {
			if !strings.EqualFold(m.Users[i].Email, email) {
				return types.User{}, ErrOwnerAlreadyExists
			}
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignLegacyThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	for i := range m.Users {
		if strings.EqualFold(m.Users[i].Email, email) {
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].IsOwner = true
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignLegacyThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	owner := types.User{
		ID:           identity.ProposedOwnerID(email),
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		IsOwner:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.Users = append(m.Users, owner)
	m.assignLegacyThreadsToOwner(owner.ID)
	return owner, nil
}

func (m *MemoryRepository) CreateOwnerSetupToken(_ context.Context, tokenHash string, expiresAt time.Time) (types.OwnerSetupToken, error) {
	if strings.TrimSpace(tokenHash) == "" || !expiresAt.After(time.Now().UTC()) {
		return types.OwnerSetupToken{}, errors.New("owner setup token hash and future expiry are required")
	}
	now := time.Now().UTC()
	nowValue := isoMillis(now)
	for index := range m.OwnerSetupTokens {
		if m.OwnerSetupTokens[index].Token.ConsumedAt == nil && m.OwnerSetupTokens[index].Token.RevokedAt == nil {
			m.OwnerSetupTokens[index].Token.RevokedAt = &nowValue
		}
	}
	purpose := "bootstrap"
	for _, user := range m.Users {
		if user.IsOwner {
			purpose = "recovery"
			break
		}
	}
	token := types.OwnerSetupToken{
		ID:        "ost_" + uuid.NewString(),
		Purpose:   purpose,
		CreatedAt: nowValue,
		ExpiresAt: isoMillis(expiresAt),
	}
	m.OwnerSetupTokens = append(m.OwnerSetupTokens, memoryOwnerSetupToken{Token: token, TokenHash: tokenHash})
	return token, nil
}

func (m *MemoryRepository) UseOwnerSetupToken(_ context.Context, tokenHash string, email string, displayName string, passwordHash string) (types.User, types.OwnerSetupToken, error) {
	now := time.Now().UTC()
	for index := range m.OwnerSetupTokens {
		entry := &m.OwnerSetupTokens[index]
		expiresAt, err := time.Parse(time.RFC3339Nano, entry.Token.ExpiresAt)
		if err != nil || entry.TokenHash != tokenHash || entry.Token.ConsumedAt != nil || entry.Token.RevokedAt != nil || !expiresAt.After(now) {
			continue
		}
		ownerIndex := -1
		for userIndex := range m.Users {
			if m.Users[userIndex].IsOwner {
				ownerIndex = userIndex
				break
			}
		}
		if entry.Token.Purpose == "bootstrap" && ownerIndex >= 0 {
			return types.User{}, types.OwnerSetupToken{}, ErrOwnerSetupTokenInvalid
		}
		if entry.Token.Purpose == "recovery" && ownerIndex < 0 {
			return types.User{}, types.OwnerSetupToken{}, ErrOwnerSetupTokenInvalid
		}
		if ownerIndex >= 0 && !strings.EqualFold(m.Users[ownerIndex].Email, strings.TrimSpace(email)) {
			return types.User{}, types.OwnerSetupToken{}, ErrOwnerAlreadyExists
		}
		owner, err := m.bootstrapOwner(strings.TrimSpace(email), strings.TrimSpace(displayName), passwordHash)
		if err != nil {
			return types.User{}, types.OwnerSetupToken{}, err
		}
		consumedAt := isoMillis(now)
		entry.Token.ConsumedAt = &consumedAt
		return owner, entry.Token, nil
	}
	return types.User{}, types.OwnerSetupToken{}, ErrOwnerSetupTokenInvalid
}

func (m *MemoryRepository) bootstrapOwner(email string, displayName string, passwordHash string) (types.User, error) {
	now := isoMillis(time.Now())
	if email == "" || displayName == "" || passwordHash == "" {
		return types.User{}, errors.New("owner email, display name, and password hash are required")
	}
	for i := range m.Users {
		if m.Users[i].IsOwner {
			if !strings.EqualFold(m.Users[i].Email, email) {
				return types.User{}, ErrOwnerAlreadyExists
			}
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignLegacyThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	for i := range m.Users {
		if strings.EqualFold(m.Users[i].Email, email) {
			m.Users[i].DisplayName = displayName
			m.Users[i].PasswordHash = &passwordHash
			m.Users[i].IsOwner = true
			m.Users[i].DisabledAt = nil
			m.Users[i].UpdatedAt = now
			m.assignLegacyThreadsToOwner(m.Users[i].ID)
			return m.Users[i], nil
		}
	}
	owner := types.User{
		ID:           "usr_" + uuid.NewString(),
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		IsOwner:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.Users = append(m.Users, owner)
	m.assignLegacyThreadsToOwner(owner.ID)
	return owner, nil
}

func (m *MemoryRepository) assignLegacyThreadsToOwner(ownerUserID string) {
	for i := range m.Threads {
		if strings.TrimSpace(m.Threads[i].OwnerUserID) == "" {
			m.Threads[i].OwnerUserID = ownerUserID
		}
	}
}

func (m *MemoryRepository) CreateSignupInvitation(_ context.Context, createdByUserID string, tokenHash string, expiresAt time.Time, teamIDs []string) (types.SignupInvitation, error) {
	if strings.TrimSpace(createdByUserID) == "" || strings.TrimSpace(tokenHash) == "" || !expiresAt.After(time.Now().UTC()) {
		return types.SignupInvitation{}, errors.New("invitation creator, token hash, and future expiry are required")
	}
	teamIDs = uniqueNonEmptyStrings(teamIDs)
	teams := make([]types.Team, 0, len(teamIDs))
	for _, teamID := range teamIDs {
		found := false
		for _, team := range m.Teams {
			if team.ID == teamID {
				teams = append(teams, team)
				found = true
				break
			}
		}
		if !found {
			return types.SignupInvitation{}, types.ErrTeamNotFound
		}
	}
	now := isoMillis(time.Now().UTC())
	invitation := types.SignupInvitation{
		ID:              "inv_" + uuid.NewString(),
		CreatedByUserID: createdByUserID,
		CreatedAt:       now,
		ExpiresAt:       isoMillis(expiresAt),
		Teams:           teams,
	}
	m.SignupInvitations = append(m.SignupInvitations, memorySignupInvitation{Invitation: invitation, TokenHash: tokenHash, TeamIDs: teamIDs})
	return invitation, nil
}

func (m *MemoryRepository) ListSignupInvitations(ctx context.Context) ([]types.SignupInvitation, error) {
	page, err := m.ListSignupInvitationsPage(ctx, types.PageRequest{})
	return page.Invitations, err
}

func (m *MemoryRepository) ListSignupInvitationsPage(_ context.Context, pageRequest types.PageRequest) (types.SignupInvitationPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	invitations := make([]types.SignupInvitation, 0, len(m.SignupInvitations))
	for index := len(m.SignupInvitations) - 1; index >= 0; index-- {
		invitation, err := m.hydrateSignupInvitation(m.SignupInvitations[index])
		if err != nil {
			return types.SignupInvitationPage{}, err
		}
		invitations = append(invitations, invitation)
	}
	start := pageRequest.Offset
	if start > len(invitations) {
		start = len(invitations)
	}
	end := start + pageRequest.Limit + 1
	if end > len(invitations) {
		end = len(invitations)
	}
	window := invitations[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.SignupInvitationPage{Invitations: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) RevokeSignupInvitation(_ context.Context, invitationID string) (bool, error) {
	now := isoMillis(time.Now().UTC())
	for index := range m.SignupInvitations {
		invitation := &m.SignupInvitations[index].Invitation
		if invitation.ID == invitationID && invitation.ConsumedAt == nil && invitation.RevokedAt == nil {
			invitation.RevokedAt = &now
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryRepository) FindSignupInvitation(_ context.Context, tokenHash string) (*types.SignupInvitation, error) {
	now := time.Now().UTC()
	for _, entry := range m.SignupInvitations {
		expiresAt, err := time.Parse(time.RFC3339Nano, entry.Invitation.ExpiresAt)
		if err != nil || entry.TokenHash != tokenHash || entry.Invitation.ConsumedAt != nil || entry.Invitation.RevokedAt != nil || !expiresAt.After(now) {
			continue
		}
		invitation, err := m.hydrateSignupInvitation(entry)
		if err != nil {
			return nil, err
		}
		return &invitation, nil
	}
	return nil, nil
}

func (m *MemoryRepository) RegisterWithSignupInvitation(_ context.Context, tokenHash string, email string, displayName string, passwordHash string, sessionSecretHash string, sessionExpiresAt time.Time) (types.User, types.UserSession, types.SignupInvitation, error) {
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	now := time.Now().UTC()
	invitationIndex := -1
	for index, entry := range m.SignupInvitations {
		expiresAt, err := time.Parse(time.RFC3339Nano, entry.Invitation.ExpiresAt)
		if err == nil && entry.TokenHash == tokenHash && entry.Invitation.ConsumedAt == nil && entry.Invitation.RevokedAt == nil && expiresAt.After(now) {
			invitationIndex = index
			break
		}
	}
	if invitationIndex < 0 || email == "" || displayName == "" || passwordHash == "" || sessionSecretHash == "" || !sessionExpiresAt.After(now) {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, types.ErrSignupInvitationInvalid
	}
	for _, user := range m.Users {
		if strings.EqualFold(strings.TrimSpace(user.Email), email) {
			return types.User{}, types.UserSession{}, types.SignupInvitation{}, types.ErrEmailAlreadyRegistered
		}
	}
	nowValue := isoMillis(now)
	invitationValue, err := m.hydrateSignupInvitation(m.SignupInvitations[invitationIndex])
	if err != nil {
		return types.User{}, types.UserSession{}, types.SignupInvitation{}, err
	}
	user := types.User{
		ID:           "usr_" + uuid.NewString(),
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &passwordHash,
		CreatedAt:    nowValue,
		UpdatedAt:    nowValue,
	}
	memberships := make([]types.TeamMembership, 0, len(invitationValue.Teams))
	for _, team := range invitationValue.Teams {
		memberships = append(memberships, types.TeamMembership{TeamID: team.ID, UserID: user.ID, CreatedAt: nowValue})
	}
	session := types.UserSession{
		ID:         "sess_" + uuid.NewString(),
		UserID:     user.ID,
		SecretHash: sessionSecretHash,
		CreatedAt:  nowValue,
		ExpiresAt:  isoMillis(sessionExpiresAt),
	}
	consumedAt := nowValue
	invitation := &m.SignupInvitations[invitationIndex].Invitation
	invitation.ConsumedAt = &consumedAt
	invitation.ConsumedByUserID = &user.ID
	invitation.Teams = invitationValue.Teams
	m.Users = append(m.Users, user)
	m.Sessions = append(m.Sessions, session)
	m.TeamMemberships = append(m.TeamMemberships, memberships...)
	return user, session, *invitation, nil
}

func (m *MemoryRepository) hydrateSignupInvitation(entry memorySignupInvitation) (types.SignupInvitation, error) {
	invitation := entry.Invitation
	invitation.Teams = make([]types.Team, 0, len(entry.TeamIDs))
	for _, teamID := range entry.TeamIDs {
		found := false
		for _, team := range m.Teams {
			if team.ID == teamID {
				invitation.Teams = append(invitation.Teams, team)
				found = true
				break
			}
		}
		if !found {
			return types.SignupInvitation{}, types.ErrTeamNotFound
		}
	}
	sort.SliceStable(invitation.Teams, func(i, j int) bool {
		if !strings.EqualFold(invitation.Teams[i].Name, invitation.Teams[j].Name) {
			return strings.ToLower(invitation.Teams[i].Name) < strings.ToLower(invitation.Teams[j].Name)
		}
		return invitation.Teams[i].ID < invitation.Teams[j].ID
	})
	return invitation, nil
}

func (m *MemoryRepository) CreateTeam(_ context.Context, slug string, name string) (types.Team, error) {
	slug = strings.TrimSpace(slug)
	name = strings.TrimSpace(name)
	for _, team := range m.Teams {
		if strings.EqualFold(team.Slug, slug) {
			return types.Team{}, types.ErrTeamSlugConflict
		}
	}
	now := isoMillis(time.Now().UTC())
	team := types.Team{ID: "team_" + uuid.NewString(), Slug: slug, Name: name, CreatedAt: now, UpdatedAt: now}
	m.Teams = append(m.Teams, team)
	return team, nil
}

func (m *MemoryRepository) RenameTeam(_ context.Context, teamID string, name string) (types.Team, error) {
	teamID = strings.TrimSpace(teamID)
	for index := range m.Teams {
		if m.Teams[index].ID != teamID {
			continue
		}
		m.Teams[index].Name = strings.TrimSpace(name)
		m.Teams[index].UpdatedAt = isoMillis(time.Now().UTC())
		return m.Teams[index], nil
	}
	return types.Team{}, types.ErrTeamNotFound
}

func (m *MemoryRepository) ListTeams(ctx context.Context) ([]types.Team, error) {
	page, err := m.ListTeamsPage(ctx, types.PageRequest{}, 10)
	if err != nil {
		return nil, err
	}
	teams := make([]types.Team, 0, len(page.Teams))
	for _, item := range page.Teams {
		teams = append(teams, item.Team)
	}
	return teams, nil
}

func (m *MemoryRepository) ListTeamsPage(_ context.Context, pageRequest types.PageRequest, memberLimit int) (types.TeamPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	if memberLimit < 1 {
		memberLimit = 10
	}
	if memberLimit > 50 {
		memberLimit = 50
	}
	teams := append([]types.Team(nil), m.Teams...)
	sort.SliceStable(teams, func(i, j int) bool {
		if !strings.EqualFold(teams[i].Name, teams[j].Name) {
			return strings.ToLower(teams[i].Name) < strings.ToLower(teams[j].Name)
		}
		if !strings.EqualFold(teams[i].Slug, teams[j].Slug) {
			return strings.ToLower(teams[i].Slug) < strings.ToLower(teams[j].Slug)
		}
		return teams[i].ID < teams[j].ID
	})
	start := pageRequest.Offset
	if start > len(teams) {
		start = len(teams)
	}
	end := start + pageRequest.Limit + 1
	if end > len(teams) {
		end = len(teams)
	}
	window := teams[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	result := make([]types.TeamWithMembers, 0, visible)
	for _, team := range window[:visible] {
		members := []types.User{}
		wanted := map[string]bool{}
		for _, membership := range m.TeamMemberships {
			if membership.TeamID == team.ID {
				wanted[membership.UserID] = true
			}
		}
		for _, user := range m.Users {
			if wanted[user.ID] {
				members = append(members, user)
			}
		}
		sort.SliceStable(members, func(i, j int) bool {
			if members[i].IsOwner != members[j].IsOwner {
				return members[i].IsOwner
			}
			if !strings.EqualFold(members[i].DisplayName, members[j].DisplayName) {
				return strings.ToLower(members[i].DisplayName) < strings.ToLower(members[j].DisplayName)
			}
			return members[i].ID < members[j].ID
		})
		fetched := len(members)
		if fetched > memberLimit {
			fetched = memberLimit + 1
		}
		memberVisible, memberPage := types.PageWindow(types.PageRequest{Limit: memberLimit}, fetched)
		if memberVisible > len(members) {
			memberVisible = len(members)
		}
		result = append(result, types.TeamWithMembers{Team: team, Members: members[:memberVisible], MemberCount: len(members), MembersPage: memberPage})
	}
	return types.TeamPage{Teams: result, Page: pageInfo}, nil
}

func (m *MemoryRepository) ListUserTeams(_ context.Context, userID string) ([]types.Team, error) {
	wanted := map[string]bool{}
	for _, membership := range m.TeamMemberships {
		if membership.UserID == strings.TrimSpace(userID) {
			wanted[membership.TeamID] = true
		}
	}
	teams := []types.Team{}
	for _, team := range m.Teams {
		if wanted[team.ID] {
			teams = append(teams, team)
		}
	}
	sort.SliceStable(teams, func(i, j int) bool {
		if !strings.EqualFold(teams[i].Name, teams[j].Name) {
			return strings.ToLower(teams[i].Name) < strings.ToLower(teams[j].Name)
		}
		return teams[i].ID < teams[j].ID
	})
	return teams, nil
}

func (m *MemoryRepository) ListUserTeamsPage(_ context.Context, userID string, pageRequest types.PageRequest) (types.UserTeamPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	wanted := map[string]bool{}
	for _, membership := range m.TeamMemberships {
		if membership.UserID == strings.TrimSpace(userID) {
			wanted[membership.TeamID] = true
		}
	}
	teams := []types.Team{}
	for _, team := range m.Teams {
		if wanted[team.ID] {
			teams = append(teams, team)
		}
	}
	sort.SliceStable(teams, func(i, j int) bool {
		if !strings.EqualFold(teams[i].Name, teams[j].Name) {
			return strings.ToLower(teams[i].Name) < strings.ToLower(teams[j].Name)
		}
		return teams[i].ID < teams[j].ID
	})
	start := pageRequest.Offset
	if start > len(teams) {
		start = len(teams)
	}
	end := start + pageRequest.Limit + 1
	if end > len(teams) {
		end = len(teams)
	}
	window := teams[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.UserTeamPage{Teams: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) ListTeamMembers(ctx context.Context, teamID string) ([]types.User, error) {
	page, err := m.ListTeamMembersPage(ctx, teamID, types.PageRequest{})
	return page.Members, err
}

func (m *MemoryRepository) ListTeamMembersPage(_ context.Context, teamID string, pageRequest types.PageRequest) (types.TeamMemberPage, error) {
	teamID = strings.TrimSpace(teamID)
	pageRequest = types.NormalizePageRequest(pageRequest)
	teamFound := false
	for _, team := range m.Teams {
		if team.ID == teamID {
			teamFound = true
			break
		}
	}
	if !teamFound {
		return types.TeamMemberPage{}, types.ErrTeamNotFound
	}
	wanted := map[string]bool{}
	for _, membership := range m.TeamMemberships {
		if membership.TeamID == teamID {
			wanted[membership.UserID] = true
		}
	}
	users := []types.User{}
	for _, user := range m.Users {
		if wanted[user.ID] {
			users = append(users, user)
		}
	}
	sort.SliceStable(users, func(i, j int) bool {
		if users[i].IsOwner != users[j].IsOwner {
			return users[i].IsOwner
		}
		if !strings.EqualFold(users[i].DisplayName, users[j].DisplayName) {
			return strings.ToLower(users[i].DisplayName) < strings.ToLower(users[j].DisplayName)
		}
		return users[i].ID < users[j].ID
	})
	start := pageRequest.Offset
	if start > len(users) {
		start = len(users)
	}
	end := start + pageRequest.Limit + 1
	if end > len(users) {
		end = len(users)
	}
	window := users[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.TeamMemberPage{Members: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) AddTeamMember(_ context.Context, teamID string, userID string) (types.TeamMembership, error) {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	teamFound := false
	for _, team := range m.Teams {
		if team.ID == teamID {
			teamFound = true
			break
		}
	}
	if !teamFound {
		return types.TeamMembership{}, types.ErrTeamNotFound
	}
	userFound := false
	for _, user := range m.Users {
		if user.ID == userID {
			if user.DisabledAt != nil {
				return types.TeamMembership{}, types.ErrUserDisabled
			}
			userFound = true
			break
		}
	}
	if !userFound {
		return types.TeamMembership{}, types.ErrUserNotFound
	}
	for _, membership := range m.TeamMemberships {
		if membership.TeamID == teamID && membership.UserID == userID {
			return membership, nil
		}
	}
	membership := types.TeamMembership{TeamID: teamID, UserID: userID, CreatedAt: isoMillis(time.Now().UTC())}
	m.TeamMemberships = append(m.TeamMemberships, membership)
	return membership, nil
}

func (m *MemoryRepository) RemoveTeamMember(_ context.Context, teamID string, userID string) (bool, error) {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	teamFound := false
	for _, team := range m.Teams {
		if team.ID == teamID {
			teamFound = true
			break
		}
	}
	if !teamFound {
		return false, types.ErrTeamNotFound
	}
	userFound := false
	for _, user := range m.Users {
		if user.ID == userID {
			userFound = true
			break
		}
	}
	if !userFound {
		return false, types.ErrUserNotFound
	}
	for index, membership := range m.TeamMemberships {
		if membership.TeamID == teamID && membership.UserID == userID {
			m.TeamMemberships = append(m.TeamMemberships[:index], m.TeamMemberships[index+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryRepository) ListUsers(ctx context.Context) ([]types.User, error) {
	page, err := m.ListUsersPage(ctx, types.PageRequest{})
	return page.Users, err
}

func (m *MemoryRepository) ListUsersPage(_ context.Context, pageRequest types.PageRequest) (types.UserPage, error) {
	pageRequest = types.NormalizePageRequest(pageRequest)
	users := append([]types.User(nil), m.Users...)
	sort.SliceStable(users, func(i, j int) bool {
		if users[i].IsOwner != users[j].IsOwner {
			return users[i].IsOwner
		}
		if users[i].CreatedAt != users[j].CreatedAt {
			return users[i].CreatedAt < users[j].CreatedAt
		}
		return users[i].ID < users[j].ID
	})
	start := pageRequest.Offset
	if start > len(users) {
		start = len(users)
	}
	end := start + pageRequest.Limit + 1
	if end > len(users) {
		end = len(users)
	}
	window := users[start:end]
	visible, pageInfo := types.PageWindow(pageRequest, len(window))
	return types.UserPage{Users: window[:visible], Page: pageInfo}, nil
}

func (m *MemoryRepository) GetUserByID(_ context.Context, userID string) (*types.User, error) {
	for _, user := range m.Users {
		if user.ID == strings.TrimSpace(userID) {
			copyUser := user
			return &copyUser, nil
		}
	}
	return nil, nil
}

func (m *MemoryRepository) SetUserDisabled(_ context.Context, userID string, disabled bool) (types.User, error) {
	m.purgeMutex.Lock()
	defer m.purgeMutex.Unlock()
	now := isoMillis(time.Now().UTC())
	for index := range m.Users {
		user := &m.Users[index]
		if user.ID != userID {
			continue
		}
		if disabled && user.IsOwner {
			return types.User{}, types.ErrOwnerCannotBeDisabled
		}
		if disabled {
			if user.DisabledAt == nil {
				user.DisabledAt = &now
			}
			for sessionIndex := range m.Sessions {
				if m.Sessions[sessionIndex].UserID == user.ID && m.Sessions[sessionIndex].RevokedAt == nil {
					m.Sessions[sessionIndex].RevokedAt = &now
				}
			}
			for keyIndex := range m.APIKeys {
				if m.APIKeys[keyIndex].UserID == user.ID && m.APIKeys[keyIndex].RevokedAt == nil {
					m.APIKeys[keyIndex].RevokedAt = &now
					m.APIKeys[keyIndex].UpdatedAt = now
				}
			}
			for codeIndex := range m.CLICodes {
				if m.CLICodes[codeIndex].UserID == user.ID && m.CLICodes[codeIndex].ConsumedAt == nil {
					m.CLICodes[codeIndex].ConsumedAt = &now
				}
			}
			memberships := m.TeamMemberships[:0]
			for _, membership := range m.TeamMemberships {
				if membership.UserID != user.ID {
					memberships = append(memberships, membership)
				}
			}
			m.TeamMemberships = memberships
		} else {
			user.DisabledAt = nil
		}
		user.UpdatedAt = now
		return *user, nil
	}
	return types.User{}, types.ErrUserNotFound
}

func (m *MemoryRepository) CreateUser(_ context.Context, email string, displayName string, passwordHash *string) (types.User, error) {
	now := isoMillis(time.Now())
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	user := types.User{
		ID:           "usr_" + uuid.NewString(),
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.Users = append(m.Users, user)
	return user, nil
}

func (m *MemoryRepository) FindUserByEmail(_ context.Context, email string) (*types.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, user := range m.Users {
		if user.DisabledAt != nil || strings.ToLower(strings.TrimSpace(user.Email)) != email {
			continue
		}
		copy := user
		return &copy, nil
	}
	return nil, nil
}

func (m *MemoryRepository) CreateUserSession(_ context.Context, session types.UserSession) (types.UserSession, error) {
	now := isoMillis(time.Now())
	if session.ID == "" {
		session.ID = "sess_" + uuid.NewString()
	}
	session.CreatedAt = now
	if session.ExpiresAt == "" {
		session.ExpiresAt = isoMillis(time.Now().Add(30 * 24 * time.Hour))
	}
	m.Sessions = append(m.Sessions, session)
	return session, nil
}

func (m *MemoryRepository) FindUserSessionBySecretHash(_ context.Context, secretHash string) (*types.UserSession, *types.User, error) {
	now := time.Now().UTC()
	for _, session := range m.Sessions {
		if session.SecretHash != secretHash || session.RevokedAt != nil {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339, session.ExpiresAt)
		if err == nil && now.After(expiresAt) {
			continue
		}
		for _, user := range m.Users {
			if user.ID == session.UserID && user.DisabledAt == nil {
				sessionCopy := session
				userCopy := user
				return &sessionCopy, &userCopy, nil
			}
		}
	}
	return nil, nil, nil
}

func (m *MemoryRepository) MarkUserSessionUsed(_ context.Context, sessionID string) error {
	now := isoMillis(time.Now())
	for i := range m.Sessions {
		if m.Sessions[i].ID == sessionID && m.Sessions[i].RevokedAt == nil {
			m.Sessions[i].LastUsedAt = &now
			return nil
		}
	}
	return nil
}

func (m *MemoryRepository) RevokeUserSession(_ context.Context, sessionID string) error {
	now := isoMillis(time.Now())
	for i := range m.Sessions {
		if m.Sessions[i].ID == sessionID && m.Sessions[i].RevokedAt == nil {
			m.Sessions[i].RevokedAt = &now
			return nil
		}
	}
	return nil
}

func (m *MemoryRepository) CreateCLILoginCode(_ context.Context, code types.CLILoginCode) (types.CLILoginCode, error) {
	now := isoMillis(time.Now())
	if code.ID == "" {
		code.ID = "clicode_" + uuid.NewString()
	}
	code.CreatedAt = now
	if code.ExpiresAt == "" {
		code.ExpiresAt = isoMillis(time.Now().Add(5 * time.Minute))
	}
	m.CLICodes = append(m.CLICodes, code)
	return code, nil
}

func (m *MemoryRepository) ConsumeCLILoginCode(_ context.Context, codeHash string, stateHash string, redirectURI string) (*types.CLILoginCode, *types.User, error) {
	now := time.Now().UTC()
	consumedAt := isoMillis(now)
	for i := range m.CLICodes {
		code := m.CLICodes[i]
		if code.CodeHash != codeHash || code.StateHash != stateHash || code.RedirectURI != redirectURI || code.ConsumedAt != nil {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339, code.ExpiresAt)
		if err != nil || !expiresAt.After(now) {
			return nil, nil, nil
		}
		for _, user := range m.Users {
			if user.ID == code.UserID && user.DisabledAt == nil {
				m.CLICodes[i].ConsumedAt = &consumedAt
				code.ConsumedAt = &consumedAt
				userCopy := user
				return &code, &userCopy, nil
			}
		}
		return nil, nil, nil
	}
	return nil, nil, nil
}

func pendingUploadOwnedBy(upload types.PendingUpload, owner types.AuthContext) bool {
	if strings.TrimSpace(owner.UserID) == "" || upload.CreatedByUserID == nil || *upload.CreatedByUserID != owner.UserID {
		return false
	}
	if strings.TrimSpace(owner.KeyID) == "" {
		return upload.CreatedByKeyID == nil || strings.TrimSpace(*upload.CreatedByKeyID) == ""
	}
	return upload.CreatedByKeyID != nil && *upload.CreatedByKeyID == owner.KeyID
}

// normalThreadAccess mirrors the SQL normalThreadAccessPredicate.
func (m *MemoryRepository) normalThreadAccess(thread types.Thread, userID string) *types.ThreadAccess {
	if strings.TrimSpace(userID) == "" {
		return nil
	}
	matched := []string{}
	memberTeams := map[string]struct{}{}
	for _, membership := range m.TeamMemberships {
		if membership.UserID == userID {
			memberTeams[membership.TeamID] = struct{}{}
		}
	}
	for _, share := range m.ThreadTeamShares {
		if share.ThreadID != thread.ID {
			continue
		}
		if _, ok := memberTeams[share.TeamID]; ok {
			matched = append(matched, share.TeamID)
		}
	}
	sort.Strings(matched)
	isOwner := thread.OwnerUserID == userID
	if !isOwner && len(matched) == 0 {
		return nil
	}
	return &types.ThreadAccess{
		ThreadID:       thread.ID,
		OwnerUserID:    thread.OwnerUserID,
		UserID:         userID,
		IsOwner:        isOwner,
		MatchedTeamIDs: matched,
	}
}
