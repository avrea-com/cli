package commands

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

func TestStatusAuxiliaryTransportFailureKeepsMainView(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.GitRemote = func(context.Context) (string, error) { return "", nil }
	d.Now = func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	aggregates := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/aggregate") {
			aggregates++
			if r.URL.Query().Get("created_after") != "2026-10-23T12:00:00.000000+00:00" && r.URL.Query().Get("created_after") != "2026-10-16T12:00:00.000000+00:00" {
				t.Error(r.URL)
			}
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: io.ErrUnexpectedEOF}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"data\":[]}")), Request: r}, nil
	})}
	if code := Execute(t.Context(), []string{"status", "--json"}, d); code != 0 || aggregates != 4 {
		t.Fatal(code, aggregates, stderr)
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report["runs"].([]any)) != 0 || len(report["slowest_jobs"].([]any)) != 0 {
		t.Fatal(report)
	}
	if strings.Count(stderr.String(), "ConnectError") != 4 || strings.Contains(stderr.String(), "unexpected EOF") {
		t.Fatal(stderr)
	}
}

func TestStatusCancellationFailsInsteadOfRenderingPartialData(t *testing.T) {
	d, out, stderr := testDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.GitRemote = func(context.Context) (string, error) { return "", nil }
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/aggregate") {
			cancel()
			return nil, ctx.Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"data\":[]}")), Request: r}, nil
	})}
	if code := Execute(ctx, []string{"status", "--json"}, d); code != 130 || out.Len() != 0 {
		t.Fatal(code, out, stderr)
	}
}
