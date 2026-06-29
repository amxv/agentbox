package validate

import (
	"errors"
	"net/url"
)

func CreateThreadTitle(title string) error {
	if len(title) < 1 {
		return errors.New("Too small: expected string to have >=1 characters")
	}
	if len(title) > 200 {
		return errors.New("Too big: expected string to have <=200 characters")
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
		return errors.New("Invalid URL")
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
