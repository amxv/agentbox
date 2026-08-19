package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agentbox/internal/agentbox/types"
	"github.com/jackc/pgx/v5"
)

func scanThread(row threadScanner) (types.Thread, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var thread types.Thread
	err := row.Scan(threadScanDest(&thread, &createdAt, &updatedAt)...)
	thread.CreatedAt = isoMillis(createdAt)
	thread.UpdatedAt = isoMillis(updatedAt)
	return thread, err
}

func scanThreadWithVisibility(row threadScanner) (types.Thread, error) {
	thread, _, err := scanThreadWithVisibilityPosition(row)
	return thread, err
}

func scanThreadWithVisibilityPosition(row threadScanner) (types.Thread, time.Time, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var thread types.Thread
	var ownedByMe bool
	var sharedTeamsJSON []byte
	var matchedTeamsJSON []byte
	var isPublic bool
	dest := threadScanDest(&thread, &createdAt, &updatedAt)
	dest = append(dest, &ownedByMe, &sharedTeamsJSON, &matchedTeamsJSON, &isPublic)
	if err := row.Scan(dest...); err != nil {
		return types.Thread{}, time.Time{}, err
	}
	thread.CreatedAt = isoMillis(createdAt)
	thread.UpdatedAt = isoMillis(updatedAt)
	visibility, err := decodeThreadVisibilitySummary(ownedByMe, sharedTeamsJSON, matchedTeamsJSON, isPublic)
	if err != nil {
		return types.Thread{}, time.Time{}, err
	}
	thread.VisibilitySummary = visibility
	return thread, updatedAt, nil
}

func scanThreadSummaryWithVisibilityPosition(row threadScanner) (types.Thread, time.Time, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var thread types.Thread
	var messageCount int
	var lastMessageBody string
	var ownedByMe bool
	var sharedTeamsJSON []byte
	var matchedTeamsJSON []byte
	var isPublic bool
	dest := threadScanDest(&thread, &createdAt, &updatedAt)
	dest = append(dest, &messageCount, &lastMessageBody, &ownedByMe, &sharedTeamsJSON, &matchedTeamsJSON, &isPublic)
	if err := row.Scan(dest...); err != nil {
		return types.Thread{}, time.Time{}, err
	}
	thread.CreatedAt = isoMillis(createdAt)
	thread.UpdatedAt = isoMillis(updatedAt)
	thread.MessageCount = &messageCount
	preview := previewText(lastMessageBody, 180)
	thread.LastMessagePreview = &preview
	visibility, err := decodeThreadVisibilitySummary(ownedByMe, sharedTeamsJSON, matchedTeamsJSON, isPublic)
	if err != nil {
		return types.Thread{}, time.Time{}, err
	}
	thread.VisibilitySummary = visibility
	return thread, updatedAt, nil
}

func threadScanDest(thread *types.Thread, createdAt *time.Time, updatedAt *time.Time) []any {
	return []any{
		&thread.ID,
		&thread.OwnerUserID,
		&thread.Title,
		createdAt,
		updatedAt,
		&thread.CreatedBy,
		&thread.CreatedByUserID,
		&thread.CreatedByKeyID,
		&thread.CreatedByUserDisplayName,
		&thread.CreatedByActorName,
	}
}

func decodeThreadVisibilitySummary(ownedByMe bool, sharedTeamsJSON []byte, matchedTeamsJSON []byte, isPublic bool) (types.ThreadVisibilitySummary, error) {
	sharedTeams := []types.ThreadTeamSummary{}
	matchedTeams := []types.ThreadTeamSummary{}
	if len(sharedTeamsJSON) > 0 {
		if err := json.Unmarshal(sharedTeamsJSON, &sharedTeams); err != nil {
			return types.ThreadVisibilitySummary{}, err
		}
	}
	if len(matchedTeamsJSON) > 0 {
		if err := json.Unmarshal(matchedTeamsJSON, &matchedTeams); err != nil {
			return types.ThreadVisibilitySummary{}, err
		}
	}
	return types.ThreadVisibilitySummary{
		OwnedByMe:    ownedByMe,
		Private:      ownedByMe && len(sharedTeams) == 0 && !isPublic,
		SharedWithMe: !ownedByMe && len(matchedTeams) > 0,
		SharedTeams:  sharedTeams,
		MatchedTeams: matchedTeams,
		Public:       isPublic,
	}, nil
}

func newPrivateThreadVisibilitySummary() types.ThreadVisibilitySummary {
	return types.ThreadVisibilitySummary{
		OwnedByMe:    true,
		Private:      true,
		SharedTeams:  []types.ThreadTeamSummary{},
		MatchedTeams: []types.ThreadTeamSummary{},
	}
}

