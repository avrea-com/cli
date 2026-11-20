package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDispatchMatching(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	row := func(created, workflow, branch, runID string) map[string]any {
		return map[string]any{"created_at": created, "platform_workflow_id": workflow, "workflow_path": ".github/workflows/build.yml", "head_branch": branch, "platform_run_id": json.Number(runID), "event": "workflow_dispatch", "ci_provider": "github_actions"}
	}
	pushed := row("2026-10-29T12:00:01Z", "42", "main", "1")
	pushed["event"] = "push"
	avreaCI := row("2026-10-29T12:00:01Z", "42", "main", "900")
	avreaCI["ci_provider"] = "avrea_ci"
	noProvider := row("2026-10-29T12:00:01Z", "42", "main", "900")
	delete(noProvider, "ci_provider")
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
		{"an Avrea CI run on the dispatched ref", avreaCI, byTime, false},
		{"an Avrea CI run with the returned ID", avreaCI, byRunID, false},
		{"a row without ci_provider is GitHub Actions", noProvider, byRunID, true},
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
		`{"run_id":"run-other-branch","created_at":"2026-10-29T12:00:09Z","platform_workflow_id":42,"head_branch":"feat/x","platform_run_id":902,"event":"workflow_dispatch","ci_provider":"github_actions"},` +
		`{"run_id":"run-same-branch","created_at":"2026-10-29T12:00:05Z","platform_workflow_id":42,"head_branch":"main","platform_run_id":901,"event":"workflow_dispatch","ci_provider":"github_actions"}]}`
	byRunID := map[string]string{"901": `{"data":[]}`, "900": `{"data":[{"run_id":"run-dispatched","created_at":"2026-10-29T12:00:03Z","platform_workflow_id":42,"head_branch":"main","platform_run_id":900,"event":"workflow_dispatch","ci_provider":"github_actions"}]}`}
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
		// A run from a registered GitHub host is not found by the exact lookup.
		{dispatchTarget{at: now, workflow: "42", ref: "main", runID: "901"}, "run-same-branch"},
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
		if code != test.code || !strings.Contains(stderr.String(), "\n"+test.prefix+"Avrea has not recorded") || !strings.Contains(stderr.String(), "`avr run watch 900 --org "+inspectionOrg+"`") || !strings.Contains(stderr.String(), "--repo rep-fixture --org "+inspectionOrg+"`") {
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
	if _, _, err := parseWorkflowInputs(t.Context(), strings.NewReader(strings.Repeat(" ", (16<<20)+1))); err == nil {
		t.Fatal("unbounded workflow input")
	}
	input, writer := io.Pipe()
	defer func() { _ = input.Close(); _ = writer.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, _, err := parseWorkflowInputs(ctx, input); done <- err }()
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

// Without a run ID from GitHub, the command still follows the matching
// workflow_dispatch run, says how it chose it, and reports its result.
func TestWorkflowRunFollowsADispatchWithoutRunID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			_, _ = io.WriteString(w, `{"data":{}}`)
		case strings.HasSuffix(r.URL.Path, "/workflow-runs"):
			_, _ = io.WriteString(w, `{"data":[`+
				`{"run_id":"run-push","created_at":"2026-10-29T12:00:04Z","workflow_path":".github/workflows/ci.yml","head_branch":"main","event":"push","ci_provider":"github_actions"},`+
				`{"run_id":"run-dispatched","created_at":"2026-10-29T12:00:02Z","workflow_path":".github/workflows/ci.yml","head_branch":"main","event":"workflow_dispatch","ci_provider":"github_actions"}]}`)
		case strings.HasSuffix(r.URL.Path, "/workflow-runs/run-dispatched"):
			_, _ = io.WriteString(w, `{"data":{"run_id":"run-dispatched","status":"completed","conclusion":"failure","jobs":[]}}`)
		default:
			_, _ = io.WriteString(w, `{"data":[]}`)
		}
	}))
	defer server.Close()
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	d.Now = func() time.Time { return now }
	d.Wait = func(ctx context.Context, delay time.Duration) error {
		now = now.Add(delay)
		return ctx.Err()
	}
	code := Execute(t.Context(), []string{"workflow", "run", "ci.yml", "--repo", "rep-fixture", "--ref", "main", "--exit-status"}, d)
	if code != 1 || !strings.Contains(out.String(), "run-dispatched") || !strings.Contains(stderr.String(), "GitHub returned no run ID") {
		t.Fatalf("code %d out %q stderr %q", code, out, stderr)
	}
}

