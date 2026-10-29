package commands

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var runFields = []string{"run_id", "platform_run_id", "display_title", "status", "conclusion", "head_branch", "head_sha", "event", "run_number", "run_attempt", "duration_seconds", "created_at", "updated_at", "workflow_id", "workflow", "repository", "triggering_actor"}
var runStates = map[string]bool{"queued": true, "in_progress": true, "completed": true}
var runConclusions = map[string]bool{"success": true, "failure": true, "neutral": true, "cancelled": true, "skipped": true, "timed_out": true, "action_required": true, "stale": true, "startup_failure": true}
var sincePattern = regexp.MustCompile("^([0-9]+)([dhm])$")

// NewRun returns shared CI-run inspection commands.
func NewRun(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	group := &cobra.Command{Use: "run", Short: "View and manage workflow runs."}
	group.AddCommand(newRunList(d), newRunView(d), newRunWatch(d), newRunLogs(d), newRunRerun(d), newRunCancel(d))
	return group
}

func normalizeTimestamp(value string) (string, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		parsed, err := time.Parse(layout, value)
		if err != nil {
			continue
		}
		if layout == time.RFC3339Nano {
			return parsed.Format("2006-01-02T15:04:05.999999-07:00"), nil
		}
		return parsed.Format("2006-01-02T15:04:05.999999"), nil
	}
	return "", fmt.Errorf("invalid timestamp")
}

