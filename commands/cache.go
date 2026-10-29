package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var cacheFields = []string{"cache_type", "key", "ref", "size_bytes", "created_at", "last_accessed_at", "version", "hit_count"}
var cacheUsageFields = []string{"total_size_bytes", "quota_bytes", "over_quota", "by_type"}

// NewCache exposes shared build-cache inspection and confirmed deletion.
func NewCache(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "cache", Short: "Inspect and manage the Avrea build cache."}
	c.AddCommand(newCacheList(d), newCacheUsage(d), newCacheDelete(d))
	return c
}

func requiredNamedRepository(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, repo string) (string, string, error) {
	label, platform := repo, ""
	if label == "" {
		label = cfg.Repo
		if label == "" {
			remote, err := d.GitRemote(ctx)
			if err != nil && ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			label, platform = parseGitRemote(remote)
		}
		if label == "" {
			return "", "", &ExitError{1, "Error: Could not determine repository. Run from a git directory, pass --repo <org/name>, or set AVR_REPO."}
		}
		if d.IsErrorTerminal() && d.Getenv("AVR_REPO") != label {
			_, _ = fmt.Fprintln(d.Err, d.errorPaint("(using repo: "+output.CellText(label)+")", "2"))
		}
	}
	id, err := resolveRepository(ctx, d, cfg, api, org, label, platform)
	return id, label, err
}

func cacheContext(ctx context.Context, d Dependencies, org, repo string) (config.Config, *client.Client, string, string, error) {
	cfg, _, api, err := d.load()
	if err != nil {
		return cfg, api, "", "", err
	}
	if err = authenticated(cfg); err != nil {
		return cfg, api, "", "", err
	}
	id, err := resolveOrg(ctx, d, cfg, api, org)
	if err != nil {
		return cfg, api, "", "", err
	}
	repository, err := requiredWorkflowRepo(ctx, d, cfg, api, id, repo)
	return cfg, api, id, repository, err
}
func cacheScopeFlags(c *cobra.Command, org, repo *string) {
	c.Flags().StringVar(org, "org", "", "Organization ID or slug.")
	c.Flags().StringVar(repo, "repo", "", "Repository name or ID; otherwise detect from Git.")
}

func newCacheList(d Dependencies) *cobra.Command {
	var org, repo, kind, key, ref, order string
	var limit, offset int
	var web bool
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List cache entries for a repository.", Args: cobra.NoArgs}
	cacheScopeFlags(c, &org, &repo)
	o.flags(c)
	f := c.Flags()
	f.StringVar(&kind, "type", "", "Filter by cache type.")
	f.StringVar(&key, "key", "", "Filter by key prefix.")
	f.StringVar(&ref, "ref", "", "Filter by exact ref match.")
	f.IntVarP(&limit, "limit", "L", 100, "Maximum entries (1–1000).")
	f.IntVar(&offset, "offset", 0, "Number of entries to skip.")
	f.StringVar(&order, "order", "created_at.desc", "Sort order: created_at.desc or created_at.asc.")
	f.BoolVar(&web, "web", false, "Open in browser.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if offset < 0 {
			return &ExitError{2, "Error: --offset must be at least 0."}
		}
		if order != "created_at.desc" && order != "created_at.asc" {
			return &ExitError{2, "Error: invalid --order: " + output.CellText(order)}
		}
		if web && f.Changed("json") {
			return &ExitError{2, "Error: --web cannot be combined with --json."}
		}
		if done, err := o.prepare(cmd, d, cacheFields); done || err != nil {
			return err
		}
		cfg, api, id, rid, err := cacheContext(cmd.Context(), d, org, repo)
		if err != nil {
			return err
		}
		if web {
			target := consoleURL(cfg.Host) + "/org/" + url.PathEscape(orgSlug(cmd.Context(), cfg, api, id)) + "/caches/" + url.PathEscape(rid)
			label := target
			if d.IsTerminal() {
				label = "Opening: " + target
			}
			if _, err = fmt.Fprintln(d.Out, label); err != nil {
				return err
			}
			if d.IsTerminal() {
				return d.OpenBrowser(cmd.Context(), target)
			}
			return nil
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}, "order": {order}}
		if kind != "" {
			query.Set("cache_type", strings.ToLower(kind))
		}
		if key != "" {
			query.Set("key", key)
		}
		if ref != "" {
			query.Set("ref", ref)
		}
		if offset != 0 {
			query.Set("offset", strconv.Itoa(offset))
		}
		page, err := api.CacheEntries(cmd.Context(), id, rid, query)
		if err != nil {
			return apiError(cfg, err, "list cache entries")
		}
		rows := rawRecords(page)
		if f.Changed("json") {
			return o.list(cmd, d, rows, cacheFields)
		}
		if !d.IsTerminal() {
			headers := []string{"cache_type", "key", "ref", "size_bytes", "created_at"}
			var cells [][]string
			for _, row := range rows {
				values := make([]string, len(headers))
				for i, k := range headers {
					values[i] = fieldText(row, k)
				}
				if _, ok := row["size_bytes"]; !ok {
					values[3] = "0"
				}
				cells = append(cells, values)
			}
			return output.Table(d.Out, headers, cells, true)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No data found.")
		} else {
			var cells [][]string
			for _, row := range rows {
				key, ref := fieldText(row, "key"), fieldText(row, "ref")
				if key == "" {
					key = "-"
				}
				if ref == "" {
					ref = "-"
				}
				cells = append(cells, []string{diagnosticDefaultText(row, "cache_type", "N/A"), key, ref, formatSize(row["size_bytes"]), formatTimestamp(fieldText(row, "created_at"))})
			}
			err = output.Grid(d.Out, []string{"Type", "Key", "Ref", "Size", "Created"}, cells)
		}
		if err != nil {
			return err
		}
		total := float64(len(rows))
		if value, ok := page.Fields["total"]; ok {
			total = metricNumber(value)
		}
		shown := offset + len(rows)
		if float64(shown) < total {
			_, err = fmt.Fprintf(d.Err, "\nShowing %d/%s. Next page: --offset %d\n", shown, pythonValue(page.Fields["total"]), shown)
		}
		return err
	})
	return c
}

