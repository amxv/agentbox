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

	"agentbox/internal/agentbox/messageformat"
	"agentbox/internal/agentbox/profiles"
	"agentbox/internal/agentbox/types"
	"agentbox/internal/agentbox/version"
)

type Runner struct {
	Stdout     io.Writer
	Stderr     io.Writer
	Stdin      io.Reader
	HTTPClient *http.Client
	RunExternal ExternalCommandFunc
}

type RuntimeConfig struct {
	ProfileName string
	BaseURL     string
	APIKey      string
	Source      string
}

type asset struct {
	ID         string  `json:"id"`
	FileName   string  `json:"file_name"`
	MimeType   *string `json:"mime_type"`
	SizeBytes  int64   `json:"size_bytes"`
	PublicURL  *string `json:"public_url"`
	StorageKey string  `json:"storage_key"`
}

type message struct {
	ID              string  `json:"id"`
	ThreadID        string  `json:"thread_id"`
	Author          string  `json:"author"`
	Body            string  `json:"body"`
	BodyContentType *string `json:"body_content_type"`
	CreatedAt       string  `json:"created_at"`
	Assets          []asset `json:"assets"`
}

type thread struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at"`
	CreatedBy string    `json:"created_by"`
	Messages  []message `json:"messages,omitempty"`
}

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func Main(args []string) int {
	return NewRunner().Run(args)
}

func NewRunner() *Runner {
	return &Runner{
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Stdin:      os.Stdin,
		HTTPClient: http.DefaultClient,
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
	case "doctor":
		return r.runDoctor(cmdArgs, *profileName)
	case "mcp-url":
		return r.runMCPURL(cmdArgs, *profileName)
	case "init":
		return r.runInit(cmdArgs, *profileName)
	case "connect":
		return r.runConnect(cmdArgs, *profileName)
	case "deploy":
		return r.runDeploy(cmdArgs, *profileName)
	case "keys":
		return r.runKeys(cmdArgs)
	case "list":
		return r.runList(cmdArgs, *profileName)
	case "create":
		return r.runCreate(cmdArgs, *profileName)
	case "get":
		return r.runGet(cmdArgs, *profileName)
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
  doctor                  check profile, API, MCP, and attachment access
  mcp-url                 print the full MCP URL for the selected profile
  init                    save a local profile and optionally verify it
  connect                 print ChatGPT MCP setup instructions
  deploy                  help deploy Agentbox to Vercel
  keys                    generate and manage labeled API keys
  list                    list recent threads
  create <title>          create a thread
  get <thread-id>         read a thread
  download <thread-id>    download all attachments from a thread
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
		"doctor": `Usage: agentbox doctor [--json]

Check profile, health, authenticated API access, signed download URLs, and MCP URL generation.`,
		"mcp-url": `Usage: agentbox mcp-url [--json]

Print the full MCP URL for the selected profile, including its API key.`,
		"init": `Usage: agentbox init [--profile-name <name>] [--base-url <url>] [--api-key <key>] [--skip-doctor] [--json]

Save a local CLI profile, prompting for any missing values.`,
		"connect": `Usage: agentbox connect chatgpt [--json]

Print the MCP URL for the selected profile plus the ChatGPT app setup steps.`,
		"deploy": `Usage: agentbox deploy vercel [options]

Link backend/dashboard Vercel projects, set env vars, deploy both services, and optionally save a local CLI profile.`,
		"keys": `Usage: agentbox keys [command]

Generate and manage labeled API keys for AGENTBOX_API_KEYS.

Commands:
  create <label>          generate a labeled API key
  list                    show configured key labels
  revoke <label>          remove a labeled API key`,
		"list": `Usage: agentbox list [-n <limit>] [--json]

List recent Agentbox threads.`,
		"create": `Usage: agentbox create <title> [--json]

Create a new Agentbox thread.`,
		"get": `Usage: agentbox get <thread-id> [--json]

Read an Agentbox thread and its messages.`,
		"download": `Usage: agentbox download <thread-id> [-o <dir>] [--json]

Download all attachments from a thread to a local directory.`,
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
		}
		_ = json.Unmarshal(bytes, &payload)
		if payload.Error != "" {
			return errors.New(payload.Error)
		}
		return fmt.Errorf("Request failed with HTTP %d", res.StatusCode)
	}
	return nil
}

