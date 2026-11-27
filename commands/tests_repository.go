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
var testFrameworks = []string{"pytest", "vitest", "jest", "go", "junit", "playwright", "bun", "rust"}

// The API accepts a minimum result duration of 1 to this many milliseconds.
const testMaxDuration = 99999999

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

// testChoices validates a repeatable choice flag and returns its values
// sorted and without repeats, the order the API confirms them in.
func testChoices(cmd *cobra.Command, flag string, values, choices []string) ([]string, error) {
	for _, value := range values {
		if !slices.Contains(choices, value) {
			return nil, usageError(cmd, "Invalid value for '--"+flag+"': "+pythonQuote(value)+" is not one of '"+strings.Join(choices, "', '")+"'.")
		}
	}
	selected := slices.Clone(values)
	slices.Sort(selected)
	return slices.Compact(selected), nil
}

// selectionParam is the API's operator for one or more selected values.
func selectionParam(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return "eq." + values[0]
	default:
		return "in.(" + strings.Join(values, ",") + ")"
	}
}

// testSelection holds the filters of a repository-wide read. They are sent on
// every page, and every page must confirm them.
type testSelection struct {
	text                 string
	outcomes, frameworks []string
	minDuration          *int64
}

func (s testSelection) apply(query url.Values) {
	if s.text != "" {
		query.Set("q", s.text)
	}
	if value := selectionParam(s.outcomes); value != "" {
		query.Set("outcome", value)
	}
	if value := selectionParam(s.frameworks); value != "" {
		query.Set("framework", value)
	}
	if s.minDuration != nil {
		query.Set("duration_ms", "gte."+strconv.FormatInt(*s.minDuration, 10))
	}
}

func (s testSelection) confirmedBy(filters map[string]any) bool {
	sorted := func(value any) []string {
		names := []string{}
		for _, item := range anyList(value) {
			name, _ := item.(string)
			names = append(names, name)
		}
		slices.Sort(names)
		return names
	}
	text, _ := filters["query"].(string)
	minimum, hasMinimum := filters["min_duration_ms"].(json.Number)
	durationConfirmed := filters["min_duration_ms"] == nil && s.minDuration == nil
	if hasMinimum && s.minDuration != nil {
		value, err := minimum.Int64()
		durationConfirmed = err == nil && value == *s.minDuration
	}
	return text == s.text && slices.Equal(sorted(filters["outcomes"]), s.outcomes) && slices.Equal(sorted(filters["frameworks"]), s.frameworks) && durationConfirmed
}

// testDurationMilliseconds reads a test duration: whole milliseconds, seconds
// or minutes (500ms, 2s, 1m), or a bare number of milliseconds, the unit of
// the Time (ms) columns. It is false outside the range the API accepts.
func testDurationMilliseconds(value string) (int64, bool) {
	text := strings.ToLower(strings.TrimSpace(value))
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(text, "ms"):
		text = strings.TrimSuffix(text, "ms")
	case strings.HasSuffix(text, "s"):
		text, multiplier = strings.TrimSuffix(text, "s"), 1000
	case strings.HasSuffix(text, "m"):
		text, multiplier = strings.TrimSuffix(text, "m"), 60000
	}
	// Nine digits cannot overflow and already exceed the range in every unit.
	if text == "" || len(text) > 9 || strings.Trim(text, "0123456789") != "" {
		return 0, false
	}
	number, _ := strconv.ParseInt(text, 10, 64)
	milliseconds := number * multiplier
	return milliseconds, milliseconds >= 1 && milliseconds <= testMaxDuration
}

// testScopeFlags names the organization and repository a response was read
// from, for a follow-up command.
func testScopeFlags(response map[string]any) string {
	return " --org " + testText(response["organization_id"]) + " --repo " + testText(response["repository_id"])
}

// writeResultCommands prints, for each failed or errored row, the command
// that reads that result and when it started failing. A result ID is too
// long for a table column.
func writeResultCommands(b *strings.Builder, response map[string]any, rows []any, label func(map[string]any) string) {
	scope, first := testScopeFlags(response), true
	for _, item := range rows {
		row := item.(map[string]any)
		id, _ := row["result_id"].(string)
		if outcome := row["outcome"]; id == "" || (outcome != "failed" && outcome != "errored") {
			continue
		}
		if first {
			b.WriteString("\nRead a failed result and when it started failing:\n")
			first = false
		}
		fmt.Fprintf(b, "  %s\n    avr tests view %s%s\n", label(row), shellQuotePython(id), scope)
	}
}

// testSelectionFlags are the filters search and rank share.
type testSelectionFlags struct {
	outcomes, frameworks []string
	minDuration          string
}

func (f *testSelectionFlags) add(c *cobra.Command, statusHelp string) {
	c.Flags().StringArrayVar(&f.outcomes, "status", nil, statusHelp)
	c.Flags().StringArrayVar(&f.frameworks, "framework", nil, "Test framework (repeatable; matches any selected framework).")
	c.Flags().StringVar(&f.minDuration, "min-duration", "", "Only tests with a result at least this long: 500ms, 2s, 1m or milliseconds.")
}

