// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func audit_eventsDescription(path string) docgen.Description {
	switch path {
	case "avr audit-events list":
		return docgen.Description{
			Help: "List audit events for the organization.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    acting_api_key_id, action, actor_type, actor_user_id, client_ip,\n" +
				"    created_at, event_data, event_id, resource_id, resource_type\n",
			Short: "List audit events for the organization.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "resource_type", Kind: "option", Opts: []string{"--resource-type"}, Type: "text", Nargs: 1, Help: descriptionText("Filter by resource type (e.g. api_key, user).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "action", Kind: "option", Opts: []string{"--action"}, Type: "text", Nargs: 1, Help: descriptionText("Filter by action (CREATE, UPDATE, DELETE, ...).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "actor_user_id", Kind: "option", Opts: []string{"--actor-user-id"}, Type: "text", Nargs: 1, Help: descriptionText("Filter by acting user id.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "created_after", Kind: "option", Opts: []string{"--from", "--created-after"}, Type: "text", Nargs: 1, Help: descriptionText("ISO-8601 lower bound (inclusive) on created_at.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "created_before", Kind: "option", Opts: []string{"--to", "--created-before"}, Type: "text", Nargs: 1, Help: descriptionText("ISO-8601 upper bound (exclusive) on created_at.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max events to return."), ShowDefault: true, Default: 100}, Suffix: "  [default: 100; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Opaque cursor from a previous response's next_cursor.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr audit-events":
		return docgen.Description{
			Help:  "View audit events for organization writes.",
			Short: "View audit events for organization writes.",
			Usage: []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Commands: []docgen.HelpCommand{
				{Name: "list", Description: "List audit events for the organization."},
			},
		}
	}
	return docgen.Description{}
}
