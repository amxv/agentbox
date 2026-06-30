package messageformat

import "testing"

func TestResolveExplicitContentTypes(t *testing.T) {
	plain := Plain
	got, err := Resolve(&plain, "# Title", "note.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != Plain {
		t.Fatalf("explicit plain = %q", got)
	}

	markdown := Markdown
	got, err = Resolve(&markdown, "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != Markdown {
		t.Fatalf("explicit markdown = %q", got)
	}
}

func TestResolveRejectsInvalidContentType(t *testing.T) {
	invalid := "text/html"
	if _, err := Resolve(&invalid, "hello", ""); err == nil {
		t.Fatal("expected invalid content type error")
	}
}

func TestInferMarkdownSignals(t *testing.T) {
	cases := []struct {
		name string
		body string
		path string
		want string
	}{
		{name: "markdown file", body: "hello", path: "handoff.md", want: Markdown},
		{name: "table", body: "| A | B |\n| --- | --- |\n| 1 | 2 |", want: Markdown},
		{name: "mermaid fence", body: "```mermaid\nflowchart TD\n  A --> B\n```", want: Markdown},
		{name: "code fence", body: "```go\nfmt.Println(\"hi\")\n```", want: Markdown},
		{name: "plain short chat", body: "done", want: Plain},
		{name: "plain shell-ish", body: "git commit -m '# fix'", want: Plain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Infer(tc.body, tc.path); got != tc.want {
				t.Fatalf("Infer() = %q, want %q", got, tc.want)
			}
		})
	}
}
