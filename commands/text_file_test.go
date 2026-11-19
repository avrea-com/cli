package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every file read through the shared limit fails the same way, whether it is
// a skill, an SSH file or a flag value given as @file.
func TestTextFilesShareTheReadLimits(t *testing.T) {
	dir := t.TempDir()
	large := filepath.Join(dir, "large")
	if err := os.WriteFile(large, make([]byte, maxTextFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "binary")
	if err := os.WriteFile(binary, []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	limit := filepath.Join(dir, "limit")
	if err := os.WriteFile(limit, []byte(strings.Repeat("a", maxTextFile)), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, want string }{{large, "file exceeds 16 MiB"}, {binary, "file is not valid UTF-8"}} {
		if _, err := readTextFile(test.path, "Skill file"); err == nil || !strings.Contains(err.Error(), "Skill file: "+test.want) {
			t.Error(test.path, err)
		}
		if _, _, err := vmInputFile(test.path, "--ssh-key", "SSH key"); err == nil || !strings.Contains(err.Error(), "--ssh-key") || !strings.Contains(err.Error(), test.want) {
			t.Error(test.path, err)
		}
	}
	if text, err := readTextFile(limit, "Skill file"); err != nil || len(text) != maxTextFile {
		t.Fatal("a file at the limit must be read", err)
	}
	if text, err := readTextFile(filepath.Join(dir, "missing"), "Skill file"); err != nil || text != "" {
		t.Fatal("a missing file reads as empty", err)
	}
	if _, _, err := vmInputFile(filepath.Join(dir, "missing"), "--ssh-key", "SSH key"); err == nil {
		t.Fatal("a missing @file is an error")
	}
}
