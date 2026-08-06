package migrations

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
)

var migrationFilenamePattern = regexp.MustCompile(`^(\d{4})_[a-z0-9][a-z0-9_]*\.sql$`)

//go:embed *.sql
var embeddedFiles embed.FS

type Migration struct {
	Version  string
	Name     string
	SQL      string
	Checksum string
}

func Load() ([]Migration, error) {
	return loadFS(embeddedFiles)
}

func loadFS(files fs.FS) ([]Migration, error) {
	entries, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(entries)

	migrations := make([]Migration, 0, len(entries))
	seenVersions := make(map[string]string, len(entries))
	for _, name := range entries {
		matches := migrationFilenamePattern.FindStringSubmatch(name)
		if matches == nil {
			return nil, fmt.Errorf("invalid migration filename %q", name)
		}
		version := matches[1]
		if previous, ok := seenVersions[version]; ok {
			return nil, fmt.Errorf("migration version %s is used by both %q and %q", version, previous, name)
		}
		contents, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		checksum := sha256.Sum256(contents)
		migrations = append(migrations, Migration{
			Version:  version,
			Name:     name,
			SQL:      string(contents),
			Checksum: hex.EncodeToString(checksum[:]),
		})
		seenVersions[version] = name
	}
	if len(migrations) == 0 {
		return nil, fmt.Errorf("no SQL migrations found")
	}
	return migrations, nil
}
