package util

import (
	"errors"
	"path"
	"path/filepath"
	"strings"
)

var ErrInvalidFilename = errors.New("invalid filename")

func SafeFilename(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ErrInvalidFilename
	}

	normalized := strings.ReplaceAll(input, "\\", "/")
	base := path.Base(normalized)

	if base != normalized ||
		base == "." ||
		base == ".." ||
		strings.ContainsRune(base, '\x00') {
		return "", ErrInvalidFilename
	}

	return base, nil
}

func SafeContentPath(root, objectID, filename string) (string, error) {
	safeName, err := SafeFilename(filename)
	if err != nil {
		return "", err
	}

	baseDir, err := filepath.Abs(filepath.Join(root, objectID))
	if err != nil {
		return "", err
	}

	target, err := filepath.Abs(filepath.Join(baseDir, safeName))
	if err != nil {
		return "", err
	}

	relative, err := filepath.Rel(baseDir, target)
	if err != nil {
		return "", err
	}

	if relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrInvalidFilename
	}

	return target, nil
}
