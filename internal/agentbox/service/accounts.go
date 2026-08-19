package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
)

func (s *Service) IssueOwnerSetupToken(ctx context.Context, ttl time.Duration) (OwnerSetupTokenResult, error) {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if ttl > 24*time.Hour {
		return OwnerSetupTokenResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "Owner setup token expiry may not exceed 24 hours."}
	}
	secret, err := generateOwnerSetupToken()
	if err != nil {
		return OwnerSetupTokenResult{}, err
	}
	token, err := s.repo.CreateOwnerSetupToken(ctx, hashSecret(secret), time.Now().UTC().Add(ttl))
	if err != nil {
		return OwnerSetupTokenResult{}, err
	}
	return OwnerSetupTokenResult{
		Token:     secret,
		Purpose:   token.Purpose,
		ExpiresAt: token.ExpiresAt,
	}, nil
}

func (s *Service) CompleteOwnerSetup(ctx context.Context, token string, email string, displayName string, password string) (types.AuthContext, string, types.User, error) {
	token = strings.TrimSpace(token)
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if token == "" || email == "" || displayName == "" || password == "" {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "INVALID_ARGUMENT", Message: "token, email, display_name, and password are required."}
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	owner, _, err := s.repo.UseOwnerSetupToken(ctx, hashSecret(token), email, displayName, passwordHash)
	if errors.Is(err, types.ErrOwnerSetupTokenInvalid) {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "INVALID_OWNER_SETUP_TOKEN", Message: "Owner setup token is invalid, expired, revoked, or already used.", Err: err}
	}
	if errors.Is(err, types.ErrOwnerAlreadyExists) {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "OWNER_EMAIL_MISMATCH", Message: "Recovery must use the permanent owner's existing email address.", Err: err}
	}
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	authContext, sessionSecret, err := s.createSessionForUser(ctx, owner)
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	return authContext, sessionSecret, owner, nil
}

func (s *Service) CreateSignupInvitation(ctx context.Context, authContext types.AuthContext, ttl time.Duration, requestedTeamIDs ...string) (SignupInvitationTokenResult, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return SignupInvitationTokenResult{}, err
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	secret, err := generateSignupInvitationToken()
	if err != nil {
		return SignupInvitationTokenResult{}, err
	}
	teamIDs := uniqueTrimmedStrings(requestedTeamIDs)
	invitation, err := s.repo.CreateSignupInvitation(ctx, authContext.UserID, hashSecret(secret), time.Now().UTC().Add(ttl), teamIDs)
	if errors.Is(err, types.ErrTeamNotFound) {
		return SignupInvitationTokenResult{}, CodedError{Code: "TEAM_NOT_FOUND", Message: "One or more selected teams no longer exist.", Err: err}
	}
	if err != nil {
		return SignupInvitationTokenResult{}, err
	}
	return SignupInvitationTokenResult{Invitation: invitation, Token: secret}, nil
}

func (s *Service) ListSignupInvitations(ctx context.Context, authContext types.AuthContext) ([]types.SignupInvitation, error) {
	page, err := s.ListSignupInvitationsPage(ctx, authContext, types.PageRequest{})
	return page.Invitations, err
}

func (s *Service) ListSignupInvitationsPage(ctx context.Context, authContext types.AuthContext, pageRequest types.PageRequest) (types.SignupInvitationPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.SignupInvitationPage{}, err
	}
	return s.repo.ListSignupInvitationsPage(ctx, pageRequest)
}

func (s *Service) RevokeSignupInvitation(ctx context.Context, authContext types.AuthContext, invitationID string) error {
	if err := requireOwnerBrowser(authContext); err != nil {
		return err
	}
	invitationID = strings.TrimSpace(invitationID)
	if invitationID == "" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "invitation_id is required."}
	}
	revoked, err := s.repo.RevokeSignupInvitation(ctx, invitationID)
	if err != nil {
		return err
	}
	if !revoked {
		return CodedError{Code: "INVITATION_NOT_FOUND", Message: "Active invitation not found."}
	}
	return nil
}

