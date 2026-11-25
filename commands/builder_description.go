// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func builderDescription(path string) docgen.Description {
	switch path {
	case "avr builder list":
		return docgen.Description{
			Help: "List builder sessions, newest first.\n" +
				"\n" +
				"Sessions are never hard-deleted, so terminal (STOPPED/FAILED) sessions\n" +
				"keep appearing here; filter with --state RUNNING to see only live ones.\n" +
				"Non-admin members see only their own sessions.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr builder list\n" +
				"    avr builder list --state RUNNING\n" +
				"    avr builder list --json builder_session_id,state,stop_at\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    builder_session_id, cpu_count, created_at, disk_gb, memory_mb, platform,\n" +
				"    repository_full_name, state, state_reason, stop_at, updated_at\n",
			Short: "List builder sessions, newest first.",
			Parameters: append([]docgen.Parameter{
				serviceTokenOrgParameter(),
				{Param: docgen.Param{Name: "state", Kind: "option", Opts: []string{"--state"}, Type: "choice", Nargs: 1, Help: descriptionText("Filter by state."), Choices: builderSessionStates}, Label: "--state [pending|starting|running|stopping|stopped|failed]", CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max sessions."), ShowDefault: true, Default: 50}, Suffix: "  [default: 50; 1<=x<=100]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Pagination cursor from a previous response.")}, CompletionKind: "String"},
			}, serviceTokenJSONParameters()...),
		}
	case "avr builder stop":
		return docgen.Description{
			Help: "Stop a builder session, reclaiming its VM and quota slot.\n" +
				"\n" +
				"Asynchronous while the VM is live: the session shows STOPPING until the\n" +
				"node confirms teardown. Stopping is idempotent — repeating it on an\n" +
				"already-stopped session succeeds.\n",
			Short: "Stop a builder session, reclaiming its VM and quota slot.",
			Usage: []string{"[OPTIONS]", "SESSION_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "session_id", Kind: "argument", Opts: []string{"session_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				serviceTokenOrgParameter(),
				{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip the confirmation prompt.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit the raw API response as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr builder":
		return docgen.Description{
			Help: "Manage remote builder sessions.\n" +
				"\n" +
				"Builder sessions are the ephemeral VMs behind `avr build`. `avr build`\n" +
				"normally creates and deletes its own, but a crashed client leaves its\n" +
				"session running until the TTL expires — `list` finds those, `stop`\n" +
				"reclaims them (and their quota) immediately.\n",
			Short:    "Manage remote builder sessions.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "list", Description: "List builder sessions, newest first.", AutoShort: true},
				{Name: "stop", Description: "Stop a builder session, reclaiming its VM and quota slot.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
