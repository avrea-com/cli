package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

// groupRecordFields are the fields of a group the API returns from a write,
// which has no path.
var groupRecordFields = slices.DeleteFunc(slices.Clone(groupFields), func(field string) bool { return field == "path" })

var groupCreateFields = append(slices.Clone(groupRecordFields), "idempotency_key")

var groupDeleteFields = []string{"group_id", "path", "deleted"}

// groupRoute is the API path of the organization's groups, or of one group and
// what lies below it.
func groupRoute(org string, parts ...string) string {
	path := "/orgs/" + url.PathEscape(org) + "/groups"
	for _, part := range parts {
		path += "/" + url.PathEscape(part)
	}
	return path
}

// groupUnwritten are the 503 reasons the group routes give before any write,
// as serviceTokenUnwritten does for the service-token routes. Any other 503,
// such as one from a proxy after the write committed, leaves the outcome
// unknown.
var groupUnwritten = []string{"Delegation authorization is not ready"}

// groupIdPOwned is the API's reason for refusing to delete a group its SAML
// group mappings own. Removing the last mapping hands the group to Avrea.
const groupIdPOwned = "IdP-synced groups cannot be deleted manually"

// groupRefusal names the hints a refused group request gets beyond apiError's
// and the API's own reason.
type groupRefusal struct {
	action   string // completes "You don't have access to ..."
	notFound string // 404
	conflict string // 409 without Retry-After
	unknown  string // a write: what to do when its outcome is unknown
}

// checkBeforeRetry advises checking a write of unknown outcome with command
// before retrying it.
func checkBeforeRetry(command string) string {
	return "Check it with " + command + " before retrying."
}

const groupHiddenHint = "The group may not exist, or may be hidden from you. If groups are unavailable with this credential, organization or server, `avr group list` fails the same way."

// groupFailure explains a failed group request. A write whose answer was lost
// or unreadable, or that a server error ended after it may have committed,
// says the outcome is unknown and how to check it before any retry; a write
// the API refused says nothing changed.
func groupFailure(cfg config.Config, err error, r groupRefusal) error {
	write := r.unknown != ""
	var he *client.HTTPError
	if !errors.As(err, &he) {
		if !write {
			return err
		}
		code := 1
		if errors.Is(err, context.Canceled) {
			code = 130
		}
		return &ExitError{code, "Error: Could not confirm the request to " + r.action + ": " + err.Error() + ".\n  Hint: The outcome is unknown. " + r.unknown}
	}
	if he.Status == 401 || reauthenticationRequired(he) {
		refused := apiError(cfg, he, r.action)
		var exit *ExitError
		if write && errors.As(refused, &exit) {
			return &ExitError{exit.Code, insertTokenHint(errors.New(exit.Message), "Nothing changed.").Error()}
		}
		return refused
	}
	unwritten := he.Status == 503 && slices.Contains(groupUnwritten, he.Detail)
	if write && he.Status >= 500 && !unwritten {
		message := fmt.Sprintf("Error: Avrea is having trouble (HTTP %d), and the request to %s may have taken effect.", he.Status, r.action)
		if he.Detail != "" {
			message += "\n  Detail: " + output.CellText(he.Detail)
		}
		message += "\n  Hint: The outcome is unknown. " + r.unknown
		if cfg.Host != config.DefaultHost {
			message += "\n  API: " + cfg.Host + he.Path
		}
		return &ExitError{1, message}
	}
	var hints []string
	if write {
		hints = append(hints, "Nothing changed.")
	}
	switch {
	case he.Status == 404:
		hints = append(hints, r.notFound)
	case he.Status == 409 && he.RetryAfter != "":
		hints = append(hints, "Another change holds the group; the request is safe to retry. "+tokenRetryAfter(he))
	case he.Status == 409:
		hints = append(hints, r.conflict)
	case unwritten:
		hints = append(hints, tokenRetryAfter(he))
	}
	return insertTokenHint(apiError(cfg, he, r.action), strings.Join(slices.DeleteFunc(hints, func(hint string) bool { return hint == "" }), " "))
}

// groupCommand is an avr group command to suggest, run on group in org;
// group may be empty for a command that takes none.
func groupCommand(command, group, org string) string {
	if group != "" {
		command += " " + shellQuotePython(group)
	}
	return "`avr group " + command + " --org " + shellQuotePython(org) + "`"
}

