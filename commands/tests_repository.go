package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var testSearchFields = []string{"organization_id", "repository_id", "status", "scope", "truncated", "filters", "data", "pagination"}
var testHistoryFields = []string{"organization_id", "repository_id", "test_id", "configuration_digest", "identity", "status", "scope", "data", "pagination"}
var testDigest = lazyRegexp("^[0-9a-f]{64}$")

// The API serves at most this many rows per request on both endpoints.
const testPageLimit = 100

// The API ends a repository test search walk after this many rows.
const testSearchWalk = 1000

// testRepository resolves the organization and repository shared by the
// repository-wide test commands.
func testRepository(cmd *cobra.Command, d Dependencies, org, repo string) (config.Config, *client.Client, string, string, error) {
	cfg, _, api, err := d.load()
	if err != nil {
		return cfg, nil, "", "", err
	}
	if err = authenticated(cfg); err != nil {
		return cfg, nil, "", "", err
	}
	organization, err := resolveOrg(cmd.Context(), d, cfg, api, org)
	if err != nil {
		return cfg, nil, "", "", err
	}
	repository, err := requiredWorkflowRepo(cmd.Context(), d, cfg, api, organization, repo)
	return cfg, api, organization, repository, err
}

func testOutcomeQuery(cmd *cobra.Command, outcomes []string) (string, []string, error) {
	for _, outcome := range outcomes {
		if !slices.Contains(testOutcomes, outcome) {
			return "", nil, usageError(cmd, "Invalid value for '--status': "+pythonQuote(outcome)+" is not one of 'passed', 'failed', 'skipped', 'errored'.")
		}
	}
	selected := slices.Clone(outcomes)
	slices.Sort(selected)
	selected = slices.Compact(selected)
	switch len(selected) {
	case 0:
		return "", nil, nil
	case 1:
		return "eq." + selected[0], selected, nil
	default:
		return "in.(" + strings.Join(selected, ",") + ")", selected, nil
	}
}

// searchRepositoryTests follows short pages until limit rows are read. The
// first page's status and scope describe the listing.
func searchRepositoryTests(ctx context.Context, api *client.Client, cfg config.Config, path, text, outcome string, outcomes []string, limit int, cursor *string) (map[string]any, error) {
	invalid := &ExitError{1, "Error: The API returned an invalid repository test listing."}
	rows := make([]any, 0)
	var snapshot map[string]any
	truncated := false
	seen := map[string]bool{}
	if cursor != nil {
		seen[*cursor] = true
	}
	for range 20 {
		query := url.Values{"limit": {strconv.Itoa(min(limit-len(rows), testPageLimit))}}
		if text != "" {
			query.Set("q", text)
		}
		if outcome != "" {
			query.Set("outcome", outcome)
		}
		if cursor != nil {
			query.Set("cursor", *cursor)
		}
		page, err := getTestResults(ctx, api, cfg, path, query, "search repository tests", "Check the repository and organization, and that Repository Tests are enabled. If you passed --cursor, restart without it.")
		if err != nil {
			return nil, err
		}
		data, dataOK := page["data"].([]any)
		pagination, paginationOK := page["pagination"].(map[string]any)
		_, statusOK := page["status"].(string)
		if !dataOK || !paginationOK || !statusOK || len(data) > limit-len(rows) {
			return nil, invalid
		}
		for _, row := range data {
			if _, ok := row.(map[string]any); !ok {
				return nil, invalid
			}
		}
		filters, _ := page["filters"].(map[string]any)
		confirmed := make([]string, 0, len(outcomes))
		if values, ok := filters["outcomes"].([]any); ok {
			for _, value := range values {
				name, _ := value.(string)
				confirmed = append(confirmed, name)
			}
			slices.Sort(confirmed)
		}
		confirmedText, _ := filters["query"].(string)
		if confirmedText != text || !slices.Equal(confirmed, outcomes) {
			return nil, &ExitError{1, "Error: The API did not confirm the requested test filters. It may need an update."}
		}
		if value, _ := page["truncated"].(bool); value {
			truncated = true
		}
		if snapshot != nil {
			if err := laterTestPageError(page); err != nil {
				return nil, err
			}
		}
		if snapshot == nil {
			snapshot = page
		}
		rows = append(rows, data...)
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
		if cursor == nil || len(rows) == limit {
			break
		}
	}
	var next any
	if cursor != nil {
		next = *cursor
	}
	snapshot["data"], snapshot["pagination"], snapshot["truncated"] = rows, map[string]any{"next_cursor": next}, truncated
	return snapshot, nil
}