func TestWorkflowInputCancellationDoesNotWaitForStdin(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://fixture.test", Token: "fixture"}, nil
	}
	d.IsInputTerminal = func() bool { return false }
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	d.In = reader
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Execute(ctx, []string{"workflow", "run", "build.yml", "--json"}, d) }()
	if _, err := writer.Write([]byte(`{"count":`)); err != nil {
		t.Fatal(err)
	}
	cancel()
	drained := make(chan struct{})
	go func() { _, _ = writer.Write([]byte(`1}`)); _ = writer.Close(); close(drained) }()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatal(code, stderr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stdin cancellation blocked")
	}
	<-drained
}

const (
	avreaWorkflowFixture  = "wfl-019a0000000070008000000000000004"
	avreaAdmissionFixture = "wap-019a0000000070008000000000000006"
	avreaRunFixture       = "run-019a0000000070008000000000000007"
)

// avreaDispatchServer answers an Avrea CI dispatch with a receipt, and the
// receipt with each of admissions in turn, repeating the last. An empty
// admission is a 503.
func avreaDispatchServer(t *testing.T, dispatch string, admissions ...string) (*httptest.Server, *atomic.Value) {
	t.Helper()
	var body atomic.Value
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/repos/rep-fixture/dispatch-workflow"):
			var request map[string]any
			_ = json.NewDecoder(r.Body).Decode(&request)
			body.Store(request)
			_, _ = io.WriteString(w, dispatch)
		case strings.HasSuffix(r.URL.Path, "/repos/rep-fixture/workflow-admissions/"+avreaAdmissionFixture) && len(admissions) > 0:
			admission := admissions[min(int(reads.Add(1)), len(admissions))-1]
			if admission == "" {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			_, _ = io.WriteString(w, admission)
		case strings.HasSuffix(r.URL.Path, "/repos/rep-fixture"):
			_, _ = io.WriteString(w, `{"data":{"repository_id":"rep-fixture","default_branch":"main"}}`)
		case strings.HasSuffix(r.URL.Path, "/workflows"):
			_, _ = io.WriteString(w, `{"data":[{"workflow_id":"`+avreaWorkflowFixture+`","ci_provider":"avrea_ci","platform_id":null,"name":"Build","path":".avrea/workflows/build.yml"}]}`)
		case strings.HasSuffix(r.URL.Path, "/workflow-runs/"+avreaRunFixture):
			_, _ = io.WriteString(w, `{"data":{"run_id":"`+avreaRunFixture+`","status":"completed","conclusion":"failure","jobs":[]}}`)
		case strings.HasSuffix(r.URL.Path, "/workflow-runs"):
			t.Error("an Avrea CI dispatch must not guess its run from the run list")
		default:
			_, _ = io.WriteString(w, `{"data":[]}`)
		}
	}))
	t.Cleanup(server.Close)
	return server, &body
}

func avreaDispatchDeps(t *testing.T, server *httptest.Server) (Dependencies, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	d, stdout, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	d.Now = func() time.Time { return now }
	d.Wait = func(ctx context.Context, delay time.Duration) error {
		now = now.Add(delay)
		return ctx.Err()
	}
	return d, stdout, stderr
}

func avreaAdmission(state, reason string, runs ...string) string {
	record, _ := json.Marshal(map[string]any{"data": map[string]any{"admission_id": avreaAdmissionFixture, "state": state, "reason": reason, "run_ids": append([]string{}, runs...)}})
	return string(record)
}

const avreaDispatchAccepted = `{"data":{"workflow":"` + avreaWorkflowFixture + `","ref":"main","platform_run_id":null,"run_id":null,"admission_id":"` + avreaAdmissionFixture + `"}}`

