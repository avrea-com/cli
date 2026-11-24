package commands

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var effectiveAccessFields = []string{"user_id", "org_role", "org_role_source", "groups", "resource_access", "resource_access_complete", "resource_access_truncated", "root_capabilities", "notes", "governance"}

var governanceFields = []string{"groups_enabled", "resource_access_enabled", "delegation_enabled", "eligible_root_admin", "root_capabilities", "manual_root_capabilities", "groups"}

func newOrgMembersAccess(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "access MEMBER", Short: "Explain a member's effective access and where it comes from.", Args: positionalArguments("MEMBER")}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if _, err := classifyMember(args[0], "MEMBER", true); err != nil {
			return err
		}
		if done, err := o.prepare(cmd, d, effectiveAccessFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Explaining access", "explain access")
		if err != nil {
			return err
		}
		resolver := &memberResolver{cfg: cfg, api: api, org: id, label: "MEMBER"}
		member, err := resolver.resolve(cmd.Context(), args[0], true)
		if err != nil {
			return err
		}
		record, err := api.DataRecord(cmd.Context(), http.MethodGet, "/orgs/"+url.PathEscape(id)+"/members/"+url.PathEscape(member.userID)+"/effective-access", nil)
		if err != nil {
			return groupFailure(cfg, err, groupRefusal{action: "explain the member's access", notFound: "The member may not be active in the organization, or may be hidden from you."})
		}
		record["user_id"] = member.userID
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record, effectiveAccessFields)
		}
		return renderEffectiveAccess(d, member, record)
	})
	return c
}

func renderEffectiveAccess(d Dependencies, member memberRef, record map[string]any) error {
	role := fieldText(record, "org_role")
	if source := fieldText(record, "org_role_source"); source != "" {
		role += " (" + source + ")"
	}
	if err := output.KeyValues(d.Out, [][2]string{{"Member", member.display()}, {"Role", role}}); err != nil {
		return err
	}
	groups := objectList(record["groups"])
	if _, err := fmt.Fprintln(d.Out, "\nGroups:"); err != nil {
		return err
	}
	if len(groups) == 0 {
		if _, err := fmt.Fprintln(d.Out, "  none"); err != nil {
			return err
		}
	}
	for _, group := range groups {
		line := "  " + effectiveGroupPath(group) + " (" + fieldText(group, "group_id") + ")"
		if source := fieldText(group, "source"); source != "" {
			line += "  " + source
		}
		if stale := fieldText(group, "stale_sources"); stale != "" {
			line += "  stale: " + stale
		}
		if state := fieldText(group, "state"); state != "" && !strings.EqualFold(state, "active") {
			line += "  " + state
		}
		if _, err := fmt.Fprintln(d.Out, output.CellText(line)); err != nil {
			return err
		}
	}
	entries := objectList(record["resource_access"])
	if _, err := fmt.Fprintln(d.Out, "\nAccess:"); err != nil {
		return err
	}
	if len(entries) == 0 {
		if _, err := fmt.Fprintln(d.Out, "  none"); err != nil {
			return err
		}
	} else {
		cells := make([][]string, 0, len(entries))
		for _, entry := range entries {
			object := fieldText(entry, "resource_id")
			if entry["resource_id"] == nil {
				object = "all"
				if fieldText(entry, "resource_type") == "repository" {
					// A repository wildcard covers Avrea repositories; the code
					// host decides the rest, in an entry of its own.
					object = "all Avrea repositories"
					if fieldText(entry, "via") == "code_host" {
						object = "repositories on the code host"
					}
				}
			}
			cells = append(cells, []string{fieldText(entry, "resource_type"), object, fieldText(entry, "access_level"), accessSource(entry)})
		}
		if err := output.Grid(d.Out, []string{"Type", "Object", "Level", "Because"}, cells); err != nil {
			return err
		}
	}
	if record["resource_access_truncated"] == true {
		if _, err := fmt.Fprintln(d.Out, "\nThis list is partial: it leaves out VMs past the newest 200 the member created, or their groups' service tokens created."); err != nil {
			return err
		}
	} else if record["resource_access_complete"] == false {
		if _, err := fmt.Fprintln(d.Out, "\nSome entries come from groups you may not see, so they do not name the group."); err != nil {
			return err
		}
	}
	if capabilities := objectList(record["root_capabilities"]); len(capabilities) > 0 {
		names := make([]string, len(capabilities))
		for i, capability := range capabilities {
			names[i] = fieldText(capability, "capability") + " (" + fieldText(capability, "via") + ")"
		}
		if _, err := fmt.Fprintln(d.Out, "\nOrganization capabilities: "+output.CellText(strings.Join(names, ", "))); err != nil {
			return err
		}
	}
	for _, note := range settingsStrings(record["notes"]) {
		if _, err := fmt.Fprintln(d.Out, "\nNote: "+output.CellText(note)); err != nil {
			return err
		}
	}
	return nil
}