func repositoryTestMessage(state any) string {
	switch state {
	case "not_recorded":
		return "No test reports are recorded for this repository."
	case "expired":
		return "The recorded test reports have expired."
	default:
		return testListingMessage(state)
	}
}

func testCount(value any) int64 {
	number, _ := value.(json.Number)
	count, _ := number.Int64()
	return count
}

func testTime(value any) string {
	text, _ := value.(string)
	if text == "" {
		return "unknown"
	}
	return formatTimestamp(text)
}

// reportsNotRead names the reports a scope counted but could not read yet.
func reportsNotRead(scope map[string]any) string {
	var b strings.Builder
	if count := testCount(scope["reports_indexing"]); count > 0 {
		fmt.Fprintf(&b, "; still indexing: %d", count)
	}
	if count := testCount(scope["reports_unavailable"]); count > 0 {
		fmt.Fprintf(&b, "; unavailable: %d", count)
	}
	return b.String()
}

func showRepositoryTests(w io.Writer, response map[string]any) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Organization: %s  Repository: %s\n", testText(response["organization_id"]), testText(response["repository_id"]))
	if scope, ok := response["scope"].(map[string]any); ok {
		fmt.Fprintf(&b, "Reports searched: %s", testText(scope["reports_searched"]))
		if scope["oldest_recorded_at"] != nil && scope["newest_recorded_at"] != nil {
			fmt.Fprintf(&b, " (%s to %s)", testTime(scope["oldest_recorded_at"]), testTime(scope["newest_recorded_at"]))
		}
		b.WriteString(reportsNotRead(scope) + "\n")
		if complete, ok := scope["complete"].(bool); ok && !complete {
			b.WriteString("Not every recorded report was searched; absent tests may still be recorded.\n")
		}
	}
	rows := response["data"].([]any)
	if len(rows) == 0 {
		message := repositoryTestMessage(response["status"])
		if message == "" {
			message = "No matching tests."
			if response["pagination"].(map[string]any)["next_cursor"] != nil {
				message = "No matches in the searched portion."
			}
		}
		b.WriteString(message + "\n")
		_, err := io.WriteString(w, b.String())
		return err
	}
	if message := testListingMessage(response["status"]); message != "" {
		b.WriteString(message + "\n")
	}
	cells := make([][]string, 0, len(rows))
	for _, item := range rows {
		row := item.(map[string]any)
		cells = append(cells, []string{testText(row["class_name"]), testText(row["name"]), testText(row["outcome"]), testText(row["duration_ms"]), testTime(row["recorded_at"]), testText(row["job_id"])})
	}
	if err := output.Grid(&b, []string{"Class", "Test", "Outcome", "Time (ms)", "Recorded", "Job"}, cells); err != nil {
		return err
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func newTestsSearch(d Dependencies) *cobra.Command {
	var org, repo, cursor string
	var outcomes []string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "search [QUERY]", Short: "Search the tests recorded for a repository.", Args: cobra.MaximumNArgs(1)}
	orgFlag(c, &org)
	c.Flags().StringVarP(&repo, "repo", "R", "", "Repository (owner/name or rep-ID). Defaults to the current git repository.")
	c.Flags().IntVarP(&limit, "limit", "L", 50, "Maximum tests to return; short pages are followed, up to 20 result pages.")
	c.Flags().StringVar(&cursor, "cursor", "", "Continue from a previous response's next_cursor.")
	c.Flags().StringArrayVar(&outcomes, "status", nil, "Show each test's most recent result with one of these outcomes (repeatable). A test that has passed since still appears.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if limit < 1 || limit > 1000 {
			return usageError(cmd, "Invalid value for '--limit' / '-L': "+strconv.Itoa(limit)+" is not in the range 1<=x<=1000.")
		}
		outcome, selected, err := testOutcomeQuery(cmd, outcomes)
		if err != nil {
			return err
		}
		text := ""
		if len(args) == 1 {
			text = strings.TrimSpace(args[0])
			if text == "" || utf8.RuneCountInString(text) > 200 {
				return usageError(cmd, "Invalid value for QUERY: use between 1 and 200 characters")
			}
		}
		var start *string
		if cmd.Flags().Changed("cursor") {
			if !validTestCursor(cursor) {
				return usageError(cmd, "Invalid value: --cursor must be 1..1024 chars and not surrounded by whitespace")
			}
			start = &cursor
		}
		if done, err := o.prepare(cmd, d, testSearchFields); done || err != nil {
			return err
		}
		cfg, api, organization, repository, err := testRepository(cmd, d, org, repo)
		if err != nil {
			return err
		}
		response, err := searchRepositoryTests(cmd.Context(), api, cfg, "/orgs/"+organization+"/repos/"+repository+"/tests", text, outcome, selected, limit, start)
		if err != nil {
			return err
		}
		response["organization_id"], response["repository_id"] = organization, repository
		if cmd.Flags().Changed("json") {
			err = o.record(cmd, d, response, testSearchFields)
		} else {
			err = showRepositoryTests(d.Out, response)
		}
		if err != nil {
			return err
		}
		if next, ok := response["pagination"].(map[string]any)["next_cursor"].(string); ok {
			_, err = fmt.Fprintf(d.Err, "More results available. Continue with the same query and filters and --cursor %s\n", shellQuotePython(testText(next)))
		}
		if truncated, _ := response["truncated"].(bool); truncated && err == nil {
			_, err = fmt.Fprintln(d.Err, "Search stopped at 1,000 tests; more match. Narrow the query or --status.")
		}
		return err
	})
	return c
}

