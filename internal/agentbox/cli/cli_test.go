package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	authpkg "agentbox/internal/agentbox/auth"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/httpapi"
	"agentbox/internal/agentbox/profiles"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/version"
)

func setThreadVisibilityForTest(ctx context.Context, repository interface {
	ManageThreadVisibility(context.Context, string, string, types.ManageThreadVisibilityInput) (types.ManagedThreadVisibility, error)
}, userID string, threadID string, desiredTeamIDs []string) (types.ThreadVisibility, error) {
	current, err := repository.ManageThreadVisibility(ctx, userID, threadID, types.ManageThreadVisibilityInput{})
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	desired := map[string]bool{}
	for _, teamID := range desiredTeamIDs {
		desired[teamID] = true
	}
	currentIDs := map[string]bool{}
	input := types.ManageThreadVisibilityInput{}
	for _, team := range current.SharedTeams {
		currentIDs[team.ID] = true
		if !desired[team.ID] {
			input.RemoveTeams = append(input.RemoveTeams, team.ID)
		}
	}
	for _, teamID := range desiredTeamIDs {
		if !currentIDs[teamID] {
			input.AddTeams = append(input.AddTeams, teamID)
		}
	}
	state, err := repository.ManageThreadVisibility(ctx, userID, threadID, input)
	if err != nil {
		return types.ThreadVisibility{}, err
	}
	return types.ThreadVisibility{ThreadID: state.ThreadID, OwnerUserID: state.OwnerUserID, SharedTeams: state.SharedTeams}, nil
}

func TestCLIGlobalVersionFlags(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"-V"}, {"version"}} {
		var out bytes.Buffer
		var stderr bytes.Buffer
		runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil)}
		if code := runner.Run(args); code != 0 {
			t.Fatalf("%v failed: code=%d stderr=%s", args, code, stderr.String())
		}
		if got := strings.TrimSpace(out.String()); got != version.Version {
			t.Fatalf("%v output = %q, want %q", args, got, version.Version)
		}
	}
}

func dbHashForTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestCLIHelpOutput(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"--help"}, []string{"Usage: agentbox [options] <command>", "Commands:", "mcp-url", "owner"}},
		{[]string{"-h"}, []string{"Usage: agentbox [options] <command>", "profiles"}},
		{[]string{"profiles", "--help"}, []string{"Usage: agentbox profiles [options] [command]", "add <name>"}},
		{[]string{"profiles", "add", "--help"}, []string{"Usage: agentbox profiles add <name>", "--base-url <url>"}},
		{[]string{"doctor", "--help"}, []string{"Usage: agentbox doctor", "authenticated API access"}},
		{[]string{"owner", "--help"}, []string{"Usage: agentbox owner setup-token", "permanent deployment owner"}},
		{[]string{"deploy", "vercel", "--help"}, []string{"Usage: agentbox deploy vercel", "does not mutate Vercel"}},
		{[]string{"login", "--help"}, []string{"Usage: agentbox login", "user-owned credential"}},
		{[]string{"mcp-url", "--help"}, []string{"Usage: agentbox mcp-url", "user and actor diagnostics"}},
		{[]string{"connect", "--help"}, []string{"Usage: agentbox connect chatgpt", "user-owned ChatGPT credential"}},
		{[]string{"raycast-key", "--help"}, []string{"Usage: agentbox raycast-key", "Raycast"}},
		{[]string{"keys", "create", "--help"}, []string{"Usage: agentbox keys create <name>", "signed-in profile's user"}},
		{[]string{"keys", "list", "--help"}, []string{"Usage: agentbox keys list", "signed-in profile's user"}},
		{[]string{"keys", "revoke", "--help"}, []string{"Usage: agentbox keys revoke <name>", "signed-in profile's user"}},
		{[]string{"search", "--help"}, []string{"Usage: agentbox search <query>", "message counts"}},
		{[]string{"create", "--help"}, []string{"--message <body>", "first message"}},
		{[]string{"visibility", "--help"}, []string{"Usage: agentbox visibility <thread-id>", "atomically change", "--share-team"}},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		var stderr bytes.Buffer
		runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil)}
		if code := runner.Run(tc.args); code != 0 {
			t.Fatalf("%v failed: code=%d stderr=%s", tc.args, code, stderr.String())
		}
		for _, want := range tc.want {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("%v output missing %q:\n%s", tc.args, want, out.String())
			}
		}
		if stderr.Len() != 0 {
			t.Fatalf("%v wrote stderr: %s", tc.args, stderr.String())
		}
	}
}

