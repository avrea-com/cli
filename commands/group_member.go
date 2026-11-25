package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var groupMemberFields = []string{"user_id", "name", "email", "source", "stale_sources", "added_at"}

var groupMemberResultFields = []string{"member", "user_id", "action", "status", "detail"}

// groupMemberPageLimit is the largest page GET .../members serves.
const groupMemberPageLimit = 250

// groupMemberBatchLimit is the most members one bulk request may change.
const groupMemberBatchLimit = 250

var userIDPattern = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile(`^usr-[0-9a-f]{32}$`) })

// memberRef is a member as the caller named them, and whom that resolved to.
type memberRef struct{ input, userID, name string }

func (m memberRef) display() string {
	if m.name == "" || m.name == m.input {
		return m.userID
	}
	return m.name + " (" + m.userID + ")"
}

// memberResolver turns a MEMBER argument into a user ID. A write names a
// member by user ID, @me or a verified email: a display name is set by the
// member, so one who took a name an administrator uses for someone else would
// receive what was meant for that person. Reads also accept a name.
type memberResolver struct {
	cfg     config.Config
	api     *client.Client
	org     string
	label   string
	me      string
	members []map[string]any
}

// memberKind is how a MEMBER argument names a member.
type memberKind int

const (
	memberSelf memberKind = iota
	memberID
	memberEmail
	memberName
)

// classifyMember reads a MEMBER argument without asking the API, so a value no
// request could resolve is refused before any request.
func classifyMember(value, label string, names bool) (memberKind, error) {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return 0, &ExitError{2, "Error: Invalid value for '" + label + "': expected a user ID (usr-...), @me or an email address."}
	case value == "@me":
		return memberSelf, nil
	case userIDPattern().MatchString(value):
		return memberID, nil
	case strings.Contains(strings.TrimPrefix(value, "@"), "@"):
		return memberEmail, nil
	case strings.HasPrefix(value, "usr-"):
		return 0, &ExitError{2, "Error: Invalid value for '" + label + "': " + pythonQuote(output.CellText(value)) + " is not a user ID. A user ID is usr- followed by 32 lowercase hexadecimal characters."}
	case names:
		return memberName, nil
	}
	return 0, &ExitError{2, "Error: Invalid value for '" + label + "': " + pythonQuote(output.CellText(value)) + " is not a user ID, @me or an email address. A change names a member by user ID or verified email, never by display name, which members set themselves.\n  Hint: Ask the person for their user ID (avr auth status shows it), or an organization administrator."}
}

func (r *memberResolver) resolve(ctx context.Context, value string, names bool) (memberRef, error) {
	value = strings.TrimSpace(value)
	kind, err := classifyMember(value, r.label, names)
	if err != nil {
		return memberRef{}, err
	}
	switch kind {
	case memberSelf:
		if r.me == "" {
			record, err := r.api.Do(ctx, http.MethodGet, "/users/me", nil)
			if err != nil {
				return memberRef{}, apiError(r.cfg, err, "read your account")
			}
			if r.me = fieldText(record, "id"); !userIDPattern().MatchString(r.me) {
				return memberRef{}, errors.New("unexpected API response: your account has no user ID")
			}
		}
		return memberRef{input: value, userID: r.me}, nil
	case memberID:
		return memberRef{input: value, userID: value}, nil
	case memberEmail:
		return r.byEmail(ctx, value)
	case memberName:
		return r.byName(ctx, value)
	default:
		return memberRef{}, fmt.Errorf("unknown member kind %d", kind)
	}
}

// byEmail asks the API for the active member who verified the address. The API
// answers only for members the caller may manage and answers any other address
// as no match, so a miss never means the address is unknown to Avrea.
func (r *memberResolver) byEmail(ctx context.Context, email string) (memberRef, error) {
	page, err := r.api.PostList(ctx, "/orgs/"+url.PathEscape(r.org)+"/member-lookups", map[string]any{"email": email})
	quoted := pythonQuote(output.CellText(email))
	if err != nil {
		var he *client.HTTPError
		if errors.As(err, &he) && he.Status == 404 && !reauthenticationRequired(he) {
			return memberRef{}, &ExitError{1, "Error: Looking up members by email is not available with this credential, organization or server (HTTP 404).\n  Hint: Name the member by user ID instead."}
		}
		result := apiError(r.cfg, err, "look up members by email")
		if errors.As(err, &he) && he.Status == 403 && !reauthenticationRequired(he) {
			result = insertTokenHint(result, "Only administrators who manage members can look them up by email.")
		}
		return memberRef{}, result
	}
	switch len(page.Items) {
	case 0:
		return memberRef{}, &ExitError{1, "Error: No member you can manage has verified " + quoted + ".\n  Hint: Ask the person for their user ID (avr auth status shows it), or an organization administrator. Never take one from a name in the member list: members set their names themselves."}
	case 1:
		id := fieldText(page.Items[0], "user_id")
		if !userIDPattern().MatchString(id) {
			return memberRef{}, fmt.Errorf("unexpected API response: the member who verified %s has no user ID", quoted)
		}
		return memberRef{input: email, userID: id, name: fieldText(page.Items[0], "name")}, nil
	}
	return memberRef{}, fmt.Errorf("unexpected API response: %d members verified %s", len(page.Items), quoted)
}

