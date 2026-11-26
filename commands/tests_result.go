package commands

import (
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var testViewFields = []string{"organization_id", "repository_id", "result_id", "status", "job_id", "execution_id", "commit_sha", "report_finished_at", "data", "failure_start"}
var testResultStates = []string{"available", "expired", "not_recorded"}
var testListingStates = []string{"available", "expired", "not_recorded", "indexing", "unavailable", "limited"}

// not_failed answers a result that did not fail; view asks only for failures.
var testFailureStarts = []string{"started", "no_earlier_pass", "inconsistent", "branch_unknown"}

func newTestsView(d Dependencies) *cobra.Command {
	var org, repo string
	var web bool
	var o jsonOptions
	c := &cobra.Command{Use: "view RESULT", Short: "Read one recorded test result and when its failure started.", Args: positionalArguments("RESULT")}
	orgFlag(c, &org)
	c.Flags().StringVarP(&repo, "repo", "R", "", "Repository (owner/name or rep-ID). Defaults to the current git repository.")
	c.Flags().BoolVar(&web, "web", false, "Open the result in a browser.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		// A result ID is opaque and follows the cursor rules: 1 to 1024
		// characters without surrounding whitespace.
		result := args[0]
		if !validTestCursor(result) {
			return usageError(cmd, "Invalid value for RESULT: use a result_id from avr tests search, rank or list")
		}
		if web && cmd.Flags().Changed("json") {
			return &ExitError{2, "Error: --web cannot be combined with --json."}
		}
		if done, err := o.prepare(cmd, d, testViewFields); done || err != nil {
			return err
		}
		cfg, api, organization, repository, err := testRepository(cmd, d, org, repo)
		if err != nil {
			return err
		}
		if web {
			slug := orgSlug(cmd.Context(), cfg, api, organization)
			target := consoleURL(cfg.Host) + "/org/" + url.PathEscape(slug) + "/repos/" + url.PathEscape(repository) + "/tests/results/" + url.PathEscape(result)
			label := target
			if d.IsTerminal() {
				label = "Opening: " + target
			}
			if _, err = fmt.Fprintln(d.Out, label); err != nil || !d.IsTerminal() {
				return err
			}
			return d.openWeb(cmd.Context(), target)
		}
		path := "/orgs/" + organization + "/repos/" + repository + "/tests/results/" + url.PathEscape(result)
		response, err := getTestResults(cmd.Context(), api, cfg, path, nil, "read the test result", "Check the result ID, repository and organization, and that Repository Tests are enabled. A result ID belongs to one repository.")
		if err != nil {
			return err
		}
		data, _ := response["data"].(map[string]any)
		state, _ := response["status"].(string)
		if response["result_id"] != result || !slices.Contains(testResultStates, state) || (response["data"] != nil && data == nil) || (state == "available" && !slices.Contains(testOutcomes, fieldText(data, "outcome"))) {
			return &ExitError{1, "Error: The API returned an invalid test result."}
		}
		response["failure_start"] = nil
		// JSON that leaves out failure_start does not need the second read.
		wanted := true
		if cmd.Flags().Changed("json") {
			fields, _ := output.Fields(o.fields, testViewFields)
			wanted = slices.Contains(fields, "failure_start")
		}
		if wanted && state == "available" && (data["outcome"] == "failed" || data["outcome"] == "errored") {
			start, err := getTestResults(cmd.Context(), api, cfg, path+"/failure-start", nil, "check when the test started failing", "Check that Repository Tests are enabled, then retry.")
			if err != nil {
				return err
			}
			if !validFailureStart(start) {
				return &ExitError{1, "Error: The API returned an invalid failure start."}
			}
			response["failure_start"] = start
		}
		response["organization_id"], response["repository_id"] = organization, repository
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, response, testViewFields)
		}
		return showTestResult(d.Out, response)
	})
	return c
}

// validFailureStart checks that a failure start holds what its statements
// need: the runs it names, and one commit for an inconsistent outcome.
func validFailureStart(start map[string]any) bool {
	state, _ := start["status"].(string)
	kind, _ := start["start"].(string)
	if !slices.Contains(testListingStates, state) || (start["start"] != nil && !slices.Contains(testFailureStarts, kind)) || (state == "available") != (kind != "") {
		return false
	}
	first, hasFirst := start["first_failure"].(map[string]any)
	last, hasLast := start["last_pass"].(map[string]any)
	switch kind {
	case "started":
		return hasFirst && hasLast
	case "inconsistent":
		return hasFirst && hasLast && fieldText(first, "commit_sha") != "" && first["commit_sha"] == last["commit_sha"]
	case "no_earlier_pass":
		return hasFirst
	default:
		return true
	}
}

