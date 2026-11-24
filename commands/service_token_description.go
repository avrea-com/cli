// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import (
	"fmt"

	"github.com/avrea-com/cli/docgen"
)

const serviceTokenJSONFields = "    blocked_by, created_at, created_by, expires_at, expiring_soon, grants,\n" +
	"    grants_version, group_id, id, last_used_at, name, organization_id,\n" +
	"    previous_valid_until, revoked_at, revoked_reason, rotated_at,\n" +
	"    rotation_version, status, vm_cap, vm_count"

// groupOptionHelp states how --group selects a group.
const groupOptionHelp = "--group takes a group ID or a group's path as `avr group list` shows it,\n" +
	"such as Organization/Platform/CI. Names compare without case. A value equal to\n" +
	"one group's whole path selects that group; otherwise the end of a path,\n" +
	"such as Platform/CI or a group name like CI, must select exactly one\n" +
	"group.\n"

// serviceTokenSecretJSONHelp states the --json contract of the commands that
// return a secret once.
const serviceTokenSecretJSONHelp = "With --json, the selection must include the token field, because the\n" +
	"secret cannot be read again. If a --jq filter fails or drops the\n" +
	"secret, the command prints the unfiltered JSON instead and exits\n" +
	"nonzero, so the secret is not lost.\n"

