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
	"time"

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
	apiKey := fs.String("api-key", "", "existing user API key with keys:write scope")
	adminKey := fs.String("admin-key", "", "deprecated; deployment-wide key management is disabled")
	localKeyName := fs.String("local-key-name", "local", "API key name for this CLI")
	chatgptKeyName := fs.String("chatgpt-key-name", "chatgpt", "API key name for ChatGPT")
	skipDoctor := fs.Bool("skip-doctor", false, "skip the doctor verification step")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*adminKey) != "" {
		return errors.New("--admin-key is no longer supported by init. Sign in first or provide --api-key for an existing user credential with keys:write scope.")
	}

	reader := bufio.NewReader(r.Stdin)
	if strings.TrimSpace(*baseURL) == "" {
		value, err := promptRequired(reader, r.Stdout, "Agentbox base URL")
		if err != nil {
			return err
		}
		*baseURL = value
	}
	if strings.TrimSpace(*apiKey) == "" {
		if value := strings.TrimSpace(os.Getenv("AGENTBOX_API_KEY")); value != "" {
			*apiKey = value
		}
	}
	if strings.TrimSpace(*apiKey) == "" {
		value, err := promptRequired(reader, r.Stdout, "Existing user API key")
		if err != nil {
			return err
		}
		*apiKey = value
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

	localKey, err := r.createUserAPIKey(strings.TrimSpace(*baseURL), strings.TrimSpace(*apiKey), strings.TrimSpace(*localKeyName), "local")
	if err != nil {
		return err
	}
	chatgptKey, err := r.createUserAPIKey(strings.TrimSpace(*baseURL), strings.TrimSpace(*apiKey), strings.TrimSpace(*chatgptKeyName), "chatgpt")
	if err != nil {
		return err
	}

	store, err := profiles.SaveProfile(profiles.Profile{
		Name:     strings.TrimSpace(*profileName),
		BaseURL:  strings.TrimSpace(*baseURL),
		APIKey:   localKey.Secret,
		UserID:   localKey.UserID,
		KeyName:  localKey.Name,
		AuthType: "api_key",
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
	key, err := r.createTenantAPIKeyForProfile(profileName, "chatgpt", "chatgpt")
	if err != nil {
		return err
	}
	cfg, err := r.runtimeConfig(profileName)
	if err != nil {
		return err
	}
	endpoint, err := endpointWithKey(cfg.BaseURL, "/api/mcp", key.Secret)
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
			"profile":        cfg.ProfileName,
			"source":         cfg.Source,
			"key_name":       key.Name,
			"key_masked":     key.KeyMasked,
			"mcp_url":        endpoint.String(),
			"mcp_url_masked": profiles.SanitizeURL(endpoint.String()),
			"steps":          steps,
		})
	}
	fmt.Fprintf(r.Stdout, "Profile: %s (%s)\n", cfg.ProfileName, cfg.Source)

	fmt.Fprintf(r.Stdout, "Created ChatGPT API key %q. Store this secret now: %s\n", key.Name, key.Secret)
	fmt.Fprintf(r.Stdout, "MCP URL: %s\n\n", endpoint.String())
	printNumberedSteps(r.Stdout, "ChatGPT setup:", steps)
	return nil
}

