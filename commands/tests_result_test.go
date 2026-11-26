package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

// An opaque result ID; the CLI must never decode it.
const viewResultID = "eyJibG9jayI6MSwib2Zmc2V0IjoyfQ"

func jsonBody(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func testResultRecord(status, outcome, summary string) map[string]any {
	record := map[string]any{"result_id": viewResultID, "status": status, "job_id": resultTestJob, "execution_id": "exe-1", "commit_sha": "c0ffee", "report_finished_at": "2026-11-24T19:25:48Z", "data": nil}
	if status == "available" {
		record["data"] = map[string]any{"result_id": viewResultID, "test_id": historyTestID, "framework": "pytest", "suite": "pytest", "class_name": "tests.unit", "name": "test_spec", "outcome": outcome, "duration_ms": 712, "failure_summary": summary}
	}
	return record
}

func failureStartRecord(status string, start any) map[string]any {
	run := func(commit, job string) map[string]any {
		return map[string]any{"commit_sha": commit, "job_id": job, "result_id": "r", "started_at": "2026-11-24T18:00:00Z"}
	}
	record := map[string]any{"status": status, "start": start, "branch": "main", "failed_reports": 3, "first_failure": nil, "last_pass": nil,
		"scope": map[string]any{"complete": false, "reports_searched": 9, "reports_indexing": 1, "reports_unavailable": 0, "oldest_recorded_at": nil, "newest_recorded_at": nil}}
	switch start {
	case "started":
		record["first_failure"], record["last_pass"] = run("bbbbbbb", "job-first"), run("aaaaaaa", "job-pass")
	case "inconsistent":
		// It passed on the commit of the first failure.
		record["first_failure"], record["last_pass"] = run("bbbbbbb", "job-first"), run("bbbbbbb", "job-pass")
	case "no_earlier_pass":
		record["first_failure"] = run("bbbbbbb", "job-first")
	}
	return record
}

// viewDeps serves a result and its failure start, and records the paths read.
func viewDeps(t *testing.T, result, start map[string]any) (func(...string) (int, string, string), *[]string) {
	t.Helper()
	paths := []string{}
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		paths = append(paths, r.URL.EscapedPath())
		base := "/orgs/" + inspectionOrg + "/repos/" + resultTestRepo + "/tests/results/" + viewResultID
		switch r.URL.EscapedPath() {
		case base:
			return 200, jsonBody(t, result)
		case base + "/failure-start":
			return 200, jsonBody(t, start)
		}
		t.Errorf("unexpected request %s", r.URL)
		return 500, `{}`
	})
	return execute, &paths
}

func TestTestsViewReadsAFailureAndWhenItStarted(t *testing.T) {
	execute, paths := viewDeps(t, testResultRecord("available", "failed", "first line\nsecond \x1b[31mline"), failureStartRecord("available", "started"))
	code, out, stderr := execute("view", viewResultID, "--repo", resultTestRepo)
	if code != 0 || len(*paths) != 2 {
		t.Fatal(code, *paths, out, stderr)
	}
	for _, want := range []string{
		"Test: tests.unit.test_spec", "Outcome: failed  Time (ms): 712", "Job: " + resultTestJob,
		"Failure summary:\nfirst line\nsecond \\u001b[31mline\n",
		"Branch: main", "Started failing at", "job job-first", "Last passed at", "job job-pass",
		"Failed in 3 reports since then, including this one.",
		"If runs on this branch follow commit order, the change is in aaaaaaa..bbbbbbb.",
		"Reports checked: 9; still indexing: 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Error("terminal control reached the output")
	}
}

func TestTestsViewChecksFailureStartOnlyForFailures(t *testing.T) {
	for _, tc := range []struct{ status, outcome string }{
		{"available", "passed"}, {"available", "skipped"}, {"expired", ""}, {"not_recorded", ""},
	} {
		t.Run(tc.status+"/"+tc.outcome, func(t *testing.T) {
			execute, paths := viewDeps(t, testResultRecord(tc.status, tc.outcome, ""), nil)
			code, out, stderr := execute("view", viewResultID, "--repo", resultTestRepo, "--json", "status,failure_start")
			var record map[string]any
			if err := json.Unmarshal([]byte(out), &record); err != nil || code != 0 || len(*paths) != 1 {
				t.Fatal(code, err, *paths, out, stderr)
			}
			if value, ok := record["failure_start"]; !ok || value != nil || record["status"] != tc.status {
				t.Fatal(out)
			}
		})
	}
	for status, want := range map[string]string{"expired": "no longer retained", "not_recorded": "No test report is recorded"} {
		execute, _ := viewDeps(t, testResultRecord(status, "", ""), nil)
		if code, out, stderr := execute("view", viewResultID, "--repo", resultTestRepo); code != 0 || !strings.Contains(out, want) || strings.Contains(out, "When it started") {
			t.Fatal(status, code, out, stderr)
		}
	}
}

