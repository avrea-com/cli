package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

const inspectionOrg = "org-019a0000000070008000000000000001"
const inspectionRun = "run-019a0000000070008000000000000003"
const inspectionJob = "job-019a0000000070008000000000000005"

func TestRunReferenceFormsAndBoundaries(t *testing.T) {
	for _, raw := range []string{inspectionRun, "42", "00042", "https://github.com/acme/web/actions/runs/42", "https://github.com/acme/web/actions/runs/42/attempts/2?check_suite_focus=true", "https://console.staging.avrea.com/org/acme/runs/" + inspectionRun} {
		if _, err := parseRunReference(raw, "https://api.staging.avrea.com"); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{"run-legacy", "0", "-1", "４２", "https://github.com:443/acme/web/actions/runs/42", "https://user@github.com/acme/web/actions/runs/42", "https://github.com/acme/web/actions/runs/42#fragment", "https://github.com.evil.test/acme/web/actions/runs/42", "https://github.com/acme%2Fweb/other/actions/runs/42", "https://github.com/acme/web/actions/runs/42/attempts/0", "https://console.avrea.com/org/acme/runs/" + inspectionRun, "https://console.other.test/org/acme/runs/" + inspectionRun} {
		if _, err := parseRunReference(raw, "https://api.staging.avrea.com"); err == nil {
			t.Errorf("accepted invalid/environment-crossing reference %q", raw)
		}
	}
	ref, err := parseRunReference("0009007199254740993", "https://api.avrea.com")
	if err != nil || ref.platformID != "9007199254740993" {
		t.Fatal(ref, err)
	}
}

func TestWatchMultipleJobsTransitionsAndFinalPayload(t *testing.T) {
	d, out, stderr := testDeps(t)
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include") != "jobs" {
			t.Error(r.URL)
		}
		polls++
		jobs := []map[string]any{
			{"job_id": inspectionJob, "platform_job_id": int64(9007199254740993), "job_name": "Unit", "state": "in_progress", "conclusion": nil},
			{"job_id": "job-019a0000000070008000000000000006", "job_name": "Lint", "state": "queued"},
		}
		state := "in_progress"
		if polls >= 3 {
			jobs[0]["state"] = "completed"
			jobs[0]["conclusion"] = "success"
			jobs[1]["state"] = "completed"
			jobs[1]["conclusion"] = "skipped"
			state = "completed"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"run_id": inspectionRun, "status": state, "conclusion": "success", "duration_seconds": 12.125, "jobs": jobs}})
	}))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	d.Wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	d.Now = func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }
	if code := Execute(t.Context(), []string{"run", "watch", inspectionRun, "--exit-status"}, d); code != 0 {
		t.Fatal(code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{"job_started", "job_completed", "job_completed", "run_completed"}
	if len(lines) != len(want) || polls != 3 {
		t.Fatal(polls, out)
	}
	for i, line := range lines {
		var event map[string]any
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event["event"] != want[i] || event["run_id"] != inspectionRun || event["timestamp"] != "2026-10-29T12:00:00Z" {
			t.Fatal(event)
		}
		if i == 0 && event["conclusion"] != nil {
			t.Fatal("unfinished job conclusion is not null")
		}
		if i == 0 && event["platform_job_id"] != json.Number("9007199254740993") {
			t.Fatal("platform ID lost precision", event)
		}
		if i == 3 {
			if _, ok := event["avrea_job_id"]; ok {
				t.Fatal("job fields on run event")
			}
			if event["duration_seconds"] != json.Number("12.125") {
				t.Fatal(event)
			}
		}
	}
}

func TestWatchCancellationRestoresTerminal(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprint(terminal), func(t *testing.T) {
			d, out, stderr := testDeps(t)
			d.IsTerminal = func() bool { return terminal }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, `{"data":{"run_id":%q,"status":"in_progress","jobs":[]}}`, inspectionRun)
			}))
			defer server.Close()
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: server.URL, Token: "avs_fixture", Org: inspectionOrg}, nil
			}
			d.Wait = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
			if code := Execute(ctx, []string{"--no-links", "run", "watch", inspectionRun}, d); code != 130 {
				t.Fatal(code, stderr)
			}
			if terminal && (!strings.Contains(out.String(), "No jobs yet.") || !strings.Contains(out.String(), "\x1b[23;0t\x1b[?25h") || !strings.HasSuffix(out.String(), "\nStopped watching.\n")) {
				t.Fatal("terminal state not restored", out)
			}
			if !terminal && (strings.Contains(out.String(), "\x1b") || stderr.String() != "\nAborted!\n") {
				t.Fatal("stream interruption diagnostic", out, stderr)
			}
		})
	}
}

func TestWatchCancellationDuringRequest(t *testing.T) {
	d, _, stderr := testDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { cancel(); <-r.Context().Done() }))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	if code := Execute(ctx, []string{"run", "watch", inspectionRun}, d); code != 130 {
		t.Fatal(code, stderr)
	}
}

func TestWatchSleepCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := waitPoll(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// A connection failure must remain retryable after the transport sanitizes the
// error text; the underlying URL may contain credentials and must not leak.
func TestWatchRecoversConnectionFailure(t *testing.T) {
	d, out, stderr := testDeps(t)
	calls := 0
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("private-url-must-not-leak")}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"data":{"run_id":%q,"status":"completed","conclusion":"success"}}`, inspectionRun))), Request: r}, nil
	})}
	d.Wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	if code := Execute(t.Context(), []string{"run", "watch", inspectionRun}, d); code != 0 {
		t.Fatal(code, stderr)
	}
	if calls != 2 || !strings.Contains(out.String(), `"reason":"ConnectError"`) {
		t.Fatal(calls, out, stderr)
	}
	if strings.Contains(out.String()+stderr.String(), "private-url") {
		t.Fatal("transport secret leaked")
	}
}

type watchTransportFunc func(*http.Request) (*http.Response, error)

func (f watchTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestConsoleURLMatchesPythonEnvironmentMapping(t *testing.T) {
	for host, want := range map[string]string{"https://api.avrea.com": "https://console.avrea.com", "https://api.staging.avrea.com": "https://console.staging.avrea.com", "http://127.0.0.1:6001": "http://127.0.0.1"} {
		if got := consoleURL(host); got != want {
			t.Errorf("%s: %s != %s", host, got, want)
		}
	}
}
