package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avrea-com/cli/internal/version"
)

func TestBootstrapInstallsNativeReleaseWithoutPython(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("VM bootstrap uses POSIX shells")
	}
	// Limit PATH to the installer's system tools: no Python, pip or pipx.
	tools := t.TempDir()
	for _, name := range []string{"sh", "uname", "curl", "tar", "gzip", "cut", "mktemp", "rm", "cat", "grep", "mkdir", "chmod", "mv", "sha256sum", "shasum"} {
		path, err := exec.LookPath(name)
		if err != nil && (name == "sha256sum" || name == "shasum") {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(t.TempDir(), "home with spaces")
	stem := fmt.Sprintf("avr_%s_%s_%s", version.Current(), runtime.GOOS, runtime.GOARCH)
	payload := []byte("#!/bin/sh\necho native-fixture\n")
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: stem + "/avr", Mode: 0755, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	var corrupt atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/v" + version.Current() + "/" + stem + ".tar.gz":
			_, _ = w.Write(archive.Bytes())
		case "/download/v" + version.Current() + "/" + stem + ".tar.gz.sha256":
			digest := sha256.Sum256(archive.Bytes())
			if corrupt.Load() {
				digest[0] ^= 1
			}
			_, _ = fmt.Fprintf(w, "%x  %s.tar.gz\n", digest, stem)
		default:
			t.Errorf("unexpected release request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	steps, err := bootstrapSteps(bootstrapOptions{installAVR: true}, nil, nil, "", nil)
	if err != nil || len(steps) != 1 {
		t.Fatal(steps, err)
	}
	install := func() ([]byte, error) {
		command := exec.CommandContext(t.Context(), "/bin/sh", "-c", steps[0].script)
		command.Env = append(os.Environ(), "HOME="+home, "PATH="+tools, "AVR_RELEASE_BASE_URL="+server.URL)
		return command.CombinedOutput()
	}
	for range 2 {
		if out, err := install(); err != nil || !bytes.Contains(out, []byte("Add this directory to PATH:")) {
			t.Fatalf("install/reinstall: %s: %v", out, err)
		}
	}
	binary := filepath.Join(home, ".local", "bin", "avr")
	if out, err := exec.CommandContext(t.Context(), binary).CombinedOutput(); err != nil || string(out) != "native-fixture\n" {
		t.Fatalf("native executable: %s: %v", out, err)
	}
	corrupt.Store(true)
	if out, err := install(); err == nil || !bytes.Contains(out, []byte("Checksum mismatch")) {
		t.Fatalf("corrupt archive accepted: %s: %v", out, err)
	}
	if got, err := os.ReadFile(binary); err != nil || !bytes.Equal(got, payload) {
		t.Fatal("failed install changed existing binary", err)
	}
}

func TestBootstrapEnvScriptPrivateIdempotentLoginHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires bash")
	}
	home := t.TempDir()
	profile := filepath.Join(home, ".bash_profile")
	if err := os.WriteFile(profile, []byte("# existing login settings\n"), 0600); err != nil {
		t.Fatal(err)
	}
	env := bootstrapEnvironment{{"FIRST", "a'b $(false)"}, {"EMPTY", ""}}
	steps, err := bootstrapSteps(bootstrapOptions{}, env, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		command := exec.CommandContext(t.Context(), "/bin/bash", "-c", steps[0].script)
		command.Env = append(os.Environ(), "HOME="+home)
		command.Stdin = strings.NewReader(steps[0].stdin)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	pin := filepath.Join(home, ".avrea", "bootstrap.env")
	info, err := os.Stat(pin)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	for _, path := range []string{profile, filepath.Join(home, ".bashrc")} {
		command := exec.CommandContext(t.Context(), "/bin/bash", "-c", ". \"$1\"; printf '%s|%s' \"$FIRST\" \"$EMPTY\"", "bash", path)
		raw, err := command.Output()
		if err != nil || string(raw) != "a'b $(false)|" {
			t.Fatalf("%q: %v", raw, err)
		}
		raw, err = os.ReadFile(path)
		if err != nil || strings.Count(string(raw), "# avrea bootstrap env") != 1 {
			t.Fatalf("%q: %v", raw, err)
		}
	}
	raw, _ := os.ReadFile(profile)
	if !strings.HasPrefix(string(raw), "# existing login settings\n") {
		t.Fatal("lost existing login settings")
	}
	if _, err := os.Stat(filepath.Join(home, ".profile")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created a lower-priority login file", err)
	}
}

func TestBootstrapSSHTimeoutAndLaunchFailureRemovePins(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable stub")
	}
	for _, mode := range []string{"timeout", "cancel", "missing", "exit"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			t.Setenv("PATH", directory)
			t.Setenv("TMPDIR", directory)
			if mode != "missing" {
				script := "#!/bin/sh\nexec /bin/sleep 30\n"
				if mode == "exit" {
					script = "#!/bin/sh\nexit 42\n"
				}
				if err := os.WriteFile(filepath.Join(directory, "ssh"), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			d, _, _ := testDeps(t)
			d.defaults()
			ep := map[string]any{"external_ip": "192.0.2.1", "external_port": float64(22), "username": "runner", "host_key": "ssh-ed25519 fixture"}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			timeout := 30 * time.Second
			if mode == "timeout" {
				timeout = 20 * time.Millisecond
			}
			if mode == "cancel" {
				cancel()
			}
			code, err := bootstrapSSH(ctx, d, ep, []string{"true"}, "", "fixture-secret", timeout, true)
			switch mode {
			case "timeout":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(code, err)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) {
					t.Fatal(code, err)
				}
			case "missing":
				if err == nil {
					t.Fatal("missing SSH accepted")
				}
			case "exit":
				if code != 42 || err != nil {
					t.Fatal(code, err)
				}
			}
			files, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if file.Name() != "ssh" {
					t.Fatal("temporary pin remains", file.Name())
				}
			}
		})
	}
}

func TestBootstrapCredentialHelperBoundAndCancellation(t *testing.T) {
	var output bootstrapTokenBuffer
	if _, err := output.Write(make([]byte, 1<<20)); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("excess")); err == nil || output.Len() != 1<<20 {
		t.Fatal("credential buffer exceeded its bound", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	directory := t.TempDir()
	t.Setenv("PATH", directory)
	if err := os.WriteFile(filepath.Join(directory, "gh"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	value, err := bootstrapGitHubToken(ctx)
	if value != "" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("helper cancellation not propagated", err)
	}
}