func (r *Runner) runProfiles(args []string, globalProfileName string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		r.printCommandHelp("profiles")
		return nil
	}
	if len(args) == 0 || args[0] == "--json" {
		fs := newFlagSet("profiles")
		jsonOut := fs.Bool("json", false, "print raw JSON")
		if err := parseFlags(fs, args); err != nil {
			return err
		}
		envProfiles, err := profiles.ParseProfilesConfig(os.Getenv("AGENTBOX_PROFILES"))
		if err != nil {
			return err
		}
		store, err := profiles.ReadStore()
		if err != nil {
			return err
		}
		selected := defaultString(globalProfileName, os.Getenv("AGENTBOX_PROFILE"))
		resolved, err := profiles.Resolve(selected)
		if err != nil {
			return err
		}
		source := "none"
		listed := []map[string]string{}
		if len(envProfiles) > 0 {
			source = "env"
			for _, profile := range envProfiles {
				listed = append(listed, map[string]string{"name": profile.Name, "base_url": profile.BaseURL, "source": "env"})
			}
		} else if len(store.Profiles) > 0 {
			source = "config"
			for _, profile := range store.Profiles {
				listed = append(listed, map[string]string{"name": profile.Name, "base_url": profile.BaseURL, "source": "config"})
			}
		} else if (os.Getenv("AGENTBOX_BASE_URL") != "" || os.Getenv("AGENTBOX_URL") != "") && os.Getenv("AGENTBOX_API_KEY") != "" {
			source = "legacy-env"
			baseURL := os.Getenv("AGENTBOX_BASE_URL")
			if baseURL == "" {
				baseURL = os.Getenv("AGENTBOX_URL")
			}
			listed = append(listed, map[string]string{"name": "default", "base_url": strings.TrimRight(baseURL, "/"), "source": "legacy-env"})
		}
		var active any
		if resolved != nil {
			active = resolved.Name
		}
		data := map[string]any{
			"source":                source,
			"config_path":           profiles.DefaultConfigPath(),
			"active_profile":        active,
			"stored_active_profile": nullString(store.ActiveProfileName),
			"profiles":              listed,
		}
		if *jsonOut {
			return printJSON(r.Stdout, data)
		}
		if len(listed) == 0 {
			fmt.Fprintln(r.Stdout, `No CLI profiles configured. Add one with "agentbox profiles add" or set AGENTBOX_BASE_URL and AGENTBOX_API_KEY.`)
			fmt.Fprintf(r.Stdout, "Config path: %s\n", profiles.DefaultConfigPath())
			return nil
		}
		fmt.Fprintf(r.Stdout, "Config path: %s\n", profiles.DefaultConfigPath())
		fmt.Fprintf(r.Stdout, "Source: %s\n", source)
		for _, profile := range listed {
			prefix := " "
			if activeName, ok := active.(string); ok && profile["name"] == activeName {
				prefix = "*"
			}
			fmt.Fprintf(r.Stdout, "%s %s\t%s\t%s\n", prefix, profile["name"], profile["base_url"], profile["source"])
		}
		return nil
	}
	subcmd := args[0]
	if len(args) > 1 && isHelpArg(args[1]) {
		r.printProfilesSubcommandHelp(subcmd)
		return nil
	}
	switch subcmd {
	case "add":
		fs := newFlagSet("profiles add")
		baseURL := fs.String("base-url", "", "Agentbox deployment base URL")
		apiKey := fs.String("api-key", "", "Agentbox API key")
		activate := fs.Bool("activate", false, "make this the active stored profile")
		jsonOut := fs.Bool("json", false, "print raw JSON")
		if err := parseFlags(fs, args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 || *baseURL == "" || *apiKey == "" {
			return errors.New("Usage: agentbox profiles add <name> --base-url <url> --api-key <key> [--activate] [--json]")
		}
		name := fs.Arg(0)
		store, err := profiles.SaveProfile(profiles.Profile{Name: name, BaseURL: *baseURL, APIKey: *apiKey}, *activate)
		if err != nil {
			return err
		}
		result := profileStoreResult("saved_profile", name, store)
		if *jsonOut {
			return printJSON(r.Stdout, result)
		}
		fmt.Fprintf(r.Stdout, "Saved profile %q in %s.\n", name, profiles.DefaultConfigPath())
		if store.ActiveProfileName == name {
			fmt.Fprintf(r.Stdout, "Active profile: %s\n", name)
		}
		return nil
	case "remove":
		fs := newFlagSet("profiles remove")
		jsonOut := fs.Bool("json", false, "print raw JSON")
		if err := parseFlags(fs, args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("Usage: agentbox profiles remove <name> [--json]")
		}
		name := fs.Arg(0)
		store, err := profiles.RemoveProfile(name)
		if err != nil {
			return err
		}
		result := profileStoreResult("removed_profile", name, store)
		if *jsonOut {
			return printJSON(r.Stdout, result)
		}
		fmt.Fprintf(r.Stdout, "Removed profile %q.\n", name)
		fmt.Fprintf(r.Stdout, "Active profile: %s\n", defaultString(store.ActiveProfileName, "none"))
		return nil
	case "use":
		fs := newFlagSet("profiles use")
		jsonOut := fs.Bool("json", false, "print raw JSON")
		if err := parseFlags(fs, args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("Usage: agentbox profiles use <name> [--json]")
		}
		store, err := profiles.SetActiveProfile(fs.Arg(0))
		if err != nil {
			return err
		}
		result := map[string]any{"active_profile": store.ActiveProfileName, "config_path": profiles.DefaultConfigPath()}
		if *jsonOut {
			return printJSON(r.Stdout, result)
		}
		fmt.Fprintf(r.Stdout, "Active profile: %s\n", store.ActiveProfileName)
		return nil
	case "show":
		fs := newFlagSet("profiles show")
		jsonOut := fs.Bool("json", false, "print raw JSON")
		if err := parseFlags(fs, args[1:]); err != nil {
			return err
		}
		name := globalProfileName
		if fs.NArg() > 0 {
			name = fs.Arg(0)
		}
		resolved, err := profiles.Resolve(name)
		if err != nil {
			return err
		}
		if resolved == nil {
			return errors.New(`No CLI profile resolved. Add one with "agentbox profiles add" or set AGENTBOX_BASE_URL and AGENTBOX_API_KEY.`)
		}
		result := map[string]any{
			"name":           resolved.Name,
			"base_url":       resolved.BaseURL,
			"api_key_masked": profiles.MaskSecret(resolved.APIKey),
			"source":         resolved.Source,
			"config_path":    profiles.DefaultConfigPath(),
		}
		if *jsonOut {
			return printJSON(r.Stdout, result)
		}
		fmt.Fprintf(r.Stdout, "%s\t%s\t%s\t%s\n", resolved.Name, resolved.BaseURL, resolved.Source, profiles.MaskSecret(resolved.APIKey))
		return nil
	default:
		return fmt.Errorf("Unknown profiles command %q.", subcmd)
	}
}