func (s *Service) InspectSignupInvitation(ctx context.Context, token string) (SignupInvitationInspection, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return SignupInvitationInspection{}, CodedError{Code: "INVALID_INVITATION", Message: "Invitation is invalid, expired, revoked, or already used."}
	}
	invitation, err := s.repo.FindSignupInvitation(ctx, hashSecret(token))
	if err != nil {
		return SignupInvitationInspection{}, err
	}
	if invitation == nil {
		return SignupInvitationInspection{}, CodedError{Code: "INVALID_INVITATION", Message: "Invitation is invalid, expired, revoked, or already used."}
	}
	return SignupInvitationInspection{Valid: true, ExpiresAt: invitation.ExpiresAt}, nil
}

func (s *Service) RegisterWithSignupInvitation(ctx context.Context, token string, email string, displayName string, password string) (types.AuthContext, string, types.User, error) {
	token = strings.TrimSpace(token)
	email = strings.TrimSpace(email)
	displayName = strings.TrimSpace(displayName)
	if token == "" || email == "" || displayName == "" || password == "" {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "INVALID_ARGUMENT", Message: "token, email, display_name, and password are required."}
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	sessionSecret, err := generateSessionSecret()
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	user, session, _, err := s.repo.RegisterWithSignupInvitation(
		ctx,
		hashSecret(token),
		email,
		displayName,
		passwordHash,
		hashSecret(sessionSecret),
		time.Now().UTC().Add(30*24*time.Hour),
	)
	if errors.Is(err, types.ErrSignupInvitationInvalid) {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "INVALID_INVITATION", Message: "Invitation is invalid, expired, revoked, or already used.", Err: err}
	}
	if errors.Is(err, types.ErrEmailAlreadyRegistered) {
		return types.AuthContext{}, "", types.User{}, CodedError{Code: "REGISTRATION_UNAVAILABLE", Message: "Registration could not be completed with those details.", Err: err}
	}
	if err != nil {
		return types.AuthContext{}, "", types.User{}, err
	}
	return authContextForUserSession(session, user), sessionSecret, user, nil
}

func (s *Service) ListUsers(ctx context.Context, authContext types.AuthContext) ([]types.User, error) {
	page, err := s.ListUsersPage(ctx, authContext, types.PageRequest{})
	return page.Users, err
}

func (s *Service) ListUsersPage(ctx context.Context, authContext types.AuthContext, pageRequest types.PageRequest) (types.UserPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.UserPage{}, err
	}
	return s.repo.ListUsersPage(ctx, pageRequest)
}

func (s *Service) ListOwnerContentThreads(ctx context.Context, ownerContext OwnerWebContext, params types.OwnerContentListParams) ([]types.OwnerContentThreadSummary, error) {
	page, err := s.ListOwnerContentThreadsPage(ctx, ownerContext, params)
	return page.Threads, err
}

func (s *Service) ListOwnerContentThreadsPage(ctx context.Context, ownerContext OwnerWebContext, params types.OwnerContentListParams) (types.OwnerContentThreadPage, error) {
	if err := requireOwnerWebContext(ownerContext); err != nil {
		return types.OwnerContentThreadPage{}, err
	}
	params.UserID = strings.TrimSpace(params.UserID)
	params.TeamRef = strings.TrimSpace(params.TeamRef)
	request := types.NormalizePageRequest(types.PageRequest{Limit: params.Limit, Offset: params.Offset})
	params.Limit, params.Offset = request.Limit, request.Offset
	return s.repo.ListOwnerContentThreadsPage(ctx, ownerContext.userID, params)
}

func (s *Service) SearchOwnerContentThreads(ctx context.Context, ownerContext OwnerWebContext, params types.OwnerContentSearchParams) ([]types.OwnerContentThreadSummary, error) {
	page, err := s.SearchOwnerContentThreadsPage(ctx, ownerContext, params)
	return page.Threads, err
}

func (s *Service) SearchOwnerContentThreadsPage(ctx context.Context, ownerContext OwnerWebContext, params types.OwnerContentSearchParams) (types.OwnerContentThreadPage, error) {
	if err := requireOwnerWebContext(ownerContext); err != nil {
		return types.OwnerContentThreadPage{}, err
	}
	params.Query = strings.TrimSpace(params.Query)
	if params.Query == "" {
		return types.OwnerContentThreadPage{}, CodedError{Code: "INVALID_ARGUMENT", Message: "q is required."}
	}
	params.UserID = strings.TrimSpace(params.UserID)
	params.TeamRef = strings.TrimSpace(params.TeamRef)
	request := types.NormalizePageRequest(types.PageRequest{Limit: params.Limit, Offset: params.Offset})
	params.Limit, params.Offset = request.Limit, request.Offset
	return s.repo.SearchOwnerContentThreadsPage(ctx, ownerContext.userID, params)
}

