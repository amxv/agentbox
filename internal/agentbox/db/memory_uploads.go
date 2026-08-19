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
	for _, existing := range m.Messages {
		if existing.ThreadID == threadID && existing.Position >= message.Position {
			message.Position = existing.Position + 1
		}
	}
	if message.Position == 0 {
		message.Position = 1
	}
	m.Messages = append(m.Messages, message)
	m.Threads[threadIndex].UpdatedAt = isoMillis(time.Now())
	for index, asset := range newAssets {
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
			Position:                 int64(index + 1),
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
