package httpapi

import (
	"testing"

	"agentbox/internal/agentbox/types"
)

func TestViewerAssetPathsExposeMarkdownPreviewWithoutEagerObjectURLs(t *testing.T) {
	markdownMIME := "text/plain"
	binaryMIME := "application/octet-stream"
	thread := &types.ThreadWithMessages{
		Messages: []types.Message{{
			ID: "msg_preview",
			Assets: []types.Asset{
				{ID: "ast_markdown", FileName: "handoff.md", MimeType: &markdownMIME},
				{ID: "ast_binary", FileName: "archive.bin", MimeType: &binaryMIME},
			},
		}},
	}

	view := withViewerAssetPaths(thread)
	if len(view.Messages) != 1 || len(view.Messages[0].Assets) != 2 {
		t.Fatalf("viewer assets=%#v", view.Messages)
	}
	markdown := view.Messages[0].Assets[0]
	if markdown.PreviewPath != "/api/assets/ast_markdown/preview-url" || markdown.DownloadPath != "/api/assets/ast_markdown/download-url" {
		t.Fatalf("markdown viewer asset=%#v", markdown)
	}
	if markdown.DownloadURL != nil {
		t.Fatalf("markdown viewer eagerly exposed object URL=%q", *markdown.DownloadURL)
	}
	if binary := view.Messages[0].Assets[1]; binary.PreviewPath != "" || binary.DownloadPath == "" {
		t.Fatalf("binary viewer asset=%#v", binary)
	}
}