func scanMessage(row threadScanner, assets []types.Asset) (types.Message, error) {
	var createdAt time.Time
	var bodyContentType *string
	var message types.Message
	err := row.Scan(
		&message.ID,
		&message.ThreadID,
		&message.Author,
		&message.Body,
		&bodyContentType,
		&createdAt,
		&message.CreatedByUserID,
		&message.CreatedByKeyID,
		&message.CreatedByUserDisplayName,
		&message.CreatedByActorName,
	)
	message.BodyContentType = bodyContentType
	message.CreatedAt = isoMillis(createdAt)
	if assets == nil {
		message.Assets = []types.Asset{}
	} else {
		message.Assets = assets
	}
	return message, err
}

func scanAsset(row threadScanner) (types.Asset, error) {
	var createdAt time.Time
	var purgedAt *time.Time
	var purgeLastAttemptAt *time.Time
	var mimeType *string
	var asset types.Asset
	err := row.Scan(
		&asset.ID,
		&asset.MessageID,
		&asset.StorageKey,
		&asset.FileName,
		&mimeType,
		&asset.SizeBytes,
		&createdAt,
		&asset.CreatedBy,
		&asset.CreatedByUserID,
		&asset.CreatedByKeyID,
		&asset.CreatedByUserDisplayName,
		&asset.CreatedByActorName,
		&purgedAt,
		&asset.PurgedByUserID,
		&purgeLastAttemptAt,
		&asset.PurgeError,
	)
	asset.MimeType = mimeType
	asset.Filename = asset.FileName
	asset.DownloadURL = nil
	asset.CreatedAt = isoMillis(createdAt)
	asset.PurgedAt = optionalISOTime(purgedAt)
	asset.PurgeLastAttemptAt = optionalISOTime(purgeLastAttemptAt)
	return asset, err
}

func scanPendingUpload(row threadScanner) (types.PendingUpload, error) {
	var createdAt time.Time
	var expiresAt time.Time
	var consumedAt *time.Time
	var finalizationStartedAt *time.Time
	var rejectedAt *time.Time
	var mimeType *string
	var expectedSHA256 *string
	var finalStorageKey *string
	var finalizationToken *string
	var rejectionReason *string
	upload := types.PendingUpload{}
	err := row.Scan(
		&upload.ID,
		&upload.ThreadID,
		&upload.StorageKey,
		&upload.FileName,
		&mimeType,
		&upload.SizeBytes,
		&expectedSHA256,
		&upload.Status,
		&finalStorageKey,
		&finalizationToken,
		&finalizationStartedAt,
		&rejectedAt,
		&rejectionReason,
		&createdAt,
		&expiresAt,
		&upload.CreatedBy,
		&upload.CreatedByUserID,
		&upload.CreatedByKeyID,
		&upload.CreatedByUserDisplayName,
		&upload.CreatedByActorName,
		&consumedAt,
	)
	upload.MimeType = mimeType
	upload.ExpectedSHA256 = valueOrEmpty(expectedSHA256)
	upload.FinalStorageKey = valueOrEmpty(finalStorageKey)
	upload.FinalizationToken = valueOrEmpty(finalizationToken)
	upload.FinalizationStartedAt = optionalISOTime(finalizationStartedAt)
	upload.RejectedAt = optionalISOTime(rejectedAt)
	upload.RejectionReason = valueOrEmpty(rejectionReason)
	upload.CreatedAt = isoMillis(createdAt)
	upload.ExpiresAt = isoMillis(expiresAt)
	if consumedAt != nil {
		value := isoMillis(*consumedAt)
		upload.ConsumedAt = &value
	}
	return upload, err
}

func scanAPIKey(row threadScanner) (types.APIKey, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var lastUsedAt *time.Time
	var revokedAt *time.Time
	key := types.APIKey{}
	err := row.Scan(&key.ID, &key.UserID, &key.Name, &key.Purpose, &key.TokenPrefix, &key.TokenHash, &key.Scopes, &createdAt, &updatedAt, &lastUsedAt, &revokedAt)
	key.KeyMasked = maskSecret(key.TokenPrefix)
	key.CreatedAt = isoMillis(createdAt)
	key.UpdatedAt = isoMillis(updatedAt)
	if lastUsedAt != nil {
		value := isoMillis(*lastUsedAt)
		key.LastUsedAt = &value
	}
	if revokedAt != nil {
		value := isoMillis(*revokedAt)
		key.RevokedAt = &value
	}
	return key, err
}

