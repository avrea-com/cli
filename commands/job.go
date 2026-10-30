package commands

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var jobFields = []string{"job_id", "ci_provider", "run_id", "platform", "platform_job_id", "job_name", "state", "conclusion", "duration_seconds", "created_at", "started_at", "completed_at", "running_on_avrea", "repository_id", "platform_run_id", "labels", "repository"}
var jobViewFields = append(append([]string{}, jobFields...), "steps", "workflow_run")

// NewJob returns independently owned job inspection and connection commands.
func NewJob(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	cmd := &cobra.Command{Use: "job", Aliases: []string{"jobs"}, Short: "Inspect Avrea job VMs (SSH, logs)."}
	cmd.AddCommand(newJobList(d), newJobView(d), newJobLogs(d), newJobSSH(d), newJobWatch(d), newJobMetrics(d))
	return cmd
}

func projectJob(fields map[string]any) map[string]any {
	result := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		result[k] = v
	}
	result["labels"] = fields["job_labels"]
	result["repository"] = fields["repository_full_name"]
	return result
}

func sinceTimestamp(value string, now time.Time) (string, error) {
	parts := sincePattern.FindStringSubmatch(value)
	if parts == nil {
		return "", &ExitError{1, "Error: Invalid --since value: " + output.CellText(value) + " (use e.g. 30d, 7d, 24h, 30m)"}
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	unit := time.Minute
	switch parts[2] {
	case "d":
		unit = 24 * time.Hour
	case "h":
		unit = time.Hour
	}
	if err != nil || n > int64((1<<63-1)/unit) {
		return "", fmt.Errorf("--since value is too large")
	}
	return now.UTC().Add(-time.Duration(n) * unit).Format("2006-01-02T15:04:05.000000-07:00"), nil
}

func newJobList(d Dependencies) *cobra.Command {
	var o jsonOptions
	var org, since, cursor, order, provider string
	var repos, names, statuses, workflows []string
	var limit int
	var on bool
	c := &cobra.Command{Use: "list", Short: "List jobs for an organization.", Args: cobra.NoArgs}
	o.flags(c)
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringArrayVar(&repos, "repo", nil, "Repository name or ID (repeatable); otherwise detect from Git.")
	f.StringArrayVar(&names, "name", nil, "Job name (repeatable).")
	f.StringArrayVar(&statuses, "status", nil, "State or conclusion (repeatable).")
	f.StringArrayVarP(&workflows, "workflow", "w", nil, "Workflow ID (repeatable).")
	f.BoolVar(&on, "on-avrea", false, "Show jobs running on Avrea.")
	f.Var(inverseBool{&on}, "shadowing", "Show shadowing jobs.")
	f.Lookup("shadowing").NoOptDefVal = "true"
	f.Lookup("shadowing").DefValue = "false"
	f.StringVar(&provider, "ci-provider", "", "Filter by CI engine.")
	f.StringVar(&since, "since", "", "Relative time window, e.g. 7d, 24h or 30m.")
	f.StringVar(&cursor, "cursor", "", "Pagination cursor.")
	f.StringVar(&order, "order", "created_at.desc", "Sort order.")
	f.IntVarP(&limit, "limit", "L", 20, "Maximum jobs (1–1000).")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if err := validateCIProvider(provider); err != nil {
			return err
		}
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if order != "created_at.desc" && order != "created_at.asc" {
			return &ExitError{2, "Error: invalid --order"}
		}
		for _, s := range statuses {
			v := strings.ToLower(s)
			if !runStates[v] && !runConclusions[v] {
				return &ExitError{2, "Error: invalid --status: " + output.CellText(s)}
			}
		}
		if done, err := o.prepare(cmd, d, jobFields); done || err != nil {
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
		ids, err := resolveRepositories(cmd.Context(), d, cfg, api, id, repos, true)
		if err != nil {
			return err
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}, "order": {order}}
		for key, values := range map[string][]string{"repository_ids": ids, "job_names": names, "workflow_ids": workflows} {
			for _, v := range values {
				query.Add(key, v)
			}
		}
		for _, s := range statuses {
			v := strings.ToLower(s)
			key := "conclusions"
			if runStates[v] {
				key = "states"
			}
			query.Add(key, v)
		}
		if f.Changed("on-avrea") || f.Changed("shadowing") {
			query.Set("running_on_avrea", strconv.FormatBool(on))
		}
		if since != "" {
			v, e := sinceTimestamp(since, d.Now())
			if e != nil {
				return e
			}
			query.Set("created_after", v)
		}
		if f.Changed("cursor") {
			if cursor == "" || strings.TrimSpace(cursor) != cursor || len(cursor) > 512 {
				return &ExitError{2, "Error: --cursor must be 1..512 chars and not surrounded by whitespace"}
			}
			query.Set("cursor", cursor)
		}
		if provider != "" {
			query.Set("ci_provider", provider)
		}
		page, err := api.Jobs(cmd.Context(), id, query)
		if err != nil {
			return apiError(cfg, err, "list jobs")
		}
		if f.Changed("json") {
			records := []map[string]any{}
			for _, r := range page.Items {
				records = append(records, projectJob(r.Fields))
			}
			return o.list(cmd, d, records, jobFields)
		}
		if len(page.Items) == 0 {
			_, err = fmt.Fprintln(d.Out, "No jobs found.")
			return err
		}
		slug := ""
		if linksEnabled(cmd, d) {
			slug = orgSlug(cmd.Context(), cfg, api, id)
		}
		if err = renderJobList(d, rawRecords(page), cfg.Host, slug); err != nil {
			return err
		}
		if page.NextCursor != "" {
			_, err = fmt.Fprintf(d.Err, "\nMore results available. Next page: --cursor %s\n", output.CellText(page.NextCursor))
		}
		return err
	})
	return c
}