func showTestResult(w io.Writer, response map[string]any) error {
	var b strings.Builder
	data, _ := response["data"].(map[string]any)
	if data != nil {
		fmt.Fprintf(&b, "Test: %s  Suite: %s  Framework: %s\n", testLabel(data), testText(data["suite"]), testText(data["framework"]))
	}
	fmt.Fprintf(&b, "Result: %s\n", testText(response["status"]))
	if data != nil {
		fmt.Fprintf(&b, "Outcome: %s  Time (ms): %s\n", testText(data["outcome"]), testText(data["duration_ms"]))
	}
	fmt.Fprintf(&b, "Job: %s\n", testText(response["job_id"]))
	if response["execution_id"] != nil {
		fmt.Fprintf(&b, "Execution: %s\n", testText(response["execution_id"]))
	}
	fmt.Fprintf(&b, "Commit: %s  Finished: %s\n", testText(response["commit_sha"]), testTime(response["report_finished_at"]))
	fmt.Fprintf(&b, "Organization: %s  Repository: %s\n", testText(response["organization_id"]), testText(response["repository_id"]))
	switch response["status"] {
	case "expired":
		b.WriteString("The report has expired; its test content is no longer retained.\n")
	case "not_recorded":
		b.WriteString("No test report is recorded for this result.\n")
	}
	if data != nil && testValuePresent(data["failure_summary"]) {
		// The summary is the reason to open a result: keep its lines, but
		// escape terminal controls as every other untrusted value is.
		text, _ := data["failure_summary"].(string)
		fmt.Fprintf(&b, "\nFailure summary:\n%s\n", strings.TrimRight(output.MultilineText(text), "\n"))
	}
	if start, ok := response["failure_start"].(map[string]any); ok {
		b.WriteString("\n" + failureStartText(start))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// failureStartText states what the API established about when a failure
// started, and only that: a commit range is named only where the API says
// runs follow commit order on the branch.
func failureStartText(start map[string]any) string {
	var b strings.Builder
	b.WriteString("When it started\n")
	if testValuePresent(start["branch"]) {
		fmt.Fprintf(&b, "Branch: %s\n", testText(start["branch"]))
	}
	run := func(key string) string {
		r, _ := start[key].(map[string]any)
		return fmt.Sprintf("%s (commit %s, job %s)", testTime(r["started_at"]), testText(r["commit_sha"]), testText(r["job_id"]))
	}
	first, _ := start["first_failure"].(map[string]any)
	last, _ := start["last_pass"].(map[string]any)
	switch start["start"] {
	case "started":
		fmt.Fprintf(&b, "Started failing at %s.\nLast passed at %s.\n", run("first_failure"), run("last_pass"))
		fmt.Fprintf(&b, "Failed in %s reports since then, including this one.\n", testText(start["failed_reports"]))
		if from, to := fieldText(last, "commit_sha"), fieldText(first, "commit_sha"); from != "" && to != "" && from != to {
			fmt.Fprintf(&b, "If runs on this branch follow commit order, the change is in %s..%s.\n", output.CellText(from), output.CellText(to))
		}
	case "no_earlier_pass":
		fmt.Fprintf(&b, "Failed in every checked report on this branch where it ran: %s, the oldest at %s.\n", testText(start["failed_reports"]), run("first_failure"))
		b.WriteString("The failure may have started before the checked reports.\n")
	case "inconsistent":
		fmt.Fprintf(&b, "Passed at %s, on the commit of the first failure at %s.\n", run("last_pass"), run("first_failure"))
		b.WriteString("The outcome changed without a commit change, so this test looks flaky.\n")
	case "branch_unknown":
		b.WriteString("The result's run has no branch of this repository to compare with, as when it names none or comes from a fork.\n")
	default:
		switch start["status"] {
		case "indexing":
			b.WriteString("Not checked: a report this check needs is still being indexed. Retry later.\n")
		case "unavailable":
			b.WriteString("Not checked: a report this check needs cannot currently be read. Retry later.\n")
		case "limited":
			b.WriteString("Not checked: the check reached a read limit. Retry later.\n")
		case "expired":
			b.WriteString("Not checked: the result's report is no longer retained.\n")
		default:
			b.WriteString("Not checked: the result's report is not recorded.\n")
		}
	}
	if scope, ok := start["scope"].(map[string]any); ok {
		fmt.Fprintf(&b, "Reports checked: %s%s\n", testText(scope["reports_searched"]), reportsNotRead(scope))
	}
	return b.String()
}
