// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func healthDescription(path string) docgen.Description {
	switch path {
	case "avr health":
		return docgen.Description{
			Help: "Check Avrea platform status.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr health\n" +
				"    avr health --json status\n" +
				"    avr health --json '*' -q '.status'\n",
			Short: "Check Avrea platform status.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	}
	return docgen.Description{}
}
