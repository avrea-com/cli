package commands

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var groupFields = []string{"group_id", "parent_id", "display_name", "label", "ownership", "state", "suspension_reason", "soft_deleted_at", "member_count", "created_at", "updated_at", "path"}

// groupPageLimit is the largest page GET /orgs/{org_id}/groups serves.
const groupPageLimit = 250

// NewGroup exposes the organization's groups to a signed-in member, so that
// the --group options of other commands can be given a group's path.
func NewGroup(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "group", Aliases: []string{"groups"}, Short: "View the organization's groups."}
	c.AddCommand(newGroupList(d))
	return c
}

func groupListHint(org string) string {
	return "Run \x60avr group list --org " + shellQuotePython(org) + "\x60 to see the organization's groups."
}

func groupError(cfg config.Config, err error) error {
	var he *client.HTTPError
	if !errors.As(err, &he) || he.Status != 404 {
		return apiError(cfg, err, "list groups")
	}
	message := "Error: Groups are not available for this organization (HTTP 404).\n  Hint: Check the organization and API host. If groups should be available, contact support@avrea.com."
	if cfg.Host != config.DefaultHost {
		message += "\n  API: " + cfg.Host + he.Path
	}
	return &ExitError{1, message}
}

// walkPages follows a listing's cursors until it ends or holds limit rows; a
// limit of 0 reads everything. fetch reads the page at a cursor, given how
// many rows are still wanted. more reports rows beyond the limit. A cursor
// that repeats, or arrives with an empty page, ends the walk with an error:
// following it would loop or list the same rows again.
func walkPages(noun string, limit int, fetch func(cursor string, wanted int) (client.Page, error)) (rows []map[string]any, more bool, err error) {
	rows = []map[string]any{}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := fetch(cursor, limit-len(rows))
		if err != nil {
			return nil, false, err
		}
		rows = append(rows, page.Items...)
		cursor = page.NextCursor
		if limit > 0 && len(rows) >= limit {
			return rows[:limit], cursor != "" || len(rows) > limit, nil
		}
		if cursor == "" {
			return rows, false, nil
		}
		if len(page.Items) == 0 || seen[cursor] {
			return nil, false, errors.New(noun + " listing returned a cursor that does not advance")
		}
		seen[cursor] = true
	}
}

// listGroups reads every group of the organization.
func listGroups(ctx context.Context, cfg config.Config, api *client.Client, org string) ([]map[string]any, error) {
	rows, _, err := walkPages("group", 0, func(cursor string, _ int) (client.Page, error) {
		query := url.Values{"limit": {strconv.Itoa(groupPageLimit)}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		page, err := api.Groups(ctx, org, query)
		if err != nil {
			return page, groupError(cfg, err)
		}
		return page, nil
	})
	return rows, err
}

// groupPaths returns each row's display names from the root down. The walk
// stops at a parent it has already visited or cannot find, so a malformed
// chain cannot loop.
func groupPaths(rows []map[string]any) [][]string {
	byID := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		byID[fieldText(row, "group_id")] = row
	}
	paths := make([][]string, len(rows))
	for i, row := range rows {
		names := []string{fieldText(row, "display_name")}
		seen := map[string]bool{fieldText(row, "group_id"): true}
		for parent := byID[fieldText(row, "parent_id")]; parent != nil && !seen[fieldText(parent, "group_id")]; parent = byID[fieldText(parent, "parent_id")] {
			seen[fieldText(parent, "group_id")] = true
			names = append(names, fieldText(parent, "display_name"))
		}
		slices.Reverse(names)
		paths[i] = names
	}
	return paths
}

// checkGroupOption rejects a --group value no group can match before any
// request.
func checkGroupOption(value string) error {
	if strings.TrimSpace(value) == "" {
		return &ExitError{2, "Error: Invalid value for '--group': expected a group name, path or ID (grp-...)."}
	}
	return nil
}

