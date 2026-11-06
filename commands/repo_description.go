// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func repoDescription(path string) docgen.Description {
	switch path {
	case "avr repo clone":
		return docgen.Description{
			Help: "Clone a repository from its nearest available Avrea mirror.\n" +
				"\n" +
				"REPOSITORY is an owner/repository name or an Avrea repository ID (rep-...).\n" +
				"When several placements are available, the first to serve the repository\n" +
				"wins. A single placement is used directly. Use --placement to select an\n" +
				"exact URL or hostname. Requires Git 2.31 or newer.\n" +
				"\n" +
				"The origin remote stays on the selected read-only mirror. A repository-local\n" +
				"Avrea credential helper obtains fresh Git JWTs for subsequent fetch and pull\n" +
				"operations using your Avrea login. Push changes to the source repository\n" +
				"using a separate remote. Tokens normally last one hour; scoped API keys have\n" +
				"a shorter server-enforced lifetime. Tokens are never saved in the repository.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo clone foo/faa\n" +
				"    avr repo clone foo/faa my-checkout --depth 1\n" +
				"    avr repo clone foo/faa --placement https://git.fi.a.avrea.io\n",
			Short:          "Clone a repository from its nearest available Avrea mirror.",
			CompletionHelp: "Clone a repository from its nearest...",
			Usage:          []string{"[OPTIONS]", "REPOSITORY", "[DIRECTORY]"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "repository", Kind: "argument", Opts: []string{"repository"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "directory", Kind: "argument", Opts: []string{"directory"}, Type: "directory", Nargs: 1}, CompletionKind: "Path"},
				{Param: docgen.Param{Name: "placement", Kind: "option", Opts: []string{"--placement"}, Type: "text", Nargs: 1, Help: descriptionText("Exact mirror URL or hostname from the repository's placements.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "branch", Kind: "option", Opts: []string{"--branch", "-b"}, Type: "text", Nargs: 1, Help: descriptionText("Branch or tag to check out.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "depth", Kind: "option", Opts: []string{"--depth"}, Type: "integer range", Nargs: 1, Help: descriptionText("Create a shallow clone with this many commits.")}, Suffix: "  [x>=1]", CompletionKind: "IntRange"},
			},
		}
	case "avr repo list":
		return docgen.Description{
			Help: "List repositories you can access in an organization.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo list\n" +
				"    avr repo list --json full_name,repository_id\n" +
				"    avr repo list --json '*' -q '.[].full_name'\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    full_name, platform, platform_repository_id, repository_id\n",
			Short:          "List repositories you can access in an organization.",
			CompletionHelp: "List repositories you can access in an...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max repositories to return."), ShowDefault: true, Default: 100}, Suffix: "  [default: 100; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo mirror create":
		return docgen.Description{
			Help: "Enable mirroring and place a connected repository at a location.\n" +
				"\n" +
				"REPOSITORY is an owner/repo name on GitHub or a namespace/repo name on\n" +
				"GitLab (including nested groups). GitLab repositories cannot be mirrored\n" +
				"until GitLab mirroring is enabled for your organization. The repository\n" +
				"must already be connected to the\n" +
				"selected Avrea organization. Requires the organization admin role.\n" +
				"\n" +
				"Safe to repeat: existing placements and their source configuration are\n" +
				"preserved. The initial sync runs asynchronously; use `mirror status`\n" +
				"with the returned repository ID to check progress.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo mirror locations\n" +
				"    avr repo mirror create acme/widgets --location gsc-fi\n" +
				"    avr repo mirror create acme/team/widgets --source gitlab --location gsc-fi\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    enabled, full_name, placements, repository_id\n",
			Short:          "Enable mirroring and place a connected repository at a location.",
			CompletionHelp: "Enable mirroring and place a connected...",
			Usage:          []string{"[OPTIONS]", "REPOSITORY"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "repository", Kind: "argument", Opts: []string{"repository"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "source", Kind: "option", Opts: []string{"--source"}, Type: "choice", Nargs: 1, Help: descriptionText("Source platform. GitLab requires GitLab mirroring to be enabled for your organization."), ShowDefault: true, Default: "github", Choices: []string{"github", "gitlab"}}, Suffix: "  [default: github]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "location", Kind: "option", Opts: []string{"--location"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("Cluster ID, datacenter ID, or unique name from `avr repo mirror locations`.")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses the default organization.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo mirror disable":
		return docgen.Description{
			Help: "Stop mirroring the repository into avrea-git.\n" +
				"\n" +
				"Placements are kept but become inert, so re-enabling restores them.\n" +
				"Requires the organization admin role.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo mirror disable\n" +
				"    avr repo mirror disable --repo acme/widgets --yes\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    enabled, full_name, placements, repository_id\n",
			Short: "Stop mirroring the repository into avrea-git.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip the confirmation prompt.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "source", Kind: "option", Opts: []string{"--source"}, Type: "choice", Nargs: 1, Help: descriptionText("Source platform. GitLab requires GitLab mirroring to be enabled for your organization."), ShowDefault: true, Default: "github", Choices: []string{"github", "gitlab"}}, Suffix: "  [default: github]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo mirror enable":
		return docgen.Description{
			Help: "Declare the repository mirrored into avrea-git.\n" +
				"\n" +
				"Enabling makes existing placements active again; a freshly declared\n" +
				"repository still needs at least one placement (`avr repo mirror place`)\n" +
				"before anything is synced. Requires the organization admin role.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo mirror enable\n" +
				"    avr repo mirror enable --repo acme/widgets\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    enabled, full_name, placements, repository_id\n",
			Short:          "Declare the repository mirrored into avrea-git.",
			CompletionHelp: "Declare the repository mirrored into...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "source", Kind: "option", Opts: []string{"--source"}, Type: "choice", Nargs: 1, Help: descriptionText("Source platform. GitLab requires GitLab mirroring to be enabled for your organization."), ShowDefault: true, Default: "github", Choices: []string{"github", "gitlab"}}, Suffix: "  [default: github]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo mirror locations":
		return docgen.Description{
			Help: "List available mirror locations and their cluster IDs.\n" +
				"\n" +
				"Use a cluster ID, datacenter ID, or unique name with `mirror create --location`.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo mirror locations\n" +
				"    avr repo mirror locations --json cluster_id,name\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    cluster_id, datacenter_id, name\n",
			Short:          "List available mirror locations and their cluster IDs.",
			CompletionHelp: "List available mirror locations and their...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses the default organization.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo mirror place":
		return docgen.Description{
			Help: "Place the repository's mirror in a git cluster.\n" +
				"\n" +
				"CLUSTER_ID is one of the ids from `avr repo mirror locations`. Placing is\n" +
				"idempotent; a new placement syncs from the upstream platform copy.\n" +
				"Mirroring must be enabled first. Requires the organization admin role.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo mirror place gsc-fi\n" +
				"    avr repo mirror place gsc-fi --repo acme/widgets\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    enabled, full_name, placements, repository_id\n",
			Short:          "Place the repository's mirror in a git cluster.",
			CompletionHelp: "Place the repository's mirror in a git...",
			Usage:          []string{"[OPTIONS]", "CLUSTER_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "cluster_id", Kind: "argument", Opts: []string{"cluster_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "source", Kind: "option", Opts: []string{"--source"}, Type: "choice", Nargs: 1, Help: descriptionText("Source platform. GitLab requires GitLab mirroring to be enabled for your organization."), ShowDefault: true, Default: "github", Choices: []string{"github", "gitlab"}}, Suffix: "  [default: github]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo mirror status":
		return docgen.Description{
			Help: "Show the repository's git-mirror declaration and placements.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo mirror status\n" +
				"    avr repo mirror status --repo acme/widgets\n" +
				"    avr repo mirror status --json enabled,placements\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    enabled, full_name, placements, repository_id\n",
			Short:          "Show the repository's git-mirror declaration and placements.",
			CompletionHelp: "Show the repository's git-mirror...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "source", Kind: "option", Opts: []string{"--source"}, Type: "choice", Nargs: 1, Help: descriptionText("Source platform. GitLab requires GitLab mirroring to be enabled for your organization."), ShowDefault: true, Default: "github", Choices: []string{"github", "gitlab"}}, Suffix: "  [default: github]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo mirror unplace":
		return docgen.Description{
			Help: "Remove the repository's mirror from a git cluster.\n" +
				"\n" +
				"The mirrored data in that cluster is dropped; other placements are\n" +
				"unaffected. Requires the organization admin role.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo mirror unplace gsc-fi\n" +
				"    avr repo mirror unplace gsc-fi --repo acme/widgets --yes\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    enabled, full_name, placements, repository_id\n",
			Short:          "Remove the repository's mirror from a git cluster.",
			CompletionHelp: "Remove the repository's mirror from a git...",
			Usage:          []string{"[OPTIONS]", "CLUSTER_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "cluster_id", Kind: "argument", Opts: []string{"cluster_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip the confirmation prompt.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "source", Kind: "option", Opts: []string{"--source"}, Type: "choice", Nargs: 1, Help: descriptionText("Source platform. GitLab requires GitLab mirroring to be enabled for your organization."), ShowDefault: true, Default: "github", Choices: []string{"github", "gitlab"}}, Suffix: "  [default: github]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo mirror":
		return docgen.Description{
			Help:           "Manage this repository's avrea-git mirror (feature-flagged).",
			Short:          "Manage this repository's avrea-git mirror (feature-flagged).",
			CompletionHelp: "Manage this repository's avrea-git mirror...",
			Usage:          []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections:       true,
			Commands: []docgen.HelpCommand{
				{Name: "create", Description: "Enable mirroring and place a connected repository at a location.", AutoShort: true},
				{Name: "disable", Description: "Stop mirroring the repository into avrea-git.", AutoShort: true},
				{Name: "enable", Description: "Declare the repository mirrored into avrea-git.", AutoShort: true},
				{Name: "locations", Description: "List available mirror locations and their cluster IDs.", AutoShort: true},
				{Name: "place", Description: "Place the repository's mirror in a git cluster.", AutoShort: true},
				{Name: "status", Description: "Show the repository's git-mirror declaration and placements.", AutoShort: true},
				{Name: "unplace", Description: "Remove the repository's mirror from a git cluster.", AutoShort: true},
			},
		}
	case "avr repo public-mirror cancel":
		return docgen.Description{
			Help: "Cancel one pending public-mirror request.\n" +
				"\n" +
				"An approved mirror is global and cannot be withdrawn by a requester.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo public-mirror cancel pmr-0123456789abcdef0123456789abcdef\n" +
				"    avr repo public-mirror cancel pmr-0123456789abcdef0123456789abcdef --yes\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    approval_state, created_at, github_snapshot, public_access_expires_at,\n" +
				"    reason, repository_full_name, repository_id, request_id,\n" +
				"    requester_organization_id, requester_user_id, review_note, reviewed_at,\n" +
				"    reviewed_by_user_id, status, updated_at\n",
			Short: "Cancel one pending public-mirror request.",
			Usage: []string{"[OPTIONS]", "REQUEST_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "request_id", Kind: "argument", Opts: []string{"request_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip the confirmation prompt.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo public-mirror check":
		return docgen.Description{
			Help: "Check whether a public GitHub repository mirror is available.\n" +
				"\n" +
				"FULL_NAME must be an owner/repository name. This performs an exact lookup;\n" +
				"the global public-mirror catalog cannot be listed.\n" +
				"\n" +
				"A repository that is not mirrored is an answer, not a failure: the command\n" +
				"reports ``Available: no`` (``available: false`` under --json) and exits 0.\n" +
				"Only a real failure — denied, malformed name, server error — exits non-zero.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo public-mirror check rust-lang/rust\n" +
				"    avr repo public-mirror check rust-lang/rust --json '*'\n" +
				"    avr repo public-mirror check rust-lang/rust --json available,default_branch\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    approval_state, available, default_branch, https_clone_url,\n" +
				"    installation_kind, is_archived, is_disabled, is_fork, mirror_enabled,\n" +
				"    platform_owner_id, platform_owner_login, platform_owner_type,\n" +
				"    platform_pushed_at, platform_repository_id, platform_size_kb,\n" +
				"    public_access_expires_at, public_metadata_verified_at,\n" +
				"    repository_full_name, repository_id\n",
			Short:          "Check whether a public GitHub repository mirror is available.",
			CompletionHelp: "Check whether a public GitHub repository...",
			Usage:          []string{"[OPTIONS]", "FULL_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "full_name", Kind: "argument", Opts: []string{"full_name"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo public-mirror request":
		return docgen.Description{
			Help: "Request a mirror of a public GitHub repository.\n" +
				"\n" +
				"FULL_NAME is the canonical GitHub owner/repository name. Avrea validates\n" +
				"the repository with GitHub before recording the request.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo public-mirror request rust-lang/rust\n" +
				"    avr repo public-mirror request rust-lang/rust --reason \"Build dependency\"\n" +
				"    avr repo public-mirror request rust-lang/rust --org acme --json '*'\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    approval_state, created_at, github_snapshot, public_access_expires_at,\n" +
				"    reason, repository_full_name, repository_id, request_id,\n" +
				"    requester_organization_id, requester_user_id, review_note, reviewed_at,\n" +
				"    reviewed_by_user_id, status, updated_at\n",
			Short:          "Request a mirror of a public GitHub repository.",
			CompletionHelp: "Request a mirror of a public GitHub...",
			Usage:          []string{"[OPTIONS]", "FULL_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "full_name", Kind: "argument", Opts: []string{"full_name"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "reason", Kind: "option", Opts: []string{"--reason"}, Type: "text", Nargs: 1, Help: descriptionText("Why your organization needs this repository mirrored.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo public-mirror requests":
		return docgen.Description{
			Help: "List your organization's public-mirror requests.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo public-mirror requests\n" +
				"    avr repo public-mirror requests --org acme\n" +
				"    avr repo public-mirror requests --json request_id,status,repository_full_name\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    approval_state, created_at, github_snapshot, public_access_expires_at,\n" +
				"    reason, repository_full_name, repository_id, request_id,\n" +
				"    requester_organization_id, requester_user_id, review_note, reviewed_at,\n" +
				"    reviewed_by_user_id, status, updated_at\n",
			Short:          "List your organization's public-mirror requests.",
			CompletionHelp: "List your organization's public-mirror...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo public-mirror view":
		return docgen.Description{
			Help: "View one public-mirror request.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr repo public-mirror view pmr-0123456789abcdef0123456789abcdef\n" +
				"    avr repo public-mirror view pmr-0123456789abcdef0123456789abcdef --json '*'\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    approval_state, created_at, github_snapshot, public_access_expires_at,\n" +
				"    reason, repository_full_name, repository_id, request_id,\n" +
				"    requester_organization_id, requester_user_id, review_note, reviewed_at,\n" +
				"    reviewed_by_user_id, status, updated_at\n",
			Short: "View one public-mirror request.",
			Usage: []string{"[OPTIONS]", "REQUEST_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "request_id", Kind: "argument", Opts: []string{"request_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr repo public-mirror":
		return docgen.Description{
			Help:           "Request and browse mirrors of public GitHub repositories.",
			Short:          "Request and browse mirrors of public GitHub repositories.",
			CompletionHelp: "Request and browse mirrors of public...",
			Usage:          []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections:       true,
			Commands: []docgen.HelpCommand{
				{Name: "cancel", Description: "Cancel one pending public-mirror request.", AutoShort: true},
				{Name: "check", Description: "Check whether a public GitHub repository mirror is available.", AutoShort: true},
				{Name: "request", Description: "Request a mirror of a public GitHub repository.", AutoShort: true},
				{Name: "requests", Description: "List your organization's public-mirror requests.", AutoShort: true},
				{Name: "view", Description: "View one public-mirror request.", AutoShort: true},
			},
		}
	case "avr repo":
		return docgen.Description{
			Help:           "Manage repositories, git mirrors, and public mirrors.",
			Short:          "Manage repositories, git mirrors, and public mirrors.",
			CompletionHelp: "Manage repositories, git mirrors, and...",
			Usage:          []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections:       true,
			Commands: []docgen.HelpCommand{
				{Name: "clone", Description: "Clone a repository from its nearest available Avrea mirror.", AutoShort: true},
				{Name: "list", Description: "List repositories you can access in an organization.", AutoShort: true},
				{Name: "mirror", Description: "Manage this repository's avrea-git mirror (feature-flagged).", AutoShort: true},
				{Name: "public-mirror", Description: "Request and browse mirrors of public GitHub repositories.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
