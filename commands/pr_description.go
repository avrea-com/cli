// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func prDescription(path string) docgen.Description {
	switch path {
	case "avr pr comment":
		return docgen.Description{
			Help: "Comment on a pull request.\n" +
				"\n" +
				"Avrea stores the comment and, when GitHub sync is enabled for the\n" +
				"organization, publishes it to GitHub asynchronously as the Avrea GitHub App or\n" +
				"your linked GitHub account, with attribution.\n" +
				"\n" +
				"Without --path, the comment applies to the whole pull request. With --path and\n" +
				"--line, it starts an inline review thread on the pull request's current head\n" +
				"commit. --start-line extends it to a range of lines. Line numbers refer to the\n" +
				"new file (--side right) or the old file (--side left).\n" +
				"\n" +
				"Requires a signed-in account (scoped tokens cannot comment), pull request\n" +
				"comments enabled for the organization, and an Avrea Git mirror of the\n" +
				"repository.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr pr comment 42 --body \"Looks good to me.\"\n" +
				"    avr pr comment 42 --body-file review.md\n" +
				"    go test ./... 2>&1 | tail -n 20 | avr pr comment 42 --body-file -\n" +
				"    avr pr comment 42 --path src/app.go --line 18 --body \"Handle this error.\"\n" +
				"    avr pr comment 42 --path src/app.go --start-line 10 --line 14 --side left --body \"Why?\"\n" +
				"    avr pr comment 42 --body \"Ship it\" --json id,publication\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    base_sha, body, commit_sha, created_at, end_line, id, path, publication,\n" +
				"    pull_number, repository_id, side, start_line, thread_id\n",
			Short: "Comment on a pull request.",
			Usage: []string{"[OPTIONS]", "NUMBER"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "number", Kind: "argument", Opts: []string{"number"}, Type: "integer", Required: true, Nargs: 1}, CompletionKind: "Int"},
				{Param: docgen.Param{Name: "body", Kind: "option", Opts: []string{"--body"}, Type: "text", Nargs: 1, Help: descriptionText("Comment text in Markdown.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "body_file", Kind: "option", Opts: []string{"--body-file"}, Type: "filename", Nargs: 1, Help: descriptionText("Read the comment text from a file, or - for stdin.")}, CompletionKind: "File"},
				{Param: docgen.Param{Name: "path", Kind: "option", Opts: []string{"--path"}, Type: "text", Nargs: 1, Help: descriptionText("Comment inline on this file, relative to the repository root. Requires --line.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "line", Kind: "option", Opts: []string{"--line"}, Type: "integer range", Nargs: 1, Help: descriptionText("Line to comment on, or the last line of a --start-line range.")}, Suffix: "  [x>=1]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "start_line", Kind: "option", Opts: []string{"--start-line"}, Type: "integer range", Nargs: 1, Help: descriptionText("First line of a multi-line inline comment. Defaults to --line.")}, Suffix: "  [x>=1]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "side", Kind: "option", Opts: []string{"--side"}, Type: "choice", Nargs: 1, Help: descriptionText("Diff side the lines refer to: right for the new file, left for the old file."), ShowDefault: true, Default: "right", Choices: []string{"right", "left"}}, Suffix: "  [default: right]", CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "repository", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
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
			Help:     "View and comment on pull requests.",
			Short:    "View and comment on pull requests.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "comment", Description: "Comment on a pull request.", AutoShort: true},
				{Name: "list", Description: "List pull requests across repositories.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
