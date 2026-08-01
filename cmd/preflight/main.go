package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentbox/internal/agentbox/assets"
	"agentbox/internal/agentbox/backup"
	"agentbox/internal/agentbox/config"
	"agentbox/internal/agentbox/db"
)

const defaultPreflightTimeout = 2 * time.Hour

type commandOptions struct {
	OutputDir    string
	RunID        string
	SourceBucket string
	SourcePrefix string
	BackupBucket string
	BackupPrefix string
	PGDumpBinary string
	Timeout      time.Duration
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	cfg := config.LoadFromEnv()
	options, err := parseOptions(args, cfg, stderr, time.Now().UTC())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := validateConfig(cfg, options); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()

	repository, err := db.Open(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "open PostgreSQL: %v\n", err)
		return 1
	}
	defer repository.Close()

	store, err := assets.NewR2Store(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "open R2: %v\n", err)
		return 1
	}

	runner := backup.Runner{
		Snapshots: repository,
		Dumper: backup.PGDump{
			DatabaseURL: cfg.DatabaseURL,
			Binary:      options.PGDumpBinary,
		},
		Objects: store,
	}
	manifest, runErr := runner.Run(ctx, backup.Options{
		RunID:        options.RunID,
		OutputDir:    options.OutputDir,
		SourceBucket: options.SourceBucket,
		SourcePrefix: options.SourcePrefix,
		BackupBucket: options.BackupBucket,
		BackupPrefix: options.BackupPrefix,
	})

	manifestPath := ""
	if manifest.RunDirectory != "" {
		manifestPath = filepath.Join(manifest.RunDirectory, "manifest.json")
		if err := backup.WriteManifest(manifestPath, manifest); err != nil {
			fmt.Fprintf(stderr, "write preflight manifest: %v\n", err)
			return 1
		}
	}

	printSummary(stdout, manifest, manifestPath)
	if runErr != nil {
		if errors.Is(runErr, backup.ErrPreflightNotReady) {
			fmt.Fprintln(stderr, "AgentBox backup preflight is not ready. Resolve every error in the manifest before cutover.")
		} else {
			fmt.Fprintf(stderr, "AgentBox backup preflight failed: %v\n", runErr)
		}
		return 1
	}
	return 0
}

