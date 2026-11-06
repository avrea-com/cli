package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"0.7.0", "0.6.0", true}, {"0.7.0", "0.7.0-rc.1", true},
		{"0.7.0", "0.7.0", false}, {"0.7.0", "0.8.0", false},
		{"0.10.0", "0.9.0", true}, {"0.8.0-rc.1", "0.7.0", false},
		{"garbage", "0.7.0", false}, {"0.8.0", "dev", false},
	} {
		if got := newer(tc.a, tc.b); got != tc.want {
			t.Errorf("%s > %s: %v", tc.a, tc.b, got)
		}
	}
}

func TestCacheRefreshAndFailures(t *testing.T) {
	count := 0
	body := `{"tag_name":"v0.8.0"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.Header.Get("Authorization") != "" {
			t.Error("credentials sent")
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	c := Checker{Version: "0.7.0", cachePath: filepath.Join(t.TempDir(), "update.json"), endpoint: server.URL, now: func() time.Time { return now }, executable: "/opt/homebrew/Cellar/avr/0.7.0/bin/avr"}
	if got := c.check(t.Context()); !strings.Contains(got, "0.8.0") || !strings.Contains(got, "brew upgrade avr") {
		t.Fatal(got)
	}
	_ = c.check(t.Context())
	if count != 1 {
		t.Fatal(count)
	}
	now = now.Add(maxAge)
	body = `{"tag_name":"v999.0.0","prerelease":true}`
	if got := c.check(t.Context()); strings.Contains(got, "999") {
		t.Fatal(got)
	}
	_ = c.check(t.Context())
	if count != 2 {
		t.Fatal(count)
	}
	now = now.Add(maxAge)
	body = `not json`
	_ = c.check(t.Context())
	_ = c.check(t.Context())
	if count != 3 {
		t.Fatal(count)
	}
	// Corruption is a cache miss, with a bounded replacement.
	if err := os.WriteFile(c.cachePath, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = c.check(t.Context())
	if count != 4 {
		t.Fatal(count)
	}
	data, err := os.ReadFile(c.cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var value cache
	if json.Unmarshal(data, &value) != nil || !value.Checked.Equal(now) {
		t.Fatal(string(data))
	}
}

func TestCancelledCheckDoesNotDelayCaller(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	c := Checker{Version: "0.7.0", cachePath: filepath.Join(t.TempDir(), "cache"), endpoint: server.URL}
	ctx, cancel := context.WithCancel(t.Context())
	notices := c.Start(ctx)
	<-started
	// The process may disappear now: the cache must already suppress a retry.
	data, err := os.ReadFile(c.cachePath)
	var saved cache
	if err != nil || json.Unmarshal(data, &saved) != nil || saved.Checked.IsZero() {
		t.Fatal("attempt was not saved before the request", err, string(data))
	}
	cancel()
	if notice, ok := <-notices; ok {
		t.Fatal(notice)
	}
	if notice := c.check(t.Context()); notice != "" {
		t.Fatal(notice)
	}
}

func TestInstructions(t *testing.T) {
	for path, want := range map[string]string{
		"/opt/homebrew/Cellar/avr/0.7.0/bin/avr":                               "brew upgrade",
		"/home/user/.local/share/mise/installs/github-avrea-com-cli/0.7.0/avr": "mise upgrade",
		"/home/user/.local/share/uv/tools/avr-cli/bin/avr":                     "uv tool upgrade",
		"/home/user/.local/share/pipx/venvs/avr-cli/bin/avr":                   "pipx upgrade",
		"/custom/avr": "Install instructions:",
	} {
		if got := instruction(path); !strings.Contains(got, want) {
			t.Fatal(path, got)
		}
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "share", "avr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "share", "avr", "install-method"), []byte("pypi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(instruction(filepath.Join(dir, "bin", "avr")), "python -m pip") {
		t.Fatal("missing pip instruction")
	}
}