func (s *Service) GetOwnerContentThread(ctx context.Context, ownerContext OwnerWebContext, threadID string) (*types.OwnerContentThreadDetail, error) {
	if err := requireOwnerWebContext(ownerContext); err != nil {
		return nil, err
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "thread_id is required."}
	}
	thread, err := s.repo.GetOwnerContentThread(ctx, ownerContext.userID, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	return thread, nil
}

func (s *Service) SignedOwnerContentAssetDownloadURL(ctx context.Context, ownerContext OwnerWebContext, assetID string, expiresSeconds int) (string, error) {
	return s.signedOwnerContentAssetURL(ctx, ownerContext, assetID, expiresSeconds, false)
}

func (s *Service) SignedOwnerContentAssetPreviewURL(ctx context.Context, ownerContext OwnerWebContext, assetID string, expiresSeconds int) (string, error) {
	return s.signedOwnerContentAssetURL(ctx, ownerContext, assetID, expiresSeconds, true)
}

func (s *Service) signedOwnerContentAssetURL(ctx context.Context, ownerContext OwnerWebContext, assetID string, expiresSeconds int, inline bool) (string, error) {
	if err := requireOwnerWebContext(ownerContext); err != nil {
		return "", err
	}
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "asset_id is required."}
	}
	asset, err := s.repo.GetOwnerContentAsset(ctx, assetID)
	if err != nil {
		return "", err
	}
	if asset == nil {
		return "", CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset not found."}
	}
	if asset.PurgedAt != nil {
		return "", CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	if inline && (asset.MimeType == nil || !strings.HasPrefix(strings.ToLower(*asset.MimeType), "image/")) {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "This attachment type does not support inline preview."}
	}
	if err := s.inspectAvailableAsset(ctx, *asset); err != nil {
		return "", err
	}
	return s.createSignedAssetURL(ctx, *asset, validate.ClampSignedURLExpiry(expiresSeconds), inline)
}

func supportsDashboardInlinePreview(asset types.Asset) bool {
	if asset.MimeType != nil {
		mimeType := strings.ToLower(strings.TrimSpace(*asset.MimeType))
		if strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, messageformat.Markdown) {
			return true
		}
	}
	return messageformat.IsMarkdownPath(asset.FileName)
}

func (s *Service) ListOwnerAPIKeys(ctx context.Context, authContext types.AuthContext) ([]types.APIKey, error) {
	page, err := s.ListOwnerAPIKeysPage(ctx, authContext, types.PageRequest{})
	return page.Credentials, err
}

func (s *Service) ListOwnerAPIKeysPage(ctx context.Context, authContext types.AuthContext, pageRequest types.PageRequest) (types.APIKeyPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.APIKeyPage{}, err
	}
	return s.repo.ListAllAPIKeysPage(ctx, pageRequest)
}

func (s *Service) RevokeOwnerAPIKey(ctx context.Context, authContext types.AuthContext, keyID string) error {
	if err := requireOwnerBrowser(authContext); err != nil {
		return err
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "credential_id is required."}
	}
	revoked, err := s.repo.RevokeAPIKeyByID(ctx, keyID)
	if err != nil {
		return err
	}
	if !revoked {
		return CodedError{Code: "CREDENTIAL_NOT_FOUND", Message: "Credential not found.", Err: ErrAPIKeyNotFound}
	}
	return nil
}

func (s *Service) SetUserDisabled(ctx context.Context, authContext types.AuthContext, userID string, disabled bool) (types.User, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.User{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return types.User{}, CodedError{Code: "INVALID_ARGUMENT", Message: "user_id is required."}
	}
	user, err := s.repo.SetUserDisabled(ctx, userID, disabled)
	if errors.Is(err, types.ErrUserNotFound) {
		return types.User{}, CodedError{Code: "USER_NOT_FOUND", Message: "User not found.", Err: err}
	}
	if errors.Is(err, types.ErrOwnerCannotBeDisabled) {
		return types.User{}, CodedError{Code: "OWNER_IMMUTABLE", Message: "The permanent deployment owner cannot be disabled.", Err: err}
	}
	if err != nil {
		return types.User{}, err
	}
	return user, nil
}

