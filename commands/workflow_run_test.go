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
		return map[string]any{"created_at": created, "platform_workflow_id": workflow, "head_branch": branch, "platform_run_id": json.Number(runID)}
	}
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
		{"any workflow when unknown", row("2026-10-29T12:00:01Z", "43", "main", "1"), dispatchTarget{at: now, ref: "main"}, true},
		{"another branch", row("2026-10-29T12:00:01Z", "42", "feat/x", "1"), byTime, false},
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
	runs := `{"data":[` +
		`{"run_id":"run-other-branch","created_at":"2026-10-29T12:00:09Z","platform_workflow_id":42,"head_branch":"feat/x","platform_run_id":902},` +
		`{"run_id":"run-same-branch","created_at":"2026-10-29T12:00:05Z","platform_workflow_id":42,"head_branch":"main","platform_run_id":901},` +
		`{"run_id":"run-dispatched","created_at":"2026-10-29T12:00:03Z","platform_workflow_id":42,"head_branch":"main","platform_run_id":900}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, runs)
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
		d.Now = func() time.Time { return now }
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
		args []string
		code int
	}{
		{[]string{"workflow", "run", "ci.yml", "--repo", "rep-fixture", "--ref", "main", "--exit-status"}, 1},
		{[]string{"workflow", "run", "ci.yml", "--repo", "rep-fixture", "--ref", "main"}, 0},
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
		if code != test.code || !strings.Contains(stderr.String(), "avr run watch") {
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
