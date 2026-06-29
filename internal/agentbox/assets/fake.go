package assets

import (
	"context"
	"net/url"
	"strconv"

	"agentbox/internal/agentbox/types"
)

type FakeStore struct {
	MaxFileSizeBytes int64
	PublicBaseURL    string
	Uploads          []types.NewAsset
}

func (f *FakeStore) UploadAssetBytes(_ context.Context, params UploadBytesParams) (types.NewAsset, error) {
	limit := f.MaxFileSizeBytes
	if limit == 0 {
		limit = 25 * 1024 * 1024
	}
	if int64(len(params.Bytes)) > limit {
		return types.NewAsset{}, errTooLarge(limit)
	}
	fileName := SanitizeFilename(params.FileName)
	storageKey := MakeStorageKey(params.ThreadID, defaultString(params.MessageHint, "message"), fileName)
	asset := types.NewAsset{
		StorageKey: storageKey,
		FileName:   fileName,
		MimeType:   InferMimeType(fileName, params.MimeType),
		SizeBytes:  int64(len(params.Bytes)),
		PublicURL:  PublicURLForKey(f.PublicBaseURL, storageKey),
	}
	f.Uploads = append(f.Uploads, asset)
	return asset, nil
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

func (f *FakeStore) UploadChatGPTFile(ctx context.Context, threadID string, input ChatGPTFileInput) (types.NewAsset, error) {
	file, err := NormalizeChatGPTFileInput(input)
	if err != nil {
		return types.NewAsset{}, err
	}
	fileName := file.FileID + ".bin"
	if file.FileName != nil {
		fileName = *file.FileName
	}
	return f.UploadAssetBytes(ctx, UploadBytesParams{
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
