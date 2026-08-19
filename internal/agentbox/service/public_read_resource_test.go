package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/types"
)

type trackedPublicThreadLease struct {
	thread   types.ThreadWithMessages
	closeErr error
	closed   atomic.Bool
}

func (l *trackedPublicThreadLease) Thread() types.ThreadWithMessages { return l.thread }
func (l *trackedPublicThreadLease) Close(context.Context) error {
	l.closed.Store(true)
	return l.closeErr
}

type trackedAssetLease struct {
	asset    types.Asset
	closeErr error
	closed   atomic.Bool
}

func (l *trackedAssetLease) Asset() types.Asset { return l.asset }
func (l *trackedAssetLease) Close(context.Context) error {
	l.closed.Store(true)
	return l.closeErr
}

type publicReadRepository struct {
	*db.MemoryRepository
	threadLease       types.PublicThreadAuthorizationLease
	threadLeaseErr    error
	assetLeases       []types.AssetAuthorizationLease
	assetLeaseCalls   int
	denyAssetAfter    int
	assetLeaseErrorAt int
}

func (r *publicReadRepository) AcquirePublicThreadLease(context.Context, string) (types.PublicThreadAuthorizationLease, error) {
	return r.threadLease, r.threadLeaseErr
}

func (r *publicReadRepository) AcquirePublicAssetSigningLease(context.Context, string, string) (types.AssetAuthorizationLease, error) {
	r.assetLeaseCalls++
	if r.assetLeaseErrorAt > 0 && r.assetLeaseCalls == r.assetLeaseErrorAt {
		return nil, errors.New("simulated asset lease failure")
	}
	if r.denyAssetAfter > 0 && r.assetLeaseCalls > r.denyAssetAfter {
		return nil, nil
	}
	index := r.assetLeaseCalls - 1
	if index < 0 || index >= len(r.assetLeases) {
		return nil, nil
	}
	return r.assetLeases[index], nil
}

type observedHeadStore struct {
	*assets.FakeStore
	headCalls  atomic.Int32
	beforeHead func()
}

func (s *observedHeadStore) HeadAssetObject(ctx context.Context, storageKey string) (assets.ObjectMetadata, error) {
	s.headCalls.Add(1)
	if s.beforeHead != nil {
		s.beforeHead()
	}
	return s.FakeStore.HeadAssetObject(ctx, storageKey)
}

func TestPublicThreadClosesAuthorizationSnapshotBeforeRenderingWithoutR2Heads(t *testing.T) {
	imageType := "image/png"
	thread := types.ThreadWithMessages{
		Thread: types.Thread{ID: "thr_public_close", Title: "Public close", CreatedAt: "2026-08-04T00:00:00Z", UpdatedAt: "2026-08-04T00:00:00Z", CreatedBy: "Owner"},
		Messages: []types.Message{{
			ID: "msg_public_close", Author: "Owner", Body: "attachments", CreatedAt: "2026-08-04T00:00:00Z",
			Assets: []types.Asset{
				{ID: "asset_public_one", MessageID: "msg_public_close", StorageKey: "missing/one.png", FileName: "one.png", MimeType: &imageType, SizeBytes: 10},
				{ID: "asset_public_two", MessageID: "msg_public_close", StorageKey: "missing/two.png", FileName: "two.png", MimeType: &imageType, SizeBytes: 20},
			},
		}},
	}
	lease := &trackedPublicThreadLease{thread: thread}
	repository := &publicReadRepository{MemoryRepository: &db.MemoryRepository{}, threadLease: lease}
	store := &observedHeadStore{FakeStore: &assets.FakeStore{}}
	svc := New(repository, store)

	view, err := svc.GetPublicThread(t.Context(), "agpub_resource_test")
	if err != nil {
		t.Fatal(err)
	}
	if !lease.closed.Load() {
		t.Fatal("public thread authorization snapshot was not closed")
	}
	if store.headCalls.Load() != 0 {
		t.Fatalf("public payload performed %d eager object HEAD requests", store.headCalls.Load())
	}
	if len(view.Messages) != 1 || len(view.Messages[0].Assets) != 2 {
		t.Fatalf("public view=%#v", view)
	}
	for _, asset := range view.Messages[0].Assets {
		if asset.DownloadPath == "" || asset.PreviewPath == "" || asset.Unavailable {
			t.Fatalf("public payload should defer availability to signing endpoint: %#v", asset)
		}
	}
}

func TestPublicThreadCloseFailureIsReturnedBeforeAnyStorageWork(t *testing.T) {
	lease := &trackedPublicThreadLease{
		thread:   types.ThreadWithMessages{Thread: types.Thread{ID: "thr_close_failure"}},
		closeErr: errors.New("simulated commit failure"),
	}
	repository := &publicReadRepository{MemoryRepository: &db.MemoryRepository{}, threadLease: lease}
	store := &observedHeadStore{FakeStore: &assets.FakeStore{}}
	svc := New(repository, store)

	view, err := svc.GetPublicThread(t.Context(), "agpub_close_failure")
	if view != nil || err == nil || !strings.Contains(err.Error(), "close public thread authorization snapshot") || !strings.Contains(err.Error(), "simulated commit failure") {
		t.Fatalf("view=%#v err=%v", view, err)
	}
	if store.headCalls.Load() != 0 {
		t.Fatalf("close failure still performed %d storage calls", store.headCalls.Load())
	}
}

