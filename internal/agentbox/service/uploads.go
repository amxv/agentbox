package service

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
	"github.com/google/uuid"
)

func (s *Service) PostMessage(ctx context.Context, auth types.AuthContext, params PostMessageParams) (types.Message, error) {
	if err := requireScope(auth, "threads:write"); err != nil {
		return types.Message{}, err
	}
	if params.File != nil || len(params.UploadedAssets) > 0 {
		if err := requireScope(auth, "assets:write"); err != nil {
			return types.Message{}, err
		}
	}
	if err := validate.PostMessage(params.ThreadID); err != nil {
		return types.Message{}, err
	}
	thread, err := s.repo.GetThread(ctx, auth.UserID, params.ThreadID)
	if err != nil {
		return types.Message{}, err
	}
	if thread == nil {
		return types.Message{}, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	bodyContentType, err := messageformat.Resolve(params.BodyContentType, params.Body, "")
	if err != nil {
		return types.Message{}, err
	}

	newAssets := []types.NewAsset{}
	if params.File != nil {
		asset, err := s.assets.UploadChatGPTFile(ctx, auth.UserID, params.ThreadID, *params.File)
		if err != nil {
			return types.Message{}, err
		}
		newAssets = append(newAssets, asset)
	}
	cleanupAndReturn := func(cause error) (types.Message, error) {
		return types.Message{}, s.compensateUploadedAssets(ctx, newAssets, cause)
	}

	if len(params.UploadedAssets) == 0 {
		message, err := s.repo.PostMessage(ctx, auth.UserID, params.ThreadID, auth, params.Body, &bodyContentType, newAssets)
		if err != nil {
			return cleanupAndReturn(err)
		}
		return message, nil
	}

	claimed, finalized, pendingUploadIDs, token, err := s.finalizePendingUploads(ctx, auth, params.ThreadID, params.UploadedAssets)
	if err != nil {
		return cleanupAndReturn(err)
	}
	message, err := s.repo.PostMessageWithFinalizedUploads(ctx, auth.UserID, params.ThreadID, auth, params.Body, &bodyContentType, newAssets, finalized, pendingUploadIDs, token)
	if err != nil {
		// Commit failures are deliberately not followed by eager final-object
		// deletion: the commit outcome can be ambiguous. The pre-registered
		// final-candidate cleanup rows delete only keys not referenced by an
		// active asset, so a committed canonical object is never removed.
		_ = s.repo.ReleasePendingUploadsFinalization(ctx, auth.UserID, params.ThreadID, auth, token, pendingUploadIDs, "")
		if errors.Is(err, types.ErrPendingUploadUnavailable) || errors.Is(err, types.ErrThreadNotFound) {
			return cleanupAndReturn(CodedError{Code: "UPLOAD_UNAVAILABLE", Message: "One or more uploads lost authorization or changed before finalization completed.", Err: err})
		}
		return cleanupAndReturn(err)
	}
	for _, upload := range claimed {
		// Delete staging immediately, but retain the due-at-expiry cleanup row.
		// If the still-valid presigned URL is replayed, the final expiry pass
		// deletes the recreated staging object without touching the final key.
		_ = s.assets.DeleteAssetObject(ctx, upload.StorageKey)
	}
	return message, nil
}

func (s *Service) PostMessageWithAsset(ctx context.Context, auth types.AuthContext, params PostMessageWithAssetParams) (types.Message, error) {
	if err := requireScope(auth, "threads:write"); err != nil {
		return types.Message{}, err
	}
	if len(params.Bytes) > 0 || params.FileName != "" {
		if err := requireScope(auth, "assets:write"); err != nil {
			return types.Message{}, err
		}
	}
	if err := validate.PostMessage(params.ThreadID); err != nil {
		return types.Message{}, err
	}
	thread, err := s.repo.GetThread(ctx, auth.UserID, params.ThreadID)
	if err != nil {
		return types.Message{}, err
	}
	if thread == nil {
		return types.Message{}, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	bodyContentType, err := messageformat.Resolve(params.BodyContentType, params.Body, "")
	if err != nil {
		return types.Message{}, err
	}
	newAssets := []types.NewAsset{}
	if len(params.Bytes) > 0 || params.FileName != "" {
		asset, err := s.assets.UploadAssetBytes(ctx, assets.UploadBytesParams{
			UserID:   auth.UserID,
			ThreadID: params.ThreadID,
			Bytes:    params.Bytes,
			FileName: params.FileName,
			MimeType: params.MimeType,
		})
		if err != nil {
			return types.Message{}, err
		}
		newAssets = append(newAssets, asset)
	}
	message, err := s.repo.PostMessage(ctx, auth.UserID, params.ThreadID, auth, params.Body, &bodyContentType, newAssets)
	if err != nil {
		return types.Message{}, s.compensateUploadedAssets(ctx, newAssets, err)
	}
	return message, nil
}

func (s *Service) CreatePresignedUploads(ctx context.Context, auth types.AuthContext, threadID string, files []types.UploadIntentFile) ([]types.PresignedUpload, error) {
	if err := requireScope(auth, "threads:write"); err != nil {
		return nil, err
	}
	if err := requireScope(auth, "assets:write"); err != nil {
		return nil, err
	}
	if err := validate.PostMessage(threadID); err != nil {
		return nil, err
	}
	thread, err := s.repo.GetThread(ctx, auth.UserID, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
	}
	if len(files) == 0 {
		return []types.PresignedUpload{}, nil
	}
	if len(files) > 10 {
		return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "At most 10 files can be uploaded at once."}
	}
	_, _ = s.cleanupPendingUploads(ctx, 10)

	validated := make([]types.UploadIntentFile, len(files))
	for index, file := range files {
		file.FileName = strings.TrimSpace(file.FileName)
		file.SHA256 = strings.ToLower(strings.TrimSpace(file.SHA256))
		if file.FileName == "" {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "file_name is required."}
		}
		if file.SizeBytes < 0 {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "size_bytes must be >= 0."}
		}
		if !sha256HexPattern.MatchString(file.SHA256) {
			return nil, CodedError{Code: "INVALID_ARGUMENT", Message: "sha256 must be exactly 64 hexadecimal characters."}
		}
		validated[index] = file
	}

	uploads := make([]types.PresignedUpload, 0, len(validated))
	pending := make([]types.PendingUpload, 0, len(validated))
	for _, file := range validated {
		uploadID := "upl_" + uuid.NewString()
		presigned, err := s.assets.CreatePresignedAssetUploadURL(ctx, assets.PresignedUploadParams{
			UserID:           auth.UserID,
			ThreadID:         threadID,
			UploadID:         uploadID,
			FileName:         file.FileName,
			MimeType:         file.MimeType,
			SizeBytes:        file.SizeBytes,
			SHA256:           file.SHA256,
			ExpiresInSeconds: 900,
		})
		if err != nil {
			return nil, err
		}
		expiresAt := time.Now().UTC().Add(time.Duration(presigned.ExpiresIn) * time.Second).Format(time.RFC3339)
		pending = append(pending, types.PendingUpload{
			ID:                       presigned.UploadID,
			ThreadID:                 threadID,
			StorageKey:               presigned.StorageKey,
			FileName:                 presigned.FileName,
			MimeType:                 presigned.MimeType,
			SizeBytes:                presigned.SizeBytes,
			ExpectedSHA256:           presigned.SHA256,
			Status:                   "pending",
			ExpiresAt:                expiresAt,
			CreatedBy:                auth.ActorName,
			CreatedByUserID:          optionalString(auth.UserID),
			CreatedByKeyID:           optionalString(auth.KeyID),
			CreatedByUserDisplayName: optionalString(auth.UserDisplayName),
			CreatedByActorName:       optionalString(auth.ActorName),
		})
		uploads = append(uploads, presigned)
	}
	if _, err := s.repo.CreatePendingUploads(ctx, auth.UserID, pending); err != nil {
		if errors.Is(err, types.ErrThreadNotFound) {
			return nil, CodedError{Code: "THREAD_NOT_FOUND", Message: ErrThreadNotFound.Error(), Err: ErrThreadNotFound}
		}
		if errors.Is(err, types.ErrPendingUploadQuotaExceeded) {
			return nil, CodedError{Code: "UPLOAD_QUOTA_EXCEEDED", Message: "Too many unconsumed uploads are active. Wait for expiry or finalize existing uploads.", Err: err}
		}
		return nil, err
	}
	return uploads, nil
}

