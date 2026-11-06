// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func cacheDescription(path string) docgen.Description {
	switch path {
	case "avr cache delete":
		return docgen.Description{
			Help: "Delete cache entries by key name or all entries.\n" +
				"\n" +
				"Exactly one of --key or --all must be provided.\n" +
				"When using --key, --type is required to scope the deletion.\n" +
				"With --all, the purge runs asynchronously; the command reports its status.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr cache delete --repo rep-abc123 --type gha --key \"node_modules\" --ref refs/heads/main --yes\n" +
				"    avr cache delete --repo rep-abc123 --all --yes\n",
			Short:          "Delete cache entries by key name or all entries.",
			CompletionHelp: "Delete cache entries by key name or all...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "cache_type", Kind: "option", Opts: []string{"--type"}, Type: "text", Nargs: 1, Help: descriptionText("Cache type (required with --key, e.g. gha, bazel, sccache).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "key", Kind: "option", Opts: []string{"--key"}, Type: "text", Nargs: 1, Help: descriptionText("Delete entries matching this cache key name.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "ref", Kind: "option", Opts: []string{"--ref"}, Type: "text", Nargs: 1, Help: descriptionText("Ref to narrow deletion scope (used by gha).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "delete_all", Kind: "option", Opts: []string{"--all"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Delete ALL cache entries for the repository.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip confirmation prompt.")}, CompletionKind: "Bool"},
			},
		}
	case "avr cache list":
		return docgen.Description{
			Help: "List cache entries for a repository.\n" +
				"Large caches use cursor pagination; follow the next-page hint on stderr.\n" +
				"Sort order and offsets apply only to smaller, sorted result sets.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr cache list --repo rep-abc123\n" +
				"    avr cache list --repo rep-abc123 --type gha --limit 50\n" +
				"    avr cache list --repo rep-abc123 --key \"node_modules\" --ref refs/heads/main\n" +
				"    avr cache list --repo rep-abc123 --json key,size_bytes,created_at\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    cache_type, created_at, hit_count, key, last_accessed_at, ref, size_bytes, version\n",
			Short: "List cache entries for a repository.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "cache_type", Kind: "option", Opts: []string{"--type"}, Type: "text", Nargs: 1, Help: descriptionText("Filter by cache type (e.g. gha, bazel, turbo, rclone).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "key", Kind: "option", Opts: []string{"--key"}, Type: "text", Nargs: 1, Help: descriptionText("Filter by key prefix.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "ref", Kind: "option", Opts: []string{"--ref"}, Type: "text", Nargs: 1, Help: descriptionText("Filter by exact ref match.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max entries to return."), ShowDefault: true, Default: 100}, Suffix: "  [default: 100; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "offset", Kind: "option", Opts: []string{"--offset"}, Type: "integer range", Nargs: 1, Help: descriptionText("Number of entries to skip in sorted results."), ShowDefault: true, Default: 0}, Suffix: "  [default: 0; x>=0]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Pagination cursor for large caches. Cannot combine with --offset.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "order", Kind: "option", Opts: []string{"--order"}, Type: "choice", Nargs: 1, Help: descriptionText("Sort order."), ShowDefault: true, Default: "created_at.desc", Choices: []string{"created_at.desc", "created_at.asc"}}, Suffix: "  [default: created_at.desc]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "web", Kind: "option", Opts: []string{"--web"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Open in browser.")}, CompletionKind: "Bool"},
			},
		}
	case "avr cache usage":
		return docgen.Description{
			Help: "Show cache usage summary for a repository.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr cache usage --repo rep-abc123\n" +
				"    avr cache usage --repo rep-abc123 --json '*'\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    by_type, over_quota, quota_bytes, total_size_bytes\n",
			Short: "Show cache usage summary for a repository.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr cache":
		return docgen.Description{
			Help:     "Inspect and manage the Avrea build cache.",
			Short:    "Inspect and manage the Avrea build cache.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "delete", Description: "Delete cache entries by key name or all entries.", AutoShort: true},
				{Name: "list", Description: "List cache entries for a repository.", AutoShort: true},
				{Name: "usage", Description: "Show cache usage summary for a repository.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
