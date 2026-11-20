package commands

import "testing"

func TestNoFailedStepsReportsMissingOutcomesForEveryProvider(t *testing.T) {
	const job = "job-019a0000000070008000000000000005"
	concluded := []map[string]any{{"name": "build", "conclusion": "success"}}
	if got := noFailedSteps(job, concluded); got != "No failed steps." {
		t.Fatal(got)
	}
	for _, steps := range [][]map[string]any{nil, {{"name": "build", "conclusion": "success"}, {"name": "test"}}} {
		if got := noFailedSteps(job, steps); got == "No failed steps." {
			t.Fatal(steps, got)
		}
	}
}
