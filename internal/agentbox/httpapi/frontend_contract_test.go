package httpapi

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFrontendRequestLifecycleVisibilityAndPaginationContracts(t *testing.T) {
	root := repositoryRoot(t)
	assertSourceContains(t, root, "app/threads/inbox-view.tsx", "new AbortController()", "controller.abort()")
	assertSourceContains(t, root, "app/owner/content/owner-content-view.tsx", "new AbortController()", "controller.abort()", "page.next_cursor", "page.previous_cursor")
	assertSourceContains(t, root, "app/threads/[threadId]/thread-visibility-control.tsx", "const isPublic = visibility?.public ?? false", "const privateOnly = selectedTeamIDs.length === 0 && !isPublic", "The public read-only link remains live")
	assertSourceContains(t, root, "app/share/[token]/public-thread-view.tsx", "asset.preview_path", "Attachment unavailable")
	assertSourceContains(t, root, "app/owner/users/owner-users-view.tsx", "Load more users", "Load more teams", "Load more credentials", "Load more invitations", "Load more members")
	assertSourceContains(t, root, "app/api/public/threads/[token]/assets/[assetId]/preview/route.ts", "/preview")
}

func TestSupersededVisibilityMutationSurfacesAreAbsent(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{
		"internal/agentbox/service/service.go",
		"internal/agentbox/db/repository.go",
		"internal/agentbox/db/memory.go",
		"internal/agentbox/httpapi/server.go",
	} {
		contents := readSource(t, root, relative)
		for _, forbidden := range []string{"SetThreadVisibility(", "CreateThreadPublicLink(", "RevokeThreadPublicLink(", "GetThreadPublicLink("} {
			if strings.Contains(contents, forbidden) {
				t.Fatalf("%s still contains retired visibility mutation %q", relative, forbidden)
			}
		}
	}
	visibilityRoute := readSource(t, root, "app/api/threads/[threadId]/visibility/route.ts")
	if strings.Contains(visibilityRoute, "export async function PUT") {
		t.Fatal("visibility compatibility PUT remains exported")
	}
	if _, err := os.Stat(filepath.Join(root, "app/api/threads/[threadId]/public-link")); !os.IsNotExist(err) {
		t.Fatalf("retired public-link proxy still exists: %v", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
}

func assertSourceContains(t *testing.T, root string, relative string, snippets ...string) {
	t.Helper()
	contents := readSource(t, root, relative)
	for _, snippet := range snippets {
		if !strings.Contains(contents, snippet) {
			t.Fatalf("%s does not contain required contract %q", relative, snippet)
		}
	}
}

func readSource(t *testing.T, root string, relative string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return string(contents)
}
