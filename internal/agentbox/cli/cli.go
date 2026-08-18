package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/profiles"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/version"
)

type Runner struct {
	Stdout      io.Writer
	Stderr      io.Writer
	Stdin       io.Reader
	HTTPClient  *http.Client
	RunExternal ExternalCommandFunc
}

type RuntimeConfig struct {
	ProfileName string
	BaseURL     string
	APIKey      string
	Source      string
	Profile     profiles.Profile
}

const defaultGetBodyBudget = 5000

type asset struct {
	ID          string  `json:"id"`
	FileName    string  `json:"file_name"`
	Filename    string  `json:"filename"`
	MimeType    *string `json:"mime_type"`
	SizeBytes   int64   `json:"size_bytes"`
	PublicURL   *string `json:"public_url"`
	DownloadURL *string `json:"download_url"`
	StorageKey  string  `json:"storage_key"`
}

type message struct {
	ID                       string  `json:"id"`
	ThreadID                 string  `json:"thread_id"`
	Author                   string  `json:"author"`
	Body                     string  `json:"body"`
	BodyContentType          *string `json:"body_content_type"`
	CreatedAt                string  `json:"created_at"`
	Assets                   []asset `json:"assets"`
	CreatedByUserDisplayName *string `json:"created_by_user_display_name"`
	CreatedByActorName       *string `json:"created_by_actor_name"`
}

type thread struct {
	ID                       string                        `json:"id"`
	Title                    string                        `json:"title"`
	CreatedAt                string                        `json:"created_at"`
	UpdatedAt                string                        `json:"updated_at"`
	CreatedBy                string                        `json:"created_by"`
	CreatedByUserDisplayName *string                       `json:"created_by_user_display_name"`
	CreatedByActorName       *string                       `json:"created_by_actor_name"`
	VisibilitySummary        types.ThreadVisibilitySummary `json:"visibility_summary"`
	Messages                 []message                     `json:"messages,omitempty"`
}

type searchThreadResult struct {
	ID                       string                        `json:"id"`
	Title                    string                        `json:"title"`
	CreatedAt                string                        `json:"created_at"`
	UpdatedAt                string                        `json:"updated_at"`
	CreatedBy                string                        `json:"created_by"`
	CreatedByUserDisplayName *string                       `json:"created_by_user_display_name"`
	CreatedByActorName       *string                       `json:"created_by_actor_name"`
	MessageCount             int                           `json:"message_count"`
	LastMessagePreview       string                        `json:"last_message_preview"`
	MatchedSnippets          []string                      `json:"matched_snippets"`
	VisibilitySummary        types.ThreadVisibilitySummary `json:"visibility_summary"`
}

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type repeatedStringFlag []string

func (values *repeatedStringFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *repeatedStringFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("team identifier must not be empty")
	}
	*values = append(*values, value)
	return nil
}

func Main(args []string) int {
	return NewRunner().Run(args)
}

func NewRunner() *Runner {
	return &Runner{
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Stdin:       os.Stdin,
		HTTPClient:  http.DefaultClient,
		RunExternal: defaultExternalCommand,
	}
}

func (r *Runner) Run(args []string) int {
	if r.Stdout == nil {
		r.Stdout = io.Discard
	}
	if r.Stderr == nil {
		r.Stderr = io.Discard
	}
	if r.Stdin == nil {
		r.Stdin = bytes.NewReader(nil)
	}
	if r.HTTPClient == nil {
		r.HTTPClient = http.DefaultClient
	}
	if r.RunExternal == nil {
		r.RunExternal = defaultExternalCommand
	}
	if err := r.run(args); err != nil {
		fmt.Fprintln(r.Stderr, err.Error())
		return 1
	}
	return 0
}

func (r *Runner) run(args []string) error {
	if len(args) == 0 {
		r.printTopLevelHelp()
		return nil
	}
	if isHelpArg(args[0]) {
		r.printTopLevelHelp()
		return nil
	}
	global := flag.NewFlagSet("agentbox", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	profileName := global.String("profile", "", "use a named profile")
	global.StringVar(profileName, "p", "", "use a named profile")
	showVersion := global.Bool("version", false, "output the version number")
	global.BoolVar(showVersion, "V", false, "output the version number")
	if err := global.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintln(r.Stdout, version.Version)
		return nil
	}
	rest := global.Args()
	if len(rest) == 0 {
		r.printTopLevelHelp()
		return nil
	}
	cmd := rest[0]
	cmdArgs := rest[1:]
	if len(cmdArgs) > 0 && isHelpArg(cmdArgs[0]) {
		r.printCommandHelp(cmd)
		return nil
	}
	switch cmd {
	case "--version", "-v", "version":
		fmt.Fprintln(r.Stdout, version.Version)
		return nil
	case "profiles":
		return r.runProfiles(cmdArgs, *profileName)
	case "login":
		return r.runLogin(cmdArgs, *profileName)
	case "doctor":
		return r.runDoctor(cmdArgs, *profileName)
	case "mcp-url":
		return r.runMCPURL(cmdArgs, *profileName)
	case "owner":
		return r.runOwner(cmdArgs, *profileName)
	case "connect":
		return r.runConnect(cmdArgs, *profileName)
	case "raycast-key":
		return r.runRaycastKey(cmdArgs, *profileName)
	case "deploy":
		return r.runDeploy(cmdArgs, *profileName)
	case "keys":
		return r.runKeys(cmdArgs, *profileName)
	case "list":
		return r.runList(cmdArgs, *profileName)
	case "search":
		return r.runSearch(cmdArgs, *profileName)
	case "create":
		return r.runCreate(cmdArgs, *profileName)
	case "get":
		return r.runGet(cmdArgs, *profileName)
	case "visibility":
		return r.runVisibility(cmdArgs, *profileName)
	case "download":
		return r.runDownload(cmdArgs, *profileName)
	case "post":
		return r.runPost(cmdArgs, *profileName)
	default:
		return fmt.Errorf("Unknown command %q.", cmd)
	}
}

