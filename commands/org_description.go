// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func orgDescription(path string) docgen.Description {
	switch path {
	case "avr org create":
		return docgen.Description{
			Help: "Create a new organization.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    name, organization_id, role, slug\n",
			Short: "Create a new organization.",
			Usage: []string{"[OPTIONS]", "NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "name", Kind: "argument", Opts: []string{"name"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org email-domain claim":
		return docgen.Description{
			Help: "Claim a company domain using DNS ownership verification (admin only).\n" +
				"\n" +
				"The domain does not need to match your GitHub or Avrea account email.\n" +
				"Publish the returned TXT record, then run ``email-domain verify``.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr org email-domain claim example.com\n" +
				"    avr org email-domain claim corp.example.com --org acme\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    created_at, dns_record_name, dns_record_value, domain,\n" +
				"    organization_email_domain_id, verified, verified_at\n",
			Short:          "Claim a company domain using DNS ownership verification (admin only).",
			CompletionHelp: "Claim a company domain using DNS ownership...",
			Usage:          []string{"[OPTIONS]", "DOMAIN"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "domain", Kind: "argument", Opts: []string{"domain"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org email-domain list":
		return docgen.Description{
			Help: "List claimed organization email domains (admin only).\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    created_at, dns_record_name, dns_record_value, domain,\n" +
				"    organization_email_domain_id, verified, verified_at\n",
			Short:          "List claimed organization email domains (admin only).",
			CompletionHelp: "List claimed organization email domains...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org email-domain set":
		return docgen.Description{
			Help: "Set email domains for automatic org membership (admin only).\n" +
				"\n" +
				"Replaces all existing domains — a typo wipes the org's auto-membership\n" +
				"policy. Confirms before applying; pass --yes to skip the prompt (required\n" +
				"when stdout isn't a TTY, e.g. in CI).\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr org email-domain set example.com\n" +
				"    avr org email-domain set example.com corp.example.com --yes\n",
			Short:          "Set email domains for automatic org membership (admin only).",
			CompletionHelp: "Set email domains for automatic org...",
			Usage:          []string{"[OPTIONS]", "DOMAINS..."},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "confirmed", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip confirmation prompt.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "domains", Kind: "argument", Opts: []string{"domains"}, Type: "text", Required: true, Nargs: -1}, CompletionKind: "String"},
			},
		}
	case "avr org email-domain verify":
		return docgen.Description{
			Help: "Check a claimed domain's DNS TXT record (admin only).\n" +
				"\n" +
				"Each invocation performs a fresh DNS lookup. If DNS has not propagated,\n" +
				"wait and run the command again.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr org email-domain verify example.com\n" +
				"    avr org email-domain verify corp.example.com --org acme\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    created_at, dns_record_name, dns_record_value, domain,\n" +
				"    organization_email_domain_id, verified, verified_at\n",
			Short:          "Check a claimed domain's DNS TXT record (admin only).",
			CompletionHelp: "Check a claimed domain's DNS TXT record...",
			Usage:          []string{"[OPTIONS]", "DOMAIN"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "domain", Kind: "argument", Opts: []string{"domain"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org email-domain":
		return docgen.Description{
			Help:     "Claim and verify organization email domains.",
			Short:    "Claim and verify organization email domains.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "claim", Description: "Claim a company domain using DNS ownership verification (admin only).", AutoShort: true},
				{Name: "list", Description: "List claimed organization email domains (admin only).", AutoShort: true},
				{Name: "set", Description: "Set email domains for automatic org membership (admin only).", AutoShort: true},
				{Name: "verify", Description: "Check a claimed domain's DNS TXT record (admin only).", AutoShort: true},
			},
		}
	case "avr org install add":
		return docgen.Description{
			Help: "Add a GitHub or GitLab installation.\n" +
				"\n" +
				"GitHub is the default and retains the browser-based GitHub App flow.\n" +
				"\n" +
				"For GitLab, create an expiring gitlab.com group access token with Reporter\n" +
				"role, api, and read_repository. The token is read from a hidden prompt\n" +
				"or stdin and is never accepted as a command-line value.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr org install add\n" +
				"    avr org install add --platform gitlab --group-full-path acme/platform --project-selection all\n" +
				"    printf '%s\\n' \"$GITLAB_TOKEN\" | avr org install add --platform gitlab \\\n" +
				"        --group-full-path acme/platform --project-selection selected \\\n" +
				"        --project-id 123 --access-token-stdin\n" +
				"\n" +
				"With --json, GitHub emits the installations detected within --wait-seconds\n" +
				"as a list and keeps progress messages on stderr; GitLab emits the\n" +
				"registered installation, and the field list or jq filter must keep\n" +
				"webhook_setup, which carries the one-time webhook secret.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS (GitHub)\n" +
				"    created_at, installation_id, organization_name, organization_slug,\n" +
				"    platform, platform_installation_id, platform_settings_url, state,\n" +
				"    target_id, target_name, target_type, configured_runner_count, degraded_runner_count\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS (GitLab)\n" +
				"    access_token_expires_on, access_token_invalid_at,\n" +
				"    access_token_last_validated_at, created_at, credential_kind,\n" +
				"    gitlab_namespace_full_path, gitlab_namespace_id, gitlab_project_ids,\n" +
				"    installation_id, organization_id, project_selection_mode, configured_runner_count,\n" +
				"    degraded_runner_count, selected_project_count, state, target_type,\n" +
				"    updated_at, webhook_secret_rotated_at, webhook_setup\n",
			Short: "Add a GitHub or GitLab installation.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "platform", Kind: "option", Opts: []string{"--platform"}, Type: "choice", Nargs: 1, Help: descriptionText("Source platform to install."), ShowDefault: true, Default: "github", Choices: []string{"github", "gitlab"}}, Suffix: "  [default: github]", CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "group_full_path", Kind: "option", Opts: []string{"--group-full-path"}, Type: "text", Nargs: 1, Help: descriptionText("GitLab group full path, such as acme/platform.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "project_selection", Kind: "option", Opts: []string{"--project-selection"}, Type: "choice", Nargs: 1, Help: descriptionText("GitLab project selection mode."), Choices: []string{"all", "selected"}}, CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "project_ids", Kind: "option", Opts: []string{"--project-id"}, Type: "integer range", Multiple: true, Nargs: 1, Help: descriptionText("GitLab numeric project ID to select (repeatable, up to 1,000).")}, Suffix: "  [x>=1]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "access_token_stdin", Kind: "option", Opts: []string{"--access-token-stdin"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Read the GitLab group access token from stdin instead of prompting.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "no_browser", Kind: "option", Opts: []string{"--no-browser"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Do not open browser automatically.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "wait_seconds", Kind: "option", Opts: []string{"--wait-seconds"}, Type: "integer", Nargs: 1, Help: descriptionText("Seconds to wait for detection."), ShowDefault: true, Default: 120}, Suffix: "  [default: 120]", CompletionKind: "Int"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org install list":
		return docgen.Description{
			Help: "List accessible installations across all your organizations.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    created_at, installation_id, organization_name, organization_slug,\n" +
				"    platform, platform_installation_id, platform_settings_url, state,\n" +
				"    target_id, target_name, target_type, configured_runner_count, degraded_runner_count\n",
			Short:          "List accessible installations across all your organizations.",
			CompletionHelp: "List accessible installations across all...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org install remove":
		return docgen.Description{
			Help: "Remove an installation using its provider-specific lifecycle.\n" +
				"\n" +
				"GitHub installations are suspended. GitLab installations are permanently\n" +
				"disconnected, including their Avrea history; use `avr org install rotate`\n" +
				"for a credential problem instead. Pass --yes to skip confirmation.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    action, installation_id, organization_id, platform\n",
			Short:          "Remove an installation using its provider-specific lifecycle.",
			CompletionHelp: "Remove an installation using its...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "installation_id", Kind: "option", Opts: []string{"--installation-id"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("Installation ID to remove (ins-xxx format)")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "confirmed", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip confirmation prompt.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org install rotate":
		return docgen.Description{
			Help: "Rotate a GitLab installation's access token and webhook secret.\n" +
				"\n" +
				"Create an expiring gitlab.com group access token with Reporter role,\n" +
				"api, and read_repository. The new token is read from a hidden prompt\n" +
				"or stdin and is never accepted as a command-line value.\n" +
				"\n" +
				"With --json, the field list or jq filter must keep webhook_setup, which\n" +
				"carries the one-time webhook secret.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    access_token_expires_on, access_token_invalid_at,\n" +
				"    access_token_last_validated_at, created_at, credential_kind,\n" +
				"    gitlab_namespace_full_path, gitlab_namespace_id, gitlab_project_ids,\n" +
				"    installation_id, organization_id, project_selection_mode, configured_runner_count,\n" +
				"    degraded_runner_count, selected_project_count, state, target_type,\n" +
				"    updated_at, webhook_secret_rotated_at, webhook_setup\n",
			Short:          "Rotate a GitLab installation's access token and webhook secret.",
			CompletionHelp: "Rotate a GitLab installation's access...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "installation_id", Kind: "option", Opts: []string{"--installation-id"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("GitLab installation ID (ins-xxx format).")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "access_token_stdin", Kind: "option", Opts: []string{"--access-token-stdin"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Read the GitLab group access token from stdin instead of prompting.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org install runner create":
		return docgen.Description{
			Help: "Create group runners in GitLab and configure their machine types in Avrea.\n" +
				"\n" +
				"Supply your own personal access token with create_runner scope and Owner\n" +
				"role. It is sent only to gitlab.com and is never persisted. Already\n" +
				"configured machine types must be replaced with 'runner set'.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    data, setup\n",
			Short:          "Create group runners in GitLab and configure their machine types in Avrea.",
			CompletionHelp: "Create group runners in GitLab and...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "installation_id", Kind: "option", Opts: []string{"--installation-id"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("GitLab installation ID (ins-xxx format).")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "machine_types", Kind: "option", Opts: []string{"--machine-type"}, Type: "text", Required: true, Multiple: true, Nargs: 1, Help: descriptionText("Machine type to create; repeat for multiple machine types.")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "protected", Kind: "option", Opts: []string{"--protected"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Restrict the runners to protected branches and tags.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "personal_access_token_stdin", Kind: "option", Opts: []string{"--personal-access-token-stdin"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Read your personal access token from standard input instead of a hidden prompt.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org install runner delete":
		return docgen.Description{
			Help: "Remove MACHINE_TYPE's runner configuration from Avrea.\n" +
				"\n" +
				"The runner object in GitLab remains for the group Owner to remove.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    deleted, installation_id, machine_type\n",
			Short:          "Remove MACHINE_TYPE's runner configuration from Avrea.",
			CompletionHelp: "Remove MACHINE_TYPE's runner configuration...",
			Usage:          []string{"[OPTIONS]", "MACHINE_TYPE"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "machine_type", Kind: "argument", Opts: []string{"machine_type"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "installation_id", Kind: "option", Opts: []string{"--installation-id"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("GitLab installation ID (ins-xxx format).")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "confirmed", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip the confirmation prompt.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org install runner list":
		return docgen.Description{
			Help: "List all machine types, runner status, and exact GitLab setup instructions.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    data, setup\n",
			Short:          "List all machine types, runner status, and exact GitLab setup instructions.",
			CompletionHelp: "List all machine types, runner status, and...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "installation_id", Kind: "option", Opts: []string{"--installation-id"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("GitLab installation ID (ins-xxx format).")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org install runner set":
		return docgen.Description{
			Help: "Verify and configure MACHINE_TYPE, replacing its previous runner if present.\n" +
				"\n" +
				"Read the token from a hidden prompt or --runner-token-stdin.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    architecture, configured, cpu_count, description, disk_gb,\n" +
				"    invalid_at, machine_type, memory_mb, paused, run_untagged,\n" +
				"    runner_id, status, tag, token_expires_at, verified_at\n",
			Short:          "Verify and configure MACHINE_TYPE, replacing its previous runner if present.",
			CompletionHelp: "Verify and configure MACHINE_TYPE,...",
			Usage:          []string{"[OPTIONS]", "MACHINE_TYPE"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "machine_type", Kind: "argument", Opts: []string{"machine_type"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "installation_id", Kind: "option", Opts: []string{"--installation-id"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("GitLab installation ID (ins-xxx format).")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "runner_token_stdin", Kind: "option", Opts: []string{"--runner-token-stdin"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Read the runner authentication token from standard input.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org install runner":
		return docgen.Description{
			Help:           "Manage GitLab runners, one per Linux machine type.",
			Short:          "Manage GitLab runners, one per Linux machine type.",
			CompletionHelp: "Manage GitLab runners, one per Linux...",
			Usage:          []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections:       true,
			Commands: []docgen.HelpCommand{
				{Name: "create", Description: "Create group runners in GitLab and configure their machine types in Avrea.", AutoShort: true},
				{Name: "delete", Description: "Remove MACHINE_TYPE's runner configuration from Avrea.", AutoShort: true},
				{Name: "list", Description: "List all machine types, runner status, and exact GitLab setup instructions.", AutoShort: true},
				{Name: "set", Description: "Verify and configure MACHINE_TYPE, replacing its previous runner if present.", AutoShort: true},
			},
		}
	case "avr org install set-selection":
		return docgen.Description{
			Help: "Replace a GitLab installation's project selection.\n" +
				"\n" +
				"Use `--project-selection selected` without `--project-id` to select no\n" +
				"projects. This updates the selection only; the next project sync applies\n" +
				"it.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    access_token_expires_on, access_token_invalid_at,\n" +
				"    access_token_last_validated_at, created_at, credential_kind,\n" +
				"    gitlab_namespace_full_path, gitlab_namespace_id, gitlab_project_ids,\n" +
				"    installation_id, organization_id, project_selection_mode, configured_runner_count,\n" +
				"    degraded_runner_count, selected_project_count, state, target_type,\n" +
				"    updated_at, webhook_secret_rotated_at\n",
			Short:          "Replace a GitLab installation's project selection.",
			CompletionHelp: "Replace a GitLab installation's project...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "installation_id", Kind: "option", Opts: []string{"--installation-id"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("GitLab installation ID (ins-xxx format).")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "project_selection", Kind: "option", Opts: []string{"--project-selection"}, Type: "choice", Required: true, Nargs: 1, Help: descriptionText("Project selection mode."), Choices: []string{"all", "selected"}}, Suffix: "  [required]", CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "project_ids", Kind: "option", Opts: []string{"--project-id"}, Type: "integer range", Multiple: true, Nargs: 1, Help: descriptionText("GitLab numeric project ID to select (repeatable, up to 1,000).")}, Suffix: "  [x>=1]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org install":
		return docgen.Description{
			Help:     "Manage GitHub and GitLab installations.",
			Short:    "Manage GitHub and GitLab installations.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "add", Description: "Add a GitHub or GitLab installation.", AutoShort: true},
				{Name: "list", Description: "List accessible installations across all your organizations.", AutoShort: true},
				{Name: "remove", Description: "Remove an installation using its provider-specific lifecycle.", AutoShort: true},
				{Name: "rotate", Description: "Rotate a GitLab installation's access token and webhook secret.", AutoShort: true},
				{Name: "runner", Description: "Manage GitLab runners, one per Linux machine type.", AutoShort: true},
				{Name: "set-selection", Description: "Replace a GitLab installation's project selection.", AutoShort: true},
			},
		}
	case "avr org list":
		return docgen.Description{
			Help: "List organizations you belong to.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr org list\n" +
				"    avr org list --json slug,role\n" +
				"    avr org list --json '*' -q '.[] | select(.role == \"admin\")'\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    name, organization_id, role, slug\n",
			Short: "List organizations you belong to.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org members":
		return docgen.Description{
			Help: "List organization members (admin only).\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr org members\n" +
				"    avr org members --org org-abc123\n" +
				"    avr org members --json name,role\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    joined_at, name, role, user_id\n",
			Short: "List organization members (admin only).",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org saml configure":
		return docgen.Description{
			Help: "Create or replace SAML configuration from IdP metadata (admin only).\n" +
				"\n" +
				"METADATA is an IdP metadata XML file; pass - to read it from stdin.\n" +
				"Reconfiguring requires the complete metadata document again.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr org saml configure idp-metadata.xml\n" +
				"    cat idp-metadata.xml | avr org saml configure - --org acme\n" +
				"    avr org saml configure idp.xml --email-attribute mail \\\n" +
				"        --given-name-attribute firstName --family-name-attribute lastName\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    allow_idp_initiated, attr_email, attr_family_name, attr_given_name,\n" +
				"    attr_groups, created_at, default_role, idp_entity_id, idp_slo_url,\n" +
				"    idp_sso_url, is_enforced, jit_provisioning, name_id_format,\n" +
				"    organization_id, organization_saml_config_id, updated_at\n",
			Short:          "Create or replace SAML configuration from IdP metadata (admin only).",
			CompletionHelp: "Create or replace SAML configuration from...",
			Usage:          []string{"[OPTIONS]", "METADATA"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "attr_email", Kind: "option", Opts: []string{"--email-attribute", "--attr-email"}, Type: "text", Nargs: 1, Help: descriptionText("IdP attribute carrying the member email."), ShowDefault: true, Default: "email"}, Suffix: "  [default: email]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "attr_given_name", Kind: "option", Opts: []string{"--given-name-attribute", "--attr-given-name"}, Type: "text", Nargs: 1, Help: descriptionText("IdP given-name attribute.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "attr_family_name", Kind: "option", Opts: []string{"--family-name-attribute", "--attr-family-name"}, Type: "text", Nargs: 1, Help: descriptionText("IdP family-name attribute.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "attr_groups", Kind: "option", Opts: []string{"--groups-attribute", "--attr-groups"}, Type: "text", Nargs: 1, Help: descriptionText("IdP groups attribute.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "default_role", Kind: "option", Opts: []string{"--default-role"}, Type: "choice", Nargs: 1, Help: descriptionText("Role assigned to JIT-provisioned members."), ShowDefault: true, Default: "user", Choices: []string{"user", "admin", "billing_admin"}}, Suffix: "  [default: user]", CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "jit_provisioning", Kind: "option", Opts: []string{"--jit-provisioning"}, Secondary: []string{"--no-jit-provisioning"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Allow SAML to provision new members."), ShowDefault: true, Default: true}, Suffix: "  [default: jit-provisioning]", CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "allow_idp_initiated", Kind: "option", Opts: []string{"--allow-idp-initiated"}, Secondary: []string{"--no-allow-idp-initiated"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Allow sign-in initiated from the IdP."), ShowDefault: true, Default: false}, Suffix: "  [default: no-allow-idp-initiated]", CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "metadata", Kind: "argument", Opts: []string{"metadata"}, Type: "filename", Required: true, Nargs: 1}, CompletionKind: "File"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org saml enforcement":
		return docgen.Description{
			Help: "Enable or disable mandatory SAML sign-in (admin only).\n" +
				"\n" +
				"Enabling requires a configured SAML connection and at least one verified\n" +
				"company domain.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr org saml enforcement on\n" +
				"    avr org saml enforcement off --org acme\n",
			Short:          "Enable or disable mandatory SAML sign-in (admin only).",
			CompletionHelp: "Enable or disable mandatory SAML sign-in...",
			Usage:          []string{"[OPTIONS]", "{on|off}"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "state", Kind: "argument", Opts: []string{"state"}, Type: "choice", Required: true, Nargs: 1, Choices: []string{"on", "off"}}, CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org saml remove":
		return docgen.Description{
			Help: "Remove the organization's SAML configuration (admin only).\n" +
				"\n" +
				"Pass --yes to skip the confirmation prompt (required when prompts are\n" +
				"disabled for automation).\n",
			Short:          "Remove the organization's SAML configuration (admin only).",
			CompletionHelp: "Remove the organization's SAML...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "confirmed", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip confirmation prompt.")}, CompletionKind: "Bool"},
			},
		}
	case "avr org saml show":
		return docgen.Description{
			Help: "Show the current SAML configuration (admin only).\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    allow_idp_initiated, attr_email, attr_family_name, attr_given_name,\n" +
				"    attr_groups, created_at, default_role, idp_entity_id, idp_slo_url,\n" +
				"    idp_sso_url, is_enforced, jit_provisioning, name_id_format,\n" +
				"    organization_id, organization_saml_config_id, updated_at\n",
			Short:          "Show the current SAML configuration (admin only).",
			CompletionHelp: "Show the current SAML configuration (admin...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr org saml sp-metadata":
		return docgen.Description{
			Help: "Print Avrea's SAML service-provider metadata XML.\n" +
				"\n" +
				"Redirect stdout to a file for import into your identity provider.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr org saml sp-metadata > avrea-sp.xml\n" +
				"    avr org saml sp-metadata --org acme\n",
			Short:          "Print Avrea's SAML service-provider metadata XML.",
			CompletionHelp: "Print Avrea's SAML service-provider...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
			},
		}
	case "avr org saml test":
		return docgen.Description{
			Help: "Test the SAML connection in a browser (admin only).\n" +
				"\n" +
				"The test performs a real IdP sign-in and displays the parsed assertion\n" +
				"without creating a new Avrea session.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr org saml test\n" +
				"    avr org saml test --org acme --no-browser\n",
			Short:          "Test the SAML connection in a browser (admin only).",
			CompletionHelp: "Test the SAML connection in a browser...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "no_browser", Kind: "option", Opts: []string{"--no-browser"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print the test URL without opening a browser.")}, CompletionKind: "Bool"},
			},
		}
	case "avr org saml":
		return docgen.Description{
			Help:           "Configure SAML single sign-on for an organization.",
			Short:          "Configure SAML single sign-on for an organization.",
			CompletionHelp: "Configure SAML single sign-on for an...",
			Usage:          []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections:       true,
			Commands: []docgen.HelpCommand{
				{Name: "configure", Description: "Create or replace SAML configuration from IdP metadata (admin only).", AutoShort: true},
				{Name: "enforcement", Description: "Enable or disable mandatory SAML sign-in (admin only).", AutoShort: true},
				{Name: "remove", Description: "Remove the organization's SAML configuration (admin only).", AutoShort: true},
				{Name: "show", Description: "Show the current SAML configuration (admin only).", AutoShort: true},
				{Name: "sp-metadata", Description: "Print Avrea's SAML service-provider metadata XML.", AutoShort: true},
				{Name: "test", Description: "Test the SAML connection in a browser (admin only).", AutoShort: true},
			},
		}
	case "avr org":
		return docgen.Description{
			Help:     "Manage organizations and installations.",
			Short:    "Manage organizations and installations.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "create", Description: "Create a new organization.", AutoShort: true},
				{Name: "email-domain", Description: "Claim and verify organization email domains.", AutoShort: true},
				{Name: "install", Description: "Manage GitHub and GitLab installations.", AutoShort: true},
				{Name: "list", Description: "List organizations you belong to.", AutoShort: true},
				{Name: "members", Description: "List organization members (admin only).", AutoShort: true},
				{Name: "saml", Description: "Configure SAML single sign-on for an organization.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
