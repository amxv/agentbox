package validate

import (
	"errors"
	"net/url"
)

func CreateThreadTitle(title string) error {
	if len(title) < 1 {
		return errors.New(zodIssue(map[string]any{
			"origin":    "string",
			"code":      "too_small",
			"minimum":   1,
			"inclusive": true,
			"path":      []string{"title"},
			"message":   "Too small: expected string to have >=1 characters",
		}))
	}
	if len(title) > 200 {
		return errors.New(zodIssue(map[string]any{
			"origin":    "string",
			"code":      "too_big",
			"maximum":   200,
			"inclusive": true,
			"path":      []string{"title"},
			"message":   "Too big: expected string to have <=200 characters",
		}))
	}
	return nil
}

func ThreadID(threadID string) error {
	if threadID == "" {
		return errors.New("Too small: expected string to have >=1 characters")
	}
	return nil
}

func PostMessage(threadID string) error {
	return ThreadID(threadID)
}

func FileReference(downloadURL string, fileID string) error {
	if _, err := url.ParseRequestURI(downloadURL); err != nil {
		return errors.New(zodIssue(map[string]any{
			"code":    "invalid_format",
			"format":  "url",
			"path":    []string{"download_url"},
			"message": "Invalid URL",
		}))
	}
	if fileID == "" {
		return errors.New("Too small: expected string to have >=1 characters")
	}
	return nil
}

func ClampSignedURLExpiry(seconds int) int {
	if seconds == 0 {
		return 300
	}
	if seconds < 60 {
		return 60
	}
	if seconds > 3600 {
		return 3600
	}
	return seconds
}

func zodIssue(issue map[string]any) string {
	switch issue["code"] {
	case "too_small":
		return "[\n  {\n    \"origin\": \"string\",\n    \"code\": \"too_small\",\n    \"minimum\": 1,\n    \"inclusive\": true,\n    \"path\": [\n      \"title\"\n    ],\n    \"message\": \"Too small: expected string to have >=1 characters\"\n  }\n]"
	case "too_big":
		return "[\n  {\n    \"origin\": \"string\",\n    \"code\": \"too_big\",\n    \"maximum\": 200,\n    \"inclusive\": true,\n    \"path\": [\n      \"title\"\n    ],\n    \"message\": \"Too big: expected string to have <=200 characters\"\n  }\n]"
	case "invalid_format":
		return "[\n  {\n    \"code\": \"invalid_format\",\n    \"format\": \"url\",\n    \"path\": [\n      \"download_url\"\n    ],\n    \"message\": \"Invalid URL\"\n  }\n]"
	default:
		return "Validation failed"
	}
}