func TestWorkflowRunDispatchesAvreaCIByWorkflowID(t *testing.T) {
	server, dispatched := avreaDispatchServer(t, avreaDispatchAccepted)
	for _, identifier := range []string{avreaWorkflowFixture, ".avrea/workflows/build.yml", "Build"} {
		d, stdout, stderr := avreaDispatchDeps(t, server)
		code := Execute(t.Context(), []string{"workflow", "run", identifier, "--repo", "rep-fixture", "--ref", "main", "-f", "count=2", "--no-watch"}, d)
		if code != 0 || !strings.Contains(stdout.String(), ".avrea/workflows/build.yml") {
			t.Fatal(identifier, code, stdout, stderr)
		}
		body, _ := dispatched.Load().(map[string]any)
		key, _ := body["invocation_key"].(string)
		if inputs, _ := body["inputs"].(map[string]any); body["workflow"] != avreaWorkflowFixture || body["ref"] != "main" || len(inputs) != 1 || inputs["count"] != "2" {
			t.Fatal(identifier, body)
		}
		// A generated key lets the printed command recover a lost response.
		if _, err := uuid.Parse(key); err != nil || !strings.Contains(stderr.String(), "--idempotency-key "+key) {
			t.Fatal(identifier, body, stderr)
		}
	}
}

func TestWorkflowRunFollowsTheRunItsAvreaCIDispatchStarted(t *testing.T) {
	server, _ := avreaDispatchServer(t, avreaDispatchAccepted, avreaAdmission("pending", ""), avreaAdmission("admitted", "", avreaRunFixture))
	d, stdout, stderr := avreaDispatchDeps(t, server)
	code := Execute(t.Context(), []string{"workflow", "run", avreaWorkflowFixture, "--repo", "rep-fixture", "--ref", "main", "--exit-status"}, d)
	if code != 1 || !strings.Contains(stdout.String(), avreaRunFixture) {
		t.Fatalf("code %d out %q stderr %q", code, stdout, stderr)
	}
}

func TestWorkflowRunReusesTheInvocationKeyAndItsRun(t *testing.T) {
	const key = "019a0000-0000-7000-8000-000000000008"
	server, dispatched := avreaDispatchServer(t, `{"data":{"workflow":"`+avreaWorkflowFixture+`","ref":"main","run_id":"`+avreaRunFixture+`","admission_id":"`+avreaAdmissionFixture+`"}}`)
	d, stdout, stderr := avreaDispatchDeps(t, server)
	code := Execute(t.Context(), []string{"workflow", "run", avreaWorkflowFixture, "--repo", "rep-fixture", "--ref", "main", "--idempotency-key", key}, d)
	body, _ := dispatched.Load().(map[string]any)
	if sent, _ := uuid.Parse(fieldText(body, "invocation_key")); code != 0 || sent.String() != key || !strings.Contains(stdout.String(), avreaRunFixture) {
		t.Fatalf("code %d body %v out %q stderr %q", code, body, stdout, stderr)
	}
}

func TestWorkflowRunReportsARejectedAvreaCIDispatch(t *testing.T) {
	server, _ := avreaDispatchServer(t, avreaDispatchAccepted, avreaAdmission("rejected", "workflow_invalid"))
	d, _, stderr := avreaDispatchDeps(t, server)
	code := Execute(t.Context(), []string{"workflow", "run", avreaWorkflowFixture, "--repo", "rep-fixture", "--ref", "main"}, d)
	if code != 1 || !strings.Contains(stderr.String(), "workflow_invalid") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
}

func TestWorkflowRunResumesAnAvreaCIDispatchThatHasNoRunYet(t *testing.T) {
	server, _ := avreaDispatchServer(t, avreaDispatchAccepted, avreaAdmission("pending", ""))
	for _, test := range []struct {
		args []string
		code int
	}{{nil, 0}, {[]string{"--exit-status"}, 1}} {
		d, _, stderr := avreaDispatchDeps(t, server)
		code := Execute(t.Context(), append([]string{"workflow", "run", avreaWorkflowFixture, "--repo", "rep-fixture", "--ref", "main"}, test.args...), d)
		if code != test.code || !strings.Contains(stderr.String(), "has not started the run") || strings.Count(stderr.String(), "--idempotency-key") < 2 {
			t.Fatalf("code %d stderr %q", code, stderr)
		}
	}
}

func TestWorkflowRunRejectsAnInvocationKeyForGitHubActions(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://fixture.test", Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	code := Execute(t.Context(), []string{"workflow", "run", "ci.yml", "--repo", "rep-fixture", "--ref", "main", "--idempotency-key", "019a0000-0000-7000-8000-000000000008"}, d)
	if code != 1 || !strings.Contains(stderr.String(), "only for Avrea CI") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
}