func newCacheUsage(d Dependencies) *cobra.Command {
	var org, repo string
	var o jsonOptions
	c := &cobra.Command{Use: "usage", Short: "Show cache usage summary for a repository.", Args: cobra.NoArgs}
	cacheScopeFlags(c, &org, &repo)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, cacheUsageFields); done || err != nil {
			return err
		}
		cfg, api, id, rid, err := cacheContext(cmd.Context(), d, org, repo)
		if err != nil {
			return err
		}
		record, err := api.CacheUsage(cmd.Context(), id, rid)
		if err != nil {
			return apiError(cfg, err, "get cache usage")
		}
		row := record.Fields
		if cmd.Flags().Changed("json") {
			fields, e := output.Fields(o.fields, cacheUsageFields)
			if e != nil {
				return e
			}
			return output.JSON(cmd.Context(), d.Out, row, fields, o.expression)
		}
		total, quota := metricNumber(row["total_size_bytes"]), metricNumber(row["quota_bytes"])
		pct := 0.0
		if quota > 0 {
			pct = total / quota * 100
		}
		over := "no"
		if row["over_quota"] == true {
			over = "yes"
		}
		if _, err = fmt.Fprintf(d.Out, "Total Size  %s\nQuota       %s\nUsage       %.1f%%\nOver Quota  %s\n", formatSize(row["total_size_bytes"]), formatSize(row["quota_bytes"]), pct, over); err != nil {
			return err
		}
		if byType := objectList(row["by_type"]); len(byType) > 0 {
			if _, err = fmt.Fprintln(d.Out); err != nil {
				return err
			}
			var cells [][]string
			for _, item := range byType {
				count := "N/A"
				if value, ok := item["entry_count"]; ok {
					count = pythonValue(value)
				}
				kind := "N/A"
				if value, ok := item["cache_type"]; ok {
					kind = pythonValue(value)
				}
				cells = append(cells, []string{kind, formatSize(item["size_bytes"]), count})
			}
			return output.Grid(d.Out, []string{"Type", "Size", "Count"}, cells)
		}
		return nil
	})
	return c
}

func newCacheDelete(d Dependencies) *cobra.Command {
	var org, repo, kind, key, ref string
	var all, yes bool
	c := &cobra.Command{Use: "delete", Short: "Delete cache entries by key name or all entries.", Args: cobra.NoArgs}
	cacheScopeFlags(c, &org, &repo)
	f := c.Flags()
	f.StringVar(&kind, "type", "", "Cache type (required with --key).")
	f.StringVar(&key, "key", "", "Delete entries matching this cache key name.")
	f.StringVar(&ref, "ref", "", "Ref to narrow deletion scope.")
	f.BoolVar(&all, "all", false, "Delete all cache entries for the repository.")
	f.BoolVarP(&yes, "yes", "y", false, "Skip confirmation.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		if (key != "") == all {
			return &ExitError{1, "Error: Provide exactly one of --key or --all.\nAborted!"}
		}
		if key != "" && kind == "" {
			return &ExitError{1, "Error: --type is required when using --key.\nAborted!"}
		}
		id, err := resolveOrg(cmd.Context(), d, cfg, api, org)
		if err != nil {
			return err
		}
		rid, label, err := requiredNamedRepository(cmd.Context(), d, cfg, api, id, repo)
		if err != nil {
			return err
		}
		endpoint := "/orgs/" + url.PathEscape(id) + "/repos/" + url.PathEscape(rid) + "/cache"
		action, promptAction := "purge cache", "cache purge needs confirmation"
		prompt := "Delete ALL cache entries for " + output.CellText(label) + "?"
		if key != "" {
			action, promptAction = "delete cache entries", "cache delete needs confirmation"
			scope := "across all refs"
			if ref != "" {
				scope = "ref '" + output.CellText(ref) + "'"
			}
			prompt = fmt.Sprintf("Delete entries in %s matching type '%s', key '%s' (%s)?", output.CellText(label), output.CellText(kind), output.CellText(key), scope)
			query := url.Values{"cache_type": {kind}, "key": {key}}
			if ref != "" {
				query.Set("ref", ref)
			}
			endpoint += "/entries?" + query.Encode()
		}
		if !yes {
			if err = confirmMutation(cmd.Context(), d, promptAction, prompt); err != nil {
				return err
			}
		}
		response, err := api.Do(cmd.Context(), http.MethodDelete, endpoint, nil)
		if err != nil {
			return apiError(cfg, err, action)
		}
		count := "0"
		if value, ok := response["deleted_count"]; ok {
			count = pythonValue(value)
		}
		noun := "entries"
		if metricNumber(response["deleted_count"]) == 1 {
			noun = "entry"
		}
		if key != "" {
			_, err = fmt.Fprintf(d.Out, "Deleted %s cache %s.\n", count, noun)
		} else {
			_, err = fmt.Fprintf(d.Out, "Purged %s cache %s from %s.\n", count, noun, output.CellText(label))
		}
		return err
	})
	return c
}
