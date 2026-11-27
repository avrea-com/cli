<p align="center">
  <img src="https://raw.githubusercontent.com/avrea-com/cli/main/.github/banner.png" alt="Avrea CLI" width="640" />
</p>
<p align="center">
  <a href="https://www.skills.sh/avrea-com/cli/avrea-cli"><img src="https://www.skills.sh/b/avrea-com/cli" alt="skills.sh" /></a>
  <a href="https://github.com/avrea-com/cli/actions/workflows/ci.yml"><img src="https://github.com/avrea-com/cli/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI" /></a>
  <a href="https://github.com/avrea-com/cli/releases/latest"><img src="https://img.shields.io/github/v/release/avrea-com/cli" alt="Release" /></a>
  <a href="https://github.com/avrea-com/cli/releases"><img src="https://img.shields.io/github/downloads/avrea-com/cli/total" alt="Downloads" /></a>
  <a href="https://github.com/avrea-com/cli/blob/main/LICENSE"><img src="https://img.shields.io/github/license/avrea-com/cli" alt="License" /></a>
</p>
<p align="center">
  <a href="https://console.avrea.com">Console</a> ·
  <a href="https://docs.avrea.com/cli/">Documentation</a> ·
  <a href="https://avrea.com">Website</a>
</p>

# Avrea CLI

