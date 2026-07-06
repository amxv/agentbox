package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"agentbox/internal/agentbox/profiles"
)

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
