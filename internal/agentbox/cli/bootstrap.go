package cli

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"agentbox/internal/agentbox/auth"
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
	apiKey := fs.String("api-key", "", "Agentbox API key")
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
	if strings.TrimSpace(*apiKey) == "" {
		value, err := promptRequired(reader, r.Stdout, "Agentbox API key")
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

	store, err := profiles.SaveProfile(profiles.Profile{
		Name:    strings.TrimSpace(*profileName),
		BaseURL: strings.TrimSpace(*baseURL),
		APIKey:  strings.TrimSpace(*apiKey),
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
		"api_key_masked":  profiles.MaskSecret(strings.TrimSpace(*apiKey)),
		"resolved_source": "config",
	}
	if *jsonOut {
		if !*skipDoctor {
			result["checks"] = r.doctorChecks(strings.TrimSpace(*profileName))
		}
		return printJSON(r.Stdout, result)
	}

	fmt.Fprintf(r.Stdout, "Saved profile %q in %s.\n", *profileName, profiles.DefaultConfigPath())
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
	fmt.Fprintln(r.Stdout, "ChatGPT setup:")
	for i, step := range steps {
		fmt.Fprintf(r.Stdout, "%d. %s\n", i+1, step)
	}
	return nil
}

func (r *Runner) runDeploy(args []string, globalProfileName string) error {
	if len(args) == 0 || args[0] != "vercel" {
		return errors.New(`Usage: agentbox deploy vercel [options]`)
	}
	return r.runDeployVercel(args[1:], globalProfileName)
}

func (r *Runner) runDeployVercel(args []string, globalProfileName string) error {
	fs := newFlagSet("deploy vercel")
	backendProject := fs.String("backend-project", "agentbox-go", "Vercel backend project name")
	dashboardProject := fs.String("dashboard-project", "agentbox", "Vercel dashboard project name")
	databaseURL := fs.String("database-url", "", "Postgres connection string")
	r2AccountID := fs.String("r2-account-id", "", "Cloudflare R2 account ID")
	r2AccessKeyID := fs.String("r2-access-key-id", "", "Cloudflare R2 access key ID")
	r2SecretAccessKey := fs.String("r2-secret-access-key", "", "Cloudflare R2 secret access key")
	r2Bucket := fs.String("r2-bucket", "", "Cloudflare R2 bucket")
	r2PublicBaseURL := fs.String("r2-public-base-url", "", "optional public base URL for assets")
	allowedOrigins := fs.String("allowed-origins", "", "optional comma-separated allowed origins")
	autoMigrate := fs.String("auto-migrate", "", "optional AGENTBOX_AUTO_MIGRATE value")
	dbPoolSize := fs.String("db-pool-size", "", "optional AGENTBOX_DB_POOL_SIZE value")
	maxFileSizeBytes := fs.String("max-file-size-bytes", "", "optional AGENTBOX_MAX_FILE_SIZE_BYTES value")
	adminKey := fs.String("admin-key", "", "viewer/admin key")
	chatgptLabel := fs.String("chatgpt-label", "chatgpt", "label for the ChatGPT key")
	chatgptAuthor := fs.String("chatgpt-author", "", "author name for the ChatGPT key")
	chatgptKey := fs.String("chatgpt-key", "", "ChatGPT API key value")
	localLabel := fs.String("local-label", defaultString(globalProfileName, "local"), "label for the local key")
	localAuthor := fs.String("local-author", "", "author name for the local key")
	localKey := fs.String("local-key", "", "local API key value")
	profileName := fs.String("profile-name", defaultString(globalProfileName, "prod"), "local CLI profile name to save after deploy")
	skipProfile := fs.Bool("skip-profile", false, "skip saving a local CLI profile")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	reader := bufio.NewReader(r.Stdin)
	requiredValues := []struct {
		label string
		value *string
	}{
		{"DATABASE_URL", databaseURL},
		{"R2_ACCOUNT_ID", r2AccountID},
		{"R2_ACCESS_KEY_ID", r2AccessKeyID},
		{"R2_SECRET_ACCESS_KEY", r2SecretAccessKey},
		{"R2_BUCKET", r2Bucket},
		{"AGENTBOX_ADMIN_KEY", adminKey},
	}
	for _, item := range requiredValues {
		if strings.TrimSpace(*item.value) == "" {
			value, err := promptRequired(reader, r.Stdout, item.label)
			if err != nil {
				return err
			}
			*item.value = value
		}
	}
	if strings.TrimSpace(*chatgptKey) == "" {
		*chatgptKey = mustGenerateSecret()
	}
	if strings.TrimSpace(*localKey) == "" {
		*localKey = mustGenerateSecret()
	}
	if strings.TrimSpace(*chatgptAuthor) == "" {
		*chatgptAuthor = strings.TrimSpace(*chatgptLabel)
	}
	if strings.TrimSpace(*localAuthor) == "" {
		*localAuthor = strings.TrimSpace(*localLabel)
	}

	apiKeys := serializeAPIKeys([]auth.KeyConfig{
		{Name: strings.TrimSpace(*chatgptLabel), Key: strings.TrimSpace(*chatgptKey), Author: strings.TrimSpace(*chatgptAuthor)},
		{Name: strings.TrimSpace(*localLabel), Key: strings.TrimSpace(*localKey), Author: strings.TrimSpace(*localAuthor)},
	})

	backendEnv := map[string]string{
		"DATABASE_URL":         strings.TrimSpace(*databaseURL),
		"AGENTBOX_API_KEYS":    apiKeys,
		"AGENTBOX_ADMIN_KEY":   strings.TrimSpace(*adminKey),
		"R2_ACCOUNT_ID":        strings.TrimSpace(*r2AccountID),
		"R2_ACCESS_KEY_ID":     strings.TrimSpace(*r2AccessKeyID),
		"R2_SECRET_ACCESS_KEY": strings.TrimSpace(*r2SecretAccessKey),
		"R2_BUCKET":            strings.TrimSpace(*r2Bucket),
		"AGENTBOX_ENV":         "production",
	}
	if strings.TrimSpace(*r2PublicBaseURL) != "" {
		backendEnv["R2_PUBLIC_BASE_URL"] = strings.TrimSpace(*r2PublicBaseURL)
	}
	if strings.TrimSpace(*allowedOrigins) != "" {
		backendEnv["AGENTBOX_ALLOWED_ORIGINS"] = strings.TrimSpace(*allowedOrigins)
	}
	if strings.TrimSpace(*autoMigrate) != "" {
		backendEnv["AGENTBOX_AUTO_MIGRATE"] = strings.TrimSpace(*autoMigrate)
	}
	if strings.TrimSpace(*dbPoolSize) != "" {
		backendEnv["AGENTBOX_DB_POOL_SIZE"] = strings.TrimSpace(*dbPoolSize)
	}
	if strings.TrimSpace(*maxFileSizeBytes) != "" {
		backendEnv["AGENTBOX_MAX_FILE_SIZE_BYTES"] = strings.TrimSpace(*maxFileSizeBytes)
	}

	if _, _, err := r.RunExternal("vercel", []string{"link", "--yes", "--project", strings.TrimSpace(*backendProject)}, "", nil); err != nil {
		return fmt.Errorf("vercel backend link failed: %w", err)
	}
	if err := r.replaceVercelEnv(strings.TrimSpace(*backendProject), "production", backendEnv); err != nil {
		return err
	}
	backendStdout, backendStderr, err := r.RunExternal("vercel", []string{"--prod", "--yes", "-A", "deploy/vercel/backend/vercel.json"}, "", nil)
	if err != nil {
		return formatCommandError("backend deploy", err, backendStderr)
	}
	backendURL := firstVercelURL(backendStdout)
	if backendURL == "" {
		value, promptErr := promptRequired(reader, r.Stdout, "Backend deployment URL")
		if promptErr != nil {
			return promptErr
		}
		backendURL = value
	}

	if _, _, err := r.RunExternal("vercel", []string{"link", "--yes", "--project", strings.TrimSpace(*dashboardProject)}, "", nil); err != nil {
		return fmt.Errorf("vercel dashboard link failed: %w", err)
	}
	if err := r.replaceVercelEnv(strings.TrimSpace(*dashboardProject), "production", map[string]string{
		"AGENTBOX_BACKEND_URL": strings.TrimRight(strings.TrimSpace(backendURL), "/"),
	}); err != nil {
		return err
	}
	dashboardStdout, dashboardStderr, err := r.RunExternal("vercel", []string{"--prod", "--yes", "-A", "deploy/vercel/dashboard/vercel.json"}, "", nil)
	if err != nil {
		return formatCommandError("dashboard deploy", err, dashboardStderr)
	}
	dashboardURL := firstVercelURL(dashboardStdout)

	if !*skipProfile {
		if _, err := profiles.SaveProfile(profiles.Profile{
			Name:    strings.TrimSpace(*profileName),
			BaseURL: strings.TrimRight(strings.TrimSpace(defaultString(dashboardURL, backendURL)), "/"),
			APIKey:  strings.TrimSpace(*localKey),
		}, true); err != nil {
			return err
		}
	}

	result := map[string]any{
		"backend_project":  strings.TrimSpace(*backendProject),
		"backend_url":      strings.TrimRight(strings.TrimSpace(backendURL), "/"),
		"dashboard_project": strings.TrimSpace(*dashboardProject),
		"dashboard_url":    strings.TrimRight(strings.TrimSpace(dashboardURL), "/"),
		"api_keys": []map[string]string{
			{"label": strings.TrimSpace(*chatgptLabel), "author": strings.TrimSpace(*chatgptAuthor), "key_masked": profiles.MaskSecret(strings.TrimSpace(*chatgptKey))},
			{"label": strings.TrimSpace(*localLabel), "author": strings.TrimSpace(*localAuthor), "key_masked": profiles.MaskSecret(strings.TrimSpace(*localKey))},
		},
		"profile_name": defaultString(strings.TrimSpace(*profileName), ""),
		"config_path":  profiles.DefaultConfigPath(),
		"migration_command": "bun run db:migrate",
	}
	if *jsonOut {
		return printJSON(r.Stdout, result)
	}
	fmt.Fprintf(r.Stdout, "Backend deployed: %s\n", strings.TrimRight(strings.TrimSpace(backendURL), "/"))
	if strings.TrimSpace(dashboardURL) != "" {
		fmt.Fprintf(r.Stdout, "Dashboard deployed: %s\n", strings.TrimRight(strings.TrimSpace(dashboardURL), "/"))
	}
	fmt.Fprintf(r.Stdout, "Saved local label %q and ChatGPT label %q in AGENTBOX_API_KEYS.\n", strings.TrimSpace(*localLabel), strings.TrimSpace(*chatgptLabel))
	if !*skipProfile {
		fmt.Fprintf(r.Stdout, "Saved local CLI profile %q in %s.\n", strings.TrimSpace(*profileName), profiles.DefaultConfigPath())
	}
	fmt.Fprintln(r.Stdout, "Next steps:")
	fmt.Fprintln(r.Stdout, "1. Run bun run db:migrate with the backend production env loaded.")
	fmt.Fprintf(r.Stdout, "2. Run agentbox --profile %s doctor\n", strings.TrimSpace(*profileName))
	fmt.Fprintf(r.Stdout, "3. Run agentbox --profile %s connect chatgpt\n", strings.TrimSpace(*profileName))
	return nil
}

func (r *Runner) runKeys(args []string) error {
	if len(args) == 0 {
		return errors.New(`Usage: agentbox keys [create|list|revoke]`)
	}
	switch args[0] {
	case "create":
		return r.runKeysCreate(args[1:])
	case "list":
		return r.runKeysList(args[1:])
	case "revoke":
		return r.runKeysRevoke(args[1:])
	default:
		return fmt.Errorf("Unknown keys command %q.", args[0])
	}
}

func (r *Runner) runKeysCreate(args []string) error {
	fs := newFlagSet("keys create")
	author := fs.String("author", "", "message author for this key")
	project := fs.String("project", "", "optional Vercel project to update")
	environment := fs.String("environment", "production", "Vercel environment name")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox keys create <label> [--author <author>] [--project <name>] [--environment production] [--json]")
	}
	label := strings.TrimSpace(fs.Arg(0))
	if label == "" {
		return errors.New("Key label is required.")
	}
	entry := auth.KeyConfig{Name: label, Key: mustGenerateSecret(), Author: defaultString(strings.TrimSpace(*author), label)}
	keys, err := r.loadVercelAPIKeys(strings.TrimSpace(*project), strings.TrimSpace(*environment))
	if err != nil {
		return err
	}
	keys = appendOrReplaceKey(keys, entry)
	if strings.TrimSpace(*project) != "" {
		if err := r.replaceVercelEnv(strings.TrimSpace(*project), strings.TrimSpace(*environment), map[string]string{
			"AGENTBOX_API_KEYS": serializeAPIKeys(keys),
		}); err != nil {
			return err
		}
	}
	result := map[string]any{
		"label":           entry.Name,
		"author":          entry.Author,
		"key":             entry.Key,
		"key_masked":      profiles.MaskSecret(entry.Key),
		"env_value":       serializeAPIKeys(keys),
		"vercel_project":  nullString(strings.TrimSpace(*project)),
		"environment":     strings.TrimSpace(*environment),
	}
	if *jsonOut {
		return printJSON(r.Stdout, result)
	}
	fmt.Fprintf(r.Stdout, "Created labeled key %q for author %q.\n", entry.Name, entry.Author)
	if strings.TrimSpace(*project) != "" {
		fmt.Fprintf(r.Stdout, "Updated AGENTBOX_API_KEYS on Vercel project %q.\n", strings.TrimSpace(*project))
	}
	fmt.Fprintf(r.Stdout, "Secret: %s\n", entry.Key)
	return nil
}

