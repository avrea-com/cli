package commands

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCPUProfileLifecycle(t *testing.T) {
	for _, mode := range []string{"success", "failure", "cancelled", "embedded"} {
		t.Run(mode, func(t *testing.T) {
			d, _, stderr := testDeps(t)
			path := filepath.Join(t.TempDir(), "cpu.pprof")
			d.Getenv = func(key string) string {
				if key == "AVR_CPU_PROFILE" {
					return path
				}
				return ""
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := 0
			var code int
			if mode == "embedded" || mode == "cancelled" {
				root := NewRoot(d)
				root.AddCommand(&cobra.Command{Use: "probe", RunE: func(c *cobra.Command, _ []string) error {
					if mode == "cancelled" {
						cancel()
						return c.Context().Err()
					}
					return nil
				}})
				if mode == "cancelled" {
					want = 130
				}
				code = ExecuteRoot(ctx, root, []string{"probe"}, d.Err, d)
			} else {
				args := []string{"--help"}
				if mode == "failure" {
					args = []string{"no-such-command"}
					want = 2
				}
				code = Execute(ctx, args, d)
			}
			if code != want {
				t.Fatal(code, want, stderr)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			info, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
				t.Fatal(info.Mode())
			}
			zipped, err := gzip.NewReader(file)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = zipped.Close() }()
			data, err := io.ReadAll(zipped)
			if err != nil || len(data) == 0 {
				t.Fatal("profile not flushed", err)
			}
		})
	}
}

func TestCPUProfileDoesNotOverwriteAndCompletionSkipsIt(t *testing.T) {
	d, _, stderr := testDeps(t)
	path := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	d.Getenv = func(key string) string {
		if key == "AVR_CPU_PROFILE" {
			return path
		}
		return ""
	}
	if code := Execute(t.Context(), []string{"--help"}, d); code != 1 || !strings.Contains(stderr.String(), "CPU profiling") {
		t.Fatal(code, stderr)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep" {
		t.Fatal(string(data), err)
	}
	path = filepath.Join(t.TempDir(), "completion.pprof")
	d.Getenv = func(key string) string {
		return map[string]string{"AVR_CPU_PROFILE": path, "_AVR_COMPLETE": "bash_complete", "COMP_WORDS": "avr ", "COMP_CWORD": "1"}[key]
	}
	if code := Execute(t.Context(), nil, d); code != 0 {
		t.Fatal(code, stderr)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completion created profile", err)
	}
}
