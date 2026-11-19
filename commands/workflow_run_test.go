package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDispatchMatching(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	row := func(created, workflow, branch, runID string) map[string]any {
		return map[string]any{"created_at": created, "platform_workflow_id": workflow, "workflow_path": ".github/workflows/build.yml", "head_branch": branch, "platform_run_id": json.Number(runID), "event": "workflow_dispatch"}
	}
	pushed := row("2026-10-29T12:00:01Z", "42", "main", "1")
	pushed["event"] = "push"
	byFile := dispatchTarget{at: now, file: "build.yml", ref: "main"}
	byTime := dispatchTarget{at: now, workflow: "42", ref: "main"}
	byRunID := dispatchTarget{at: now, workflow: "42", ref: "main", runID: "900"}
	for _, test := range []struct {
		name   string
		row    map[string]any
		target dispatchTarget
		match  bool
	}{
		{"same workflow and branch, just after dispatch", row("2026-10-29T11:59:55Z", "42", "main", "1"), byTime, true},
		{"created before the dispatch", row("2026-10-29T11:59:54Z", "42", "main", "1"), byTime, false},
		{"another workflow", row("2026-10-29T12:00:01Z", "43", "main", "1"), byTime, false},
		{"a push to the dispatched ref", pushed, byTime, false},
		{"same file when the workflow ID is unknown", row("2026-10-29T12:00:01Z", "43", "main", "1"), byFile, true},
		{"another file when the workflow ID is unknown", row("2026-10-29T12:00:01Z", "43", "main", "1"), dispatchTarget{at: now, file: "deploy.yml", ref: "main"}, false},
		{"another branch", row("2026-10-29T12:00:01Z", "42", "feat/x", "1"), byTime, false},
		{"fully qualified branch", row("2026-10-29T12:00:01Z", "42", "main", "1"), dispatchTarget{at: now, workflow: "42", ref: "refs/heads/main"}, true},
		{"fully qualified tag", row("2026-10-29T12:00:01Z", "42", "v1", "1"), dispatchTarget{at: now, workflow: "42", ref: "refs/tags/v1"}, true},
		{"invalid time", row("invalid", "42", "main", "1"), byTime, false},
		{"the run GitHub returned", row("2026-10-29T12:00:01Z", "42", "main", "900"), byRunID, true},
		{"another run of the same workflow and branch", row("2026-10-29T12:00:01Z", "42", "main", "901"), byRunID, false},
	} {
		if got := dispatchMatches(test.row, test.target); got != test.match {
			t.Errorf("%s: got %v", test.name, got)
		}
	}
}

// A newer run of the same workflow on another branch, or another run on the
// same branch, must never stand in for the dispatched one.
func TestDispatchPollingSelectsTheDispatchedRun(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	// The newest page no longer holds the dispatched run on a busy repository;
	// only a lookup by its run ID finds it.
	newest := `{"data":[` +
		`{"run_id":"run-other-branch","created_at":"2026-10-29T12:00:09Z","platform_workflow_id":42,"head_branch":"feat/x","platform_run_id":902,"event":"workflow_dispatch"},` +
		`{"run_id":"run-same-branch","created_at":"2026-10-29T12:00:05Z","platform_workflow_id":42,"head_branch":"main","platform_run_id":901,"event":"workflow_dispatch"}]}`
	byRunID := map[string]string{"900": `{"data":[{"run_id":"run-dispatched","created_at":"2026-10-29T12:00:03Z","platform_workflow_id":42,"head_branch":"main","platform_run_id":900,"event":"workflow_dispatch"}]}`}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.URL.Query().Get("platform_run_id"); id != "" {
			_, _ = io.WriteString(w, byRunID[id])
			return
		}
		_, _ = io.WriteString(w, newest)
	}))
	defer server.Close()
	for _, test := range []struct {
		target dispatchTarget
		want   string
	}{
		{dispatchTarget{at: now, workflow: "42", ref: "main", runID: "900"}, "run-dispatched"},
		{dispatchTarget{at: now, workflow: "42", ref: "main"}, "run-same-branch"},
	} {
		d, _, _ := testDeps(t)
		d = d.defaults()
		clock := now
		d.Now = func() time.Time { return clock }
		d.Wait = func(ctx context.Context, delay time.Duration) error {
			clock = clock.Add(delay)
			return ctx.Err()
		}
		cfg := config.Config{Host: server.URL, Token: "avr_fixture"}
		row, err := pollDispatchedRun(t.Context(), d, cfg, client.New(cfg, nil, "fixture"), inspectionOrg, "rep-fixture", test.target)
		if err != nil || fieldText(row, "run_id") != test.want {
			t.Fatal(test.target, row, err)
		}
	}
}

