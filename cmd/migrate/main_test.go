package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRejectsInvalidOptionsBeforeOpeningPostgres(t *testing.T) {
	for _, args := range [][]string{{"--timeout", "0s"}, {"unexpected"}} {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestRunHelpDocumentsBoundedMigration(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "-through") || !strings.Contains(stderr.String(), "0016") {
		t.Fatalf("help=%s", stderr.String())
	}
}
