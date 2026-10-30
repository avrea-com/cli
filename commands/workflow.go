package commands

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var workflowFields = []string{"ci_provider", "failure_count", "flaked_count", "median_duration_seconds", "workflow_id", "platform_workflow_id", "name", "path", "repository", "runs", "completed_runs"}
var workflowViewFields = append(append([]string{}, workflowFields...), "p95_duration_seconds", "jobs")

// NewWorkflow returns shared workflow discovery and execution commands.
func NewWorkflow(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	group := &cobra.Command{Use: "workflow", Aliases: []string{"workflows"}, Short: "List and view workflow definitions."}
	group.AddCommand(newWorkflowList(d), newWorkflowView(d), newWorkflowRun(d))
	return group
}

func workflowProjection(bucket map[string]any) map[string]any {
	row := make(map[string]any, len(bucket)+8)
	for k, v := range bucket {
		row[k] = v
	}
	wf, _ := bucket["workflow"].(map[string]any)
	for target, source := range map[string]string{"ci_provider": "ci_provider", "workflow_id": "workflow_id", "platform_workflow_id": "platform_id", "name": "name", "path": "path", "repository": "repository_full_name"} {
		row[target] = wf[source]
	}
	row["runs"], row["completed_runs"] = bucket["count"], bucket["completed_count"]
	return row
}

func newWorkflowList(d Dependencies) *cobra.Command {
	var o jsonOptions
	var org, since string
	var repos []string
	var limit int
	c := &cobra.Command{Use: "list", Short: "List workflows with aggregate run stats.", Args: cobra.NoArgs}
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringArrayVar(&repos, "repo", nil, "Filter by repository name or ID (repeatable).")
	f.StringVar(&since, "since", "30d", "Time window: 30d, 7d, 24h, or all.")
	f.IntVarP(&limit, "limit", "L", 20, "Maximum workflows to show (1–1000).")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if done, err := o.prepare(cmd, d, workflowFields); done || err != nil {
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
		ids, err := resolveRepositories(cmd.Context(), d, cfg, api, id, repos, false)
		if err != nil {
			return err
		}
		query := url.Values{"time_bucket": {"total"}}
		if len(ids) > 0 {
			query["repository_ids"] = ids
		}
		if since != "all" {
			cutoff, e := sinceTimestamp(since, d.Now())
			if e != nil {
				return e
			}
			query.Set("created_after", cutoff)
		}
		page, err := api.WorkflowAggregates(cmd.Context(), id, query)
		if err != nil {
			return apiError(cfg, err, "fetch workflow aggregate")
		}
		sort.SliceStable(page.Items, func(i, j int) bool { return page.Items[i].Value.Count > page.Items[j].Value.Count })
		total := len(page.Items)
		if total > limit {
			page.Items = page.Items[:limit]
		}
		rows := rawRecords(page)
		if f.Changed("json") {
			projected := make([]map[string]any, len(rows))
			for i, row := range rows {
				projected[i] = workflowProjection(row)
			}
			return o.list(cmd, d, projected, workflowFields)
		}
		return renderWorkflowList(d, rows, total, since)
	})
	return c
}

func requiredWorkflowRepo(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, repo string) (string, error) {
	var repos []string
	if repo != "" {
		repos = []string{repo}
	}
	ids, err := resolveRepositories(ctx, d, cfg, api, org, repos, false)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", &ExitError{1, "Error: Could not determine repository. Run from a git directory, pass --repo <org/name>, or set AVR_REPO."}
	}
	return ids[0], nil
}

func lookupWorkflow(ctx context.Context, cfg config.Config, api *client.Client, org, repo, identifier string) (map[string]any, error) {
	var rows []map[string]any
	query := url.Values{}
	for range 50 {
		page, err := api.RepositoryWorkflows(ctx, org, repo, query)
		if err != nil {
			base := apiError(cfg, err, "list workflows for resolution")
			var httpErr *client.HTTPError
			if errors.As(err, &httpErr) && httpErr.Status == 404 {
				return nil, &ExitError{1, base.Error() + fmt.Sprintf("\nRepository '%s' is not in this org or you lack access. Check `avr repo list --org <org>`.", output.CellText(repo))}
			}
			return nil, base
		}
		rows = append(rows, rawRecords(page)...)
		if page.NextCursor == "" {
			break
		}
		query.Set("cursor", page.NextCursor)
	}
	safe := output.CellText(identifier)
	if strings.HasPrefix(identifier, "wfl-") || strings.HasPrefix(identifier, "awf-") {
		for _, wf := range rows {
			if fieldText(wf, "workflow_id") == identifier {
				return wf, nil
			}
		}
		return nil, &ExitError{1, fmt.Sprintf("Error: Workflow '%s' not found in this repository", safe)}
	}
	if decimalPattern.MatchString(identifier) {
		value := strings.TrimLeft(identifier, "0")
		if value == "" {
			value = "0"
		}
		for _, wf := range rows {
			if fieldText(wf, "platform_id") == value {
				return wf, nil
			}
		}
		return nil, &ExitError{1, fmt.Sprintf("Error: Workflow with platform ID '%s' not found in this repository", safe)}
	}
	var matches []map[string]any
	for _, wf := range rows {
		if strings.EqualFold(fieldText(wf, "path"), identifier) {
			matches = append(matches, wf)
		}
	}
	if len(matches) == 0 {
		for _, wf := range rows {
			if strings.EqualFold(fieldText(wf, "name"), identifier) {
				matches = append(matches, wf)
			}
		}
	}
	if len(matches) == 0 {
		for _, wf := range rows {
			base := strings.ToLower(path.Base(fieldText(wf, "path")))
			stem := strings.TrimSuffix(strings.TrimSuffix(base, ".yml"), ".yaml")
			if base == strings.ToLower(identifier) || stem == strings.ToLower(identifier) {
				matches = append(matches, wf)
			}
		}
	}
	if len(matches) == 0 {
		return nil, &ExitError{1, fmt.Sprintf("Error: Workflow '%s' not found. Run 'avr workflow list --repo <repo>' to see available workflows.", safe)}
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, wf := range matches {
			names[i] = output.CellText(fieldText(wf, "name"))
		}
		return nil, &ExitError{1, fmt.Sprintf("Error: Ambiguous workflow '%s'. Matches: %s. Pass the full workflow ID or file path instead.", safe, strings.Join(names, ", "))}
	}
	return matches[0], nil
}

