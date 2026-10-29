package commands

import (
	"errors"
	"fmt"
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

var prFields = []string{"number", "title", "state", "draft", "merged", "author_login", "base_ref", "head_ref", "head_sha", "base_sha", "created_at", "updated_at", "comment_count", "unresolved_thread_count", "check_status", "mergeability", "repository_id", "repository_full_name"}

const prFeature = "feature.org-pull-requests.enabled"

// NewPR returns shared pull-request inspection commands.
func NewPR(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	group := &cobra.Command{Use: "pr", Aliases: []string{"prs"}, Short: "View pull requests."}
	group.AddCommand(newPRList(d))
	return group
}
func prRequestError(cfg config.Config, err error, action string) error {
	var transport *client.TransportError
	if errors.As(err, &transport) {
		return &ExitError{1, "Error: Could not list pull requests because the request to Avrea failed: " + transport.Reason}
	}
	return apiError(cfg, err, action)
}
func newPRList(d Dependencies) *cobra.Command {
	var o jsonOptions
	var org, scope, state, cursor string
	var repos []string
	var limit int
	c := &cobra.Command{Use: "list", Short: "List pull requests across repositories.", Args: cobra.NoArgs}
	o.flags(c)
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringArrayVar(&repos, "repo", nil, "Repository name or ID (repeatable); otherwise detect from Git.")
	f.StringVar(&scope, "scope", "all", "List all, authored, or involved pull requests.")
	f.StringVar(&state, "state", "open", "Filter by state: open, closed, merged, or all.")
	f.IntVarP(&limit, "limit", "L", 20, "Maximum pull requests (1–200).")
	f.StringVar(&cursor, "cursor", "", "Pagination cursor.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 200 {
			return &ExitError{2, "Error: --limit must be between 1 and 200."}
		}
		scope, state = strings.ToLower(scope), strings.ToLower(state)
		if !slices.Contains([]string{"all", "authored", "involved"}, scope) {
			return &ExitError{2, "Error: invalid --scope: " + output.CellText(scope)}
		}
		if !slices.Contains([]string{"open", "closed", "merged", "all"}, state) {
			return &ExitError{2, "Error: invalid --state: " + output.CellText(state)}
		}
		if done, err := o.prepare(cmd, d, prFields); done || err != nil {
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
		feature, err := api.FeatureFlag(cmd.Context(), id, prFeature)
		if err != nil {
			var httpErr *client.HTTPError
			if errors.As(err, &httpErr) && httpErr.Status == 404 {
				return &ExitError{1, "Error: Sorry, we could not find this organization or confirm pull request listing support. Organizations without Avrea Git are not supported yet. Coming soon."}
			}
			return prRequestError(cfg, err, "check pull request listing availability")
		}
		if feature.Fields["enabled"] != true {
			return &ExitError{1, "Error: Sorry, pull request listing is not yet supported for this organization. Coming soon."}
		}
		ids, err := resolveRepositories(cmd.Context(), d, cfg, api, id, repos, true)
		if err != nil {
			return err
		}
		query := url.Values{"scope": {scope}, "limit": {strconv.Itoa(limit)}}
		if state != "all" {
			query.Set("state", state)
		}
		if len(ids) > 0 {
			query["repository_ids"] = ids
		}
		if f.Changed("cursor") {
			if cursor == "" || strings.TrimSpace(cursor) != cursor || utf8.RuneCountInString(cursor) > 512 {
				return &ExitError{2, "Error: --cursor must be 1..512 chars and not surrounded by whitespace"}
			}
			query.Set("cursor", cursor)
		}
		page, err := api.PullRequests(cmd.Context(), id, query)
		if err != nil {
			return prRequestError(cfg, err, "list pull requests")
		}
		rows := rawRecords(page)
		if f.Changed("json") {
			return o.list(cmd, d, rows, prFields)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No pull requests found.")
			return err
		}
		if !d.IsTerminal() {
			err = renderPRPipe(d, rows)
		} else {
			slug := ""
			if linksEnabled(cmd, d) {
				slug = orgSlug(cmd.Context(), cfg, api, id)
			}
			err = renderPRTable(d, rows, len(ids) != 1, cfg.Host, slug)
		}
		if err != nil {
			return err
		}
		if page.NextCursor != "" {
			_, err = fmt.Fprintf(d.Err, "\nMore results available. Next page: --cursor %s\n", output.CellText(page.NextCursor))
		}
		return err
	})
	return c
}
func prState(row map[string]any) string {
	if row["merged"] == true {
		return "merged"
	}
	if row["draft"] == true && row["state"] != "closed" {
		return "draft"
	}
	if state := fieldText(row, "state"); state != "" {
		return state
	}
	return "unknown"
}
func prMergeStatus(row map[string]any) string {
	merge, _ := row["mergeability"].(map[string]any)
	if status := fieldText(merge, "status"); status != "" {
		return status
	}
	if row["merged"] == true || row["draft"] == true || row["state"] == "closed" {
		return "-"
	}
	return "?"
}
func renderPRPipe(d Dependencies, rows []map[string]any) error {
	headers := []string{"number", "repository", "state", "title", "author", "head_ref", "base_ref", "check_status", "mergeability", "comment_count", "unresolved_threads", "head_sha", "updated_at"}
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		repo := fieldText(row, "repository_full_name")
		if repo == "" {
			repo = fieldText(row, "repository_id")
		}
		merge, _ := row["mergeability"].(map[string]any)
		cells = append(cells, []string{fieldText(row, "number"), repo, prState(row), fieldText(row, "title"), fieldText(row, "author_login"), fieldText(row, "head_ref"), fieldText(row, "base_ref"), fieldText(row, "check_status"), fieldText(merge, "status"), fieldText(row, "comment_count"), fieldText(row, "unresolved_thread_count"), fieldText(row, "head_sha"), fieldText(row, "updated_at")})
	}
	return output.Table(d.Out, headers, cells, true)
}
func renderPRTable(d Dependencies, rows []map[string]any, showRepo bool, host, slug string) error {
	widths := []int{8, 30, 8, 11, 12, 7, 48, 18, 10}
	headers := []string{"PR", "REPOSITORY", "STATE", "CHECKS", "MERGE", "UNRES", "TITLE", "AUTHOR", "UPDATED"}
	cell := func(text string, index int) string {
		return padded(shortCell(text, widths[index]-1), widths[index], false)
	}
	var b strings.Builder
	b.WriteString("  ")
	for i, label := range headers {
		if i == 1 && !showRepo {
			continue
		}
		b.WriteString(d.paint(cell(label, i), gray+";4"))
	}
	b.WriteByte('\n')
	for _, row := range rows {
		fallback := func(key, otherwise string) string {
			if s := fieldText(row, key); s != "" {
				return s
			}
			return otherwise
		}
		repo := fallback("repository_full_name", fallback("repository_id", "?"))
		number := "?"
		if row["number"] != nil {
			number = "#" + fieldText(row, "number")
		}
		unresolved := "?"
		if row["unresolved_thread_count"] != nil {
			unresolved = fieldText(row, "unresolved_thread_count")
		}
		values := []string{number, repo, prState(row), fallback("check_status", "-"), prMergeStatus(row), unresolved, fieldText(row, "title"), fallback("author_login", "?"), relativeAge(fieldText(row, "updated_at"), d.Now())}
		target := ""
		if slug != "" && fieldText(row, "repository_id") != "" && row["number"] != nil {
			target = consoleURL(host) + "/org/" + url.PathEscape(slug) + "/repos/" + url.PathEscape(fieldText(row, "repository_id")) + "/pulls/" + fieldText(row, "number")
		}
		b.WriteString("  ")
		for i, text := range values {
			if i == 1 && !showRepo {
				continue
			}
			text = cell(text, i)
			if i == 0 {
				text = d.paint(text, "36")
			}
			if i == 0 || i == 6 {
				text = linkCell(text, target)
			}
			if i == 8 {
				text = d.paint(text, "2")
			}
			b.WriteString(text)
		}
		b.WriteByte('\n')
	}
	_, err := fmt.Fprint(d.Out, b.String())
	return err
}
