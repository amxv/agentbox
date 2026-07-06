package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"agentbox/internal/agentbox/profiles"
)

type cliLoginExchangeResponse struct {
	APIKey struct {
		Name   string `json:"name"`
		Secret string `json:"key"`
		Masked string `json:"key_masked"`
	} `json:"api_key"`
	Tenant struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"tenant"`
	User struct {
		ID          string `json:"id"`
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
	} `json:"user"`
	AuthType string `json:"auth_type"`
}

type cliCallbackResult struct {
	Code  string
	State string
	Error string
}

func (r *Runner) runLogin(args []string, globalProfileName string) error {
	fs := newFlagSet("login")
	baseURL := fs.String("base-url", "", "Agentbox dashboard/base URL")
	profileName := fs.String("profile-name", defaultString(globalProfileName, "local"), "stored profile name")
	keyName := fs.String("key-name", "", "tenant API key name for this CLI")
	noOpen := fs.Bool("no-open", false, "print the login URL instead of opening a browser")
	timeoutSeconds := fs.Int("timeout", 180, "seconds to wait for browser login")
	jsonOut := fs.Bool("json", false, "print raw JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("Usage: agentbox login [--base-url <url>] [--profile-name <name>] [--key-name <name>] [--no-open] [--timeout <seconds>] [--json]")
	}
	resolvedBaseURL := strings.TrimSpace(*baseURL)
	if resolvedBaseURL == "" {
		if existing, err := profiles.Resolve(globalProfileName); err == nil && existing != nil {
			resolvedBaseURL = existing.BaseURL
		}
	}
	if resolvedBaseURL == "" {
		resolvedBaseURL = strings.TrimSpace(osEnvFirst("AGENTBOX_BASE_URL", "AGENTBOX_URL"))
	}
	if resolvedBaseURL == "" {
		return errors.New("Set --base-url or configure an existing profile with a base URL.")
	}
	resolvedBaseURL = strings.TrimRight(resolvedBaseURL, "/")
	profile := strings.TrimSpace(*profileName)
	if profile == "" {
		profile = "local"
	}
	name := strings.TrimSpace(*keyName)
	if name == "" {
		name = defaultLoginKeyName()
	}
	state, err := randomURLToken(32)
	if err != nil {
		return err
	}
	callback, stop, err := startLoginCallbackServer()
	if err != nil {
		return err
	}
	defer stop()

	loginURL, err := buildLoginURL(resolvedBaseURL, state, callback.RedirectURI)
	if err != nil {
		return err
	}
	if *jsonOut {
		fmt.Fprintf(r.Stderr, "Open this URL to sign in: %s\n", loginURL)
	} else {
		fmt.Fprintf(r.Stdout, "Open this URL to sign in: %s\n", loginURL)
	}
	if !*noOpen {
		if err := r.openBrowser(loginURL); err != nil {
			fmt.Fprintf(r.Stderr, "Could not open browser automatically: %v\n", err)
		}
	}
	timeout := time.Duration(*timeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	result, err := callback.Wait(timeout)
	if err != nil {
		return err
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	if result.State != state {
		return errors.New("CLI login state did not match.")
	}
	exchanged, err := r.exchangeCLILoginCode(resolvedBaseURL, result.Code, state, callback.RedirectURI, name)
	if err != nil {
		return err
	}
	if exchanged.APIKey.Secret == "" {
		return errors.New("CLI login exchange did not return an API key.")
	}
	store, err := profiles.SaveProfile(profiles.Profile{
		Name:       profile,
		BaseURL:    resolvedBaseURL,
		APIKey:     exchanged.APIKey.Secret,
		TenantID:   exchanged.Tenant.ID,
		TenantSlug: exchanged.Tenant.Slug,
		TenantName: exchanged.Tenant.Name,
		UserID:     exchanged.User.ID,
		KeyName:    exchanged.APIKey.Name,
		AuthType:   defaultString(exchanged.AuthType, "api_key"),
	}, true)
	if err != nil {
		return err
	}
	output := map[string]any{
		"profile":        profile,
		"base_url":       resolvedBaseURL,
		"config_path":    profiles.DefaultConfigPath(),
		"active_profile": nullString(store.ActiveProfileName),
		"tenant":         exchanged.Tenant,
		"user":           exchanged.User,
		"api_key_name":   exchanged.APIKey.Name,
		"api_key_masked": exchanged.APIKey.Masked,
		"auth_type":      defaultString(exchanged.AuthType, "api_key"),
	}
	if *jsonOut {
		return printJSON(r.Stdout, output)
	}
	fmt.Fprintf(r.Stdout, "Saved profile %q in %s.\n", profile, profiles.DefaultConfigPath())
	fmt.Fprintf(r.Stdout, "Tenant: %s (%s)\n", exchanged.Tenant.Name, exchanged.Tenant.ID)
	userLabel := exchanged.User.Email
	if strings.TrimSpace(userLabel) == "" {
		userLabel = exchanged.User.ID
	}
	fmt.Fprintf(r.Stdout, "User: %s\n", userLabel)
	fmt.Fprintf(r.Stdout, "API key: %s %s\n", exchanged.APIKey.Name, exchanged.APIKey.Masked)
	return nil
}

type loginCallbackServer struct {
	RedirectURI string
	result      chan cliCallbackResult
	server      *http.Server
}

func startLoginCallbackServer() (*loginCallbackServer, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	callback := &loginCallbackServer{
		RedirectURI: "http://" + listener.Addr().String() + "/callback",
		result:      make(chan cliCallbackResult, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, req *http.Request) {
		result := cliCallbackResult{
			Code:  strings.TrimSpace(req.URL.Query().Get("code")),
			State: strings.TrimSpace(req.URL.Query().Get("state")),
			Error: strings.TrimSpace(req.URL.Query().Get("error")),
		}
		select {
		case callback.result <- result:
		default:
		}
		w.Header().Set("content-type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>Agentbox CLI login</title><p>You can return to the terminal.</p>")
	})
	callback.server = &http.Server{Handler: mux}
	go func() {
		_ = callback.server.Serve(listener)
	}()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = callback.server.Shutdown(ctx)
	}
	return callback, stop, nil
}

func (c *loginCallbackServer) Wait(timeout time.Duration) (cliCallbackResult, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-c.result:
		if result.Code == "" && result.Error == "" {
			return cliCallbackResult{}, errors.New("CLI login callback did not include a code.")
		}
		return result, nil
	case <-timer.C:
		return cliCallbackResult{}, errors.New("Timed out waiting for browser login.")
	}
}

func buildLoginURL(baseURL string, state string, redirectURI string) (string, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/") + "/")
	if err != nil {
		return "", err
	}
	path, _ := url.Parse("login/cli")
	resolved := base.ResolveReference(path)
	query := resolved.Query()
	query.Set("state", state)
	query.Set("redirect_uri", redirectURI)
	resolved.RawQuery = query.Encode()
	return resolved.String(), nil
}

func (r *Runner) exchangeCLILoginCode(baseURL string, code string, state string, redirectURI string, keyName string) (cliLoginExchangeResponse, error) {
	endpoint, err := url.JoinPath(strings.TrimRight(baseURL, "/"), "/api/auth/cli/exchange")
	if err != nil {
		return cliLoginExchangeResponse{}, err
	}
	payload, _ := json.Marshal(map[string]string{
		"code":         code,
		"state":        state,
		"redirect_uri": redirectURI,
		"key_name":     keyName,
	})
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return cliLoginExchangeResponse{}, err
	}
	req.Header.Set("content-type", "application/json")
	res, err := r.HTTPClient.Do(req)
	if err != nil {
		return cliLoginExchangeResponse{}, err
	}
	defer res.Body.Close()
	var decoded cliLoginExchangeResponse
	var failure struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	decoder := json.NewDecoder(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_ = decoder.Decode(&failure)
		if failure.Error != "" {
			if failure.Code != "" {
				return cliLoginExchangeResponse{}, fmt.Errorf("%s: %s", failure.Code, failure.Error)
			}
			return cliLoginExchangeResponse{}, errors.New(failure.Error)
		}
		return cliLoginExchangeResponse{}, fmt.Errorf("Request failed with HTTP %d", res.StatusCode)
	}
	if err := decoder.Decode(&decoded); err != nil {
		return cliLoginExchangeResponse{}, err
	}
	return decoded, nil
}

func (r *Runner) openBrowser(loginURL string) error {
	switch runtime.GOOS {
	case "darwin":
		_, _, err := r.RunExternal("open", []string{loginURL}, "", nil)
		return err
	case "windows":
		_, _, err := r.RunExternal("rundll32", []string{"url.dll,FileProtocolHandler", loginURL}, "", nil)
		return err
	default:
		_, _, err := r.RunExternal("xdg-open", []string{loginURL}, "", nil)
		return err
	}
}

func randomURLToken(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func defaultLoginKeyName() string {
	host, err := osHostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "cli"
	}
	host = strings.ToLower(strings.TrimSpace(host))
	var cleaned strings.Builder
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			cleaned.WriteRune(r)
		}
	}
	if cleaned.Len() == 0 {
		return "cli"
	}
	if cleaned.Len() > 48 {
		return "cli-" + cleaned.String()[:48]
	}
	return "cli-" + cleaned.String()
}

func osEnvFirst(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func osHostname() (string, error) {
	return os.Hostname()
}