func TestCLIRequiresEnvOrProfileWithActionableMessage(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	t.Setenv("AGENTBOX_BASE_URL", "")
	t.Setenv("AGENTBOX_URL", "")
	t.Setenv("AGENTBOX_API_KEY", "")
	t.Setenv("AGENTBOX_PROFILE", "")
	t.Setenv("AGENTBOX_PROFILES", "")

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil)}

	if code := runner.Run([]string{"list"}); code == 0 {
		t.Fatal("list without config unexpectedly succeeded")
	}

	got := stderr.String()
	if !strings.Contains(got, "Set AGENTBOX_BASE_URL and AGENTBOX_API_KEY or configure profiles in") {
		t.Fatalf("stderr missing env guidance: %s", got)
	}
	if !strings.Contains(got, "profiles.json") {
		t.Fatalf("stderr missing config path: %s", got)
	}
}

func TestCLIMCPURLPrintsFullKeyURL(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	server := newTestServer(t)
	defer server.Close()

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}
	if code := runner.Run([]string{"profiles", "add", "local", "--base-url", server.URL, "--api-key", "dev-key", "--activate"}); code != 0 {
		t.Fatalf("profiles add failed: stderr=%s", stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"mcp-url"}); code != 0 {
		t.Fatalf("mcp-url failed: code=%d stderr=%s", code, stderr.String())
	}
	want := server.URL + "/api/mcp?key=dev-key"
	if got := strings.TrimSpace(out.String()); got != want {
		t.Fatalf("mcp-url output = %q, want %q", got, want)
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"mcp-url", "--json"}); code != 0 {
		t.Fatalf("mcp-url --json failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), `"mcp_url": "`+want+`"`) || !strings.Contains(out.String(), `"profile": "local"`) {
		t.Fatalf("mcp-url json output = %s", out.String())
	}
	if !strings.Contains(out.String(), `"mcp_url_masked"`) || !strings.Contains(out.String(), `"auth"`) {
		t.Fatalf("mcp-url json missing diagnostics = %s", out.String())
	}
}

func TestCLIProfilesAddPersistsOnboardingIdentityMetadata(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	server := newTestServer(t)
	defer server.Close()

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}
	if code := runner.Run([]string{
		"profiles", "add", "local",
		"--base-url", server.URL,
		"--api-key", "dev-key",
		"--user-id", "usr_onboarding",
		"--key-name", "Local CLI",
		"--auth-type", "api_key",
		"--activate",
	}); code != 0 {
		t.Fatalf("profiles add failed: code=%d stderr=%s", code, stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"profiles", "show", "local", "--json"}); code != 0 {
		t.Fatalf("profiles show failed: code=%d stderr=%s", code, stderr.String())
	}
	profileJSON := out.String()
	if !strings.Contains(profileJSON, `"user_id": "usr_onboarding"`) || !strings.Contains(profileJSON, `"key_name": "Local CLI"`) || !strings.Contains(profileJSON, `"auth_type": "api_key"`) {
		t.Fatalf("profile metadata output=%s", profileJSON)
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"list"}); code != 0 {
		t.Fatalf("onboarding profile could not list: code=%d stderr=%s", code, stderr.String())
	}
}

