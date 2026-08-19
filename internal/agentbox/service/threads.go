package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
)

func (s *Service) ListThreads(ctx context.Context, auth types.AuthContext, limit int) ([]types.Thread, error) {
	return s.ListThreadsFiltered(ctx, auth, types.ThreadListParams{Limit: limit})
}

func (s *Service) ListThreadsFiltered(ctx context.Context, auth types.AuthContext, params types.ThreadListParams) ([]types.Thread, error) {
	page, err := s.ListThreadsPage(ctx, auth, params)
	return page.Threads, err
}

func (s *Service) ListThreadsPage(ctx context.Context, auth types.AuthContext, params types.ThreadListParams) (types.ThreadPage, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return types.ThreadPage{}, err
	}
	if params.Limit == 0 {
		params.Limit = 50
	}
	if params.Limit < 1 {
		params.Limit = 1
	}
	if params.Limit > 200 {
		params.Limit = 200
	}
	if err := normalizeThreadFilter(&params.Filter, &params.TeamRef); err != nil {
		return types.ThreadPage{}, err
	}
	if err := validateThreadPageCursor(params.Cursor); err != nil {
		return types.ThreadPage{}, err
	}
	return s.repo.ListThreadsPage(ctx, auth.UserID, params)
}

func (s *Service) SearchThreads(ctx context.Context, auth types.AuthContext, params types.SearchThreadParams) ([]types.SearchThreadResult, error) {
	page, err := s.SearchThreadsPage(ctx, auth, params)
	return page.Threads, err
}

func (s *Service) SearchThreadsPage(ctx context.Context, auth types.AuthContext, params types.SearchThreadParams) (types.SearchThreadPage, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return types.SearchThreadPage{}, err
	}
	params.Query = strings.TrimSpace(params.Query)
	if params.Query == "" {
		return types.SearchThreadPage{}, CodedError{Code: "INVALID_ARGUMENT", Message: "query is required."}
	}
	if params.Limit == 0 {
		params.Limit = 20
	}
	if params.Limit < 1 {
		params.Limit = 1
	}
	if params.Limit > 100 {
		params.Limit = 100
	}
	if params.CreatedBy != nil {
		value := strings.TrimSpace(*params.CreatedBy)
		params.CreatedBy = &value
	}
	if params.UpdatedAfter != nil {
		value := strings.TrimSpace(*params.UpdatedAfter)
		params.UpdatedAfter = &value
	}
	if params.UpdatedAfter != nil && *params.UpdatedAfter != "" {
		if _, err := time.Parse(time.RFC3339, *params.UpdatedAfter); err != nil {
			return types.SearchThreadPage{}, CodedError{Code: "INVALID_ARGUMENT", Message: "updated_after must be an RFC3339 timestamp."}
		}
	}
	if err := normalizeThreadFilter(&params.Filter, &params.TeamRef); err != nil {
		return types.SearchThreadPage{}, err
	}
	if err := validateThreadPageCursor(params.Cursor); err != nil {
		return types.SearchThreadPage{}, err
	}
	return s.repo.SearchThreadsPage(ctx, auth.UserID, params)
}

func validateThreadPageCursor(cursor *types.ThreadPageCursor) error {
	if cursor == nil {
		return nil
	}
	if cursor.UpdatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "cursor is invalid."}
	}
	return nil
}

func normalizeThreadFilter(filter *string, teamRef *string) error {
	*filter = strings.ToLower(strings.TrimSpace(*filter))
	*teamRef = strings.TrimSpace(*teamRef)
	if *filter == "" {
		*filter = types.ThreadFilterAll
	}
	switch *filter {
	case types.ThreadFilterAll, types.ThreadFilterPrivate, types.ThreadFilterShared, types.ThreadFilterPublic:
		if *teamRef != "" {
			return CodedError{Code: "INVALID_ARGUMENT", Message: "team is valid only with filter=team."}
		}
	case types.ThreadFilterTeam:
		if *teamRef == "" {
			return CodedError{Code: "INVALID_ARGUMENT", Message: "team is required when filter=team."}
		}
	default:
		return CodedError{Code: "INVALID_ARGUMENT", Message: "filter must be all, private, shared, team, or public."}
	}
	return nil
}

func (s *Service) CreateThread(ctx context.Context, auth types.AuthContext, title string) (types.Thread, error) {
	if err := requireScope(auth, "threads:write"); err != nil {
		return types.Thread{}, err
	}
	if err := validate.CreateThreadTitle(title); err != nil {
		return types.Thread{}, err
	}
	return s.repo.CreateThread(ctx, auth.UserID, title, auth)
}