// byName matches a display name, ignoring case, over the organization's
// active members. Reads only.
func (r *memberResolver) byName(ctx context.Context, name string) (memberRef, error) {
	if r.members == nil {
		rows, err := listOrganizationMembers(ctx, r.cfg, r.api, r.org)
		if err != nil {
			return memberRef{}, err
		}
		r.members = rows
	}
	var matches []memberRef
	for _, row := range r.members {
		if strings.EqualFold(fieldText(row, "state"), "active") && strings.EqualFold(fieldText(row, "name"), name) {
			matches = append(matches, memberRef{input: name, userID: fieldText(row, "user_id"), name: fieldText(row, "name")})
		}
	}
	quoted := pythonQuote(output.CellText(name))
	switch len(matches) {
	case 0:
		return memberRef{}, &ExitError{1, "Error: Invalid value for '" + r.label + "': no active member is named " + quoted + ".\n  Hint: Run `avr org members --org " + shellQuotePython(r.org) + "` to see members, or pass a user ID."}
	case 1:
		return matches[0], nil
	}
	var message strings.Builder
	fmt.Fprintf(&message, "Error: Invalid value for '%s': %s names %d members:", r.label, quoted, len(matches))
	for _, m := range matches {
		fmt.Fprintf(&message, "\n    %s", output.CellText(m.userID))
	}
	message.WriteString("\n  Hint: Pass the user ID.")
	return memberRef{}, &ExitError{1, message.String()}
}

// listOrganizationMembers reads every member, following cursors if the API
// pages the listing.
func listOrganizationMembers(ctx context.Context, cfg config.Config, api *client.Client, org string) ([]map[string]any, error) {
	rows, _, err := walkPages("member", 0, func(cursor string, _ int) (client.Page, error) {
		query := url.Values{}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		page, err := api.DataList(ctx, "/orgs/"+url.PathEscape(org)+"/members", query)
		if err != nil {
			return page, apiError(cfg, err, "list members")
		}
		return page, nil
	})
	return rows, err
}

// memberValues reads MEMBER arguments and, with --file, one member per line;
// blank lines, comments and repeats are skipped.
func memberValues(ctx context.Context, d Dependencies, args []string, file string) ([]string, error) {
	values := make([]string, len(args))
	for i, arg := range args {
		values[i] = strings.TrimSpace(arg)
	}
	values = distinct(values)
	if file != "" {
		reader := d.In
		if file != "-" {
			handle, err := os.Open(file)
			if err != nil {
				return nil, &ExitError{2, "Error: Invalid value for '--file': " + err.Error()}
			}
			defer func() { _ = handle.Close() }()
			reader = handle
		}
		// Reading stops at the first member past the batch limit, and an
		// interrupt ends a read that standard input keeps open. The read owns
		// its own slice, since it can outlive an interrupted command, and
		// starts no read after the interrupt: the rest of the input belongs
		// to whatever reads it next.
		given := values
		var err error
		values, err = configurationRead(ctx, func() ([]string, error) {
			read := slices.Clone(given)
			seen := map[string]bool{}
			for _, value := range read {
				seen[value] = true
			}
			scanner := bufio.NewScanner(reader)
			for len(read) <= groupMemberBatchLimit && ctx.Err() == nil && scanner.Scan() {
				if line := strings.TrimSpace(scanner.Text()); line != "" && !memberFileComment(line) && !seen[line] {
					seen[line] = true
					read = append(read, line)
				}
			}
			return read, scanner.Err()
		})
		if err != nil {
			return nil, err
		}
	}
	if len(values) > groupMemberBatchLimit {
		return nil, &ExitError{2, fmt.Sprintf("Error: One change takes at most %d members; more were given.\n  Hint: Split the list and run the command once per part.", groupMemberBatchLimit)}
	}
	return values, nil
}

// memberFileComment reports whether a --file line is a comment: one starting
// with #, unless the whole line is an email address, which may start with #
// too. Skipping such an address would leave that member unchanged; a line
// read as an address that no member verified fails the lookup instead.
func memberFileComment(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	address, err := mail.ParseAddress(line)
	return err != nil || address.Address != line
}

func newGroupMember(d Dependencies) *cobra.Command {
	c := &cobra.Command{Use: "member", Aliases: []string{"members"}, Short: "List, add and remove a group's members."}
	c.AddCommand(newGroupMemberList(d), newGroupMemberChange(d, "add"), newGroupMemberChange(d, "remove"))
	return c
}

