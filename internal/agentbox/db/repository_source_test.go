package db

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRepositoryRequestPathContainsNoSchemaDDL(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	contents, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), "repository.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToLower(string(contents))
	for _, forbidden := range []string{
		"ensureschema(",
		"create table ",
		"alter table ",
		"create extension ",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("repository.go still contains request-path schema operation %q", forbidden)
		}
	}
}