// Avrea CI converts each input from its JSON spelling, so a boolean must
// arrive as true and a number with every digit it was given. GitHub inputs
// keep the spelling the CLI has always sent.
func TestWorkflowRunJSONInputsKeepTheirSpellingForAvreaCI(t *testing.T) {
	const input = `{"flag":true,"exact":1.00000000000000001,"big":1e400,"text":"a b","nothing":null,"list":[1,"x"]}`
	server, dispatched := avreaDispatchServer(t, avreaDispatchAccepted)
	for _, test := range []struct {
		workflow string
		want     map[string]any
	}{
		{avreaWorkflowFixture, map[string]any{"flag": "true", "exact": "1.00000000000000001", "big": "1e400", "text": "a b", "nothing": "null", "list": `[1,"x"]`}},
		{"ci.yml", map[string]any{"flag": "True", "exact": "1.0", "big": "inf", "text": "a b", "nothing": "None", "list": "[1, 'x']"}},
	} {
		d, _, stderr := avreaDispatchDeps(t, server)
		d.In = strings.NewReader(input)
		d.IsInputTerminal = func() bool { return false }
		code := Execute(t.Context(), []string{"workflow", "run", test.workflow, "--repo", "rep-fixture", "--ref", "main", "--json", "--no-watch"}, d)
		body, _ := dispatched.Load().(map[string]any)
		if inputs, _ := body["inputs"].(map[string]any); code != 0 || !reflect.DeepEqual(inputs, test.want) {
			t.Fatalf("%s: code %d inputs %v stderr %q", test.workflow, code, body["inputs"], stderr)
		}
	}
}

// The recovery command is the only way to repeat a dispatch that was accepted
// but never answered, so it is printed before the request.
func TestWorkflowRunPrintsTheRecoveryCommandWhateverTheDispatchReturns(t *testing.T) {
	server, _ := avreaDispatchServer(t, `{"data":{}}`)
	d, _, stderr := avreaDispatchDeps(t, server)
	code := Execute(t.Context(), []string{"workflow", "run", avreaWorkflowFixture, "--repo", "rep-fixture", "--ref", "main", "--no-watch"}, d)
	if printed, _, _ := strings.Cut(stderr.String(), "invalid Avrea CI dispatch receipt"); code != 1 || !strings.Contains(printed, "--idempotency-key") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
}

func TestAvreaCIDispatchCommandRoundTripsControlCharacters(t *testing.T) {
	command := avreaCIDispatchCommand(config.Config{Host: "https://api.example.test"}, "org-fixture", "rep-fixture", avreaWorkflowFixture, "main", "key-fixture",
		map[string]string{"notes": "it's\r\n\x1b[31mred", "plain": "a b"})
	if strings.ContainsAny(command, "\r\n\x1b") {
		t.Fatalf("raw control characters in %q", command)
	}
	for _, argument := range []string{`$'notes=it\'s\r\n\x1b[31mred'`, `'plain=a b'`} {
		if !strings.Contains(command, argument) {
			t.Fatalf("%q lacks %s", command, argument)
		}
	}
}

func TestWorkflowRunQualifiesTheDefaultBranchForAvreaCI(t *testing.T) {
	server, dispatched := avreaDispatchServer(t, avreaDispatchAccepted)
	d, stdout, stderr := avreaDispatchDeps(t, server)
	code := Execute(t.Context(), []string{"workflow", "run", avreaWorkflowFixture, "--repo", "rep-fixture", "--no-watch"}, d)
	if body, _ := dispatched.Load().(map[string]any); code != 0 || body["ref"] != "refs/heads/main" {
		t.Fatalf("code %d body %v stderr %q", code, body, stderr)
	}
	// The user sees the branch as the repository names it.
	if !strings.Contains(stdout.String(), " at main.\n") {
		t.Fatalf("out %q", stdout)
	}
}

// --no-watch reports the run the dispatch started, or the command that
// follows it once it starts, never the GitHub run-list hint.
func TestWorkflowRunNoWatchIdentifiesTheAvreaCIRun(t *testing.T) {
	started := `{"data":{"workflow":"` + avreaWorkflowFixture + `","ref":"main","run_id":"` + avreaRunFixture + `","admission_id":"` + avreaAdmissionFixture + `"}}`
	for _, test := range []struct {
		dispatch string
		started  bool
	}{{started, true}, {avreaDispatchAccepted, false}} {
		server, _ := avreaDispatchServer(t, test.dispatch)
		d, stdout, stderr := avreaDispatchDeps(t, server)
		code := Execute(t.Context(), []string{"workflow", "run", avreaWorkflowFixture, "--repo", "rep-fixture", "--ref", "main", "--no-watch"}, d)
		_, hint, _ := strings.Cut(stderr.String(), "Follow the run with:")
		if code != 0 || strings.Contains(stderr.String(), "avr run list") || strings.Contains(stdout.String(), avreaRunFixture) != test.started ||
			strings.Contains(hint, "--idempotency-key") == test.started || strings.Contains(hint, "--no-watch") {
			t.Fatalf("started %v: code %d out %q stderr %q", test.started, code, stdout, stderr)
		}
	}
}