func newRunList(d Dependencies) *cobra.Command {
	var options jsonOptions
	var org, since, after, before, cursor, order string
	var repos, statuses, branches, workflows []string
	var limit int
	var web bool
	cmd := &cobra.Command{Use: "list", Short: "List workflow runs for an organization.", Args: cobra.NoArgs}
	options.flags(cmd)
	f := cmd.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringArrayVar(&repos, "repo", nil, "Filter by repository name or ID (repeatable); otherwise detect from Git.")
	f.StringArrayVar(&statuses, "status", nil, "Filter by run state or conclusion (repeatable).")
	f.StringArrayVar(&branches, "branch", nil, "Filter by head branch (repeatable).")
	f.StringArrayVarP(&workflows, "workflow", "w", nil, "Filter by workflow ID (repeatable).")
	f.StringVar(&since, "since", "", "Relative time window, e.g. 7d, 24h or 30m.")
	f.StringVar(&after, "created-after", "", "Only runs created after this ISO timestamp.")
	f.StringVar(&after, "from", "", "Alias for --created-after.")
	f.StringVar(&before, "created-before", "", "Only runs created before this ISO timestamp.")
	f.StringVar(&before, "to", "", "Alias for --created-before.")
	f.IntVarP(&limit, "limit", "L", 20, "Maximum runs to return (1–1000).")
	f.StringVar(&cursor, "cursor", "", "Pagination cursor from a previous response.")
	f.StringVar(&order, "order", "created_at.desc", "Sort by created_at.desc or created_at.asc.")
	f.BoolVar(&web, "web", false, "Open in browser.")
	cmd.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if order != "created_at.desc" && order != "created_at.asc" {
			return &ExitError{2, "Error: --order must be created_at.desc or created_at.asc."}
		}
		for _, s := range statuses {
			v := strings.ToLower(s)
			if !runStates[v] && !runConclusions[v] {
				return &ExitError{2, "Error: invalid --status: " + output.CellText(s)}
			}
		}
		if web && cmd.Flags().Changed("json") {
			return &ExitError{2, "Error: --web cannot be combined with --json."}
		}
		if done, err := options.prepare(cmd, d, runFields); done || err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		id, err := resolveOrg(cmd.Context(), d, cfg, api, org)
		if err != nil {
			return err
		}
		if web {
			target := consoleURL(cfg.Host) + "/org/" + url.PathEscape(orgSlug(cmd.Context(), cfg, api, id)) + "?view=workflows"
			_, err = fmt.Fprintln(d.Out, target)
			if err != nil {
				return err
			}
			if d.IsTerminal() {
				return d.OpenBrowser(cmd.Context(), target)
			}
			return nil
		}
		resolved, err := resolveRepositories(cmd.Context(), d, cfg, api, id, repos, true)
		if err != nil {
			return err
		}
		if since != "" && (after != "" || before != "") {
			return &ExitError{2, "Error: --since cannot be combined with --created-after or --created-before"}
		}
		if since != "" {
			after, err = sinceTimestamp(since, d.Now())
			if err != nil {
				return err
			}
		}

		query := url.Values{"limit": {strconv.Itoa(limit)}, "order": {order}, "include": {"workflow"}}
		for _, pair := range [][2]string{{"created_after", after}, {"created_before", before}} {
			if pair[1] != "" {
				normalized, err := normalizeTimestamp(pair[1])
				if err != nil {
					return fmt.Errorf("--%s must be a valid ISO-8601 timestamp", strings.ReplaceAll(pair[0], "_", "-"))
				}
				query.Set(pair[0], normalized)
			}
		}
		if cmd.Flags().Changed("cursor") {
			if cursor == "" || strings.TrimSpace(cursor) != cursor || len(cursor) > 512 {
				return &ExitError{2, "Error: --cursor must be 1..512 chars and not surrounded by whitespace"}
			}
			query.Set("cursor", cursor)
		}
		for _, s := range statuses {
			v := strings.ToLower(s)
			key := "conclusions"
			if runStates[v] {
				key = "statuses"
			}
			query.Add(key, v)
		}
		for key, values := range map[string][]string{"repository_ids": resolved, "head_branches": branches, "workflow_ids": workflows} {
			for _, v := range values {
				query.Add(key, v)
			}
		}
		page, err := api.Runs(cmd.Context(), id, query)
		if err != nil {
			return apiError(cfg, err, "list workflow runs")
		}
		if cmd.Flags().Changed("json") {
			return options.list(cmd, d, rawRecords(page), runFields)
		}
		if len(page.Items) == 0 {
			_, err = fmt.Fprintln(d.Out, "No workflow runs found.")
			if since != "" || len(resolved) > 0 || len(workflows) > 0 {
				_, _ = fmt.Fprintln(d.Err, "  Try: --since 90d, drop --repo/--workflow, or check \x60avr run list --json '*' --jq '. | length'\x60.")
			}
			return err
		}
		rows := [][]string{}
		for _, record := range page.Items {
			r := record.Fields
			workflow, _ := r["workflow"].(map[string]any)
			state := fieldText(r, "conclusion")
			if state == "" {
				state = fieldText(r, "status")
			}
			rows = append(rows, []string{state, fieldText(r, "display_title"), fieldText(workflow, "name"), fieldText(r, "head_branch"), fieldText(r, "event"), fieldText(r, "run_id"), fieldText(r, "duration_seconds"), fieldText(r, "created_at")})
		}
		if !d.IsTerminal() {
			return output.Table(d.Out, []string{"status", "title", "workflow", "branch", "event", "run_id", "duration_seconds", "created_at"}, rows, true)
		}
		for i, row := range rows {

			row[6] = formatDuration(page.Items[i].Fields["duration_seconds"])
			row[7] = relativeAge(fieldText(page.Items[i].Fields, "created_at"), d.Now())
		}
		var links [][]string
		if linksEnabled(cmd, d) {
			slug := orgSlug(cmd.Context(), cfg, api, id)
			for i, row := range rows {
				target := consoleURL(cfg.Host) + "/org/" + url.PathEscape(slug) + "/runs/" + url.PathEscape(row[5])
				repo, _ := page.Items[i].Fields["repository"].(map[string]any)
				branchTarget := ""
				if name, branch := fieldText(repo, "full_name"), fieldText(page.Items[i].Fields, "head_branch"); strings.Contains(name, "/") && branch != "" {
					branchTarget = "https://github.com/" + name + "/tree/" + url.PathEscape(branch)
				}
				links = append(links, []string{"", target, target, branchTarget, "", target, "", ""})
			}
		}
		if err = d.runTable(rows, links, rawRecords(page)); err != nil {
			return err
		}
		if page.NextCursor != "" {
			_, err = fmt.Fprintf(d.Err, "\nMore results available. Next page: --cursor %s\n", output.CellText(page.NextCursor))
		}
		return err
	})
	return cmd
}