// resolveGroup returns the ID of the group value names. A group ID is used as
// is. Any other value names the one group whose whole path it is, or else the
// one group whose path ends with it after a /. Names compare without case, as
// the API keeps sibling names unique that way, and deleted groups are left
// out: they cannot own a token and would only make a name ambiguous.
func resolveGroup(ctx context.Context, cfg config.Config, api *client.Client, org, value string) (string, error) {
	if serviceTokenGroupPattern().MatchString(value) {
		return value, nil
	}
	rows, err := listGroups(ctx, cfg, api, org)
	if err != nil {
		return "", err
	}
	type match struct{ path, id string }
	var whole, matches []match
	wanted := strings.ToLower(value)
	for i, names := range groupPaths(rows) {
		if rows[i]["soft_deleted_at"] != nil {
			continue
		}
		path := strings.Join(names, "/")
		found := match{path, fieldText(rows[i], "group_id")}
		switch folded := strings.ToLower(path); {
		case folded == wanted:
			whole = append(whole, found)
			matches = append(matches, found)
		case strings.HasSuffix(folded, "/"+wanted):
			matches = append(matches, found)
		}
	}
	// A whole path cannot be made longer, so it wins over paths that end
	// with the same names.
	if len(whole) == 1 {
		return whole[0].id, nil
	}
	quoted := pythonQuote(output.CellText(value))
	switch len(matches) {
	case 0:
		message := "Error: Invalid value for '--group': no group is named " + quoted + " or has that path."
		if strings.HasPrefix(value, "grp-") {
			message += " A group ID is grp- followed by 32 lowercase hexadecimal characters."
		}
		return "", &ExitError{2, message + "\n  Hint: " + groupListHint(org)}
	case 1:
		return matches[0].id, nil
	}
	slices.SortFunc(matches, func(a, b match) int { return strings.Compare(a.path+"\x00"+a.id, b.path+"\x00"+b.id) })
	var message strings.Builder
	fmt.Fprintf(&message, "Error: Invalid value for '--group': %s matches %d groups:", quoted, len(matches))
	for _, m := range matches {
		fmt.Fprintf(&message, "\n    %s  (%s)", output.CellText(m.path), output.CellText(m.id))
	}
	message.WriteString("\n  Hint: Pass a longer path or the group ID.")
	return "", &ExitError{2, message.String()}
}

func newGroupList(d Dependencies) *cobra.Command {
	var org string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List the organization's groups by path.", Args: cobra.NoArgs}
	c.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	c.Flags().IntVarP(&limit, "limit", "L", 50, "Maximum groups (1–1000).")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if done, err := o.prepare(cmd, d, groupFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Listing groups", "list groups")
		if err != nil {
			return err
		}
		records, err := listGroups(cmd.Context(), cfg, api, id)
		if err != nil {
			return err
		}
		paths := groupPaths(records)
		order := make([]int, len(records))
		for i := range order {
			order[i] = i
		}
		// Comparing name lists keeps a parent before its children and siblings
		// together, whatever characters the names hold.
		slices.SortStableFunc(order, func(a, b int) int { return slices.Compare(paths[a], paths[b]) })
		// Paths need every group, so the limit cuts the sorted list.
		rows := make([]map[string]any, min(limit, len(records)))
		for i, index := range order[:len(rows)] {
			row := maps.Clone(records[index])
			row["path"] = strings.Join(paths[index], "/")
			rows[i] = row
		}
		if cmd.Flags().Changed("json") {
			err = o.list(cmd, d, rows, groupFields)
		} else {
			err = renderGroupList(d, rows)
		}
		if err != nil {
			return err
		}
		if len(rows) < len(records) {
			advice := " Raise --limit to see the rest."
			if limit == 1000 {
				advice = ""
			}
			_, err = fmt.Fprintf(d.Err, "\nShowing the first %d of %d groups.%s\n", len(rows), len(records), advice)
		}
		return err
	})
	return c
}

func renderGroupList(d Dependencies, rows []map[string]any) error {
	piped := !d.IsTerminal()
	if !piped && len(rows) == 0 {
		_, err := fmt.Fprintln(d.Out, "No data found.")
		return err
	}
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, []string{fieldText(row, "group_id"), fieldText(row, "path"), fieldText(row, "label"), fieldText(row, "state"), fieldText(row, "member_count")})
	}
	if piped {
		return output.Table(d.Out, []string{"group_id", "path", "label", "state", "member_count"}, cells, true)
	}
	return output.Grid(d.Out, []string{"Group ID", "Path", "Label", "State", "Members"}, cells)
}
