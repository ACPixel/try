package main

import (
	"os"
	"regexp"
	"strings"
)

var nonSlugChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func expandHomeDir(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return strings.Replace(path, "~", home, 1), nil
	}
	return path, nil
}

func slugifyName(name string) string {
	slug := strings.Trim(nonSlugChars.ReplaceAllString(strings.TrimSpace(name), "-"), "-")
	if slug == "" {
		return "untitled"
	}
	return slug
}

func isTerminal(f *os.File) bool {
	fileInfo, err := f.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}
