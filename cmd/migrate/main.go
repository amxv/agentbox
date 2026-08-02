package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	set := flag.NewFlagSet("agentbox-migrate", flag.ContinueOnError)
	set.SetOutput(stderr)
	through := set.String("through", "", "apply migrations through this exact version, for example 0016")
	timeout := set.Duration("timeout", 30*time.Second, "maximum migration duration")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if len(set.Args()) != 0 {
		fmt.Fprintf(stderr, "unexpected positional arguments: %s\n", strings.Join(set.Args(), " "))
		return 2
	}
	if *timeout <= 0 {
		fmt.Fprintln(stderr, "--timeout must be greater than zero")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	repo, err := db.Open(ctx, config.LoadFromEnv())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer repo.Close()

	var migrateErr error
	if strings.TrimSpace(*through) == "" {
		migrateErr = repo.Migrate(ctx)
	} else {
		migrateErr = repo.MigrateThrough(ctx, *through)
	}
	if migrateErr != nil {
		fmt.Fprintln(stderr, migrateErr)
		return 1
	}
	if strings.TrimSpace(*through) == "" {
		fmt.Fprintln(stdout, "Agentbox migrations are up to date.")
	} else {
		fmt.Fprintf(stdout, "Agentbox migrations are applied through %s.\n", strings.TrimSpace(*through))
	}
	return 0
}