// selectHistoryTest finds the one recorded test a name refers to. An exact
// name wins over tests that only contain it, so the whole search walk is read
// first; a walk that stops early never picks a test. Substring fallback also
// requires complete report coverage so an omitted exact match cannot lose.
func selectHistoryTest(ctx context.Context, api *client.Client, cfg config.Config, path, name, scopeFlags string) (map[string]any, error) {
	browse := "Find tests with:\n  avr tests search" + scopeFlags
	find := "Pass a test ID and its configuration; find them with:\n  avr tests search " + shellQuotePython(name) + scopeFlags + " -L " + strconv.Itoa(testSearchWalk) + " --json data"
	beyond := "The search for " + pythonQuote(name) + " stopped before reading every test that contains it, so a test with exactly this name may be beyond what was searched. " + find
	page, err := searchRepositoryTests(ctx, api, cfg, path, name, "", nil, testSearchWalk, nil)
	var stopped *testListingStopped
	if errors.As(err, &stopped) {
		return nil, &ExitError{1, stopped.Message + "\n" + beyond}
	}
	if err != nil {
		return nil, err
	}
	var exact, contained []map[string]any
	seen := map[string]bool{}
	for _, item := range page["data"].([]any) {
		row := item.(map[string]any)
		id, configuration := fieldText(row, "test_id"), fieldText(row, "configuration_digest")
		if !testDigest().MatchString(id) || !testDigest().MatchString(configuration) {
			return nil, &ExitError{1, "Error: The API returned an invalid repository test listing."}
		}
		if seen[id+configuration] {
			continue
		}
		seen[id+configuration] = true
		if row["name"] == name {
			exact = append(exact, row)
		} else if strings.Contains(strings.ToLower(fieldText(row, "name")), strings.ToLower(name)) {
			contained = append(contained, row)
		}
	}
	choices := func(rows []map[string]any) string {
		lines := []string{}
		for _, row := range rows[:min(len(rows), 10)] {
			lines = append(lines, fmt.Sprintf("  %s.%s\n    avr tests history %s --configuration %s%s", testText(row["class_name"]), testText(row["name"]), row["test_id"], row["configuration_digest"], scopeFlags))
		}
		if len(rows) > 10 {
			lines = append(lines, "More tests match than are listed; use a more specific name.")
		}
		return strings.Join(lines, "\n")
	}
	if page["pagination"].(map[string]any)["next_cursor"] != nil || page["truncated"] == true {
		message := "Error: " + beyond
		if len(exact) > 0 {
			message += "\nTests with this exact name found so far:\n" + choices(exact)
		}
		return nil, &ExitError{1, message}
	}
	matches := exact
	if len(matches) == 0 {
		scope, _ := page["scope"].(map[string]any)
		if scope["complete"] == false || testCount(scope["reports_indexing"]) > 0 || testCount(scope["reports_unavailable"]) > 0 {
			message := "Not every recorded report was searched; a test with exactly this name may be in an unsearched report."
			if len(contained) > 0 {
				message += " " + find + "\nTests containing this name found so far:\n" + choices(contained)
			} else {
				reason := repositoryTestMessage(page["status"])
				if page["status"] == "not_recorded" || page["status"] == "expired" {
					reason = "No retained test reports were found in the searched portion."
				}
				if reason != "" {
					message = reason + "\n" + message
				}
				message += " " + browse
			}
			return nil, &ExitError{1, "Error: " + message}
		}
		matches = contained
	}
	switch len(matches) {
	case 0:
		message := repositoryTestMessage(page["status"])
		if message == "" {
			message = "No recorded test matches " + pythonQuote(name) + ". " + browse
		}
		return nil, &ExitError{1, "Error: " + message}
	case 1:
		return matches[0], nil
	default:
		return nil, &ExitError{1, "Error: Several recorded tests match " + pythonQuote(name) + ". Pass a test ID and its configuration:\n" + choices(matches)}
	}
}

