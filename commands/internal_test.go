package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDocsPruneOnlyStalePagesAndHonorCancellation(t *testing.T) {
	root := t.TempDir()
	for name, value := range map[string]string{"old.md": "old", "nested/old.1": "old", "keep.txt": "keep", "nested/keep.mjs": "keep"} {
		if err := writeDocFile(filepath.Join(root, name), value); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := writeDocDir(ctx, root, map[string]string{"new.md": "new"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.md")); err != nil {
		t.Fatal("cancelled generation pruned pages", err)
	}
	if err := writeDocDir(t.Context(), root, map[string]string{"new.md": "new", "nested/fresh.1": "fresh"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old.md", "nested/old.1"} {
		if _, err := os.Stat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal(name, err)
		}
	}
	for name, want := range map[string]string{"new.md": "new", "nested/fresh.1": "fresh", "keep.txt": "keep", "nested/keep.mjs": "keep"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(got) != want {
			t.Fatal(name, string(got), err)
		}
	}
}
func TestDocsCleanupDoesNotWalkDirectorySymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require privileges")
	}
	outside, root := t.TempDir(), t.TempDir()
	if err := writeDocFile(filepath.Join(outside, "keep.md"), "outside"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := writeDocDir(t.Context(), root, map[string]string{"index.md": "new"}); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(outside, "keep.md")); err != nil || string(raw) != "outside" {
		t.Fatal(string(raw), err)
	}
}