func scanAPIKeyWithSetup(row threadScanner) (types.APIKey, string, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var lastUsedAt *time.Time
	var revokedAt *time.Time
	var setupBaseURL string
	key := types.APIKey{}
	err := row.Scan(
		&key.ID,
		&key.UserID,
		&key.Name,
		&key.Purpose,
		&key.TokenPrefix,
		&key.TokenHash,
		&key.Scopes,
		&createdAt,
		&updatedAt,
		&lastUsedAt,
		&revokedAt,
		&setupBaseURL,
	)
	key.KeyMasked = maskSecret(key.TokenPrefix)
	key.CreatedAt = isoMillis(createdAt)
	key.UpdatedAt = isoMillis(updatedAt)
	key.LastUsedAt = optionalISOTime(lastUsedAt)
	key.RevokedAt = optionalISOTime(revokedAt)
	return key, setupBaseURL, err
}

func scanAPIKeyAndUser(row threadScanner) (types.APIKey, types.User, error) {
	var keyCreatedAt time.Time
	var keyUpdatedAt time.Time
	var keyLastUsedAt *time.Time
	var keyRevokedAt *time.Time
	var userCreatedAt time.Time
	var userUpdatedAt time.Time
	var userDisabledAt *time.Time
	key := types.APIKey{}
	user := types.User{}
	err := row.Scan(
		&key.ID,
		&key.UserID,
		&key.Name,
		&key.Purpose,
		&key.TokenPrefix,
		&key.TokenHash,
		&key.Scopes,
		&keyCreatedAt,
		&keyUpdatedAt,
		&keyLastUsedAt,
		&keyRevokedAt,
		&user.ID,
		&user.Email,
		&user.DisplayName,
		&user.PasswordHash,
		&user.IsOwner,
		&userCreatedAt,
		&userUpdatedAt,
		&userDisabledAt,
	)
	key.KeyMasked = maskSecret(key.TokenPrefix)
	key.CreatedAt = isoMillis(keyCreatedAt)
	key.UpdatedAt = isoMillis(keyUpdatedAt)
	if keyLastUsedAt != nil {
		value := isoMillis(*keyLastUsedAt)
		key.LastUsedAt = &value
	}
	if keyRevokedAt != nil {
		value := isoMillis(*keyRevokedAt)
		key.RevokedAt = &value
	}
	user.CreatedAt = isoMillis(userCreatedAt)
	user.UpdatedAt = isoMillis(userUpdatedAt)
	if userDisabledAt != nil {
		value := isoMillis(*userDisabledAt)
		user.DisabledAt = &value
	}
	return key, user, err
}

func scanUser(row threadScanner) (types.User, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var disabledAt *time.Time
	user := types.User{}
	err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.IsOwner, &createdAt, &updatedAt, &disabledAt)
	user.CreatedAt = isoMillis(createdAt)
	user.UpdatedAt = isoMillis(updatedAt)
	if disabledAt != nil {
		value := isoMillis(*disabledAt)
		user.DisabledAt = &value
	}
	return user, err
}

func scanUserSession(row threadScanner) (types.UserSession, error) {
	var createdAt time.Time
	var lastUsedAt *time.Time
	var expiresAt time.Time
	var revokedAt *time.Time
	session := types.UserSession{}
	err := row.Scan(&session.ID, &session.UserID, &session.SecretHash, &createdAt, &lastUsedAt, &expiresAt, &revokedAt)
	session.CreatedAt = isoMillis(createdAt)
	session.ExpiresAt = isoMillis(expiresAt)
	if lastUsedAt != nil {
		value := isoMillis(*lastUsedAt)
		session.LastUsedAt = &value
	}
	if revokedAt != nil {
		value := isoMillis(*revokedAt)
		session.RevokedAt = &value
	}
	return session, err
}

func scanUserSessionAndUser(row threadScanner) (types.UserSession, types.User, error) {
	var sessionCreatedAt time.Time
	var sessionLastUsedAt *time.Time
	var expiresAt time.Time
	var revokedAt *time.Time
	var userCreatedAt time.Time
	var userUpdatedAt time.Time
	var disabledAt *time.Time
	session := types.UserSession{}
	user := types.User{}
	err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.SecretHash,
		&sessionCreatedAt,
		&sessionLastUsedAt,
		&expiresAt,
		&revokedAt,
		&user.ID,
		&user.Email,
		&user.DisplayName,
		&user.PasswordHash,
		&user.IsOwner,
		&userCreatedAt,
		&userUpdatedAt,
		&disabledAt,
	)
	session.CreatedAt = isoMillis(sessionCreatedAt)
	session.ExpiresAt = isoMillis(expiresAt)
	if sessionLastUsedAt != nil {
		value := isoMillis(*sessionLastUsedAt)
		session.LastUsedAt = &value
	}
	if revokedAt != nil {
		value := isoMillis(*revokedAt)
		session.RevokedAt = &value
	}
	user.CreatedAt = isoMillis(userCreatedAt)
	user.UpdatedAt = isoMillis(userUpdatedAt)
	if disabledAt != nil {
		value := isoMillis(*disabledAt)
		user.DisabledAt = &value
	}
	return session, user, err
}