func (s *Service) finalizePendingUploads(ctx context.Context, auth types.AuthContext, threadID string, refs []types.UploadedAssetReference) ([]types.PendingUpload, []types.NewAsset, []string, string, error) {
	ids := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		id := strings.TrimSpace(ref.UploadID)
		if id == "" {
			return nil, nil, nil, "", CodedError{Code: "INVALID_ARGUMENT", Message: "upload_id is required."}
		}
		if seen[id] {
			return nil, nil, nil, "", CodedError{Code: "INVALID_ARGUMENT", Message: "upload_id values must be unique."}
		}
		seen[id] = true
		ids = append(ids, id)
	}
	pending, err := s.repo.GetPendingUploads(ctx, auth.UserID, threadID, ids, auth)
	if err != nil {
		return nil, nil, nil, "", err
	}
	byID := map[string]types.PendingUpload{}
	for _, upload := range pending {
		byID[upload.ID] = upload
	}
	targets := make([]types.UploadFinalizationTarget, 0, len(ids))
	for _, id := range ids {
		upload, ok := byID[id]
		if !ok || upload.ConsumedAt != nil || upload.Status != "pending" || !sha256HexPattern.MatchString(upload.ExpectedSHA256) {
			return nil, nil, nil, "", CodedError{Code: "UPLOAD_UNAVAILABLE", Message: "Upload was not found, is not pending, or has already been used."}
		}
		targets = append(targets, types.UploadFinalizationTarget{
			UploadID:        id,
			FinalStorageKey: assets.MakeFinalStorageKey(auth.UserID, threadID, id, upload.FileName, upload.ExpectedSHA256),
		})
	}
	token := "fin_" + uuid.NewString()
	claimed, err := s.repo.ClaimPendingUploadsForFinalization(ctx, auth.UserID, threadID, auth, token, targets)
	if errors.Is(err, types.ErrPendingUploadFinalizing) {
		return nil, nil, nil, "", CodedError{Code: "UPLOAD_FINALIZING", Message: "One or more uploads are already being finalized.", Err: err}
	}
	if errors.Is(err, types.ErrPendingUploadUnavailable) || errors.Is(err, types.ErrThreadNotFound) {
		return nil, nil, nil, "", CodedError{Code: "UPLOAD_UNAVAILABLE", Message: "One or more uploads expired, changed, or lost authorization before finalization.", Err: err}
	}
	if err != nil {
		return nil, nil, nil, "", err
	}
	copied := []types.NewAsset{}
	fail := func(cause error, reject bool) ([]types.PendingUpload, []types.NewAsset, []string, string, error) {
		for _, asset := range copied {
			_ = s.assets.DeleteAssetObject(ctx, asset.StorageKey)
		}
		reason := ""
		if reject {
			reason = cause.Error()
		}
		_ = s.repo.ReleasePendingUploadsFinalization(ctx, auth.UserID, threadID, auth, token, ids, reason)
		if reject {
			_, _ = s.cleanupPendingUploads(ctx, 10)
		}
		return nil, nil, nil, "", cause
	}
	for _, upload := range claimed {
		metadata, err := s.assets.HeadAssetObject(ctx, upload.StorageKey)
		if errors.Is(err, assets.ErrObjectNotFound) {
			return fail(CodedError{Code: "UPLOAD_NOT_MATERIALIZED", Message: "The staging object does not exist yet. Retry the upload before finalizing.", Err: err}, false)
		}
		if err != nil {
			return fail(fmt.Errorf("inspect staging upload %s: %w", upload.ID, err), false)
		}
		if err := verifyUploadObject(upload, metadata); err != nil {
			return fail(CodedError{Code: "UPLOAD_METADATA_MISMATCH", Message: err.Error(), Err: err}, true)
		}
		if strings.TrimSpace(metadata.ETag) == "" {
			return fail(CodedError{Code: "UPLOAD_METADATA_MISMATCH", Message: "Uploaded staging object has no stable ETag for conditional promotion."}, true)
		}
		finalMetadata, err := s.assets.CopyAssetObject(ctx, upload.StorageKey, upload.FinalStorageKey, metadata.ETag)
		if err != nil {
			return fail(fmt.Errorf("promote staging upload %s: %w", upload.ID, err), false)
		}
		if err := verifyUploadObject(upload, finalMetadata); err != nil {
			return fail(CodedError{Code: "UPLOAD_METADATA_MISMATCH", Message: "Promoted object failed identity verification: " + err.Error(), Err: err}, true)
		}
		if assets.SHA256FromFinalStorageKey(upload.FinalStorageKey) != upload.ExpectedSHA256 {
			return fail(CodedError{Code: "UPLOAD_METADATA_MISMATCH", Message: "Promoted object key does not encode the expected SHA-256 identity."}, true)
		}
		asset := types.NewAsset{
			StorageKey:    upload.FinalStorageKey,
			FileName:      upload.FileName,
			MimeType:      upload.MimeType,
			SizeBytes:     upload.SizeBytes,
			ContentSHA256: upload.ExpectedSHA256,
		}
		copied = append(copied, asset)
	}
	return claimed, copied, ids, token, nil
}

