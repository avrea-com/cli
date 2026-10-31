package commands

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

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