func TestCLIProfilesAndThreadCommands(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	server := newTestServer(t)
	defer server.Close()

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}

	code := runner.Run([]string{"profiles", "add", "local", "--base-url", server.URL, "--api-key", "dev-key", "--activate", "--json"})
	if code != 0 {
		t.Fatalf("profiles add failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), `"saved_profile": "local"`) {
		t.Fatalf("profiles add output = %s", out.String())
	}

	out.Reset()
	stderr.Reset()
	code = runner.Run([]string{"create", "CLI thread"})
	if code != 0 {
		t.Fatalf("create failed: code=%d stderr=%s", code, stderr.String())
	}
	createdFields := strings.Split(strings.TrimSpace(out.String()), "\t")
	if len(createdFields) != 2 || !strings.HasPrefix(createdFields[0], "thr_") || createdFields[1] != "CLI thread" {
		t.Fatalf("create output = %q", out.String())
	}
	threadID := createdFields[0]

	out.Reset()
	stderr.Reset()
	code = runner.Run([]string{"create", "Initial CLI thread", "--message", "first message from cli", "--plain", "--json"})
	if code != 0 {
		t.Fatalf("create with message failed: code=%d stderr=%s", code, stderr.String())
	}
	var createdWithMessage struct {
		Thread struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"thread"`
		Message struct {
			ThreadID        string  `json:"thread_id"`
			Body            string  `json:"body"`
			BodyContentType *string `json:"body_content_type"`
		} `json:"message"`
	}
	if err := json.Unmarshal(out.Bytes(), &createdWithMessage); err != nil {
		t.Fatal(err)
	}
	if createdWithMessage.Thread.Title != "Initial CLI thread" || createdWithMessage.Message.ThreadID != createdWithMessage.Thread.ID || createdWithMessage.Message.Body != "first message from cli" {
		t.Fatalf("created with message = %#v", createdWithMessage)
	}
	if createdWithMessage.Message.BodyContentType == nil || *createdWithMessage.Message.BodyContentType != "text/plain" {
		t.Fatalf("created message content type = %#v", createdWithMessage.Message.BodyContentType)
	}

	out.Reset()
	stderr.Reset()
	code = runner.Run([]string{"post", threadID, "hello from cli"})
	if code != 0 {
		t.Fatalf("post failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "msg_") {
		t.Fatalf("post output = %q", out.String())
	}

	out.Reset()
	stderr.Reset()
	code = runner.Run([]string{"get", threadID})
	if code != 0 {
		t.Fatalf("get failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "# CLI thread") || !strings.Contains(out.String(), "hello from cli") || !strings.Contains(out.String(), "Seed · dev") || !strings.Contains(out.String(), "visibility: Private") {
		t.Fatalf("get output = %s", out.String())
	}

	out.Reset()
	stderr.Reset()
	code = runner.Run([]string{"list", "--json"})
	if code != 0 {
		t.Fatalf("list failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), `"threads"`) || !strings.Contains(out.String(), "CLI thread") || !strings.Contains(out.String(), `"private": true`) || !strings.Contains(out.String(), `"created_by_user_display_name": "Seed"`) || !strings.Contains(out.String(), `"created_by_actor_name": "dev"`) {
		t.Fatalf("list output = %s", out.String())
	}

	out.Reset()
	stderr.Reset()
	code = runner.Run([]string{"list"})
	if code != 0 {
		t.Fatalf("plain list failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "Private · Created by Seed · dev") {
		t.Fatalf("plain list metadata = %s", out.String())
	}

	out.Reset()
	stderr.Reset()
	code = runner.Run([]string{"search", "first message", "--json"})
	if code != 0 {
		t.Fatalf("search failed: code=%d stderr=%s", code, stderr.String())
	}
	var searchPayload struct {
		Threads []struct {
			Title                    string                        `json:"title"`
			CreatedByUserDisplayName *string                       `json:"created_by_user_display_name"`
			CreatedByActorName       *string                       `json:"created_by_actor_name"`
			MessageCount             int                           `json:"message_count"`
			LastMessagePreview       string                        `json:"last_message_preview"`
			MatchedSnippets          []string                      `json:"matched_snippets"`
			VisibilitySummary        types.ThreadVisibilitySummary `json:"visibility_summary"`
		} `json:"threads"`
	}
	if err := json.Unmarshal(out.Bytes(), &searchPayload); err != nil {
		t.Fatal(err)
	}
	if len(searchPayload.Threads) == 0 || searchPayload.Threads[0].Title != "Initial CLI thread" || searchPayload.Threads[0].MessageCount != 1 || searchPayload.Threads[0].LastMessagePreview == "" || !searchPayload.Threads[0].VisibilitySummary.Private || searchPayload.Threads[0].CreatedByUserDisplayName == nil || *searchPayload.Threads[0].CreatedByUserDisplayName != "Seed" || searchPayload.Threads[0].CreatedByActorName == nil || *searchPayload.Threads[0].CreatedByActorName != "dev" {
		t.Fatalf("search payload = %#v", searchPayload)
	}

	out.Reset()
	stderr.Reset()
	code = runner.Run([]string{"search", "first message", "--created-by", "dev"})
	if code != 0 {
		t.Fatalf("search text failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "Initial CLI thread") || !strings.Contains(out.String(), "first message from cli") || !strings.Contains(out.String(), "Private · Created by Seed · dev") {
		t.Fatalf("search text output = %s", out.String())
	}
}

func TestAttributionLabelUsesSnapshotsAndLegacyFallback(t *testing.T) {
	user := "Ashray"
	actor := "ChatGPT"
	if got := attributionLabel(&user, &actor, "legacy"); got != "Ashray · ChatGPT" {
		t.Fatalf("snapshot attribution = %q", got)
	}
	if got := attributionLabel(nil, nil, " Legacy agent "); got != " Legacy agent " {
		t.Fatalf("legacy attribution = %q", got)
	}
	if got := attributionLabel(nil, nil, ""); got != "Agentbox user" {
		t.Fatalf("empty attribution = %q", got)
	}
}

func TestCLIVisibilityReadsAndMutatesAtomically(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	repo := &db.MemoryRepository{}
	authContext := types.AuthContext{
		UserID:          "usr_visibility_cli",
		UserDisplayName: "Visibility CLI",
		SubjectType:     types.AuthSubjectUserSession,
		ActorName:       "Web dashboard",
	}
	repo.Users = append(repo.Users, types.User{ID: authContext.UserID, Email: "visibility-cli@example.com", DisplayName: authContext.UserDisplayName})
	svc := service.New(repo, &assets.FakeStore{})
	if _, err := svc.CreateAPIKeyWithScopes(t.Context(), authContext, "dev", []string{"threads:read", "threads:write", "assets:read", "assets:write", "mcp:use"}); err != nil {
		t.Fatal(err)
	}
	repo.APIKeys[0].Key = "visibility-dev-key"
	repo.APIKeys[0].TokenHash = dbHashForTest("visibility-dev-key")
	svc = service.New(repo, &assets.FakeStore{})

	oldTeam, err := repo.CreateTeam(t.Context(), "cli-old", "CLI Old")
	if err != nil {
		t.Fatal(err)
	}
	newTeam, err := repo.CreateTeam(t.Context(), "cli-new", "CLI New")
	if err != nil {
		t.Fatal(err)
	}
	unavailableTeam, err := repo.CreateTeam(t.Context(), "cli-unavailable", "CLI Unavailable")
	if err != nil {
		t.Fatal(err)
	}
	for _, teamID := range []string{oldTeam.ID, newTeam.ID} {
		if _, err := repo.AddTeamMember(t.Context(), teamID, authContext.UserID); err != nil {
			t.Fatal(err)
		}
	}
	thread, err := svc.CreateThread(t.Context(), authContext, "CLI visibility thread")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setThreadVisibilityForTest(t.Context(), repo, authContext.UserID, thread.ID, []string{oldTeam.ID}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(httpapi.NewServer(config.Config{AppPublicURL: "https://agentbox.example"}, svc))
	defer server.Close()
	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}
	if code := runner.Run([]string{"profiles", "add", "visibility", "--base-url", server.URL, "--api-key", "visibility-dev-key", "--activate"}); code != 0 {
		t.Fatalf("profiles add failed: code=%d stderr=%s", code, stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"visibility", thread.ID}); code != 0 {
		t.Fatalf("visibility read failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), "CLI Old (cli-old)") || !strings.Contains(out.String(), "CLI New (cli-new)") || !strings.Contains(out.String(), "Public: Off") {
		t.Fatalf("visibility text output=%s", out.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{
		"visibility", thread.ID,
		"--share-team", "cli-new",
		"--share-team", newTeam.ID,
		"--unshare-team", oldTeam.ID,
		"--publish",
		"--json",
	}); code != 0 {
		t.Fatalf("visibility mutation failed: code=%d stderr=%s", code, stderr.String())
	}
	var mutated struct {
		Visibility types.ManagedThreadVisibility `json:"visibility"`
	}
	if err := json.Unmarshal(out.Bytes(), &mutated); err != nil {
		t.Fatal(err)
	}
	if len(mutated.Visibility.SharedTeams) != 1 || mutated.Visibility.SharedTeams[0].ID != newTeam.ID || !mutated.Visibility.Public || !strings.HasPrefix(mutated.Visibility.PublicURL, "https://agentbox.example/share/agpub_") {
		t.Fatalf("visibility mutation=%#v", mutated.Visibility)
	}
	firstPublicURL := mutated.Visibility.PublicURL

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"visibility", thread.ID, "--json"}); code != 0 {
		t.Fatalf("visibility redisplay failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), firstPublicURL) || !strings.Contains(out.String(), `"slug": "cli-new"`) {
		t.Fatalf("visibility redisplay output=%s", out.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"visibility", thread.ID, "--regenerate-public-link", "--json"}); code != 0 {
		t.Fatalf("visibility regenerate failed: code=%d stderr=%s", code, stderr.String())
	}
	var regenerated struct {
		Visibility types.ManagedThreadVisibility `json:"visibility"`
	}
	if err := json.Unmarshal(out.Bytes(), &regenerated); err != nil {
		t.Fatal(err)
	}
	if regenerated.Visibility.PublicURL == "" || regenerated.Visibility.PublicURL == firstPublicURL {
		t.Fatalf("regenerated visibility=%#v", regenerated.Visibility)
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"visibility", thread.ID, "--publish", "--unpublish"}); code == 0 {
		t.Fatal("conflicting public flags unexpectedly succeeded")
	}
	if !strings.Contains(stderr.String(), "Use only one of --publish or --unpublish") {
		t.Fatalf("conflicting public flags stderr=%s", stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"visibility", thread.ID, "--share-team", unavailableTeam.Slug}); code == 0 {
		t.Fatal("unavailable team share unexpectedly succeeded")
	}
	if !strings.Contains(stderr.String(), "TEAM_NOT_AVAILABLE") {
		t.Fatalf("unavailable team stderr=%s", stderr.String())
	}
	state, err := repo.ManageThreadVisibility(t.Context(), authContext.UserID, thread.ID, types.ManageThreadVisibilityInput{})
	if err != nil || len(state.SharedTeams) != 1 || state.SharedTeams[0].ID != newTeam.ID || !state.Public {
		t.Fatalf("failed mutation changed state=%#v err=%v", state, err)
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"visibility", thread.ID, "--unpublish", "--json"}); code != 0 {
		t.Fatalf("visibility unpublish failed: code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(out.String(), `"public": true`) || !strings.Contains(out.String(), `"public": false`) {
		t.Fatalf("unpublish output=%s", out.String())
	}
}

func TestCLIPostMultipartAsset(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	server := newTestServer(t)
	defer server.Close()
	assetPath := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(assetPath, []byte("asset body"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}
	if code := runner.Run([]string{"profiles", "add", "local", "--base-url", server.URL, "--api-key", "dev-key", "--activate"}); code != 0 {
		t.Fatalf("profiles add failed: stderr=%s", stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"create", "Asset thread"}); code != 0 {
		t.Fatalf("create failed: stderr=%s", stderr.String())
	}
	threadID := strings.Split(strings.TrimSpace(out.String()), "\t")[0]

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"post", threadID, "with asset", "--asset", assetPath, "--json"}); code != 0 {
		t.Fatalf("post asset failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), `"file_name": "note.txt"`) || !strings.Contains(out.String(), `"size_bytes": 10`) {
		t.Fatalf("post asset output = %s", out.String())
	}
}

func TestCLIPostReadsPipedStdin(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	server := newTestServer(t)
	defer server.Close()

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader([]byte("hello from stdin")), HTTPClient: server.Client()}
	if code := runner.Run([]string{"profiles", "add", "local", "--base-url", server.URL, "--api-key", "dev-key", "--activate"}); code != 0 {
		t.Fatalf("profiles add failed: stderr=%s", stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"create", "stdin thread"}); code != 0 {
		t.Fatalf("create failed: stderr=%s", stderr.String())
	}
	threadID := strings.Split(strings.TrimSpace(out.String()), "\t")[0]

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"post", threadID}); code != 0 {
		t.Fatalf("post failed: code=%d stderr=%s", code, stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"get", threadID, "--json"}); code != 0 {
		t.Fatalf("get failed: code=%d stderr=%s", code, stderr.String())
	}
	var payload struct {
		Thread struct {
			Messages []struct {
				Body            string  `json:"body"`
				BodyContentType *string `json:"body_content_type"`
			} `json:"messages"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload.Thread.Messages[len(payload.Thread.Messages)-1].Body; got != "hello from stdin" {
		t.Fatalf("stdin body = %q", got)
	}
	if got := payload.Thread.Messages[len(payload.Thread.Messages)-1].BodyContentType; got == nil || *got != "text/plain" {
		t.Fatalf("stdin content type = %#v", got)
	}
}

func TestCLIPostAutoDetectsMarkdownFile(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	server := newTestServer(t)
	defer server.Close()
	messagePath := filepath.Join(t.TempDir(), "handoff.md")
	if err := os.WriteFile(messagePath, []byte("# Handoff\n\n| Task | Status |\n| --- | --- |\n| Render markdown | Done |\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}
	if code := runner.Run([]string{"profiles", "add", "local", "--base-url", server.URL, "--api-key", "dev-key", "--activate"}); code != 0 {
		t.Fatalf("profiles add failed: stderr=%s", stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"create", "markdown thread"}); code != 0 {
		t.Fatalf("create failed: stderr=%s", stderr.String())
	}
	threadID := strings.Split(strings.TrimSpace(out.String()), "\t")[0]

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"post", threadID, "--file", messagePath, "--json"}); code != 0 {
		t.Fatalf("post markdown failed: code=%d stderr=%s", code, stderr.String())
	}
	var payload struct {
		Message struct {
			BodyContentType *string `json:"body_content_type"`
		} `json:"message"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Message.BodyContentType == nil || *payload.Message.BodyContentType != "text/markdown" {
		t.Fatalf("markdown content type = %#v", payload.Message.BodyContentType)
	}
}

func TestCLIDoctorChecksSignedDownloadURL(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	server := newTestServer(t)
	defer server.Close()

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}
	if code := runner.Run([]string{"profiles", "add", "local", "--base-url", server.URL, "--api-key", "dev-key", "--activate"}); code != 0 {
		t.Fatalf("profiles add failed: stderr=%s", stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"doctor"}); code != 0 {
		t.Fatalf("doctor failed: code=%d stderr=%s stdout=%s", code, stderr.String(), out.String())
	}
	if !strings.Contains(out.String(), "signed download URL") || !strings.Contains(out.String(), "seed.txt") {
		t.Fatalf("doctor output = %s", out.String())
	}
}

func TestCLIConnectChatGPTPrintsMCPInstructions(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	server := newTestServer(t)
	defer server.Close()

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}
	if code := runner.Run([]string{"profiles", "add", "local", "--base-url", server.URL, "--api-key", "dev-key", "--activate"}); code != 0 {
		t.Fatalf("profiles add failed: stderr=%s", stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"connect", "chatgpt"}); code != 0 {
		t.Fatalf("connect chatgpt failed: code=%d stderr=%s", code, stderr.String())
	}
	output := out.String()
	if !strings.Contains(output, "Created ChatGPT API key \"chatgpt\"") || !strings.Contains(output, server.URL+"/api/mcp?key=") || strings.Contains(output, server.URL+"/api/mcp?key=dev-key") {
		t.Fatalf("connect output missing mcp url: %s", output)
	}
	if !strings.Contains(output, "Apps -> Advanced settings") || !strings.Contains(output, "Select no auth") {
		t.Fatalf("connect output missing ChatGPT instructions: %s", output)
	}
}

func TestCLIRaycastKeyPrintsPreferences(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	server := newTestServer(t)
	defer server.Close()

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}
	if code := runner.Run([]string{"profiles", "add", "local", "--base-url", server.URL, "--api-key", "dev-key", "--activate"}); code != 0 {
		t.Fatalf("profiles add failed: stderr=%s", stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"raycast-key"}); code != 0 {
		t.Fatalf("raycast-key failed: code=%d stderr=%s", code, stderr.String())
	}
	output := out.String()
	if !strings.Contains(output, `Created Raycast API key "raycast"`) || !strings.Contains(output, "Agentbox URL: "+server.URL) || !strings.Contains(output, "Agentbox API Key: ") {
		t.Fatalf("raycast-key output = %s", output)
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"keys", "create", "raycast", "--json"}); code != 0 {
		t.Fatalf("keys create raycast failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), `"raycast_base_url": "`+server.URL+`"`) || !strings.Contains(out.String(), `"raycast_api_key": "`) {
		t.Fatalf("keys create raycast json = %s", out.String())
	}
}

func TestCLIDeployVercelPrintsGuideWithoutMutating(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())

	called := false
	fake := func(name string, args []string, stdin string, _ map[string]string) (string, string, error) {
		called = true
		return "", "", nil
	}

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{
		Stdout:      &out,
		Stderr:      &stderr,
		Stdin:       bytes.NewReader(nil),
		RunExternal: fake,
	}

	args := []string{"deploy", "vercel"}
	if code := runner.Run(args); code != 0 {
		t.Fatalf("deploy vercel failed: code=%d stderr=%s stdout=%s", code, stderr.String(), out.String())
	}
	output := out.String()
	if !strings.Contains(output, "Vercel deployment guide:") || !strings.Contains(output, "vercel env add AGENTBOX_APP_PUBLIC_URL production") || !strings.Contains(output, "agentbox owner setup-token --base-url") {
		t.Fatalf("deploy output = %s", output)
	}
	if called {
		t.Fatal("deploy vercel should not run external commands")
	}
}

