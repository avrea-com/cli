// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func settingsDescription(path string) docgen.Description {
	switch path {
	case "avr settings list":
		return docgen.Description{
			Help: "List settings with their current values and source.\n" +
				"\n" +
				"Inside a connected checkout the repository is auto-detected from the git\n" +
				"remote and its effective values are shown. Pass --org to see organization\n" +
				"values (this suppresses auto-detection), or --repo / AVR_REPO to target a\n" +
				"repository.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr settings list --org org-abc123\n" +
				"    avr settings list --org org-abc123 --repo rep-xyz789\n" +
				"    avr settings list --repo acme/web\n" +
				"    avr settings list --prefix cache.\n" +
				"    avr settings list --json key,value,source\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    key, source, value\n",
			Short:          "List settings with their current values and source.",
			CompletionHelp: "List settings with their current values...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git unless --org given.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "prefix", Kind: "option", Opts: []string{"--prefix"}, Type: "text", Nargs: 1, Help: descriptionText("Filter by key prefix (e.g. 'cache.').")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "web", Kind: "option", Opts: []string{"--web"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Open in browser.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr settings reset":
		return docgen.Description{
			Help: "Remove a setting override, reverting to the inherited or default value.\n" +
				"\n" +
				"Clears the repository override when run inside a connected checkout (the\n" +
				"repo is auto-detected), when --repo is given, or when AVR_REPO is set. Pass\n" +
				"--org to clear the organization-scoped value (this suppresses auto-detection).\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr settings reset cache.gha.enabled --repo rep-xyz789\n" +
				"    avr settings reset cache.packages.enabled --org org-abc123\n",
			Short:          "Remove a setting override, reverting to the inherited or default value.",
			CompletionHelp: "Remove a setting override, reverting to...",
			Usage:          []string{"[OPTIONS]", "KEY"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "key", Kind: "argument", Opts: []string{"key"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git unless --org given.")}, CompletionKind: "String"},
			},
		}
	case "avr settings schema":
		return docgen.Description{
			Help: "List available setting definitions.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr settings schema\n" +
				"    avr settings schema --prefix cache. --scope repository\n" +
				"    avr settings schema --json '*'\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    choices, default, description, inherits, key, max_value, min_value,\n" +
				"    scopes, value_type\n",
			Short: "List available setting definitions.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "prefix", Kind: "option", Opts: []string{"--prefix"}, Type: "text", Nargs: 1, Help: descriptionText("Filter by key prefix (e.g. 'cache.').")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "scope", Kind: "option", Opts: []string{"--scope"}, Type: "choice", Nargs: 1, Help: descriptionText("Filter by scope."), Choices: []string{"repository", "organization"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr settings set":
		return docgen.Description{
			Help: "Set a setting value.\n" +
				"\n" +
				"VALUE is parsed as a boolean (true/false) or integer when possible,\n" +
				"otherwise treated as a string.\n" +
				"\n" +
				"Writes a repository override when run inside a connected checkout (the repo\n" +
				"is auto-detected), when --repo is given, or when AVR_REPO is set. Otherwise\n" +
				"targets org scope (the default org, or --org); org-only settings must be set\n" +
				"with --org. A checkout whose repo isn't connected to the org is an error\n" +
				"rather than a silent org-wide write.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr settings set cache.gha.enabled false --org org-abc123\n" +
				"    avr settings set cache.packages.enabled true --repo rep-xyz789\n",
			Short: "Set a setting value.",
			Usage: []string{"[OPTIONS]", "KEY", "VALUE"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "key", Kind: "argument", Opts: []string{"key"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "value", Kind: "argument", Opts: []string{"value"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git unless --org given.")}, CompletionKind: "String"},
			},
		}
	case "avr settings":
		return docgen.Description{
			Help:     "View and toggle cache and runner settings.",
			Short:    "View and toggle cache and runner settings.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "list", Description: "List settings with their current values and source.", AutoShort: true},
				{Name: "reset", Description: "Remove a setting override, reverting to the inherited or default value.", AutoShort: true},
				{Name: "schema", Description: "List available setting definitions.", AutoShort: true},
				{Name: "set", Description: "Set a setting value.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
