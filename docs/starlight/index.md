---
title: Avrea CLI
description: "Avrea on the command line."
---

<p align="center">
  <img src="https://raw.githubusercontent.com/avrea-com/cli/main/.github/banner.png" alt="Avrea CLI" width="640" />
</p>

# Avrea CLI

Official command-line client for [Avrea](https://avrea.com/). `avr` brings runs, jobs, logs, and workflow control into your terminal.

## Install

### Homebrew

```sh
brew install avrea-com/tap/avr
```

### PyPI

```sh
uv tool install avr-cli
```

`uv tool install` puts `avr` on your `PATH` in an isolated environment
and fetches Python 3.14 automatically if you don't have it. Or, with an
existing Python 3.14:

```sh
pip install avr-cli
```

### From source

```sh
git clone https://github.com/avrea-com/cli && cd cli && uv sync
uv run avr --version
```

### Verifying a release

Release artefacts (the binary tarballs and the PyPI wheel/sdist) carry keyless
[SLSA build provenance](https://slsa.dev/) signed via Sigstore. Verify a
downloaded artefact against this repository with the GitHub CLI:

```sh
gh attestation verify avr_0.1.5_linux_amd64.tar.gz --repo avrea-com/cli
```

A successful check confirms the artefact was built by this repository's release
workflow and has not been tampered with.

## Authenticate

Browser-based login (recommended):

```sh
avr auth login
```

Show who you are and where you're pointing:

```sh
avr auth status
avr config            # combined: host, auth, active org, default repo, all with sources
```

Log out (revokes the active API key on the server):

```sh
avr auth logout
```

For non-interactive environments (CI, scripts), set a token via env var instead. See [Configuration](#configuration).

## Your first command

Inside any git checkout of an Avrea-connected repository:

```sh
avr run list                           # uses git remote to scope the query
avr run view <run-id>                  # full run with jobs
avr run view <run-id> --log-failed     # jump straight to failed-step output
```

Auto-detection means most commands "just work" without `--repo`. Outside a git tree (or to override), pass `--repo org/name` or `--repo rep-xxx`.

## Common workflows

**Watch a run live**

```sh
avr run watch                          # auto-selects the latest active run for the current repo
avr run watch <run-id>                 # specific run
avr run watch <run-id> --exit-status   # propagate the run's success/failure into the shell exit code
```

**Triage a failure without leaving the terminal**

```sh
avr run view <run-id> --log-failed     # only the steps that failed, grouped by job
avr run logs <run-id>                  # full log dump, paginated
avr job logs <job-id> --follow         # tail one job in real time
```

**Trigger a workflow_dispatch**

```sh
avr workflow run ci.yml                                         # default branch
avr workflow run ci.yml --ref feat/x -f env=staging             # with inputs
avr workflow run "Build and Deploy" --watch --exit-status       # dispatch + watch + script-friendly exit
echo '{"env":"prod"}' | avr workflow run deploy.yml --json      # inputs from stdin
```

WORKFLOW accepts an Avrea ID (`wfl-...`), the GitHub numeric ID, a filename (`ci.yml`, `ci`), or the display name.

**SSH into a running job's VM**

```sh
avr job ssh <job-id>
avr job ssh <job-id> --print-command   # just print the ssh string
```

**Live VM metrics while a job runs**

```sh
avr job metrics <job-id> --watch       # CPU/memory/IO gauges, refreshed every few seconds
avr job metrics <job-id>               # static post-mortem after the job ended
```

**Long-running dev VMs**

Durable, org-scoped VMs reachable over SSH (plus RDP/VNC for a desktop), separate from job VMs. Create one with your key, then set it up in a single command:

```sh
avr vm create --name dev --os linux --size 2-vcpu \
  --ssh-key @~/.ssh/id_ed25519.pub --ephemeral --wait      # boot, wait for SSH, print connect
avr vm create --name ci --os linux --size 8-vcpu --ephemeral \
  --repo org/project --disable-cache gha,packages          # attach a repo, narrow which caches it uses
avr vm bootstrap <vm-id> --setup-github --install claude,codex \
  --repo https://github.com/org/project --env AWS_REGION=eu-north-1
avr vm ssh <vm-id>                                          # SSH session, host key pinned when published (or: -- <cmd>)
avr vm ssh <vm-id> --login -- claude -p 'summarize the repo' # one-off command in a login shell (sees forwarded env)
avr vm ssh <vm-id> --session dev                            # persistent tmux session, survives a dropped connection
avr vm ssh-config <vm-id> --append                          # add `ssh <alias>` (scp/rsync, VS Code Remote-SSH) to ~/.ssh/config
avr vm list ; avr vm stop <vm-id>                           # lifecycle
```

`bootstrap` runs each step over SSH and streams it live, feeding secrets (GitHub token, env values, agent credentials) on stdin rather than argv. `--forward-agent-creds` carries your local `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`, or a Claude subscription token — if none is set it offers to run `claude setup-token` and forwards the pasted `CLAUDE_CODE_OAUTH_TOKEN`. Disks are ephemeral, so re-run it after every `avr vm start`. `avr vm ssh` pins the VM's host key when its endpoint publishes one; if it doesn't, `avr` warns and falls back to trust-on-first-use — pass `--session <name>` to attach to (or create) a persistent tmux session that survives a dropped connection. A one-off `avr vm ssh -- <cmd>` runs in a non-login shell that sources no startup files, so pass `--login` when the command needs bootstrap-forwarded env (e.g. `claude`'s subscription token). `avr vm ssh-config` emits a host-key-pinned SSH config block so plain `ssh`, scp/rsync, and VS Code / Cursor Remote-SSH reach the VM without wrapping each tool — `--append` writes it into `~/.ssh/config` for you (idempotent: re-run after a restart to refresh the endpoint in place). For a desktop, `avr vm rdp` / `avr vm vnc` tunnel over the same SSH endpoint, and `avr vm port-forward` is the generic primitive (repeat `--port` to forward several ports over one connection). A VM created with `--repo` inherits that repository's build/CI caches; `--disable-cache <name>` narrows them per VM (it can turn an inherited cache off, never on).

**Cancel or rerun a run**

```sh
avr run cancel <run-id>
avr run rerun <run-id>
avr run rerun <run-id> --failed        # only failed jobs
```

**Workflow stats and the slowest steps**

```sh
avr workflow list                      # aggregate stats per workflow over the last 30d
avr workflow view <wfl-id>             # per-job p95/median/failure breakdown + recent runs
avr status                             # org-wide health: recent runs, slowest workflows, cache usage
```

**Cache**

```sh
avr cache list --repo <repo>           # entries
avr cache usage --repo <repo>          # size vs quota
avr cache delete --key <key> --type <type> --repo <repo>
```

## Scripting & automation

Every list/view command supports `--json` for structured output.

**Discover available fields**

```sh
avr run list --json '?'                # prints the field schema
```

**Select specific fields**

```sh
avr run list --json status,conclusion,head_branch
avr run list --json '*'                # all fields
```

**Pipe through `jq`**

```sh
avr run list --json '*' -q '.[] | select(.conclusion == "failure") | .run_id'
avr workflow list --json name,runs -q '.[] | "\(.runs)\t\(.name)"'
```

`-q/--jq` is built in (no need to pipe to `jq` yourself), but the schema-projected output is also `jq`-friendly.

**Detect "auth required" in scripts**

```sh
avr run list >/dev/null
case $? in
  0) ;;
  4) echo "logged out, re-auth needed"; exit 4 ;;
  *) echo "transient failure" ;;
esac
```

Exit code `4` is reserved for "auth required". `1` is general failure; `2` is a usage error.

**Pipe-aware output**

When stdout isn't a TTY, list commands switch to tab-separated rows (no color, no truncation, ISO timestamps), so `avr run list | awk` works without flags. `avr run watch | jq -c .` automatically switches to NDJSON event mode.

## Configuration

### Environment variables

| Variable         | Purpose                                                                                          |
| ---------------- | ------------------------------------------------------------------------------------------------ |
| `AVR_HOST`       | Avrea API URL. Defaults to the active host in `hosts.json`, then `https://api.avrea.com`.        |
| `AVR_TOKEN`      | API key. Overrides whatever's stored for the active host.                                         |
| `AVR_ORG`        | Default organization ID. Overrides the stored default.                                           |
| `AVR_REPO`       | Default repository (`org/name` or `rep-xxx`). Overrides git auto-detect.                          |
| `AVR_BROWSER`    | Browser to launch for `--web` and OAuth login. Falls back to `BROWSER`, then system default.      |
| `AVR_PAGER`      | Pager for long output. Overrides `PAGER`. Set to empty string to disable paging.                  |
| `AVR_LINKS`      | Set to `0` to disable OSC 8 hyperlinks. Same as `--no-links`.                                     |
| `AVR_DEBUG`      | Comma-separated debug categories. Set to `1`/`true` for general debug logging.                    |
| `AVR_PROMPT_DISABLED` | Refuse interactive prompts (scripts fail fast instead of hanging on stdin).                  |
| `AVR_CONFIG_DIR` | Override the config directory. Defaults to `$XDG_CONFIG_HOME/avrea` or `~/.config/avrea`.         |
| `AVR_MACOS_NATIVE_PATHS` | Set to `1` on macOS to use `~/Library/...` instead of the default `~/.config/avrea`.       |
| `NO_COLOR`       | Disable colored output (any non-empty value). Same as `--no-color`.                                |

### Config commands

```sh
avr config                        # status: host, auth, org, repo with sources
avr config list                   # same as bare `avr config`
avr config set org <org-id>       # store a default organization
avr config get org
avr config unset org
```

Persistent state lives in `$XDG_CONFIG_HOME/avrea/hosts.json` (host-keyed credentials and per-host defaults).

### Switching hosts

```sh
avr auth switch                   # interactive picker across stored hosts
AVR_HOST=https://beta.api.avrea.com avr run list   # one-shot override
```

## Tips

**Open anything on the console**

```sh
avr run view <run-id> --web
avr workflow view <wfl-id> --web
avr cache list --repo <repo> --web
```

**Click-to-open in your terminal**

IDs in tables are wrapped in OSC 8 hyperlinks. Clicking one opens the matching console page in your browser (supported by iTerm2, Kitty, WezTerm, Ghostty, GNOME Terminal, Konsole, Windows Terminal, and most modern terminals). Disable with `--no-links` or `AVR_LINKS=0` if your terminal renders them as visible garbage.

**Tab completion**

```sh
# bash
eval "$(_AVR_COMPLETE=bash_source avr)"
# zsh
eval "$(_AVR_COMPLETE=zsh_source avr)"
# fish
_AVR_COMPLETE=fish_source avr | source
```

**Verbose mode**

```sh
avr -v run list                   # prints HTTP requests, useful for debugging
```

**Filter the help surface**

```sh
avr --help                        # top-level groups
avr <group> --help                # subcommands
avr <group> <command> --help      # full flag list with examples
```

## Documentation

- Full command reference: [`docs/REFERENCE.md`](./docs/REFERENCE.md) (also available as `man avr`)
- Online docs: [docs.avrea.com/cli](https://docs.avrea.com/cli)
- Print the same reference inline: `avr --help` and `avr <command> --help`

## Contributing and reporting issues

- Bug reports and feature requests: [github.com/avrea-com/cli/issues](https://github.com/avrea-com/cli/issues)

## Reporting a vulnerability

If you believe you have found a security issue in `avr`, please email
<security@avrea.com>. Do not open a public issue. See [SECURITY.md](SECURITY.md)
for our coordinated-disclosure policy, response targets, and safe-harbour terms.

## License

Licensed under the [Apache License, Version 2.0](./LICENSE). Copyright 2026 Avrea.