func (r *Runner) printTopLevelHelp() {
	fmt.Fprintln(r.Stdout, `Usage: agentbox [options] <command>

CLI for Agentbox, a small threaded message relay for ChatGPT and local agents.

Options:
  -p, --profile <name>    use a named profile
  -V, --version           output the version number
  -h, --help              display help

Commands:
  profiles                inspect and manage CLI profiles
  login                   sign in through the browser and save a user-owned profile
  doctor                  check profile, API, MCP, and attachment access
  mcp-url                 print the full MCP URL for the selected profile
  owner                   issue one-time owner bootstrap or recovery links
  connect                 print ChatGPT MCP setup instructions
  raycast-key <label>     create an independent Raycast installation credential
  deploy                  print self-hosting deployment guidance
  keys                    manage credentials owned by the signed-in user
  list                    list recent threads
  search <query>          search threads by title and message body
  create <title>          create a thread
  get <thr_...|msg_...>   peek at a thread or message without dumping large bodies
  visibility <thread-id>  inspect or change thread visibility
  download <thread-id>    download all or one numbered attachment from a thread
  post <thread-id>        post a message to a thread

Run "agentbox <command> --help" for command-specific usage.`)
}

func (r *Runner) printCommandHelp(command string) {
	usage := map[string]string{
		"profiles": `Usage: agentbox profiles [options] [command]

Inspect and manage CLI profiles.

Options:
  --json                  print raw JSON
  -h, --help              display help

Commands:
  add <name>              create or update a stored profile
  remove <name>           delete a stored profile
  use <name>              switch the active stored profile
  show [name]             show the resolved profile`,
		"login": `Usage: agentbox login [--base-url <url>] [--profile-name <name>] [--key-name <name>] [--no-open] [--timeout <seconds>] [--json]

Open browser-based Agentbox auth, exchange the one-time CLI code for a user-owned credential, and save a local profile.`,
		"doctor": `Usage: agentbox doctor [--json]

Check profile, health, authenticated API access, signed download URLs, and MCP URL generation.`,
		"mcp-url": `Usage: agentbox mcp-url [--json]

Print the full MCP URL for the selected profile, including its API key. JSON output includes sanitized user and actor diagnostics when available.`,
		"owner": `Usage: agentbox owner setup-token [--base-url <url>] [--app-url <url>] [--admin-key <key>] [--expires 30m] [--json]

Issue a short-lived, one-time browser link that creates the permanent deployment owner or recovers that same owner account. The deployment secret is sent only to the backend and is never embedded in the browser URL.`,
		"connect": `Usage: agentbox connect chatgpt [--json]

Create a user-owned ChatGPT credential, then print the MCP URL and ChatGPT app setup steps. Store the printed MCP URL securely because it includes the key.`,
		"raycast-key": `Usage: agentbox raycast-key <installation-label> [--json]

Create an independently labeled user-owned Raycast credential and print the complete developer-mode setup bundle.`,
		"deploy": `Usage: agentbox deploy vercel

Print the Vercel commands for deploying the backend and optional dashboard. This command does not mutate Vercel projects or env vars.`,
		"keys": `Usage: agentbox keys [command]

Manage credentials owned by the signed-in profile's user.

Commands:
  create <name>           create or replace a named custom API key
  list                    page through active and revoked credential metadata
  rotate <credential-id>  rotate one active credential by stable ID
  revoke <credential-id>  revoke one credential by stable ID`,
		"list": `Usage: agentbox list [-n <limit>] [--json]

List recent Agentbox threads.`,
		"search": `Usage: agentbox search <query> [-n <limit>] [--created-by <name>] [--updated-after <timestamp>] [--json]

Search Agentbox threads by title and message body. Results include message counts, last-message previews, and matched snippets.`,
		"create": `Usage: agentbox create <title> [--message <body> | --file <path>] [--format auto|markdown|plain] [--json]

Create a new Agentbox thread. Use --message or --file to create the first message in the same request. The default format is auto; use --plain or --markdown to force body_content_type.`,
		"get": `Usage: agentbox get <thr_...|msg_...> [--full] [-o <path>] [--force] [--json]

Inspect a thread or message. Human-readable output is bounded to about 5,000 body characters by default so large remote content is not dumped into the terminal or an agent context accidentally.

Use --full to deliberately print complete bodies. Use -o/--output to write complete content directly to a file; message output is the exact body, while thread output is readable Markdown. Existing files are not overwritten unless --force is provided. --json keeps the complete structured API response for automation.

Examples:
  agentbox get thr_...
  agentbox get msg_...
  agentbox get msg_... --full
  agentbox get msg_... -o report.md
  agentbox get thr_... -o thread.md`,
		"visibility": `Usage: agentbox visibility <thread-id> [--share-team <slug-or-id>] [--unshare-team <slug-or-id>] [--publish | --unpublish] [--regenerate-public-link] [--json]

Read or atomically change a thread's team shares and public read-only link. Team flags may be repeated. Without mutation flags, prints the current visibility and teams available to the acting user.`,
		"download": `Usage: agentbox download <thread-id> [-o <dir>] [--json]
       agentbox download <thread-id> --attachment <number> [-o <file>] [--force] [--json]

Download attachments from a thread. Without --attachment, -o is the destination directory and every attachment is downloaded. With --attachment, choose the 1-based number shown by "agentbox get <thread-id>" and -o names the destination file. If -o is omitted for one attachment, the original filename is used. Existing selected-output files are not overwritten unless --force is provided.

Examples:
  agentbox download thr_... -o ./attachments
  agentbox download thr_... --attachment 1 -o ./renamed-file.pdf`,
		"post": `Usage: agentbox post <thread-id> [message] [-f <path>] [-a <path>] [--format auto|markdown|plain] [--json]

Post a message to a thread. If message is omitted and stdin is piped, the CLI reads the message body from stdin. The default format is auto; .md/.markdown files, Markdown tables, fenced code blocks, and Mermaid blocks are marked as Markdown. Use --plain for raw logs or --markdown to force Markdown rendering.`,
	}
	if text, ok := usage[command]; ok {
		fmt.Fprintln(r.Stdout, text)
		return
	}
	r.printTopLevelHelp()
}

func (r *Runner) runtimeConfig(profileName string) (RuntimeConfig, error) {
	resolved, err := profiles.Resolve(profileName)
	if err != nil {
		return RuntimeConfig{}, err
	}
	if resolved == nil {
		return RuntimeConfig{}, fmt.Errorf("Set AGENTBOX_BASE_URL and AGENTBOX_API_KEY or configure profiles in %s.", profiles.DefaultConfigPath())
	}
	return RuntimeConfig{
		ProfileName: resolved.Name,
		BaseURL:     resolved.BaseURL,
		APIKey:      resolved.APIKey,
		Source:      resolved.Source,
		Profile:     resolved.Profile,
	}, nil
}

