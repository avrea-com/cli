// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func internalDescription(path string) docgen.Description {
	switch path {
	case "avr internal docs":
		return docgen.Description{
			Help:  "Generate avr reference docs from the native command tree.",
			Short: "Generate avr reference docs from the native command tree.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "fmt", Kind: "option", Opts: []string{"--format"}, Type: "text", Nargs: 1, Help: descriptionText("Output format."), Choices: []string{"markdown", "json", "starlight", "man", "all"}}, Label: "--format [markdown|json|starlight|man|all]", Suffix: "  [default: all]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "out", Kind: "option", Opts: []string{"--out"}, Type: "text", Nargs: 1, Help: descriptionText("Output target. For 'markdown'/'json' it's a file path (or directory; the standard filename is used). For 'starlight'/'man' it's a directory. For 'all' it's a base directory (writes docs/ and man/ subtrees).")}, Label: "--out PATH", CompletionKind: "Path", FileOkay: true},
				{Param: docgen.Param{Name: "to_stdout", Kind: "option", Opts: []string{"--stdout"}, Type: "text", Nargs: 1, IsFlag: true, Help: descriptionText("Write to stdout (only valid for single-file formats: markdown, json).")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "readme", Kind: "option", Opts: []string{"--readme"}, Type: "text", Nargs: 1, Help: descriptionText("README to embed as the Starlight overview page (default: README.md beside the executable; omitted if not found).")}, Label: "--readme FILE", CompletionKind: "Path", FileOkay: true},
			},
		}
	case "avr internal":
		return docgen.Description{
			Help:  "Maintenance commands (hidden).",
			Short: "Maintenance commands (hidden).",
			Usage: []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Commands: []docgen.HelpCommand{
				{Name: "docs", Description: "Generate avr reference docs from the native command tree."},
			},
		}
	}
	return docgen.Description{}
}
