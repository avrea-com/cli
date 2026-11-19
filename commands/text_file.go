package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxTextFile = 16 << 20

var (
	errTextTooLarge = errors.New("file exceeds 16 MiB")
	errTextNotUTF8  = errors.New("file is not valid UTF-8")
)

// readLimitedText reads a local UTF-8 text file of at most 16 MiB, so a huge
// or binary file named on the command line fails instead of filling memory.
func readLimitedText(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxTextFile+1))
	switch {
	case err != nil:
		return "", err
	case len(data) > maxTextFile:
		return "", errTextTooLarge
	case !utf8.Valid(data):
		return "", errTextNotUTF8
	}
	return string(data), nil
}

// readTextFile reads at most 16 MiB of UTF-8 text with newlines normalized.
// A missing file reads as empty. label names the file in errors.
func readTextFile(path, label string) (string, error) {
	raw, err := readLimitedText(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", nil
	case errors.Is(err, errTextTooLarge), errors.Is(err, errTextNotUTF8):
		return "", fmt.Errorf("%s: %w", label, err)
	case err != nil:
		return "", err
	}
	// Python read_text uses universal newline handling.
	return strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"), nil
}

// writeTextAtomic replaces path through a temporary file in the same
// directory, so a reader sees the old or the new content, never a partial
// file. The mode is set before the rename. Missing parent directories are
// created private; existing ones keep their permissions.
func writeTextAtomic(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = io.WriteString(file, content); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
