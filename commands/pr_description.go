// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func prDescription(path string) docgen.Description {
	switch path {
	case "avr pr list":
		return docgen.Description{
			Help: "List pull requests across repositories.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr pr list\n" +
				"    avr pr list --scope authored\n" +
				"    avr pr list --repo acme/widgets --state merged\n" +
				"    avr pr list --json number,title,mergeability\n" +
				"    avr pr list --json '?'           # list available fields\n" +
				"    avr pr list --json '*'           # all fields\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    author_login, base_ref, base_sha, check_status, comment_count, created_at,\n" +
				"    draft, head_ref, head_sha, mergeability, merged, number, repository_full_name,\n" +
				"    repository_id, state, title, unresolved_thread_count, updated_at\n",
			Short: "List pull requests across repositories.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repository_ids", Kind: "option", Opts: []string{"--repo"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Filter by repository (org/repo or rep-xxx). Pass --repo more than once to filter multiple repositories. Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "scope", Kind: "option", Opts: []string{"--scope"}, Type: "choice", Nargs: 1, Help: descriptionText("List all, authored, involved, rest, or attention PRs. Attention requires open state."), ShowDefault: true, Default: "all", Choices: []string{"all", "authored", "involved", "rest", "attention"}}, Suffix: "  [default: all]", CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "state", Kind: "option", Opts: []string{"--state"}, Type: "choice", Nargs: 1, Help: descriptionText("Filter by pull request state. 'all' removes the state filter."), ShowDefault: true, Default: "open", Choices: []string{"open", "closed", "merged", "all"}}, Suffix: "  [default: open]", CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max PRs to return."), ShowDefault: true, Default: 20}, Suffix: "  [default: 20; 1<=x<=200]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Pagination cursor from a previous response.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr pr":
		return docgen.Description{
			Help:     "View pull requests.",
			Short:    "View pull requests.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "list", Description: "List pull requests across repositories.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
