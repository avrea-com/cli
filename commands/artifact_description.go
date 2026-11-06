// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func artifactDescription(path string) docgen.Description {
	switch path {
	case "avr artifact delete":
		return docgen.Description{
			Help:  "Delete ART_ID from the selected repository.",
			Short: "Delete ART_ID from the selected repository.",
			Usage: []string{"[OPTIONS]", "ART_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "artifact_id", Kind: "argument", Opts: []string{"artifact_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug; defaults to the configured organization.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository name or ID; defaults to the current git repository.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip confirmation.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr artifact download":
		return docgen.Description{
			Help:           "Download ART_ID and verify its size and SHA-256 before replacing the destination.",
			Short:          "Download ART_ID and verify its size and SHA-256 before replacing the destination.",
			CompletionHelp: "Download ART_ID and verify its size and...",
			Usage:          []string{"[OPTIONS]", "ART_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "artifact_id", Kind: "argument", Opts: []string{"artifact_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug; defaults to the configured organization.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository name or ID; defaults to the current git repository.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "path", Kind: "option", Opts: []string{"--path"}, Type: "path", Nargs: 1, Help: descriptionText("Destination directory or filename. ZIPs stay archived."), ShowDefault: true, Default: "."}, Suffix: "  [default: .]", CompletionKind: "Path", FileOkay: true},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr artifact get":
		return docgen.Description{
			Help:           "Get metadata for ART_ID in the selected repository.",
			Short:          "Get metadata for ART_ID in the selected repository.",
			CompletionHelp: "Get metadata for ART_ID in the selected...",
			Usage:          []string{"[OPTIONS]", "ART_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "artifact_id", Kind: "argument", Opts: []string{"artifact_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug; defaults to the configured organization.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository name or ID; defaults to the current git repository.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr artifact list":
		return docgen.Description{
			Help:           "List artifacts in REPO, optionally restricted to one run.",
			Short:          "List artifacts in REPO, optionally restricted to one run.",
			CompletionHelp: "List artifacts in REPO, optionally...",
			Usage:          []string{"[OPTIONS]", "REPO"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "repo", Kind: "argument", Opts: []string{"repo"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "run_id", Kind: "option", Opts: []string{"--run-id"}, Type: "text", Nargs: 1, Help: descriptionText("Numeric GitHub run ID, run- ID, or native awr- ID.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Maximum artifacts per page."), ShowDefault: true, Default: 50}, Suffix: "  [default: 50; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Continue from a previous page's cursor.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr artifact":
		return docgen.Description{
			Help:     "Manage workflow artifacts.",
			Short:    "Manage workflow artifacts.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "delete", Description: "Delete ART_ID from the selected repository.", AutoShort: true},
				{Name: "download", Description: "Download ART_ID and verify its size and SHA-256 before replacing the...", AutoShort: true},
				{Name: "get", Description: "Get metadata for ART_ID in the selected repository.", AutoShort: true},
				{Name: "list", Description: "List artifacts in REPO, optionally restricted to one run.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
