// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func vmDescription(path string) docgen.Description {
	switch path {
	case "avr vm bootstrap":
		return docgen.Description{
			Help: "Set up a RUNNING VM with your dev essentials over SSH.\n" +
				"\n" +
				"\b\n" +
				"Each selected step runs on the VM and streams its output; bootstrap stops at\n" +
				"the first failure. Secrets (GitHub token, forwarded env values, agent\n" +
				"credentials) ride SSH stdin, never argv. A forwarded CLAUDE_CODE_OAUTH_TOKEN\n" +
				"is a year-long, subscription-wide bearer, so it is only carried under the\n" +
				"explicit --forward-agent-creds opt-in. Bootstrap changes survive pause\n" +
				"and resume on the preserved disk. Example:\n" +
				"\n" +
				"\b\n" +
				"    avr vm bootstrap cvm-abc123 --setup-github --install claude,codex \\\n" +
				"      --repo https://github.com/me/project --env AWS_REGION=eu-north-1\n",
			Short:          "Set up a RUNNING VM with your dev essentials over SSH.",
			CompletionHelp: "Set up a RUNNING VM with your dev...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "identity_file", Kind: "option", Opts: []string{"-i", "--identity"}, Type: "path", Nargs: 1, Help: descriptionText("Private key file to pass to ssh as -i.")}, CompletionKind: "Path", FileOkay: true},
				{Param: docgen.Param{Name: "setup_github", Kind: "option", Opts: []string{"--setup-github"}, Secondary: []string{"--no-setup-github"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Forward your local `gh auth token` into the VM (gh auth login --with-token + gh auth setup-git)."), ShowDefault: true, Default: false}, Suffix: "  [default: no-setup-github]", CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "install_agents", Kind: "option", Opts: []string{"--install"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Install an agent CLI (repeatable or comma-separated): claude, codex.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "forward_agent_creds", Kind: "option", Opts: []string{"--forward-agent-creds"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Forward the installed agents' credentials from your environment (ANTHROPIC_API_KEY / CLAUDE_CODE_OAUTH_TOKEN / OPENAI_API_KEY). If claude has none, offers to run `claude setup-token`."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "install_avr", Kind: "option", Opts: []string{"--install-avr"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Install the native avr release matching this CLI in ~/.local/bin (requires curl, tar and SHA-256 tooling)."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "repo_url", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Clone this git repo into the VM's home directory.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_ref", Kind: "option", Opts: []string{"--ref"}, Type: "text", Nargs: 1, Help: descriptionText("Branch to check out after cloning (requires --repo). Tags, PR refs, and raw SHAs are unsupported.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "dotfiles_url", Kind: "option", Opts: []string{"--dotfiles"}, Type: "text", Nargs: 1, Help: descriptionText("Clone this dotfiles repo and run its installer.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "env_flags", Kind: "option", Opts: []string{"--env"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Set an env var in the VM: KEY=VALUE, or a bare KEY to forward it from your environment. Repeatable.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "run_raw", Kind: "option", Opts: []string{"--run"}, Type: "text", Nargs: 1, Help: descriptionText("Run a custom script last: an inline script, or @path to a file.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "print_only", Kind: "option", Opts: []string{"--print"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print the ordered plan (secrets redacted) without running.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm capabilities":
		return docgen.Description{
			Help:           "List available operating systems, architectures, sizes, and quotas.",
			Short:          "List available operating systems, architectures, sizes, and quotas.",
			CompletionHelp: "List available operating systems,...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit capabilities and quotas as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm create":
		return docgen.Description{
			Help: "Create a long-running VM.\n" +
				"\n" +
				"\b\n" +
				"Provisioning is asynchronous: poll `avr vm show <id>` until the state is\n" +
				"RUNNING and endpoints are populated, or pass --wait to block until then and\n" +
				"print a ready-to-paste connect command with the password baked in. The\n" +
				"response carries a one-time password for the VM's local account; save it\n" +
				"now, it is never stored.\n" +
				"\n" +
				"\b\n" +
				"With --from-spawn-script it creates an agent sandbox for a coding-agent\n" +
				"vendor's work order instead, for that vendor's controller to run. For\n" +
				"example, as Claude Code's spawn-runner hook:\n" +
				"    exec avr vm create --from-spawn-script claude-code --org <org-id>\n",
			Short: "Create a long-running VM.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "display_name", Kind: "option", Opts: []string{"--name"}, Type: "text", Nargs: 1, Help: descriptionText("Unique VM name. Generated by the server when omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "os_type", Kind: "option", Opts: []string{"--os"}, Type: "choice", Nargs: 1, Help: descriptionText("Guest operating system. Required unless --from-spawn-script, which is always linux."), Choices: []string{"linux", "macos", "windows"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "architecture", Kind: "option", Opts: []string{"--arch"}, Type: "text", Nargs: 1, Help: descriptionText("CPU architecture (see: avr vm capabilities). Defaults to the selected OS's default.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "idempotency_key", Kind: "option", Opts: []string{"--idempotency-key"}, Type: "text", Nargs: 1, Help: descriptionText("Retry creates safely with the same organization-scoped key.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "https_ports", Kind: "option", Opts: []string{"--https-port"}, Type: "integer range", Multiple: true, Nargs: 1, Help: descriptionText("Publish a guest port as public HTTPS. Repeatable; the first is default.")}, Suffix: "  [1<=x<=65535]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "os_version", Kind: "option", Opts: []string{"--os-version"}, Type: "text", Nargs: 1, Help: descriptionText("Guest OS version (e.g. ubuntu-26.04; see: avr vm capabilities). Defaults to the latest version for the chosen --os.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "size", Kind: "option", Opts: []string{"--size"}, Type: "text", Nargs: 1, Help: descriptionText("Hardware tier, e.g. 2-vcpu (see: avr vm capabilities). Availability: linux x86_64 1-32 vCPU, linux arm64 up to 16, macos 8/16, windows 2-16. Required unless --from-spawn-script, which defaults to 4-vcpu.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "ssh_keys", Kind: "option", Opts: []string{"--ssh-key"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("SSH public key, or @path to read one from a file. Repeatable.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "remote_desktop", Kind: "option", Opts: []string{"--remote-desktop"}, Secondary: []string{"--no-remote-desktop"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Enable a remote desktop: RDP (Windows, Linux) or VNC (macOS Screen Sharing). Availability depends on OS version; the server validates."), ShowDefault: true, Default: false}, Suffix: "  [default: no-remote-desktop]", CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "ttl", Kind: "option", Opts: []string{"--ttl"}, Type: "text", Nargs: 1, Help: descriptionText("TTL before pause, or deletion when pause is unavailable (e.g. 8h, 7d, 1800s). Default 8h, max 7d.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "egress_rules_raw", Kind: "option", Opts: []string{"--egress-rules"}, Type: "text", Nargs: 1, Help: descriptionText("Per-VM egress firewall rules as a JSON array, or @path to a JSON file.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Git repository (owner/repo) to preload into the VM at boot. Best-effort; the checkout is warmed from Avrea's mirror when available.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "ref", Kind: "option", Opts: []string{"--ref"}, Type: "text", Nargs: 1, Help: descriptionText("Branch to preload (default: the repository's default branch). Requires --repo. Tags, pull-request refs, and raw commit SHAs are not supported.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "fetch_depth", Kind: "option", Opts: []string{"--fetch-depth"}, Type: "integer range", Nargs: 1, Help: descriptionText("Git history to preload with --repo, like actions/checkout fetch-depth: 1 (default) = branch tip, N = N commits, 0 = all branches and tags.")}, Suffix: "  [x>=0]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "disable_cache", Kind: "option", Opts: []string{"--disable-cache"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Disable a build/CI cache on this VM (repeatable, or comma-separated). Narrowing only: a VM can turn off an inherited cache but not turn one on. e.g. gha, packages, bazel, gradle, maven, turbo, nx, go-build (or a raw cache.<name>.enabled key). Repository-scoped caches require --repo.")}, Label: "--disable-cache CACHE", CompletionKind: "String"},
				{Param: docgen.Param{Name: "ephemeral", Kind: "option", Opts: []string{"--ephemeral"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Ignored compatibility flag."), Hidden: true, Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "wait", Kind: "option", Opts: []string{"--wait"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Wait until the VM is RUNNING, then print a ready-to-paste connect command with the password baked in."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "wait_timeout", Kind: "option", Opts: []string{"--wait-timeout"}, Type: "integer", Nargs: 1, Help: descriptionText("Seconds to wait when --wait is set."), ShowDefault: true, Default: 300}, Suffix: "  [default: 300]", CompletionKind: "Int"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit the raw API response (VM plus one-time password) as JSON.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "spawn_provider", Kind: "option", Opts: []string{"--from-spawn-script"}, Type: "choice", Nargs: 1, Help: descriptionText("Run as a coding-agent vendor controller's spawn script: read the work order the controller passes in its environment, create an agent sandbox that starts the vendor's worker, print its ID, and exit 0 when created or already created, 1 to have the vendor retry, 2 or higher when refused. Needs an organization ID (org-...) from --org or the default org."), Choices: []string{"claude-code"}}, CompletionKind: "Choice", CaseSensitive: true},
			},
		}
	case "avr vm delete":
		return docgen.Description{
			Help:  "Delete a VM. Asynchronous while live: shows DELETING until the node confirms the stop.",
			Short: "Delete a VM.",
			Usage: []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"--yes", "-y"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip the confirmation prompt.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "wait", Kind: "option", Opts: []string{"--wait"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Wait until the VM is fully deleted before returning."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "wait_timeout", Kind: "option", Opts: []string{"--wait-timeout"}, Type: "integer", Nargs: 1, Help: descriptionText("Seconds to wait when --wait is set."), ShowDefault: true, Default: 300}, Suffix: "  [default: 300]", CompletionKind: "Int"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit the raw API response as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm fork":
		return docgen.Description{
			Help: "Boot a copy of a PAUSED VM's disk; the source remains paused.\n" +
				"\n" +
				"Copies the source's machine configuration and disk, including its SSH host\n" +
				"keys and password, but discards memory state. A scoped-token password may\n" +
				"be replaced without returning its replacement. No password is returned; use\n" +
				"update --rotate-password if needed. Requires admin access to the source.\n",
			Short:          "Boot a copy of a PAUSED VM's disk; the source remains paused.",
			CompletionHelp: "Boot a copy of a PAUSED VM's disk; the...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "display_name", Kind: "option", Opts: []string{"--name"}, Type: "text", Nargs: 1, Help: descriptionText("Fork name. Defaults to a name derived from the source.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "ttl", Kind: "option", Opts: []string{"--ttl"}, Type: "text", Nargs: 1, Help: descriptionText("Fork TTL (e.g. 8h, 7d). Default 8h, max 7d.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "wait", Kind: "option", Opts: []string{"--wait"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Wait for the fork to become connectable.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "wait_timeout", Kind: "option", Opts: []string{"--wait-timeout"}, Type: "integer range", Nargs: 1, Help: descriptionText("Seconds to wait when --wait is set."), ShowDefault: true, Default: 300}, Suffix: "  [default: 300; x>=0]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit the fork VM as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm list":
		return docgen.Description{
			Help:           "List the organization's VMs, newest first (deleted ones excluded).",
			Short:          "List the organization's VMs, newest first (deleted ones excluded).",
			CompletionHelp: "List the organization's VMs, newest first...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "state", Kind: "option", Opts: []string{"--state"}, Type: "choice", Nargs: 1, Help: descriptionText("Filter by lifecycle state."), Choices: []string{"PENDING", "STARTING", "RUNNING", "PAUSING", "PAUSED", "STOPPING", "DELETING", "FAILED"}}, Label: "--state [pending|starting|running|pausing|paused|stopping|deleting|failed]", CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "scope", Kind: "option", Opts: []string{"--scope"}, Type: "choice", Nargs: 1, Help: descriptionText("Visible VMs or only your own."), Default: "all", Choices: []string{"all", "mine"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "sandbox", Kind: "option", Opts: []string{"--sandbox"}, Secondary: []string{"--no-sandbox"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Agent sandboxes only, or human-created VMs only.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max VMs to return."), ShowDefault: true, Default: 50}, Suffix: "  [default: 50; 1<=x<=100]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Pagination cursor from a previous response.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit the VM list as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm metrics":
		return docgen.Description{
			Help:           "Show CPU/memory/IO metrics for a VM's current or last run.",
			Short:          "Show CPU/memory/IO metrics for a VM's current or last run.",
			CompletionHelp: "Show CPU/memory/IO metrics for a VM's...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "sources", Kind: "option", Opts: []string{"--source"}, Type: "choice", Multiple: true, Nargs: 1, Help: descriptionText("Metric source. Repeatable; defaults to cpu and memory."), Choices: []string{"cpu", "memory", "filesystem", "load", "disk-io", "disk-ops", "network"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "start", Kind: "option", Opts: []string{"--start"}, Type: "integer", Nargs: 1, Help: descriptionText("Start time in Unix seconds (single-shot only).")}, CompletionKind: "Int"},
				{Param: docgen.Param{Name: "end", Kind: "option", Opts: []string{"--end"}, Type: "integer", Nargs: 1, Help: descriptionText("End time in Unix seconds (single-shot only).")}, CompletionKind: "Int"},
				{Param: docgen.Param{Name: "watch", Kind: "option", Opts: []string{"--watch", "-w"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Stream live metrics until the run completes; Ctrl-C to exit.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit metrics JSON; watch emits one snapshot per line.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm pause":
		return docgen.Description{
			Help: "Pause a RUNNING Linux x86_64 VM while preserving its disk.\n" +
				"\n" +
				"By default the snapshot is disk-only: the filesystem survives, but resume\n" +
				"performs a fresh boot and running processes do not. Pass --memory to also\n" +
				"preserve guest memory and continue processes from where they stopped.\n",
			Short:          "Pause a RUNNING Linux x86_64 VM while preserving its disk.",
			CompletionHelp: "Pause a RUNNING Linux x86_64 VM while...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "memory", Kind: "option", Opts: []string{"--memory"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Include guest memory and device state so running processes continue on resume."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "wait", Kind: "option", Opts: []string{"--wait"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Wait until the VM reaches PAUSED before returning."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "wait_timeout", Kind: "option", Opts: []string{"--wait-timeout"}, Type: "integer", Nargs: 1, Help: descriptionText("Seconds to wait when --wait is set."), ShowDefault: true, Default: 300}, Suffix: "  [default: 300]", CompletionKind: "Int"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit the raw API response as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm port-forward":
		return docgen.Description{
			Help: "Forward one or more local ports to TCP ports on the VM over SSH.\n" +
				"\n" +
				"The generic primitive behind `avr vm rdp` / `avr vm vnc`: opens\n" +
				"127.0.0.1:<local> -> <VM>:<guest> through the VM's SSH endpoint for each\n" +
				"--port, and holds them open until Ctrl-C. Bring your own client.\n" +
				"\n" +
				"\b\n" +
				"    avr vm port-forward cvm-abc123 --port 8080\n" +
				"    avr vm port-forward cvm-abc123 --port 8080 --port 5432 --port 9000:3000\n",
			Short:          "Forward one or more local ports to TCP ports on the VM over SSH.",
			CompletionHelp: "Forward one or more local ports to TCP...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "port_specs", Kind: "option", Opts: []string{"--port"}, Type: "text", Required: true, Multiple: true, Nargs: 1, Help: descriptionText("Guest TCP port to forward, optionally with a local bind port (e.g. 8080 or 9000:8080). Repeatable.")}, Label: "--port [LOCAL:]GUEST", Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "local_port", Kind: "option", Opts: []string{"--local-port"}, Type: "integer range", Nargs: 1, Help: descriptionText("Local port to bind for a single bare --port (with multiple ports, use --port LOCAL:GUEST instead).")}, Suffix: "  [1<=x<=65535]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "identity_file", Kind: "option", Opts: []string{"-i", "--identity"}, Type: "path", Nargs: 1, Help: descriptionText("Private key file to pass to ssh as -i.")}, CompletionKind: "Path", FileOkay: true},
				{Param: docgen.Param{Name: "print_only", Kind: "option", Opts: []string{"--print"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print the ssh command and exit, without opening the tunnel.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm rdp":
		return docgen.Description{
			Help: "Open an RDP desktop on a Windows or Linux VM over an SSH tunnel.\n" +
				"\n" +
				"Forwards a local port to the guest's RDP service (:3389) through the VM's\n" +
				"SSH endpoint, so the desktop is never exposed publicly. Holds the tunnel\n" +
				"open until Ctrl-C; pass --launch to also start a local RDP client.\n",
			Short:          "Open an RDP desktop on a Windows or Linux VM over an SSH tunnel.",
			CompletionHelp: "Open an RDP desktop on a Windows or Linux...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "local_port", Kind: "option", Opts: []string{"--local-port"}, Type: "integer range", Nargs: 1, Help: descriptionText("Local port to bind (default: an unused port).")}, Suffix: "  [1<=x<=65535]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "identity_file", Kind: "option", Opts: []string{"-i", "--identity"}, Type: "path", Nargs: 1, Help: descriptionText("Private key file to pass to ssh as -i.")}, CompletionKind: "Path", FileOkay: true},
				{Param: docgen.Param{Name: "launch", Kind: "option", Opts: []string{"--launch"}, Secondary: []string{"--no-launch"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Also start a local RDP client, instead of just printing the connect command."), ShowDefault: true, Default: false}, Suffix: "  [default: no-launch]", CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "print_only", Kind: "option", Opts: []string{"--print"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print the tunnel and client commands and exit, without opening the tunnel.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm resume":
		return docgen.Description{
			Help: "Resume a PAUSED VM from its preserved snapshot.\n" +
				"\n" +
				"The existing password remains unless resume withdraws credentials supplied\n" +
				"to a scoped token. Use update --rotate-password to obtain a fresh one.\n" +
				"If a memory restore cannot complete,\n" +
				"retry with --discard-memory to boot the preserved disk without restoring\n" +
				"running processes.\n",
			Short:          "Resume a PAUSED VM from its preserved snapshot.",
			CompletionHelp: "Resume a PAUSED VM from its preserved...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "discard_memory", Kind: "option", Opts: []string{"--discard-memory"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Ignore saved memory state and fresh-boot the preserved disk (recovery for a stuck memory restore)."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "wait", Kind: "option", Opts: []string{"--wait"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Wait until the VM is RUNNING and connectable."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "wait_timeout", Kind: "option", Opts: []string{"--wait-timeout"}, Type: "integer", Nargs: 1, Help: descriptionText("Seconds to wait when --wait is set."), ShowDefault: true, Default: 300}, Suffix: "  [default: 300]", CompletionKind: "Int"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit the raw API response as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm rotate-https-label":
		return docgen.Description{
			Help: "Replace public HTTPS URLs to invalidate leaked URLs.\n" +
				"\n" +
				"Routes change once ingress applies the rotation. On a running VM,\n" +
				"AVREA_SANDBOX_URL (in new login shells) and /vm/info follow within seconds;\n" +
				"a paused VM picks up the new URLs at resume.\n",
			Short:          "Replace public HTTPS URLs to invalidate leaked URLs.",
			CompletionHelp: "Replace public HTTPS URLs to invalidate...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit the updated VM as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm show":
		return docgen.Description{
			Help:           "Show a VM's details, including connection endpoints and egress rules.",
			Short:          "Show a VM's details, including connection endpoints and egress rules.",
			CompletionHelp: "Show a VM's details, including connection...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit the full VM record (with egress rules) as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm ssh":
		return docgen.Description{
			Help: "Open an SSH session to a RUNNING VM, or run a command on it.\n" +
				"\n" +
				"With no extra arguments this opens an interactive session. Anything after\n" +
				"`--` is run as a remote command instead, e.g.:\n" +
				"\n" +
				"    avr vm ssh cvm-abc123 -- uname -a\n" +
				"\n" +
				"A one-off `-- <cmd>` runs in a non-login shell that sources no startup files,\n" +
				"so it won't see env forwarded by `avr vm bootstrap`. Pass `--login` to run it\n" +
				"in a login shell instead (e.g. so `claude` finds its subscription token):\n" +
				"\n" +
				"    avr vm ssh cvm-abc123 --login -- claude -p 'summarize the repo'\n" +
				"\n" +
				"Pass `--session <name>` to attach to (or create) a persistent tmux session,\n" +
				"so the shell and any long-running process in it survive a dropped\n" +
				"connection; reconnect with the same name to resume where you left off. A\n" +
				"`-- <cmd>` given with --session runs only when the session is first created;\n" +
				"reattaching to an existing session resumes it and does not rerun the command.\n" +
				"\n" +
				"When the VM's endpoint publishes a host key it is pinned, so the first\n" +
				"connect neither prompts nor is spoofable. If the endpoint has no host key,\n" +
				"`avr` prints a warning and falls back to trust-on-first-use, so this\n" +
				"spoofing protection is conditional rather than guaranteed. For\n" +
				"port-forwarding use `avr vm port-forward`.\n",
			Short:          "Open an SSH session to a RUNNING VM, or run a command on it.",
			CompletionHelp: "Open an SSH session to a RUNNING VM, or...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME", "[SSH_ARGS]..."},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "ssh_args", Kind: "argument", Opts: []string{"ssh_args"}, Type: "text", Nargs: -1}, CompletionKind: "Unprocessed"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "identity_file", Kind: "option", Opts: []string{"-i", "--identity"}, Type: "path", Nargs: 1, Help: descriptionText("Private key file to pass to ssh as -i.")}, CompletionKind: "Path", FileOkay: true},
				{Param: docgen.Param{Name: "session_name", Kind: "option", Opts: []string{"--session"}, Type: "text", Nargs: 1, Help: descriptionText("Attach to (or create) a persistent tmux session by this name, so the shell and any long-running process in it survive a dropped connection. Reconnect with the same --session. A `-- <cmd>` runs only when the session is created, not on reattach.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "login", Kind: "option", Opts: []string{"--login"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Run the `-- <cmd>` in a login shell (bash -lc) so it sees bootstrap-forwarded env like CLAUDE_CODE_OAUTH_TOKEN. Needed for a non-interactive command; a plain `ssh host cmd` shell sources nothing."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "print_only", Kind: "option", Opts: []string{"--print"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print the ssh command instead of running it.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm ssh-config":
		return docgen.Description{
			Help: "Print (or --append) an ssh_config Host block for a RUNNING VM.\n" +
				"\n" +
				"Reach the VM with plain `ssh`, scp/rsync, and VS Code / Cursor Remote-SSH,\n" +
				"host key pinned, without wrapping each tool. Redirect it yourself:\n" +
				"\n" +
				"    avr vm ssh-config cvm-abc123 >> ~/.ssh/config\n" +
				"\n" +
				"or let --append manage the block for you (idempotent — re-run after a\n" +
				"resume to refresh the endpoint in place):\n" +
				"\n" +
				"    avr vm ssh-config cvm-abc123 --append\n" +
				"\n" +
				"The block references a dedicated known_hosts file that this command writes\n" +
				"the pinned host key into (one entry per VM). If the endpoint publishes no\n" +
				"host key, the block falls back to accept-new (trust-on-first-use) and a\n" +
				"warning is printed.\n",
			Short:          "Print (or --append) an ssh_config Host block for a RUNNING VM.",
			CompletionHelp: "Print (or --append) an ssh_config Host...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "identity_file", Kind: "option", Opts: []string{"-i", "--identity"}, Type: "path", Nargs: 1, Help: descriptionText("IdentityFile to write into the block.")}, CompletionKind: "Path", FileOkay: true},
				{Param: docgen.Param{Name: "host_alias", Kind: "option", Opts: []string{"--host-alias"}, Type: "text", Nargs: 1, Help: descriptionText("Host alias for the block (default: avr-<vm-id>).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "known_hosts_file", Kind: "option", Opts: []string{"--known-hosts-file"}, Type: "path", Nargs: 1, Help: descriptionText("Where to pin the host key (default: ~/.ssh/avr_known_hosts).")}, CompletionKind: "Path", FileOkay: true},
				{Param: docgen.Param{Name: "append", Kind: "option", Opts: []string{"--append"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Write the block into your SSH config (default: ~/.ssh/config) instead of printing it, replacing any prior block for the same alias in place."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "config_file", Kind: "option", Opts: []string{"--config-file"}, Type: "path", Nargs: 1, Help: descriptionText("SSH config file for --append (default: ~/.ssh/config).")}, CompletionKind: "Path", FileOkay: true},
			},
		}
	case "avr vm update":
		return docgen.Description{
			Help: "Update a VM's name, TTL, or SSH keys, or rotate its password.\n" +
				"\n" +
				"Use avr vm pause, avr vm resume, or avr vm delete for lifecycle changes.\n",
			Short:          "Update a VM's name, TTL, or SSH keys, or rotate its password.",
			CompletionHelp: "Update a VM's name, TTL, or SSH keys, or...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "display_name", Kind: "option", Opts: []string{"--name"}, Type: "text", Nargs: 1, Help: descriptionText("New display name.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "ttl", Kind: "option", Opts: []string{"--ttl"}, Type: "text", Nargs: 1, Help: descriptionText("Extend the TTL window from now (e.g. 8h, 7d). Max 7d.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "ssh_keys", Kind: "option", Opts: []string{"--ssh-key"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Replace stored SSH public keys (literal or @path). Repeatable. Applies live on a RUNNING VM, otherwise at next resume.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "rotate_password", Kind: "option", Opts: []string{"--rotate-password"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Provision a fresh one-time password (returned in the response)."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "egress_rules_raw", Kind: "option", Opts: []string{"--egress-rules"}, Type: "text", Nargs: 1, Help: descriptionText("Replace the per-VM egress rules with this JSON array (or @path to a file).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit the raw API response as JSON.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "https_ports", Kind: "option", Opts: []string{"--https-port"}, Type: "integer range", Multiple: true, Nargs: 1, Help: descriptionText("Replace public HTTPS ports. Repeatable; applies live on a RUNNING VM, otherwise at next start or resume.")}, Suffix: "  [1<=x<=65535]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "clear_https_ports", Kind: "option", Opts: []string{"--clear-https-ports"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Publish no HTTPS ports. Applies live on a RUNNING VM, otherwise at next start or resume.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm usage":
		return docgen.Description{
			Help: "Show usage metering (runtime / vCPU / memory seconds) per VM.\n" +
				"\n" +
				"Each power-on cycle's window is clipped to the requested period and summed.\n" +
				"Deleted VMs are included: usage survives deletion.\n",
			Short:          "Show usage metering (runtime / vCPU / memory seconds) per VM.",
			CompletionHelp: "Show usage metering (runtime / vCPU /...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "period_start", Kind: "option", Opts: []string{"--start"}, Type: "datetime", Nargs: 1, Help: descriptionText("Inclusive period start (default: 30 days ago).")}, Label: "--start [%Y-%m-%d|%Y-%m-%dT%H:%M:%S]", CompletionKind: "DateTime"},
				{Param: docgen.Param{Name: "period_end", Kind: "option", Opts: []string{"--end"}, Type: "datetime", Nargs: 1, Help: descriptionText("Exclusive period end (default: now).")}, Label: "--end [%Y-%m-%d|%Y-%m-%dT%H:%M:%S]", CompletionKind: "DateTime"},
				{Param: docgen.Param{Name: "as_json", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit the usage report as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm vnc":
		return docgen.Description{
			Help: "Open a VNC desktop on a macOS VM (Screen Sharing) over an SSH tunnel.\n" +
				"\n" +
				"Forwards a local port to the guest's Screen Sharing service (:5900) through\n" +
				"the VM's SSH endpoint, so the desktop is never exposed publicly. Holds the\n" +
				"tunnel open until Ctrl-C; pass --launch to also open Screen Sharing.\n",
			Short:          "Open a VNC desktop on a macOS VM (Screen Sharing) over an SSH tunnel.",
			CompletionHelp: "Open a VNC desktop on a macOS VM (Screen...",
			Usage:          []string{"[OPTIONS]", "VM_ID_OR_NAME"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "vm_id", Kind: "argument", Opts: []string{"vm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "local_port", Kind: "option", Opts: []string{"--local-port"}, Type: "integer range", Nargs: 1, Help: descriptionText("Local port to bind (default: an unused port).")}, Suffix: "  [1<=x<=65535]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "identity_file", Kind: "option", Opts: []string{"-i", "--identity"}, Type: "path", Nargs: 1, Help: descriptionText("Private key file to pass to ssh as -i.")}, CompletionKind: "Path", FileOkay: true},
				{Param: docgen.Param{Name: "launch", Kind: "option", Opts: []string{"--launch"}, Secondary: []string{"--no-launch"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Also start a local VNC client (macOS Screen Sharing), instead of just printing the connect command."), ShowDefault: true, Default: false}, Suffix: "  [default: no-launch]", CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "print_only", Kind: "option", Opts: []string{"--print"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print the tunnel and client commands and exit, without opening the tunnel.")}, CompletionKind: "Bool"},
			},
		}
	case "avr vm":
		return docgen.Description{
			Help:  "Manage long-running VMs (SSH/RDP/VNC).",
			Short: "Manage long-running VMs (SSH/RDP/VNC).",
			Usage: []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Commands: []docgen.HelpCommand{
				{Name: "bootstrap", Description: "Set up a RUNNING VM with your dev essentials over SSH."},
				{Name: "capabilities", Description: "List available operating systems, architectures,..."},
				{Name: "create", Description: "Create a long-running VM."},
				{Name: "delete", Description: "Delete a VM."},
				{Name: "fork", Description: "Boot a copy of a PAUSED VM's disk; the source..."},
				{Name: "list", Description: "List the organization's VMs, newest first (deleted..."},
				{Name: "metrics", Description: "Show CPU/memory/IO metrics for a VM's current or..."},
				{Name: "pause", Description: "Pause a RUNNING Linux x86_64 VM while preserving..."},
				{Name: "port-forward", Description: "Forward one or more local ports to TCP ports on the..."},
				{Name: "rdp", Description: "Open an RDP desktop on a Windows or Linux VM over..."},
				{Name: "resume", Description: "Resume a PAUSED VM from its preserved snapshot."},
				{Name: "rotate-https-label", Description: "Replace public HTTPS URLs to invalidate leaked URLs."},
				{Name: "show", Description: "Show a VM's details, including connection endpoints..."},
				{Name: "ssh", Description: "Open an SSH session to a RUNNING VM, or run a..."},
				{Name: "ssh-config", Description: "Print (or --append) an ssh_config Host block for a..."},
				{Name: "update", Description: "Update a VM's name, TTL, or SSH keys, or rotate its..."},
				{Name: "usage", Description: "Show usage metering (runtime / vCPU / memory..."},
				{Name: "vnc", Description: "Open a VNC desktop on a macOS VM (Screen Sharing)..."},
			},
		}
	}
	return docgen.Description{}
}
