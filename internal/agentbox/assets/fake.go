package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentbox/internal/agentbox/types"
)

type FakeStore struct {
	MaxFileSizeBytes int64
	AssetBucket      string
	Uploads          []types.NewAsset
	ChatGPTInputs    []ChatGPTFileInput
	ChatGPTFailure   error
	Buckets          map[string]map[string]ObjectMetadata
	ObjectBytes      map[string]map[string][]byte
	CopyCalls        []copyObjectRequest
	DeleteCalls      []string
	ReadCalls        []ReadAssetRangeParams
	SignedURLCalls   []SignedURLParams
	HeadFailures     map[string]error
	CopyFailures     map[string]error
	DeleteFailures   map[string]error
	ReadFailures     map[string]error
	AfterUpload      func(types.NewAsset)
	BeforeDelete     func(string)
	BeforeRead       func(ReadAssetRangeParams)
	AfterRead        func(ReadAssetRangeParams, []byte)
	mutex            sync.Mutex
}

func (f *FakeStore) UploadAssetBytes(_ context.Context, params UploadBytesParams) (types.NewAsset, error) {
	f.mutex.Lock()
	limit := f.MaxFileSizeBytes
	if limit == 0 {
		limit = 25 * 1024 * 1024
	}
	if int64(len(params.Bytes)) > limit {
		f.mutex.Unlock()
		return types.NewAsset{}, errTooLarge(limit)
	}
	fileName := SanitizeFilename(params.FileName)
	digest := sha256.Sum256(params.Bytes)
	digestHex := hex.EncodeToString(digest[:])
	storageKey := MakeFinalStorageKey(params.UserID, params.ThreadID, defaultString(params.MessageHint, "message"), fileName, digestHex)
	asset := types.NewAsset{StorageKey: storageKey, FileName: fileName, MimeType: InferMimeType(fileName, params.MimeType), SizeBytes: int64(len(params.Bytes))}
	asset.ContentSHA256 = digestHex
	f.Uploads = append(f.Uploads, asset)
	bucket := f.AssetBucket
	if bucket == "" {
		bucket = "assets"
	}
	f.ensureBucket(bucket)
	f.ensureByteBucket(bucket)
	now := time.Now().UTC()
	f.Buckets[bucket][storageKey] = ObjectMetadata{Bucket: bucket, Key: storageKey, SizeBytes: asset.SizeBytes, ETag: digestHex, ContentType: asset.MimeType, Metadata: map[string]string{"agentbox-sha256": digestHex}, LastModified: &now}
	f.ObjectBytes[bucket][storageKey] = append([]byte(nil), params.Bytes...)
	hook := f.AfterUpload
	f.mutex.Unlock()
	if hook != nil {
		hook(asset)
	}
	return asset, nil
}

func (f *FakeStore) PutObject(bucket string, key string, sizeBytes int64, etag string) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.ensureBucket(bucket)
	now := time.Now().UTC()
	f.Buckets[bucket][key] = ObjectMetadata{
		Bucket:       bucket,
		Key:          key,
		SizeBytes:    sizeBytes,
		ETag:         normalizeETag(etag),
		LastModified: &now,
	}
}

func (f *FakeStore) PutAssetObject(key string, sizeBytes int64, contentType *string) {
	f.PutAssetObjectWithSHA(key, sizeBytes, contentType, "")
}

func (f *FakeStore) PutAssetObjectWithSHA(key string, sizeBytes int64, contentType *string, digestHex string) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	bucket := f.AssetBucket
	if bucket == "" {
		bucket = "assets"
	}
	f.ensureBucket(bucket)
	now := time.Now().UTC()
	metadata := map[string]string{}
	if strings.TrimSpace(digestHex) != "" {
		metadata["agentbox-sha256"] = strings.ToLower(strings.TrimSpace(digestHex))
	}
	f.Buckets[bucket][key] = ObjectMetadata{
		Bucket:       bucket,
		Key:          key,
		SizeBytes:    sizeBytes,
		ETag:         defaultString(strings.TrimSpace(digestHex), "etag-"+strings.ReplaceAll(key, "/", "-")),
		ContentType:  contentType,
		Metadata:     metadata,
		LastModified: &now,
	}
}

func (f *FakeStore) PutAssetBytes(key string, contents []byte, contentType *string) {
	digest := sha256.Sum256(contents)
	digestHex := hex.EncodeToString(digest[:])
	f.mutex.Lock()
	defer f.mutex.Unlock()
	bucket := f.AssetBucket
	if bucket == "" {
		bucket = "assets"
	}
	f.ensureBucket(bucket)
	f.ensureByteBucket(bucket)
	now := time.Now().UTC()
	f.Buckets[bucket][key] = ObjectMetadata{
		Bucket:       bucket,
		Key:          key,
		SizeBytes:    int64(len(contents)),
		ETag:         digestHex,
		ContentType:  contentType,
		Metadata:     map[string]string{"agentbox-sha256": digestHex},
		LastModified: &now,
	}
	f.ObjectBytes[bucket][key] = append([]byte(nil), contents...)
}