func (r *Runner) printProfilesSubcommandHelp(command string) {
	usage := map[string]string{
		"add": `Usage: agentbox profiles add <name> --base-url <url> --api-key <key> [--activate] [--json]

Create or update a stored CLI profile.`,
		"remove": `Usage: agentbox profiles remove <name> [--json]

Delete a stored CLI profile.`,
		"use": `Usage: agentbox profiles use <name> [--json]

Switch the active stored CLI profile.`,
		"show": `Usage: agentbox profiles show [name] [--json]

Show the resolved profile for this invocation.`,
	}
	if text, ok := usage[command]; ok {
		fmt.Fprintln(r.Stdout, text)
		return
	}
	r.printCommandHelp("profiles")
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
		return printJSON(r.Stdout, map[string]any{
			"mcp_url": endpoint.String(),
			"profile": cfg.ProfileName,
			"source":  cfg.Source,
		})
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
	}
	return nil
}

func (r *Runner) runCreate(args []string, profileName string) error {
	fs := newFlagSet("create")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox create <title> [--json]")
	}
	payload, _ := json.Marshal(map[string]string{"title": fs.Arg(0)})
	var data struct {
		Thread thread `json:"thread"`
	}
	if err := r.request("/api/threads", http.MethodPost, bytes.NewReader(payload), map[string]string{"content-type": "application/json"}, profileName, &data); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(r.Stdout, data)
	}
	fmt.Fprintf(r.Stdout, "%s\t%s\n", data.Thread.ID, data.Thread.Title)
	return nil
}