func TestCLIAdminKeyManagementIsDisabled(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	server := newTestServer(t)
	defer server.Close()

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}

	if code := runner.Run([]string{"keys", "create", "builder", "--base-url", server.URL, "--admin-key", "adm"}); code == 0 {
		t.Fatalf("legacy admin key creation unexpectedly succeeded: stdout=%s", out.String())
	}
	if !strings.Contains(stderr.String(), "--admin-key and --base-url are no longer supported") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestCLIOwnerSetupTokenPrintsBrowserLinkWithoutDeploymentSecret(t *testing.T) {
	repo := &db.MemoryRepository{}
	server := httptest.NewServer(httpapi.NewServer(config.Config{AdminKey: "deployment-secret"}, service.New(repo, &assets.FakeStore{})))
	t.Cleanup(server.Close)

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}
	if code := runner.Run([]string{"owner", "setup-token", "--base-url", server.URL, "--admin-key", "deployment-secret", "--expires", "15m"}); code != 0 {
		t.Fatalf("owner setup-token failed: code=%d stderr=%s", code, stderr.String())
	}
	output := out.String()
	if !strings.Contains(output, "Issued owner bootstrap token.") || !strings.Contains(output, server.URL+"/owner/setup?token=agos_") {
		t.Fatalf("owner setup-token output=%s", output)
	}
	if strings.Contains(output, "deployment-secret") {
		t.Fatalf("deployment secret leaked in CLI output: %s", output)
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"owner", "setup-token", "--base-url", server.URL, "--admin-key", "deployment-secret", "--expires", "25h"}); code == 0 {
		t.Fatalf("oversized setup token expiry unexpectedly succeeded: %s", out.String())
	}
	if !strings.Contains(stderr.String(), "no more than 24h") {
		t.Fatalf("oversized expiry stderr=%s", stderr.String())
	}
}