// With --exit-status the exit code is the dispatched run's result, so a run
// that never appeared cannot report success.
func TestWorkflowRunExitStatusWhenTheRunNeverAppears(t *testing.T) {
	for _, test := range []struct {
		args   []string
		code   int
		prefix string
	}{
		{[]string{"workflow", "run", "ci.yml", "--repo", "rep-fixture", "--ref", "main", "--exit-status"}, 1, "Error: "},
		{[]string{"workflow", "run", "ci.yml", "--repo", "rep-fixture", "--ref", "main"}, 0, "  "},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				_, _ = io.WriteString(w, `{"data":{"platform_run_id":900}}`)
				return
			}
			_, _ = io.WriteString(w, `{"data":[]}`)
		}))
		d, _, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
		}
		now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
		d.Now = func() time.Time { return now }
		d.Wait = func(ctx context.Context, delay time.Duration) error {
			now = now.Add(delay)
			return ctx.Err()
		}
		code := Execute(t.Context(), test.args, d)
		server.Close()
		// The run ID GitHub returned resumes the watch without scraping stdout.
		if code != test.code || !strings.Contains(stderr.String(), "\n"+test.prefix+"Avrea has not recorded") || !strings.Contains(stderr.String(), "`avr run watch 900`") {
			t.Fatalf("%v: code %d stderr %q", test.args, code, stderr)
		}
	}
}

func TestDispatchPollingBoundAndCancellation(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			d, _, _ := testDeps(t)
			d = d.defaults()
			now := time.Now()
			d.Now = func() time.Time { return now }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			waits := 0
			d.Wait = func(ctx context.Context, delay time.Duration) error {
				waits++
				now = now.Add(delay)
				if stop {
					cancel()
				}
				return ctx.Err()
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("repository_ids") != "rep-fixture" {
					t.Error(r.URL)
				}
				w.WriteHeader(429)
				_, _ = io.WriteString(w, "{}")
			}))
			defer server.Close()
			cfg := config.Config{Host: server.URL, Token: "avr_fixture"}
			row, err := pollDispatchedRun(ctx, d, cfg, client.New(cfg, nil, "fixture"), inspectionOrg, "rep-fixture", dispatchTarget{at: now, workflow: "42", ref: "main"})
			if row != nil {
				t.Fatal(row)
			}
			if stop {
				if !errors.Is(err, context.Canceled) || waits != 1 {
					t.Fatal(err, waits)
				}
			} else if err != nil || calls != 90 || waits != 90 {
				t.Fatal(err, calls, waits)
			}
		})
	}
}

func TestWorkflowInputsBoundAndCancellation(t *testing.T) {
	if _, err := parseWorkflowInputs(t.Context(), strings.NewReader(strings.Repeat(" ", (16<<20)+1))); err == nil {
		t.Fatal("unbounded workflow input")
	}
	input, writer := io.Pipe()
	defer func() { _ = input.Close(); _ = writer.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := parseWorkflowInputs(ctx, input); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("workflow input ignored cancellation")
	}
}

func TestWorkflowInputsRejectTerminalBeforeNetwork(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.IsInputTerminal = func() bool { return true }
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	code := Execute(t.Context(), []string{"workflow", "run", "build.yml", "--json"}, d)
	if code != 1 || !strings.Contains(stderr.String(), "Pipe a JSON object") {
		t.Fatal(code, stderr)
	}
}
