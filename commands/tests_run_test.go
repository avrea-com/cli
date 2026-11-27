package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

const (
	runTestOtherJob   = "job-019a0000000070008000000000000004"
	runTestIndexedJob = "job-019a0000000070008000000000000005"
	runTestAddedJob   = "job-019a0000000070008000000000000006"
)

var runTestsPath = "/orgs/" + inspectionOrg + "/repos/" + resultTestRepo + "/workflow-runs/" + inspectionRun + "/tests"

// runJob summarizes one job; a job without a recorded report has no counts.
func runJob(id, name, status string, failed, errored int) string {
	total, counts := strconv.Itoa(3+failed+errored), fmt.Sprintf(`{"passed":3,"failed":%d,"skipped":0,"errored":%d}`, failed, errored)
	if status == "not_recorded" || status == "expired" {
		total, counts = "null", "null"
	}
	return fmt.Sprintf(`{"job_id":%q,"name":%q,"execution_id":"exe","status":%q,"reporting_state":"complete","reason":null,"commit_sha":"abc","observed_at":null,"total_count":%s,"counts":%s}`, id, name, status, total, counts)
}

func runRow(job, name, outcome, summary string) string {
	return fmt.Sprintf(`{"test_id":%q,"framework":"pytest","result_id":"r","suite":"pytest","class_name":"tests.unit","name":%q,"outcome":%q,"duration_ms":5,"failure_summary":%q,"job_id":%q}`, historyTestID, name, outcome, summary, job)
}

func runPage(status, filters, cursor string, jobs []string, rows ...string) string {
	return fmt.Sprintf(`{"run_id":%q,"status":%q,"filters":%s,"jobs":[%s],"data":[%s],"pagination":{"next_cursor":%s}}`, inspectionRun, status, filters, strings.Join(jobs, ","), strings.Join(rows, ","), cursor)
}

const runFailureFilters = `{"outcomes":["errored","failed"]}`

func TestTestsAllJobsListsFailuresAcrossJobs(t *testing.T) {
	calls := 0
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		calls++
		query := r.URL.Query()
		if r.URL.Path != runTestsPath || query.Get("outcome") != "in.(errored,failed)" || query.Get("limit") != "100" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		if calls == 1 {
			// The other job's report could not be read on this page; the
			// cursor moves past it.
			jobs := []string{runJob(resultTestJob, "unit", "available", 1, 0), runJob(runTestOtherJob, "integration", "limited", 1, 1), runJob(runTestIndexedJob, "lint", "indexing", 0, 0)}
			return 200, runPage("limited", runFailureFilters, `"c2"`, jobs, runRow(resultTestJob, "test_login", "failed", "boom"))
		}
		if query.Get("cursor") != "c2" {
			t.Errorf("cursor not followed: %s", r.URL)
		}
		jobs := []string{runJob(resultTestJob, "unit", "available", 1, 0), runJob(runTestOtherJob, "integration", "available", 1, 1), runJob(runTestIndexedJob, "lint", "indexing", 0, 0), runJob(runTestAddedJob, "deploy", "not_recorded", 0, 0)}
		return 200, runPage("available", runFailureFilters, `null`, jobs)
	})
	code, out, stderr := execute("list", inspectionRun, "--all-jobs", "--repo", resultTestRepo, "--status", "failed", "--status", "errored", "-L", "250")
	if code != 0 || calls != 2 {
		t.Fatal(code, calls, out, stderr)
	}
	for _, want := range []string{
		"Run: " + inspectionRun, "Results: limited", "unit", "deploy", "Matching results shown: 1", "test_login",
		"tests.unit.test_login in unit: boom",
		"Results of integration (" + runTestOtherJob + ") may be incomplete: a read of its report reached a limit. Its report records 2 errored or failed results in total. Read the job with avr tests list " + runTestOtherJob + " --org " + inspectionOrg + " --repo " + resultTestRepo + ".",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// A job whose counts show no failure is not named, even while indexing.
	if strings.Contains(out, "("+runTestIndexedJob+")") || strings.Contains(out, "No matching tests") || stderr != "" {
		t.Errorf("unexpected output:\n%s\n%s", out, stderr)
	}
}

