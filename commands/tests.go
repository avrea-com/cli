package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var testSnapshotFields = []string{"job_id", "execution_id", "status", "reporting_state", "reason", "commit_sha", "observed_at", "total_count"}
var testResultFields = append(append([]string{"organization_id", "repository_id"}, testSnapshotFields...), "data", "pagination")
var testJobID = lazyRegexp("^(job|awj)-[0-9a-f]{32}$")

// NewTests reads the retained results of a job, including repeated occurrences.
func NewTests(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	group := &cobra.Command{Use: "tests", Short: "Read a job's tests, timings and recorded failure messages.", Annotations: map[string]string{"job-shorthand": "list"}}
	var org, repo, cursor string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list JOB", Short: "Read the latest confirmed execution of JOB, including repeated tests.", Args: positionalArguments("JOB")}
	orgFlag(c, &org)
	c.Flags().StringVar(&repo, "repo", "", "Repository (owner/name or rep-ID). Defaults to the job's repository.")
	c.Flags().IntVarP(&limit, "limit", "L", 100, "Maximum occurrences to return; short pages are followed, up to 20 result pages.")
	c.Flags().StringVar(&cursor, "cursor", "", "Continue from a previous response's next_cursor.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		job := args[0]
		if !testJobID().MatchString(job) {
			return &ExitError{2, "Error: Invalid value for JOB: use the Avrea job ID from 'avr job list' (job- or awj- followed by 32 lowercase hex characters)"}
		}
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if done, err := o.prepare(cmd, d, testResultFields); done || err != nil {
			return err
		}
		var start *string
		if cmd.Flags().Changed("cursor") {
			if !validTestCursor(cursor) {
				return &ExitError{2, "Error: --cursor must be 1..1024 chars and not surrounded by whitespace"}
			}
			start = &cursor
		}
		cfg, api, organization, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		repository := repo
		if repository != "" {
			repository, err = requiredWorkflowRepo(cmd.Context(), d, cfg, api, organization, repo)
		} else {
			var response map[string]any
			response, err = getTestResults(cmd.Context(), api, cfg, "/orgs/"+organization+"/jobs/"+job, nil, "read the job", "Check the job ID, or pass --org with the job's organization.")
			if err == nil {
				data := response
				if nested, exists := response["data"]; exists {
					data, _ = nested.(map[string]any)
				}
				repository, _ = data["repository_id"].(string)
				if !cloneRepositoryID().MatchString(repository) {
					return &ExitError{1, "Error: The job has no available repository. Check the job and selected organization."}
				}
				if data["job_id"] != job {
					return &ExitError{1, "Error: The API returned a different job. Retry with the exact Avrea job ID."}
				}
			}
		}
		if err != nil {
			return err
		}
		response, err := collectTestResults(cmd.Context(), api, cfg, "/orgs/"+organization+"/repos/"+repository+"/jobs/"+job+"/tests", job, limit, start)
		if err != nil {
			return err
		}
		response["organization_id"], response["repository_id"] = organization, repository
		if cmd.Flags().Changed("json") {
			err = o.record(cmd, d, response, testResultFields)
		} else {
			err = showTestResults(d.Out, response)
		}
		if err != nil {
			return err
		}
		if next, ok := response["pagination"].(map[string]any)["next_cursor"].(string); ok {
			_, err = fmt.Fprintf(d.Err, "More results available. Continue with --cursor %s\n", shellQuotePython(testText(next)))
		}
		return err
	})
	group.AddCommand(c)
	return group
}

// Expand only a job-prefixed first argument of the actual tests group. Unknown
// verbs and misplaced options remain usage errors, as in the Python command.
func expandTestsShorthand(root *cobra.Command, args []string) []string {
	command, remaining, _ := root.Find(args)
	if command == nil || command.Annotations["job-shorthand"] != "list" {
		return args
	}
	for _, arg := range remaining {
		switch arg {
		case "-v", "--verbose", "--no-color", "--links", "--no-links":
			continue
		}
		if !strings.HasPrefix(arg, "job-") && !strings.HasPrefix(arg, "awj-") {
			return args
		}
		for i, original := range args {
			if original == arg {
				result := append([]string(nil), args[:i]...)
				result = append(result, "list")
				return append(result, args[i:]...)
			}
		}
	}
	return args
}

func validTestCursor(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && utf8.RuneCountInString(value) <= 1024
}

func getTestResults(ctx context.Context, api *client.Client, cfg config.Config, path string, query url.Values, action, hint string) (map[string]any, error) {
	response, err := api.Get(ctx, path, query)
	if err == nil {
		return response, nil
	}
	result := apiError(cfg, err, action)
	var status *client.HTTPError
	if errors.As(err, &status) && status.Status == 404 {
		first, rest, found := strings.Cut(result.Error(), "\n")
		if found {
			rest = "\n" + rest
		}
		result = errors.New(first + "\n  Hint: " + hint + rest)
	}
	return nil, result
}