// The printed command repeats the dispatch as it was asked for, so a script
// that retries it keeps its exit code and its pace.
func TestWorkflowRunRecoveryCommandKeepsTheWatchFlags(t *testing.T) {
	server, _ := avreaDispatchServer(t, avreaDispatchAccepted, avreaAdmission("pending", ""))
	for _, test := range []struct{ args, want []string }{
		{[]string{"--exit-status", "--interval", "7"}, []string{" --exit-status", " --interval 7"}},
		{[]string{"--no-watch"}, []string{" --no-watch"}},
		{nil, nil},
	} {
		d, _, stderr := avreaDispatchDeps(t, server)
		Execute(t.Context(), append([]string{"workflow", "run", avreaWorkflowFixture, "--repo", "rep-fixture", "--ref", "main"}, test.args...), d)
		_, rest, _ := strings.Cut(stderr.String(), "Retry or resume this dispatch with:\n")
		command, _, _ := strings.Cut(rest, "\n")
		for _, flag := range []string{" --exit-status", " --interval 7", " --no-watch"} {
			wanted := false
			for _, want := range test.want {
				wanted = wanted || want == flag
			}
			if command == "" || strings.Contains(command, flag) != wanted {
				t.Fatalf("%v: %q", test.args, command)
			}
		}
	}
}

func TestWorkflowRunRejectsAnInvalidRunIDInTheDispatchReceipt(t *testing.T) {
	server, _ := avreaDispatchServer(t, `{"data":{"workflow":"`+avreaWorkflowFixture+`","ref":"main","run_id":"../orgs","admission_id":"`+avreaAdmissionFixture+`"}}`)
	d, stdout, stderr := avreaDispatchDeps(t, server)
	code := Execute(t.Context(), []string{"workflow", "run", avreaWorkflowFixture, "--repo", "rep-fixture", "--ref", "main"}, d)
	if code != 1 || !strings.Contains(stderr.String(), "invalid Avrea CI dispatch receipt") || strings.Contains(stdout.String(), "Triggered") {
		t.Fatalf("code %d out %q stderr %q", code, stdout, stderr)
	}
}

func TestWorkflowRunReportsARejectionWithoutAReason(t *testing.T) {
	server, _ := avreaDispatchServer(t, avreaDispatchAccepted, `{"data":{"admission_id":"`+avreaAdmissionFixture+`","state":"rejected","reason":null,"run_ids":[]}}`)
	d, _, stderr := avreaDispatchDeps(t, server)
	code := Execute(t.Context(), []string{"workflow", "run", avreaWorkflowFixture, "--repo", "rep-fixture", "--ref", "main"}, d)
	if code != 1 || !strings.Contains(stderr.String(), "rejected the dispatch without giving a reason.") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
}

// The command is pasted into whichever shell the user has; macOS ships
// bash 3.2 as /bin/bash.
func TestAvreaCIDispatchCommandSurvivesTheShell(t *testing.T) {
	const value = "it's\r\n\x1b[31mred \u0085\u202e\u2066 a\\b é"
	for _, shell := range []string{"/bin/bash", "zsh"} {
		path, err := exec.LookPath(shell)
		if err != nil {
			continue
		}
		got, err := exec.CommandContext(t.Context(), path, "-c", "printf %s "+shellQuoteControls(value)).Output()
		if err != nil || string(got) != value {
			t.Errorf("%s: %q, %v", path, got, err)
		}
	}
}

func TestWorkflowRunReportsAnUnreadableDispatchReceipt(t *testing.T) {
	server, _ := avreaDispatchServer(t, avreaDispatchAccepted, "")
	d, _, stderr := avreaDispatchDeps(t, server)
	code := Execute(t.Context(), []string{"workflow", "run", avreaWorkflowFixture, "--repo", "rep-fixture", "--ref", "main"}, d)
	if code != 1 || !strings.Contains(stderr.String(), "Could not read the dispatch receipt") || strings.Contains(stderr.String(), "has not started the run") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
}
