// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func groupDescription(path string) docgen.Description {
	switch path {
	case "avr group list":
		return docgen.Description{
			Help: "List the organization's groups by path.\n" +
				"\n" +
				"A group's path is the display names from the root group down, joined\n" +
				"with /, such as Acme/Platform/CI; a parent is listed before its\n" +
				"children. Pass a group's ID or path to --group when you create or list\n" +
				"service tokens.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr group list\n" +
				"    avr group list --json group_id,path --jq '.[].path'\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    created_at, display_name, group_id, label, member_count, ownership,\n" +
				"    parent_id, path, soft_deleted_at, state, suspension_reason, updated_at\n",
			Short: "List the organization's groups by path.",
			Parameters: append([]docgen.Parameter{
				serviceTokenOrgParameter(),
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max groups to return."), ShowDefault: true, Default: 50}, Suffix: "  [default: 50; 1<=x<=1000]", CompletionKind: "IntRange"},
			}, serviceTokenJSONParameters()...),
		}
	case "avr group":
		return docgen.Description{
			Help: "View the organization's groups.\n" +
				"\n" +
				"Each service token belongs to one of these groups. Organization members\n" +
				"list them while signed in with `avr auth login`; a scoped or service\n" +
				"token cannot.\n",
			Short:    "View the organization's groups.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "list", Description: "List the organization's groups by path.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