func serviceTokenOrgParameter() docgen.Parameter {
	return docgen.Parameter{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"}
}

func serviceTokenJSONParameters() []docgen.Parameter {
	return []docgen.Parameter{
		{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
		{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
	}
}

func serviceTokenIDParameter() docgen.Parameter {
	return docgen.Parameter{Param: docgen.Param{Name: "token_id", Kind: "argument", Opts: []string{"token_id"}, Type: "_token_id", Required: true, Nargs: 1}, CompletionKind: "Func"}
}

// serviceTokenGrantParameters describes the selection options; replace adds
// the ones only set-access takes.
func serviceTokenGrantParameters(replace bool) []docgen.Parameter {
	limit := "Let the token create VMs and own up to this many at once; deleting a VM frees a place. A token of the root group cannot create VMs."
	if replace {
		limit += " Lowering it keeps the VMs the token owns. Omit to keep the current maximum."
	}
	parameters := []docgen.Parameter{
		{Param: docgen.Param{Name: "repos", Kind: "option", Opts: []string{"--repo"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Grant read access to a repository (org/repo or rep-xxx). Repeatable.")}, CompletionKind: "String"},
		{Param: docgen.Param{Name: "firewall_rules", Kind: "option", Opts: []string{"--firewall-rule"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Grant read access to an egress firewall rule, by rule ID (efr-xxx). Repeatable.")}, CompletionKind: "String"},
		{Param: docgen.Param{Name: "all_firewall_rules", Kind: "option", Opts: []string{"--all-firewall-rules"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Grant read access to every egress firewall rule, including rules added later.")}, CompletionKind: "Bool"},
		{Param: docgen.Param{Name: "vms", Kind: "option", Opts: []string{"--vm"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Grant access to a VM, by VM ID (cvm-xxx): read, or write with cvm-xxx:write. Repeatable.")}, CompletionKind: "String"},
		{Param: docgen.Param{Name: "all_vms", Kind: "option", Opts: []string{"--all-vms"}, Type: "choice", Nargs: 1, Help: descriptionText("Grant read or write access to every VM of the organization, including VMs created later."), Choices: serviceTokenLevels}, CompletionKind: "Choice", CaseSensitive: true},
		{Param: docgen.Param{Name: "max_owned_vms", Kind: "option", Opts: []string{"--max-owned-vms"}, Type: "integer range", Nargs: 1, Help: descriptionText(limit)}, Suffix: fmt.Sprintf("  [1<=x<=%d]", serviceTokenMaxVMCap), CompletionKind: "IntRange"},
	}
	if replace {
		parameters = append(parameters, docgen.Parameter{Param: docgen.Param{Name: "no_vm_create", Kind: "option", Opts: []string{"--no-vm-create"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Stop the token creating VMs; its grants stay unless grant options are given. It keeps administering the VMs it created.")}, CompletionKind: "Bool"})
	}
	return parameters
}

func serviceTokenLifetimeParameters() []docgen.Parameter {
	return []docgen.Parameter{
		{Param: docgen.Param{Name: "ttl", Kind: "option", Opts: []string{"--ttl"}, Type: "text", Nargs: 1, Help: descriptionText("Lifetime: e.g. 30m, 8h, 7d, or a number of seconds. 60 seconds to 90 days; the server default is 30 days.")}, CompletionKind: "String"},
		{Param: docgen.Param{Name: "never_expires", Kind: "option", Opts: []string{"--never-expires"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("The secret never expires: anyone holding it keeps its access until it is revoked. Cannot be combined with --ttl.")}, CompletionKind: "Bool"},
	}
}

func serviceTokenYesParameter(help string) docgen.Parameter {
	return docgen.Parameter{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText(help)}, CompletionKind: "Bool"}
}

func serviceTokenDescription(path string) docgen.Description {
	switch path {
	case "avr service-token create":
		return docgen.Description{
			Help: "Create a service token and print its secret once.\n" +
				"\n" +
				"The token belongs to the group --group names, not to you, and keeps\n" +
				"working when you leave the organization. It needs at least one grant\n" +
				"or --max-owned-vms. Repository and firewall-rule grants are reads; a\n" +
				"VM grant is read or write. Each grant must be within your own access\n" +
				"now. With --max-owned-vms the token creates VMs and administers the\n" +
				"ones it created; a token of the root group cannot create VMs. The\n" +
				"secret is shown once and cannot be retrieved later: store it, for\n" +
				"example as a CI secret, and set it as AVR_TOKEN where the token is used.\n" +
				"The API may ask for a recent sign-in; the command then exits 4 and says\n" +
				"to sign in again.\n" +
				"\n" +
				groupOptionHelp +
				"\n" +
				serviceTokenSecretJSONHelp +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr service-token create --name ci --group Platform/CI --repo acme/api\n" +
				"    avr service-token create --name egress --group CI --all-firewall-rules --ttl 7d\n" +
				"    avr service-token create --name agents --group Platform/CI --repo acme/api --max-owned-vms 5\n" +
				"    avr service-token create --name ci --group <group-id> --repo acme/api --json token --jq .token\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				serviceTokenJSONFields + ", token\n",
			Short:          "Create a service token and print its secret once.",
			CompletionHelp: "Create a service token and print its...",
			Parameters: append(append(append([]docgen.Parameter{
				{Param: docgen.Param{Name: "name", Kind: "option", Opts: []string{"--name"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("Token name (1-100 characters).")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "group_id", Kind: "option", Opts: []string{"--group"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("The group that owns the token: its name, path or ID (grp-xxx).")}, Suffix: "  [required]", CompletionKind: "String"},
			}, serviceTokenGrantParameters(false)...), serviceTokenLifetimeParameters()...),
				append([]docgen.Parameter{serviceTokenOrgParameter()}, serviceTokenJSONParameters()...)...),
		}
	case "avr service-token list":
		return docgen.Description{
			Help: "List the organization's service tokens, soonest expiry first.\n" +
				"\n" +
				"Lists live, expired and revoked tokens; tokens that never expire come\n" +
				"last. A live token whose requests are refused all the same, for example\n" +
				"while its group is suspended, shows as blocked with the reason.\n" +
				"\n" +
				groupOptionHelp +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr service-token list\n" +
				"    avr service-token list --status live --group Platform/CI\n" +
				"    avr service-token list --json id,name,expires_at\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				serviceTokenJSONFields + "\n",
			Short:          "List the organization's service tokens, soonest expiry first.",
			CompletionHelp: "List the organization's service tokens,...",
			Parameters: append([]docgen.Parameter{
				serviceTokenOrgParameter(),
				{Param: docgen.Param{Name: "group_id", Kind: "option", Opts: []string{"--group"}, Type: "text", Nargs: 1, Help: descriptionText("Only the tokens this group owns, by group name, path or ID (grp-xxx).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "status", Kind: "option", Opts: []string{"--status"}, Type: "choice", Nargs: 1, Help: descriptionText("Only tokens with this status."), Choices: []string{"live", "expired", "revoked"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max tokens to return."), ShowDefault: true, Default: 50}, Suffix: "  [default: 50; 1<=x<=1000]", CompletionKind: "IntRange"},
			}, serviceTokenJSONParameters()...),
		}
	case "avr service-token view":
		return docgen.Description{
			Help: "Show a service token's details, versions and grants.\n" +
				"\n" +
				"Reads a revoked or expired token as well. Its grants are removed when it\n" +
				"is revoked and 30 days after it expires. The secret itself is never\n" +
				"shown again after `avr service-token create` or `rotate`.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr service-token view <token-id>\n" +
				"    avr service-token view <token-id> --json grants\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				serviceTokenJSONFields + "\n",
			Short:          "Show a service token's details, versions and grants.",
			CompletionHelp: "Show a service token's details,...",
			Usage:          []string{"[OPTIONS]", "TOKEN_ID"},
			Parameters:     append([]docgen.Parameter{serviceTokenIDParameter(), serviceTokenOrgParameter()}, serviceTokenJSONParameters()...),
		}
	case "avr service-token set-access":
		return docgen.Description{
			Help: "Replace a service token's grants or change its VM creation.\n" +
				"\n" +
				"Grant options (--repo, --firewall-rule, --all-firewall-rules, --vm and\n" +
				"--all-vms) replace every grant the token holds: a grant you leave out\n" +
				"is removed, so repeat the ones to keep. Without grant options the\n" +
				"grants stay. The maximum of owned VMs stays unless --max-owned-vms or\n" +
				"--no-vm-create changes it. When the change removes or lowers a grant\n" +
				"or VM creation, the command lists what goes and asks first; without a\n" +
				"terminal it then refuses unless --yes is given. It reads the token first and the change fails if its access\n" +
				"changes in between. A grant that widens its access must be within your\n" +
				"own access now. The API may ask for a recent sign-in; the command then\n" +
				"exits 4 and says to sign in again. The change applies to the token's\n" +
				"next request.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr service-token set-access <token-id> --repo acme/api --repo acme/web\n" +
				"    avr service-token set-access <token-id> --all-firewall-rules --yes\n" +
				"    avr service-token set-access <token-id> --max-owned-vms 10\n" +
				"    avr service-token set-access <token-id> --no-vm-create\n",
			Short:          "Replace a service token's grants or change its VM creation.",
			CompletionHelp: "Replace a service token's grants or...",
			Usage:          []string{"[OPTIONS]", "TOKEN_ID"},
			Parameters: append(append([]docgen.Parameter{serviceTokenIDParameter()}, serviceTokenGrantParameters(true)...), serviceTokenOrgParameter(),
				serviceTokenYesParameter("Apply the change without asking, even when it removes or lowers access.")),
		}
	case "avr service-token rotate":
		return docgen.Description{
			Help: "Issue a service token a new secret and print it once.\n" +
				"\n" +
				"The new secret gets its own lifetime. The old secret keeps working for\n" +
				"the overlap, never past the expiry of either secret, so pipelines can\n" +
				"switch without a gap. Rotating again inside the overlap retires the old\n" +
				"secret at once; run from a terminal, the command asks first unless --yes\n" +
				"is given. The API may ask for a recent sign-in; the command then exits 4\n" +
				"and says to sign in again.\n" +
				"\n" +
				serviceTokenSecretJSONHelp +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr service-token rotate <token-id>\n" +
				"    avr service-token rotate <token-id> --ttl 30d --overlap 2h\n" +
				"    avr service-token rotate <token-id> --json token --jq .token\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				serviceTokenJSONFields + ", token\n",
			Short:          "Issue a service token a new secret and print it once.",
			CompletionHelp: "Issue a service token a new secret and...",
			Usage:          []string{"[OPTIONS]", "TOKEN_ID"},
			Parameters: append(append(append([]docgen.Parameter{serviceTokenIDParameter()}, serviceTokenLifetimeParameters()...),
				docgen.Parameter{Param: docgen.Param{Name: "overlap", Kind: "option", Opts: []string{"--overlap"}, Type: "text", Nargs: 1, Help: descriptionText("How long the old secret keeps working: e.g. 2h, 3d, or a number of seconds. 1 hour to 7 days; the server default is 24 hours.")}, CompletionKind: "String"},
				serviceTokenOrgParameter(), serviceTokenYesParameter("Skip the confirmation prompt.")), serviceTokenJSONParameters()...),
		}
	case "avr service-token revoke":
		return docgen.Description{
			Help: "Revoke a service token.\n" +
				"\n" +
				"Its current secret and any old secret still in its rotation overlap stop\n" +
				"working on their next request, its grants are removed, and the VMs it\n" +
				"created are deleted. Revoking a token that is already revoked succeeds.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr service-token revoke <token-id>\n" +
				"    avr service-token revoke <token-id> --yes\n",
			Short:      "Revoke a service token.",
			Usage:      []string{"[OPTIONS]", "TOKEN_ID"},
			Parameters: []docgen.Parameter{serviceTokenIDParameter(), serviceTokenOrgParameter(), serviceTokenYesParameter("Skip the confirmation prompt.")},
		}
	case "avr service-token revoke-all":
		return docgen.Description{
			Help: "Revoke every service token of the organization.\n" +
				"\n" +
				"For a suspected leak: every unrevoked service token of the organization,\n" +
				"expired ones included, stops working on its next request, and the VMs\n" +
				"those tokens created are deleted.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr service-token revoke-all --org <org>\n" +
				"    avr service-token revoke-all --org <org> --yes\n",
			Short:      "Revoke every service token of the organization.",
			Parameters: []docgen.Parameter{serviceTokenOrgParameter(), serviceTokenYesParameter("Skip the confirmation prompt.")},
		}
	case "avr service-token":
		return docgen.Description{
			Help: "Create and manage the organization's service tokens.\n" +
				"\n" +
				"A service token (avt_...) belongs to a group of one organization rather\n" +
				"than to a person, and keeps working when its creator leaves. It reads\n" +
				"only the repositories and egress firewall rules its access names.\n" +
				"Organization admins manage service tokens while signed in (avr auth\n" +
				"device-login or avr auth login); a scoped or service token cannot.\n" +
				"\n" +
				"A service token reaches the VMs its grants name. With --max-owned-vms it\n" +
				"creates VMs, and it administers those VMs even after VM creation is\n" +
				"stopped. It runs avr vm create without --repo or --egress-rules, list,\n" +
				"show, ssh, ssh-config, metrics without --watch, update with --name or\n" +
				"--ttl, rotate-https-label, pause and delete; resume and fork are\n" +
				"refused. avr vm capabilities needs a VM grant or --max-owned-vms.\n" +
				"Revoking the token deletes the VMs it created.\n" +
				"\n" +
				"To use a service token, set AVR_TOKEN to its secret. Its organization\n" +
				"applies automatically; set AVR_ORG to the org-... ID in loops to save a\n" +
				"request per command. By default, a service token has 60 requests per\n" +
				"minute, or 31 per VM it may own when that is more, up to 600. Commands\n" +
				"that need a person, such as avr feedback, refuse it, and avr auth logout\n" +
				"does not revoke it: unset AVR_TOKEN instead. After a suspected leak,\n" +
				"revoke-all revokes every service token of the organization.\n",
			Short:    "Create and manage the organization's service tokens.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "create", Description: "Create a service token and print its secret once.", AutoShort: true},
				{Name: "list", Description: "List the organization's service tokens, soonest expiry first.", AutoShort: true},
				{Name: "revoke", Description: "Revoke a service token.", AutoShort: true},
				{Name: "revoke-all", Description: "Revoke every service token of the organization.", AutoShort: true},
				{Name: "rotate", Description: "Issue a service token a new secret and print it once.", AutoShort: true},
				{Name: "set-access", Description: "Replace a service token's whole access selection.", AutoShort: true},
				{Name: "view", Description: "Show a service token's details, versions and grants.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
