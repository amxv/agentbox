package messageformat

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	Auto     = "auto"
	Plain    = "text/plain"
	Markdown = "text/markdown"
)

var (
	ErrInvalidContentType = errors.New("body_content_type must be text/plain, text/markdown, or auto")

	fencedCodeRE   = regexp.MustCompile("(?m)^```[\\s\\S]*```")
	mermaidFenceRE = regexp.MustCompile("(?mi)^```mermaid\\b")
	headingRE      = regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s+\S`)
	tableRE        = regexp.MustCompile(`(?m)^\s*\|.+\|\s*\n\s*\|?\s*:?-{3,}:?\s*\|`)
	taskListRE     = regexp.MustCompile(`(?m)^\s*[-*+]\s+\[[ xX]\]\s+`)
	bulletListRE   = regexp.MustCompile(`(?m)^\s*[-*+]\s+\S`)
	numberListRE   = regexp.MustCompile(`(?m)^\s*\d+\.\s+\S`)
	quoteRE        = regexp.MustCompile(`(?m)^\s{0,3}>\s+\S`)
	linkRE         = regexp.MustCompile(`\[[^\]]+\]\([^)]+\)`)
	mermaidRE      = regexp.MustCompile(`(?m)^\s*(graph|flowchart|sequenceDiagram|classDiagram|stateDiagram|stateDiagram-v2|erDiagram|gantt|pie|journey|mindmap|timeline)\b`)
)

// Resolve normalizes an optional requested content type. Empty and "auto"
// values infer from the body and, when present, the source file path.
func Resolve(requested *string, body string, sourcePath string) (string, error) {
	if requested != nil {
		switch strings.ToLower(strings.TrimSpace(*requested)) {
		case "", Auto:
			return Infer(body, sourcePath), nil
		case Plain:
			return Plain, nil
		case Markdown:
			return Markdown, nil
		default:
			return "", ErrInvalidContentType
		}
	}
	return Infer(body, sourcePath), nil
}

// Infer returns a conservative message body content type. It intentionally
// requires stronger Markdown signals for short messages so shell snippets,
// logs, and quick chat replies do not get over-rendered.
func Infer(body string, sourcePath string) string {
	if isMarkdownPath(sourcePath) {
		return Markdown
	}
	text := strings.TrimSpace(body)
	if text == "" {
		return Plain
	}
	score := 0
	if fencedCodeRE.MatchString(text) {
		score += 4
	}
	if mermaidFenceRE.MatchString(text) {
		score += 6
	}
	if headingRE.MatchString(text) {
		score += 2
	}
	if tableRE.MatchString(text) {
		score += 4
	}
	if taskListRE.MatchString(text) {
		score += 3
	}
	if len(bulletListRE.FindAllString(text, -1)) >= 2 {
		score += 2
	}
	if len(numberListRE.FindAllString(text, -1)) >= 2 {
		score += 2
	}
	if quoteRE.MatchString(text) {
		score += 2
	}
	if linkRE.MatchString(text) {
		score += 2
	}
	if mermaidRE.MatchString(text) {
		score += 5
	}
	if len(text) < 80 && score < 4 {
		return Plain
	}
	if score >= 3 {
		return Markdown
	}
	return Plain
}

func isMarkdownPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown" || ext == ".mdown" || ext == ".mkd"
}
