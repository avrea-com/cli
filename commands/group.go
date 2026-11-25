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
	"unicode"

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
	c := &cobra.Command{Use: "group", Aliases: []string{"groups"}, Short: "Build and manage the organization's group tree."}
	c.AddCommand(newGroupList(d), newGroupView(d), newGroupCreate(d), newGroupEdit(d), newGroupDelete(d), newGroupMember(d), newGroupLimit(d), newGroupUsage(d))
	return c
}

func groupListHint(org string) string {
	return "Run " + groupCommand("list", "", org) + " to see the organization's groups."
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

// groupOption returns a --group value as resolveGroup compares it, and
// rejects one no group can match before any request.
func groupOption(value string) (string, error) { return groupReference(value, "--group") }

// groupReference reads a group name, path or ID given as label, the option or
// argument named in errors. Surrounding spaces go, as the API drops them from
// names. Slashes stay, since a name may hold one; matchGroup also tries the
// value without them.
func groupReference(value, label string) (string, error) {
	value = strings.TrimSpace(value)
	if bareGroupValue(value) == "" {
		return "", &ExitError{2, "Error: Invalid value for '" + label + "': expected a group name, path or ID (grp-...)."}
	}
	return value, nil
}

// bareGroupValue is value without the slashes and spaces around it, as a
// path is often copied.
func bareGroupValue(value string) string { return strings.TrimFunc(value, aroundGroupValue) }

func aroundGroupValue(r rune) bool { return r == '/' || unicode.IsSpace(r) }

// groupRef is a resolved group. path is empty when the group was named by ID,
// which needs no listing.
type groupRef struct{ id, path string }

// display names the group as a person would look for it.
func (g groupRef) display() string {
	if g.path == "" {
		return g.id
	}
	return g.path + " (" + g.id + ")"
}

// resolveGroup returns the ID of the group value names. A group ID is used as
// is. Any other value names the one group whose whole path it is, or else the
// one group whose path ends with it after a /. Names compare without case, as
// the API keeps sibling names unique that way, and deleted groups are left
// out: they cannot own a token and would only make a name ambiguous.
func resolveGroup(ctx context.Context, cfg config.Config, api *client.Client, org, value string) (string, error) {
	group, err := findGroup(ctx, cfg, api, org, value, "--group")
	return group.id, err
}

// findGroup resolves value as resolveGroup does; label names the option or
// argument in errors.
func findGroup(ctx context.Context, cfg config.Config, api *client.Client, org, value, label string) (groupRef, error) {
	if serviceTokenGroupPattern().MatchString(value) {
		return groupRef{id: value}, nil
	}
	rows, err := listGroups(ctx, cfg, api, org)
	if err != nil {
		return groupRef{}, err
	}
	return matchGroup(rows, value, label, org)
}

// matchGroup resolves value against an organization's listed groups. A bare
// group ID is used as is, with its path when the listing holds it.
func matchGroup(rows []map[string]any, value, label, org string) (groupRef, error) {
	paths := groupPaths(rows)
	byID := func(id string) groupRef {
		for i, row := range rows {
			if fieldText(row, "group_id") == id {
				return groupRef{id: id, path: strings.Join(paths[i], "/")}
			}
		}
		return groupRef{id: id}
	}
	if serviceTokenGroupPattern().MatchString(value) {
		return byID(value), nil
	}
	// A value copied with slashes or spaces around it may name a group, or a
	// group ID, without some of them. It is read as typed and without those
	// at its start, its end or both. When readings name different groups,
	// only the ID tells which one was meant: Acme/Ops/ is a group of its own
	// when one is named Ops/, and so is grp-.../ when a name is that.
	var readings [][]groupRef
	for _, reading := range []string{value, strings.TrimLeftFunc(value, aroundGroupValue), strings.TrimRightFunc(value, aroundGroupValue), bareGroupValue(value)} {
		found := groupCandidates(rows, paths, reading)
		if serviceTokenGroupPattern().MatchString(reading) {
			found = []groupRef{byID(reading)}
		}
		if len(found) > 0 && !slices.ContainsFunc(readings, func(other []groupRef) bool { return slices.Equal(other, found) }) {
			readings = append(readings, found)
		}
	}
	var matches []groupRef
	for _, found := range readings {
		for _, group := range found {
			if !slices.Contains(matches, group) {
				matches = append(matches, group)
			}
		}
	}
	hint := "Pass a longer path or the group ID."
	if len(readings) > 1 {
		hint = "Pass the group ID: the value names different groups with and without the slashes around it."
	}
	quoted := pythonQuote(output.CellText(value))
	switch len(matches) {
	case 0:
		message := "Error: Invalid value for '" + label + "': no group is named " + quoted + " or has that path."
		if strings.HasPrefix(value, "grp-") {
			message += " A group ID is grp- followed by 32 lowercase hexadecimal characters."
		}
		return groupRef{}, &ExitError{2, message + "\n  Hint: " + groupListHint(org)}
	case 1:
		return matches[0], nil
	}
	slices.SortFunc(matches, func(a, b groupRef) int { return strings.Compare(a.path+"\x00"+a.id, b.path+"\x00"+b.id) })
	var message strings.Builder
	fmt.Fprintf(&message, "Error: Invalid value for '%s': %s matches %d groups:", label, quoted, len(matches))
	for _, m := range matches {
		fmt.Fprintf(&message, "\n    %s  (%s)", output.CellText(m.path), output.CellText(m.id))
	}
	message.WriteString("\n  Hint: " + hint)
	return groupRef{}, &ExitError{2, message.String()}
}

// groupCandidates returns the live group whose whole path is value, or else
// every live group whose path ends with value after a /. A whole path cannot
// be made longer, so it wins over paths that end with the same names.
func groupCandidates(rows []map[string]any, paths [][]string, value string) []groupRef {
	var whole, matches []groupRef
	wanted := strings.ToLower(value)
	for i, names := range paths {
		if rows[i]["soft_deleted_at"] != nil {
			continue
		}
		path := strings.Join(names, "/")
		found := groupRef{id: fieldText(rows[i], "group_id"), path: path}
		switch folded := strings.ToLower(path); {
		case folded == wanted:
			whole = append(whole, found)
			matches = append(matches, found)
		case strings.HasSuffix(folded, "/"+wanted):
			matches = append(matches, found)
		}
	}
	if len(whole) == 1 {
		return whole
	}
	return matches
}

func newGroupList(d Dependencies) *cobra.Command {
	var org string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List the organization's groups by path.", Args: cobra.NoArgs}
	c.Flags().StringVarP(&org, "org", "o", "", "Organization ID or slug.")
	c.Flags().IntVarP(&limit, "limit", "L", 50, "Maximum groups.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 {
			return &ExitError{2, "Error: --limit must be at least 1."}
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
			_, err = fmt.Fprintf(d.Err, "\nShowing the first %d of %d groups. Raise --limit to see the rest.\n", len(rows), len(records))
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