func verifyUploadObject(upload types.PendingUpload, metadata assets.ObjectMetadata) error {
	if metadata.SizeBytes != upload.SizeBytes {
		return fmt.Errorf("uploaded object size %d does not match the signed size %d", metadata.SizeBytes, upload.SizeBytes)
	}
	if upload.MimeType != nil && (metadata.ContentType == nil || normalizeContentType(*metadata.ContentType) != normalizeContentType(*upload.MimeType)) {
		return errors.New("uploaded object content type does not match the signed content type")
	}
	actualSHA256 := strings.ToLower(strings.TrimSpace(metadata.Metadata["agentbox-sha256"]))
	if actualSHA256 != upload.ExpectedSHA256 {
		return errors.New("uploaded object SHA-256 identity does not match the signed checksum")
	}
	if metadata.ChecksumSHA256 != "" {
		expectedBytes, _ := hex.DecodeString(upload.ExpectedSHA256)
		expectedBase64 := base64.StdEncoding.EncodeToString(expectedBytes)
		if strings.TrimSpace(metadata.ChecksumSHA256) != expectedBase64 {
			return errors.New("storage-reported SHA-256 checksum does not match the signed checksum")
		}
	}
	return nil
}

func (s *Service) CleanupPendingUploads(ctx context.Context, limit int) (types.UploadCleanupResult, error) {
	return s.cleanupPendingUploads(ctx, limit)
}

