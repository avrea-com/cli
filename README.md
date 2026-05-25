# Avrea CLI

Official command-line client for [Avrea](https://avrea.com/) — `avr` brings runs, jobs, logs, and workflow control into your terminal so you don't have to bounce between tabs.

## Install

The Homebrew tap and PyPI package are expected to release soon.
Until the first release is available, install from source:

```sh
git clone https://github.com/avrea-com/cli && cd cli && uv sync
uv run avr --version
```

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

For non-interactive environments (CI, scripts), set a token via env var instead — see [Configuration](#configuration).

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

IDs in tables are wrapped in OSC 8 hyperlinks — clicking opens the matching console page in your browser (supported by iTerm2, Kitty, WezTerm, Ghostty, GNOME Terminal, Konsole, Windows Terminal, and most modern terminals). Disable with `--no-links` or `AVR_LINKS=0` if your terminal renders them as visible garbage.

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

If you believe you have found a security issue in `avr`, please contact <security@avrea.com>.

## License

Licensed under the [Apache License, Version 2.0](./LICENSE). Copyright 2026 Avrea.