func (f *FakeStore) headObject(_ context.Context, bucket string, key string) (ObjectMetadata, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if err := f.HeadFailures[failureKey(bucket, key)]; err != nil {
		return ObjectMetadata{}, err
	}
	objects := f.Buckets[bucket]
	object, ok := objects[key]
	if !ok {
		return ObjectMetadata{}, fmtObjectNotFound(bucket, key)
	}
	return object, nil
}

func (f *FakeStore) copyObject(_ context.Context, request copyObjectRequest) (ObjectMetadata, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if err := f.CopyFailures[failureKey(request.DestinationBucket, request.DestinationKey)]; err != nil {
		return ObjectMetadata{}, err
	}
	source, ok := f.Buckets[request.SourceBucket][request.SourceKey]
	if !ok {
		return ObjectMetadata{}, fmtObjectNotFound(request.SourceBucket, request.SourceKey)
	}
	if request.ExpectedETag != "" && normalizeETag(request.ExpectedETag) != source.ETag {
		return ObjectMetadata{}, errors.New("source ETag changed before copy")
	}
	f.ensureBucket(request.DestinationBucket)
	f.ensureByteBucket(request.DestinationBucket)
	now := time.Now().UTC()
	copied := source
	copied.Bucket = request.DestinationBucket
	copied.Key = request.DestinationKey
	copied.LastModified = &now
	f.Buckets[request.DestinationBucket][request.DestinationKey] = copied
	if sourceBytes, ok := f.ObjectBytes[request.SourceBucket][request.SourceKey]; ok {
		f.ObjectBytes[request.DestinationBucket][request.DestinationKey] = append([]byte(nil), sourceBytes...)
	}
	f.CopyCalls = append(f.CopyCalls, request)
	return copied, nil
}

func (f *FakeStore) CreatePresignedAssetUploadURL(_ context.Context, params PresignedUploadParams) (types.PresignedUpload, error) {
	limit := f.MaxFileSizeBytes
	if limit == 0 {
		limit = 25 * 1024 * 1024
	}
	if params.SizeBytes > limit {
		return types.PresignedUpload{}, errTooLarge(limit)
	}
	digestHex, _, err := normalizeSHA256(params.SHA256)
	if err != nil {
		return types.PresignedUpload{}, err
	}
	fileName := SanitizeFilename(params.FileName)
	storageKey := MakeStagingStorageKey(params.UserID, params.ThreadID, defaultString(params.UploadID, "upload"), fileName)
	mimeType := InferMimeType(fileName, params.MimeType)
	contentType := "application/octet-stream"
	if mimeType != nil {
		contentType = *mimeType
	}
	return types.PresignedUpload{
		UploadID:   params.UploadID,
		StorageKey: storageKey,
		FileName:   fileName,
		MimeType:   mimeType,
		SizeBytes:  params.SizeBytes,
		SHA256:     digestHex,
		UploadURL:  "https://r2-upload.test/" + storageKey,
		ExpiresIn:  900,
		RequiredHeaders: map[string]string{
			"content-type":               contentType,
			"x-amz-meta-agentbox-sha256": digestHex,
		},
	}, nil
}