func (r *Runner) runKeysList(args []string) error {
	fs := newFlagSet("keys list")
	project := fs.String("project", "", "optional Vercel project to read")
	environment := fs.String("environment", "production", "Vercel environment name")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	keys, err := r.loadVercelAPIKeys(strings.TrimSpace(*project), strings.TrimSpace(*environment))
	if err != nil {
		return err
	}
	type listedKey struct {
		Label     string `json:"label"`
		Author    string `json:"author"`
		KeyMasked string `json:"key_masked"`
	}
	result := make([]listedKey, 0, len(keys))
	for _, key := range keys {
		result = append(result, listedKey{Label: key.Name, Author: key.Author, KeyMasked: profiles.MaskSecret(key.Key)})
	}
	if *jsonOut {
		return printJSON(r.Stdout, map[string]any{"keys": result})
	}
	if len(result) == 0 {
		fmt.Fprintln(r.Stdout, "No Agentbox API keys found.")
		return nil
	}
	for _, key := range result {
		fmt.Fprintf(r.Stdout, "%s\t%s\t%s\n", key.Label, key.Author, key.KeyMasked)
	}
	return nil
}

func (r *Runner) runKeysRevoke(args []string) error {
	fs := newFlagSet("keys revoke")
	project := fs.String("project", "", "Vercel project to update")
	environment := fs.String("environment", "production", "Vercel environment name")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("Usage: agentbox keys revoke <label> --project <name> [--environment production] [--json]")
	}
	projectName := strings.TrimSpace(*project)
	if projectName == "" {
		return errors.New("keys revoke requires --project so AGENTBOX_API_KEYS can be updated.")
	}
	label := strings.TrimSpace(fs.Arg(0))
	keys, err := r.loadVercelAPIKeys(projectName, strings.TrimSpace(*environment))
	if err != nil {
		return err
	}
	filtered := make([]auth.KeyConfig, 0, len(keys))
	removed := false
	for _, key := range keys {
		if key.Name == label {
			removed = true
			continue
		}
		filtered = append(filtered, key)
	}
	if !removed {
		return fmt.Errorf("Unknown Agentbox key %q.", label)
	}
	if err := r.replaceVercelEnv(projectName, strings.TrimSpace(*environment), map[string]string{
		"AGENTBOX_API_KEYS": serializeAPIKeys(filtered),
	}); err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(r.Stdout, map[string]any{
			"revoked_label": label,
			"remaining":     len(filtered),
			"env_value":     serializeAPIKeys(filtered),
		})
	}
	fmt.Fprintf(r.Stdout, "Revoked labeled key %q on Vercel project %q.\n", label, projectName)
	return nil
}

