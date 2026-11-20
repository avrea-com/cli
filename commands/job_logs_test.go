package commands

import (
	"strings"
	"testing"
)

// Step outcomes are missing only where a failure should have left some: a
// job that was skipped, succeeded or is still running has no failed steps.
func TestNoFailedStepsReportsMissingOutcomesOfFailedJobs(t *testing.T) {
	const job = "job-019a0000000070008000000000000005"
	concluded := []map[string]any{{"name": "build", "conclusion": "success"}}
	running := []map[string]any{{"name": "build", "conclusion": "success"}, {"name": "test"}}
	for _, test := range []struct {
		conclusion string
		steps      []map[string]any
		missing    bool
	}{
		{"failure", concluded, false},
		{"failure", nil, true},
		{"failure", running, true},
		{"cancelled", running, true},
		{"skipped", nil, false},
		{"success", nil, false},
		{"", running, false},
		{"", nil, false},
	} {
		got := noFailedSteps(job, test.conclusion, test.steps)
		if (got != "No failed steps.") != test.missing || test.missing && !strings.Contains(got, "avr job logs "+job) {
			t.Errorf("%q %v: %s", test.conclusion, test.steps, got)
		}
	}
}

func TestRunLogsFailedLeavesOutSkippedJobs(t *testing.T) {
	d, _, _ := testDeps(t)
	jobs := []map[string]any{
		{"job_id": "job-skipped", "job_name": "deploy", "conclusion": "skipped"},
		{"job_id": "job-passed", "job_name": "lint", "conclusion": "success"},
		{"job_id": "job-failed", "job_name": "test", "conclusion": "failure"},
		{"job_id": "job-running", "job_name": "build", "steps": []any{map[string]any{"name": "compile"}}},
	}
	content, err := runLogs(t.Context(), d, nil, jobs, true, "https://api.example.test", "")
	if err != nil || strings.Contains(content, "job-skipped") || strings.Contains(content, "job-passed") ||
		strings.Count(content, "Step outcomes are unavailable") != 1 || strings.Count(content, "No failed steps.") != 1 {
		t.Fatal(content, err)
	}
}