func renderJobList(d Dependencies, jobs []map[string]any, host, slug string) error {
	if !d.IsTerminal() {
		rows := [][]string{}
		for _, j := range jobs {
			state := fieldText(j, "state")
			if state == "completed" && fieldText(j, "conclusion") != "" {
				state = fieldText(j, "conclusion")
			}
			on := "shadow"
			if j["running_on_avrea"] == true {
				on = "yes"
			}
			rows = append(rows, []string{state, fieldText(j, "job_name"), fieldText(j, "repository_full_name"), on, fieldText(j, "job_id"), fieldText(j, "duration_seconds"), fieldText(j, "created_at"), jobCIProvider(j)})
		}
		return output.Table(d.Out, []string{"status", "job_name", "repository", "on_avrea", "job_id", "duration_seconds", "created_at", "ci_provider"}, rows, true)
	}
	widths := []int{38, 28, 15, 7, 10, 36}
	headers := []string{"NAME", "REPOSITORY", "STATUS", "ON", "AGE", "ID"}
	cells := make([]string, 6)
	for i, h := range headers {
		cells[i] = d.paint(padded(h, widths[i], false), gray+";4")
	}
	if _, err := fmt.Fprintln(d.Out, "  "+strings.Join(cells, " ")+" CI"); err != nil {
		return err
	}
	for _, j := range jobs {
		state := fieldText(j, "state")
		color := ""
		if state == "completed" {
			if v := fieldText(j, "conclusion"); v != "" {
				state = v
			}
			color = conclusionColor(fieldText(j, "conclusion"))
		}
		on, onColor := "shadow", "2"
		if j["running_on_avrea"] == true {
			on = "yes"
			onColor = ""
		}
		target := jobTarget(host, slug, fieldText(j, "job_id"))
		cells = []string{
			linkCell(d.paint(padded(shortCell(fieldText(j, "job_name"), 36), 38, false), "1"), target),
			d.paint(padded(shortCell(fieldText(j, "repository_full_name"), 26), 28, false), "2"),
			d.paint(padded(shortCell(state, 14), 15, false), color),
			d.paint(padded(on, 7, false), onColor),
			d.paint(padded(relativeAge(fieldText(j, "created_at"), d.Now()), 10, false), "2"),
			linkCell(d.paint(output.CellText(fieldText(j, "job_id")), "36"), target), output.CellText(jobCIProvider(j)),
		}
		if _, err := fmt.Fprintln(d.Out, d.indicator(j, "state")+" "+strings.Join(cells, " ")); err != nil {
			return err
		}
	}
	return nil
}

func jobTarget(host, slug, id string) string {
	if slug == "" || id == "" {
		return ""
	}
	return consoleURL(host) + "/org/" + url.PathEscape(slug) + "/jobs/" + url.PathEscape(id)
}
