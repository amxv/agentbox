package backup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type PGDump struct {
	DatabaseURL string
	Binary      string
}

func (d PGDump) Dump(ctx context.Context, snapshotID string, destinationPath string) error {
	binary := strings.TrimSpace(d.Binary)
	if binary == "" {
		binary = "pg_dump"
	}
	if strings.TrimSpace(d.DatabaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is required for pg_dump")
	}
	if strings.TrimSpace(snapshotID) == "" {
		return fmt.Errorf("PostgreSQL exported snapshot ID is required")
	}

	command := exec.CommandContext(
		ctx,
		binary,
		"--format=custom",
		"--no-owner",
		"--no-acl",
		"--snapshot="+snapshotID,
		"--file="+destinationPath,
	)
	command.Env = append(os.Environ(), "PGDATABASE="+d.DatabaseURL)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return fmt.Errorf("run %s: %w", binary, err)
		}
		return fmt.Errorf("run %s: %w: %s", binary, err, detail)
	}
	return nil
}