func scanCLILoginCode(row threadScanner) (types.CLILoginCode, error) {
	var createdAt time.Time
	var expiresAt time.Time
	var consumedAt *time.Time
	code := types.CLILoginCode{}
	err := row.Scan(&code.ID, &code.UserID, &code.CodeHash, &code.StateHash, &code.RedirectURI, &createdAt, &expiresAt, &consumedAt)
	code.CreatedAt = isoMillis(createdAt)
	code.ExpiresAt = isoMillis(expiresAt)
	if consumedAt != nil {
		value := isoMillis(*consumedAt)
		code.ConsumedAt = &value
	}
	return code, err
}

func scanOwnerSetupToken(row threadScanner) (types.OwnerSetupToken, error) {
	var createdAt time.Time
	var expiresAt time.Time
	var consumedAt *time.Time
	var revokedAt *time.Time
	token := types.OwnerSetupToken{}
	err := row.Scan(&token.ID, &token.Purpose, &createdAt, &expiresAt, &consumedAt, &revokedAt)
	token.CreatedAt = isoMillis(createdAt)
	token.ExpiresAt = isoMillis(expiresAt)
	if consumedAt != nil {
		value := isoMillis(*consumedAt)
		token.ConsumedAt = &value
	}
	if revokedAt != nil {
		value := isoMillis(*revokedAt)
		token.RevokedAt = &value
	}
	return token, err
}

func scanSignupInvitation(row threadScanner) (types.SignupInvitation, error) {
	var createdAt time.Time
	var expiresAt time.Time
	var consumedAt *time.Time
	var revokedAt *time.Time
	invitation := types.SignupInvitation{}
	err := row.Scan(
		&invitation.ID,
		&invitation.CreatedByUserID,
		&createdAt,
		&expiresAt,
		&consumedAt,
		&invitation.ConsumedByUserID,
		&revokedAt,
	)
	invitation.CreatedAt = isoMillis(createdAt)
	invitation.ExpiresAt = isoMillis(expiresAt)
	if consumedAt != nil {
		value := isoMillis(*consumedAt)
		invitation.ConsumedAt = &value
	}
	if revokedAt != nil {
		value := isoMillis(*revokedAt)
		invitation.RevokedAt = &value
	}
	return invitation, err
}

func scanTeam(row threadScanner) (types.Team, error) {
	var createdAt time.Time
	var updatedAt time.Time
	team := types.Team{}
	err := row.Scan(&team.ID, &team.Slug, &team.Name, &createdAt, &updatedAt)
	team.CreatedAt = isoMillis(createdAt)
	team.UpdatedAt = isoMillis(updatedAt)
	return team, err
}

func scanTeams(rows pgx.Rows) ([]types.Team, error) {
	teams := []types.Team{}
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func scanTeamMembership(row threadScanner) (types.TeamMembership, error) {
	var createdAt time.Time
	membership := types.TeamMembership{}
	err := row.Scan(&membership.TeamID, &membership.UserID, &createdAt)
	membership.CreatedAt = isoMillis(createdAt)
	return membership, err
}

func scanThreadPublicLink(row threadScanner) (types.ThreadPublicLink, error) {
	var createdAt time.Time
	var updatedAt time.Time
	var revokedAt *time.Time
	link := types.ThreadPublicLink{}
	err := row.Scan(
		&link.ThreadID,
		&link.Token,
		&link.TokenHash,
		&link.TokenPrefix,
		&link.CreatedByUserID,
		&createdAt,
		&updatedAt,
		&revokedAt,
	)
	link.CreatedAt = isoMillis(createdAt)
	link.UpdatedAt = isoMillis(updatedAt)
	link.RevokedAt = optionalISOTime(revokedAt)
	return link, err
}

func hashSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func tokenPrefix(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func maskSecret(value string) string {
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalISOTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := isoMillis(*value)
	return &formatted
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func StorageKey(threadID string, messageHint string, fileName string) string {
	return fmt.Sprintf("agentbox/%s/%s/%s", threadID, messageHint, fileName)
}

func isoMillis(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}

func previewText(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func matchedSnippets(query string, title string, body string) []string {
	snippets := []string{}
	if strings.Contains(strings.ToLower(title), strings.ToLower(query)) {
		snippets = append(snippets, previewText(title, 180))
	}
	if body != "" {
		snippets = append(snippets, previewText(body, 240))
	}
	return snippets
}
