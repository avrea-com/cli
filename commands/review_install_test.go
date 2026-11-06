package commands

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
)

func TestLoginAuthErrorCompletesWithoutExchange(t *testing.T) {
	for _, reason := range []string{"signup_blocked", "identity_conflict", "provider_not_available", "bad\x1b[31mvalue"} {
		t.Run(reason, func(t *testing.T) {
			d, _, stderr := testDeps(t)
			d.LoginTimeout = time.Second
			d.LoadConfig = func() (config.Config, error) { return config.Config{Host: "https://api.test"}, nil }
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) {
				t.Error("failed authentication attempted API exchange")
				return nil, errors.New("unexpected API request")
			})}
			listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			d.Listen = func(context.Context, string, string) (net.Listener, error) { return listener, nil }
			d.OpenBrowser = func(ctx context.Context, target string) error {
				opened, _ := url.Parse(target)
				cb, _ := url.Parse(opened.Query().Get("next"))
				for _, state := range []string{"wrong", cb.Query().Get("cli_state")} {
					query := url.Values{"cli_state": {state}, "auth_error": {reason}}
					req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+listener.Addr().String()+"/callback?"+query.Encode(), nil)
					req.Host = "127.0.0.1:8765"
					response, err := http.DefaultClient.Do(req)
					if err != nil {
						return err
					}
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
					want := 200
					if state == "wrong" {
						want = 400
					}
					if response.StatusCode != want {
						t.Errorf("callback: %d, want %d", response.StatusCode, want)
					}
				}
				return nil
			}
			if code := Execute(t.Context(), []string{"auth", "login"}, d); code != 1 || !strings.Contains(stderr.String(), "authentication failed:") || strings.Contains(stderr.String(), "timed out") || strings.Contains(stderr.String(), "\x1b") {
				t.Fatal(code, stderr)
			}
			if _, err := os.Stat(d.Store.Path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("stored failed login", err)
			}
		})
	}
}

func TestSuccessfulReturnAfterCancellationStillFails(t *testing.T) {
	d, _, stderr := testDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	root := NewRoot(d)
	root.AddCommand(&cobra.Command{Use: "swallowed", RunE: func(*cobra.Command, []string) error { cancel(); return nil }})
	if code := ExecuteRoot(ctx, root, []string{"swallowed"}, stderr, d); code != 130 {
		t.Fatal(code, stderr)
	}
}

type closedReviewOutput struct{}

func (closedReviewOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestUnreachableHealthWithClosedJSONOutputFails(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.Out = closedReviewOutput{}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	if code := Execute(t.Context(), []string{"health", "--json", "status"}, d); code != 1 {
		t.Fatal(code, stderr)
	}
}

func TestArtifactPublicationWithoutHardLinks(t *testing.T) {
	for _, mode := range []string{"copy", "exists", "cancel", "read-error"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			source, target := filepath.Join(dir, "source"), filepath.Join(dir, "target")
			if err := os.WriteFile(source, []byte("verified payload"), 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "exists" {
				if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "read-error" {
				source = dir
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			err := publishArtifact(ctx, source, target, func(string, string) error { return os.ErrPermission })
			data, readErr := os.ReadFile(target)
			switch mode {
			case "copy":
				if err != nil || string(data) != "verified payload" {
					t.Fatal(err, string(data))
				}
			case "exists":
				if !errors.Is(err, os.ErrExist) || string(data) != "keep" {
					t.Fatal(err, string(data))
				}
			default:
				if err == nil || !errors.Is(readErr, os.ErrNotExist) {
					t.Fatal(err, readErr)
				}
			}
		})
	}
}

func TestReplayDisplayPreservesJSONAndLabelsEscapedArguments(t *testing.T) {
	inputs, err := nativeInputs(nil, []string{"text=line\r\x1b\u0085"}, map[string]string{"text": "string"})
	if err != nil {
		t.Fatal(err)
	}
	retry := "printf '%s' " + shellQuotePython(inputs) + " | avr workflow run build --idempotency-key fixture"
	if got := dispatchRetryMessage("Retry the exact request:", retry); got != "Retry the exact request:\n"+retry {
		t.Fatal(got)
	}
	unsafe := retry + " --ref 'bad\x1b[31mref'"
	for _, got := range []string{dispatchRetryMessage("Retry the exact request:", unsafe), (nativeReceipt{id: "fixture", retry: unsafe}).pendingMessage()} {
		if strings.Contains(got, "\x1b") || !strings.Contains(got, "escaped control characters") {
			t.Fatal(got)
		}
	}
	if got := metricRate(1, "ops\x1b[31m"); strings.Contains(got, "\x1b") {
		t.Fatal(got)
	}
}

func TestGitLabRunnerDetailsEscapeUntrustedValues(t *testing.T) {
	d, out, _ := testDeps(t)
	collection := map[string]any{"data": []any{map[string]any{"run_untagged": "bad\x1b[31m", "paused": "bad\u202e"}}}
	if err := renderGitLabRunners(d, collection); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b\u202e") || !strings.Contains(out.String(), "run_untagged=bad") {
		t.Fatal(out)
	}
}

func TestCloneHelperRetainsStableLauncherAcrossUpgrade(t *testing.T) {
	dir := t.TempDir()
	old, newer, launcher := filepath.Join(dir, "version-one"), filepath.Join(dir, "version-two"), filepath.Join(dir, "avr")
	for _, path := range []string{old, newer} {
		if err := os.WriteFile(path, []byte(path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(old, launcher); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	got := cloneHelperExecutable(old, launcher)
	if got != launcher {
		t.Fatal(got)
	}
	if err := os.Remove(launcher); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(newer, launcher); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(got)
	if err != nil || string(data) != newer {
		t.Fatal(err, string(data))
	}
	if other := cloneHelperExecutable(newer, dir+"/unrelated"); other != newer {
		t.Fatal(other)
	}
}

func TestCloneHelperFollowsVersionedPATHSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	dir := t.TempDir()
	old, newer := filepath.Join(dir, "old", "avr"), filepath.Join(dir, "new", "avr")
	for _, path := range []string{old, newer} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho upgraded\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", filepath.Dir(old))
	helper := cloneHelperExecutable(old, old)
	if helper != "avr" {
		t.Fatal(helper)
	}
	t.Setenv("PATH", filepath.Dir(newer))
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	result, err := exec.CommandContext(t.Context(), helper).Output()
	if err != nil || string(result) != "upgraded\n" {
		t.Fatal(string(result), err)
	}
}
