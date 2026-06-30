package cli

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
	"agentbox/internal/agentbox/httpapi"
	"agentbox/internal/agentbox/profiles"
	"agentbox/internal/agentbox/service"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/version"
)

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

func TestCLIHelpOutput(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"--help"}, []string{"Usage: agentbox [options] <command>", "Commands:", "mcp-url"}},
		{[]string{"-h"}, []string{"Usage: agentbox [options] <command>", "profiles"}},
		{[]string{"profiles", "--help"}, []string{"Usage: agentbox profiles [options] [command]", "add <name>"}},
		{[]string{"profiles", "add", "--help"}, []string{"Usage: agentbox profiles add <name>", "--base-url <url>"}},
		{[]string{"doctor", "--help"}, []string{"Usage: agentbox doctor", "authenticated API access"}},
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
	if !strings.Contains(out.String(), "# CLI thread") || !strings.Contains(out.String(), "hello from cli") {
		t.Fatalf("get output = %s", out.String())
	}

	out.Reset()
	stderr.Reset()
	code = runner.Run([]string{"list", "--json"})
	if code != 0 {
		t.Fatalf("list failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), `"threads"`) || !strings.Contains(out.String(), "CLI thread") {
		t.Fatalf("list output = %s", out.String())
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

func TestCLIInitSavesProfile(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())

	var out bytes.Buffer
	var stderr bytes.Buffer
	runner := &Runner{
		Stdout: &out,
		Stderr: &stderr,
		Stdin:  bytes.NewReader(nil),
	}

	if code := runner.Run([]string{"init", "--profile-name", "prod", "--base-url", "https://agentbox.example.com", "--api-key", "local-secret", "--skip-doctor"}); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(out.String(), `Saved profile "prod"`) {
		t.Fatalf("init output = %s", out.String())
	}
	resolved, err := profiles.Resolve("prod")
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.BaseURL != "https://agentbox.example.com" || resolved.APIKey != "local-secret" {
		t.Fatalf("resolved profile = %#v", resolved)
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
	if !strings.Contains(output, server.URL+"/api/mcp?key=dev-key") {
		t.Fatalf("connect output missing mcp url: %s", output)
	}
	if !strings.Contains(output, "Apps -> Advanced settings") || !strings.Contains(output, "Select no auth") {
		t.Fatalf("connect output missing ChatGPT instructions: %s", output)
	}
}

func TestCLIDeployVercelRunsExpectedCommands(t *testing.T) {
	t.Setenv("AGENTBOX_CONFIG_DIR", t.TempDir())

	type invocation struct {
		name  string
		args  []string
		stdin string
	}
	var calls []invocation
	fake := func(name string, args []string, stdin string, _ map[string]string) (string, string, error) {
		calls = append(calls, invocation{name: name, args: slices.Clone(args), stdin: stdin})
		if len(args) >= 4 && args[0] == "--prod" && args[3] == "deploy/vercel/backend/vercel.json" {
			return "https://agentbox-go-test.vercel.app\n", "", nil
		}
		if len(args) >= 4 && args[0] == "--prod" && args[3] == "deploy/vercel/dashboard/vercel.json" {
			return "https://agentbox-test.vercel.app\n", "", nil
		}
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

	args := []string{
		"deploy", "vercel",
		"--database-url", "postgres://db",
		"--r2-account-id", "acc",
		"--r2-access-key-id", "keyid",
		"--r2-secret-access-key", "secret",
		"--r2-bucket", "bucket",
		"--admin-key", "adminkey",
		"--chatgpt-key", "chat-key",
		"--local-key", "local-key",
		"--profile-name", "prod",
	}
	if code := runner.Run(args); code != 0 {
		t.Fatalf("deploy vercel failed: code=%d stderr=%s stdout=%s", code, stderr.String(), out.String())
	}
	output := out.String()
	if !strings.Contains(output, "Backend deployed: https://agentbox-go-test.vercel.app") {
		t.Fatalf("deploy output = %s", output)
	}
	if !strings.Contains(output, `Saved local CLI profile "prod"`) {
		t.Fatalf("deploy output missing profile save: %s", output)
	}
	recorded := make([]string, 0, len(calls))
	for _, call := range calls {
		recorded = append(recorded, call.name+" "+strings.Join(call.args, " "))
	}
	joined := strings.Join(recorded, "\n")
	for _, want := range []string{
		"vercel link --yes --project agentbox-go",
		"vercel env add DATABASE_URL production",
		"vercel --prod --yes -A deploy/vercel/backend/vercel.json",
		"vercel link --yes --project agentbox",
		"vercel env add AGENTBOX_BACKEND_URL production",
		"vercel --prod --yes -A deploy/vercel/dashboard/vercel.json",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("recorded commands missing %q:\n%s", want, joined)
		}
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
	fake := &assets.FakeStore{PublicBaseURL: "https://assets.example.com"}
	svc := service.New(repo, fake)
	thread, err := svc.CreateThread(t.Context(), types.Actor{Name: "seed", KeyName: "seed"}, "Seed")
	if err != nil {
		t.Fatal(err)
	}
	textType := "text/plain"
	if _, err := repo.PostMessage(t.Context(), thread.ID, "seed", "seed asset", nil, &types.NewAsset{
		StorageKey: "agentbox/seed/message/seed.txt",
		FileName:   "seed.txt",
		MimeType:   &textType,
		SizeBytes:  int64(len("seed bytes")),
	}); err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(httpapi.NewServer(config.Config{}, svc))
}
