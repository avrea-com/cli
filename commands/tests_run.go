package commands

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var testRunFields = []string{"organization_id", "repository_id", "run_id", "status", "filters", "jobs", "data", "pagination"}

// runTestResultWalk reads a run's results job by job. Every page lists every
// job, and a job whose report could not be read is reported on that page
// only. The next page either retries that job, delivering its rows, or has
// passed it. The walk keeps a job's failure until a later page delivers rows
// of that job, so the jobs it returns name every job that may be incomplete.
// status is the last page's; the command adds what the kept failures imply.
func runTestResultWalk(path, run string) testResultWalk {
	invalid := &ExitError{1, "Error: The API returned invalid run test results."}
	return testResultWalk{
		path: path, action: "read run test results", invalid: "run test results", pageLimit: 100,
		hint: "Check the run, repository and organization, and that Repository Tests are enabled.",
		check: func(page, first map[string]any) error {
			values, ok := page["jobs"].([]any)
			jobs := objectList(page["jobs"])
			status, _ := page["status"].(string)
			if page["run_id"] != run || !ok || len(values) != len(jobs) || !slices.Contains(testListingStates, status) {
				return invalid
			}
			listed := map[string]bool{}
			for _, job := range jobs {
				id, _ := job["job_id"].(string)
				state, _ := job["status"].(string)
				if !testJobID().MatchString(id) || listed[id] || !slices.Contains(testListingStates, state) {
					return invalid
				}
				listed[id] = true
				if err := validateTestCounts(job); err != nil {
					return err
				}
			}
			for _, row := range page["data"].([]any) {
				if id, _ := row.(map[string]any)["job_id"].(string); !listed[id] {
					return invalid
				}
			}
			if first == nil {
				return nil
			}
			// Later pages list the jobs as they are now, including jobs a
			// running workflow added since the first page.
			delivered := map[string]bool{}
			for _, row := range page["data"].([]any) {
				delivered[row.(map[string]any)["job_id"].(string)] = true
			}
			failed := map[string]any{}
			for _, job := range objectList(first["jobs"]) {
				if testListingMessage(job["status"]) != "" {
					failed[job["job_id"].(string)] = job["status"]
				}
			}
			for _, job := range jobs {
				id := job["job_id"].(string)
				if state, ok := failed[id]; ok && !delivered[id] && testListingMessage(job["status"]) == "" {
					job["status"] = state
				}
			}
			first["jobs"], first["status"] = page["jobs"], status
			return nil
		},
	}
}

// resolveTestsRun finds the organization, run and repository that
// --all-jobs reads. A run ID with --repo needs no run lookup.
func resolveTestsRun(cmd *cobra.Command, d Dependencies, cfg config.Config, api *client.Client, target, org, repo string) (string, string, string, error) {
	ctx := cmd.Context()
	ref, err := parseRunReference(target, cfg.Host)
	if err != nil {
		var exit *ExitError
		if errors.Is(err, errInvalidRunReference) || (errors.As(err, &exit) && exit.Code == 2) {
			err = usageError(cmd, "Invalid value for RUN_OR_JOB: With --all-jobs, use an Avrea run ID, a positive GitHub run ID, a GitHub Actions run URL or an Avrea console run URL.")
		}
		return "", "", "", err
	}
	if ref.platformJobID != "" {
		return "", "", "", usageError(cmd, "This is a job URL, but --all-jobs reads a whole run. Pass the run or attempt URL, or omit --all-jobs to read that job.")
	}
	ref, organization, err := resolveRunOrg(ctx, d, cfg, api, ref, org)
	if err != nil {
		return "", "", "", err
	}
	run, repository := ref.id, ""
	if repo != "" {
		if repository, err = requiredWorkflowRepo(ctx, d, cfg, api, organization, repo); err != nil {
			return "", "", "", err
		}
	}
	if run == "" || repository == "" {
		record, err := resolveRun(ctx, cfg, api, organization, ref, nil, "resolve the run")
		if err != nil {
			return "", "", "", err
		}
		run = fieldText(record, "run_id")
		if repository == "" {
			repository = fieldText(record, "repository_id")
		}
	}
	if !runIDPattern().MatchString(run) || !repositoryIDPattern().MatchString(repository) {
		return "", "", "", &ExitError{1, "Error: The run has no available repository. Check the run and selected organization."}
	}
	return organization, run, repository, nil
}

// incompleteRunJobs are the jobs whose results may not all have been
// listed, except those whose counts show no result with a selected outcome.
// Counts cover a job's whole report before any name, suite or class filter.
func incompleteRunJobs(jobs []map[string]any, outcomes []string) []map[string]any {
	selected := outcomes
	if len(selected) == 0 {
		selected = testOutcomes
	}
	incomplete := []map[string]any{}
	for _, job := range jobs {
		if testListingMessage(job["status"]) == "" {
			continue
		}
		if counts, ok := job["counts"].(map[string]any); ok {
			var count int64
			for _, outcome := range selected {
				count += testCount(counts[outcome])
			}
			if count == 0 {
				continue
			}
		}
		incomplete = append(incomplete, job)
	}
	return incomplete
}