// The API resumes a job whose read failed after earlier reads on the next
// page; once that page delivers the job's rows, it is no longer incomplete.
func TestTestsAllJobsClearsAFailureTheNextPageRetried(t *testing.T) {
	calls := 0
	execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
		calls++
		if calls == 1 {
			jobs := []string{runJob(resultTestJob, "unit", "available", 1, 0), runJob(runTestOtherJob, "integration", "limited", 1, 0)}
			return 200, runPage("limited", runFailureFilters, `"c2"`, jobs, runRow(resultTestJob, "test_login", "failed", "boom"))
		}
		jobs := []string{runJob(resultTestJob, "unit", "available", 1, 0), runJob(runTestOtherJob, "integration", "available", 1, 0)}
		return 200, runPage("available", runFailureFilters, `null`, jobs, runRow(runTestOtherJob, "test_flow", "failed", "bang"))
	})
	code, out, stderr := execute("list", inspectionRun, "--all-jobs", "--repo", resultTestRepo, "--status", "failed", "--status", "errored")
	if code != 0 || calls != 2 || !strings.Contains(out, "Results: available") || !strings.Contains(out, "test_flow in integration: bang") {
		t.Fatal(code, calls, out, stderr)
	}
	if strings.Contains(out, "may be incomplete") || strings.Contains(out, "limited") {
		t.Errorf("a retried job is still reported as incomplete:\n%s", out)
	}
	if !strings.Contains(out, "  tests.unit.test_flow in integration\n    avr tests view r --org ") {
		t.Errorf("missing the view command of a failed result in:\n%s", out)
	}
}

// The walk's status follows the API's order of states: a job an earlier page
// passed at a read limit outranks a job the last page reports as indexing.
func TestTestsAllJobsStatusKeepsTheApiOrderAcrossPages(t *testing.T) {
	calls := 0
	execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
		calls++
		if calls == 1 {
			jobs := []string{runJob(resultTestJob, "unit", "limited", 1, 0), runJob(runTestOtherJob, "integration", "available", 1, 0)}
			return 200, runPage("limited", runFailureFilters, `"c2"`, jobs, runRow(runTestOtherJob, "test_flow", "failed", "bang"))
		}
		jobs := []string{runJob(resultTestJob, "unit", "available", 1, 0), runJob(runTestOtherJob, "integration", "available", 1, 0), runJob(runTestIndexedJob, "e2e", "indexing", 1, 0)}
		return 200, runPage("indexing", runFailureFilters, `null`, jobs)
	})
	code, out, stderr := execute("list", inspectionRun, "--all-jobs", "--repo", resultTestRepo, "--status", "failed", "--status", "errored", "--json", "status,jobs", "--jq", `[.status, (.jobs[] | .status)] | join(",")`)
	if code != 0 || calls != 2 || strings.TrimSpace(out) != "limited,limited,available,indexing" {
		t.Fatal(code, calls, out, stderr)
	}
}

func TestTestsAllJobsEmptyContinuationSaysRemaining(t *testing.T) {
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		if r.URL.Query().Get("cursor") != "c2" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		return 200, runPage("available", `{"name":"test_login"}`, `null`, []string{runJob(resultTestJob, "unit", "available", 0, 0)})
	})
	code, out, stderr := execute("list", inspectionRun, "--all-jobs", "--repo", resultTestRepo, "--test", "test_login", "--cursor", "c2")
	if code != 0 || !strings.Contains(out, "No matching tests in the remaining retained results.") {
		t.Fatal(code, out, stderr)
	}
}

// With no rows and no job named as incomplete, the run's own status still
// says why nothing was listed, as it does for one job.
func TestTestsAllJobsEmptyListingStatesTheRunStatus(t *testing.T) {
	for _, tc := range []struct {
		status, filters string
		args            []string
		jobs            []string
	}{
		// The indexing job's counts record no failure, so it is not named.
		{"indexing", `{"outcome":"failed"}`, []string{"--status", "failed"}, []string{runJob(resultTestJob, "unit", "available", 0, 0), runJob(runTestIndexedJob, "lint", "indexing", 0, 0)}},
		{"unavailable", `{"name":"test_login"}`, []string{"--test", "test_login"}, []string{runJob(resultTestJob, "unit", "available", 0, 0)}},
		{"limited", `{}`, nil, []string{runJob(resultTestJob, "unit", "available", 0, 0)}},
	} {
		t.Run(tc.status, func(t *testing.T) {
			execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
				return 200, runPage(tc.status, tc.filters, `null`, tc.jobs)
			})
			code, out, stderr := execute(append([]string{"list", inspectionRun, "--all-jobs", "--repo", resultTestRepo}, tc.args...)...)
			if code != 0 || !strings.Contains(out, testListingMessage(tc.status)) {
				t.Fatal(code, out, stderr)
			}
			if strings.Contains(out, "No matching tests") || strings.Contains(out, "No test results returned") || strings.Contains(out, "may be incomplete") {
				t.Errorf("unexpected output:\n%s", out)
			}
		})
	}
}

func TestTestsAllJobsJSONKeepsWhatCouldNotBeListed(t *testing.T) {
	calls := 0
	execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
		calls++
		if calls == 1 {
			return 200, runPage("available", `{}`, `"c2"`, []string{runJob(resultTestJob, "unit", "available", 0, 0)})
		}
		return 200, runPage("unavailable", `{}`, `null`, []string{runJob(resultTestJob, "unit", "unavailable", 0, 0)})
	})
	code, out, stderr := execute("list", inspectionRun, "--all-jobs", "--repo", resultTestRepo, "--json", "run_id,status,jobs")
	var record struct {
		RunID  string `json:"run_id"`
		Status string
		Jobs   []map[string]any
	}
	if err := json.Unmarshal([]byte(out), &record); err != nil || code != 0 {
		t.Fatal(code, err, out, stderr)
	}
	if record.RunID != inspectionRun || record.Status != "unavailable" || len(record.Jobs) != 1 || record.Jobs[0]["status"] != "unavailable" {
		t.Fatal(out)
	}
}

