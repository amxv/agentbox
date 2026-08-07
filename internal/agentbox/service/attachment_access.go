package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/backup"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/validate"
)

const (
	DefaultAttachmentReadBytes int64 = 64 * 1024
	MaxAttachmentReadBytes     int64 = 128 * 1024
	MinAttachmentReadBytes     int64 = utf8.UTFMax
	attachmentTextSampleBytes  int64 = 8 * 1024
)

type AttachmentSummary struct {
	ID        string `json:"id"`
	FileName  string `json:"file_name"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
}

type AttachmentReadRange struct {
	StartByte  int64  `json:"start_byte"`
	EndByte    int64  `json:"end_byte"`
	TotalBytes int64  `json:"total_bytes"`
	HasMore    bool   `json:"has_more"`
	NextOffset *int64 `json:"next_offset,omitempty"`
}

type AttachmentReadResult struct {
	Asset    AttachmentSummary   `json:"asset"`
	Encoding string              `json:"encoding"`
	Text     string              `json:"text"`
	Range    AttachmentReadRange `json:"range"`
}

type AttachmentDownload struct {
	Asset     AttachmentSummary
	URL       string
	ExpiresIn int
}

func (s *Service) ReadAttachment(ctx context.Context, auth types.AuthContext, assetID string, offsetBytes int64, maxBytes int64) (AttachmentReadResult, error) {
	if err := requireScope(auth, "assets:read"); err != nil {
		return AttachmentReadResult{}, err
	}
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return AttachmentReadResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "asset_id is required."}
	}
	if offsetBytes < 0 {
		return AttachmentReadResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "offset_bytes must be >= 0."}
	}
	if maxBytes == 0 {
		maxBytes = DefaultAttachmentReadBytes
	}
	if maxBytes < MinAttachmentReadBytes || maxBytes > MaxAttachmentReadBytes {
		return AttachmentReadResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: fmt.Sprintf("max_bytes must be between %d and %d.", MinAttachmentReadBytes, MaxAttachmentReadBytes)}
	}

	asset, err := s.authorizedAssetSnapshot(ctx, auth, assetID)
	if err != nil {
		return AttachmentReadResult{}, err
	}
	if offsetBytes > asset.SizeBytes {
		return AttachmentReadResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "offset_bytes cannot exceed the attachment size."}
	}
	metadata, err := s.inspectAvailableAssetMetadata(ctx, asset)
	if err != nil {
		return AttachmentReadResult{}, err
	}

	var sample []byte
	if asset.SizeBytes > 0 {
		sampleSize := minInt64(attachmentTextSampleBytes, asset.SizeBytes)
		sample, err = s.readAttachmentRange(ctx, asset, metadata.ETag, 0, sampleSize)
		if err != nil {
			return AttachmentReadResult{}, err
		}
		if !looksLikeUTF8Text(sample, sampleSize < asset.SizeBytes) {
			return AttachmentReadResult{}, CodedError{Code: "ATTACHMENT_NOT_TEXT", Message: "Attachment is not safely readable as UTF-8 text. Use download_attachment to retrieve the original file."}
		}
	}

	requestedSize := minInt64(maxBytes, asset.SizeBytes-offsetBytes)
	var raw []byte
	if requestedSize > 0 {
		if offsetBytes == 0 && int64(len(sample)) >= requestedSize {
			raw = append([]byte(nil), sample[:requestedSize]...)
		} else {
			raw, err = s.readAttachmentRange(ctx, asset, metadata.ETag, offsetBytes, requestedSize)
			if err != nil {
				return AttachmentReadResult{}, err
			}
		}
	}

	text, consumed, err := normalizeUTF8AttachmentChunk(raw, offsetBytes, asset.SizeBytes)
	if err != nil {
		return AttachmentReadResult{}, err
	}
	endByte := offsetBytes + consumed
	hasMore := endByte < asset.SizeBytes
	var nextOffset *int64
	if hasMore {
		next := endByte
		nextOffset = &next
	}

	if err := s.reauthorizeAssetSnapshot(ctx, auth, asset); err != nil {
		return AttachmentReadResult{}, err
	}
	return AttachmentReadResult{
		Asset:    attachmentSummary(asset),
		Encoding: "utf-8",
		Text:     text,
		Range: AttachmentReadRange{
			StartByte:  offsetBytes,
			EndByte:    endByte,
			TotalBytes: asset.SizeBytes,
			HasMore:    hasMore,
			NextOffset: nextOffset,
		},
	}, nil
}

func (s *Service) PrepareAttachmentDownload(ctx context.Context, auth types.AuthContext, assetID string, expiresInSeconds int) (AttachmentDownload, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return AttachmentDownload{}, CodedError{Code: "INVALID_ARGUMENT", Message: "asset_id is required."}
	}
	asset, err := s.authorizedAssetSnapshot(ctx, auth, assetID)
	if err != nil {
		return AttachmentDownload{}, err
	}
	safeExpires := validate.ClampSignedURLExpiry(expiresInSeconds)
	signedURL, err := s.SignedAssetDownloadURL(ctx, auth, assetID, safeExpires)
	if err != nil {
		return AttachmentDownload{}, err
	}
	if err := s.reauthorizeAssetSnapshot(ctx, auth, asset); err != nil {
		return AttachmentDownload{}, err
	}
	return AttachmentDownload{
		Asset:     attachmentSummary(asset),
		URL:       signedURL,
		ExpiresIn: safeExpires,
	}, nil
}

func (s *Service) authorizedAssetSnapshot(ctx context.Context, auth types.AuthContext, assetID string) (types.Asset, error) {
	if err := requireScope(auth, "assets:read"); err != nil {
		return types.Asset{}, err
	}
	lease, err := s.repo.AcquireAssetSigningLease(ctx, auth.UserID, assetID)
	if err != nil {
		return types.Asset{}, err
	}
	if lease == nil {
		return types.Asset{}, CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset not found."}
	}
	asset := lease.Asset()
	if err := lease.Close(ctx); err != nil {
		return types.Asset{}, fmt.Errorf("close attachment authorization snapshot: %w", err)
	}
	if asset.PurgedAt != nil {
		return types.Asset{}, CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	return asset, nil
}

func (s *Service) reauthorizeAssetSnapshot(ctx context.Context, auth types.AuthContext, original types.Asset) error {
	lease, err := s.repo.AcquireAssetSigningLease(ctx, auth.UserID, original.ID)
	if err != nil {
		return err
	}
	if lease == nil {
		return CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset not found."}
	}
	current := lease.Asset()
	if !sameAssetIdentity(original, current) {
		if err := lease.Close(ctx); err != nil {
			return fmt.Errorf("close changed attachment authorization: %w", err)
		}
		return CodedError{Code: "ATTACHMENT_NOT_FOUND", Message: "Asset changed before the operation completed."}
	}
	if current.PurgedAt != nil {
		if err := lease.Close(ctx); err != nil {
			return fmt.Errorf("close purged attachment authorization: %w", err)
		}
		return CodedError{Code: "ATTACHMENT_PURGED", Message: "Attachment deleted by deployment owner."}
	}
	if err := lease.Close(ctx); err != nil {
		return fmt.Errorf("close attachment authorization: %w", err)
	}
	return nil
}

func (s *Service) readAttachmentRange(ctx context.Context, asset types.Asset, expectedETag string, offsetBytes int64, maxBytes int64) ([]byte, error) {
	contents, err := s.assets.ReadAssetObjectRange(ctx, assets.ReadAssetRangeParams{
		StorageKey:   asset.StorageKey,
		OffsetBytes:  offsetBytes,
		MaxBytes:     maxBytes,
		ExpectedETag: expectedETag,
	})
	if errors.Is(err, backup.ErrObjectNotFound) {
		return nil, CodedError{Code: "ATTACHMENT_UNAVAILABLE", Message: "Attachment unavailable because its stored object is missing.", Err: err}
	}
	if errors.Is(err, assets.ErrObjectChanged) {
		return nil, CodedError{Code: "ATTACHMENT_UNAVAILABLE", Message: "Attachment unavailable because its stored object changed during the read.", Err: err}
	}
	if err != nil {
		return nil, fmt.Errorf("read attachment object: %w", err)
	}
	return contents, nil
}

func attachmentSummary(asset types.Asset) AttachmentSummary {
	mimeType := ""
	if asset.MimeType != nil {
		mimeType = strings.TrimSpace(*asset.MimeType)
	}
	return AttachmentSummary{ID: asset.ID, FileName: asset.FileName, MimeType: mimeType, SizeBytes: asset.SizeBytes}
}

func looksLikeUTF8Text(contents []byte, mayEndMidRune bool) bool {
	if len(contents) == 0 {
		return true
	}
	if len(contents) >= 3 && contents[0] == 0xef && contents[1] == 0xbb && contents[2] == 0xbf {
		contents = contents[3:]
	}
	validLength, ok := validUTF8Prefix(contents, mayEndMidRune)
	if !ok {
		return false
	}
	return hasTextLikeControls(contents[:validLength])
}

func normalizeUTF8AttachmentChunk(contents []byte, offsetBytes int64, totalBytes int64) (string, int64, error) {
	if len(contents) == 0 {
		return "", 0, nil
	}
	if offsetBytes > 0 && contents[0]&0xc0 == 0x80 {
		return "", 0, CodedError{Code: "INVALID_ARGUMENT", Message: "offset_bytes must point to a UTF-8 code point boundary. Follow next_offset from the previous read."}
	}
	bomLength := 0
	textBytes := contents
	if offsetBytes == 0 && len(contents) >= 3 && contents[0] == 0xef && contents[1] == 0xbb && contents[2] == 0xbf {
		bomLength = 3
		textBytes = contents[3:]
	}
	mayEndMidRune := offsetBytes+int64(len(contents)) < totalBytes
	validLength, ok := validUTF8Prefix(textBytes, mayEndMidRune)
	if !ok || !hasTextLikeControls(textBytes[:validLength]) {
		return "", 0, CodedError{Code: "ATTACHMENT_NOT_TEXT", Message: "Attachment is not safely readable as UTF-8 text. Use download_attachment to retrieve the original file."}
	}
	consumed := int64(bomLength + validLength)
	if consumed == 0 && len(contents) > 0 {
		return "", 0, CodedError{Code: "ATTACHMENT_NOT_TEXT", Message: "Attachment contains an incomplete or invalid UTF-8 sequence. Use download_attachment to retrieve the original file."}
	}
	return string(textBytes[:validLength]), consumed, nil
}

func validUTF8Prefix(contents []byte, allowIncompleteSuffix bool) (int, bool) {
	for offset := 0; offset < len(contents); {
		runeValue, size := utf8.DecodeRune(contents[offset:])
		if runeValue == utf8.RuneError && size == 1 {
			if allowIncompleteSuffix && !utf8.FullRune(contents[offset:]) {
				return offset, true
			}
			return 0, false
		}
		offset += size
		if offset == len(contents) {
			return offset, true
		}
	}
	return len(contents), true
}

func hasTextLikeControls(contents []byte) bool {
	if len(contents) == 0 {
		return true
	}
	controlCount := 0
	runeCount := 0
	for len(contents) > 0 {
		runeValue, size := utf8.DecodeRune(contents)
		if runeValue == utf8.RuneError && size == 1 {
			return false
		}
		contents = contents[size:]
		runeCount++
		if runeValue == 0 {
			return false
		}
		if (runeValue < 0x20 || (runeValue >= 0x7f && runeValue <= 0x9f)) && runeValue != '\t' && runeValue != '\n' && runeValue != '\r' && runeValue != '\f' && runeValue != 0x1b {
			controlCount++
		}
	}
	return controlCount <= 8 || controlCount*100 <= runeCount
}

func minInt64(left int64, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
