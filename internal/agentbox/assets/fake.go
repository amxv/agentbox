package assets

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentbox/internal/agentbox/backup"
	"agentbox/internal/agentbox/types"
)

type FakeStore struct {
	MaxFileSizeBytes int64
	Uploads          []types.NewAsset
	Buckets          map[string]map[string]backup.ObjectMetadata
	CopyCalls        []backup.CopyObjectRequest
	DeleteCalls      []string
	HeadFailures     map[string]error
	ListFailures     map[string]error
	CopyFailures     map[string]error
	DeleteFailures   map[string]error
	mutex            sync.Mutex
}

func (f *FakeStore) UploadAssetBytes(_ context.Context, params UploadBytesParams) (types.NewAsset, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	limit := f.MaxFileSizeBytes
	if limit == 0 {
		limit = 25 * 1024 * 1024
	}
	if int64(len(params.Bytes)) > limit {
		return types.NewAsset{}, errTooLarge(limit)
	}
	fileName := SanitizeFilename(params.FileName)
	storageKey := MakeStorageKey(params.UserID, params.ThreadID, defaultString(params.MessageHint, "message"), fileName)
	asset := types.NewAsset{
		StorageKey: storageKey,
		FileName:   fileName,
		MimeType:   InferMimeType(fileName, params.MimeType),
		SizeBytes:  int64(len(params.Bytes)),
	}
	f.Uploads = append(f.Uploads, asset)
	return asset, nil
}

func (f *FakeStore) PutObject(bucket string, key string, sizeBytes int64, etag string) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.ensureBucket(bucket)
	now := time.Now().UTC()
	f.Buckets[bucket][key] = backup.ObjectMetadata{
		Bucket:       bucket,
		Key:          key,
		SizeBytes:    sizeBytes,
		ETag:         normalizeETag(etag),
		LastModified: &now,
	}
}

func (f *FakeStore) HeadObject(_ context.Context, bucket string, key string) (backup.ObjectMetadata, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if err := f.HeadFailures[failureKey(bucket, key)]; err != nil {
		return backup.ObjectMetadata{}, err
	}
	objects := f.Buckets[bucket]
	object, ok := objects[key]
	if !ok {
		return backup.ObjectMetadata{}, fmtObjectNotFound(bucket, key)
	}
	return object, nil
}

func (f *FakeStore) ListObjects(_ context.Context, bucket string, prefix string) ([]backup.ObjectMetadata, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if err := f.ListFailures[bucket]; err != nil {
		return nil, err
	}
	objects := []backup.ObjectMetadata{}
	for key, object := range f.Buckets[bucket] {
		if strings.HasPrefix(key, prefix) {
			objects = append(objects, object)
		}
	}
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].Key < objects[j].Key
	})
	return objects, nil
}

func (f *FakeStore) CopyObject(_ context.Context, request backup.CopyObjectRequest) (backup.ObjectMetadata, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if err := f.CopyFailures[failureKey(request.DestinationBucket, request.DestinationKey)]; err != nil {
		return backup.ObjectMetadata{}, err
	}
	source, ok := f.Buckets[request.SourceBucket][request.SourceKey]
	if !ok {
		return backup.ObjectMetadata{}, fmtObjectNotFound(request.SourceBucket, request.SourceKey)
	}
	if request.ExpectedETag != "" && normalizeETag(request.ExpectedETag) != source.ETag {
		return backup.ObjectMetadata{}, errors.New("source ETag changed before copy")
	}
	f.ensureBucket(request.DestinationBucket)
	now := time.Now().UTC()
	copied := source
	copied.Bucket = request.DestinationBucket
	copied.Key = request.DestinationKey
	copied.LastModified = &now
	f.Buckets[request.DestinationBucket][request.DestinationKey] = copied
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
	fileName := SanitizeFilename(params.FileName)
	storageKey := MakeStorageKey(params.UserID, params.ThreadID, defaultString(params.UploadID, "upload"), fileName)
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
		UploadURL:  "https://r2-upload.test/" + storageKey,
		ExpiresIn:  900,
		RequiredHeaders: map[string]string{
			"content-type": contentType,
		},
	}, nil
}

func (f *FakeStore) CreateSignedAssetDownloadURL(_ context.Context, params SignedURLParams) (string, error) {
	u := url.URL{Scheme: "https", Host: "r2.test", Path: "/" + params.StorageKey}
	q := u.Query()
	expires := params.ExpiresInSeconds
	if expires == 0 {
		expires = 300
	}
	q.Set("X-Amz-Expires", strconv.Itoa(expires))
	q.Set("response-content-disposition", `attachment; filename="`+params.FileName+`"`)
	if params.MimeType != nil {
		q.Set("response-content-type", *params.MimeType)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (f *FakeStore) DeleteAssetObject(_ context.Context, storageKey string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	storageKey = strings.TrimSpace(storageKey)
	if storageKey == "" {
		return errors.New("asset storage key is required")
	}
	f.DeleteCalls = append(f.DeleteCalls, storageKey)
	if err := f.DeleteFailures[storageKey]; err != nil {
		return err
	}
	for bucket, objects := range f.Buckets {
		delete(objects, storageKey)
		f.Buckets[bucket] = objects
	}
	return nil
}

func (f *FakeStore) UploadChatGPTFile(ctx context.Context, userID string, threadID string, input ChatGPTFileInput) (types.NewAsset, error) {
	file, err := NormalizeChatGPTFileInput(input)
	if err != nil {
		return types.NewAsset{}, err
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
		f.Buckets = make(map[string]map[string]backup.ObjectMetadata)
	}
	if f.Buckets[bucket] == nil {
		f.Buckets[bucket] = make(map[string]backup.ObjectMetadata)
	}
}

func failureKey(bucket string, key string) string {
	return bucket + "\x00" + key
}

func fmtObjectNotFound(bucket string, key string) error {
	return errors.Join(backup.ErrObjectNotFound, errors.New("r2://"+bucket+"/"+key))
}
