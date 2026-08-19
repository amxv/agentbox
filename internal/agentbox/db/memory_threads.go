package db

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"agentbox/internal/agentbox/types"
	"github.com/google/uuid"
)

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
			sortMessageAssets(&copyMessage)
			messages = append(messages, copyMessage)
		}
		sort.SliceStable(messages, func(i, j int) bool {
			return messagePositionLess(messages[i], messages[j])
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
		var lastMessage types.Message
		hasLastMessage := false
		matchedBody := ""
		titleMatches := strings.Contains(strings.ToLower(thread.Title), query)
		for _, message := range m.Messages {
			if message.ThreadID != thread.ID {
				continue
			}
			messageCount++
			if !hasLastMessage || messagePositionAfter(message, lastMessage) {
				lastBody = message.Body
				lastMessage = message
				hasLastMessage = true
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
		lastBody, matchedBody := "", ""
		var lastMessage types.Message
		hasLastMessage := false
		titleMatches := queryLower == "" || strings.Contains(strings.ToLower(thread.Title), queryLower)
		for _, message := range m.Messages {
			if message.ThreadID != thread.ID {
				continue
			}
			messageCount++
			if !hasLastMessage || messagePositionAfter(message, lastMessage) {
				lastBody = message.Body
				lastMessage = message
				hasLastMessage = true
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
			sortMessageAssets(&message)
			messages = append(messages, message)
		}
		sort.SliceStable(messages, func(i, j int) bool {
			return messagePositionLess(messages[i], messages[j])
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
		Position:                 1,
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
			sortMessageAssets(&message)
			messages = append(messages, message)
		}
		sort.Slice(messages, func(i, j int) bool {
			return messagePositionLess(messages[i], messages[j])
		})
		thread.VisibilitySummary = m.threadVisibilitySummary(thread, userID)
		return &types.ThreadWithMessages{Thread: thread, Messages: messages, Visibility: m.threadVisibility(thread)}, nil
	}
	return nil, nil
}

func (m *MemoryRepository) GetMessage(_ context.Context, userID string, messageID string) (*types.Message, error) {
	for _, message := range m.Messages {
		if message.ID != messageID {
			continue
		}
		for _, thread := range m.Threads {
			if thread.ID != message.ThreadID || m.normalThreadAccess(thread, userID) == nil {
				continue
			}
			copy := message
			copy.Assets = []types.Asset{}
			for _, asset := range m.Assets {
				if asset.MessageID != message.ID {
					continue
				}
				assetCopy := asset
				assetCopy.DownloadURL = nil
				copy.Assets = append(copy.Assets, assetCopy)
			}
			sortMessageAssets(&copy)
			return &copy, nil
		}
		return nil, nil
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