func TestTestsAllJobsResolvesTheRunsRepository(t *testing.T) {
	paths := []string{}
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/orgs/" + inspectionOrg + "/workflow-runs/" + inspectionRun:
			return 200, fmt.Sprintf(`{"run_id":%q,"repository_id":%q,"jobs":[{"job_id":%q,"job_name":"unit"},{"job_id":%q,"job_name":"integration"}]}`, inspectionRun, resultTestRepo, resultTestJob, runTestOtherJob)
		case runTestsPath:
			return 200, runPage("available", `{}`, `"next"`, []string{runJob(resultTestJob, "unit", "available", 0, 0)}, runRow(resultTestJob, "test_login", "passed", ""))
		}
		t.Errorf("unexpected request %s", r.URL)
		return 500, `{}`
	})
	code, out, stderr := execute("list", inspectionRun, "--all-jobs", "-L", "1")
	if code != 0 || len(paths) != 2 || !strings.Contains(out, "Results shown: 1") {
		t.Fatal(code, paths, out, stderr)
	}
	if !strings.Contains(stderr, "More results available. Continue with --all-jobs, the same filters and --cursor next") {
		t.Error(stderr)
	}
	// Without --all-jobs, a run with several jobs points to it.
	if code, out, stderr := execute("list", inspectionRun); code != 1 || !strings.Contains(stderr, "read every job with --all-jobs") {
		t.Fatal(code, out, stderr)
	}
}

func TestTestsAllJobsRejectsOneJobSelectorsBeforeRequests(t *testing.T) {
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		t.Errorf("unexpected request %s", r.URL)
		return 500, `{}`
	})
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"list", inspectionRun, "--all-jobs", "--job", "unit"}, 2, "cannot be combined with --job or --summary"},
		{[]string{"list", inspectionRun, "--all-jobs", "--summary"}, 2, "cannot be combined with --job or --summary"},
		{[]string{"list", resultTestJob, "--all-jobs"}, 2, "--all-jobs reads a whole run"},
		{[]string{"list", "https://github.com/owner/repo/actions/runs/123/job/456", "--all-jobs"}, 2, "Pass the run or attempt URL, or omit --all-jobs to read that job."},
		{[]string{"list", "https://github.com/owner/repo/pulls/1", "--all-jobs"}, 2, "With --all-jobs, use an Avrea run ID"},
	} {
		code, out, stderr := execute(tc.args...)
		if code != tc.code || out != "" || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: %d %q %q", tc.args, code, out, stderr)
		}
	}
}

func TestTestsAllJobsRejectsInvalidPagesWithoutOutput(t *testing.T) {
	unit := runJob(resultTestJob, "unit", "available", 0, 0)
	for name, page := range map[string]string{
		"other run":          strings.Replace(runPage("available", `{}`, `null`, []string{unit}), inspectionRun, "run-019a0000000070008000000000000009", 1),
		"no jobs list":       strings.Replace(runPage("available", `{}`, `null`, nil), `"jobs":[]`, `"jobs":null`, 1),
		"row of no job":      runPage("available", `{}`, `null`, []string{unit}, runRow(runTestOtherJob, "test_x", "passed", "")),
		"repeated job":       runPage("available", `{}`, `null`, []string{unit, unit}),
		"unknown job status": runPage("available", `{}`, `null`, []string{runJob(resultTestJob, "unit", "pending", 0, 0)}),
		"unknown run status": runPage("pending", `{}`, `null`, []string{unit}),
		"counts off total":   runPage("available", `{}`, `null`, []string{strings.Replace(unit, `"total_count":3`, `"total_count":4`, 1)}),
	} {
		t.Run(name, func(t *testing.T) {
			execute := repositoryTestsDeps(t, func(*http.Request) (int, string) { return 200, page })
			if code, out, stderr := execute("list", inspectionRun, "--all-jobs", "--repo", resultTestRepo); code != 1 || out != "" || !strings.Contains(stderr, "The API returned invalid") {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestTestsAllJobsInvalidCursorSaysToRestart(t *testing.T) {
	execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
		return 400, `{"detail":"Invalid cursor"}`
	})
	code, out, stderr := execute("list", inspectionRun, "--all-jobs", "--repo", resultTestRepo, "--cursor", "stale")
	if code != 1 || out != "" || !strings.Contains(stderr, "Use the same filters the cursor was issued with, or restart without --cursor.") {
		t.Fatal(code, out, stderr)
	}
}