func (r *Runner) endpoint(path string, profileName string) (*url.URL, error) {
	cfg, err := r.runtimeConfig(profileName)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(cfg.BaseURL, "/") + "/"
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, err
	}
	resolved := parsed.ResolveReference(endpoint)
	query := resolved.Query()
	query.Set("key", cfg.APIKey)
	resolved.RawQuery = query.Encode()
	return resolved, nil
}

func endpointWithKey(baseURL string, path string, apiKey string) (*url.URL, error) {
	base := strings.TrimRight(baseURL, "/") + "/"
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, err
	}
	resolved := parsed.ResolveReference(endpoint)
	query := resolved.Query()
	query.Set("key", apiKey)
	resolved.RawQuery = query.Encode()
	return resolved, nil
}

func (r *Runner) request(path string, method string, body io.Reader, headers map[string]string, profileName string, target any) error {
	endpoint, err := r.endpoint(path, profileName)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, endpoint.String(), body)
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if maintenanceKey := strings.TrimSpace(os.Getenv("AGENTBOX_MAINTENANCE_BYPASS_KEY")); maintenanceKey != "" {
		req.Header.Set("x-agentbox-maintenance-key", maintenanceKey)
	}
	res, err := r.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	bytes, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if len(bytes) > 0 {
		if err := json.Unmarshal(bytes, target); err != nil {
			return err
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var payload struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		_ = json.Unmarshal(bytes, &payload)
		if payload.Error != "" {
			if payload.Code != "" {
				return fmt.Errorf("%s: %s", payload.Code, payload.Error)
			}
			return errors.New(payload.Error)
		}
		return fmt.Errorf("Request failed with HTTP %d", res.StatusCode)
	}
	return nil
}

func (r *Runner) runDoctor(args []string, profileName string) error {
	fs := newFlagSet("doctor")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	checks := r.doctorChecks(profileName)
	if *jsonOut {
		return printJSON(r.Stdout, map[string]any{"checks": checks})
	}
	failed := 0
	for _, check := range checks {
		icon := "✓"
		if check.Status == "skip" {
			icon = "-"
		}
		if check.Status == "fail" {
			icon = "✗"
			failed++
		}
		detail := ""
		if check.Detail != "" {
			detail = " — " + check.Detail
		}
		fmt.Fprintf(r.Stdout, "%s %s%s\n", icon, check.Name, detail)
	}
	if failed > 0 {
		return fmt.Errorf("%d check%s failed.", failed, plural(failed))
	}
	return nil
}

func (r *Runner) runMCPURL(args []string, profileName string) error {
	fs := newFlagSet("mcp-url")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	endpoint, err := r.endpoint("/api/mcp", profileName)
	if err != nil {
		return err
	}
	if *jsonOut {
		cfg, err := r.runtimeConfig(profileName)
		if err != nil {
			return err
		}
		output := map[string]any{
			"mcp_url":        endpoint.String(),
			"mcp_url_masked": profiles.SanitizeURL(endpoint.String()),
			"profile":        cfg.ProfileName,
			"source":         cfg.Source,
		}
		var me struct {
			Auth types.AuthContext `json:"auth"`
		}
		if err := r.request("/api/auth/me", http.MethodGet, nil, nil, profileName, &me); err == nil {
			output["auth"] = me.Auth
		}
		return printJSON(r.Stdout, output)
	}
	fmt.Fprintln(r.Stdout, endpoint.String())
	return nil
}

func (r *Runner) doctorChecks(profileName string) []doctorCheck {
	var checks []doctorCheck
	add := func(name string, status string, detail string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Detail: detail})
	}
	cfg, err := r.runtimeConfig(profileName)
	if err != nil {
		add("profile", "fail", err.Error())
		add("base URL", "fail", err.Error())
		add("API key", "fail", err.Error())
		add("health endpoint", "fail", err.Error())
		add("authenticated API", "fail", err.Error())
		add("ChatGPT MCP URL", "fail", err.Error())
		return checks
	}
	add("profile", "pass", fmt.Sprintf("%s (%s)", cfg.ProfileName, cfg.Source))
	add("base URL", "pass", cfg.BaseURL)
	add("API key", "pass", fmt.Sprintf("Profile %s includes key %s", cfg.ProfileName, profiles.MaskSecret(cfg.APIKey)))

	healthURL, _ := url.JoinPath(strings.TrimRight(cfg.BaseURL, "/"), "/api/health")
	if res, err := r.HTTPClient.Get(healthURL); err != nil {
		add("health endpoint", "fail", err.Error())
	} else {
		_ = res.Body.Close()
		status := "fail"
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			status = "pass"
		}
		add("health endpoint", status, fmt.Sprintf("HTTP %d", res.StatusCode))
	}
	var listed struct {
		Threads []thread `json:"threads"`
	}
	if err := r.request("/api/threads?limit=10", http.MethodGet, nil, nil, profileName, &listed); err != nil {
		add("authenticated API", "fail", err.Error())
	} else {
		add("authenticated API", "pass", fmt.Sprintf("%d thread(s) visible", len(listed.Threads)))
		var me struct {
			Auth struct {
				UserID    string `json:"user_id"`
				ActorName string `json:"actor_name"`
			} `json:"auth"`
		}
		if err := r.request("/api/auth/me", http.MethodGet, nil, nil, profileName, &me); err == nil && me.Auth.UserID != "" {
			detail := "user " + me.Auth.UserID
			if me.Auth.ActorName != "" {
				detail += " (" + me.Auth.ActorName + ")"
			}
			add("resolved user", "pass", detail)
		}
		asset, err := r.findRecentAsset(listed.Threads, profileName)
		if err != nil {
			add("signed download URL", "fail", err.Error())
		} else if asset == nil {
			add("signed download URL", "skip", "No attachments found in recent threads")
		} else {
			var signed struct {
				DownloadURL string `json:"download_url"`
			}
			if err := r.request("/api/assets/"+url.PathEscape(asset.ID)+"/download-url", http.MethodGet, nil, nil, profileName, &signed); err != nil {
				add("signed download URL", "fail", err.Error())
			} else if signed.DownloadURL == "" {
				add("signed download URL", "fail", asset.FileName)
			} else {
				add("signed download URL", "pass", asset.FileName)
			}
		}
	}
	endpoint, err := r.endpoint("/api/mcp", profileName)
	if err != nil {
		add("ChatGPT MCP URL", "fail", err.Error())
	} else {
		add("ChatGPT MCP URL", "pass", profiles.SanitizeURL(endpoint.String()))
	}
	return checks
}