func (s *Service) cleanupPendingUploads(ctx context.Context, limit int) (types.UploadCleanupResult, error) {
	candidates, err := s.repo.ListUploadCleanupCandidates(ctx, limit)
	if err != nil {
		return types.UploadCleanupResult{}, err
	}
	result := types.UploadCleanupResult{Failures: []string{}}
	for _, candidate := range candidates {
		result.Attempted++
		if err := s.assets.DeleteAssetObject(ctx, candidate.StorageKey); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, candidate.ID+": "+err.Error())
			_ = s.repo.MarkUploadCleanupFailure(ctx, candidate.ID, err.Error())
			continue
		}
		if err := s.repo.MarkUploadCleanupSuccess(ctx, candidate.ID); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, candidate.ID+": "+err.Error())
			continue
		}
		result.Cleaned++
	}
	return result, nil
}

func normalizeContentType(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if index := strings.IndexByte(value, ';'); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	return value
}

func (s *Service) compensateUploadedAssets(ctx context.Context, uploaded []types.NewAsset, cause error) error {
	if len(uploaded) == 0 {
		return cause
	}
	cleanupErrors := []string{}
	for _, asset := range uploaded {
		if err := s.assets.DeleteAssetObject(ctx, asset.StorageKey); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("%s: %v", asset.StorageKey, err))
		}
	}
	if len(cleanupErrors) == 0 {
		return cause
	}
	return fmt.Errorf("%w; uploaded-object cleanup also failed: %s", cause, strings.Join(cleanupErrors, "; "))
}

func (s *Service) SignedAssetDownloadURL(ctx context.Context, auth types.AuthContext, assetID string, expiresInSeconds int) (string, error) {
	return s.signedAssetURL(ctx, auth, assetID, expiresInSeconds, false)
}

func (s *Service) SignedAssetPreviewURL(ctx context.Context, auth types.AuthContext, assetID string, expiresInSeconds int) (string, error) {
	return s.signedAssetURL(ctx, auth, assetID, expiresInSeconds, true)
}

