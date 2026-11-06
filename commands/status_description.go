// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func statusDescription(path string) docgen.Description {
	switch path {
	case "avr status":
		return docgen.Description{
			Help:           "Show recent runs, performance stats, and cache health.",
			Short:          "Show recent runs, performance stats, and cache health.",
			CompletionHelp: "Show recent runs, performance stats, and...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "since", Kind: "option", Opts: []string{"--since"}, Type: "text", Nargs: 1, Help: descriptionText("Time window for stats panels: '7d', '24h', etc."), ShowDefault: true, Default: "7d"}, Suffix: "  [default: 7d]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_output", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Output raw JSON.")}, CompletionKind: "Bool"},
			},
		}
	}
	return docgen.Description{}
}
