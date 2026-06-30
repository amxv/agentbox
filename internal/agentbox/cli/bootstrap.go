package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"agentbox/internal/agentbox/profiles"
)

type ExternalCommandFunc func(name string, args []string, stdin string, env map[string]string) (stdout string, stderr string, err error)

func defaultExternalCommand(name string, args []string, stdin string, env map[string]string) (string, string, error) {
	cmd := exec.Command(name, args...)
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for key, value := range env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func (r *Runner) runInit(args []string, globalProfileName string) error {
	fs := newFlagSet("init")
	profileName := fs.String("profile-name", defaultString(globalProfileName, "local"), "stored profile name")
	baseURL := fs.String("base-url", "", "Agentbox base URL")
	adminKey := fs.String("admin-key", "", "Agentbox admin API key")
	localKeyName := fs.String("local-key-name", "local", "API key name for this CLI")
	chatgptKeyName := fs.String("chatgpt-key-name", "chatgpt", "API key name for ChatGPT")
	skipDoctor := fs.Bool("skip-doctor", false, "skip the doctor verification step")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	reader := bufio.NewReader(r.Stdin)
	if strings.TrimSpace(*baseURL) == "" {
		value, err := promptRequired(reader, r.Stdout, "Agentbox base URL")
		if err != nil {
			return err
		}
		*baseURL = value
	}
	if strings.TrimSpace(*adminKey) == "" {
		if value := strings.TrimSpace(os.Getenv("AGENTBOX_ADMIN_KEY")); value != "" {
			*adminKey = value
		}
	}
	if strings.TrimSpace(*adminKey) == "" {
		value, err := promptRequired(reader, r.Stdout, "Agentbox admin API key")
		if err != nil {
			return err
		}
		*adminKey = value
	}
	if strings.TrimSpace(*profileName) == "" {
		value, err := promptWithDefault(reader, r.Stdout, "Profile name", "local")
		if err != nil {
			return err
		}
		*profileName = value
	}
	if strings.TrimSpace(*localKeyName) == "" {
		value, err := promptWithDefault(reader, r.Stdout, "Local API key name", "local")
		if err != nil {
			return err
		}
		*localKeyName = value
	}
	if strings.TrimSpace(*chatgptKeyName) == "" {
		value, err := promptWithDefault(reader, r.Stdout, "ChatGPT API key name", "chatgpt")
		if err != nil {
			return err
		}
		*chatgptKeyName = value
	}

	localKey, err := r.createRemoteAPIKey(strings.TrimSpace(*baseURL), strings.TrimSpace(*adminKey), strings.TrimSpace(*localKeyName))
	if err != nil {
		return err
	}
	chatgptKey, err := r.createRemoteAPIKey(strings.TrimSpace(*baseURL), strings.TrimSpace(*adminKey), strings.TrimSpace(*chatgptKeyName))
	if err != nil {
		return err
	}

	store, err := profiles.SaveProfile(profiles.Profile{
		Name:    strings.TrimSpace(*profileName),
		BaseURL: strings.TrimSpace(*baseURL),
		APIKey:  localKey.Secret,
	}, true)
	if err != nil {
		return err
	}

	result := map[string]any{
		"profile":         strings.TrimSpace(*profileName),
		"base_url":        strings.TrimRight(strings.TrimSpace(*baseURL), "/"),
		"config_path":     profiles.DefaultConfigPath(),
		"active_profile":  nullString(store.ActiveProfileName),
		"doctor_skipped":  *skipDoctor,
		"api_key_name":    localKey.Name,
		"api_key_masked":  localKey.KeyMasked,
		"chatgpt_key":     chatgptKey.Secret,
		"chatgpt_name":    chatgptKey.Name,
		"resolved_source": "config",
		"mcp_url":         strings.TrimRight(strings.TrimSpace(*baseURL), "/") + "/api/mcp?key=" + url.QueryEscape(chatgptKey.Secret),
	}
	if *jsonOut {
		if !*skipDoctor {
			result["checks"] = r.doctorChecks(strings.TrimSpace(*profileName))
		}
		return printJSON(r.Stdout, result)
	}

	fmt.Fprintf(r.Stdout, "Saved profile %q in %s.\n", *profileName, profiles.DefaultConfigPath())
	fmt.Fprintf(r.Stdout, "Created local API key %q and saved it to the profile.\n", localKey.Name)
	fmt.Fprintf(r.Stdout, "Created ChatGPT API key %q. Store this secret now: %s\n", chatgptKey.Name, chatgptKey.Secret)
	fmt.Fprintf(r.Stdout, "ChatGPT MCP URL: %s\n\n", result["mcp_url"])
	printChatGPTSteps(r.Stdout)
	if !*skipDoctor {
		if err := r.runDoctor(nil, strings.TrimSpace(*profileName)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) runConnect(args []string, profileName string) error {
	if len(args) == 0 || args[0] != "chatgpt" {
		return errors.New(`Usage: agentbox connect chatgpt [--json]`)
	}
	fs := newFlagSet("connect chatgpt")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args[1:]); err != nil {
		return err
	}
	endpoint, err := r.endpoint("/api/mcp", profileName)
	if err != nil {
		return err
	}
	cfg, err := r.runtimeConfig(profileName)
	if err != nil {
		return err
	}
	steps := []string{
		"Open ChatGPT.",
		"Go to Apps -> Advanced settings.",
		"Turn on developer mode.",
		"Choose Create app.",
		"Select no auth.",
		"Paste the MCP URL.",
	}
	if *jsonOut {
		return printJSON(r.Stdout, map[string]any{
			"profile": cfg.ProfileName,
			"source":  cfg.Source,
			"mcp_url": endpoint.String(),
			"steps":   steps,
		})
	}
	fmt.Fprintf(r.Stdout, "Profile: %s (%s)\n", cfg.ProfileName, cfg.Source)
	fmt.Fprintf(r.Stdout, "MCP URL: %s\n\n", endpoint.String())
	printNumberedSteps(r.Stdout, "ChatGPT setup:", steps)
	return nil
}

func (r *Runner) runDeploy(args []string, globalProfileName string) error {
	if len(args) == 0 || args[0] != "vercel" {
		return errors.New(`Usage: agentbox deploy vercel`)
	}
	if len(args) > 1 && isHelpArg(args[1]) {
		r.printCommandHelp("deploy")
		return nil
	}
	return r.runDeployVercel(args[1:], globalProfileName)
}

func (r *Runner) runDeployVercel(args []string, globalProfileName string) error {
	_ = globalProfileName
	fs := newFlagSet("deploy vercel")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("Usage: agentbox deploy vercel")
	}
	commands := []string{
		"vercel link --yes --project agentbox-go",
		"vercel env add DATABASE_URL production",
		"vercel env add AGENTBOX_ADMIN_KEY production",
		"vercel env add R2_ACCOUNT_ID production",
		"vercel env add R2_ACCESS_KEY_ID production",
		"vercel env add R2_SECRET_ACCESS_KEY production",
		"vercel env add R2_BUCKET production",
		"vercel env add AGENTBOX_ENV production",
		"vercel --prod --yes -A deploy/vercel/backend/vercel.json",
		"bun run db:migrate",
		"agentbox init --base-url https://YOUR-BACKEND.vercel.app --admin-key \"$AGENTBOX_ADMIN_KEY\"",
		"vercel link --yes --project agentbox",
		"printf 'https://YOUR-BACKEND.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production",
		"vercel --prod --yes -A deploy/vercel/dashboard/vercel.json",
	}
	if *jsonOut {
		return printJSON(r.Stdout, map[string]any{"commands": commands})
	}
	fmt.Fprintln(r.Stdout, "Vercel deployment guide:")
	for _, command := range commands {
		fmt.Fprintf(r.Stdout, "  %s\n", command)
	}
	fmt.Fprintln(r.Stdout, "\nThe Go backend is required. The Next.js dashboard is optional and deploys separately.")
	return nil
}

type remoteAPIKey struct {
	Name      string `json:"name"`
	Secret    string `json:"key"`
	KeyMasked string `json:"key_masked"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (r *Runner) runKeys(args []string, profileName string) error {
	if len(args) == 0 {
		return errors.New(`Usage: agentbox keys [create|list|revoke]`)
	}
	if isHelpArg(args[0]) {
		r.printCommandHelp("keys")
		return nil
	}
	if len(args) > 1 && isHelpArg(args[1]) {
		r.printKeysSubcommandHelp(args[0])
		return nil
	}
	switch args[0] {
	case "create":
		return r.runKeysCreate(args[1:], profileName)
	case "list":
		return r.runKeysList(args[1:], profileName)
	case "revoke":
		return r.runKeysRevoke(args[1:], profileName)
	default:
		return fmt.Errorf("Unknown keys command %q.", args[0])
	}
}

func (r *Runner) runKeysCreate(args []string, profileName string) error {
	fs := newFlagSet("keys create")
	baseURL := fs.String("base-url", "", "Agentbox backend URL")
	adminKey := fs.String("admin-key", "", "Agentbox admin API key")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox keys create <name> [--base-url <url>] [--admin-key <key>] [--json]")
	}
	key, err := r.createRemoteAPIKeyForProfile(profileName, strings.TrimSpace(*baseURL), strings.TrimSpace(*adminKey), strings.TrimSpace(fs.Arg(0)))
	if err != nil {
		return err
	}
	result := map[string]any{
		"name":       key.Name,
		"key":        key.Secret,
		"key_masked": key.KeyMasked,
	}
	if *jsonOut {
		return printJSON(r.Stdout, result)
	}
	fmt.Fprintf(r.Stdout, "Created API key %q.\n", key.Name)
	fmt.Fprintf(r.Stdout, "Secret: %s\n", key.Secret)
	fmt.Fprintln(r.Stdout, "Store this secret now; it is shown only in this response.")
	return nil
}

func (r *Runner) runKeysList(args []string, profileName string) error {
	fs := newFlagSet("keys list")
	baseURL := fs.String("base-url", "", "Agentbox backend URL")
	adminKey := fs.String("admin-key", "", "Agentbox admin API key")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	resolvedBaseURL, resolvedAdminKey, err := r.adminConnection(profileName, strings.TrimSpace(*baseURL), strings.TrimSpace(*adminKey))
	if err != nil {
		return err
	}
	var data struct {
		Keys []remoteAPIKey `json:"keys"`
	}
	if err := r.adminRequest(resolvedBaseURL, resolvedAdminKey, "/api/admin/keys", http.MethodGet, nil, &data); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(r.Stdout, data)
	}
	if len(data.Keys) == 0 {
		fmt.Fprintln(r.Stdout, "No Agentbox API keys found.")
		return nil
	}
	for _, key := range data.Keys {
		fmt.Fprintf(r.Stdout, "%s\t%s\t%s\n", key.Name, key.KeyMasked, key.UpdatedAt)
	}
	return nil
}

func (r *Runner) runKeysRevoke(args []string, profileName string) error {
	fs := newFlagSet("keys revoke")
	baseURL := fs.String("base-url", "", "Agentbox backend URL")
	adminKey := fs.String("admin-key", "", "Agentbox admin API key")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox keys revoke <name> [--base-url <url>] [--admin-key <key>] [--json]")
	}
	name := strings.TrimSpace(fs.Arg(0))
	resolvedBaseURL, resolvedAdminKey, err := r.adminConnection(profileName, strings.TrimSpace(*baseURL), strings.TrimSpace(*adminKey))
	if err != nil {
		return err
	}
	var data struct {
		Revoked string `json:"revoked"`
	}
	if err := r.adminRequest(resolvedBaseURL, resolvedAdminKey, "/api/admin/keys/"+url.PathEscape(name), http.MethodDelete, nil, &data); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(r.Stdout, data)
	}
	fmt.Fprintf(r.Stdout, "Revoked API key %q.\n", data.Revoked)
	return nil
}

func (r *Runner) createRemoteAPIKeyForProfile(profileName string, explicitBaseURL string, explicitAdminKey string, name string) (remoteAPIKey, error) {
	baseURL, adminKey, err := r.adminConnection(profileName, explicitBaseURL, explicitAdminKey)
	if err != nil {
		return remoteAPIKey{}, err
	}
	return r.createRemoteAPIKey(baseURL, adminKey, name)
}

func (r *Runner) createRemoteAPIKey(baseURL string, adminKey string, name string) (remoteAPIKey, error) {
	if strings.TrimSpace(name) == "" {
		return remoteAPIKey{}, errors.New("API key name is required.")
	}
	payload, _ := json.Marshal(map[string]string{"name": strings.TrimSpace(name)})
	var data struct {
		Key remoteAPIKey `json:"key"`
	}
	if err := r.adminRequest(baseURL, adminKey, "/api/admin/keys", http.MethodPost, bytes.NewReader(payload), &data); err != nil {
		return remoteAPIKey{}, err
	}
	return data.Key, nil
}

func (r *Runner) adminConnection(profileName string, explicitBaseURL string, explicitAdminKey string) (string, string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(explicitBaseURL), "/")
	if baseURL == "" {
		if value := strings.TrimSpace(os.Getenv("AGENTBOX_BASE_URL")); value != "" {
			baseURL = strings.TrimRight(value, "/")
		} else if value := strings.TrimSpace(os.Getenv("AGENTBOX_URL")); value != "" {
			baseURL = strings.TrimRight(value, "/")
		}
	}
	if baseURL == "" {
		resolved, err := profiles.Resolve(profileName)
		if err != nil {
			return "", "", err
		}
		if resolved != nil {
			baseURL = strings.TrimRight(resolved.BaseURL, "/")
		}
	}
	if baseURL == "" {
		return "", "", fmt.Errorf("Set --base-url, AGENTBOX_BASE_URL, or configure a profile in %s.", profiles.DefaultConfigPath())
	}
	adminKey := strings.TrimSpace(explicitAdminKey)
	if adminKey == "" {
		adminKey = strings.TrimSpace(os.Getenv("AGENTBOX_ADMIN_KEY"))
	}
	if adminKey == "" {
		return "", "", errors.New("Set --admin-key or AGENTBOX_ADMIN_KEY to use the admin API.")
	}
	return baseURL, adminKey, nil
}

func (r *Runner) adminRequest(baseURL string, adminKey string, path string, method string, body io.Reader, target any) error {
	endpoint, err := url.JoinPath(strings.TrimRight(baseURL, "/"), path)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("x-agentbox-admin-key", adminKey)
	if body != nil {
		req.Header.Set("content-type", "application/json")
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
	if len(bytes) > 0 && target != nil {
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

func (r *Runner) printKeysSubcommandHelp(command string) {
	usage := map[string]string{
		"create": `Usage: agentbox keys create <name> [--base-url <url>] [--admin-key <key>] [--json]

Create or replace a named DB-backed API key through the backend admin API. The secret is printed once.`,
		"list": `Usage: agentbox keys list [--base-url <url>] [--admin-key <key>] [--json]

List DB-backed API key names and masked key values through the backend admin API.`,
		"revoke": `Usage: agentbox keys revoke <name> [--base-url <url>] [--admin-key <key>] [--json]

Revoke a DB-backed API key by name through the backend admin API.`,
	}
	if text, ok := usage[command]; ok {
		fmt.Fprintln(r.Stdout, text)
		return
	}
	r.printCommandHelp("keys")
}

func printChatGPTSteps(output io.Writer) {
	steps := []string{
		"Open ChatGPT.",
		"Go to Apps -> Advanced settings.",
		"Turn on developer mode.",
		"Choose Create app.",
		"Select no auth.",
		"Paste the MCP URL.",
	}
	printNumberedSteps(output, "ChatGPT setup:", steps)
}

func printNumberedSteps(output io.Writer, title string, steps []string) {
	fmt.Fprintln(output, title)
	for i, step := range steps {
		fmt.Fprintf(output, "%d. %s\n", i+1, step)
	}
}

func promptRequired(reader *bufio.Reader, output io.Writer, label string) (string, error) {
	for {
		value, err := promptWithDefault(reader, output, label, "")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}
}

func promptWithDefault(reader *bufio.Reader, output io.Writer, label string, fallback string) (string, error) {
	prompt := label
	if fallback != "" {
		prompt += " [" + fallback + "]"
	}
	prompt += ": "
	if _, err := fmt.Fprint(output, prompt); err != nil {
		return "", err
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return fallback, nil
	}
	return value, nil
}
