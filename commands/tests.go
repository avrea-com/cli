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

var testSnapshotFields = []string{"job_id", "execution_id", "status", "reporting_state", "reason", "commit_sha", "observed_at", "total_count", "filters"}
var testResultFields = append(append([]string{"organization_id", "repository_id"}, testSnapshotFields...), "data", "pagination")
var testJobID = lazyRegexp("^(job|awj)-[0-9a-f]{32}$")

// NewTests reads the retained results of a job, including repeated occurrences.
func NewTests(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	group := &cobra.Command{Use: "tests", Short: "Read a job's tests, timings and recorded failure messages.", Annotations: map[string]string{"job-shorthand": "list"}}
	var org, repo, cursor, jobName, testName, suite, className, outcome string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list RUN_OR_JOB", Short: "Read the latest confirmed execution of JOB, including repeated tests.", Args: positionalArguments("RUN_OR_JOB")}
	orgFlag(c, &org)
	c.Flags().StringVar(&repo, "repo", "", "Repository (owner/name or rep-ID). Defaults to the job's repository.")
	c.Flags().IntVarP(&limit, "limit", "L", 100, "Maximum occurrences to return; short pages are followed, up to 20 result pages.")
	c.Flags().StringVar(&cursor, "cursor", "", "Continue from a previous response's next_cursor.")
	c.Flags().StringVar(&jobName, "job", "", "Select one run job by case-insensitive name substring.")
	c.Flags().StringVar(&testName, "test", "", "Exact test name (case-sensitive; no wildcards).")
	c.Flags().StringVar(&suite, "suite", "", "Exact suite name; combined with other filters using AND.")
	c.Flags().StringVar(&className, "class", "", "Exact nonempty class name.")
	c.Flags().StringVar(&outcome, "status", "", "Recorded test outcome.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if limit < 1 || limit > 1000 {
			return usageError(cmd, "Invalid value for '--limit' / '-L': "+strconv.Itoa(limit)+" is not in the range 1<=x<=1000.")
		}
		if cmd.Flags().Changed("status") && outcome != "passed" && outcome != "failed" && outcome != "skipped" && outcome != "errored" {
			return usageError(cmd, "Invalid value for '--status': "+pythonQuote(outcome)+" is not one of 'passed', 'failed', 'skipped', 'errored'.")
		}
		if done, err := o.prepare(cmd, d, testResultFields); done || err != nil {
			return err
		}
		filters := map[string]any{}
		for _, filter := range []struct {
			flag, key, value string
			max              int
		}{
			{"test", "name", testName, 4096}, {"suite", "suite", suite, 256}, {"class", "class_name", className, 512}, {"status", "outcome", outcome, 0},
		} {
			if !cmd.Flags().Changed(filter.flag) {
				continue
			}
			if filter.max > 0 && (utf8.RuneCountInString(filter.value) < 1 || utf8.RuneCountInString(filter.value) > filter.max) {
				return usageError(cmd, fmt.Sprintf("Invalid value for --%s: use between 1 and %d characters", filter.flag, filter.max))
			}
			filters[filter.key] = filter.value
		}
		var start *string
		if cmd.Flags().Changed("cursor") {
			if !validTestCursor(cursor) {
				return usageError(cmd, "Invalid value: --cursor must be 1..1024 chars and not surrounded by whitespace")
			}
			start = &cursor
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		organization, job, err := resolveTestsJob(cmd, d, cfg, api, args[0], org, jobName, cmd.Flags().Changed("job"))
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
		response, err := collectTestResults(cmd.Context(), api, cfg, "/orgs/"+organization+"/repos/"+repository+"/jobs/"+job+"/tests", job, limit, start, filters)
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
			message := "More results available."
			if len(filters) > 0 {
				message = "Search incomplete; more of the report remains."
			}
			_, err = fmt.Fprintf(d.Err, "%s Continue with the same filters and --cursor %s\n", message, shellQuotePython(testText(next)))
		}
		return err
	})
	group.AddCommand(c)
	return group
}

// Expand a run/job reference in the actual tests group. Unknown
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
		if !strings.HasPrefix(arg, "job-") && !strings.HasPrefix(arg, "awj-") && !strings.HasPrefix(arg, "run-") && !strings.HasPrefix(arg, "awr-") && !strings.HasPrefix(arg, "https://") && !decimalPattern().MatchString(arg) {
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
	if errors.As(err, &status) && (status.Status == 404 || (status.Status == 400 && action == "read job test results")) {
		if status.Status == 400 {
			hint = "Use the same filters the cursor was issued with, or restart without --cursor."
		}
		first, rest, found := strings.Cut(result.Error(), "\n")
		if found {
			rest = "\n" + rest
		}
		result = errors.New(first + "\n  Hint: " + hint + rest)
	}
	return nil, result
}

