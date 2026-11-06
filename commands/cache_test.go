package commands

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestCacheDeleteCancellationNeverMutates(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	calls := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(_ *http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF })}
	input, writer := io.Pipe()
	defer func() { _ = input.Close(); _ = writer.Close() }()
	d.In = input
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	code := Execute(ctx, []string{"cache", "delete", "--repo", "rep-019a0000000070008000000000000002", "--all"}, d)
	if code != 130 || calls != 0 || !strings.Contains(out.String(), "Delete ALL cache entries") {
		t.Fatal(code, calls, out, stderr)
	}
}

func TestCacheWebOnlyResolvesOrganizationBeforeOpening(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.IsTerminal = func() bool { return true }
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	calls := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/users/me/organizations" {
			t.Error("unexpected cache fetch", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"data\":[{\"organization_id\":\"" + inspectionOrg + "\",\"slug\":\"acme\"}]}")), Request: r}, nil
	})}
	opened := ""
	d.OpenBrowser = func(_ context.Context, target string) error { opened = target; return nil }
	code := Execute(t.Context(), []string{"cache", "list", "--repo", "rep-019a0000000070008000000000000002", "--web"}, d)
	expected := "https://console.example.test/org/acme/caches/rep-019a0000000070008000000000000002"
	// API hostnames map to the matching console, as in Python.
	if code != 0 || calls != 1 || opened != expected || out.String() != "Opening: "+expected+"\n" {
		t.Fatal(code, calls, opened, out, stderr)
	}
}