func (r *Runner) findRecentAsset(threads []thread, profileName string) (*asset, error) {
	for _, listed := range threads {
		var detailed struct {
			Thread thread `json:"thread"`
		}
		if err := r.request("/api/threads/"+url.PathEscape(listed.ID), http.MethodGet, nil, nil, profileName, &detailed); err != nil {
			return nil, err
		}
		for _, message := range detailed.Thread.Messages {
			if len(message.Assets) > 0 {
				found := message.Assets[0]
				return &found, nil
			}
		}
	}
	return nil, nil
}

func (r *Runner) runList(args []string, profileName string) error {
	fs := newFlagSet("list")
	limit := fs.String("limit", "50", "maximum number of threads")
	fs.StringVar(limit, "n", "50", "maximum number of threads")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	var data struct {
		Threads []thread `json:"threads"`
	}
	if err := r.request("/api/threads?limit="+strconv.Itoa(numberOrZero(*limit)), http.MethodGet, nil, nil, profileName, &data); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(r.Stdout, data)
	}
	for _, thread := range data.Threads {
		fmt.Fprintf(r.Stdout, "%s\t%s\t%s\n", thread.ID, thread.UpdatedAt, thread.Title)
		fmt.Fprintf(r.Stdout, "  %s · Created by %s\n", visibilitySummaryLabel(thread.VisibilitySummary), attributionLabel(thread.CreatedByUserDisplayName, thread.CreatedByActorName, thread.CreatedBy))
	}
	return nil
}

func (r *Runner) runSearch(args []string, profileName string) error {
	fs := newFlagSet("search")
	limit := fs.String("limit", "20", "maximum number of results")
	fs.StringVar(limit, "n", "20", "maximum number of results")
	createdBy := fs.String("created-by", "", "filter by thread creator")
	updatedAfter := fs.String("updated-after", "", "filter by RFC3339 updated_at timestamp")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox search <query> [-n <limit>] [--created-by <name>] [--updated-after <timestamp>] [--json]")
	}
	query := url.Values{}
	query.Set("query", fs.Arg(0))
	query.Set("limit", strconv.Itoa(numberOrZero(*limit)))
	if strings.TrimSpace(*createdBy) != "" {
		query.Set("created_by", strings.TrimSpace(*createdBy))
	}
	if strings.TrimSpace(*updatedAfter) != "" {
		query.Set("updated_after", strings.TrimSpace(*updatedAfter))
	}
	var data struct {
		Threads []searchThreadResult `json:"threads"`
	}
	if err := r.request("/api/threads?"+query.Encode(), http.MethodGet, nil, nil, profileName, &data); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(r.Stdout, data)
	}
	for _, thread := range data.Threads {
		fmt.Fprintf(r.Stdout, "%s\t%s\t%d\t%s\n", thread.ID, thread.UpdatedAt, thread.MessageCount, thread.Title)
		fmt.Fprintf(r.Stdout, "  %s · Created by %s\n", visibilitySummaryLabel(thread.VisibilitySummary), attributionLabel(thread.CreatedByUserDisplayName, thread.CreatedByActorName, thread.CreatedBy))
		if thread.LastMessagePreview != "" {
			fmt.Fprintf(r.Stdout, "  %s\n", thread.LastMessagePreview)
		}
		for _, snippet := range thread.MatchedSnippets {
			if snippet != "" && snippet != thread.LastMessagePreview {
				fmt.Fprintf(r.Stdout, "  match: %s\n", snippet)
			}
		}
	}
	return nil
}

func (r *Runner) runCreate(args []string, profileName string) error {
	fs := newFlagSet("create")
	messageBody := fs.String("message", "", "create the first message with this body")
	fs.StringVar(messageBody, "m", "", "create the first message with this body")
	filePath := fs.String("file", "", "read the first message body from a Markdown/text file")
	fs.StringVar(filePath, "f", "", "read the first message body from a Markdown/text file")
	format := fs.String("format", messageformat.Auto, "initial message body format: auto, markdown, or plain")
	markdown := fs.Bool("markdown", false, "render initial message body as Markdown")
	plain := fs.Bool("plain", false, "render initial message body as plain text")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox create <title> [--message <body> | --file <path>] [--format auto|markdown|plain] [--json]")
	}
	if *messageBody != "" && *filePath != "" {
		return errors.New("Use only one of --message or --file.")
	}
	body := *messageBody
	if *filePath != "" {
		bytes, err := os.ReadFile(*filePath)
		if err != nil {
			return err
		}
		body = string(bytes)
	}
	payload := map[string]string{"title": fs.Arg(0)}
	if body != "" || *filePath != "" || *messageBody != "" {
		requestedFormat, err := requestedBodyContentType(*format, *markdown, *plain)
		if err != nil {
			return err
		}
		bodyContentType, err := messageformat.Resolve(requestedFormat, body, *filePath)
		if err != nil {
			return err
		}
		payload["initial_message"] = body
		payload["body_content_type"] = bodyContentType
	}
	payloadBytes, _ := json.Marshal(payload)
	var data struct {
		Thread  thread         `json:"thread"`
		Message *types.Message `json:"message,omitempty"`
	}
	if err := r.request("/api/threads", http.MethodPost, bytes.NewReader(payloadBytes), map[string]string{"content-type": "application/json"}, profileName, &data); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(r.Stdout, data)
	}
	fmt.Fprintf(r.Stdout, "%s\t%s\n", data.Thread.ID, data.Thread.Title)
	if data.Message != nil && data.Message.ID != "" {
		fmt.Fprintf(r.Stdout, "%s\tinitial message\n", data.Message.ID)
	}
	return nil
}