func newGroupMemberList(d Dependencies) *cobra.Command {
	var org string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list GROUP", Short: "List a group's direct members.", Args: positionalArguments("GROUP")}
	orgFlag(c, &org)
	c.Flags().IntVarP(&limit, "limit", "L", 100, "Maximum members.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		value, err := groupReference(args[0], "GROUP")
		if err != nil {
			return err
		}
		if limit < 1 {
			return &ExitError{2, "Error: --limit must be at least 1."}
		}
		if done, err := o.prepare(cmd, d, groupMemberFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Listing group members", "list group members")
		if err != nil {
			return err
		}
		group, err := findGroup(cmd.Context(), cfg, api, id, value, "GROUP")
		if err != nil {
			return err
		}
		rows, more, err := walkPages("member", limit, func(cursor string, wanted int) (client.Page, error) {
			query := url.Values{"limit": {strconv.Itoa(min(wanted, groupMemberPageLimit))}}
			if cursor != "" {
				query.Set("cursor", cursor)
			}
			page, err := api.DataList(cmd.Context(), groupRoute(id, group.id, "members"), query)
			if err != nil {
				return page, groupFailure(cfg, err, groupRefusal{action: "list the group's members", notFound: groupHiddenHint})
			}
			return page, nil
		})
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("json") {
			err = o.list(cmd, d, rows, groupMemberFields)
		} else {
			err = renderGroupMembers(d, rows)
		}
		if err == nil && more {
			_, err = fmt.Fprintf(d.Err, "\nShowing the first %d members. Raise --limit to see more.\n", len(rows))
		}
		return err
	})
	return c
}

func renderGroupMembers(d Dependencies, rows []map[string]any) error {
	headers := []string{"user_id", "name", "email", "source", "stale_sources"}
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		line := make([]string, len(headers))
		for i, key := range headers {
			line[i] = fieldText(row, key)
		}
		cells = append(cells, line)
	}
	if !d.IsTerminal() {
		return output.Table(d.Out, headers, cells, true)
	}
	if len(rows) == 0 {
		_, err := fmt.Fprintln(d.Out, "No members.")
		return err
	}
	return output.Grid(d.Out, []string{"User ID", "Name", "Email", "Source", "Stale sources"}, cells)
}

// newGroupMemberChange builds `group member add` and `group member remove`.
// Each sends one bulk request, which the API applies in one transaction with a
// result per member and an item limit of its own; avr never splits a batch.
func newGroupMemberChange(d Dependencies, action string) *cobra.Command {
	var org, file string
	var yes bool
	var o jsonOptions
	short := "Add members to a group."
	if action == "remove" {
		short = "Remove members from a group."
	}
	c := &cobra.Command{Use: action + " GROUP [MEMBER]...", Short: short, Args: cobra.MinimumNArgs(1)}
	f := c.Flags()
	orgFlag(c, &org)
	f.StringVar(&file, "file", "", "Read members from a file, one per line; - reads standard input.")
	if action == "remove" {
		f.BoolVarP(&yes, "yes", "y", false, "Remove without asking.")
	}
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		value, err := groupReference(args[0], "GROUP")
		if err != nil {
			return err
		}
		// Output options are checked, and fields listed, before any member is
		// read, so neither waits on standard input.
		if done, err := o.prepare(cmd, d, groupMemberResultFields); done || err != nil {
			return err
		}
		if action == "remove" && file == "-" && !yes {
			return &ExitError{2, "Error: Removing members read from standard input needs --yes: the confirmation prompt cannot read the same input."}
		}
		values, err := memberValues(cmd.Context(), d, args[1:], file)
		if err != nil {
			return err
		}
		if len(values) == 0 {
			return &ExitError{2, "Error: Pass at least one MEMBER, or --file."}
		}
		for _, value := range values {
			if _, err := classifyMember(value, "MEMBER", false); err != nil {
				return err
			}
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Changing group members", "change group members")
		if err != nil {
			return err
		}
		group, err := findGroup(cmd.Context(), cfg, api, id, value, "GROUP")
		if err != nil {
			return err
		}
		resolver := &memberResolver{cfg: cfg, api: api, org: id, label: "MEMBER"}
		var members []memberRef
		seen := map[string]bool{}
		for _, value := range values {
			member, err := resolver.resolve(cmd.Context(), value, false)
			if err != nil {
				return err
			}
			if !seen[member.userID] {
				seen[member.userID] = true
				members = append(members, member)
			}
		}
		if action == "remove" {
			var preface strings.Builder
			fmt.Fprintf(&preface, "Remove %d member(s) from group %s:", len(members), output.CellText(group.display()))
			for _, member := range members {
				fmt.Fprintf(&preface, "\n  %s", output.CellText(member.display()))
			}
			if err = confirmChange(cmd, d, yes, "removing group members needs confirmation", preface.String(), "Remove them?"); err != nil {
				return err
			}
		}
		ids := make([]string, len(members))
		for i, member := range members {
			ids[i] = member.userID
		}
		// The check names the group by ID: a rerun by path or email could
		// reach another group, or no longer find a member it removed.
		check := checkBeforeRetry(groupCommand("member list", group.id, id))
		refusal := groupRefusal{
			action:   action + " group members",
			notFound: groupHiddenHint,
			unknown:  check,
		}
		response, err := api.Do(cmd.Context(), http.MethodPost, groupRoute(id, group.id, "members", "bulk"), map[string]any{action: ids})
		if err != nil {
			return groupFailure(cfg, err, refusal)
		}
		rows, err := groupMemberResults(action, members, response)
		if err != nil {
			return groupFailure(cfg, err, refusal)
		}
		if cmd.Flags().Changed("json") {
			err = o.list(cmd, d, rows, groupMemberResultFields)
		} else {
			err = renderGroupMemberResults(d, rows)
		}
		summary := groupMemberSummary(rows, check)
		if err != nil {
			// The summary can be the only report that a member did not
			// change, so an output failure must not hide it.
			if summary != "" {
				_, _ = fmt.Fprintln(d.Err, summary)
			}
			return err
		}
		if summary != "" {
			return &ExitError{1, summary}
		}
		return nil
	})
	return c
}