func (f testSelectionFlags) selection(cmd *cobra.Command, args []string) (testSelection, error) {
	var s testSelection
	var err error
	if s.outcomes, err = testChoices(cmd, "status", f.outcomes, testOutcomes); err != nil {
		return s, err
	}
	if s.frameworks, err = testChoices(cmd, "framework", f.frameworks, testFrameworks); err != nil {
		return s, err
	}
	if cmd.Flags().Changed("min-duration") {
		milliseconds, ok := testDurationMilliseconds(f.minDuration)
		if !ok {
			return s, usageError(cmd, "Invalid value for '--min-duration': "+pythonQuote(output.CellText(f.minDuration))+" is not a duration such as 500ms, 2s or 1m, or a number of milliseconds, from 1ms to "+strconv.Itoa(testMaxDuration)+"ms.")
		}
		s.minDuration = &milliseconds
	}
	if len(args) == 1 {
		s.text = strings.TrimSpace(args[0])
		if s.text == "" || utf8.RuneCountInString(s.text) > 200 {
			return s, usageError(cmd, "Invalid value for QUERY: use between 1 and 200 characters")
		}
	}
	return s, nil
}

// searchRepositoryTests reads the latest result of each matching test.
func searchRepositoryTests(ctx context.Context, api *client.Client, cfg config.Config, path string, selection testSelection, limit int, cursor *string) (map[string]any, error) {
	query := url.Values{}
	selection.apply(query)
	confirmed := func(page map[string]any) bool {
		filters, _ := page["filters"].(map[string]any)
		return selection.confirmedBy(filters)
	}
	return walkRepositoryTests(ctx, api, cfg, path, "search repository tests", query, confirmed, limit, cursor)
}