func (s *Service) signedAssetURL(ctx context.Context, auth types.AuthContext, assetID string, expiresInSeconds int, inline bool) (string, error) {
	if err := requireScope(auth, "assets:read"); err != nil {
		return "", err
	}
	lease, err := s.repo.AcquireAssetSigningLease(ctx, auth.UserID, assetID)
	if err != nil {
		return "", err
	}
	if lease == nil {
		return "", CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset not found."}
	}
	asset := lease.Asset()
	if err := lease.Close(ctx); err != nil {
		return "", fmt.Errorf("close attachment authorization snapshot: %w", err)
	}
	if asset.PurgedAt != nil {
		return "", CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	if inline && !supportsDashboardInlinePreview(asset) {
		return "", CodedError{Code: "INVALID_ARGUMENT", Message: "This attachment type does not support inline preview."}
	}
	if err := s.inspectAvailableAsset(ctx, asset); err != nil {
		return "", err
	}

	signingLease, err := s.repo.AcquireAssetSigningLease(ctx, auth.UserID, assetID)
	if err != nil {
		return "", err
	}
	if signingLease == nil {
		return "", CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset not found."}
	}
	signingAsset := signingLease.Asset()
	if !sameAssetIdentity(asset, signingAsset) {
		if err := signingLease.Close(ctx); err != nil {
			return "", fmt.Errorf("close changed attachment signing authorization: %w", err)
		}
		return "", CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset changed before signing."}
	}
	if signingAsset.PurgedAt != nil {
		if err := signingLease.Close(ctx); err != nil {
			return "", fmt.Errorf("close purged attachment signing authorization: %w", err)
		}
		return "", CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	signedURL, err := s.createSignedAssetURL(ctx, signingAsset, validate.ClampSignedURLExpiry(expiresInSeconds), inline)
	if err != nil {
		if closeErr := signingLease.Close(ctx); closeErr != nil {
			return "", fmt.Errorf("sign attachment: %v; close signing authorization: %w", err, closeErr)
		}
		return "", err
	}
	if err := signingLease.Close(ctx); err != nil {
		return "", fmt.Errorf("close attachment signing authorization: %w", err)
	}
	return signedURL, nil
}

func (s *Service) inspectAvailableAsset(ctx context.Context, asset types.Asset) error {
	_, err := s.inspectAvailableAssetMetadata(ctx, asset)
	return err
}

func (s *Service) inspectAvailableAssetMetadata(ctx context.Context, asset types.Asset) (assets.ObjectMetadata, error) {
	metadata, err := s.assets.HeadAssetObject(ctx, asset.StorageKey)
	if errors.Is(err, assets.ErrObjectNotFound) {
		return assets.ObjectMetadata{}, CodedError{Code: "ATTACHMENT_UNAVAILABLE", Message: "Attachment unavailable because its stored object is missing.", Err: err}
	}
	if err != nil {
		return assets.ObjectMetadata{}, fmt.Errorf("inspect attachment object: %w", err)
	}
	if metadata.SizeBytes != asset.SizeBytes {
		return assets.ObjectMetadata{}, CodedError{Code: "ATTACHMENT_UNAVAILABLE", Message: "Attachment unavailable because its stored object does not match the recorded metadata."}
	}
	if expectedSHA256 := assets.SHA256FromFinalStorageKey(asset.StorageKey); expectedSHA256 != "" {
		actualSHA256 := strings.ToLower(strings.TrimSpace(metadata.Metadata["agentbox-sha256"]))
		if actualSHA256 != expectedSHA256 {
			return assets.ObjectMetadata{}, CodedError{Code: "ATTACHMENT_UNAVAILABLE", Message: "Attachment unavailable because its stored object failed SHA-256 identity verification."}
		}
		if metadata.ChecksumSHA256 != "" {
			expectedBytes, _ := hex.DecodeString(expectedSHA256)
			if strings.TrimSpace(metadata.ChecksumSHA256) != base64.StdEncoding.EncodeToString(expectedBytes) {
				return assets.ObjectMetadata{}, CodedError{Code: "ATTACHMENT_UNAVAILABLE", Message: "Attachment unavailable because its storage checksum failed identity verification."}
			}
		}
	}
	return metadata, nil
}

func (s *Service) createSignedAssetURL(ctx context.Context, asset types.Asset, expiresInSeconds int, inline bool) (string, error) {
	return s.assets.CreateSignedAssetDownloadURL(ctx, assets.SignedURLParams{
		StorageKey:       asset.StorageKey,
		FileName:         asset.FileName,
		MimeType:         asset.MimeType,
		ExpiresInSeconds: expiresInSeconds,
		Inline:           inline,
	})
}

func sameAssetIdentity(left types.Asset, right types.Asset) bool {
	return left.ID == right.ID &&
		left.MessageID == right.MessageID &&
		left.StorageKey == right.StorageKey &&
		left.FileName == right.FileName &&
		left.SizeBytes == right.SizeBytes &&
		sameOptionalText(left.MimeType, right.MimeType)
}

func sameOptionalText(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return strings.TrimSpace(*left) == strings.TrimSpace(*right)
}