func (s *Service) PurgeUserAttachments(ctx context.Context, authContext types.AuthContext, userID string, limit int) (types.AttachmentPurgeResult, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.AttachmentPurgeResult{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return types.AttachmentPurgeResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "user_id is required."}
	}
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return types.AttachmentPurgeResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "limit must be between 1 and 100."}
	}
	lease, err := s.repo.AcquireAttachmentPurgeLease(ctx, userID)
	if errors.Is(err, types.ErrUserNotFound) {
		return types.AttachmentPurgeResult{}, CodedError{Code: "USER_NOT_FOUND", Message: "User not found.", Err: err}
	}
	if errors.Is(err, types.ErrOwnerCannotBeDisabled) {
		return types.AttachmentPurgeResult{}, CodedError{Code: "OWNER_IMMUTABLE", Message: "The permanent deployment owner's attachments cannot be purged.", Err: err}
	}
	if errors.Is(err, types.ErrUserMustBeDisabled) {
		return types.AttachmentPurgeResult{}, CodedError{Code: "USER_ACTIVE", Message: "Attachments can be purged only after the user is disabled.", Err: err}
	}
	if err != nil {
		return types.AttachmentPurgeResult{}, err
	}
	defer func() { _ = lease.Close(ctx) }()

	candidates, err := s.repo.ListAssetPurgeCandidates(ctx, userID, limit)
	if err != nil {
		return types.AttachmentPurgeResult{}, err
	}
	result := types.AttachmentPurgeResult{UserID: userID, Failures: []types.AttachmentPurgeFailure{}}
	for _, candidate := range candidates {
		result.Attempted++
		if err := s.assets.DeleteAssetObject(ctx, candidate.StorageKey); err != nil {
			message := boundedPurgeError(err)
			_ = s.repo.MarkAssetPurgeFailure(ctx, candidate.AssetID, message)
			result.Failed++
			result.Failures = append(result.Failures, types.AttachmentPurgeFailure{AssetID: candidate.AssetID, Error: message})
			continue
		}
		marked, err := s.repo.MarkAssetPurged(ctx, candidate.AssetID, authContext.UserID)
		if err != nil || !marked {
			message := "failed to record attachment tombstone"
			if err != nil {
				message = boundedPurgeError(err)
			}
			_ = s.repo.MarkAssetPurgeFailure(ctx, candidate.AssetID, message)
			result.Failed++
			result.Failures = append(result.Failures, types.AttachmentPurgeFailure{AssetID: candidate.AssetID, Error: message})
			continue
		}
		result.Purged++
	}
	result.Remaining, err = s.repo.CountUnpurgedAssetsByUploader(ctx, userID)
	if err != nil {
		return types.AttachmentPurgeResult{}, err
	}
	result.Complete = result.Remaining == 0
	return result, nil
}

func boundedPurgeError(err error) string {
	message := err.Error()
	if len(message) > 500 {
		return message[:500]
	}
	return message
}

func (s *Service) CreateTeam(ctx context.Context, authContext types.AuthContext, slug string, name string) (types.Team, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.Team{}, err
	}
	slug, err := normalizeTeamSlug(slug)
	if err != nil {
		return types.Team{}, err
	}
	name, err = normalizeTeamName(name)
	if err != nil {
		return types.Team{}, err
	}
	team, err := s.repo.CreateTeam(ctx, slug, name)
	if errors.Is(err, types.ErrTeamSlugConflict) {
		return types.Team{}, CodedError{Code: "TEAM_SLUG_CONFLICT", Message: "That team slug is already in use.", Err: err}
	}
	if err != nil {
		return types.Team{}, err
	}
	return team, nil
}

func (s *Service) RenameTeam(ctx context.Context, authContext types.AuthContext, teamID string, name string) (types.Team, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.Team{}, err
	}
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return types.Team{}, CodedError{Code: "INVALID_ARGUMENT", Message: "team_id is required."}
	}
	name, err := normalizeTeamName(name)
	if err != nil {
		return types.Team{}, err
	}
	team, err := s.repo.RenameTeam(ctx, teamID, name)
	if errors.Is(err, types.ErrTeamNotFound) {
		return types.Team{}, CodedError{Code: "TEAM_NOT_FOUND", Message: "Team not found.", Err: err}
	}
	if err != nil {
		return types.Team{}, err
	}
	return team, nil
}