func (s *Service) CreateThreadWithMessage(ctx context.Context, auth types.AuthContext, title string, body string, bodyContentType *string) (types.Thread, types.Message, error) {
	if err := requireScope(auth, "threads:write"); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	if err := validate.CreateThreadTitle(title); err != nil {
		return types.Thread{}, types.Message{}, err
	}
	resolvedContentType, err := messageformat.Resolve(bodyContentType, body, "")
	if err != nil {
		return types.Thread{}, types.Message{}, err
	}
	return s.repo.CreateThreadWithMessage(ctx, auth.UserID, title, auth, body, &resolvedContentType)
}

func (s *Service) GetThread(ctx context.Context, auth types.AuthContext, threadID string) (*types.ThreadWithMessages, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return nil, err
	}
	thread, err := s.repo.GetThread(ctx, auth.UserID, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	return thread, nil
}

func (s *Service) GetMessage(ctx context.Context, auth types.AuthContext, messageID string) (*types.Message, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return nil, err
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "message_id is required."}
	}
	message, err := s.repo.GetMessage(ctx, auth.UserID, messageID)
	if err != nil {
		return nil, err
	}
	if message == nil {
		return nil, CodedError{Code: "MESSAGE_NOT_FOUND", Message: "Message not found."}
	}
	return message, nil
}

func (s *Service) GetThreadVisibility(ctx context.Context, auth types.AuthContext, threadID string) (*types.ThreadVisibility, error) {
	if err := requireScope(auth, "threads:read"); err != nil {
		return nil, err
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "thread_id is required."}
	}
	visibility, err := s.repo.GetThreadVisibility(ctx, auth.UserID, threadID)
	if err != nil {
		return nil, err
	}
	if visibility == nil {
		return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	return visibility, nil
}

func (s *Service) ManageThreadVisibility(ctx context.Context, auth types.AuthContext, threadID string, baseURL string, input types.ManageThreadVisibilityInput) (types.ManagedThreadVisibility, error) {
	mutation := len(input.AddTeams) > 0 || len(input.RemoveTeams) > 0 || input.Public != nil || input.RegeneratePublicLink
	if mutation {
		if err := requireScope(auth, "threads:write"); err != nil {
			return types.ManagedThreadVisibility{}, err
		}
	} else if err := requireScope(auth, "threads:read"); err != nil {
		return types.ManagedThreadVisibility{}, err
	}

	threadID = strings.TrimSpace(threadID)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if threadID == "" {
		return types.ManagedThreadVisibility{}, CodedError{Code: "INVALID_ARGUMENT", Message: "thread_id is required."}
	}
	input.AddTeams = uniqueTrimmedStrings(input.AddTeams)
	input.RemoveTeams = uniqueTrimmedStrings(input.RemoveTeams)
	if input.Public != nil && !*input.Public && input.RegeneratePublicLink {
		return types.ManagedThreadVisibility{}, CodedError{Code: "INVALID_ARGUMENT", Message: "regenerate_public_link cannot be combined with public=false."}
	}
	if (input.Public != nil && *input.Public) || input.RegeneratePublicLink {
		token, err := generatePublicToken()
		if err != nil {
			return types.ManagedThreadVisibility{}, err
		}
		input.PublicToken = token
		input.PublicTokenHash = hashSecret(token)
		input.PublicTokenPrefix = tokenPrefix(token)
	}

	state, err := s.repo.ManageThreadVisibility(ctx, auth.UserID, threadID, input)
	if errors.Is(err, types.ErrThreadNotFound) {
		return types.ManagedThreadVisibility{}, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	if errors.Is(err, types.ErrThreadVisibilityTeamUnavailable) {
		return types.ManagedThreadVisibility{}, CodedError{Code: "TEAM_NOT_AVAILABLE", Message: "A thread can be shared only with teams the acting user currently belongs to.", Err: err}
	}
	if errors.Is(err, types.ErrThreadVisibilityConflict) {
		return types.ManagedThreadVisibility{}, CodedError{Code: "INVALID_ARGUMENT", Message: "A team cannot be both added and removed, and a public link cannot be regenerated while unpublishing.", Err: err}
	}
	if errors.Is(err, types.ErrThreadPublicLinkNotFound) {
		return types.ManagedThreadVisibility{}, CodedError{Code: "PUBLIC_LINK_NOT_FOUND", Message: "No active public link exists to regenerate.", Err: err}
	}
	if err != nil {
		return types.ManagedThreadVisibility{}, err
	}
	if state.PublicLink != nil && strings.TrimSpace(state.PublicLink.Token) != "" && baseURL != "" {
		state.PublicURL = publicThreadURL(baseURL, state.PublicLink.Token)
	}
	return state, nil
}

func publicThreadURL(baseURL string, token string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(token) == "" {
		return ""
	}
	return baseURL + "/share/" + url.PathEscape(token)
}

func (s *Service) GetPublicThread(ctx context.Context, token string) (*types.PublicThreadView, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, CodedError{Code: "PUBLIC_LINK_NOT_FOUND", Message: "Shared thread not found."}
	}
	lease, err := s.repo.AcquirePublicThreadLease(ctx, hashSecret(token))
	if err != nil {
		return nil, err
	}
	if lease == nil {
		return nil, CodedError{Code: "PUBLIC_LINK_NOT_FOUND", Message: "Shared thread not found."}
	}
	thread := lease.Thread()
	if err := lease.Close(ctx); err != nil {
		return nil, fmt.Errorf("close public thread authorization snapshot: %w", err)
	}
	view := s.sanitizePublicThread(token, thread)
	return &view, nil
}