func newGroupView(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "view GROUP", Short: "Show a group's details and path.", Args: positionalArguments("GROUP")}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		value, err := groupReference(args[0], "GROUP")
		if err != nil {
			return err
		}
		if done, err := o.prepare(cmd, d, groupFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Viewing groups", "view groups")
		if err != nil {
			return err
		}
		// The listing gives the path, which the group's own record lacks. A
		// listed group shows its listing row, so the path and the fields come
		// from one read; an ID the listing lacks, such as a deleted group, is
		// read on its own and has no path.
		rows, err := listGroups(cmd.Context(), cfg, api, id)
		if err != nil {
			return err
		}
		group, err := matchGroup(rows, value, "GROUP", id)
		if err != nil {
			return err
		}
		var record map[string]any
		if i := slices.IndexFunc(rows, func(row map[string]any) bool { return fieldText(row, "group_id") == group.id }); i >= 0 {
			record = rows[i]
		} else if record, err = api.DataRecord(cmd.Context(), http.MethodGet, groupRoute(id, group.id), nil); err != nil {
			return groupFailure(cfg, err, groupRefusal{action: "view the group", notFound: groupHiddenHint})
		}
		record["path"] = nil
		if group.path != "" {
			record["path"] = group.path
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record, groupFields)
		}
		return renderGroup(d, record)
	})
	return c
}

func renderGroup(d Dependencies, record map[string]any) error {
	text := func(key string) string {
		if record[key] == nil {
			return "-"
		}
		return pythonValue(record[key])
	}
	pairs := [][2]string{{"Group ID", text("group_id")}}
	if _, ok := record["path"]; ok {
		pairs = append(pairs, [2]string{"Path", text("path")})
	}
	pairs = append(pairs,
		[2]string{"Name", text("display_name")},
		[2]string{"Parent ID", text("parent_id")},
		[2]string{"Label", text("label")},
		[2]string{"State", text("state")},
		[2]string{"Ownership", text("ownership")},
		[2]string{"Members", text("member_count")},
	)
	if record["soft_deleted_at"] != nil {
		pairs = append(pairs, [2]string{"Deleted", text("soft_deleted_at")})
	}
	if record["suspension_reason"] != nil {
		pairs = append(pairs, [2]string{"Suspension", text("suspension_reason")})
	}
	pairs = append(pairs, [2]string{"Created", text("created_at")}, [2]string{"Updated", text("updated_at")})
	if key, ok := record["idempotency_key"]; ok && key != nil {
		pairs = append(pairs, [2]string{"Idempotency key", text("idempotency_key")})
	}
	return output.KeyValues(d.Out, pairs)
}

func newGroupCreate(d Dependencies) *cobra.Command {
	var org, parent, label, key string
	var o jsonOptions
	c := &cobra.Command{Use: "create NAME", Short: "Create a group below a parent group.", Args: positionalArguments("NAME")}
	f := c.Flags()
	orgFlag(c, &org)
	f.StringVar(&parent, "parent", "", "The parent group: a group name, path or ID (grp-...).")
	f.StringVar(&label, "label", "", "A short label shown with the group.")
	invocationKeyFlag(c, &key, "Resume this create after a lost response; a new key is generated and printed when omitted.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		name := strings.TrimSpace(args[0])
		if name == "" {
			return &ExitError{2, "Error: Invalid value for 'NAME': a group needs a name."}
		}
		if !f.Changed("parent") {
			return &ExitError{2, "Error: Missing option '--parent'. Pass the parent group, such as --parent Organization for a group at the top of the tree."}
		}
		parentValue, err := groupReference(parent, "--parent")
		if err != nil {
			return err
		}
		if done, err := o.prepare(cmd, d, groupCreateFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Creating groups", "create groups")
		if err != nil {
			return err
		}
		under, err := findGroup(cmd.Context(), cfg, api, id, parentValue, "--parent")
		if err != nil {
			return err
		}
		// A generated key stays local: the flag's variable outlives this run
		// when an embedder executes the same command tree again.
		key := key
		if key == "" {
			if key, err = newInvocationKey(); err != nil {
				return err
			}
		}
		// Printed before the request, so a lost response still leaves the key
		// that resumes this create and no other.
		if _, err = fmt.Fprintln(d.Err, "Idempotency key: "+key); err != nil {
			return err
		}
		body := map[string]any{"parent_id": under.id, "display_name": name, "idempotency_key": key}
		if f.Changed("label") && label != "" {
			body["label"] = label
		}
		record, err := api.DataRecord(cmd.Context(), http.MethodPost, groupRoute(id), body)
		if err == nil && !serviceTokenGroupPattern().MatchString(fieldText(record, "group_id")) {
			// A receipt without the group cannot say whether it was created.
			err = errors.New("unexpected API response: the created group has no valid group_id")
		}
		if err != nil {
			return groupFailure(cfg, err, groupRefusal{
				action:   "create the group",
				notFound: "The parent group may have been deleted or be hidden from you. If groups are unavailable with this credential, organization or server, `avr group list` fails the same way.",
				conflict: groupCreateConflict(err, key),
				unknown:  "Rerun this command with --idempotency-key " + key + ": it returns the group if this create made one, and creates it if not.",
			})
		}
		record["idempotency_key"] = key
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record, groupCreateFields)
		}
		return renderGroup(d, record)
	})
	return c
}