func (r *Runner) runGet(args []string, profileName string) error {
	fs := newFlagSet("get")
	full := fs.Bool("full", false, "print complete message bodies")
	output := fs.String("output", "", "write complete content to a file")
	fs.StringVar(output, "o", "", "write complete content to a file")
	force := fs.Bool("force", false, "overwrite an existing output file")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox get <thr_...|msg_...> [--full] [-o <path>] [--force] [--json]")
	}
	if *force && strings.TrimSpace(*output) == "" {
		return errors.New("--force requires -o/--output.")
	}

	resourceID := strings.TrimSpace(fs.Arg(0))
	switch {
	case strings.HasPrefix(resourceID, "thr_"):
		var data struct {
			Thread thread `json:"thread"`
		}
		if err := r.request("/api/threads/"+url.PathEscape(resourceID), http.MethodGet, nil, nil, profileName, &data); err != nil {
			return err
		}
		if strings.TrimSpace(*output) != "" {
			contents := renderThreadMarkdown(data.Thread)
			if err := writeOutputFile(*output, []byte(contents), *force); err != nil {
				return err
			}
			if *jsonOut {
				return printJSON(r.Stdout, map[string]any{
					"thread_id":     data.Thread.ID,
					"output_path":   *output,
					"message_count": len(data.Thread.Messages),
					"bytes_written": len([]byte(contents)),
				})
			}
			fmt.Fprintf(r.Stdout, "Saved %d message%s (%d bytes) to %s\n", len(data.Thread.Messages), plural(len(data.Thread.Messages)), len([]byte(contents)), *output)
			return nil
		}
		if *jsonOut {
			return printJSON(r.Stdout, data)
		}
		budget := defaultGetBodyBudget
		if *full {
			budget = -1
		}
		printThread(r.Stdout, data.Thread, budget)
		return nil

	case strings.HasPrefix(resourceID, "msg_"):
		var data struct {
			Message message `json:"message"`
		}
		if err := r.request("/api/messages/"+url.PathEscape(resourceID), http.MethodGet, nil, nil, profileName, &data); err != nil {
			return err
		}
		if strings.TrimSpace(*output) != "" {
			if err := writeOutputFile(*output, []byte(data.Message.Body), *force); err != nil {
				return err
			}
			characterCount := utf8.RuneCountInString(data.Message.Body)
			byteCount := len([]byte(data.Message.Body))
			if *jsonOut {
				return printJSON(r.Stdout, map[string]any{
					"message_id":         data.Message.ID,
					"thread_id":          data.Message.ThreadID,
					"output_path":        *output,
					"message_count":      1,
					"characters_written": characterCount,
					"bytes_written":      byteCount,
				})
			}
			fmt.Fprintf(r.Stdout, "Saved 1 message (%d characters, %d bytes) to %s\n", characterCount, byteCount, *output)
			return nil
		}
		if *jsonOut {
			return printJSON(r.Stdout, data)
		}
		budget := defaultGetBodyBudget
		if *full {
			budget = -1
		}
		printMessage(r.Stdout, data.Message, budget, true)
		return nil

	default:
		return errors.New("get expects a typed Agentbox resource ID beginning with thr_ or msg_.")
	}
}

func (r *Runner) runVisibility(args []string, profileName string) error {
	fs := newFlagSet("visibility")
	var shareTeams repeatedStringFlag
	var unshareTeams repeatedStringFlag
	fs.Var(&shareTeams, "share-team", "share with a team slug or ID; may be repeated")
	fs.Var(&unshareTeams, "unshare-team", "remove a team share by slug or ID; may be repeated")
	publish := fs.Bool("publish", false, "enable the public read-only link")
	unpublish := fs.Bool("unpublish", false, "disable the public read-only link")
	regenerate := fs.Bool("regenerate-public-link", false, "replace the active public link")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox visibility <thread-id> [--share-team <slug-or-id>] [--unshare-team <slug-or-id>] [--publish | --unpublish] [--regenerate-public-link] [--json]")
	}
	if *publish && *unpublish {
		return errors.New("Use only one of --publish or --unpublish.")
	}
	if *unpublish && *regenerate {
		return errors.New("--regenerate-public-link cannot be combined with --unpublish.")
	}

	threadID := strings.TrimSpace(fs.Arg(0))
	path := "/api/threads/" + url.PathEscape(threadID) + "/visibility"
	mutation := len(shareTeams) > 0 || len(unshareTeams) > 0 || *publish || *unpublish || *regenerate
	method := http.MethodGet
	var body io.Reader
	headers := map[string]string(nil)
	if mutation {
		method = http.MethodPatch
		payload := types.ManageThreadVisibilityInput{
			AddTeams:             append([]string(nil), shareTeams...),
			RemoveTeams:          append([]string(nil), unshareTeams...),
			RegeneratePublicLink: *regenerate,
		}
		if *publish || *unpublish {
			public := *publish
			payload.Public = &public
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payloadBytes)
		headers = map[string]string{"content-type": "application/json"}
	}
	var data struct {
		Visibility types.ManagedThreadVisibility `json:"visibility"`
	}
	if err := r.request(path, method, body, headers, profileName, &data); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(r.Stdout, data)
	}
	printVisibility(r.Stdout, data.Visibility)
	return nil
}

func printVisibility(w io.Writer, visibility types.ManagedThreadVisibility) {
	fmt.Fprintf(w, "Thread: %s\n", visibility.ThreadID)
	fmt.Fprintf(w, "Owner: %s\n", visibility.OwnerUserID)
	if len(visibility.SharedTeams) == 0 {
		fmt.Fprintln(w, "Team access: Private")
	} else {
		fmt.Fprintln(w, "Team access:")
		for _, team := range visibility.SharedTeams {
			fmt.Fprintf(w, "- %s (%s)\n", team.Name, team.Slug)
		}
	}
	if visibility.Public {
		if visibility.PublicURL != "" {
			fmt.Fprintf(w, "Public: %s\n", visibility.PublicURL)
		} else {
			fmt.Fprintln(w, "Public: On")
		}
	} else {
		fmt.Fprintln(w, "Public: Off")
	}
	if len(visibility.AvailableTeams) == 0 {
		fmt.Fprintln(w, "Available teams: none")
		return
	}
	fmt.Fprintln(w, "Available teams:")
	for _, team := range visibility.AvailableTeams {
		fmt.Fprintf(w, "- %s (%s)\n", team.Name, team.Slug)
	}
}