func TestTestsViewStatesEachFailureStart(t *testing.T) {
	for _, tc := range []struct {
		status string
		start  any
		want   string
	}{
		{"available", "no_earlier_pass", "Failed in every checked report on this branch where it ran: 3"},
		{"available", "inconsistent", "The outcome changed without a commit change, so this test looks flaky."},
		{"available", "branch_unknown", "no branch of this repository to compare with"},
		{"indexing", nil, "Not checked: a report this check needs is still being indexed."},
		{"unavailable", nil, "Not checked: a report this check needs cannot currently be read."},
		{"limited", nil, "Not checked: the check reached a read limit."},
		{"expired", nil, "Not checked: the result's report is no longer retained."},
		{"not_recorded", nil, "Not checked: the result's report is not recorded."},
	} {
		t.Run(tc.status+"/"+jsonBody(t, tc.start), func(t *testing.T) {
			execute, _ := viewDeps(t, testResultRecord("available", "errored", ""), failureStartRecord(tc.status, tc.start))
			code, out, stderr := execute("view", viewResultID, "--repo", resultTestRepo)
			if code != 0 || !strings.Contains(out, tc.want) {
				t.Fatal(code, out, stderr)
			}
			// A commit range is a candidate only when the test passed earlier.
			if strings.Contains(out, "..") {
				t.Errorf("unexpected commit range in:\n%s", out)
			}
		})
	}
}

func TestTestsViewRejectsInvalidResponsesWithoutOutput(t *testing.T) {
	wrongID := testResultRecord("available", "failed", "")
	wrongID["result_id"] = "other"
	unknownState := testResultRecord("available", "failed", "")
	unknownState["status"] = "pending"
	noData := testResultRecord("available", "failed", "")
	noData["data"] = nil
	noLastPass := failureStartRecord("available", "started")
	noLastPass["last_pass"] = nil
	twoCommits := failureStartRecord("available", "inconsistent")
	twoCommits["last_pass"] = failureStartRecord("available", "started")["last_pass"]
	for name, tc := range map[string]struct{ result, start map[string]any }{
		"wrong result":                {wrongID, nil},
		"unknown result state":        {unknownState, nil},
		"available without data":      {noData, nil},
		"available start missing":     {testResultRecord("available", "failed", ""), failureStartRecord("available", nil)},
		"start for a stopped read":    {testResultRecord("available", "failed", ""), failureStartRecord("indexing", "started")},
		"unknown failure start kind":  {testResultRecord("available", "failed", ""), failureStartRecord("available", "began")},
		"failure that did not fail":   {testResultRecord("available", "failed", ""), failureStartRecord("available", "not_failed")},
		"started without last pass":   {testResultRecord("available", "failed", ""), noLastPass},
		"inconsistent across commits": {testResultRecord("available", "errored", ""), twoCommits},
	} {
		t.Run(name, func(t *testing.T) {
			execute, _ := viewDeps(t, tc.result, tc.start)
			if code, out, stderr := execute("view", viewResultID, "--repo", resultTestRepo); code != 1 || out != "" || !strings.Contains(stderr, "The API returned an invalid") {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestTestsViewNotFoundSaysAResultBelongsToOneRepository(t *testing.T) {
	execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
		return 404, `{"detail":"Test result not found"}`
	})
	if code, out, stderr := execute("view", viewResultID, "--repo", resultTestRepo); code != 1 || out != "" || !strings.Contains(stderr, "A result ID belongs to one repository.") {
		t.Fatal(code, out, stderr)
	}
}

func TestTestsViewRejectsBadInputBeforeRequests(t *testing.T) {
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		t.Errorf("unexpected request %s", r.URL)
		return 500, `{}`
	})
	for _, args := range [][]string{
		{"view", " " + viewResultID, "--repo", resultTestRepo},
		{"view", strings.Repeat("a", 1025), "--repo", resultTestRepo},
		{"view", viewResultID, "--repo", resultTestRepo, "--web", "--json", "status"},
	} {
		if code, out, _ := execute(args...); code != 2 || out != "" {
			t.Errorf("%.40v: %d %q", args, code, out)
		}
	}
}

func TestTestsViewWebOpensTheConsoleResult(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.IsTerminal = func() bool { return true }
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/users/me/organizations" {
			t.Error("unexpected request", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"organization_id":"` + inspectionOrg + `","slug":"acme"}]}`)), Request: r}, nil
	})}
	opened := ""
	d.OpenBrowser = func(_ context.Context, target string) error { opened = target; return nil }
	code := Execute(t.Context(), []string{"tests", "view", viewResultID, "--repo", resultTestRepo, "--web"}, d)
	expected := "https://console.example.test/org/acme/repos/" + resultTestRepo + "/tests/results/" + viewResultID
	if code != 0 || opened != expected || out.String() != "Opening: "+expected+"\n" {
		t.Fatal(code, opened, out, stderr)
	}
}

func TestTestsViewJSONWithoutFailureStartMakesOneRequest(t *testing.T) {
	execute, paths := viewDeps(t, testResultRecord("available", "failed", "boom"), nil)
	code, out, stderr := execute("view", viewResultID, "--repo", resultTestRepo, "--json", "data", "-q", ".data.outcome")
	if code != 0 || len(*paths) != 1 || strings.TrimSpace(out) != "failed" {
		t.Fatal(code, *paths, out, stderr)
	}
}

func TestTestsViewNamesATestWithoutAClass(t *testing.T) {
	result := testResultRecord("available", "passed", "")
	result["data"].(map[string]any)["class_name"] = ""
	execute, _ := viewDeps(t, result, nil)
	if code, out, stderr := execute("view", viewResultID, "--repo", resultTestRepo); code != 0 || !strings.Contains(out, "Test: test_spec  Suite:") {
		t.Fatal(code, out, stderr)
	}
}