func TestPublicAssetSigningClosesBeforeR2AndReauthorizesImmediatelyBeforeSigning(t *testing.T) {
	digest := strings.Repeat("a", 64)
	mimeType := "image/png"
	asset := types.Asset{
		ID: "asset_two_phase", MessageID: "msg_two_phase",
		StorageKey: "agentbox/final/sha256/" + digest + "/usr/thr/msg/file.png",
		FileName:   "file.png", MimeType: &mimeType, SizeBytes: 12,
	}
	first := &trackedAssetLease{asset: asset}
	second := &trackedAssetLease{asset: asset}
	repository := &publicReadRepository{
		MemoryRepository: &db.MemoryRepository{},
		assetLeases:      []types.AssetAuthorizationLease{first, second},
	}
	baseStore := &assets.FakeStore{}
	baseStore.PutAssetObjectWithSHA(asset.StorageKey, asset.SizeBytes, asset.MimeType, digest)
	store := &observedHeadStore{FakeStore: baseStore}
	store.beforeHead = func() {
		if !first.closed.Load() {
			t.Fatal("R2 HEAD ran while first public authorization lease was open")
		}
	}
	svc := New(repository, store)

	signedURL, err := svc.PublicAssetPreviewURL(t.Context(), "agpub_two_phase", asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if repository.assetLeaseCalls != 2 || !first.closed.Load() || !second.closed.Load() || store.headCalls.Load() != 1 {
		t.Fatalf("calls=%d first_closed=%t second_closed=%t heads=%d", repository.assetLeaseCalls, first.closed.Load(), second.closed.Load(), store.headCalls.Load())
	}
	if !strings.Contains(signedURL, "response-content-disposition=inline") {
		t.Fatalf("signed URL=%q", signedURL)
	}
}

func TestPublicAssetSigningRejectsTokenRevocationDuringStorageInspection(t *testing.T) {
	digest := strings.Repeat("b", 64)
	mimeType := "text/plain"
	asset := types.Asset{
		ID: "asset_revoke_race", MessageID: "msg_revoke_race",
		StorageKey: "agentbox/final/sha256/" + digest + "/usr/thr/msg/file.txt",
		FileName:   "file.txt", MimeType: &mimeType, SizeBytes: 7,
	}
	first := &trackedAssetLease{asset: asset}
	repository := &publicReadRepository{
		MemoryRepository: &db.MemoryRepository{},
		assetLeases:      []types.AssetAuthorizationLease{first},
		denyAssetAfter:   1,
	}
	baseStore := &assets.FakeStore{}
	baseStore.PutAssetObjectWithSHA(asset.StorageKey, asset.SizeBytes, asset.MimeType, digest)
	store := &observedHeadStore{FakeStore: baseStore}
	svc := New(repository, store)

	signedURL, err := svc.PublicAssetDownloadURL(t.Context(), "agpub_revoked_during_head", asset.ID)
	if signedURL != "" || !hasCodedError(err, "PUBLIC_ASSET_NOT_FOUND") {
		t.Fatalf("signedURL=%q err=%v", signedURL, err)
	}
	if repository.assetLeaseCalls != 2 || !first.closed.Load() || store.headCalls.Load() != 1 {
		t.Fatalf("calls=%d first_closed=%t heads=%d", repository.assetLeaseCalls, first.closed.Load(), store.headCalls.Load())
	}
}

func TestPublicAssetSigningReturnsSecondLeaseCloseFailure(t *testing.T) {
	digest := strings.Repeat("c", 64)
	mimeType := "text/plain"
	asset := types.Asset{
		ID: "asset_second_close", MessageID: "msg_second_close",
		StorageKey: "agentbox/final/sha256/" + digest + "/usr/thr/msg/file.txt",
		FileName:   "file.txt", MimeType: &mimeType, SizeBytes: 8,
	}
	first := &trackedAssetLease{asset: asset}
	second := &trackedAssetLease{asset: asset, closeErr: errors.New("simulated signing commit failure")}
	repository := &publicReadRepository{
		MemoryRepository: &db.MemoryRepository{},
		assetLeases:      []types.AssetAuthorizationLease{first, second},
	}
	baseStore := &assets.FakeStore{}
	baseStore.PutAssetObjectWithSHA(asset.StorageKey, asset.SizeBytes, asset.MimeType, digest)
	svc := New(repository, &observedHeadStore{FakeStore: baseStore})

	signedURL, err := svc.PublicAssetDownloadURL(t.Context(), "agpub_second_close", asset.ID)
	if signedURL != "" || err == nil || !strings.Contains(err.Error(), "close public attachment signing authorization") || !strings.Contains(err.Error(), "simulated signing commit failure") {
		t.Fatalf("signedURL=%q err=%v", signedURL, err)
	}
}
