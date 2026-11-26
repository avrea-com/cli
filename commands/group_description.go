// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import (
	"strings"

	"github.com/avrea-com/cli/docgen"
)

// groupNaming explains GROUP and the other options that name a group.
const groupNaming = "GROUP is a group ID (grp-...), the group's path from the root group,\n" +
	"such as Organization/Platform/CI, or the end of a path when only one\n" +
	"group has it, such as Platform/CI.\n"

func groupParameters(extra ...docgen.Parameter) []docgen.Parameter {
	return append(append([]docgen.Parameter{serviceTokenOrgParameter()}, extra...), serviceTokenJSONParameters()...)
}

func groupTextOption(name, label, help string) docgen.Parameter {
	return docgen.Parameter{Param: docgen.Param{Name: strings.ReplaceAll(name, "-", "_"), Kind: "option", Opts: []string{"--" + name}, Type: "text", Nargs: 1, Help: descriptionText(help)}, Label: "--" + name + " " + label, CompletionKind: "String"}
}

func groupRequired(p docgen.Parameter) docgen.Parameter {
	p.Required = true
	return p
}

func groupYesOption(help string) docgen.Parameter {
	return docgen.Parameter{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"-y", "--yes"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText(help)}, CompletionKind: "Bool"}
}

func groupDescription(path string) docgen.Description {
	switch path {
	case "avr group list":
		return docgen.Description{
			Help: "List the organization's groups by path.\n" +
				"\n" +
				"A group's path is the display names from the root group down, joined\n" +
				"with /, such as Organization/Platform/CI; a parent is listed before its\n" +
				"children. Pass a group's ID or path to --group when you create or list\n" +
				"service tokens.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr group list\n" +
				"    avr group list --json group_id,path --jq '.[].path'\n" +
				"\n" +
				jsonFieldsHelp(groupFields),
			Short: "List the organization's groups by path.",
			Parameters: groupParameters(
				docgen.Parameter{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max groups to return."), ShowDefault: true, Default: 50}, Suffix: "  [default: 50; x>=1]", CompletionKind: "IntRange"},
			),
		}
	case "avr group view":
		return docgen.Description{
			Help: "Show a group's details and path.\n" +
				"\n" +
				groupNaming +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr group view Organization/Platform\n" +
				"    avr group view Platform/CI --json group_id,member_count\n" +
				"\n" +
				jsonFieldsHelp(groupFields),
			Short:      "Show a group's details and path.",
			Parameters: groupParameters(),
		}
	case "avr group create":
		return docgen.Description{
			Help: "Create a group below a parent group.\n" +
				"\n" +
				"NAME is the new group's name, unique below its parent. --parent names\n" +
				"the parent like GROUP in other group commands; the root group,\n" +
				"Organization unless renamed, takes groups at the top of the tree.\n" +
				"\n" +
				"Each create sends an idempotency key, printed to standard error before\n" +
				"the request. If the response is lost, rerun the same command with\n" +
				"--idempotency-key KEY: it returns the group that create made instead of\n" +
				"making a second one. A key belongs to one parent, name and label.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr group create Platform --parent Organization\n" +
				"    avr group create CI --parent Organization/Platform --label runners\n" +
				"\n" +
				jsonFieldsHelp(groupCreateFields),
			Short: "Create a group below a parent group.",
			Parameters: groupParameters(
				groupRequired(groupTextOption("parent", "GROUP", "The group to create it below.")),
				groupTextOption("label", "TEXT", "A short label shown with the group."),
				docgen.Parameter{Param: docgen.Param{Name: "idempotency_key", Kind: "option", Opts: []string{"--idempotency-key"}, Type: "uuid", Nargs: 1, Help: descriptionText("Resume an earlier create with the key it printed.")}, CompletionKind: "UUID"},
			),
		}
	case "avr group edit":
		return docgen.Description{
			Help: "Rename a group or change its label.\n" +
				"\n" +
				groupNaming +
				"\n" +
				"Only the options you pass change; --label '' clears the label.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr group edit Platform/CI --name Builds\n" +
				"    avr group edit Platform/CI --label ''\n" +
				"\n" +
				jsonFieldsHelp(groupRecordFields),
			Short: "Rename a group or change its label.",
			Parameters: groupParameters(
				groupTextOption("name", "TEXT", "The group's new name."),
				groupTextOption("label", "TEXT", "The group's new label; an empty value clears it."),
			),
		}
	case "avr group delete":
		return docgen.Description{
			Help: "Delete an empty group.\n" +
				"\n" +
				groupNaming +
				"\n" +
				"Asks first unless --yes is passed. A group that still has subgroups,\n" +
				"members, a service token that is not revoked or expired, VMs its\n" +
				"service tokens created, a vCPU limit, suspended members or workflows\n" +
				"whose jobs are charged to it is not deleted; the error names what to\n" +
				"clear, at most 100 workflows at a time.\n" +
				"\n" +
				"Deleting a group also removes the resource access it grants, and stops\n" +
				"charging it for workflows of repositories that are no longer installed.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr group delete Platform/CI\n" +
				"    avr group delete grp-0123456789abcdef0123456789abcdef --yes\n" +
				"\n" +
				jsonFieldsHelp(groupDeleteFields),
			Short: "Delete an empty group.",
			Parameters: groupParameters(
				groupYesOption("Delete without asking."),
			),
		}
	case "avr group member":
		return docgen.Description{
			Help: "List, add and remove a group's members.\n" +
				"\n" +
				"A change names each MEMBER by user ID (usr-...), @me for yourself, or an\n" +
				"email address the member verified. Display names are not accepted:\n" +
				"members choose them, so one can match the wrong person. `avr auth\n" +
				"status` shows your user ID.\n",
			Short:    "List, add and remove a group's members.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "add", Description: "Add members to a group.", AutoShort: true},
				{Name: "list", Description: "List a group's direct members.", AutoShort: true},
				{Name: "remove", Description: "Remove members from a group.", AutoShort: true},
			},
		}
	case "avr group member list":
		return docgen.Description{
			Help: "List a group's direct members.\n" +
				"\n" +
				groupNaming +
				"\n" +
				"Members of subgroups are listed with the subgroup.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr group member list Platform/CI\n" +
				"    avr group member list Platform/CI -L 1000 --json user_id,email\n" +
				"\n" +
				jsonFieldsHelp(groupMemberFields),
			Short: "List a group's direct members.",
			Parameters: groupParameters(
				docgen.Parameter{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max members to return."), ShowDefault: true, Default: 100}, Suffix: "  [default: 100; x>=1]", CompletionKind: "IntRange"},
			),
		}
	case "avr group member add", "avr group member remove":
		verb, short, extra := "adds", "Add members to a group.", ""
		if path == "avr group member remove" {
			verb, short, extra = "removes", "Remove members from a group.", "Lists the members and asks first unless --yes is passed; reading\n"+
				"members from standard input (--file -) needs --yes.\n"
		}
		command := path[len("avr "):]
		return docgen.Description{
			Help: short + "\n" +
				"\n" +
				groupNaming +
				"\n" +
				"Name each MEMBER by user ID (usr-...), @me or a verified email address,\n" +
				"as arguments or one per line with --file, where a line starting with #\n" +
				"is a comment unless it is an email address, such as #ops@example.com.\n" +
				"The command " + verb + " them all in one change, up to 250, and reports\n" +
				"each member's result; a member already in the state asked for counts\n" +
				"as done, so a rerun is safe.\n" +
				extra +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr " + command + " Platform/CI @me\n" +
				"    avr " + command + " Platform/CI usr-0123456789abcdef0123456789abcdef\n" +
				"    avr " + command + " Platform/CI --file members.txt\n" +
				"\n" +
				jsonFieldsHelp(groupMemberResultFields),
			Short:      short,
			Parameters: groupParameters(groupMemberOptions(path == "avr group member remove")...),
		}
	case "avr group limit":
		return docgen.Description{
			Help: "Show, set and clear a group's vCPU limit.\n" +
				"\n" +
				"A group's vCPU limit caps the vCPU that the group and its subgroups use\n" +
				"at once. Limits on parent groups apply too: a job starts only when it\n" +
				"fits under every limit on its group's path, counting what the other\n" +
				"groups below each one already use.\n",
			Short:    "Show, set and clear a group's vCPU limit.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "clear", Description: "Remove a group's own vCPU limit.", AutoShort: true},
				{Name: "list", Description: "Show the limits that apply to a group, its own and inherited.", AutoShort: true},
				{Name: "set", Description: "Set a group's own vCPU limit.", AutoShort: true},
			},
		}
	case "avr group limit list":
		return docgen.Description{
			Help: "Show the limits that apply to a group, its own and inherited.\n" +
				"\n" +
				groupNaming +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr group limit list Platform/CI\n" +
				"\n" +
				jsonFieldsHelp(groupLimitFields),
			Short:      "Show the limits that apply to a group, its own and inherited.",
			Parameters: groupParameters(),
		}
	case "avr group limit set":
		return docgen.Description{
			Help: "Set a group's own vCPU limit.\n" +
				"\n" +
				groupNaming +
				"\n" +
				"Asks first unless --yes is passed or the change raises or keeps the\n" +
				"group's own limit: a first limit or a lower one can make new jobs wait.\n" +
				"Running jobs keep running.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr group limit set Platform/CI --max-vcpu 64\n" +
				"\n" +
				jsonFieldsHelp(groupLimitSetFields),
			Short: "Set a group's own vCPU limit.",
			Parameters: groupParameters(
				docgen.Parameter{Param: docgen.Param{Name: "max_vcpu", Kind: "option", Opts: []string{"--max-vcpu"}, Type: "integer", Nargs: 1, Required: true, Help: descriptionText("The most vCPU the group and its subgroups may use at once.")}, CompletionKind: "Int"},
				groupYesOption("Set the limit without asking."),
			),
		}
	case "avr group limit clear":
		return docgen.Description{
			Help: "Remove a group's own vCPU limit.\n" +
				"\n" +
				groupNaming +
				"\n" +
				"Asks first unless --yes is passed. Limits on parent groups still apply.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr group limit clear Platform/CI --yes\n" +
				"\n" +
				jsonFieldsHelp(groupLimitClearFields),
			Short: "Remove a group's own vCPU limit.",
			Parameters: groupParameters(
				groupYesOption("Clear without asking."),
			),
		}
	case "avr group usage":
		return docgen.Description{
			Help: "Show the vCPU a group uses and the limits on its path.\n" +
				"\n" +
				groupNaming +
				"\n" +
				"Usage counts the group and its subgroups.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr group usage Platform\n" +
				"    avr group usage Platform --json used_vcpu,pools\n" +
				"\n" +
				jsonFieldsHelp(groupUsageFields),
			Short:      "Show the vCPU a group uses and the limits on its path.",
			Parameters: groupParameters(),
		}
	case "avr group":
		return docgen.Description{
			Help: "Build and manage the organization's group tree.\n" +
				"\n" +
				"Groups hold members, service tokens and vCPU limits. Organization\n" +
				"members manage them while signed in (avr auth device-login or avr auth\n" +
				"login); a personal access token or service token cannot. What you may\n" +
				"change depends on the groups you administer: `avr org governance` shows\n" +
				"it.\n",
			Short:    "Build and manage the organization's group tree.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "create", Description: "Create a group below a parent group.", AutoShort: true},
				{Name: "delete", Description: "Delete an empty group.", AutoShort: true},
				{Name: "edit", Description: "Rename a group or change its label.", AutoShort: true},
				{Name: "limit", Description: "Show, set and clear a group's vCPU limit.", AutoShort: true},
				{Name: "list", Description: "List the organization's groups by path.", AutoShort: true},
				{Name: "member", Description: "List, add and remove a group's members.", AutoShort: true},
				{Name: "usage", Description: "Show the vCPU a group uses and the limits on its path.", AutoShort: true},
				{Name: "view", Description: "Show a group's details and path.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}

func groupMemberOptions(remove bool) []docgen.Parameter {
	options := []docgen.Parameter{{Param: docgen.Param{Name: "file", Kind: "option", Opts: []string{"--file"}, Type: "text", Nargs: 1, Help: descriptionText("Read members from a file, one per line; - reads standard input.")}, Label: "--file FILE", CompletionKind: "Path", FileOkay: true}}
	if remove {
		options = append(options, groupYesOption("Remove without asking."))
	}
	return options
}