func collectTestResults(ctx context.Context, api *client.Client, cfg config.Config, path, job string, limit int, cursor *string) (map[string]any, error) {
	rows := make([]any, 0)
	var snapshot map[string]any
	seen := map[string]bool{}
	if cursor != nil {
		seen[*cursor] = true
	}
	for range 20 {
		remaining := limit - len(rows)
		query := url.Values{"limit": {strconv.Itoa(remaining)}}
		if cursor != nil {
			query.Set("cursor", *cursor)
		}
		page, err := getTestResults(ctx, api, cfg, path, query, "read job test results", "If you passed --cursor, restart without it. Otherwise check that Repository Tests are enabled and that the job belongs to the repository.")
		if err != nil {
			return nil, err
		}
		data, dataOK := page["data"].([]any)
		pagination, paginationOK := page["pagination"].(map[string]any)
		if page["job_id"] != job || !dataOK || !paginationOK {
			return nil, &ExitError{1, "Error: The API returned invalid job test results."}
		}
		if len(data) > remaining {
			return nil, &ExitError{1, "Error: The API returned an invalid test result page."}
		}
		for _, row := range data {
			if _, ok := row.(map[string]any); !ok {
				return nil, &ExitError{1, "Error: The API returned an invalid test result page."}
			}
		}
		if snapshot == nil {
			snapshot = page
		} else {
			for _, key := range testSnapshotFields {
				if !reflect.DeepEqual(page[key], snapshot[key]) {
					return nil, &ExitError{1, "Error: Job results changed while reading pages. Retry without --cursor."}
				}
			}
		}
		cursor = nil
		if value := pagination["next_cursor"]; value != nil {
			next, ok := value.(string)
			if !ok || !validTestCursor(next) {
				return nil, &ExitError{1, "Error: The API returned an invalid next cursor."}
			}
			if seen[next] {
				return nil, &ExitError{1, "Error: The API repeated a test-results cursor; stopping pagination."}
			}
			seen[next] = true
			cursor = &next
		}
		rows = append(rows, data...)
		if cursor == nil || len(rows) == limit {
			break
		}
	}
	var next any
	if cursor != nil {
		next = *cursor
	}
	snapshot["data"], snapshot["pagination"] = rows, map[string]any{"next_cursor": next}
	return snapshot, nil
}

func testText(value any) string {
	if value == nil {
		return "unknown"
	}
	return output.CellText(pythonValue(value))
}
func testValuePresent(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return v != ""
	case bool:
		return v
	case json.Number:
		n, _ := v.Float64()
		return n != 0
	case []any:
		return len(v) != 0
	case map[string]any:
		return len(v) != 0
	default:
		return true
	}
}
func showTestResults(w io.Writer, response map[string]any) error {
	var b strings.Builder
	text := func(key string) string { return testText(response[key]) }
	fmt.Fprintf(&b, "Job: %s\nOrganization: %s  Repository: %s\nExecution: %s  Commit: %s\nReporting: %s  Results: %s\n",
		text("job_id"), text("organization_id"), text("repository_id"), text("execution_id"), text("commit_sha"), text("reporting_state"), text("status"))
	if testValuePresent(response["reason"]) {
		fmt.Fprintf(&b, "Reason: %s\n", text("reason"))
	}
	rows := response["data"].([]any)
	fmt.Fprintf(&b, "Retained occurrences: %s; shown: %d\n", text("total_count"), len(rows))
	if len(rows) > 0 {
		cells := make([][]string, 0, len(rows))
		for _, item := range rows {
			row := item.(map[string]any)
			cells = append(cells, []string{testText(row["suite"]), testText(row["class_name"]), testText(row["name"]), testText(row["outcome"]), testText(row["duration_ms"])})
		}
		if err := output.Grid(&b, []string{"Suite", "Class", "Test", "Outcome", "Time (ms)"}, cells); err != nil {
			return err
		}
		for _, item := range rows {
			row := item.(map[string]any)
			if !testValuePresent(row["failure_summary"]) {
				continue
			}
			parts := []string{}
			for _, key := range []string{"class_name", "name"} {
				if testValuePresent(row[key]) {
					parts = append(parts, testText(row[key]))
				}
			}
			label := strings.Join(parts, ".")
			if label == "" {
				label = "unknown"
			}
			fmt.Fprintf(&b, "\n%s: %s\n", label, testText(row["failure_summary"]))
		}
	} else if response["status"] == "available" {
		if !testValuePresent(response["total_count"]) && response["total_count"] != nil {
			b.WriteString("The retained report contains zero test occurrences. This does not establish that no tests ran.\n")
		} else {
			b.WriteString("No more results.\n")
		}
	} else {
		b.WriteString("No test results returned. Missing or expired reports do not mean that zero tests ran.\n")
	}
	if response["reporting_state"] == "complete" {
		b.WriteString("Complete describes the collected reports, not whether every expected test ran.\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}
