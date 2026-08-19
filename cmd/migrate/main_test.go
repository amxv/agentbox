package main

import (
	"bytes"
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
