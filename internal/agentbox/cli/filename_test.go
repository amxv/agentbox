package cli

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWindowsSafeFileName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "report:final?.pdf", want: "report_final_.pdf"},
		{name: `folder\draft|1.txt`, want: "folder_draft_1.txt"},
		{name: "CON.txt", want: "_CON.txt"},
		{name: "lpt9", want: "_lpt9"},
		{name: "CONOUT$.log", want: "_CONOUT$.log"},
		{name: "trailing. ", want: "trailing"},
		{name: ".", want: "attachment"},
		{name: "normal-file.md", want: "normal-file.md"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := windowsSafeFileName(test.name); got != test.want {
				t.Fatalf("windowsSafeFileName(%q) = %q, want %q", test.name, got, test.want)
			}
		})
	}
}

func TestWindowsSafeFileNameBoundsLongNamesAndPreservesExtension(t *testing.T) {
	got := windowsSafeFileName(strings.Repeat("a", 300) + ".tar.gz")
	if utf8.RuneCountInString(got) > windowsDefaultFileNameRuneLimit {
		t.Fatalf("filename length = %d, want <= %d", utf8.RuneCountInString(got), windowsDefaultFileNameRuneLimit)
	}
	if !strings.HasSuffix(got, ".gz") {
		t.Fatalf("filename %q lost extension", got)
	}
}

func TestLocalFileNameForOSChangesOnlyWindows(t *testing.T) {
	const unsafe = "report:final?.pdf"
	if got := localFileNameForOS(unsafe, "windows"); got != "report_final_.pdf" {
		t.Fatalf("windows filename = %q", got)
	}
	if got := localFileNameForOS(unsafe, "darwin"); got != unsafe {
		t.Fatalf("darwin filename = %q", got)
	}
	if got := localFileNameForOS(unsafe, "linux"); got != unsafe {
		t.Fatalf("linux filename = %q", got)
	}
}