// walkRepositoryTests follows short pages until limit rows are read. The
// first page's status and scope describe the listing; confirmed checks that
// each page applied the requested filters.
func walkRepositoryTests(ctx context.Context, api *client.Client, cfg config.Config, path, action string, filters url.Values, confirmed func(map[string]any) bool, limit int, cursor *string) (map[string]any, error) {
	invalid := &ExitError{1, "Error: The API returned an invalid repository test listing."}
	rows := make([]any, 0)
	var snapshot map[string]any
	truncated := false
	seen := map[string]bool{}
	if cursor != nil {
		seen[*cursor] = true
	}
	for range 20 {
		query := url.Values{}
		for key, values := range filters {
			query[key] = values
		}
		query.Set("limit", strconv.Itoa(min(limit-len(rows), testPageLimit)))
		if cursor != nil {
			query.Set("cursor", *cursor)
		}
		page, err := getTestResults(ctx, api, cfg, path, query, action, "Check the repository and organization, and that Repository Tests are enabled. If you passed --cursor, restart without it.", false)
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
		if !confirmed(page) {
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

// repositoryScopeText describes the reports a repository-wide read searched.
// incomplete is said when older reports were left out.
func repositoryScopeText(b *strings.Builder, response map[string]any, incomplete string) {
	fmt.Fprintf(b, "Organization: %s  Repository: %s\n", testText(response["organization_id"]), testText(response["repository_id"]))
	if scope, ok := response["scope"].(map[string]any); ok {
		fmt.Fprintf(b, "Reports searched: %s", testText(scope["reports_searched"]))
		if scope["oldest_recorded_at"] != nil && scope["newest_recorded_at"] != nil {
			fmt.Fprintf(b, " (%s to %s)", testTime(scope["oldest_recorded_at"]), testTime(scope["newest_recorded_at"]))
		}
		b.WriteString(reportsNotRead(scope) + "\n")
		if complete, ok := scope["complete"].(bool); ok && !complete {
			b.WriteString(incomplete + "\n")
		}
	}
}

// repositoryRowsText states why a page has no rows, or names a listing
// state that leaves its rows incomplete.
func repositoryRowsText(b *strings.Builder, response map[string]any) {
	if len(response["data"].([]any)) > 0 {
		if message := testListingMessage(response["status"]); message != "" {
			b.WriteString(message + "\n")
		}
		return
	}
	message := repositoryTestMessage(response["status"])
	if message == "" {
		message = "No matching tests."
		if response["pagination"].(map[string]any)["next_cursor"] != nil {
			message = "No matches in the searched portion."
		}
	}
	b.WriteString(message + "\n")
}

func showRepositoryTests(w io.Writer, response map[string]any) error {
	var b strings.Builder
	repositoryScopeText(&b, response, "Not every recorded report was searched; absent tests may still be recorded.")
	repositoryRowsText(&b, response)
	rows := response["data"].([]any)
	if len(rows) > 0 {
		cells := make([][]string, 0, len(rows))
		for _, item := range rows {
			row := item.(map[string]any)
			cells = append(cells, []string{testText(row["class_name"]), testText(row["name"]), testText(row["outcome"]), testText(row["duration_ms"]), testTime(row["recorded_at"]), testText(row["job_id"])})
		}
		if err := output.Grid(&b, []string{"Class", "Test", "Outcome", "Time (ms)", "Recorded", "Job"}, cells); err != nil {
			return err
		}
		writeResultCommands(&b, response, rows, testLabel)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// repositoryTestRead is one repository-wide test command. Search and rank
// share its flags, validation, paging, output and continuation.
type repositoryTestRead struct {
	use, short, statusHelp, stopped string
	// same names what a continuation must keep unchanged.
	same   string
	fields []string
	// flags adds the command's own flags, and validate checks them before
	// any request.
	flags    func(*cobra.Command)
	validate func(*cobra.Command) error
	read     func(ctx context.Context, api *client.Client, cfg config.Config, repoPath string, selection testSelection, limit int, cursor *string) (map[string]any, error)
	show     func(io.Writer, map[string]any) error
}

func newRepositoryTestsCommand(d Dependencies, spec repositoryTestRead) *cobra.Command {
	var org, repo, cursor string
	var limit int
	var filters testSelectionFlags
	var o jsonOptions
	c := &cobra.Command{Use: spec.use, Short: spec.short, Args: cobra.MaximumNArgs(1)}
	orgFlag(c, &org)
	c.Flags().StringVarP(&repo, "repo", "R", "", "Repository (owner/name or rep-ID). Defaults to the current git repository.")
	c.Flags().IntVarP(&limit, "limit", "L", 50, "Maximum tests to return; short pages are followed, up to 20 result pages.")
	c.Flags().StringVar(&cursor, "cursor", "", "Continue from a previous response's next_cursor.")
	filters.add(c, spec.statusHelp)
	if spec.flags != nil {
		spec.flags(c)
	}
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if limit < 1 || limit > testSearchWalk {
			return usageError(cmd, "Invalid value for '--limit' / '-L': "+strconv.Itoa(limit)+" is not in the range 1<=x<=1000.")
		}
		selection, err := filters.selection(cmd, args)
		if err != nil {
			return err
		}
		if spec.validate != nil {
			if err := spec.validate(cmd); err != nil {
				return err
			}
		}
		var start *string
		if cmd.Flags().Changed("cursor") {
			if !validTestCursor(cursor) {
				return usageError(cmd, "Invalid value: --cursor must be 1..1024 chars and not surrounded by whitespace")
			}
			start = &cursor
		}
		if done, err := o.prepare(cmd, d, spec.fields); done || err != nil {
			return err
		}
		cfg, api, organization, repository, err := testRepository(cmd, d, org, repo)
		if err != nil {
			return err
		}
		response, err := spec.read(cmd.Context(), api, cfg, "/orgs/"+organization+"/repos/"+repository, selection, limit, start)
		if err != nil {
			return err
		}
		response["organization_id"], response["repository_id"] = organization, repository
		if cmd.Flags().Changed("json") {
			err = o.record(cmd, d, response, spec.fields)
		} else {
			err = spec.show(d.Out, response)
		}
		if err != nil {
			return err
		}
		err = writeTestContinuation(d.Err, response, false, "", spec.same)
		if truncated, _ := response["truncated"].(bool); truncated && err == nil {
			_, err = fmt.Fprintln(d.Err, spec.stopped)
		}
		return err
	})
	return c
}

func newTestsSearch(d Dependencies) *cobra.Command {
	return newRepositoryTestsCommand(d, repositoryTestRead{
		use:        "search [QUERY]",
		short:      "Search the tests recorded for a repository.",
		statusHelp: "Show each test's most recent result with one of these outcomes (repeatable). A test that has passed since still appears.",
		stopped:    "Search stopped at 1,000 tests; more match. Narrow the query or filters.",
		same:       "the same query and filters",
		fields:     testSearchFields,
		read: func(ctx context.Context, api *client.Client, cfg config.Config, repoPath string, selection testSelection, limit int, cursor *string) (map[string]any, error) {
			return searchRepositoryTests(ctx, api, cfg, repoPath+"/tests", selection, limit, cursor)
		},
		show: showRepositoryTests,
	})
}

// selectHistoryTest finds the one recorded test a name refers to. An exact
// name wins over tests that only contain it, so the whole search walk is read
// first; a walk that stops early never picks a test. Substring fallback also
// requires complete report coverage so an omitted exact match cannot lose.
func selectHistoryTest(ctx context.Context, api *client.Client, cfg config.Config, path, name, scopeFlags string) (map[string]any, error) {
	browse := "Find tests with:\n  avr tests search" + scopeFlags
	find := "Pass a test ID and its configuration; find them with:\n  avr tests search " + shellQuotePython(name) + scopeFlags + " -L " + strconv.Itoa(testSearchWalk) + " --json data"
	beyond := "The search for " + pythonQuote(name) + " stopped before reading every test that contains it, so a test with exactly this name may be beyond what was searched. " + find
	page, err := searchRepositoryTests(ctx, api, cfg, path, testSelection{text: name}, testSearchWalk, nil)
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
		response, err := getTestResults(cmd.Context(), api, cfg, path+"/history/"+target, query, "read the test history", "Check the test ID, configuration, repository and organization, and that Repository Tests are enabled.", false)
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
