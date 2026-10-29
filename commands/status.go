package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/spf13/cobra"
)

// NewStatus returns the shared organization/repository status dashboard.
func NewStatus(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	var org, repo, since string
	var raw bool
	c := &cobra.Command{Use: "status", Short: "Show recent runs, performance stats, and cache health.", Args: cobra.NoArgs}
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringVar(&repo, "repo", "", "Repository name or ID; otherwise detect from Git.")
	f.StringVar(&since, "since", "7d", "Time window for stats panels, e.g. 7d or 24h.")
	f.BoolVar(&raw, "json", false, "Output raw JSON.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
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
		slug := orgSlug(cmd.Context(), cfg, api, id)
		var repos []string
		if repo != "" {
			repos = []string{repo}
		}
		ids, err := resolveRepositories(cmd.Context(), d, cfg, api, id, repos, true)
		if err != nil {
			return err
		}
		repoID := ""
		if len(ids) > 0 {
			repoID = ids[0]
		}
		cutoff, err := sinceTimestamp(since, d.Now())
		if err != nil {
			return err
		}
		current, _ := time.Parse(time.RFC3339Nano, cutoff)
		previous := current.Add(-d.Now().Sub(current))
		query := url.Values{"limit": {"5"}, "order": {"created_at.desc"}, "include": {"workflow"}}
		if repoID != "" {
			query["repository_ids"] = []string{repoID}
		}
		runs, err := api.Runs(cmd.Context(), id, query)
		if err != nil {
			return apiError(cfg, err, "list workflow runs")
		}
		panels := make([][]map[string]any, 0, 4)
		for _, jobs := range []bool{false, true} {
			for _, before := range []bool{false, true} {
				start, end := current, (*time.Time)(nil)
				if before {
					start, end = previous, &current
				}
				rows, err := statusAggregate(cmd.Context(), d, api, id, repoID, jobs, start, end)
				if err != nil {
					return err
				}
				panels = append(panels, rows)
			}
		}
		var cache, repoInfo map[string]any
		if repoID != "" {
			record, err := api.CacheUsage(cmd.Context(), id, repoID)
			if err != nil {
				if !statusWarning(d, err, "cache") {
					return err
				}
			} else {
				cache = record.Fields
			}
			metadata, err := api.Repository(cmd.Context(), id, repoID)
			if err != nil {
				if !statusWarning(d, err, "repo metadata") {
					return err
				}
			} else {
				repoInfo = metadata.Fields
			}
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		report := map[string]any{
			"org": slug, "since": since, "runs": rawRecords(runs), "slowest_workflows": panels[0], "slowest_jobs": panels[2],
			"workflows_slowing_down": statusDeltas(panels[0], panels[1], true),
			"jobs_slowing_down":      statusDeltas(panels[2], panels[3], false), "cache": cache,
		}
		if raw {
			b, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(d.Out, string(b))
			return err
		}
		return renderStatus(d, report, repoInfo, repoID, cfg.Host, linksEnabled(cmd, d))
	})
	return c
}
func statusWarning(d Dependencies, err error, label string) bool {
	var he *client.HTTPError
	message := ""
	if errors.As(err, &he) {
		message = fmt.Sprintf("HTTP %d", he.Status)
	}
	var transport *client.TransportError
	if errors.As(err, &transport) {
		message = transport.Reason
	}
	if message == "" {
		return false
	}
	_, _ = fmt.Fprintln(d.Err, d.hint("(could not load "+label+": "+message+")"))
	return true
}
func statusAggregate(ctx context.Context, d Dependencies, api *client.Client, org, repo string, jobs bool, after time.Time, before *time.Time) ([]map[string]any, error) {
	query := url.Values{"time_bucket": {"total"}, "created_after": {after.Format("2006-01-02T15:04:05.000000-07:00")}}
	if before != nil {
		query.Set("created_before", before.Format("2006-01-02T15:04:05.000000-07:00"))
	}
	if repo != "" {
		query["repository_ids"] = []string{repo}
	}
	endpoint := "workflow-runs/aggregate"
	var rows []map[string]any
	var err error
	if jobs {
		endpoint = "jobs/aggregate"
		page, e := api.JobAggregates(ctx, org, query)
		rows, err = rawRecords(page), e
	} else {
		page, e := api.WorkflowAggregates(ctx, org, query)
		rows, err = rawRecords(page), e
	}
	if err != nil {
		if !statusWarning(d, err, endpoint) {
			return nil, err
		}
		return []map[string]any{}, nil
	}
	return rows, nil
}
func statusDeltas(current, previous []map[string]any, workflows bool) []map[string]any {
	key := func(row map[string]any) string {
		if workflows {
			return fieldText(row, "workflow_id")
		}
		return fieldText(diagnosticObject(row["job"]), "name")
	}
	byKey := map[string]map[string]any{}
	for _, row := range previous {
		byKey[key(row)] = row
	}
	result := []map[string]any{}
	for _, cur := range current {
		prev := byKey[key(cur)]
		if prev == nil {
			continue
		}
		a, b := metricNumber(cur["success_median_duration_seconds"]), metricNumber(prev["success_median_duration_seconds"])
		if a <= 0 || b <= 0 || metricNumber(cur["success_count"]) < 3 || metricNumber(prev["success_count"]) < 3 {
			continue
		}
		delta := 100 * (a - b) / b
		if delta <= 0 {
			continue
		}
		item := map[string]any{}
		for k, v := range cur {
			item[k] = v
		}
		item["_cur_median"], item["_prev_median"], item["_delta_pct"] = cur["success_median_duration_seconds"], prev["success_median_duration_seconds"], diagnosticNumber(delta)
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return metricNumber(result[i]["_delta_pct"]) > metricNumber(result[j]["_delta_pct"])
	})
	return result[:min(len(result), 5)]
}
func statusSlowest(rows []map[string]any) []map[string]any {
	result := []map[string]any{}
	for _, row := range rows {
		if metricNumber(row["success_median_duration_seconds"]) > 0 && metricNumber(row["success_count"]) >= 2 {
			result = append(result, row)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return metricNumber(result[i]["success_median_duration_seconds"]) > metricNumber(result[j]["success_median_duration_seconds"])
	})
	return result[:min(len(result), 5)]
}
func formatSize(value any) string {
	n := metricNumber(value)
	for _, unit := range []string{"B", "KB", "MB", "GB", "TB"} {
		if math.Abs(n) < 1024 {
			return fmt.Sprintf("%.1f %s", n, unit)
		}
		n /= 1024
	}
	return fmt.Sprintf("%.1f PB", n)
}