// groupMemberResults pairs each requested member with the API's result for
// them. A member whose result is missing, repeated, for another action or of
// a status avr does not know is reported as unknown, never as success.
func groupMemberResults(action string, members []memberRef, response map[string]any) ([]map[string]any, error) {
	raw, ok := response["results"].([]any)
	if !ok {
		return nil, errors.New("unexpected API response: expected results")
	}
	byUser := map[string][]map[string]any{}
	for _, item := range raw {
		result, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("unexpected API response: expected result objects")
		}
		user := fieldText(result, "user_id")
		byUser[user] = append(byUser[user], result)
	}
	rows := make([]map[string]any, len(members))
	for i, member := range members {
		row := map[string]any{"member": member.input, "user_id": member.userID, "action": action, "status": "unknown", "detail": nil}
		results := byUser[member.userID]
		switch {
		case len(results) == 0:
			row["detail"] = "the API returned no result for this member"
		case len(results) > 1:
			row["detail"] = "the API returned more than one result for this member"
		case results[0]["action"] != action:
			row["detail"] = "the API returned a result for another action"
		case !slices.Contains([]any{"ok", "noop", "error"}, results[0]["status"]):
			row["detail"] = "the API returned a status this version of avr does not know: " + pythonValue(results[0]["status"])
		default:
			row["status"], row["detail"] = results[0]["status"], results[0]["detail"]
		}
		rows[i] = row
	}
	return rows, nil
}

// groupMemberSummary is the error that fails the command unless every member
// was changed or already in place, counting refusals apart from unknown
// outcomes, which check tells how to verify; it is empty otherwise.
func groupMemberSummary(rows []map[string]any, check string) string {
	counts := map[any]int{}
	for _, row := range rows {
		counts[row["status"]]++
	}
	if counts["error"] == 0 && counts["unknown"] == 0 {
		return ""
	}
	var parts []string
	for _, part := range []struct {
		status any
		label  string
	}{{"ok", "changed"}, {"noop", "already in place"}, {"error", "refused"}, {"unknown", "with an unknown outcome"}} {
		if counts[part.status] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[part.status], part.label))
		}
	}
	message := "Error: Not every member was changed: " + strings.Join(parts, ", ") + "."
	if counts["unknown"] > 0 {
		message += "\n  Hint: " + check
	}
	return message
}

func renderGroupMemberResults(d Dependencies, rows []map[string]any) error {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, []string{fieldText(row, "member"), fieldText(row, "user_id"), groupMemberOutcome(row)})
	}
	if !d.IsTerminal() {
		return output.Table(d.Out, []string{"member", "user_id", "result"}, cells, true)
	}
	return output.Grid(d.Out, []string{"Member", "User ID", "Result"}, cells)
}

// groupMemberOutcome says what one item did. A change already in place counts
// as success, so a rerun is safe.
func groupMemberOutcome(row map[string]any) string {
	action, status := fieldText(row, "action"), fieldText(row, "status")
	switch {
	case status == "ok" && action == "add":
		return "added"
	case status == "ok" && action == "remove":
		return "removed"
	case status == "noop" && action == "add":
		return "already a member"
	case status == "noop" && action == "remove":
		return "not a member"
	case status == "error":
		return "refused: " + fieldText(row, "detail")
	}
	return status + ": " + fieldText(row, "detail")
}
