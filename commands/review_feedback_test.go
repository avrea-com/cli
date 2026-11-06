package commands

import (
	"context"
	"fmt"
	"github.com/avrea-com/cli/config"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPRScopeAPIContract(t *testing.T) {
	for _, scope := range []string{"", "all", "authored", "involved", "rest", "attention"} {
		t.Run(scope, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			d.GitRemote = func(context.Context) (string, error) { return "", nil }
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
			}
			listed := false
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				body := `{"enabled":true}`
				if strings.HasSuffix(r.URL.Path, "/pull-requests") {
					listed = true
					want := scope
					if want == "all" {
						want = ""
					}
					if got := r.URL.Query().Get("scope"); got != want || (want == "" && r.URL.Query().Has("scope")) {
						t.Errorf("scope query %v, want %q", r.URL.Query(), want)
					}
					body = `{"data":[]}`
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			args := []string{"pr", "list"}
			if scope != "" {
				args = append(args, "--scope", scope)
			}
			if code := Execute(t.Context(), args, d); code != 0 || !listed {
				t.Fatal(code, out, stderr)
			}
		})
	}
	for _, state := range []string{"closed", "merged", "all"} {
		d, _, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) {
			t.Fatal("invalid attention filter reached config/API")
			return config.Config{}, nil
		}
		if code := Execute(t.Context(), []string{"pr", "list", "--scope", "attention", "--state", state}, d); code != 2 || !strings.Contains(stderr.String(), "open") {
			t.Fatal(code, stderr)
		}
	}
}

func TestCachePurgeReportsAsyncStatus(t *testing.T) {
	for _, tc := range []struct {
		body, want string
		key        bool
	}{
		{`{"purge":{"state":"queued"}}`, "Cache purge queued", false},
		{`{"purge":{"state":"running"}}`, "Cache purge running", false},
		{`{"purge":null}`, "Cache purge requested", false},
		{`{"deleted_count":1}`, "Deleted 1 cache entry.", true},
	} {
		t.Run(tc.want, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
			}
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "DELETE" {
					t.Fatal(r.Method, r.URL)
				}
				return &http.Response{StatusCode: 202, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})}
			args := []string{"cache", "delete", "--repo", "rep-019a0000000070008000000000000002", "--yes"}
			if tc.key {
				args = append(args, "--key", "fixture", "--type", "actions")
			} else {
				args = append(args, "--all")
			}
			if code := Execute(t.Context(), args, d); code != 0 || !strings.Contains(out.String(), tc.want) {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestCacheCursorPagination(t *testing.T) {
	for _, mode := range []string{"terminal", "pipe", "json"} {
		t.Run(mode, func(t *testing.T) {
			for _, cursor := range []string{"", "next +/"} {
				d, out, stderr := testDeps(t)
				d.IsTerminal = func() bool { return mode == "terminal" }
				d.LoadConfig = func() (config.Config, error) {
					return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
				}
				d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Query().Get("after") != cursor || r.URL.Query().Has("offset") {
						t.Error(r.URL)
					}
					next := "null"
					if cursor == "" {
						next = `"next +/"`
					}
					body := fmt.Sprintf(`{"data":[{"key":"fixture"}],"streaming":true,"total":10000,"next_cursor":%s}`, next)
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})}
				args := []string{"cache", "list", "--repo", "rep-019a0000000070008000000000000002"}
				if cursor != "" {
					args = append(args, "--cursor", cursor)
				}
				if mode == "json" {
					args = append(args, "--json", "key")
				}
				if code := Execute(t.Context(), args, d); code != 0 || !strings.Contains(out.String(), "fixture") {
					t.Fatal(code, out, stderr)
				}
				if (cursor == "") != strings.Contains(stderr.String(), "--cursor 'next +/'") || strings.Contains(stderr.String(), "/10000") || strings.Contains(stderr.String(), "--offset") {
					t.Fatal(stderr)
				}
			}
		})
	}
	d, _, stderr := testDeps(t)
	if code := Execute(t.Context(), []string{"cache", "list", "--cursor", "next", "--offset", "0"}, d); code != 2 {
		t.Fatal(code, stderr)
	}
}

func TestSSHConfigApostrophePathsParse(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("OpenSSH unavailable")
	}
	block, err := vmSSHConfigBlock("fixture", "127.0.0.1", "user", 22, "/tmp/leo's/key", "/tmp/leo's/known hosts")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(block), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), ssh, "-G", "-F", path, "fixture")
	result, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(result), "identityfile /tmp/leo's/key") || !strings.Contains(string(result), "/tmp/leo's/known hosts") {
		t.Fatal(err, string(result))
	}
}

func TestPRAttentionEmptyPageRetainsCursor(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		d, out, stderr := testDeps(t)
		d.GitRemote = func(context.Context) (string, error) { return "", nil }
		d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
		}
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
			body := `{"enabled":true}`
			if strings.HasSuffix(r.URL.Path, "/pull-requests") {
				body = `{"data":[],"pagination":{"next_cursor":"next-page"}}`
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
		args := []string{"pr", "list", "--scope", "attention"}
		if jsonOutput {
			args = append(args, "--json", "number")
		}
		if code := Execute(t.Context(), args, d); code != 0 || !strings.Contains(stderr.String(), "--cursor next-page") {
			t.Fatal(code, out, stderr)
		}
		if jsonOutput && strings.TrimSpace(out.String()) != "[]" {
			t.Fatal(out)
		}
	}
}
