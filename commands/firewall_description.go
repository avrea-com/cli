// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func firewallDescription(path string) docgen.Description {
	switch path {
	case "avr firewall edit":
		return docgen.Description{
			Help: "Edit only the supplied fields, preserving rule identity and order.\n" +
				"Switching hostname/CIDR clears the other matcher. Any or ICMP clears ports.\n" +
				"Use --ports all to remove a port restriction; numeric ports require a TCP/UDP rule.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "action", Kind: "option", Opts: []string{"--action"}, Type: "choice", Nargs: 1, Choices: []string{"allow", "deny"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "proto", Kind: "option", Opts: []string{"--proto"}, Type: "choice", Nargs: 1, Choices: []string{"tcp", "udp", "icmp", "any"}}, CompletionKind: "Choice", CaseSensitive: true},
			},
		}
	case "avr firewall export":
		return docgen.Description{Help: "Export the selected scope in the same versioned JSON format as the console.\nInherited rules and storage IDs are excluded. Use --file PATH, or redirect stdout."}
	case "avr firewall import":
		return docgen.Description{
			Help: "Import a console-compatible JSON policy into one organization or repository.\n" +
				"Append skips duplicates and supplied defaults. Replace uses the file's default;\n" +
				"without one, repository defaults inherit and organization defaults are retained.\n" +
				"Every import is validated before applying. Use --dry-run to review changes.\n" +
				"Replace prompts for confirmation; --yes is required with stdin or automation.\n" +
				"If the policy changes after preview, the import fails without overwriting it.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "mode", Kind: "option", Opts: []string{"--mode"}, Type: "choice", Nargs: 1, Default: "append", ShowDefault: true, Choices: []string{"append", "replace"}}, CompletionKind: "Choice", CaseSensitive: true},
			},
		}
	case "avr firewall add":
		return docgen.Description{
			Help:  "Add a rule. Exactly one of --cidr, --fqdn, --any must be specified.",
			Short: "Add a rule.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository ID or owner/repo. If provided, adds at repo scope.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "action", Kind: "option", Opts: []string{"--action"}, Type: "choice", Required: true, Nargs: 1, Choices: []string{"allow", "deny"}}, Suffix: "[required]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "cidr", Kind: "option", Opts: []string{"--cidr"}, Type: "text", Nargs: 1, Help: descriptionText("Destination CIDR (e.g. 10.0.0.0/8 or 1.2.3.4/32).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "fqdn", Kind: "option", Opts: []string{"--fqdn"}, Type: "text", Nargs: 1, Help: descriptionText("Destination hostname (e.g. api.example.com).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "any_matcher", Kind: "option", Opts: []string{"--any"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Set the catch-all default, preserving its ID if it already exists.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "proto", Kind: "option", Opts: []string{"--proto"}, Type: "choice", Nargs: 1, Default: "any", Choices: []string{"tcp", "udp", "icmp", "any"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "ports", Kind: "option", Opts: []string{"--ports"}, Type: "text", Nargs: 1, Help: descriptionText("Port or port range (0-65535), or all.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "position", Kind: "option", Opts: []string{"--position"}, Type: "integer", Nargs: 1, Help: descriptionText("Insert at a specific 0-indexed position.")}, CompletionKind: "Int"},
			},
		}
	case "avr firewall delete":
		return docgen.Description{
			Help:  "Delete a rule by ID.",
			Short: "Delete a rule by ID.",
			Usage: []string{"[OPTIONS]", "RULE_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "rule_id", Kind: "argument", Opts: []string{"rule_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository ID or owner/repo. If provided, deletes a repo-level rule.")}, CompletionKind: "String"},
			},
		}
	case "avr firewall flow-summaries":
		return docgen.Description{
			Help: "Show per-VM network activity summaries captured at VM stop.\n" +
				"\n" +
				"Each row is the totals + top-N destinations for one VM run. The Blocked\n" +
				"column counts both per-rule packet drops and DNS queries the firewall\n" +
				"refused to resolve. Use ``--with-drops`` to triage what the firewall\n" +
				"blocked after editing a rule or ``--job`` to include every execution\n" +
				"attempt for a job.\n",
			Short:          "Show per-VM network activity summaries captured at VM stop.",
			CompletionHelp: "Show per-VM network activity summaries...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("Repository ID or owner/repo.")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "job_id", Kind: "option", Opts: []string{"--job", "--job-id"}, Type: "text", Nargs: 1, Help: descriptionText("Filter to every VM execution attempt for a job ID.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "only_with_drops", Kind: "option", Opts: []string{"--with-drops"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Show only summaries where the firewall blocked at least one flow.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max summaries to return."), ShowDefault: true, Default: 20}, Suffix: "  [default: 20; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "offset", Kind: "option", Opts: []string{"--offset"}, Type: "integer range", Nargs: 1, Help: descriptionText("Number of summaries to skip."), ShowDefault: true, Default: 0}, Suffix: "  [default: 0; x>=0]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "start_after", Kind: "option", Opts: []string{"--from", "--start-after"}, Type: "text", Nargs: 1, Help: descriptionText("Only include summaries that started at or after this ISO-8601 timestamp.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "end_before", Kind: "option", Opts: []string{"--to", "--end-before"}, Type: "text", Nargs: 1, Help: descriptionText("Only include summaries that ended at or before this ISO-8601 timestamp.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit raw JSON instead of a table.")}, CompletionKind: "Bool"},
			},
		}
	case "avr firewall list":
		return docgen.Description{
			Help:           "List egress firewall rules for the given scope.",
			Short:          "List egress firewall rules for the given scope.",
			CompletionHelp: "List egress firewall rules for the given...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository ID or owner/repo. If provided, shows the repo-level list.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Output rules as JSON instead of a table.")}, CompletionKind: "Bool"},
			},
		}
	case "avr firewall move":
		return docgen.Description{
			Help:           "Move a rule to a new position (rewrites the full ordering atomically).",
			Short:          "Move a rule to a new position (rewrites the full ordering atomically).",
			CompletionHelp: "Move a rule to a new position (rewrites...",
			Usage:          []string{"[OPTIONS]", "RULE_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "rule_id", Kind: "argument", Opts: []string{"rule_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "to_position", Kind: "option", Opts: []string{"--to"}, Type: "integer", Required: true, Nargs: 1, Help: descriptionText("Target 0-indexed position.")}, Suffix: "  [required]", CompletionKind: "Int"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository ID or owner/repo. If provided, moves a repo-level rule.")}, CompletionKind: "String"},
			},
		}
	case "avr firewall set-default":
		return docgen.Description{
			Help:           "Set the default action, or use --inherit with --repo to restore organization inheritance.",
			Short:          "Set the default action, or use --inherit with --repo to restore organization inheritance.",
			CompletionHelp: "Set the default action or restore inheritance.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository ID or owner/repo. If provided, sets the repo-level default.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "action", Kind: "option", Opts: []string{"--action"}, Type: "choice", Nargs: 1, Choices: []string{"allow", "deny"}}, CompletionKind: "Choice", CaseSensitive: true},
			},
		}
	case "avr firewall show":
		return docgen.Description{
			Help:           "Show rules in evaluation order with repository/organization ownership. Built-in service exceptions are not listed. Running jobs retain their dispatched policy.",
			Short:          "Show rules in evaluation order and their source.",
			CompletionHelp: "Show the resolved (merged) firewall rule...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("Repository ID or owner/repo.")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Output resolved rules as JSON instead of a table.")}, CompletionKind: "Bool"},
			},
		}
	case "avr firewall":
		return docgen.Description{
			Help:           "Manage the egress firewall rule list for orgs and repositories.",
			Short:          "Manage the egress firewall rule list for orgs and repositories.",
			CompletionHelp: "Manage the egress firewall rule list for...",
			Usage:          []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Commands: []docgen.HelpCommand{
				{Name: "add", Description: "Add a rule."},
				{Name: "delete", Description: "Delete a rule by ID."},
				{Name: "edit", Description: "Edit specified fields of an existing rule."},
				{Name: "export", Description: "Export rules as portable JSON."},
				{Name: "import", Description: "Append or replace rules from JSON."},
				{Name: "flow-summaries", Description: "Show per-VM network activity summaries captured at VM..."},
				{Name: "list", Description: "List egress firewall rules for the given scope."},
				{Name: "move", Description: "Move a rule to a new position (rewrites the full..."},
				{Name: "set-default", Description: "Set the default action or restore inheritance."},
				{Name: "show", Description: "Show rules in evaluation order and their source."},
			},
		}
	}
	return docgen.Description{}
}
