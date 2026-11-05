package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBrowserArguments(t *testing.T) {
	target := "https://api.test/oauth/github/login?next=http://localhost:8765/callback&literal=$(touch)"
	for _, tt := range []struct {
		command string
		want    []string
	}{
		{`"C:\Program Files\Browser\browser.exe" --new-tab "%s"`, []string{`C:\Program Files\Browser\browser.exe`, "--new-tab", target}},
		{`browser --flag %s`, []string{"browser", "--flag", target}},
		{`C:\Tools\chrome.exe`, []string{`C:\Tools\chrome.exe`, target}},
		{`C:\Program Files\Browser\browser.exe`, []string{`C:\Program Files\Browser\browser.exe`, target}},
		{`/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`, []string{`/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`, target}},
		{`browser 'literal $HOME $(touch file)' %s`, []string{"browser", "literal $HOME $(touch file)", target}},
	} {
		args, err := browserArgs(tt.command, target)
		if err != nil || !reflect.DeepEqual(args, tt.want) {
			t.Fatalf("%q: %#v %v", tt.command, args, err)
		}
	}
	if _, err := browserArgs(`browser "unterminated`, target); err == nil {
		t.Fatal("accepted malformed browser command")
	}
}

func TestBrowserWholeExecutablePathAndDisplayFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "browser with spaces")
	if err := os.WriteFile(path, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	args, err := browserArgs(path, "https://example.test")
	if err != nil || args[0] != path || len(args) != 2 {
		t.Fatal(args, err)
	}
	d, _, stderr := testDeps(t)
	d.OpenBrowser = func(context.Context, string) error { return errors.New("private launcher details") }
	if err := d.openWeb(t.Context(), "https://example.test"); err != nil || !strings.Contains(stderr.String(), "open the URL") || strings.Contains(stderr.String(), "private") {
		t.Fatal(err, stderr)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if !errors.Is(d.openWeb(ctx, "https://example.test"), context.Canceled) {
		t.Fatal("lost cancellation")
	}
}

func TestJQHasNoExecutableDependency(t *testing.T) {
	t.Setenv("PATH", "")
	d, out, stderr := testDeps(t)
	d.Getenv = func(key string) string {
		if key == "AVR_TOKEN" {
			return "avs_fixture"
		}
		return ""
	}
	if code := Execute(context.Background(), []string{"auth", "status", "--json", "credential_type", "--jq", ".credential_type"}, d); code != 0 {
		t.Fatal(code, stderr)
	}
	if strings.TrimSpace(out.String()) != "scoped_token" {
		t.Fatal(out)
	}
}

func TestDefaultBrowserStartsWithoutWaitingForWindow(t *testing.T) {
	if directory := os.Getenv("AVR_TEST_BROWSER_CHILD"); directory != "" {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(directory, "release")); err == nil {
				_ = os.WriteFile(filepath.Join(directory, "finished"), nil, 0600)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("parent did not release browser fixture")
	}
	directory := t.TempDir()
	t.Setenv("AVR_TEST_BROWSER_CHILD", directory)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.WriteFile(filepath.Join(directory, "release"), nil, 0600)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(directory, "finished")); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Error("browser fixture did not finish")
	}()
	done := make(chan error, 1)
	go func() {
		done <- startBrowser(t.Context(), []string{executable, "-test.run=^TestDefaultBrowserStartsWithoutWaitingForWindow$"})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opener waits for browser exit")
	}
}