func (s *Service) ListOwnerTeams(ctx context.Context, authContext types.AuthContext) ([]types.TeamWithMembers, error) {
	page, err := s.ListOwnerTeamsPage(ctx, authContext, types.PageRequest{})
	return page.Teams, err
}

func (s *Service) ListOwnerTeamsPage(ctx context.Context, authContext types.AuthContext, pageRequest types.PageRequest) (types.TeamPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.TeamPage{}, err
	}
	return s.repo.ListTeamsPage(ctx, pageRequest, 10)
}

func (s *Service) ListOwnerTeamMembersPage(ctx context.Context, authContext types.AuthContext, teamID string, pageRequest types.PageRequest) (types.TeamMemberPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.TeamMemberPage{}, err
	}
	page, err := s.repo.ListTeamMembersPage(ctx, strings.TrimSpace(teamID), pageRequest)
	if errors.Is(err, types.ErrTeamNotFound) {
		return types.TeamMemberPage{}, CodedError{Code: "TEAM_NOT_FOUND", Message: "Team not found.", Err: err}
	}
	return page, err
}

func (s *Service) ListOwnerUserTeamsPage(ctx context.Context, authContext types.AuthContext, userID string, pageRequest types.PageRequest) (types.UserTeamPage, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.UserTeamPage{}, err
	}
	user, err := s.repo.GetUserByID(ctx, strings.TrimSpace(userID))
	if err != nil {
		return types.UserTeamPage{}, err
	}
	if user == nil {
		return types.UserTeamPage{}, CodedError{Code: "USER_NOT_FOUND", Message: "User not found.", Err: types.ErrUserNotFound}
	}
	return s.repo.ListUserTeamsPage(ctx, user.ID, pageRequest)
}

func (s *Service) ListMyTeams(ctx context.Context, authContext types.AuthContext) ([]types.Team, error) {
	if err := requireUserAuthContext(authContext); err != nil {
		return nil, err
	}
	return s.repo.ListUserTeams(ctx, authContext.UserID)
}

func (s *Service) AddTeamMember(ctx context.Context, authContext types.AuthContext, teamID string, userID string) (types.TeamMembership, error) {
	if err := requireOwnerBrowser(authContext); err != nil {
		return types.TeamMembership{}, err
	}
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	if teamID == "" || userID == "" {
		return types.TeamMembership{}, CodedError{Code: "INVALID_ARGUMENT", Message: "team_id and user_id are required."}
	}
	membership, err := s.repo.AddTeamMember(ctx, teamID, userID)
	if errors.Is(err, types.ErrTeamNotFound) {
		return types.TeamMembership{}, CodedError{Code: "TEAM_NOT_FOUND", Message: "Team not found.", Err: err}
	}
	if errors.Is(err, types.ErrUserNotFound) {
		return types.TeamMembership{}, CodedError{Code: "USER_NOT_FOUND", Message: "User not found.", Err: err}
	}
	if errors.Is(err, types.ErrUserDisabled) {
		return types.TeamMembership{}, CodedError{Code: "USER_DISABLED", Message: "Disabled users cannot be added to teams.", Err: err}
	}
	if err != nil {
		return types.TeamMembership{}, err
	}
	return membership, nil
}

func (s *Service) RemoveTeamMember(ctx context.Context, authContext types.AuthContext, teamID string, userID string) error {
	if err := requireOwnerBrowser(authContext); err != nil {
		return err
	}
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	if teamID == "" || userID == "" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "team_id and user_id are required."}
	}
	_, err := s.repo.RemoveTeamMember(ctx, teamID, userID)
	if errors.Is(err, types.ErrTeamNotFound) {
		return CodedError{Code: "TEAM_NOT_FOUND", Message: "Team not found.", Err: err}
	}
	if errors.Is(err, types.ErrUserNotFound) {
		return CodedError{Code: "USER_NOT_FOUND", Message: "User not found.", Err: err}
	}
	return err
}