func TestCLIKeysListAndRevokeUseUserProfile(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	server := newTestServer(t)
	defer server.Close()
	if _, err := profiles.SaveProfile(profiles.Profile{
		Name:     "user",
		BaseURL:  server.URL,
		APIKey:   "dev-key",
		KeyName:  "dev",
		AuthType: "api_key",
	}, true); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}

	if code := runner.Run([]string{"--profile", "user", "keys", "create", "user-managed"}); code != 0 {
		t.Fatalf("profile keys create failed: code=%d stderr=%s", code, stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"--profile", "user", "keys", "list", "--json"}); code != 0 {
		t.Fatalf("profile keys list failed: code=%d stderr=%s", code, stderr.String())
	}
	var listed struct {
		Keys []remoteAPIKey `json:"keys"`
	}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatalf("list output is not JSON: %v output=%s", err, out.String())
	}
	found := false
	for _, key := range listed.Keys {
		if key.Name == "user-managed" {
			found = true
			if key.UserID != "usr_seed" {
				t.Fatalf("user-managed key user_id=%q", key.UserID)
			}
		}
	}
	if !found {
		t.Fatalf("user-managed key missing from user list: %#v", listed.Keys)
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"--profile", "user", "keys", "revoke", "user-managed", "--json"}); code != 0 {
		t.Fatalf("profile keys revoke failed: code=%d stderr=%s", code, stderr.String())
	}
	var revoked struct {
		Revoked string `json:"revoked"`
	}
	if err := json.Unmarshal(out.Bytes(), &revoked); err != nil {
		t.Fatalf("revoke output is not JSON: %v output=%s", err, out.String())
	}
	if revoked.Revoked != "user-managed" {
		t.Fatalf("revoked payload = %#v", revoked)
	}
}