func (s *Service) PublicAssetDownloadURL(ctx context.Context, token string, assetID string) (string, error) {
	return s.publicAssetURL(ctx, token, assetID, false)
}

func (s *Service) PublicAssetPreviewURL(ctx context.Context, token string, assetID string) (string, error) {
	return s.publicAssetURL(ctx, token, assetID, true)
}

func (s *Service) publicAssetURL(ctx context.Context, token string, assetID string, inline bool) (string, error) {
	token = strings.TrimSpace(token)
	assetID = strings.TrimSpace(assetID)
	if token == "" || assetID == "" {
		return "", CodedError{Code: "PUBLIC_ASSET_NOT_FOUND", Message: "Public attachment not found."}
	}
	tokenHash := hashSecret(token)
	lease, err := s.repo.AcquirePublicAssetSigningLease(ctx, tokenHash, assetID)
	if err != nil {
		return "", err
	}
	if lease == nil {
		return "", CodedError{Code: "PUBLIC_ASSET_NOT_FOUND", Message: "Public attachment not found."}
	}
	asset := lease.Asset()
	if err := lease.Close(ctx); err != nil {
		return "", fmt.Errorf("close public attachment authorization snapshot: %w", err)
	}
	if asset.PurgedAt != nil {
		return "", CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	if inline && (asset.MimeType == nil || !strings.HasPrefix(strings.ToLower(*asset.MimeType), "image/")) {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "This attachment type does not support inline preview."}
	}
	if err := s.inspectAvailableAsset(ctx, asset); err != nil {
		return "", err
	}

	// Reauthorize immediately before signing so a token revocation or purge that
	// races the external storage inspection cannot turn a stale snapshot into a
	// fresh signed URL. URL generation itself is local and does not wait on R2.
	signingLease, err := s.repo.AcquirePublicAssetSigningLease(ctx, tokenHash, assetID)
	if err != nil {
		return "", err
	}
	if signingLease == nil {
		return "", CodedError{Code: "PUBLIC_ASSET_NOT_FOUND", Message: "Public attachment not found."}
	}
	signingAsset := signingLease.Asset()
	if !sameAssetIdentity(asset, signingAsset) {
		if err := signingLease.Close(ctx); err != nil {
			return "", fmt.Errorf("close changed public attachment signing authorization: %w", err)
		}
		return "", CodedError{Code: "PUBLIC_ASSET_NOT_FOUND", Message: "Public attachment changed before signing."}
	}
	if signingAsset.PurgedAt != nil {
		if err := signingLease.Close(ctx); err != nil {
			return "", fmt.Errorf("close purged public attachment signing authorization: %w", err)
		}
		return "", CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	signedURL, err := s.createSignedAssetURL(ctx, signingAsset, 300, inline)
	if err != nil {
		if closeErr := signingLease.Close(ctx); closeErr != nil {
			return "", fmt.Errorf("sign public attachment: %v; close signing authorization: %w", err, closeErr)
		}
		return "", err
	}
	if err := signingLease.Close(ctx); err != nil {
		return "", fmt.Errorf("close public attachment signing authorization: %w", err)
	}
	return signedURL, nil
}

func (s *Service) GetAsset(ctx context.Context, auth types.AuthContext, assetID string) (*types.Asset, error) {
	if err := requireScope(auth, "assets:read"); err != nil {
		return nil, err
	}
	return s.repo.GetAsset(ctx, auth.UserID, assetID)
}
