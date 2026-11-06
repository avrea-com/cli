// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func tokenDescription(path string) docgen.Description {
	switch path {
	case "avr token create":
		return docgen.Description{
			Help: "Create a scoped token and print its credential.\n" +
				"\n" +
				"The credential is shown once and cannot be retrieved later. A token needs\n" +
				"at least one of --repo, --vm or --allow-vm-create. Repository grants are\n" +
				"read-only and VM grants are admin; an optional level suffix\n" +
				"(--repo acme/api:read, --vm cvm-abc123:admin) must name that level.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr token create --name ci-read --repo acme/api --repo acme/web\n" +
				"    avr token create --name agent --vm cvm-abc123 --ttl 30m\n" +
				"    avr token create --name sandbox --allow-vm-create --vm-create-limit 3\n" +
				"    avr token create --name ci-read --repo acme/api --json token --jq .token\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    allow_vm_create, created_at, expires_at, grants, id, last_used_at,\n" +
				"    name, organization_id, revoked_at, revoked_reason, token, user_id,\n" +
				"    vm_create_count, vm_create_limit\n",
			Short:          "Create a scoped token and print its credential.",
			CompletionHelp: "Create a scoped token and print its...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "name", Kind: "option", Opts: []string{"--name"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("Token name (1-100 characters).")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "repos", Kind: "option", Opts: []string{"--repo"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Grant read access to a repository (org/repo or rep-xxx). Repeatable.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "vms", Kind: "option", Opts: []string{"--vm"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Grant admin access to a VM, by VM ID. Repeatable.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "allow_vm_create", Kind: "option", Opts: []string{"--allow-vm-create"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Let the token create VMs.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "vm_create_limit", Kind: "option", Opts: []string{"--vm-create-limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max VMs the token may create; needs --allow-vm-create. The server default is 1.")}, Suffix: "  [1<=x<=10]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "ttl", Kind: "option", Opts: []string{"--ttl"}, Type: "text", Nargs: 1, Help: descriptionText("Lifetime: e.g. 30m, 8h, 7d, or a number of seconds. 60 seconds to 7 days; the server default is 8 hours.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr token list":
		return docgen.Description{
			Help: "List live tokens, newest first.\n" +
				"\n" +
				"Shows your own tokens, or every member's when you are an organization\n" +
				"admin. Revoked and expired tokens are not listed; `avr token view` still\n" +
				"reads them by ID.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr token list\n" +
				"    avr token list --json id,name,expires_at\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    allow_vm_create, created_at, expires_at, grants, id, last_used_at,\n" +
				"    name, organization_id, revoked_at, revoked_reason, user_id,\n" +
				"    vm_create_count, vm_create_limit\n",
			Short: "List live tokens, newest first.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max tokens to return."), ShowDefault: true, Default: 50}, Suffix: "  [default: 50; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr token revoke":
		return docgen.Description{
			Help: "Revoke a token.\n" +
				"\n" +
				"Requests made with the token are refused from then on. Revoking a token\n" +
				"that is already revoked succeeds.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr token revoke <token-id>\n" +
				"    avr token revoke <token-id> --yes\n",
			Short: "Revoke a token.",
			Usage: []string{"[OPTIONS]", "TOKEN_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "token_id", Kind: "argument", Opts: []string{"token_id"}, Type: "_token_id", Required: true, Nargs: 1}, CompletionKind: "Func"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip the confirmation prompt.")}, CompletionKind: "Bool"},
			},
		}
	case "avr token view":
		return docgen.Description{
			Help: "Show a token's details and grants, including a revoked or expired one.\n" +
				"\n" +
				"The credential itself is never shown again after `avr token create`.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr token view <token-id>\n" +
				"    avr token view <token-id> --json grants\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    allow_vm_create, created_at, expires_at, grants, id, last_used_at,\n" +
				"    name, organization_id, revoked_at, revoked_reason, user_id,\n" +
				"    vm_create_count, vm_create_limit\n",
			Short:          "Show a token's details and grants, including a revoked or expired one.",
			CompletionHelp: "Show a token's details and grants,...",
			Usage:          []string{"[OPTIONS]", "TOKEN_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "token_id", Kind: "argument", Opts: []string{"token_id"}, Type: "_token_id", Required: true, Nargs: 1}, CompletionKind: "Func"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr token":
		return docgen.Description{
			Help: "Create and manage scoped access tokens.\n" +
				"\n" +
				"A scoped access token is a credential bound to one organization that\n" +
				"expires within seven days. It reaches only the repositories and VMs it\n" +
				"names, and it cannot create further tokens. To use one, set AVR_TOKEN to\n" +
				"the credential and AVR_ORG to the organization ID.\n",
			Short:    "Create and manage scoped access tokens.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "create", Description: "Create a scoped token and print its credential.", AutoShort: true},
				{Name: "list", Description: "List live tokens, newest first.", AutoShort: true},
				{Name: "revoke", Description: "Revoke a token.", AutoShort: true},
				{Name: "view", Description: "Show a token's details and grants, including a revoked or expired one.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
