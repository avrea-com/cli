package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
)

const sbomTestTask = "atk-019a0000000070008000000000000010"
const sbomTestRepo = "rep-019a0000000070008000000000000002"

func sbomTestResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func sbomTestConfig(d *Dependencies) {
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
}
func TestSBOMFailedAnalysisCanDeliverLater(t *testing.T) {
	d, out, stderr := testDeps(t)
	sbomTestConfig(&d)
	now := time.Unix(100, 0)
	d.Now = func() time.Time { return now }
	waits := []time.Duration{}
	d.Wait = func(ctx context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		now = now.Add(delay)
		return ctx.Err()
	}
	posts, snapshots, states := 0, 0, 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch {
		case r.Method == http.MethodPost:
			posts++
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["scope"] != "sbom" || payload["ref"] != strings.Repeat("a", 40) || snapshots != 1 {
				t.Fatal(payload, snapshots)
			}
			body = fmt.Sprintf("{\"ai_task_id\":%q,\"status\":\"generating\"}", sbomTestTask)
		case strings.HasSuffix(r.URL.Path, "/state"):
			states++
			if r.URL.RawQuery != "scope=sbom" {
				t.Fatal(r.URL)
			}
			body = fmt.Sprintf("{\"ai_task_id\":%q,\"status\":\"FAILED\",\"error_code\":\"ANALYSIS_FAILED\"}", sbomTestTask)
		default:
			snapshots++
			if snapshots < 5 {
				return sbomTestResponse(r, 404, "{}"), nil
			}
			body = fmt.Sprintf("{\"commit_sha\":%q,\"recorded_at\":\"2026-09-01T11:00:00Z\"}", strings.Repeat("a", 40))
		}
		return sbomTestResponse(r, 200, body), nil
	})}
	code := Execute(t.Context(), []string{"sbom", "generate", strings.Repeat("A", 40), "--repo", sbomTestRepo, "--wait", "--wait-timeout", "30"}, d)
	if code != 0 || posts != 1 || states != 1 || snapshots != 5 || !slices.Equal(waits, []time.Duration{5 * time.Second, 5 * time.Second, 5 * time.Second}) {
		t.Fatal(code, posts, states, snapshots, waits, stderr)
	}
	if !strings.Contains(stderr.String(), "failed (ANALYSIS_FAILED) after producing the SBOM") || !strings.Contains(out.String(), "SBOM generated for aaaaaaaaaaaa.") {
		t.Fatal(out, stderr)
	}
}
func TestSBOMDeadlinePollAndBackoff(t *testing.T) {
	d, _, stderr := testDeps(t)
	sbomTestConfig(&d)
	now := time.Unix(100, 0)
	start := now
	d.Now = func() time.Time { return now }
	waits := []time.Duration{}
	d.Wait = func(ctx context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		now = now.Add(delay)
		return ctx.Err()
	}
	polls, posts := 0, 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			posts++
			return sbomTestResponse(r, 200, fmt.Sprintf("{\"ai_task_id\":%q,\"status\":\"generating\"}", sbomTestTask)), nil
		}
		polls++
		remaining := max(35*time.Second-now.Sub(start), time.Second)
		if remaining > 30*time.Second {
			remaining = 30 * time.Second
		}
		deadline, ok := r.Context().Deadline()
		// The context uses a real monotonic clock; allow only execution overhead.
		if !ok || time.Until(deadline) > remaining || time.Until(deadline) < remaining-time.Second {
			t.Fatalf("request budget %v, expected %v", time.Until(deadline), remaining)
		}
		response := sbomTestResponse(r, 429, "{}")
		response.Header.Set("Retry-After", "99999999999999999999999")
		return response, nil
	})}
	code := Execute(t.Context(), []string{"sbom", "generate", "--repo", sbomTestRepo, "--wait", "--wait-timeout", "35"}, d)
	if code != 1 || posts != 1 || polls != 3 || !slices.Equal(waits, []time.Duration{30 * time.Second, 5 * time.Second}) || !strings.Contains(stderr.String(), "last error: HTTP 429") || strings.Contains(stderr.String(), "Stopped waiting") {
		t.Fatal(code, posts, polls, waits, stderr)
	}
}
func TestSBOMGeneratePOSTIsNeverRetried(t *testing.T) {
	d, _, stderr := testDeps(t)
	sbomTestConfig(&d)
	posts := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) { posts++; return sbomTestResponse(r, 503, "{}"), nil })}
	d.Wait = func(context.Context, time.Duration) error { t.Fatal("generation retried"); return nil }
	code := Execute(t.Context(), []string{"sbom", "generate", "--repo", sbomTestRepo, "--wait"}, d)
	if code != 1 || posts != 1 || strings.Contains(stderr.String(), "Stopped waiting") {
		t.Fatal(code, posts, stderr)
	}
}
func TestSBOMWaitCancellationNamesTaskOnce(t *testing.T) {
	d, _, stderr := testDeps(t)
	sbomTestConfig(&d)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			return sbomTestResponse(r, 200, fmt.Sprintf("{\"ai_task_id\":%q,\"status\":\"generating\"}", sbomTestTask)), nil
		}
		cancel()
		return nil, ctx.Err()
	})}
	code := Execute(ctx, []string{"sbom", "generate", "--repo", sbomTestRepo, "--wait"}, d)
	if code != 130 || strings.Count(stderr.String(), "Stopped waiting") != 1 || !strings.Contains(stderr.String(), sbomTestTask) {
		t.Fatal(code, stderr)
	}
}
func TestSBOMReadTimeoutIsTransientButParentCancellationIsNot(t *testing.T) {
	d, _, stderr := testDeps(t)
	d = d.defaults()
	now := time.Now()
	d.Now = func() time.Time { return now }
	api := client.New(config.Config{Host: "https://api.example.test", Token: "fixture"}, &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		return nil, &client.TransportError{Message: "fixture", Reason: "ReadTimeout"}
	})}, "test")
	poller := sbomPoller{d: d, api: api, deadline: now.Add(time.Minute), taskID: sbomTestTask}
	_, err := poller.get(t.Context(), "/state", "check SBOM generation status", false)
	var transient *sbomTransient
	if !errors.As(err, &transient) || transient.backoff != 5*time.Second || !strings.Contains(stderr.String(), "retrying") {
		t.Fatal(err, stderr)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = poller.get(ctx, "/state", "check SBOM generation status", false)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestSBOMRetryAfterValues(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"0", 0}, {" 12 ", 12 * time.Second}, {"31", 30 * time.Second}, {"99999999999999999999999", 30 * time.Second},
		{"-1", 5 * time.Second}, {"1.5", 5 * time.Second}, {"Wed, 21 Oct 2015 07:28:00 GMT", 5 * time.Second},
	} {
		if got := sbomRetryBackoff(tc.value); got != tc.want {
			t.Errorf("%q: %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestSBOMWaitJQErrorDoesNotAnnounceStoppedWait(t *testing.T) {
	d, _, stderr := testDeps(t)
	sbomTestConfig(&d)
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		status := "COMPLETED"
		if r.Method == http.MethodPost {
			status = "generating"
		}
		return sbomTestResponse(r, 200, fmt.Sprintf("{\"ai_task_id\":%q,\"status\":%q}", sbomTestTask, status)), nil
	})}
	code := Execute(t.Context(), []string{"sbom", "generate", "--repo", sbomTestRepo, "--wait", "--json", "status", "--jq", "." + "["}, d)
	if code != 1 || !strings.Contains(stderr.String(), "jq error") || strings.Contains(stderr.String(), "Stopped waiting") {
		t.Fatal(code, stderr)
	}
}

