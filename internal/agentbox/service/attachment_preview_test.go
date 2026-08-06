package service

import (
	"testing"

	"agentbox/internal/agentbox/types"
)

func TestSupportsDashboardInlinePreviewIncludesMarkdownFiles(t *testing.T) {
	markdownMIME := "text/markdown; charset=utf-8"
	plainMIME := "text/plain"
	imageMIME := "image/png"
	binaryMIME := "application/octet-stream"

	for _, test := range []struct {
		name  string
		asset types.Asset
		want  bool
	}{
		{name: "markdown mime", asset: types.Asset{FileName: "notes.txt", MimeType: &markdownMIME}, want: true},
		{name: "markdown extension overrides generic browser mime", asset: types.Asset{FileName: "handoff.md", MimeType: &plainMIME}, want: true},
		{name: "image", asset: types.Asset{FileName: "diagram.png", MimeType: &imageMIME}, want: true},
		{name: "binary", asset: types.Asset{FileName: "archive.bin", MimeType: &binaryMIME}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := supportsDashboardInlinePreview(test.asset); got != test.want {
				t.Fatalf("supportsDashboardInlinePreview(%#v)=%t want %t", test.asset, got, test.want)
			}
		})
	}
}