func newWorkflowView(d Dependencies) *cobra.Command {
	var o jsonOptions
	var org, repo, since string
	var web bool
	c := &cobra.Command{Use: "view WORKFLOW", Short: "View workflow stats and per-job breakdown.", Args: cobra.ExactArgs(1)}
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringVar(&repo, "repo", "", "Repository name or ID; otherwise detect from Git.")
	f.StringVar(&since, "since", "30d", "Time window: 30d, 7d, 24h, or all.")
	f.BoolVar(&web, "web", false, "Open in browser.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if web && f.Changed("json") {
			return &ExitError{2, "Error: --web cannot be combined with --json."}
		}
		if done, err := o.prepare(cmd, d, workflowViewFields); done || err != nil {
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
		workflow := args[0]
		if !strings.HasPrefix(workflow, "wfl-") && !strings.HasPrefix(workflow, "awf-") {
			repository, err := requiredWorkflowRepo(cmd.Context(), d, cfg, api, id, repo)
			if err != nil {
				return err
			}
			wf, err := lookupWorkflow(cmd.Context(), cfg, api, id, repository, workflow)
			if err != nil {
				return err
			}
			workflow = fieldText(wf, "workflow_id")
		}
		if web {
			target := consoleURL(cfg.Host) + "/org/" + url.PathEscape(orgSlug(cmd.Context(), cfg, api, id)) + "/workflows/" + url.PathEscape(workflow)
			if d.IsTerminal() {
				if _, err := fmt.Fprintln(d.Out, "Opening: "+target); err != nil {
					return err
				}
				_ = d.OpenBrowser(cmd.Context(), target)
				return nil
			}
			_, err = fmt.Fprintln(d.Out, target)
			return err
		}
		query := url.Values{"time_bucket": {"total"}, "workflow_ids": {workflow}, "include": {"jobs"}}
		if since != "all" {
			cutoff, err := sinceTimestamp(since, d.Now())
			if err != nil {
				return err
			}
			query.Set("created_after", cutoff)
		}
		page, err := api.WorkflowAggregates(cmd.Context(), id, query)
		if err != nil {
			return apiError(cfg, err, "fetch workflow stats")
		}
		bucket := map[string]any{}
		if len(page.Items) > 0 {
			bucket = page.Items[0].Fields
		}
		if f.Changed("json") {
			fields, err := output.Fields(o.fields, workflowViewFields)
			if err != nil {
				return err
			}
			return output.JSON(cmd.Context(), d.Out, workflowProjection(bucket), fields, o.expression)
		}
		if len(page.Items) == 0 {
			_, err = fmt.Fprintf(d.Out, "No runs found for workflow %s in the selected window.\n", output.CellText(workflow))
			return err
		}
		slug, host := "", ""
		if linksEnabled(cmd, d) {
			slug = orgSlug(cmd.Context(), cfg, api, id)
			host = consoleURL(cfg.Host)
		}
		if err := renderWorkflowSummary(d, bucket, host, slug); err != nil {
			return err
		}
		recent, err := api.Runs(cmd.Context(), id, url.Values{"workflow_ids": {workflow}, "limit": {"5"}, "order": {"created_at.desc"}})
		if err != nil {
			var httpErr *client.HTTPError
			if !errors.As(err, &httpErr) {
				return err
			}
			_, _ = fmt.Fprintln(d.Err, d.hint(fmt.Sprintf("(could not load recent runs: HTTP %d)", httpErr.Status)))
		} else if err := renderWorkflowRecent(d, rawRecords(recent), bucket, host, slug); err != nil {
			return err
		}
		if _, err = fmt.Fprintln(d.Out, "\n"+d.paint("Stats for the last "+output.CellText(since)+".", "2")+"\n"); err != nil {
			return err
		}
		wf, _ := bucket["workflow"].(map[string]any)
		repo := fieldText(wf, "repository_full_name")
		if repo == "" {
			repo = "-"
		}
		_, err = fmt.Fprintf(d.Err, "To see more runs for this workflow, try: avr run list --repo %s\nTo adjust the time window, try: avr workflow view <wfl-id> --since 7d\n", output.CellText(repo))
		return err
	})
	return c
}

func aggregateNumber(row map[string]any, key string) float64 {
	value, _ := strconv.ParseFloat(fieldText(row, key), 64)
	return value
}
func aggregateCount(row map[string]any, key string) string {
	value := fieldText(row, key)
	if value == "" {
		return "0"
	}
	return value
}