// groupCreateConflict explains a 409 the create answered without Retry-After.
func groupCreateConflict(err error, key string) string {
	var he *client.HTTPError
	if !errors.As(err, &he) {
		return ""
	}
	switch code, _ := he.Payload["code"].(string); code {
	case "idempotency_key_reused":
		return "Key " + key + " belongs to a create with another parent, name or label. Omit --idempotency-key to create a new group."
	case "idempotency_key_group_deleted":
		return "The group key " + key + " created has since been deleted. Omit --idempotency-key to create a new group."
	}
	if he.Detail == groupNameTaken {
		return "If an earlier attempt of yours created it, rerun that attempt with its idempotency key to get the group; otherwise choose another name."
	}
	return ""
}

// groupNameTaken is the API's reason for a name a sibling group already has.
// Any other conflict, such as a parent that moved during the request, is
// explained by its own reason.
const groupNameTaken = "A live sibling group already has this name"

func newGroupEdit(d Dependencies) *cobra.Command {
	var org, name, label string
	var o jsonOptions
	c := &cobra.Command{Use: "edit GROUP", Short: "Rename a group or change its label.", Args: positionalArguments("GROUP")}
	f := c.Flags()
	orgFlag(c, &org)
	f.StringVar(&name, "name", "", "The group's new name.")
	f.StringVar(&label, "label", "", "The group's new label; an empty value clears it.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		value, err := groupReference(args[0], "GROUP")
		if err != nil {
			return err
		}
		// Only the fields given are sent, so an edit cannot undo another
		// writer's change to a field it leaves alone.
		body := map[string]any{}
		if f.Changed("name") {
			if strings.TrimSpace(name) == "" {
				return &ExitError{2, "Error: Invalid value for '--name': a group needs a name."}
			}
			body["display_name"] = strings.TrimSpace(name)
		}
		if f.Changed("label") {
			body["label"] = nil
			if label != "" {
				body["label"] = label
			}
		}
		if len(body) == 0 {
			return &ExitError{2, "Error: Pass --name or --label to change the group."}
		}
		if done, err := o.prepare(cmd, d, groupRecordFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Editing groups", "edit groups")
		if err != nil {
			return err
		}
		group, err := findGroup(cmd.Context(), cfg, api, id, value, "GROUP")
		if err != nil {
			return err
		}
		record, err := api.DataRecord(cmd.Context(), http.MethodPatch, groupRoute(id, group.id), body)
		if err != nil {
			return groupFailure(cfg, err, groupRefusal{
				action:   "edit the group",
				notFound: groupHiddenHint,
				unknown:  checkBeforeRetry(groupCommand("view", group.id, id)),
			})
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record, groupRecordFields)
		}
		return renderGroup(d, record)
	})
	return c
}

