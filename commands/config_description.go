// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func configDescription(path string) docgen.Description {
	switch path {
	case "avr config get":
		return docgen.Description{
			Help: "Print the value of a configuration key.\n" +
				"\n" +
				"\b\n" +
				"Available keys:\n" +
				"  org   Active organization ID\n",
			Short: "Print the value of a configuration key.",
			Usage: []string{"[OPTIONS]", "{org}"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "key", Kind: "argument", Opts: []string{"key"}, Type: "choice", Required: true, Nargs: 1, Choices: []string{"org"}}, CompletionKind: "Choice", CaseSensitive: true},
			},
		}
	case "avr config list":
		return docgen.Description{
			Help:           "Show the active CLI configuration (host, auth, org, default repo).",
			Short:          "Show the active CLI configuration (host, auth, org, default repo).",
			CompletionHelp: "Show the active CLI configuration (host,...",
		}
	case "avr config set":
		return docgen.Description{
			Help: "Set a CLI configuration value.\n" +
				"\n" +
				"\b\n" +
				"Available keys:\n" +
				"  org   Active organization (ID or slug)\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr config set org org-abc123\n" +
				"    avr config set org acme\n",
			Short: "Set a CLI configuration value.",
			Usage: []string{"[OPTIONS]", "{org}", "VALUE"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "key", Kind: "argument", Opts: []string{"key"}, Type: "choice", Required: true, Nargs: 1, Choices: []string{"org"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "value", Kind: "argument", Opts: []string{"value"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
			},
		}
	case "avr config unset":
		return docgen.Description{
			Help: "Remove a configuration override.\n" +
				"\n" +
				"\b\n" +
				"Available keys:\n" +
				"  org   Drop the stored default organization for the active host\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr config unset org\n",
			Short: "Remove a configuration override.",
			Usage: []string{"[OPTIONS]", "{org}"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "key", Kind: "argument", Opts: []string{"key"}, Type: "choice", Required: true, Nargs: 1, Choices: []string{"org"}}, CompletionKind: "Choice", CaseSensitive: true},
			},
		}
	case "avr config":
		return docgen.Description{
			Help:     "View and manage CLI configuration.",
			Short:    "View and manage CLI configuration.",
			Usage:    []string{"[OPTIONS]", "[COMMAND] [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "get", Description: "Print the value of a configuration key.", AutoShort: true},
				{Name: "list", Description: "Show the active CLI configuration (host, auth, org, default repo).", AutoShort: true},
				{Name: "set", Description: "Set a CLI configuration value.", AutoShort: true},
				{Name: "unset", Description: "Remove a configuration override.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