func (r *Runner) runDownload(args []string, profileName string) error {
	fs := newFlagSet("download")
	output := fs.String("output", "", "destination directory, or destination file with --attachment")
	fs.StringVar(output, "o", "", "destination directory, or destination file with --attachment")
	attachmentNumber := fs.Int("attachment", 0, "1-based attachment number shown by agentbox get")
	force := fs.Bool("force", false, "overwrite an existing selected output file")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox download <thread-id> [-o <dir>] [--json] | agentbox download <thread-id> --attachment <number> [-o <file>] [--force] [--json]")
	}
	if *attachmentNumber < 0 {
		return errors.New("--attachment must be a positive 1-based number.")
	}
	if *force && *attachmentNumber == 0 {
		return errors.New("--force is only used with --attachment.")
	}
	threadID := strings.TrimSpace(fs.Arg(0))
	var data struct {
		Thread thread `json:"thread"`
	}
	if err := r.request("/api/threads/"+url.PathEscape(threadID), http.MethodGet, nil, nil, profileName, &data); err != nil {
		return err
	}

	type indexedAttachment struct {
		Number    int
		MessageID string
		Asset     asset
	}
	attachments := []indexedAttachment{}
	for _, message := range data.Thread.Messages {
		for _, asset := range message.Assets {
			attachments = append(attachments, indexedAttachment{Number: len(attachments) + 1, MessageID: message.ID, Asset: asset})
		}
	}

	if *attachmentNumber > 0 {
		if *attachmentNumber > len(attachments) {
			if len(attachments) == 0 {
				return fmt.Errorf("No attachments found for %s.", threadID)
			}
			return fmt.Errorf("Attachment %d does not exist; %s has %d attachment%s. Run \"agentbox get %s\" to see the numbered list.", *attachmentNumber, threadID, len(attachments), plural(len(attachments)), threadID)
		}
		selected := attachments[*attachmentNumber-1]
		outputPath := strings.TrimSpace(*output)
		if outputPath == "" {
			outputPath = assetFileName(selected.Asset)
		}
		if err := r.downloadAsset(selected.Asset, outputPath, profileName, *force); err != nil {
			return err
		}
		result := map[string]any{
			"thread_id":   threadID,
			"attachment":  selected.Number,
			"message_id":  selected.MessageID,
			"asset_id":    selected.Asset.ID,
			"file_name":   assetFileName(selected.Asset),
			"mime_type":   assetContentType(selected.Asset),
			"size_bytes":  selected.Asset.SizeBytes,
			"output_path": outputPath,
		}
		if *jsonOut {
			return printJSON(r.Stdout, result)
		}
		fmt.Fprintf(r.Stdout, "Saved attachment %d (%s, %s, %d bytes) to %s\n", selected.Number, assetFileName(selected.Asset), assetContentType(selected.Asset), selected.Asset.SizeBytes, outputPath)
		return nil
	}

	outputDir := strings.TrimSpace(*output)
	if outputDir == "" {
		outputDir = filepath.Join("agentbox-downloads", threadID)
	}
	downloads := []map[string]string{}
	for _, attachment := range attachments {
		outputPath := filepath.Join(outputDir, attachment.Asset.ID+"-"+assetFileName(attachment.Asset))
		if err := r.downloadAsset(attachment.Asset, outputPath, profileName, true); err != nil {
			return err
		}
		downloads = append(downloads, map[string]string{
			"message_id":  attachment.MessageID,
			"asset_id":    attachment.Asset.ID,
			"file_name":   assetFileName(attachment.Asset),
			"storage_key": attachment.Asset.StorageKey,
			"output_path": outputPath,
		})
	}
	result := map[string]any{"thread_id": threadID, "output_dir": outputDir, "downloads": downloads}
	if *jsonOut {
		return printJSON(r.Stdout, result)
	}
	if len(downloads) == 0 {
		fmt.Fprintf(r.Stdout, "No attachments found for %s.\n", threadID)
		return nil
	}
	fmt.Fprintf(r.Stdout, "Saved %d attachment%s to %s\n", len(downloads), plural(len(downloads)), outputDir)
	for _, download := range downloads {
		fmt.Fprintf(r.Stdout, "- %s -> %s\n", download["file_name"], download["output_path"])
	}
	return nil
}

func (r *Runner) downloadAsset(asset asset, outputPath string, profileName string, force bool) error {
	if err := ensureOutputAvailable(outputPath, force); err != nil {
		return err
	}

	var signed struct {
		DownloadURL string `json:"download_url"`
	}
	if err := r.request("/api/assets/"+url.PathEscape(asset.ID)+"/download-url", http.MethodGet, nil, nil, profileName, &signed); err != nil {
		return err
	}
	res, err := r.HTTPClient.Get(signed.DownloadURL)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Direct R2 download failed with HTTP %d", res.StatusCode)
	}
	file, err := openOutputFile(outputPath, force)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(outputPath)
		}
	}()
	if _, err := io.Copy(file, res.Body); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (r *Runner) runPost(args []string, profileName string) error {
	fs := newFlagSet("post")
	filePath := fs.String("file", "", "read message body from a Markdown/text file")
	fs.StringVar(filePath, "f", "", "read message body from a Markdown/text file")
	assetPath := fs.String("asset", "", "attach a local file")
	fs.StringVar(assetPath, "a", "", "attach a local file")
	format := fs.String("format", messageformat.Auto, "message body format: auto, markdown, or plain")
	markdown := fs.Bool("markdown", false, "render message body as Markdown")
	plain := fs.Bool("plain", false, "render message body as plain text")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return errors.New("Usage: agentbox post <thread-id> [message] [-f <path>] [-a <path>] [--json]")
	}
	threadID := fs.Arg(0)
	body := ""
	if fs.NArg() == 2 {
		body = fs.Arg(1)
	}
	if *filePath != "" {
		bytes, err := os.ReadFile(*filePath)
		if err != nil {
			return err
		}
		body = string(bytes)
	}
	if body == "" && shouldReadStdin(r.Stdin) {
		bytes, err := io.ReadAll(r.Stdin)
		if err != nil {
			return err
		}
		body = string(bytes)
	}
	requestedFormat, err := requestedBodyContentType(*format, *markdown, *plain)
	if err != nil {
		return err
	}
	bodyContentType, err := messageformat.Resolve(requestedFormat, body, *filePath)
	if err != nil {
		return err
	}
	var data struct {
		Message types.Message `json:"message"`
	}
	if *assetPath == "" {
		payload, _ := json.Marshal(map[string]string{"body": body, "body_content_type": bodyContentType})
		if err := r.request("/api/threads/"+url.PathEscape(threadID)+"/messages", http.MethodPost, bytes.NewReader(payload), map[string]string{"content-type": "application/json"}, profileName, &data); err != nil {
			return err
		}
	} else {
		payload, contentType, err := multipartBody(body, bodyContentType, *assetPath)
		if err != nil {
			return err
		}
		if err := r.request("/api/threads/"+url.PathEscape(threadID)+"/messages", http.MethodPost, payload, map[string]string{"content-type": contentType}, profileName, &data); err != nil {
			return err
		}
	}
	if *jsonOut {
		return printJSON(r.Stdout, data)
	}
	fmt.Fprintln(r.Stdout, data.Message.ID)
	return nil
}