func (r *Runner) runOwner(args []string, profileName string) error {
	if len(args) == 0 || args[0] != "setup-token" {
		return errors.New("Usage: agentbox owner setup-token [--base-url <url>] [--app-url <url>] [--admin-key <key>] [--expires 30m] [--json]")
	}
	fs := newFlagSet("owner setup-token")
	baseURL := fs.String("base-url", "", "Agentbox backend URL")
	appURL := fs.String("app-url", "", "dashboard URL used when the backend returns a relative setup path")
	adminKey := fs.String("admin-key", "", "Agentbox deployment admin key")
	expires := fs.Duration("expires", 30*time.Minute, "one-time token lifetime, up to 24h")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("Usage: agentbox owner setup-token [options]")
	}
	if *expires <= 0 || *expires > 24*time.Hour {
		return errors.New("--expires must be greater than zero and no more than 24h")
	}
	resolvedBaseURL, resolvedAdminKey, err := r.adminConnection(profileName, strings.TrimSpace(*baseURL), strings.TrimSpace(*adminKey))
	if err != nil {
		return err
	}
	minutes := int((*expires + time.Minute - time.Nanosecond) / time.Minute)
	payload, _ := json.Marshal(map[string]int{"expires_in_minutes": minutes})
	var result struct {
		Token     string `json:"token"`
		Purpose   string `json:"purpose"`
		ExpiresAt string `json:"expires_at"`
		SetupURL  string `json:"setup_url"`
	}
	if err := r.adminRequest(resolvedBaseURL, resolvedAdminKey, "/api/admin/owner/setup-token", http.MethodPost, bytes.NewReader(payload), &result); err != nil {
		return err
	}
	if strings.HasPrefix(result.SetupURL, "/") {
		setupBaseURL := strings.TrimSpace(*appURL)
		if setupBaseURL == "" {
			setupBaseURL = resolvedBaseURL
		}
		result.SetupURL = strings.TrimRight(setupBaseURL, "/") + result.SetupURL
	}
	if *jsonOut {
		return printJSON(r.Stdout, result)
	}
	fmt.Fprintf(r.Stdout, "Issued owner %s token.\n", result.Purpose)
	fmt.Fprintf(r.Stdout, "Expires: %s\n", result.ExpiresAt)
	fmt.Fprintf(r.Stdout, "Setup URL: %s\n", result.SetupURL)
	fmt.Fprintln(r.Stdout, "Open this URL once in a trusted browser. It contains the one-time token, not the deployment secret.")
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
		"printf 'https://YOUR-DASHBOARD.vercel.app' | vercel env add APP_PUBLIC_URL production",
		"vercel env add R2_ACCOUNT_ID production",
		"vercel env add R2_ACCESS_KEY_ID production",
		"vercel env add R2_SECRET_ACCESS_KEY production",
		"vercel env add R2_BUCKET production",
		"vercel env add AGENTBOX_ENV production",
		"vercel --prod --yes -A deploy/vercel/backend/vercel.json",
		"bun run db:migrate",
		"vercel link --yes --project agentbox",
		"printf 'https://YOUR-BACKEND.vercel.app' | vercel env add AGENTBOX_BACKEND_URL production",
		"vercel --prod --yes -A deploy/vercel/dashboard/vercel.json",
		"agentbox owner setup-token --base-url https://YOUR-BACKEND.vercel.app --app-url https://YOUR-DASHBOARD.vercel.app --admin-key \"$AGENTBOX_ADMIN_KEY\" --expires 30m",
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
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Purpose   string `json:"purpose"`
	Secret    string `json:"key"`
	KeyMasked string `json:"key_masked"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type remoteTenant struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type remoteUser struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type remoteProvisionTenantResult struct {
	Tenant     remoteTenant `json:"tenant"`
	User       remoteUser   `json:"user"`
	APIKey     remoteAPIKey `json:"api_key"`
	SetupToken string       `json:"setup_token"`
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
	adminKey := fs.String("admin-key", "", "deprecated; use an authenticated user profile")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox keys create <name> [--json]")
	}
	if strings.TrimSpace(*adminKey) != "" || strings.TrimSpace(*baseURL) != "" {
		return errors.New("--admin-key and --base-url are no longer supported for keys commands. Use an authenticated user profile.")
	}
	name := strings.TrimSpace(fs.Arg(0))
	if strings.EqualFold(name, "raycast") {
		return r.printRaycastKey(profileName, *jsonOut)
	}
	key, err := r.createTenantAPIKeyForProfile(profileName, name, "custom")
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

func (r *Runner) runRaycastKey(args []string, profileName string) error {
	fs := newFlagSet("raycast-key")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("Usage: agentbox raycast-key [--json]")
	}
	return r.printRaycastKey(profileName, *jsonOut)
}

func (r *Runner) printRaycastKey(profileName string, jsonOut bool) error {
	cfg, err := r.runtimeConfig(profileName)
	if err != nil {
		return err
	}
	key, err := r.createTenantAPIKeyForProfile(profileName, "raycast", "raycast")
	if err != nil {
		return err
	}
	result := map[string]any{
		"profile":             cfg.ProfileName,
		"source":              cfg.Source,
		"key_name":            key.Name,
		"key":                 key.Secret,
		"key_masked":          key.KeyMasked,
		"raycast_base_url":    strings.TrimRight(cfg.BaseURL, "/"),
		"raycast_api_key":     key.Secret,
		"preference_base_url": "Agentbox URL",
		"preference_api_key":  "Agentbox API Key",
	}
	if jsonOut {
		return printJSON(r.Stdout, result)
	}
	fmt.Fprintf(r.Stdout, "Created Raycast API key %q.\n", key.Name)

	fmt.Fprintln(r.Stdout, "Raycast preferences:")
	fmt.Fprintf(r.Stdout, "Agentbox URL: %s\n", strings.TrimRight(cfg.BaseURL, "/"))
	fmt.Fprintf(r.Stdout, "Agentbox API Key: %s\n", key.Secret)
	fmt.Fprintln(r.Stdout, "Store this secret now; it is shown only in this response.")
	return nil
}

func (r *Runner) runKeysList(args []string, profileName string) error {
	fs := newFlagSet("keys list")
	baseURL := fs.String("base-url", "", "Agentbox backend URL")
	adminKey := fs.String("admin-key", "", "deprecated; use an authenticated user profile")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*adminKey) != "" || strings.TrimSpace(*baseURL) != "" {
		return errors.New("--admin-key and --base-url are no longer supported for keys commands. Use an authenticated user profile.")
	}
	var data struct {
		Keys []remoteAPIKey `json:"keys"`
	}
	if err := r.request("/api/keys", http.MethodGet, nil, nil, profileName, &data); err != nil {
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
	adminKey := fs.String("admin-key", "", "deprecated; use an authenticated user profile")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox keys revoke <name> [--json]")
	}
	if strings.TrimSpace(*adminKey) != "" || strings.TrimSpace(*baseURL) != "" {
		return errors.New("--admin-key and --base-url are no longer supported for keys commands. Use an authenticated user profile.")
	}
	name := strings.TrimSpace(fs.Arg(0))
	var data struct {
		Revoked string `json:"revoked"`
	}
	if err := r.request("/api/keys/"+url.PathEscape(name), http.MethodDelete, nil, nil, profileName, &data); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(r.Stdout, data)
	}
	fmt.Fprintf(r.Stdout, "Revoked API key %q.\n", data.Revoked)
	return nil
}

func (r *Runner) createTenantAPIKeyForProfile(profileName string, name string, purpose string) (remoteAPIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return remoteAPIKey{}, errors.New("API key name is required.")
	}
	payload := map[string]any{"name": name}
	if strings.TrimSpace(purpose) != "" {
		payload["purpose"] = strings.TrimSpace(purpose)
	}
	payloadBytes, _ := json.Marshal(payload)
	var data struct {
		Key remoteAPIKey `json:"key"`
	}
	if err := r.request("/api/keys", http.MethodPost, bytes.NewReader(payloadBytes), map[string]string{"content-type": "application/json"}, profileName, &data); err != nil {
		return remoteAPIKey{}, err
	}
	return data.Key, nil
}

func (r *Runner) createUserAPIKey(baseURL string, apiKey string, name string, purpose string) (remoteAPIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return remoteAPIKey{}, errors.New("API key name is required.")
	}
	if strings.TrimSpace(apiKey) == "" {
		return remoteAPIKey{}, errors.New("An existing user API key is required.")
	}
	payload, _ := json.Marshal(map[string]string{
		"name":    name,
		"purpose": strings.TrimSpace(purpose),
	})
	endpoint, err := url.JoinPath(strings.TrimRight(baseURL, "/"), "/api/keys")
	if err != nil {
		return remoteAPIKey{}, err
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return remoteAPIKey{}, err
	}
	request.Header.Set("authorization", "Bearer "+strings.TrimSpace(apiKey))
	request.Header.Set("content-type", "application/json")
	response, err := r.HTTPClient.Do(request)
	if err != nil {
		return remoteAPIKey{}, err
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		return remoteAPIKey{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(contents, &failure)
		if failure.Error != "" {
			if failure.Code != "" {
				return remoteAPIKey{}, fmt.Errorf("%s: %s", failure.Code, failure.Error)
			}
			return remoteAPIKey{}, errors.New(failure.Error)
		}
		return remoteAPIKey{}, fmt.Errorf("Request failed with HTTP %d", response.StatusCode)
	}
	var data struct {
		Key remoteAPIKey `json:"key"`
	}
	if err := json.Unmarshal(contents, &data); err != nil {
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
		"create": `Usage: agentbox keys create <name> [--json]

Create or rotate a named credential for the signed-in profile's user. Use "raycast" to print Raycast preference values.`,
		"list": `Usage: agentbox keys list [--json]

List credential names and masked values for the signed-in profile's user.`,
		"revoke": `Usage: agentbox keys revoke <name> [--json]

Revoke a credential by name for the signed-in profile's user.`,
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