func parseOptions(args []string, cfg config.Config, stderr io.Writer, now time.Time) (commandOptions, error) {
	runID := now.UTC().Format("20060102T150405Z")
	defaults := commandOptions{
		OutputDir:    strings.TrimSpace(os.Getenv("AGENTBOX_BACKUP_OUTPUT_DIR")),
		RunID:        runID,
		SourceBucket: cfg.R2Bucket,
		SourcePrefix: strings.TrimSpace(os.Getenv("AGENTBOX_BACKUP_SOURCE_PREFIX")),
		BackupBucket: strings.TrimSpace(os.Getenv("AGENTBOX_BACKUP_BUCKET")),
		BackupPrefix: strings.Trim(strings.TrimSpace(os.Getenv("AGENTBOX_BACKUP_PREFIX")), "/"),
		PGDumpBinary: firstNonEmpty(strings.TrimSpace(os.Getenv("AGENTBOX_PG_DUMP_BINARY")), "pg_dump"),
		Timeout:      defaultPreflightTimeout,
	}
	if defaults.BackupBucket == "" {
		defaults.BackupBucket = defaults.SourceBucket
	}

	set := flag.NewFlagSet("agentbox-preflight", flag.ContinueOnError)
	set.SetOutput(stderr)
	options := defaults
	set.StringVar(&options.OutputDir, "output-dir", options.OutputDir, "secure local directory for the database dump and manifest")
	set.StringVar(&options.RunID, "run-id", options.RunID, "stable identifier for this repeatable backup run")
	set.StringVar(&options.SourceBucket, "source-bucket", options.SourceBucket, "R2 bucket containing AgentBox attachments")
	set.StringVar(&options.SourcePrefix, "source-prefix", options.SourcePrefix, "optional R2 prefix to inventory")
	set.StringVar(&options.BackupBucket, "backup-bucket", options.BackupBucket, "R2 recovery bucket (defaults to the source bucket)")
	set.StringVar(&options.BackupPrefix, "backup-prefix", options.BackupPrefix, "R2 recovery prefix (defaults to agentbox-recovery/<run-id>)")
	set.StringVar(&options.PGDumpBinary, "pg-dump-binary", options.PGDumpBinary, "pg_dump executable")
	set.DurationVar(&options.Timeout, "timeout", options.Timeout, "maximum preflight duration")
	if err := set.Parse(args); err != nil {
		return commandOptions{}, err
	}
	if len(set.Args()) != 0 {
		return commandOptions{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(set.Args(), " "))
	}

	options.OutputDir = strings.TrimSpace(options.OutputDir)
	options.RunID = strings.TrimSpace(options.RunID)
	options.SourceBucket = strings.TrimSpace(options.SourceBucket)
	options.SourcePrefix = strings.TrimLeft(strings.TrimSpace(options.SourcePrefix), "/")
	options.BackupBucket = strings.TrimSpace(options.BackupBucket)
	options.BackupPrefix = strings.Trim(strings.TrimSpace(options.BackupPrefix), "/")
	options.PGDumpBinary = strings.TrimSpace(options.PGDumpBinary)
	if options.RunID == "" {
		return commandOptions{}, errors.New("--run-id must not be empty")
	}
	if options.OutputDir == "" {
		return commandOptions{}, errors.New("--output-dir or AGENTBOX_BACKUP_OUTPUT_DIR is required")
	}
	if options.BackupBucket == "" {
		options.BackupBucket = options.SourceBucket
	}
	if options.BackupPrefix == "" {
		options.BackupPrefix = "agentbox-recovery/" + options.RunID
	}
	if options.PGDumpBinary == "" {
		options.PGDumpBinary = "pg_dump"
	}
	if options.Timeout <= 0 {
		return commandOptions{}, errors.New("--timeout must be greater than zero")
	}
	return options, nil
}

func validateConfig(cfg config.Config, options commandOptions) error {
	missing := []string{}
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if strings.TrimSpace(cfg.R2AccountID) == "" {
		missing = append(missing, "R2_ACCOUNT_ID")
	}
	if strings.TrimSpace(cfg.R2AccessKeyID) == "" {
		missing = append(missing, "R2_ACCESS_KEY_ID")
	}
	if strings.TrimSpace(cfg.R2SecretAccessKey) == "" {
		missing = append(missing, "R2_SECRET_ACCESS_KEY")
	}
	if strings.TrimSpace(options.SourceBucket) == "" {
		missing = append(missing, "R2_BUCKET or --source-bucket")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required backup configuration: %s", strings.Join(missing, ", "))
	}
	return nil
}

func printSummary(output io.Writer, manifest backup.Manifest, manifestPath string) {
	fmt.Fprintf(output, "Run: %s\n", manifest.RunID)
	fmt.Fprintf(output, "Ready: %t\n", manifest.Ready)
	fmt.Fprintf(output, "Rows: threads=%d messages=%d assets=%d pending_uploads=%d\n",
		manifest.Database.Counts.Threads,
		manifest.Database.Counts.Messages,
		manifest.Database.Counts.Assets,
		manifest.Database.Counts.PendingUploads,
	)
	fmt.Fprintf(output, "Objects: inventory=%d referenced=%d present=%d missing=%d copied=%d existing_backup=%d unreferenced=%d\n",
		manifest.Objects.InventoryCount,
		manifest.Objects.ReferencedObjectCount,
		manifest.Objects.PresentObjectCount,
		manifest.Objects.MissingObjectCount,
		manifest.Objects.CopiedObjectCount,
		manifest.Objects.AlreadyBackedUpCount,
		manifest.Objects.UnreferencedCount,
	)
	fmt.Fprintf(output, "Issues: %d\n", len(manifest.Issues))
	if manifestPath != "" {
		fmt.Fprintf(output, "Manifest: %s\n", manifestPath)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