func (f *FakeStore) CreateSignedAssetDownloadURL(_ context.Context, params SignedURLParams) (string, error) {
	f.mutex.Lock()
	f.SignedURLCalls = append(f.SignedURLCalls, params)
	f.mutex.Unlock()
	u := url.URL{Scheme: "https", Host: "r2.test", Path: "/" + params.StorageKey}
	q := u.Query()
	expires := params.ExpiresInSeconds
	if expires == 0 {
		expires = 300
	}
	q.Set("X-Amz-Expires", strconv.Itoa(expires))
	disposition := "attachment"
	if params.Inline {
		disposition = "inline"
	}
	q.Set("response-content-disposition", disposition+`; filename="`+params.FileName+`"`)
	if params.MimeType != nil {
		q.Set("response-content-type", *params.MimeType)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (f *FakeStore) HeadAssetObject(ctx context.Context, storageKey string) (ObjectMetadata, error) {
	bucket := f.AssetBucket
	if bucket == "" {
		bucket = "assets"
	}
	return f.headObject(ctx, bucket, storageKey)
}

func (f *FakeStore) ReadAssetObjectRange(_ context.Context, params ReadAssetRangeParams) ([]byte, error) {
	storageKey := strings.TrimSpace(params.StorageKey)
	if storageKey == "" {
		return nil, errors.New("asset storage key is required")
	}
	if params.OffsetBytes < 0 {
		return nil, errors.New("asset read offset must be >= 0")
	}
	if params.MaxBytes <= 0 {
		return nil, errors.New("asset read size must be > 0")
	}

	f.mutex.Lock()
	bucket := f.AssetBucket
	if bucket == "" {
		bucket = "assets"
	}
	f.ReadCalls = append(f.ReadCalls, params)
	failure := f.ReadFailures[storageKey]
	metadata, exists := f.Buckets[bucket][storageKey]
	contents, hasBytes := f.ObjectBytes[bucket][storageKey]
	beforeRead := f.BeforeRead
	afterRead := f.AfterRead
	f.mutex.Unlock()

	if beforeRead != nil {
		beforeRead(params)
	}
	if failure != nil {
		return nil, failure
	}
	if !exists {
		return nil, fmtObjectNotFound(bucket, storageKey)
	}
	if expectedETag := normalizeETag(params.ExpectedETag); expectedETag != "" && expectedETag != normalizeETag(metadata.ETag) {
		return nil, errors.Join(ErrObjectChanged, errors.New("asset object ETag changed before read"))
	}
	if !hasBytes {
		return nil, errors.New("fake asset object has metadata but no readable bytes")
	}
	if params.OffsetBytes > int64(len(contents)) {
		return nil, errors.Join(ErrObjectChanged, errors.New("asset read range is no longer satisfiable"))
	}
	end := params.OffsetBytes + params.MaxBytes
	if end < params.OffsetBytes || end > int64(len(contents)) {
		end = int64(len(contents))
	}
	result := append([]byte(nil), contents[params.OffsetBytes:end]...)
	if afterRead != nil {
		afterRead(params, append([]byte(nil), result...))
	}
	return result, nil
}

func (f *FakeStore) CopyAssetObject(ctx context.Context, sourceStorageKey string, destinationStorageKey string, expectedETag string) (ObjectMetadata, error) {
	bucket := f.AssetBucket
	if bucket == "" {
		bucket = "assets"
	}
	return f.copyObject(ctx, copyObjectRequest{
		SourceBucket:      bucket,
		SourceKey:         sourceStorageKey,
		DestinationBucket: bucket,
		DestinationKey:    destinationStorageKey,
		ExpectedETag:      expectedETag,
	})
}

func (f *FakeStore) DeleteAssetObject(_ context.Context, storageKey string) error {
	storageKey = strings.TrimSpace(storageKey)
	if storageKey == "" {
		return errors.New("asset storage key is required")
	}
	f.mutex.Lock()
	f.DeleteCalls = append(f.DeleteCalls, storageKey)
	failure := f.DeleteFailures[storageKey]
	hook := f.BeforeDelete
	f.mutex.Unlock()
	if failure != nil {
		return failure
	}
	if hook != nil {
		hook(storageKey)
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	for bucket, objects := range f.Buckets {
		delete(objects, storageKey)
		f.Buckets[bucket] = objects
		if f.ObjectBytes != nil && f.ObjectBytes[bucket] != nil {
			delete(f.ObjectBytes[bucket], storageKey)
		}
	}
	return nil
}

func (f *FakeStore) UploadChatGPTFile(ctx context.Context, userID string, threadID string, input ChatGPTFileInput) (types.NewAsset, error) {
	file, err := NormalizeChatGPTFileInput(input)
	if err != nil {
		return types.NewAsset{}, err
	}
	f.mutex.Lock()
	f.ChatGPTInputs = append(f.ChatGPTInputs, file)
	failure := f.ChatGPTFailure
	f.mutex.Unlock()
	if failure != nil {
		return types.NewAsset{}, failure
	}
	fileName := file.FileID + ".bin"
	if file.FileName != nil {
		fileName = *file.FileName
	}
	return f.UploadAssetBytes(ctx, UploadBytesParams{
		UserID:      userID,
		ThreadID:    threadID,
		MessageHint: file.FileID,
		Bytes:       []byte("fake-chatgpt-file"),
		FileName:    fileName,
		MimeType:    file.MimeType,
	})
}

func errTooLarge(limit int64) error {
	return &tooLargeError{limit: limit}
}

type tooLargeError struct {
	limit int64
}

func (e *tooLargeError) Error() string {
	return "File is too large. Max size is " + strconv.FormatInt(e.limit, 10) + " bytes."
}

func (f *FakeStore) ensureBucket(bucket string) {
	if f.Buckets == nil {
		f.Buckets = make(map[string]map[string]ObjectMetadata)
	}
	if f.Buckets[bucket] == nil {
		f.Buckets[bucket] = make(map[string]ObjectMetadata)
	}
}

func (f *FakeStore) ensureByteBucket(bucket string) {
	if f.ObjectBytes == nil {
		f.ObjectBytes = make(map[string]map[string][]byte)
	}
	if f.ObjectBytes[bucket] == nil {
		f.ObjectBytes[bucket] = make(map[string][]byte)
	}
}

func failureKey(bucket string, key string) string {
	return bucket + "\x00" + key
}

func fmtObjectNotFound(bucket string, key string) error {
	return errors.Join(ErrObjectNotFound, errors.New("r2://"+bucket+"/"+key))
}