// accessSource says why an access entry reaches the member. A source this
// version of avr does not know is shown as such, never dropped.
func accessSource(entry map[string]any) string {
	group := fieldText(entry, "group_name")
	if id := fieldText(entry, "group_id"); id != "" {
		group = strings.TrimSpace(group + " (" + id + ")")
	}
	if group == "" {
		group = "you cannot see"
	}
	switch via := fieldText(entry, "via"); via {
	case "group_grant":
		return "grant to group " + group
	case "creator":
		return "the member created it"
	case "service_token_creator":
		return "a service token of group " + group + " created it"
	case "org_role":
		return "organization role"
	case "code_host":
		return "the code host decides per repository"
	default:
		return "unknown source (" + via + ")"
	}
}

func effectiveGroupPath(group map[string]any) string {
	if path := settingsStrings(group["path"]); len(path) > 0 {
		return strings.Join(path, "/")
	}
	return fieldText(group, "display_name")
}

func newOrgGovernance(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "governance", Short: "Show what you may manage in the organization.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, governanceFields); done || err != nil {
			return err
		}
		cfg, api, id, err := personContext(cmd.Context(), d, org, "Reading governance", "read governance")
		if err != nil {
			return err
		}
		record, err := api.DataRecord(cmd.Context(), http.MethodGet, "/orgs/"+url.PathEscape(id)+"/governance", nil)
		if err != nil {
			return groupFailure(cfg, err, groupRefusal{action: "read the organization's governance", notFound: "Governance may be unavailable with this credential, organization or server."})
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record, governanceFields)
		}
		return renderGovernance(d, record)
	})
	return c
}

func renderGovernance(d Dependencies, record map[string]any) error {
	onOff := func(key string) string {
		if record[key] == true {
			return "on"
		}
		return "off"
	}
	root := "no"
	if record["eligible_root_admin"] == true {
		root = "yes"
	}
	if err := output.KeyValues(d.Out, [][2]string{
		{"Groups", onOff("groups_enabled")},
		{"Resource access", onOff("resource_access_enabled")},
		{"Delegation", onOff("delegation_enabled")},
		{"Eligible root administrator", root},
	}); err != nil {
		return err
	}
	decisions := objectList(record["root_capabilities"])
	if len(decisions) > 0 {
		cells := make([][]string, 0, len(decisions))
		for _, decision := range decisions {
			cells = append(cells, []string{fieldText(decision, "capability"), decisionText(decision)})
		}
		if _, err := fmt.Fprintln(d.Out, "\nOrganization capabilities:"); err != nil {
			return err
		}
		if err := output.Grid(d.Out, []string{"Capability", "Decision"}, cells); err != nil {
			return err
		}
	}
	// The API also lists groups where every decision is denied, such as one
	// whose suspensions the caller may only lift.
	groups := objectList(record["groups"])
	if _, err := fmt.Fprintln(d.Out, "\nGroups:"); err != nil {
		return err
	}
	if len(groups) == 0 {
		_, err := fmt.Fprintln(d.Out, "  none")
		return err
	}
	cells := make([][]string, 0, len(groups))
	for _, group := range groups {
		allowed, denied := []string{}, []string{}
		for _, decision := range objectList(group["capabilities"]) {
			if decision["allowed"] == true {
				allowed = append(allowed, fieldText(decision, "capability"))
			} else {
				denied = append(denied, fieldText(decision, "capability"))
			}
		}
		if group["can_recover_suspensions"] == true {
			allowed = append(allowed, "lift suspensions")
		}
		if group["can_suspend_group"] == true {
			allowed = append(allowed, "suspend the group")
		}
		cells = append(cells, []string{fieldText(group, "group_id"), fieldText(group, "display_name"), strings.Join(allowed, ", "), strings.Join(denied, ", ")})
	}
	return output.Grid(d.Out, []string{"Group ID", "Name", "Allowed", "Denied"}, cells)
}

// decisionText is one capability decision: allowed, or denied with the
// API's reason.
func decisionText(decision map[string]any) string {
	if decision["allowed"] == true {
		return "allowed"
	}
	if denial := fieldText(decision, "denial"); denial != "" {
		return "denied: " + denial
	}
	return "denied"
}