func (r *Runner) runGet(args []string, profileName string) error {
	fs := newFlagSet("get")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox get <thread-id> [--json]")
	}
	var data struct {
		Thread thread `json:"thread"`
	}
	if err := r.request("/api/threads/"+url.PathEscape(fs.Arg(0)), http.MethodGet, nil, nil, profileName, &data); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(r.Stdout, data)
	}
	printThread(r.Stdout, data.Thread)
	return nil
}

func (r *Runner) runDownload(args []string, profileName string) error {
	fs := newFlagSet("download")
	output := fs.String("output", "", "directory to save files into")
	fs.StringVar(output, "o", "", "directory to save files into")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox download <thread-id> [-o <dir>] [--json]")
	}
	threadID := fs.Arg(0)
	outputDir := *output
	if outputDir == "" {
		outputDir = filepath.Join("agentbox-downloads", threadID)
	}
	var data struct {
		Thread thread `json:"thread"`
	}
	if err := r.request("/api/threads/"+url.PathEscape(threadID), http.MethodGet, nil, nil, profileName, &data); err != nil {
		return err
	}
	downloads := []map[string]string{}
	for _, message := range data.Thread.Messages {
		for _, asset := range message.Assets {
			outputPath := filepath.Join(outputDir, asset.ID+"-"+asset.FileName)
			if err := r.downloadAsset(asset, outputPath, profileName); err != nil {
				return err
			}
			downloads = append(downloads, map[string]string{
				"message_id":  message.ID,
				"asset_id":    asset.ID,
				"file_name":   asset.FileName,
				"storage_key": asset.StorageKey,
				"output_path": outputPath,
			})
		}
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

func (r *Runner) downloadAsset(asset asset, outputPath string, profileName string) error {
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
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, res.Body)
	return err
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

func printThread(w io.Writer, thread thread) {
	fmt.Fprintf(w, "# %s\n", thread.Title)
	fmt.Fprintf(w, "id: %s\n", thread.ID)
	fmt.Fprintf(w, "updated: %s\n\n", thread.UpdatedAt)
	for _, message := range thread.Messages {
		fmt.Fprintf(w, "--- %s · %s · %s\n", message.Author, message.CreatedAt, message.ID)
		fmt.Fprintln(w, message.Body)
		if len(message.Assets) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Assets:")
			for _, asset := range message.Assets {
				location := asset.StorageKey
				if asset.PublicURL != nil {
					location = *asset.PublicURL
				}
				fmt.Fprintf(w, "- %s %s %s\n", asset.ID, asset.FileName, location)
			}
		}
		fmt.Fprintln(w)
	}
}

func printJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func profileStoreResult(key string, value string, store profiles.Store) map[string]any {
	listed := []map[string]string{}
	for _, profile := range store.Profiles {
		listed = append(listed, map[string]string{"name": profile.Name, "base_url": profile.BaseURL})
	}
	return map[string]any{
		key:              value,
		"active_profile": nullString(store.ActiveProfileName),
		"config_path":    profiles.DefaultConfigPath(),
		"profiles":       listed,
	}
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