func showTestHistory(w io.Writer, response map[string]any) error {
	var b strings.Builder
	if identity, ok := response["identity"].(map[string]any); ok {
		fmt.Fprintf(&b, "Test: %s.%s  Suite: %s\n", testText(identity["class_name"]), testText(identity["name"]), testText(identity["suite"]))
	}
	fmt.Fprintf(&b, "Test ID: %s\nConfiguration: %s\n", testText(response["test_id"]), testText(response["configuration_digest"]))
	fmt.Fprintf(&b, "Organization: %s  Repository: %s\n", testText(response["organization_id"]), testText(response["repository_id"]))
	fmt.Fprintf(&b, "History: %s\n", testText(response["status"]))
	if scope, ok := response["scope"].(map[string]any); ok {
		fmt.Fprintf(&b, "Reports searched: %s%s\n", testText(scope["reports_searched"]), reportsNotRead(scope))
		if testCount(scope["reports_indexing"]) > 0 {
			b.WriteString("History is still being updated; the newest reports may be missing.\n")
		}
	}
	if status := response["status"]; status == "limited" || status == "unavailable" {
		b.WriteString("This read stopped early; run the command again to retry. Missing runs do not mean the test never ran.\n")
	}
	rows := response["data"].([]any)
	if len(rows) == 0 {
		if response["status"] == "available" {
			b.WriteString("No runs found in the reports searched on this page.\n")
		}
		_, err := io.WriteString(w, b.String())
		return err
	}
	cells := make([][]string, 0, len(rows))
	for _, item := range rows {
		row := item.(map[string]any)
		// One report can hold several occurrences of a test; show the mean of
		// those that recorded a duration.
		duration := "none"
		if samples := testCount(row["duration_sample_count"]); samples > 0 {
			duration = strconv.FormatInt((testCount(row["duration_total_ms"])+samples/2)/samples, 10)
		}
		job := "unknown"
		if row["job_id"] != nil {
			job = testText(row["job_id"])
		}
		cells = append(cells, []string{testTime(row["captured_at"]), testText(row["passed_count"]), testText(row["failed_count"]), testText(row["errored_count"]), testText(row["skipped_count"]), duration, job})
	}
	if err := output.Grid(&b, []string{"Recorded", "Passed", "Failed", "Errored", "Skipped", "Time (ms)", "Job"}, cells); err != nil {
		return err
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func newTestsHistory(d Dependencies) *cobra.Command {
	var org, repo, cursor, configuration string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "history TEST", Short: "Read one test's recorded runs in a repository, newest first.", Args: positionalArguments("TEST")}
	orgFlag(c, &org)
	c.Flags().StringVarP(&repo, "repo", "R", "", "Repository (owner/name or rep-ID). Defaults to the current git repository.")
	c.Flags().StringVar(&configuration, "configuration", "", "Configuration digest of the test; required with a test ID.")
	c.Flags().IntVarP(&limit, "limit", "L", 20, "Maximum recorded runs to return.")
	c.Flags().StringVar(&cursor, "cursor", "", "Continue from a previous response's next_cursor.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if limit < 1 || limit > testPageLimit {
			return usageError(cmd, "Invalid value for '--limit' / '-L': "+strconv.Itoa(limit)+" is not in the range 1<=x<=100.")
		}
		target := strings.TrimSpace(args[0])
		byID := cmd.Flags().Changed("configuration")
		if byID && (!testDigest().MatchString(configuration) || !testDigest().MatchString(target)) {
			return usageError(cmd, "Invalid value: with --configuration, TEST and --configuration are each 64 lowercase hex characters")
		}
		if !byID && (target == "" || utf8.RuneCountInString(target) > 200) {
			return usageError(cmd, "Invalid value for TEST: use a test name of 1 to 200 characters, or a test ID with --configuration")
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		if cmd.Flags().Changed("cursor") {
			if !validTestCursor(cursor) {
				return usageError(cmd, "Invalid value: --cursor must be 1..1024 chars and not surrounded by whitespace")
			}
			query.Set("cursor", cursor)
		}
		if done, err := o.prepare(cmd, d, testHistoryFields); done || err != nil {
			return err
		}
		cfg, api, organization, repository, err := testRepository(cmd, d, org, repo)
		if err != nil {
			return err
		}
		path := "/orgs/" + organization + "/repos/" + repository + "/tests"
		scope := ""
		if cmd.Flags().Changed("repo") {
			scope += " --repo " + shellQuotePython(repo)
		}
		if cmd.Flags().Changed("org") {
			scope += " --org " + shellQuotePython(org)
		}
		var identity any
		if !byID {
			row, err := selectHistoryTest(cmd.Context(), api, cfg, path, target, scope)
			if err != nil {
				return err
			}
			target, configuration = row["test_id"].(string), row["configuration_digest"].(string)
			identity = map[string]any{"suite": row["suite"], "class_name": row["class_name"], "name": row["name"]}
		}
		query.Set("configuration", configuration)
		response, err := getTestResults(cmd.Context(), api, cfg, path+"/history/"+target, query, "read the test history", "Check the test ID, configuration, repository and organization, and that Repository Tests are enabled.")
		if err != nil {
			return err
		}
		data, dataOK := response["data"].([]any)
		pagination, paginationOK := response["pagination"].(map[string]any)
		if _, statusOK := response["status"].(string); !dataOK || !paginationOK || !statusOK || len(data) != len(objectList(response["data"])) {
			return &ExitError{1, "Error: The API returned an invalid test history."}
		}
		response["organization_id"], response["repository_id"] = organization, repository
		response["test_id"], response["configuration_digest"], response["identity"] = target, configuration, identity
		if cmd.Flags().Changed("json") {
			err = o.record(cmd, d, response, testHistoryFields)
		} else {
			err = showTestHistory(d.Out, response)
		}
		if err != nil {
			return err
		}
		if next, ok := pagination["next_cursor"].(string); ok {
			_, err = fmt.Fprintf(d.Err, "Older runs available. Continue with: avr tests history %s --configuration %s%s --cursor %s\n", target, configuration, scope, shellQuotePython(testText(next)))
		}
		return err
	})
	return c
}
