// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func sbomDescription(path string) docgen.Description {
	switch path {
	case "avr sbom download":
		return docgen.Description{
			Help: "Download an SBOM artifact (CycloneDX, SPDX, or dependency inventory).\n" +
				"\n" +
				"COMMIT is the commit of a recorded SBOM, as a full SHA or a unique prefix\n" +
				"of at least 7 characters such as the one `avr sbom list` shows. Defaults to\n" +
				"the latest SBOM.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr sbom download --repo acme/api\n" +
				"    avr sbom download --repo acme/api --format spdx --out api.spdx.json\n" +
				"    avr sbom download 3f2c9a1b7d04 --repo acme/api --out - | jq .components\n",
			Short:          "Download an SBOM artifact (CycloneDX, SPDX, or dependency inventory).",
			CompletionHelp: "Download an SBOM artifact (CycloneDX,...",
			Usage:          []string{"[OPTIONS]", "[COMMIT]"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "commit", Kind: "argument", Opts: []string{"commit"}, Type: "text", Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "commit_option", Kind: "option", Opts: []string{"--commit"}, Type: "text", Nargs: 1, Help: descriptionText("Same as the COMMIT argument.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "artifact_format", Kind: "option", Opts: []string{"--format"}, Type: "choice", Nargs: 1, Help: descriptionText("Artifact to download."), ShowDefault: true, Default: "cyclonedx", Choices: []string{"cyclonedx", "inventory", "spdx"}}, Suffix: "  [default: cyclonedx]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "out_path", Kind: "option", Opts: []string{"--out"}, Type: "text", Nargs: 1, Help: descriptionText("Output file path, or \"-\" for stdout. Defaults to the artifact filename in the current directory.")}, CompletionKind: "String"},
			},
		}
	case "avr sbom generate":
		return docgen.Description{
			Help: "Start SBOM generation for a repository.\n" +
				"\n" +
				"REF is the branch, tag, or commit SHA to analyse; an abbreviated SHA such\n" +
				"as the one `avr sbom list` shows works too. Defaults to the default-branch\n" +
				"tip. Without --wait, commit_sha is the full SHA of the commit the run\n" +
				"analyses when REF is a full SHA or one the API expanded.\n" +
				"\n" +
				"One analysis runs per repository at a time. A request for the same ref as\n" +
				"the analysis already running joins it; a request for a different ref is\n" +
				"rejected (HTTP 409) until that analysis finishes. Once an SBOM has been\n" +
				"recorded for the repository, a new run is refused (HTTP 429) for a\n" +
				"cooldown period, and the error says how many seconds remain.\n" +
				"\n" +
				"With --wait and a REF that names a commit, success means a new SBOM for\n" +
				"that commit is downloadable, whatever the analysis task's own outcome\n" +
				"(task_status); commit_sha and recorded_at identify it. A failed analysis\n" +
				"can still deliver its SBOM later, so such a wait runs to --wait-timeout\n" +
				"before reporting failure. For a branch, tag, or the default branch the API\n" +
				"does not say which commit the run resolved, so --wait reports the analysis\n" +
				"task's outcome only. Transient errors while waiting are retried until\n" +
				"--wait-timeout; a wait that times out leaves the analysis running.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr sbom generate --repo acme/api\n" +
				"    avr sbom generate v1.4.0 --repo acme/api\n" +
				"    avr sbom generate 3f2c9a1b7d04 --repo acme/api --wait\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    ai_task_id, commit_sha, error_code, recorded_at, status, task_status\n",
			Short: "Start SBOM generation for a repository.",
			Usage: []string{"[OPTIONS]", "[REF]"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "ref", Kind: "argument", Opts: []string{"ref"}, Type: "text", Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "ref_option", Kind: "option", Opts: []string{"--ref"}, Type: "text", Nargs: 1, Help: descriptionText("Same as the REF argument.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "wait", Kind: "option", Opts: []string{"--wait"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Wait until generation finishes before returning."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "wait_timeout", Kind: "option", Opts: []string{"--wait-timeout"}, Type: "integer range", Nargs: 1, Help: descriptionText("Seconds to wait when --wait is set."), ShowDefault: true, Default: 900}, Suffix: "  [default: 900; x>=1]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr sbom list":
		return docgen.Description{
			Help: "List the repository's recorded SBOMs, newest first.\n" +
				"\n" +
				"Counts shown as \"?\" were not recorded by the SBOM's schema version.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr sbom list --repo acme/api\n" +
				"    avr sbom list --repo acme/api --json commit_sha,copyleft_count\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    artifacts, branch, commit_sha, copyleft_count, generated_at,\n" +
				"    is_release, recorded_at, schema_version, total_dependencies,\n" +
				"    unresolved\n",
			Short:          "List the repository's recorded SBOMs, newest first.",
			CompletionHelp: "List the repository's recorded SBOMs,...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max SBOMs to return."), ShowDefault: true, Default: 50}, Suffix: "  [default: 50; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Opaque cursor from a previous response's next_cursor.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr sbom view":
		return docgen.Description{
			Help: "Show an SBOM's summary: dependency counts, licenses, and artifacts.\n" +
				"\n" +
				"COMMIT is the commit of a recorded SBOM, as a full SHA or a unique prefix\n" +
				"of at least 7 characters such as the one `avr sbom list` shows. Defaults to\n" +
				"the latest SBOM.\n" +
				"\n" +
				"A long license table is cut to the most common licenses and a long copyleft\n" +
				"list to its first names; --full lists all of them. Long license names are\n" +
				"clipped; the summary in --json carries them whole.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr sbom view --repo acme/api\n" +
				"    avr sbom view 3f2c9a1b7d04 --repo acme/api\n" +
				"    avr sbom view --repo acme/api --full\n" +
				"    avr sbom view --repo acme/api --json summary --jq .summary.licenses\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    artifacts, branch, commit_sha, generated_at, is_release, recorded_at,\n" +
				"    schema_version, summary\n",
			Short:          "Show an SBOM's summary: dependency counts, licenses, and artifacts.",
			CompletionHelp: "Show an SBOM's summary: dependency counts,...",
			Usage:          []string{"[OPTIONS]", "[COMMIT]"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "commit", Kind: "argument", Opts: []string{"commit"}, Type: "text", Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "commit_option", Kind: "option", Opts: []string{"--commit"}, Type: "text", Nargs: 1, Help: descriptionText("Same as the COMMIT argument.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "full", Kind: "option", Opts: []string{"--full"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("List every license and copyleft dependency in the summary. Long lists are otherwise cut to the 10 most common licenses and the first 10 copyleft dependencies."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr sbom":
		return docgen.Description{
			Help:           "View, generate, and download repository SBOMs.",
			Short:          "View, generate, and download repository SBOMs.",
			CompletionHelp: "View, generate, and download repository...",
			Usage:          []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections:       true,
			Commands: []docgen.HelpCommand{
				{Name: "download", Description: "Download an SBOM artifact (CycloneDX, SPDX, or dependency inventory).", AutoShort: true},
				{Name: "generate", Description: "Start SBOM generation for a repository.", AutoShort: true},
				{Name: "list", Description: "List the repository's recorded SBOMs, newest first.", AutoShort: true},
				{Name: "view", Description: "Show an SBOM's summary: dependency counts, licenses, and artifacts.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
