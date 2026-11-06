package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
)

func TestExplicitInterruptMessageIsPreserved(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stderr strings.Builder
	root := &cobra.Command{Use: "fixture", SilenceErrors: true, SilenceUsage: true, RunE: func(*cobra.Command, []string) error { cancel(); return &ExitError{Code: 130} }}
	code := ExecuteRoot(ctx, root, nil, &stderr)
	if code != 130 || stderr.Len() != 0 {
		t.Fatal(code, stderr.String())
	}
}

func TestJobWatchNetworkRetrySnapshotsAndCancellation(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.GitRemote = func(context.Context) (string, error) { return "", nil }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	calls, waits := 0, 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("limit") != "50" || strings.Join(r.URL.Query()["states"], ",") != "queued,in_progress" {
			t.Error(r.URL)
		}
		if calls == 1 {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("sensitive-network-address")}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"job_id":"job-fixture","platform_job_id":9007199254740993,"state":"queued"}]}`)), Request: r}, nil
	})}
	d.Wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits == 3 {
			cancel()
		}
		return ctx.Err()
	}
	code := Execute(ctx, []string{"job", "watch"}, d)
	if code != 130 || calls != 3 || waits != 3 {
		t.Fatal(code, calls, waits, stderr)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 || lines[2] != "" || lines[3] != "Stopped watching." {
		t.Fatal(out.String())
	}
	for _, line := range lines[:2] {
		var snapshot map[string]any
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&snapshot); err != nil {
			t.Fatal(err)
		}
		row := snapshot["data"].([]any)[0].(map[string]any)
		if row["platform_job_id"] != json.Number("9007199254740993") {
			t.Fatal(snapshot)
		}
	}
	if strings.Contains(out.String()+stderr.String(), "sensitive-network-address") {
		t.Fatal("unsanitized transport diagnostics")
	}
}

func TestJobWatchCancellationDuringRequest(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.GitRemote = func(context.Context) (string, error) { return "", nil }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { cancel(); <-r.Context().Done() }))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	if code := Execute(ctx, []string{"job", "watch"}, d); code != 130 {
		t.Fatal(code, stderr)
	}
}