// runWalkStatus is the status of a whole walk: the last page's, or that of a
// job an earlier page left incomplete. Where several apply, the API's own
// order decides, so a read that reached a limit is not reported as indexing.
func runWalkStatus(last any, incomplete []map[string]any) any {
	states := []any{last}
	for _, job := range incomplete {
		states = append(states, job["status"])
	}
	for _, state := range []string{"limited", "unavailable", "indexing"} {
		if slices.Contains(states, any(state)) {
			return state
		}
	}
	return last
}

// incompleteRunText names each incomplete job with the command that reads it
// alone, in the organization and repository this run was read from.
func incompleteRunText(response map[string]any, jobs []map[string]any, outcomes []string) []string {
	what := "results"
	if len(outcomes) > 0 {
		what = strings.Join(outcomes, " or ") + " results"
	}
	scope := testScopeFlags(response)
	lines := []string{}
	for _, job := range jobs {
		name, id := testText(job["name"]), testText(job["job_id"])
		var line string
		switch job["status"] {
		case "indexing":
			line = fmt.Sprintf("Results of %s (%s) were not listed: its report is still being indexed.", name, id)
		case "unavailable":
			line = fmt.Sprintf("Results of %s (%s) may be incomplete: a read of its report failed.", name, id)
		default:
			line = fmt.Sprintf("Results of %s (%s) may be incomplete: a read of its report reached a limit.", name, id)
		}
		if counts, ok := job["counts"].(map[string]any); ok {
			var count int64
			for _, outcome := range outcomes {
				count += testCount(counts[outcome])
			}
			if len(outcomes) == 0 {
				count = testCount(job["total_count"])
			}
			line += fmt.Sprintf(" Its report records %d %s in total.", count, what)
		}
		lines = append(lines, line+" Read the job with avr tests list "+id+scope+".")
	}
	return lines
}

func showRunTestResults(w io.Writer, response map[string]any, outcomes []string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Run: %s\nOrganization: %s  Repository: %s\nResults: %s\n", testText(response["run_id"]), testText(response["organization_id"]), testText(response["repository_id"]), testText(response["status"]))
	jobs := objectList(response["jobs"])
	names := map[string]string{}
	cells := make([][]string, 0, len(jobs))
	for _, job := range jobs {
		id, _ := job["job_id"].(string)
		names[id] = testText(job["name"])
		counts, _ := job["counts"].(map[string]any)
		cells = append(cells, []string{testText(job["name"]), id, testText(job["reporting_state"]), testText(job["status"]), testText(job["total_count"]), testText(counts["failed"]), testText(counts["errored"])})
	}
	if err := output.Grid(&b, []string{"Job", "Job ID", "Reporting", "Results", "Tests", "Failed", "Errored"}, cells); err != nil {
		return err
	}
	b.WriteString("Counts cover every recorded occurrence of each job's latest execution before filtering.\n")
	rows := response["data"].([]any)
	filtered := len(response["filters"].(map[string]any)) > 0
	label := "Results shown"
	if filtered {
		label = "Matching results shown"
	}
	fmt.Fprintf(&b, "%s: %d\n", label, len(rows))
	incomplete := incompleteRunJobs(jobs, outcomes)
	if len(rows) > 0 {
		cells = make([][]string, 0, len(rows))
		for _, item := range rows {
			row := item.(map[string]any)
			cells = append(cells, []string{names[row["job_id"].(string)], testText(row["suite"]), testText(row["class_name"]), testText(row["name"]), testText(row["outcome"]), testText(row["duration_ms"])})
		}
		if err := output.Grid(&b, []string{"Job", "Suite", "Class", "Test", "Outcome", "Time (ms)"}, cells); err != nil {
			return err
		}
		label := func(row map[string]any) string {
			return testLabel(row) + " in " + names[row["job_id"].(string)]
		}
		writeFailureSummaries(&b, rows, label)
		writeResultCommands(&b, response, rows, label)
	} else if len(incomplete) == 0 {
		switch {
		case response["pagination"].(map[string]any)["next_cursor"] != nil:
			b.WriteString("No matches in the searched portion.\n")
		case filtered:
			b.WriteString("No matching tests in the remaining retained results.\n")
		default:
			b.WriteString("No test results returned. Missing or expired reports do not mean that zero tests ran.\n")
		}
	}
	if len(incomplete) > 0 {
		b.WriteString("\n" + strings.Join(incompleteRunText(response, incomplete, outcomes), "\n") + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}