func requestedBodyContentType(format string, markdown bool, plain bool) (*string, error) {
	if markdown && plain {
		return nil, errors.New("Use only one of --markdown or --plain.")
	}
	if markdown {
		value := messageformat.Markdown
		return &value, nil
	}
	if plain {
		value := messageformat.Plain
		return &value, nil
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", messageformat.Auto:
		value := messageformat.Auto
		return &value, nil
	case "markdown", "md", messageformat.Markdown:
		value := messageformat.Markdown
		return &value, nil
	case "plain", "text", messageformat.Plain:
		value := messageformat.Plain
		return &value, nil
	default:
		return nil, errors.New("--format must be auto, markdown, or plain")
	}
}

func shouldReadStdin(reader io.Reader) bool {
	if reader == nil {
		return false
	}
	file, ok := reader.(*os.File)
	if !ok {
		return true
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

func multipartBody(body string, bodyContentType string, assetPath string) (*bytes.Reader, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("body", body); err != nil {
		return nil, "", err
	}
	if err := writer.WriteField("body_content_type", bodyContentType); err != nil {
		return nil, "", err
	}
	file, err := os.Open(assetPath)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	fileName := filepath.Base(assetPath)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="asset"; filename="%s"`, escapeQuotes(fileName)))
	contentType := mime.TypeByExtension(filepath.Ext(fileName))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return bytes.NewReader(buf.Bytes()), writer.FormDataContentType(), nil
}

func printThread(w io.Writer, thread thread, bodyBudget int) {
	fmt.Fprintf(w, "# %s\n", thread.Title)
	fmt.Fprintf(w, "Thread: %s\n", thread.ID)
	fmt.Fprintf(w, "Updated: %s\n", thread.UpdatedAt)
	fmt.Fprintf(w, "Created by: %s\n", attributionLabel(thread.CreatedByUserDisplayName, thread.CreatedByActorName, thread.CreatedBy))
	fmt.Fprintf(w, "Visibility: %s\n", visibilitySummaryLabel(thread.VisibilitySummary))
	fmt.Fprintf(w, "Messages: %d\n", len(thread.Messages))

	allocations, totalCharacters := bodyPreviewAllocations(thread.Messages, bodyBudget)
	shownCharacters := 0
	for _, allocation := range allocations {
		shownCharacters += allocation
	}
	if shownCharacters < totalCharacters {
		fmt.Fprintf(w, "Body preview: showing %d of %d characters across the thread.\n", shownCharacters, totalCharacters)
	}
	fmt.Fprintln(w)

	attachmentNumber := 1
	for index, message := range thread.Messages {
		fmt.Fprintf(w, "## Message %s\n", message.ID)
		printMessageMetadata(w, message)
		printMessageBody(w, message, allocations[index], true)
		if len(message.Assets) > 0 {
			fmt.Fprintln(w, "Attachments:")
			for _, attached := range message.Assets {
				fmt.Fprintf(w, "[%d] %s · %s · %d bytes · %s\n", attachmentNumber, assetFileName(attached), assetContentType(attached), attached.SizeBytes, attached.ID)
				attachmentNumber++
			}
			fmt.Fprintf(w, "Download one: agentbox download %s --attachment <number> -o <file>\n", thread.ID)
		}
		fmt.Fprintln(w)
	}
}

func printMessage(w io.Writer, message message, bodyBudget int, includeThread bool) {
	fmt.Fprintf(w, "# Message %s\n", message.ID)
	if includeThread {
		fmt.Fprintf(w, "Thread: %s\n", message.ThreadID)
	}
	printMessageMetadata(w, message)
	allocation := utf8.RuneCountInString(message.Body)
	if bodyBudget >= 0 && allocation > bodyBudget {
		allocation = bodyBudget
	}
	printMessageBody(w, message, allocation, true)
	if len(message.Assets) > 0 {
		fmt.Fprintln(w, "Attachments:")
		for _, attached := range message.Assets {
			fmt.Fprintf(w, "- %s · %s · %d bytes · %s\n", assetFileName(attached), assetContentType(attached), attached.SizeBytes, attached.ID)
		}
	}
}

func printMessageMetadata(w io.Writer, message message) {
	fmt.Fprintf(w, "Author: %s\n", attributionLabel(message.CreatedByUserDisplayName, message.CreatedByActorName, message.Author))
	fmt.Fprintf(w, "Created: %s\n", message.CreatedAt)
	fmt.Fprintf(w, "Content type: %s\n", messageContentType(message))
	fmt.Fprintf(w, "Characters: %d\n", utf8.RuneCountInString(message.Body))
	fmt.Fprintf(w, "Attachments: %d\n\n", len(message.Assets))
}

func printMessageBody(w io.Writer, message message, allocation int, includeHints bool) {
	total := utf8.RuneCountInString(message.Body)
	if total == 0 {
		fmt.Fprintln(w, "(empty message)")
		fmt.Fprintln(w)
		return
	}
	if allocation < 0 || allocation > total {
		allocation = total
	}
	preview := firstRunes(message.Body, allocation)
	fmt.Fprintln(w, preview)
	if allocation < total {
		fmt.Fprintf(w, "\nShowing %d of %d characters.\n", allocation, total)
		if includeHints {
			fmt.Fprintf(w, "View complete message: agentbox get %s --full\n", message.ID)
			fmt.Fprintf(w, "Save complete message: agentbox get %s -o message.md\n", message.ID)
		}
	}
	fmt.Fprintln(w)
}

func bodyPreviewAllocations(messages []message, bodyBudget int) ([]int, int) {
	allocations := make([]int, len(messages))
	lengths := make([]int, len(messages))
	total := 0
	active := []int{}
	for index, message := range messages {
		lengths[index] = utf8.RuneCountInString(message.Body)
		total += lengths[index]
		if lengths[index] > 0 {
			active = append(active, index)
		}
	}
	if bodyBudget < 0 || total <= bodyBudget {
		copy(allocations, lengths)
		return allocations, total
	}
	if bodyBudget <= 0 || len(active) == 0 {
		return allocations, total
	}

	remainingBudget := bodyBudget
	remaining := append([]int(nil), active...)
	for len(remaining) > 0 && remainingBudget > 0 {
		share := remainingBudget / len(remaining)
		if share == 0 {
			for _, index := range remaining {
				if remainingBudget == 0 {
					break
				}
				allocations[index]++
				remainingBudget--
			}
			break
		}

		next := []int{}
		allocatedSmall := false
		for _, index := range remaining {
			if lengths[index] <= share {
				allocations[index] = lengths[index]
				remainingBudget -= lengths[index]
				allocatedSmall = true
				continue
			}
			next = append(next, index)
		}
		if allocatedSmall {
			remaining = next
			continue
		}

		for _, index := range remaining {
			allocations[index] = share
			remainingBudget -= share
		}
		for _, index := range remaining {
			if remainingBudget == 0 {
				break
			}
			allocations[index]++
			remainingBudget--
		}
		break
	}
	return allocations, total
}

func firstRunes(value string, count int) string {
	if count <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= count {
		return value
	}
	runes := []rune(value)
	return string(runes[:count])
}

func messageContentType(message message) string {
	contentType := strings.TrimSpace(defaultStringValue(message.BodyContentType))
	if contentType == "" {
		return "unspecified"
	}
	return contentType
}

func assetFileName(attached asset) string {
	if value := strings.TrimSpace(attached.FileName); value != "" {
		return value
	}
	if value := strings.TrimSpace(attached.Filename); value != "" {
		return value
	}
	return attached.ID
}

func assetContentType(attached asset) string {
	if value := strings.TrimSpace(defaultStringValue(attached.MimeType)); value != "" {
		return value
	}
	return "application/octet-stream"
}

func renderThreadMarkdown(thread thread) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# %s\n\n", thread.Title)
	fmt.Fprintf(&builder, "- Thread: `%s`\n", thread.ID)
	fmt.Fprintf(&builder, "- Updated: %s\n", thread.UpdatedAt)
	fmt.Fprintf(&builder, "- Created by: %s\n", attributionLabel(thread.CreatedByUserDisplayName, thread.CreatedByActorName, thread.CreatedBy))
	fmt.Fprintf(&builder, "- Visibility: %s\n", visibilitySummaryLabel(thread.VisibilitySummary))
	fmt.Fprintf(&builder, "- Messages: %d\n", len(thread.Messages))
	for _, message := range thread.Messages {
		fmt.Fprintf(&builder, "\n---\n\n## Message `%s`\n\n", message.ID)
		fmt.Fprintf(&builder, "- Author: %s\n", attributionLabel(message.CreatedByUserDisplayName, message.CreatedByActorName, message.Author))
		fmt.Fprintf(&builder, "- Created: %s\n", message.CreatedAt)
		fmt.Fprintf(&builder, "- Content type: %s\n", messageContentType(message))
		fmt.Fprintf(&builder, "- Characters: %d\n", utf8.RuneCountInString(message.Body))
		fmt.Fprintf(&builder, "- Attachments: %d\n\n", len(message.Assets))
		builder.WriteString(message.Body)
		if !strings.HasSuffix(message.Body, "\n") {
			builder.WriteString("\n")
		}
		if len(message.Assets) > 0 {
			builder.WriteString("\n### Attachments\n\n")
			for _, attached := range message.Assets {
				fmt.Fprintf(&builder, "- `%s` (%s, %d bytes, `%s`)\n", assetFileName(attached), assetContentType(attached), attached.SizeBytes, attached.ID)
			}
		}
	}
	return builder.String()
}

func writeOutputFile(outputPath string, contents []byte, force bool) error {
	file, err := openOutputFile(outputPath, force)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(outputPath)
		}
	}()
	if _, err := file.Write(contents); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func openOutputFile(outputPath string, force bool) (*os.File, error) {
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return nil, errors.New("output path must not be empty")
	}
	parent := filepath.Dir(outputPath)
	if parent != "." && parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return nil, err
		}
	}
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(outputPath, flags, 0o644)
	if errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("output file already exists: %s (use --force to overwrite)", outputPath)
	}
	return file, err
}

func ensureOutputAvailable(outputPath string, force bool) error {
	if force {
		return nil
	}
	_, err := os.Stat(outputPath)
	if err == nil {
		return fmt.Errorf("output file already exists: %s (use --force to overwrite)", outputPath)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func attributionLabel(userDisplayName *string, actorName *string, fallback string) string {
	user := strings.TrimSpace(defaultStringValue(userDisplayName))
	actor := strings.TrimSpace(defaultStringValue(actorName))
	if user != "" && actor != "" && !strings.EqualFold(user, actor) {
		return user + " · " + actor
	}
	if user != "" {
		return user
	}
	if actor != "" {
		return actor
	}
	if fallback != "" {
		return fallback
	}
	return "Agentbox user"
}

func visibilitySummaryLabel(summary types.ThreadVisibilitySummary) string {
	labels := []string{}
	if summary.Private {
		labels = append(labels, "Private")
	}
	for _, team := range summary.SharedTeams {
		labels = append(labels, team.Name)
	}
	if summary.Public {
		labels = append(labels, "Public")
	}
	if len(labels) == 0 {
		if summary.OwnedByMe {
			return "Owned"
		}
		if summary.SharedWithMe {
			return "Shared"
		}
		return "Accessible"
	}
	return strings.Join(labels, ", ")
}

func defaultStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func printJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	var flags []string
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if before, _, found := strings.Cut(name, "="); found {
			name = before
		}
		if flagInfo := fs.Lookup(name); flagInfo != nil && flagInfo.DefValue != "false" && flagInfo.DefValue != "true" && !strings.Contains(arg, "=") {
			if i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		}
	}
	return fs.Parse(append(flags, positionals...))
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

func numberOrZero(value string) int {
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return number
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func escapeQuotes(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}