Official command-line client for [Avrea](https://avrea.com/). `avr` brings runs, jobs, logs, tests, caches and on-demand VMs into your terminal and into your coding agent.

## Quickstart

```sh
brew install avrea-com/tap/avr      # or another method below
avr auth device-login               # approve the sign-in from any browser
avr status                          # recent runs, performance and cache health of this repository
```

Using a coding agent? Run `avr skill install` and see [For coding agents](#for-coding-agents).

## Install

`avr` is a native executable for macOS on Apple Silicon, Linux AMD64/ARM64 and Windows AMD64. On other platforms Go supports, such as Intel macOS, build it with `go install`.

```sh
brew install avrea-com/tap/avr                          # Homebrew
mise use -g github:avrea-com/cli@latest                 # mise
go install github.com/avrea-com/cli/cmd/avr@latest      # Go
uv tool install avr-cli                                 # PyPI (also pipx or pip)
```

Shell and PowerShell installers and plain archives are on [GitHub Releases](https://github.com/avrea-com/cli/releases/latest). With `go install`, make sure `$(go env GOBIN)` is on PATH, or `$(go env GOPATH)/bin` when `GOBIN` is unset. The PyPI package uses Python only to install; `avr` itself runs without it.

Update with the same method: `brew upgrade avr`, `mise upgrade github:avrea-com/cli`, `go install github.com/avrea-com/cli/cmd/avr@latest` or `uv tool upgrade avr-cli`. If `brew upgrade avr` refuses to load the formula from an untrusted tap, run `brew trust --formula avrea-com/tap/avr` once and retry. `avr` may tell you about a new version but never updates itself. Run `avr --version` and `which -a avr` to see which installation your shell uses.

[Installation and updates](https://github.com/avrea-com/cli/blob/main/docs/INSTALL.md) covers platform requirements, version pins, installer options, Homebrew tap trust and release verification.

## Sign in

```sh
avr auth device-login
```

Open the printed link on your laptop or phone, or scan the QR code, confirm the code and approve. The CLI waits and saves the credential on the machine where you ran it. This works everywhere: your laptop, an SSH session, a VM, a container or a coding agent's terminal. Pass `--no-qr` to print only the link.

On a machine with a browser you can also use `avr auth login`, which opens the browser directly.

```sh
avr auth status         # who you are and which host you use
avr config              # host, sign-in, default organization and repository, with their sources
avr auth logout         # revoke the stored API key
```

In CI and scripts, set `AVR_TOKEN` instead: a [personal access token or service token](#tokens-for-ci-scripts-and-agents).

## For coding agents

`avr` works well from Claude Code, Codex, OpenCode, Pi, Devin and other coding agents: output is plain text or JSON, exit codes are stable, and every command documents itself in `--help`.

Install the skill once on each machine:

```sh
avr skill install                    # Claude Code, plus Codex, Cursor, Copilot, Gemini CLI, Devin and other Agent Skills tools
avr skill install --agent kiro       # one location only: claude, agents or kiro
avr skill install --project          # commit it to the repository for cloud agents
avr skill show                       # print the guide the skill points to
```

The same skill also installs without `avr`, from [skills.sh](https://www.skills.sh/avrea-com/cli) as a plugin for Claude Code, Codex or GitHub Copilot, or with Gemini CLI. It then tells the agent how to install `avr`:

```sh
npx skills add avrea-com/cli                  # skills.sh: Claude Code, Codex, Cursor and other agents
claude plugin marketplace add avrea-com/cli   # Claude Code plugin: add the marketplace,
claude plugin install avrea-cli@avrea         # then install the plugin
codex plugin marketplace add avrea-com/cli    # Codex plugin
codex plugin add avrea-cli@avrea
copilot plugin marketplace add avrea-com/cli  # GitHub Copilot CLI plugin
copilot plugin install avrea-cli@avrea
gemini skills install https://github.com/avrea-com/cli.git --path plugins/avrea-cli/skills
```

How each route updates:

- Codex updates the plugin at startup, after each `avr` release.
- Claude Code updates it at startup once auto-update is on: run `/plugin`, open **Marketplaces**, select `avrea` and turn it on. Otherwise run `claude plugin update avrea-cli@avrea`.
- Copilot CLI updates it after a one-time entry in `~/.copilot/settings.json`:

  ```json
  {
    "extraKnownMarketplaces": {
      "avrea": { "source": { "source": "github", "repo": "avrea-com/cli" }, "autoUpdate": true }
    }
  }
  ```

  In VS Code, run **Chat: Install Plugin From Source** with `https://github.com/avrea-com/cli`. It updates daily.
- skills.sh and Gemini CLI copy the files: run `npx skills update`, or the `gemini skills install` command again.

The guide itself ships with `avr`, so it updates with `avr` whichever route installed the skill.

Pick one route per machine. With both a plugin and a skills directory install, the agent loads the skill twice.

The skill uses the open [Agent Skills](https://agentskills.io/) format. Claude Code reads it from `~/.claude/skills`, and Codex, Cursor, GitHub Copilot, Gemini CLI, Devin, OpenCode, Pi and other tools read it from `~/.agents/skills`. Kiro reads only `~/.kiro/skills`: use `--agent kiro`. It tells the agent when `avr` is relevant and to read `avr skill show`, a guide to testing a change in CI, diagnosing failed or slow runs, reading tests and logs, and using on-demand VMs. The guide ships with `avr`, so it always matches the installed version. `avr skill uninstall` removes the skill.

Cloud agents such as Devin or Codex cloud read skills from the repository: run `avr skill install --project` and commit `.claude/skills` and `.agents/skills` (and `.kiro/skills` with `--agent kiro`). Their environment also needs `avr` installed and `AVR_TOKEN` set to a [personal access token or service token](#tokens-for-ci-scripts-and-agents). For an agent that reads no skills, add a line to its instructions: "Run `avr skill show` before working with Avrea CI."

What the agent needs to know:

- **Sign in:** start `avr auth device-login --no-qr` in the background; it prints a link and code, then waits until the user approves them in a browser. For unattended agents, create a [personal access token](#tokens-for-ci-scripts-and-agents) and set `AVR_TOKEN` and `AVR_ORG`.
- **No prompts:** set `AVR_PROMPT_DISABLED=1` so a command fails instead of waiting for input, and pass `--yes` only where the action is intended.
- **Structured output:** `--json '?'` lists a command's fields, `--json a,b` selects them, and `--jq` filters them without a separate `jq`.
- **Exit codes:** `0` success, `1` failure, `2` usage error, `4` sign-in required, `130` interrupted.
- **Discovery:** `avr --help`, `avr <command> --help`, the [command reference](https://docs.avrea.com/cli/reference/) and its machine-readable form, [`docs/reference.json`](https://github.com/avrea-com/cli/blob/main/docs/reference.json). `avr api <endpoint>` calls any Avrea API endpoint the CLI does not cover yet.

```sh
avr run diagnose <run> --json                         # start here for a failed or slow run
avr run view <run> --log-failed                       # failed steps only
avr tests <run> --job unit --status failed            # failed tests of one job
avr run watch <run> --exit-status                     # wait for a run; its result becomes the exit code
```

## Common workflows

Inside a git checkout of a repository connected to Avrea, commands use the git remote to pick the repository. Elsewhere, pass `--repo owner/name` or `--repo rep-...`. Run arguments accept an Avrea run ID, a GitHub run ID, or a GitHub Actions run or job URL. Every command's `--help` has the full details and more examples.

**Find and watch runs**

```sh
avr run list
avr run watch                          # the latest active run of this repository
avr run watch <run> --exit-status
avr run view <run> --web               # open it in the console
```

**Triage a failure**

```sh
avr run diagnose <run>                 # failed steps, log tails, timings, metrics and a baseline
avr run view <run> --log-failed
avr job logs <job> --follow
avr job metrics <job> --watch          # live CPU, memory and IO of the job's VM
avr job ssh <job>                      # while the job is still running
```

**Read test results**

```sh
avr tests <run> --job unit             # one job's tests, outcomes, timings and failures
avr tests <job> --status failed --status errored
avr tests <run> --job unit --summary   # counts only
avr tests search login                 # latest result of each matching test in the repository
avr tests history test_login           # one test's recorded runs, newest first
```

**Rerun, cancel and dispatch**

```sh
avr run rerun <run> --failed
avr run cancel <run>
avr workflow run ci.yml --ref feat/x -f env=staging --watch --exit-status
```

**Metrics, logs and right-sizing**

```sh
avr job metrics <job> --source cpu --source memory
avr vm metrics <vm> --watch
avr log search --repo owner/name --query "OOM" --level error   # across the repository's recent runs
avr api /orgs/<org-id>/repos/<repo-id>/workflows/right-sizing -X GET -F window_days=7
```

The last call returns a suggested runner label per job; see [Right-sizing](https://docs.avrea.com/right-sizing/). Check the workflow too: a job limited to one worker (`pytest -n 1`, `make -j1`) looks oversized.

**Workflow performance and caches**

```sh
avr workflow list                      # per-workflow stats for the last 30 days
avr workflow view <workflow>           # per-job timings, failure rates and recent runs
avr cache usage
avr cache list
```

**On-demand VMs**

Linux, macOS and Windows VMs reachable over SSH, with RDP or VNC for a desktop. A VM created with `--repo` gets that repository's caches and, when Avrea mirrors the repository, a preloaded checkout; otherwise `vm create` says so, and `avr vm bootstrap --repo` clones it in the VM.

```sh
avr vm create --name dev --os linux --size 2-vcpu --ssh-key @~/.ssh/id_ed25519.pub --wait
avr vm bootstrap dev --setup-github --install claude,codex --repo https://github.com/org/project
avr vm ssh dev --session work          # persistent tmux session
avr vm ssh-config dev --append         # plain ssh, scp and VS Code Remote-SSH
avr vm pause dev ; avr vm resume dev --wait
avr vm delete dev --yes
```

**Remote container builds**

`avr build` takes `docker build` arguments and runs the build on an ephemeral Avrea builder VM. It forwards every flag it does not own to `docker buildx build`, so `-t`, `--push`, `--build-arg`, `-f` and `--output` work as they do locally. It needs the OpenSSH client and either the docker CLI with buildx, or podman with the standalone `docker-buildx` binary.

```sh
avr build -t myimage .                                         # built remotely, loaded into the local image store
avr build --push -t registry.example.com/acme/app:latest .     # pushed with your local registry login
avr build --platform linux/amd64,linux/arm64 --push -t img .   # one builder VM per architecture
avr build --size 8-vcpu --ttl 2h -t img .
avr builder list ; avr builder stop <id>                       # sessions a killed client left behind
```

Without `--platform` the image is built for your machine's CPU architecture, as `docker build` does; pass `--platform linux/amd64` on an ARM machine to build for an x86 server. Without `--push` or `--output` the image is loaded into the local image store, and a build for several platforms needs one of the two. A `Containerfile` is used when the context has no `Dockerfile`. With docker and podman both installed, podman is used; pass `--engine docker` or set `AVR_BUILD_ENGINE=docker` for docker.

The repository, from `--repo` or the git remote, only scopes the layer cache; a build without one runs uncached. Your own `--cache-from` or `--cache-to` replaces the default cache. The builder sessions are deleted when the build exits, also on Ctrl-C. A client that is killed leaves its session running until the TTL (1 hour by default) ends.

**Clone from a nearby Git mirror**

```sh
avr repo clone owner/repo
avr repo mirror create owner/repo --location <location>   # organization admins
```

`origin` points at the read-only mirror, and `git fetch` and `git pull` keep working through your Avrea sign-in. Push through a separate remote for the source repository.

## Scripting & automation

**JSON and jq**

```sh
avr run list --json '?'                                  # list the fields
avr run list --json status,conclusion,head_branch
avr run list --json '*' -q '.[] | select(.conclusion == "failure") | .run_id'
```

`-q/--jq` is built in. Object keys come out sorted; for ordered columns use an array such as `[.status,.conclusion,.run_id]`. Some commands, such as `status` and `run diagnose`, take a plain `--json` switch.

**Piped output**

When stdout is not a terminal, list commands print tab-separated rows without color or truncation, with ISO timestamps, so `avr run list | awk` works without flags. Control characters inside cells are escaped to keep one item per row; use `--json` when the exact value matters. `avr run watch | jq -c .` switches to NDJSON events.

**Exit codes**

`0` success, `1` failure, `2` usage error, `4` sign-in required, `130` interrupted with Ctrl-C.

```sh
avr run list >/dev/null
case $? in
  0) ;;
  4) echo "sign-in required" >&2; exit 4 ;;
  *) echo "avr failed" >&2; exit 1 ;;
esac
```

### Tokens for CI, scripts and agents

Avrea has two kinds of access token: a personal access token acts as you, and a service token belongs to a group. [Access tokens](https://docs.avrea.com/access-tokens/) compares them.

A **personal access token** is a short-lived credential of your own, from 60 seconds to 7 days, that reads the repositories you grant and administers the VMs you grant or allow it to create:

```sh
avr token create --name ci-read --org <org-id> --repo acme/api --ttl 8h
avr token create --name agent --org <org-id> --vm <vm-id> --allow-vm-create --vm-create-limit 3
avr token list ; avr token revoke <token-id>
```

A **service token** belongs to a group in the organization rather than to a person, keeps working when its creator leaves, and lives 30 days unless you choose up to 90 days or no expiry. Organization admins manage it:

```sh
avr service-token create --name ci --group Platform/CI --repo acme/api --ttl 30d
avr service-token rotate <token-id> --overlap 2h
avr service-token list --status live
```

Both print the secret once, with `export AVR_TOKEN=...` and `export AVR_ORG=...` lines. Store it, for example as a CI secret. A personal access token needs the `org-...` ID in `AVR_ORG` or `--org`. See `avr token --help` and `avr service-token --help` for grants, rotation and troubleshooting.

## Configuration

| Variable | Purpose |
| --- | --- |
| `AVR_HOST` | Avrea API URL. Defaults to the active host in `hosts.json`, then `https://api.avrea.com`. |
| `AVR_TOKEN` | API key, personal access token or service token. Overrides the stored credential. |
| `AVR_ORG` | Default organization ID. A personal access token needs it, or `--org`, except for `repo clone`; a service token uses its own organization. |
| `AVR_REPO` | Default repository (`owner/name` or `rep-...`). Overrides the git remote. |
| `AVR_PROMPT_DISABLED` | Refuse interactive prompts, so scripts and agents fail fast instead of waiting for input. |
| `AVR_BUILD_ENGINE` | Local tooling for `avr build`: `auto` (the default, which prefers podman), `docker` or `podman`. Same as `--engine`. |
| `AVR_BROWSER` | Browser for `--web` and `avr auth login`. Falls back to `BROWSER`, then the system default. |
| `AVR_PAGER` | Pager for long output. Overrides `PAGER`; set it empty to disable paging. |
| `AVR_LINKS` | Set to `0` to disable terminal hyperlinks, like `--no-links`. |
| `AVR_DEBUG` | `api` (or `1`) for request diagnostics, `timing` for timings. |
| `AVR_NO_UPDATE_NOTIFIER` | Set to `1` to disable update checks and notices. |
| `AVR_CONFIG_DIR` | Config directory. Defaults to `$XDG_CONFIG_HOME/avrea` or `~/.config/avrea`. |
| `AVR_MACOS_NATIVE_PATHS` | Set to `1` on macOS to use `~/Library/...` instead. |
| `NO_COLOR` | Disable colored output, like `--no-color`. |

```sh
avr config set org <org-id>       # store a default organization
avr config get org
avr config unset org
avr auth switch                   # pick between stored hosts
```

Credentials and per-host defaults are stored in `hosts.json` in the config directory.

## Tips

**Tab completion**

```sh
# bash
eval "$(_AVR_COMPLETE=bash_source avr)"
# zsh (initialize completion first if your shell has not already done so)
autoload -Uz compinit && compinit
eval "$(_AVR_COMPLETE=zsh_source avr)"
# fish
_AVR_COMPLETE=fish_source avr | source
```

**Clickable IDs**

IDs in tables are terminal hyperlinks to the matching console page in most modern terminals. Disable them with `--no-links` or `AVR_LINKS=0` if your terminal shows them as garbage. `avr run view`, `avr run list`, `avr job view`, `avr workflow view` and `avr cache list` also take `--web` to open the console.

**Debugging**

`avr -v <command>` prints the HTTP requests it makes.

## Documentation

- Command reference: [docs.avrea.com/cli/reference](https://docs.avrea.com/cli/reference/), [`docs/REFERENCE.md`](https://github.com/avrea-com/cli/blob/main/docs/REFERENCE.md), `avr <command> --help`, and `man avr` with Homebrew and the release archives.
- [Installation and updates](https://github.com/avrea-com/cli/blob/main/docs/INSTALL.md)
- [Send feedback and check delivery](https://github.com/avrea-com/cli/blob/main/docs/go-feedback.md)

## Contributing and reporting issues

Report bugs and request features at [github.com/avrea-com/cli/issues](https://github.com/avrea-com/cli/issues).

To build from source you need the Go toolchain named in [go.mod](https://github.com/avrea-com/cli/blob/main/go.mod) and [uv](https://docs.astral.sh/uv/) for the packaging tools. Commits must be signed.

```sh
git clone git@github.com:avrea-com/cli.git      # or https://github.com/avrea-com/cli.git
cd cli
make go-build && ./build/go/avr --version
make test lint
```

## Reporting a vulnerability

If you believe you have found a security issue in `avr`, email <security@avrea.com>. Do not open a public issue. See [SECURITY.md](https://github.com/avrea-com/cli/blob/main/SECURITY.md) for our coordinated-disclosure policy, response targets and safe-harbour terms.

## License

Licensed under the [Apache License, Version 2.0](https://github.com/avrea-com/cli/blob/main/LICENSE). Copyright 2026 Avrea.