func collectTestResults(ctx context.Context, api *client.Client, cfg config.Config, path, job string, limit int, cursor *string, filters map[string]any) (map[string]any, error) {
	rows := make([]any, 0)
	var snapshot map[string]any
	seen := map[string]bool{}
	if cursor != nil {
		seen[*cursor] = true
	}
	for range 20 {
		remaining := limit - len(rows)
		query := url.Values{"limit": {strconv.Itoa(remaining)}}
		for key, value := range filters {
			query.Set(key, "eq."+value.(string))
		}
		if cursor != nil {
			query.Set("cursor", *cursor)
		}
		page, err := getTestResults(ctx, api, cfg, path, query, "read job test results", "If you passed --cursor, restart without it. Otherwise check that Repository Tests are enabled and that the job belongs to the repository.")
		if err != nil {
			return nil, err
		}
		applied := map[string]any{}
		if value, exists := page["filters"]; exists {
			values, ok := value.(map[string]any)
			if !ok {
				return nil, &ExitError{1, "Error: The API did not confirm the requested test filters. It may need an update."}
			}
			for key, value := range values {
				if value != nil {
					applied[key] = value
				}
			}
		}
		if !reflect.DeepEqual(applied, filters) {
			return nil, &ExitError{1, "Error: The API did not confirm the requested test filters. It may need an update."}
		}
		page["filters"] = applied
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
	fmt.Fprintf(&b, "Job: %s\nOrganization: %s  Repository: %s\n", text("job_id"), text("organization_id"), text("repository_id"))
	if response["execution_id"] != nil {
		fmt.Fprintf(&b, "Execution: %s\n", text("execution_id"))
	}
	fmt.Fprintf(&b, "Commit: %s\nReporting: %s  Results: %s\n", text("commit_sha"), text("reporting_state"), text("status"))
	if testValuePresent(response["reason"]) {
		fmt.Fprintf(&b, "Reason: %s\n", text("reason"))
	}
	rows := response["data"].([]any)
	label := "shown"
	if filters, _ := response["filters"].(map[string]any); len(filters) > 0 {
		label = "matching shown"
	}
	fmt.Fprintf(&b, "Retained occurrences (all tests): %s; %s: %d\n", text("total_count"), label, len(rows))
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
		} else if label == "matching shown" {
			if response["pagination"].(map[string]any)["next_cursor"] != nil {
				b.WriteString("No matches in the searched portion.\n")
			} else {
				b.WriteString("No matching tests in the remaining retained results.\n")
			}
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

func resolveTestsJob(cmd *cobra.Command, d Dependencies, cfg config.Config, api *client.Client, target, org, name string, hasName bool) (string, string, error) {
	ctx := cmd.Context()
	if strings.HasPrefix(target, "job-") || strings.HasPrefix(target, "awj-") {
		if !testJobID().MatchString(target) {
			return "", "", usageError(cmd, "Invalid value for RUN_OR_JOB: use job- or awj- followed by 32 lowercase hex characters")
		}
		if hasName {
			return "", "", usageError(cmd, "--job selects a job within a run. Omit it when passing a job ID.")
		}
		organization, err := resolveOrg(ctx, d, cfg, api, org)
		return organization, target, err
	}
	if hasName && strings.TrimSpace(name) == "" {
		return "", "", usageError(cmd, "Invalid value for --job: use a nonempty job name")
	}
	ref, err := parseRunReference(target, cfg.Host)
	if err != nil {
		var exit *ExitError
		if errors.Is(err, errInvalidRunReference) || (errors.As(err, &exit) && exit.Code == 2) {
			err = usageError(cmd, "Invalid value for RUN_OR_JOB: Use an Avrea run or job ID, a positive GitHub run ID, a GitHub Actions run URL or an Avrea console run URL. For GitLab jobs, use their Avrea job ID (job- or awj-).")
		}
		return "", "", err
	}
	_, organization, err := resolveRunOrg(ctx, d, cfg, api, ref, org)
	if err != nil {
		return "", "", err
	}
	record, err := resolveRun(ctx, cfg, api, organization, ref, []string{"jobs"}, "resolve the run's jobs")
	if err != nil {
		return "", "", err
	}
	values, ok := record.Fields["jobs"].([]any)
	jobs := objectList(record.Fields["jobs"])
	if !ok || len(values) != len(jobs) {
		return "", "", &ExitError{1, "Error: The API returned invalid run jobs."}
	}
	if len(jobs) == 0 {
		return "", "", &ExitError{1, "Error: This run has no available jobs. Pass an Avrea job ID to inspect a known job."}
	}
	matched := []map[string]any{}
	for _, job := range jobs {
		if !hasName || strings.Contains(strings.ToLower(fieldText(job, "job_name")), strings.ToLower(name)) {
			matched = append(matched, job)
		}
	}
	if len(matched) != 1 {
		choices := []string{}
		for _, job := range jobs {
			choices = append(choices, "  "+testText(job["job_name"])+": "+testText(job["job_id"]))
		}
		prefix := "Several jobs"
		if len(matched) == 0 {
			prefix = "No jobs"
		}
		return "", "", &ExitError{1, "Error: " + prefix + " match. Use --job NAME to select one job or pass its Avrea job ID.\nAvailable jobs:\n" + strings.Join(choices, "\n")}
	}
	id, ok := matched[0]["job_id"].(string)
	if !ok || !testJobID().MatchString(id) {
		return "", "", &ExitError{1, "Error: The selected job has no available Avrea job ID."}
	}
	return organization, id, nil
}