func (r *Runner) loadVercelAPIKeys(project string, environment string) ([]auth.KeyConfig, error) {
	if project == "" {
		return nil, nil
	}
	if _, _, err := r.RunExternal("vercel", []string{"link", "--yes", "--project", project}, "", nil); err != nil {
		return nil, fmt.Errorf("vercel link failed: %w", err)
	}
	tempDir, err := os.MkdirTemp("", "agentbox-vercel-env-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	envPath := filepath.Join(tempDir, ".env")
	_, stderr, err := r.RunExternal("vercel", []string{"env", "pull", envPath, "--environment", environment}, "", nil)
	if err != nil {
		return nil, formatCommandError("vercel env pull", err, stderr)
	}
	values, err := readDotEnvFile(envPath)
	if err != nil {
		return nil, err
	}
	return auth.ParseAPIKeys(values["AGENTBOX_API_KEYS"])
}

func (r *Runner) replaceVercelEnv(project string, environment string, values map[string]string) error {
	if project == "" {
		return errors.New("Vercel project is required.")
	}
	if environment == "" {
		environment = "production"
	}
	if _, _, err := r.RunExternal("vercel", []string{"link", "--yes", "--project", project}, "", nil); err != nil {
		return fmt.Errorf("vercel link failed: %w", err)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := values[key]
		_, _, _ = r.RunExternal("vercel", []string{"env", "rm", key, environment, "--yes"}, "", nil)
		_, stderr, err := r.RunExternal("vercel", []string{"env", "add", key, environment}, value+"\n", nil)
		if err != nil {
			return formatCommandError("vercel env add "+key, err, stderr)
		}
	}
	return nil
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

func mustGenerateSecret() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}

func appendOrReplaceKey(keys []auth.KeyConfig, entry auth.KeyConfig) []auth.KeyConfig {
	next := make([]auth.KeyConfig, 0, len(keys)+1)
	replaced := false
	for _, key := range keys {
		if key.Name == entry.Name {
			next = append(next, entry)
			replaced = true
			continue
		}
		next = append(next, key)
	}
	if !replaced {
		next = append(next, entry)
	}
	sort.Slice(next, func(i int, j int) bool {
		return next[i].Name < next[j].Name
	})
	return next
}

func serializeAPIKeys(keys []auth.KeyConfig) string {
	if len(keys) == 0 {
		return ""
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(key.Name) == "" || strings.TrimSpace(key.Key) == "" || strings.TrimSpace(key.Author) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%s:%s", strings.TrimSpace(key.Name), strings.TrimSpace(key.Key), strings.TrimSpace(key.Author)))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

var vercelURLPattern = regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.vercel\.app`)

func firstVercelURL(output string) string {
	match := vercelURLPattern.FindString(output)
	return strings.TrimRight(match, "/")
}

func readDotEnvFile(path string) (map[string]string, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(bytes), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"`)
		values[strings.TrimSpace(key)] = value
	}
	return values, nil
}

func formatCommandError(action string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s failed: %w", action, err)
	}
	return fmt.Errorf("%s failed: %s", action, stderr)
}