func newGroupDelete(d Dependencies) *cobra.Command {
	var org string
	var yes bool
	var o jsonOptions
	c := &cobra.Command{Use: "delete GROUP", Short: "Delete an empty group.", Args: positionalArguments("GROUP")}
	orgFlag(c, &org)
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Delete without asking.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		value, err := groupReference(args[0], "GROUP")
		if err != nil {
			return err
		}
		if done, err := o.prepare(cmd, d, groupDeleteFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Deleting groups", "delete groups")
		if err != nil {
			return err
		}
		group, err := findGroup(cmd.Context(), cfg, api, id, value, "GROUP")
		if err != nil {
			return err
		}
		if err = confirmChange(cmd, d, yes, "deleting a group needs confirmation", "", "Delete group "+output.CellText(group.display())+"? Its resource access grants go with it."); err != nil {
			return err
		}
		if _, err = api.Do(cmd.Context(), http.MethodDelete, groupRoute(id, group.id), nil); err != nil {
			if blocked := groupDeletionBlockers(cfg, err, group, id); blocked != nil {
				return blocked
			}
			refusal := groupRefusal{
				action:   "delete the group",
				notFound: groupHiddenHint,
				unknown:  "Check it with " + groupCommand("view", group.id, id) + " before retrying: a deleted group shows when it was deleted, or is not found.",
			}
			var he *client.HTTPError
			if errors.As(err, &he) && he.Detail == groupIdPOwned {
				refusal.conflict = "Remove its SAML group mappings in the Avrea console (SSO / SAML settings), then delete it again."
			}
			return groupFailure(cfg, err, refusal)
		}
		record := map[string]any{"group_id": group.id, "path": nil, "deleted": true}
		if group.path != "" {
			record["path"] = group.path
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record, groupDeleteFields)
		}
		_, err = fmt.Fprintln(d.Out, "Deleted group "+output.CellText(group.display())+".")
		return err
	})
	return c
}

// confirmChange asks before a destructive, access-reducing or
// privilege-raising change unless approved. preface, when given, is printed
// before the question. With --json the exchange goes to stderr, so standard
// output stays machine-readable.
func confirmChange(cmd *cobra.Command, d Dependencies, approved bool, action, preface, prompt string) error {
	if approved {
		return nil
	}
	if cmd.Flags().Changed("json") {
		d = configurationPrompt(d)
	}
	if preface != "" {
		if _, err := fmt.Fprintln(d.Out, preface); err != nil {
			return err
		}
	}
	return confirmMutation(cmd.Context(), d, action, prompt)
}

// groupDeletionBlockers explains a deletion the API refused because the group
// is still in use, naming each blocker and how to clear it. The CLI clears
// none of them itself. It returns nil for any other failure.
func groupDeletionBlockers(cfg config.Config, err error, group groupRef, org string) error {
	var he *client.HTTPError
	if !errors.As(err, &he) || he.Status != 409 || he.RetryAfter != "" || he.Payload["code"] != "group_in_use" {
		return nil
	}
	var lines []string
	for _, blocker := range objectList(he.Payload["blockers"]) {
		switch fieldText(blocker, "type") {
		case "limit":
			lines = append(lines, "a "+output.CellText(fieldText(blocker, "limit_key"))+" limit: clear it with "+groupCommand("limit clear", group.id, org))
		case "suspension":
			lines = append(lines, "a suspension of member "+output.CellText(fieldText(blocker, "user_id"))+": lift it in the group's settings in the Avrea console")
		case "workflow":
			workflow := fieldText(blocker, "workflow_id")
			clear := "`avr api -X PUT " + shellQuotePython("/orgs/"+org+"/workflows/"+workflow+"/attribution-context") + " -F group_id=null`"
			lines = append(lines, "workflow "+output.CellText(workflow)+" of repository "+output.CellText(fieldText(blocker, "repository_id"))+" charges its jobs to this group: an organization administrator with access to the repository can clear that with "+output.CellText(clear))
		default:
			lines = append(lines, "a blocker this version of avr does not know ("+output.CellText(pythonValue(blocker["type"]))+"): see the Avrea console")
		}
	}
	// Only workflows go unnamed: the server checks the first 100, for anyone,
	// and names those in repositories it confirmed the caller may read.
	if hidden, ok := he.Payload["hidden_blocker_count"]; ok && hidden != nil && pythonValue(hidden) != "0" {
		count := pythonValue(hidden)
		noun := "workflows"
		if count == "1" {
			noun = "workflow"
		}
		lines = append(lines, count+" more "+noun+" not listed: the server checks at most 100 at a time and names those in repositories it confirmed you can read. Once the listed ones are cleared, deleting again names more; a workflow in a repository you cannot read needs someone who can read it")
	}
	message := apiError(cfg, he, "delete the group").Error()
	if len(lines) > 0 {
		message += "\n  The group is still used by:\n    - " + strings.Join(lines, "\n    - ")
	}
	return errors.New(message + "\n  Hint: Nothing changed. Clear these, then delete the group again.")
}
