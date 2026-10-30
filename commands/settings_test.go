package commands

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestSettingsCancellationAfterResolutionDoesNotMutate(t *testing.T) {
	for _, args := range [][]string{{"set", "cache.gha.enabled", "false"}, {"reset", "cache.gha.enabled"}} {
		d, _, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/repos/resolve") {
				t.Fatal("unexpected mutation", r.Method, r.URL)
			}
			cancel()
			return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader("{\"data\":{\"repository_id\":\"rep-019a0000000070008000000000000002\",\"full_name\":\"acme/web\"}}"))}, nil
		})}
		code := Execute(ctx, append([]string{"settings"}, append(args, "--repo", "acme/web")...), d)
		cancel()
		if code != 130 || calls != 1 {
			t.Fatal(code, calls, stderr)
		}
	}
}
func TestSettingsExplicitOrgIgnoresAmbientRepository(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg, Repo: "acme/wrong"}, nil
	}
	d.GitRemote = func(context.Context) (string, error) { t.Fatal("explicit organization consulted Git"); return "", nil }
	calls := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodPut || r.URL.Path != "/orgs/"+inspectionOrg+"/settings" {
			t.Fatal(r.Method, r.URL)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || !strings.Contains(string(body), "\x22value\x22:9007199254740993") {
			t.Fatal(string(body), err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader("{\"key\":\"runner.value\",\"value\":9007199254740993,\"source\":\"organization\"}"))}, nil
	})}
	code := Execute(t.Context(), []string{"settings", "set", "runner.value", "9007199254740993", "--org", inspectionOrg}, d)
	if code != 0 || calls != 1 || !strings.Contains(out.String(), "9007199254740993 (org)") {
		t.Fatal(code, calls, out, stderr)
	}
}
func TestSettingsSchemaNeedsNoOrganizationOrRepository(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture"}, nil
	}
	d.GitRemote = func(context.Context) (string, error) { t.Fatal("schema consulted Git"); return "", nil }
	calls := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/settings/schema" {
			t.Fatal(r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader("[]"))}, nil
	})}
	code := Execute(t.Context(), []string{"settings", "schema", "--json", "*"}, d)
	if code != 0 || calls != 1 || strings.TrimSpace(out.String()) != "[]" {
		t.Fatal(code, calls, out, stderr)
	}
}
