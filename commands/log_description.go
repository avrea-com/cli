// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func logDescription(path string) docgen.Description {
	switch path {
	case "avr log search":
		return docgen.Description{
			Help: "Search logs for a repository.\n" +
				"\n" +
				"Performs full-text search when --query is provided, otherwise returns\n" +
				"logs sorted by line number. Results are filtered to logs from repositories\n" +
				"you have access to.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr log search --repo acme/web --query \"error\"\n" +
				"    avr log search --repo rep-abc123 --level error --limit 50\n" +
				"    avr log search --repo rep-abc123 --vm-id vm-xyz --stream stderr\n" +
				"    avr log search --repo rep-abc123 --query \"OOM\" --json content,timestamp,vm_id\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    content, group_name, id, level, line_number, repository_id, score,\n" +
				"    step_name, step_record_id, stream, timestamp, vm_id\n",
			Short: "Search logs for a repository.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "query", Kind: "option", Opts: []string{"--query"}, Type: "text", Nargs: 1, Help: descriptionText("Full-text search query")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "stream", Kind: "option", Opts: []string{"--stream"}, Type: "choice", Nargs: 1, Help: descriptionText("Filter by output stream"), Choices: []string{"stdout", "stderr"}}, CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "level", Kind: "option", Opts: []string{"--level"}, Type: "choice", Nargs: 1, Help: descriptionText("Filter by log level"), Choices: []string{"debug", "info", "warning", "error"}}, CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "vm_id", Kind: "option", Opts: []string{"--vm-id"}, Type: "text", Nargs: 1, Help: descriptionText("Filter by execution/VM ID")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer", Nargs: 1, Help: descriptionText("Maximum results to return"), ShowDefault: true, Default: 100}, Suffix: "  [default: 100]", CompletionKind: "Int"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr log":
		return docgen.Description{
			Help:     "Search across runner execution logs.",
			Short:    "Search across runner execution logs.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "search", Description: "Search logs for a repository.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