type sbomTruncatedBody struct{}

func (sbomTruncatedBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (sbomTruncatedBody) Close() error             { return nil }
func TestSBOMTruncatedPollResponseIsRetried(t *testing.T) {
	d, out, stderr := testDeps(t)
	sbomTestConfig(&d)
	now := time.Unix(100, 0)
	d.Now = func() time.Time { return now }
	d.Wait = func(ctx context.Context, delay time.Duration) error { now = now.Add(delay); return ctx.Err() }
	polls, posts := 0, 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			posts++
			return sbomTestResponse(r, 200, fmt.Sprintf("{\"ai_task_id\":%q,\"status\":\"generating\"}", sbomTestTask)), nil
		}
		polls++
		if polls == 1 {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: sbomTruncatedBody{}, Request: r}, nil
		}
		return sbomTestResponse(r, 200, fmt.Sprintf("{\"ai_task_id\":%q,\"status\":\"COMPLETED\"}", sbomTestTask)), nil
	})}
	code := Execute(t.Context(), []string{"sbom", "generate", "--repo", sbomTestRepo, "--wait"}, d)
	if code != 0 || posts != 1 || polls != 2 || !strings.Contains(stderr.String(), "RemoteProtocolError") || !strings.Contains(out.String(), "Analysis completed.") {
		t.Fatal(code, posts, polls, out, stderr)
	}
}
