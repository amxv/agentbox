package migrations

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadEmbeddedMigrations(t *testing.T) {
	migrations, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 6 {
		t.Fatalf("migration count = %d, want 6", len(migrations))
	}
	for index, migration := range migrations {
		wantVersion := fmt.Sprintf("%04d", index+1)
		if migration.Version != wantVersion {
			t.Fatalf("migration %d version = %q, want %q", index, migration.Version, wantVersion)
		}
		if migration.Name == "" || migration.SQL == "" || len(migration.Checksum) != 64 {
			t.Fatalf("migration %d is incomplete: %#v", index, migration)
		}
	}
}

func TestLoadRejectsDuplicateVersions(t *testing.T) {
	_, err := loadFS(fstest.MapFS{
		"0001_first.sql":  &fstest.MapFile{Data: []byte("select 1;")},
		"0001_second.sql": &fstest.MapFile{Data: []byte("select 2;")},
	})
	if err == nil || !strings.Contains(err.Error(), "version 0001") {
		t.Fatalf("error = %v, want duplicate-version failure", err)
	}
}

func TestLoadRejectsInvalidFilenames(t *testing.T) {
	_, err := loadFS(fstest.MapFS{
		"first.sql": &fstest.MapFile{Data: []byte("select 1;")},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid migration filename") {
		t.Fatalf("error = %v, want invalid-filename failure", err)
	}
}
