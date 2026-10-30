package commands

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
)

func TestVMWaitBoundsBackoffAndStopsPermanentErrors(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		status       int
		waits        []time.Duration
		calls        int
	}{
		{"capped", "90", 429, []time.Duration{30 * time.Second, 10 * time.Second}, 3},
		{"invalid header", "bad", 429, []time.Duration{3 * time.Second, 2 * time.Second}, 3},
		{"permanent", "", 403, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, stderr := testDeps(t)
			d = d.defaults()
			now := time.Unix(0, 0)
			d.Now = func() time.Time { return now }
			waits := []time.Duration{}
			d.Wait = func(_ context.Context, delay time.Duration) error {
				waits = append(waits, delay)
				now = now.Add(delay)
				return nil
			}
			cfg := config.Config{Host: "https://api.example.test", Token: "fixture"}
			calls := 0
			api := client.New(cfg, &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{tc.header}}, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
			})}, "test")
			timeout := 5
			if tc.name == "capped" {
				timeout = 40
			}
			_, disposition, err := waitForVM(t.Context(), d, cfg, api, "/vm", timeout, vmConnectable, false, nil)
			if calls != tc.calls || len(waits) != len(tc.waits) {
				t.Fatal(calls, waits, stderr)
			}
			for i := range waits {
				if waits[i] != tc.waits[i] {
					t.Fatal(waits)
				}
			}
			if tc.status == 403 {
				if err == nil {
					t.Fatal("permanent error swallowed")
				}
			} else if err != nil || disposition != "timeout" {
				t.Fatal(err, disposition)
			}
		})
	}
}
func TestVMLifecycleMutationIsNotRetried(t *testing.T) {
	for _, operation := range []string{"start", "stop", "pause", "resume", "delete"} {
		t.Run(operation, func(t *testing.T) {
			d, _, _ := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: "org-019a0000000070008000000000000001"}, nil
			}
			calls := 0
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("uncertain outcome") })}
			args := []string{"vm", operation, "cvm-019a0000000070008000000000000001", "--wait"}
			if operation == "delete" {
				args = append(args, "--yes")
			}
			if code := Execute(t.Context(), args, d); code != 1 || calls != 1 {
				t.Fatal(code, calls)
			}
		})
	}
}
func TestVMStartWaitCancellationPreservesPassword(t *testing.T) {
	d, out, stderr := testDeps(t)
	cfg := config.Config{Host: "https://api.example.test", Token: "fixture", Org: "org-019a0000000070008000000000000001"}
	d.LoadConfig = func() (config.Config, error) { return cfg, nil }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.Wait = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	calls := []string{}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method)
		body := `{"data":{"state":"PENDING"}}`
		if r.Method == http.MethodPatch {
			body = `{"data":{"vm":{"state":"PENDING"},"password":"fixture-one-time"}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	code := Execute(ctx, []string{"vm", "start", "cvm-019a0000000070008000000000000001", "--wait"}, d)
	if code != 130 || !strings.Contains(out.String(), "One-time password: fixture-one-time") || !strings.Contains(stderr.String(), "Interrupted.") || strings.Join(calls, ",") != "PATCH,GET" {
		t.Fatal(code, calls, out, stderr)
	}
}