func TestCLILoginSavesUserProfile(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())
	passwordHash, err := authpkg.HashPassword("secret-password")
	if err != nil {
		t.Fatal(err)
	}
	user := types.User{
		ID:           "usr_acme",
		Email:        "admin@example.com",
		DisplayName:  "Acme Admin",
		PasswordHash: &passwordHash,
	}
	repo := &db.MemoryRepository{
		Users: []types.User{user},
	}
	svc := service.New(repo, &assets.FakeStore{})
	apiServer := httpapi.NewServer(config.Config{SessionCookieName: config.DefaultSessionCookieName}, svc)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/login/cli" {
			state := req.URL.Query().Get("state")
			redirectURI := req.URL.Query().Get("redirect_uri")
			result, err := svc.AuthorizeCLILogin(req.Context(), types.AuthContext{
				UserID:      user.ID,
				SubjectType: types.AuthSubjectUserSession,
				ActorID:     "sess_browser",
				ActorName:   "Web dashboard",
				SessionID:   "sess_browser",
			}, state, redirectURI)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			target, err := url.Parse(result.RedirectURI)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			query := target.Query()
			query.Set("code", result.Code)
			query.Set("state", state)
			target.RawQuery = query.Encode()
			http.Redirect(w, req, target.String(), http.StatusFound)
			return
		}
		apiServer.ServeHTTP(w, req)
	}))
	defer server.Close()

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{Stdout: &out, Stderr: &stderr, Stdin: bytes.NewReader(nil), HTTPClient: server.Client()}
	runner.RunExternal = func(name string, args []string, stdin string, env map[string]string) (string, string, error) {
		if len(args) == 0 {
			t.Fatalf("browser command %s missing URL", name)
		}
		res, err := server.Client().Get(args[len(args)-1])
		if err != nil {
			return "", "", err
		}
		_ = res.Body.Close()
		return "", "", nil
	}
	if code := runner.Run([]string{"login", "--base-url", server.URL, "--profile-name", "acme-prod", "--key-name", "cli-test"}); code != 0 {
		t.Fatalf("login failed: code=%d stderr=%s stdout=%s", code, stderr.String(), out.String())
	}
	resolved, err := profiles.Resolve("acme-prod")
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.BaseURL != server.URL || resolved.APIKey == "" || resolved.UserID != user.ID || resolved.KeyName != "cli-test" || resolved.AuthType != "api_key" {
		t.Fatalf("resolved profile = %#v", resolved)
	}

	out.Reset()
	stderr.Reset()
	if code := runner.Run([]string{"--profile", "acme-prod", "list", "--json"}); code != 0 {
		t.Fatalf("list after login failed: code=%d stderr=%s stdout=%s", code, stderr.String(), out.String())
	}
}

func TestShouldReadStdinForPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	if !shouldReadStdin(reader) {
		t.Fatal("expected pipe stdin to be readable")
	}
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	repo := &db.MemoryRepository{}
	authContext := types.AuthContext{UserID: "usr_seed", SubjectType: types.AuthSubjectUserSession, ActorName: "seed"}
	repo.Users = append(repo.Users, types.User{ID: authContext.UserID, Email: "seed@example.com", DisplayName: "Seed"})
	svc := service.New(repo, &assets.FakeStore{})
	if _, err := svc.CreateAPIKeyWithScopes(t.Context(), authContext, "dev", []string{"threads:read", "threads:write", "assets:read", "assets:write", "mcp:use", "keys:read", "keys:write"}); err != nil {
		t.Fatal(err)
	}
	repo.APIKeys[0].Key = "dev-key"
	repo.APIKeys[0].TokenHash = dbHashForTest("dev-key")
	fake := &assets.FakeStore{}
	svc = service.New(repo, fake)
	thread, err := svc.CreateThread(t.Context(), authContext, "Seed")
	if err != nil {
		t.Fatal(err)
	}
	textType := "text/plain"
	if _, err := repo.PostMessage(t.Context(), authContext.UserID, thread.ID, authContext, "seed asset", nil, []types.NewAsset{{
		StorageKey: "agentbox/usr_seed/seed/message/seed.txt",
		FileName:   "seed.txt",
		MimeType:   &textType,
		SizeBytes:  int64(len("seed bytes")),
	}}); err != nil {
		t.Fatal(err)
	}
	fake.PutAssetObject("agentbox/usr_seed/seed/message/seed.txt", int64(len("seed bytes")), &textType)
	return httptest.NewServer(httpapi.NewServer(config.Config{AdminKey: "adm"}, svc))
}
