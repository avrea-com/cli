# avr(1) — Avrea command-line client

Reference for `avr` v0.2.0. Generated from the source tree — do not edit by hand. Run `make -C avr-cli docs` to regenerate.

## Name

`avr` — Avrea on the command line.

## Synopsis

```sh
avr [GLOBAL OPTIONS] COMMAND [ARGS]...
```

**Global options**

- `--version, -V` — Show the version and exit.
- `--no-color` — Disable colored output. Also honors NO_COLOR=1.
- `--verbose, -v` — Show debug information including HTTP requests.
- `--links / --no-links` — Make IDs clickable via OSC 8 hyperlinks. Auto-disabled off-TTY. Also honors AVR_LINKS=0. _(default: `True` · env: `AVR_LINKS`)_

## Aliases

- `jobs` → `job`
- `logs` → `log`
- `orgs` → `org`
- `repos` → `repo`
- `vms` → `vm`
- `workflows` → `workflow`

## Commands

### Core Commands

- [`avr status`](#avr-status) — Show recent runs, performance stats, and cache health.
- [`avr run`](#avr-run) — View and manage GitHub workflow runs.
- [`avr job`](#avr-job) — Inspect Avrea job VMs (SSH, metrics, logs).
- [`avr vm`](#avr-vm) — Manage long-running VMs (SSH/RDP/VNC).
- [`avr workflow`](#avr-workflow) — List and view workflow definitions.
- [`avr cache`](#avr-cache) — Inspect and manage the Avrea build cache.
- [`avr log`](#avr-log) — Search across runner execution logs.

### Setup & Config

- [`avr auth`](#avr-auth) — Authenticate and manage credentials.
- [`avr config`](#avr-config) — View and manage CLI configuration.
- [`avr settings`](#avr-settings) — View and toggle cache and runner settings.
- [`avr firewall`](#avr-firewall) — Manage the egress firewall rule list for orgs and repositories.
- [`avr billing`](#avr-billing) — Manage billing, invoices, and payment methods.
- [`avr audit-events`](#avr-audit-events) — View audit events for organization writes.

### Additional Commands

- [`avr repo`](#avr-repo) — Manage repositories and public mirrors.
- [`avr org`](#avr-org) — Manage organizations and installations.
- [`avr health`](#avr-health) — Check Avrea platform status.

## Reference

### `avr status`

Show recent runs, performance stats, and cache health.

```sh
avr status [OPTIONS]
```

**Options**

- `--org <TEXT>` — Organization ID or slug.
- `--repo <TEXT>` — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- `--since <TEXT>` — Time window for stats panels: '7d', '24h', etc. _(default: `7d`)_
- `--json` — Output raw JSON.

### `avr run`

View and manage GitHub workflow runs.

```sh
avr run [OPTIONS] COMMAND [ARGS]...
```

#### `avr run cancel`

Cancel an in-progress or queued workflow run.

```sh
avr run cancel [OPTIONS] RUN_ID
```

```sh
Examples:
    avr run cancel run-abc123
    avr run cancel run-abc123 --yes
```

**Arguments**

- `RUN_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug.
- `-y, --yes` — Skip the confirmation prompt.

#### `avr run list`

List workflow runs for an organization.

```sh
avr run list [OPTIONS]
```

```sh
Examples:
    avr run list
    avr run list --status failure --limit 5
    avr run list --status in_progress --json status,conclusion,head_branch
    avr run list --branch main --status completed
    avr run list --since 24h
    avr run list --json '?'           # list available fields
    avr run list --json '*'           # all fields
    avr run list --json status,conclusion -q '[.[] | select(.status == "completed")]'
```

```sh
JSON FIELDS
    conclusion, created_at, display_title, duration_seconds, event,
    head_branch, head_sha, platform_run_id, repository, run_attempt, run_id,
    run_number, status, triggering_actor, updated_at, workflow, workflow_id
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--repo <TEXT>` — Filter by repository (org/repo or rep-xxx, repeatable). Auto-detected from git remote if omitted. _(repeatable)_
- `--status <CHOICE>` — Filter by state (queued, in_progress, completed) or conclusion (success, failure, ...). Repeatable. _(choices: `action_required`, `cancelled`, `completed`, `failure`, `in_progress`, `neutral`, `queued`, `skipped`, `stale`, `startup_failure`, `success`, `timed_out` · repeatable)_
- `--branch <TEXT>` — Filter by head branch (repeatable). _(repeatable)_
- `-w, --workflow <TEXT>` — Filter by workflow ID (wfl-xxx, repeatable). _(repeatable)_
- `--since <TEXT>` — Relative time window: '7d', '24h', etc. Sugar for --created-after.
- `--from, --created-after <TEXT>` — Only runs created after this ISO timestamp.
- `--to, --created-before <TEXT>` — Only runs created before this ISO timestamp.
- `-L, --limit <INTEGER RANGE>` — Max runs to return. _(default: `20`)_
- `--cursor <TEXT>` — Pagination cursor from a previous response.
- `--order <CHOICE>` — Sort order. _(choices: `created_at.desc`, `created_at.asc` · default: `created_at.desc`)_
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.
- `--web` — Open in browser.

#### `avr run logs`

Fetch logs for a workflow run's GitHub jobs.

```sh
avr run logs [OPTIONS] RUN_ID
```

Long-form alternative to `avr run view --log[-failed]`. Use --follow to
tail logs in real time for an in-progress job; pass --job to scope to a
specific job when a run has many.

```sh
Examples:
    avr run logs run-abc123
    avr run logs run-abc123 --failed
    avr run logs run-abc123 --job test
    avr run logs run-abc123 --follow
```

**Arguments**

- `RUN_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug.
- `--job <TEXT>` — Restrict to GitHub jobs whose name contains this string.
- `-f, --follow` — Tail logs as they appear (running jobs only).
- `--failed` — Show only logs from failed jobs.
- `--all-levels` — Include diagnostic-level lines (off by default).
- `--no-pager` — Print directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.

#### `avr run rerun`

Re-run a completed workflow run.

```sh
avr run rerun [OPTIONS] RUN_ID
```

```sh
Examples:
    avr run rerun run-abc123
    avr run rerun run-abc123 --failed
    avr run rerun run-abc123 --yes
```

**Arguments**

- `RUN_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug.
- `--failed` — Re-run only the failed jobs.
- `-y, --yes` — Skip the confirmation prompt.

#### `avr run view`

View a workflow run with its jobs.

```sh
avr run view [OPTIONS] [RUN]
```

```sh
RUN accepts an Avrea run ID, a positive GitHub run ID, a GitHub Actions
run URL, or an Avrea console run URL. Without RUN, shows 10 most recent
runs.
```

```sh
Examples:
    avr run view
    avr run view run-abc123
    avr run view 123456789
    avr run view https://github.com/acme/widgets/actions/runs/123456789
    avr run view run-abc123 --steps
    avr run view run-abc123 --log-failed
    avr run view run-abc123 --job Build
    avr run view run-abc123 --json conclusion,jobs
    avr run view run-abc123 --json '*' --jq '.jobs[].job_name'
```

```sh
JSON FIELDS
    conclusion, created_at, display_title, duration_seconds, event,
    head_branch, head_sha, jobs, platform_run_id, repository, run_attempt,
    run_id, run_number, status, triggering_actor, updated_at, workflow,
    workflow_id
```

**Arguments**

- `[RUN]`

**Options**

- `--org <TEXT>` — Organization ID or slug.
- `--steps` — Expand each job to show its individual steps.
- `--log` — Print full logs for all jobs.
- `--log-failed` — Print logs only for failed steps.
- `--job <TEXT>` — Restrict view and logs to jobs whose name contains this string.
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.
- `--web` — Open in browser.
- `--no-pager` — Print logs directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.

#### `avr run watch`

Watch a workflow run until it completes.

```sh
avr run watch [OPTIONS] [RUN_ID]
```

```sh
Without RUN_ID, auto-selects the latest in-progress run. Pass --repo
(repeatable) to scope the auto-select to specific repositories.
```

```sh
Examples:
    avr run watch
    avr run watch --repo acme/web
    avr run watch --repo a/x --repo b/y
    avr run watch run-abc123 --exit-status
    avr run watch run-abc123 --ndjson | jq -c .
```

**Arguments**

- `[RUN_ID]`

**Options**

- `--org <TEXT>` — Organization ID or slug.
- `--repo <TEXT>` — Scope the auto-select to a repo (org/name or rep-xxx, repeatable). Auto-detected from git remote if omitted. _(repeatable)_
- `--exit-status` — Exit non-zero if run failed.
- `--interval <INTEGER>` — Refresh interval in seconds. _(default: `3`)_
- `--ndjson` — Force NDJSON event stream (default when stdout isn't a TTY).

### `avr job`

Inspect Avrea job VMs (SSH, metrics, logs).

```sh
avr job [OPTIONS] COMMAND [ARGS]...
```

#### `avr job list`

List jobs for an organization.

```sh
avr job list [OPTIONS]
```

```sh
Examples:
    avr job list
    avr job list --status failure --limit 5
    avr job list --status in_progress --json job_name,state,conclusion
    avr job list --since 24h
    avr job list --json '?'           # list available fields
    avr job list --json '*'           # all fields
```

```sh
JSON FIELDS
    completed_at, conclusion, created_at, duration_seconds, platform_job_id,
    platform_run_id, job_id, job_name, labels, repository, repository_id,
    running_on_avrea, started_at, state
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--repo <TEXT>` — Filter by repository (org/repo or rep-xxx, repeatable). Auto-detected from git remote if omitted. _(repeatable)_
- `--name <TEXT>` — Filter by job name (repeatable). _(repeatable)_
- `--status <CHOICE>` — Filter by state (queued, in_progress, completed) or conclusion (success, failure, ...). Repeatable. _(choices: `action_required`, `cancelled`, `completed`, `failure`, `in_progress`, `neutral`, `queued`, `skipped`, `stale`, `startup_failure`, `success`, `timed_out` · repeatable)_
- `--on-avrea / --shadowing` — Filter by Avrea-run vs shadowing jobs.
- `-w, --workflow <TEXT>` — Filter by workflow ID (wfl-xxx, repeatable). _(repeatable)_
- `--since <TEXT>` — Relative time window: '7d', '24h', etc.
- `-L, --limit <INTEGER RANGE>` — Max jobs to return. _(default: `20`)_
- `--cursor <TEXT>` — Pagination cursor from a previous response.
- `--order <CHOICE>` — Sort order. _(choices: `created_at.desc`, `created_at.asc` · default: `created_at.desc`)_
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr job logs`

View logs for a job, grouped by step.

```sh
avr job logs [OPTIONS] JOB_ID
```

```sh
Examples:
    avr job logs job-abc123
    avr job logs job-abc123 --failed
    avr job logs job-abc123 --step "Build" --level error
    avr job logs job-abc123 --follow
```

**Arguments**

- `JOB_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug.
- `--failed` — Only show logs from failed steps.
- `--step <TEXT>` — Filter to a specific step by name.
- `--level <CHOICE>` — Filter by log level. _(choices: `debug`, `info`, `notice`, `warning`, `error`)_
- `--follow, -f` — Follow logs for in-progress jobs.
- `--all-levels` — Include diagnostic-level lines (hidden by default).
- `--no-pager` — Print directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.

#### `avr job metrics`

Show CPU/memory/IO gauges for a job's VM.

```sh
avr job metrics [OPTIONS] JOB_ID
```

```sh
Examples:
    avr job metrics job-abc123
    avr job metrics job-abc123 --source cpu --source network
    avr job metrics job-abc123 --watch
```

**Arguments**

- `JOB_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug.
- `--source <CHOICE>` — Metric source (repeatable). Defaults to cpu and memory. _(choices: `cpu`, `memory`, `filesystem`, `load`, `disk-io`, `disk-ops`, `network` · repeatable)_
- `--start <INTEGER>` — Start time (Unix seconds). Defaults to execution start.
- `--end <INTEGER>` — End time (Unix seconds). Defaults to execution end or now.
- `-w, --watch` — Refresh every 5 seconds (Ctrl-C to exit).
- `--json` — Output raw metrics responses as JSON.

#### `avr job ssh`

SSH into a running job's VM.

```sh
avr job ssh [OPTIONS] JOB_ID
```

**Arguments**

- `JOB_ID`

**Options**

- `--print-command` — Print the SSH command instead of connecting.
- `--show-password` — Display the SSH password (use with caution).

#### `avr job view`

View a single job with its steps.

```sh
avr job view [OPTIONS] JOB_ID
```

```sh
Examples:
    avr job view job-abc123
    avr job view job-abc123 --log-failed
    avr job view job-abc123 --json conclusion,steps
    avr job view job-abc123 --json '*' --jq '.steps[] | select(.conclusion=="failure")'
```

```sh
JSON FIELDS
    completed_at, conclusion, created_at, duration_seconds, platform_job_id,
    platform_run_id, job_id, job_name, labels, repository, repository_id,
    running_on_avrea, started_at, state, steps, workflow_run
```

**Arguments**

- `JOB_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug.
- `--log` — Print full logs for the job.
- `--log-failed` — Print logs only for failed steps.
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.
- `--web` — Open in browser.
- `--no-pager` — Print logs directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.

#### `avr job watch`

Watch active jobs with auto-refresh (Ctrl+C to stop).

```sh
avr job watch [OPTIONS]
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--repo <TEXT>` — Filter by repository (org/repo or rep-xxx, repeatable). Auto-detected from git remote if omitted. _(repeatable)_
- `--name <TEXT>` — Filter by job name (repeatable). _(repeatable)_
- `--interval <INTEGER>` — Refresh interval in seconds. _(default: `5`)_
- `--ndjson` — Emit one JSON object per refresh (default when stdout isn't a TTY).

### `avr vm`

Manage long-running VMs (SSH/RDP/VNC).

```sh
avr vm [OPTIONS] COMMAND [ARGS]...
```

#### `avr vm bootstrap`

Set up a RUNNING VM with your dev essentials over SSH.

```sh
avr vm bootstrap [OPTIONS] VM_ID
```

```sh
Each selected step runs on the VM and streams its output; bootstrap stops at
the first failure. Secrets (GitHub token, forwarded env values, agent
credentials) ride SSH stdin, never argv. A forwarded CLAUDE_CODE_OAUTH_TOKEN
is a year-long, subscription-wide bearer, so it is only carried under the
explicit --forward-agent-creds opt-in. Disks are ephemeral, so re-run
bootstrap after every `avr vm start`. Example:
```

```sh
avr vm bootstrap cvm-abc123 --setup-github --install claude,codex \
  --repo https://github.com/me/project --env AWS_REGION=eu-north-1
```

**Arguments**

- `VM_ID`

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `-i, --identity <PATH>` — Private key file to pass to ssh as -i.
- `--setup-github / --no-setup-github` — Forward your local `gh auth token` into the VM (gh auth login --with-token + gh auth setup-git).
- `--install <TEXT>` — Install an agent CLI (repeatable or comma-separated): claude, codex. _(repeatable)_
- `--forward-agent-creds` — Forward the installed agents' credentials from your environment (ANTHROPIC_API_KEY / CLAUDE_CODE_OAUTH_TOKEN / OPENAI_API_KEY). If claude has none, offers to run `claude setup-token`.
- `--install-avr` — Install the avr CLI in the VM (pipx, else pip).
- `--repo <TEXT>` — Clone this git repo into the VM's home directory.
- `--ref <TEXT>` — Branch to check out after cloning (requires --repo). Tags, PR refs, and raw SHAs are unsupported.
- `--dotfiles <TEXT>` — Clone this dotfiles repo and run its installer.
- `--env <TEXT>` — Set an env var in the VM: KEY=VALUE, or a bare KEY to forward it from your environment. Repeatable. _(repeatable)_
- `--run <TEXT>` — Run a custom script last: an inline script, or @path to a file.
- `--print` — Print the ordered plan (secrets redacted) without running.

#### `avr vm create`

Create a long-running VM.

```sh
avr vm create [OPTIONS]
```

```sh
Provisioning is asynchronous: poll `avr vm show <id>` until the state is
RUNNING and endpoints are populated, or pass --wait to block until then and
print a ready-to-paste connect command with the password baked in. The
response carries a one-time password for the VM's local account; save it
now, it is never stored.
```

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `--name <TEXT>` — Human-readable VM name. _(required)_
- `--os <CHOICE>` — Guest operating system. _(choices: `linux`, `macos`, `windows` · required)_
- `--os-version <CHOICE>` — Guest OS version (e.g. ubuntu-26.04). Defaults to the latest version for the chosen --os. _(choices: `ubuntu-22.04`, `ubuntu-24.04`, `ubuntu-26.04`, `macos-26`, `windows-2025`)_
- `--size <CHOICE>` — Hardware tier. Availability is OS-specific: linux 1-32 vCPU, macos 8/16, windows 2-16. _(choices: `1-vcpu`, `2-vcpu`, `4-vcpu`, `8-vcpu`, `16-vcpu`, `32-vcpu` · required)_
- `--ssh-key <TEXT>` — SSH public key, or @path to read one from a file. Repeatable. _(repeatable)_
- `--remote-desktop / --no-remote-desktop` — Enable a remote desktop: RDP (Windows, Linux) or VNC (macOS Screen Sharing). Availability depends on OS version; the server validates.
- `--ttl <TEXT>` — Auto-stop the VM after this long (e.g. 8h, 7d, 1800s). Default 8h, max 7d.
- `--egress-rules <TEXT>` — Per-VM egress firewall rules as a JSON array, or @path to a JSON file.
- `--repo <TEXT>` — Git repository (owner/repo) to preload into the VM at boot. Best-effort; the checkout is warmed from Avrea's mirror when available.
- `--ref <TEXT>` — Branch to preload (default: the repository's default branch). Requires --repo. Tags, pull-request refs, and raw commit SHAs are not supported.
- `--disable-cache <TEXT>` — Disable a build/CI cache on this VM (repeatable, or comma-separated). Narrowing only: a VM can turn off an inherited cache but not turn one on. e.g. gha, packages, bazel, gradle, maven, turbo, nx, go-build (or a raw cache.&lt;name&gt;.enabled key). Repository-scoped caches require --repo. _(repeatable)_
- `--ephemeral` — Required: acknowledge that the VM's disk is ephemeral (discarded on stop).
- `--wait` — Wait until the VM is RUNNING, then print a ready-to-paste connect command with the password baked in.
- `--wait-timeout <INTEGER>` — Seconds to wait when --wait is set. _(default: `300`)_
- `--json` — Emit the raw API response (VM plus one-time password) as JSON.

#### `avr vm delete`

Delete a VM.

```sh
avr vm delete [OPTIONS] VM_ID
```

Delete a VM. Asynchronous while live: shows DELETING until the node confirms the stop.

**Arguments**

- `VM_ID`

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `--yes, -y` — Skip the confirmation prompt.
- `--wait` — Wait until the VM is fully deleted before returning.
- `--wait-timeout <INTEGER>` — Seconds to wait when --wait is set. _(default: `300`)_
- `--json` — Emit the raw API response as JSON.

#### `avr vm list`

List the organization's VMs, newest first (deleted ones excluded).

```sh
avr vm list [OPTIONS]
```

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `--state <TEXT>` — Filter by lifecycle state (e.g. RUNNING, STOPPED, PENDING).
- `-L, --limit <INTEGER RANGE>` — Max VMs to return. _(default: `50`)_
- `--cursor <TEXT>` — Pagination cursor from a previous response.
- `--json` — Emit the VM list as JSON.

#### `avr vm port-forward`

Forward one or more local ports to TCP ports on the VM over SSH.

```sh
avr vm port-forward [OPTIONS] VM_ID
```

The generic primitive behind `avr vm rdp` / `avr vm vnc`: opens
127.0.0.1:&lt;local&gt; -&gt; &lt;VM&gt;:&lt;guest&gt; through the VM's SSH endpoint for each
--port, and holds them open until Ctrl-C. Bring your own client.

```sh
avr vm port-forward cvm-abc123 --port 8080
avr vm port-forward cvm-abc123 --port 8080 --port 5432 --port 9000:3000
```

**Arguments**

- `VM_ID`

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `--port <TEXT>` — Guest TCP port to forward, optionally with a local bind port (e.g. 8080 or 9000:8080). Repeatable. _(repeatable · required)_
- `--local-port <INTEGER RANGE>` — Local port to bind for a single bare --port (with multiple ports, use --port LOCAL:GUEST instead).
- `-i, --identity <PATH>` — Private key file to pass to ssh as -i.
- `--print` — Print the ssh command and exit, without opening the tunnel.

#### `avr vm rdp`

Open an RDP desktop on a Windows or Linux VM over an SSH tunnel.

```sh
avr vm rdp [OPTIONS] VM_ID
```

Forwards a local port to the guest's RDP service (:3389) through the VM's
SSH endpoint, so the desktop is never exposed publicly. Holds the tunnel
open until Ctrl-C; pass --launch to also start a local RDP client.

**Arguments**

- `VM_ID`

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `--local-port <INTEGER RANGE>` — Local port to bind (default: an unused port).
- `-i, --identity <PATH>` — Private key file to pass to ssh as -i.
- `--launch / --no-launch` — Also start a local RDP client, instead of just printing the connect command.
- `--print` — Print the tunnel and client commands and exit, without opening the tunnel.

#### `avr vm show`

Show a VM's details, including connection endpoints and egress rules.

```sh
avr vm show [OPTIONS] VM_ID
```

**Arguments**

- `VM_ID`

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `--json` — Emit the full VM record (with egress rules) as JSON.

#### `avr vm ssh`

Open an SSH session to a RUNNING VM, or run a command on it.

```sh
avr vm ssh [OPTIONS] VM_ID [SSH_ARGS]...
```

With no extra arguments this opens an interactive session. Anything after
`--` is run as a remote command instead, e.g.:

    avr vm ssh cvm-abc123 -- uname -a

A one-off `-- <cmd>` runs in a non-login shell that sources no startup files,
so it won't see env forwarded by `avr vm bootstrap`. Pass `--login` to run it
in a login shell instead (e.g. so `claude` finds its subscription token):

    avr vm ssh cvm-abc123 --login -- claude -p 'summarize the repo'

Pass `--session <name>` to attach to (or create) a persistent tmux session,
so the shell and any long-running process in it survive a dropped
connection; reconnect with the same name to resume where you left off. A
`-- <cmd>` given with --session runs only when the session is first created;
reattaching to an existing session resumes it and does not rerun the command.

When the VM's endpoint publishes a host key it is pinned, so the first
connect neither prompts nor is spoofable. If the endpoint has no host key,
`avr` prints a warning and falls back to trust-on-first-use, so this
spoofing protection is conditional rather than guaranteed. For
port-forwarding use `avr vm port-forward`.

**Arguments**

- `VM_ID`
- `[SSH_ARGS...]`

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `-i, --identity <PATH>` — Private key file to pass to ssh as -i.
- `--session <TEXT>` — Attach to (or create) a persistent tmux session by this name, so the shell and any long-running process in it survive a dropped connection. Reconnect with the same --session. A `-- <cmd>` runs only when the session is created, not on reattach.
- `--login` — Run the `-- <cmd>` in a login shell (bash -lc) so it sees bootstrap-forwarded env like CLAUDE_CODE_OAUTH_TOKEN. Needed for a non-interactive command; a plain `ssh host cmd` shell sources nothing.
- `--print` — Print the ssh command instead of running it.

#### `avr vm ssh-config`

Print (or --append) an ssh_config Host block for a RUNNING VM.

```sh
avr vm ssh-config [OPTIONS] VM_ID
```

Reach the VM with plain `ssh`, scp/rsync, and VS Code / Cursor Remote-SSH,
host key pinned, without wrapping each tool. Redirect it yourself:

    avr vm ssh-config cvm-abc123 >> ~/.ssh/config

or let --append manage the block for you (idempotent — re-run after a
restart to refresh the endpoint in place):

    avr vm ssh-config cvm-abc123 --append

The block references a dedicated known_hosts file that this command writes
the pinned host key into (one entry per VM). If the endpoint publishes no
host key, the block falls back to accept-new (trust-on-first-use) and a
warning is printed.

**Arguments**

- `VM_ID`

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `-i, --identity <PATH>` — IdentityFile to write into the block.
- `--host-alias <TEXT>` — Host alias for the block (default: avr-&lt;vm-id&gt;).
- `--known-hosts-file <PATH>` — Where to pin the host key (default: ~/.ssh/avr_known_hosts).
- `--append` — Write the block into your SSH config (default: ~/.ssh/config) instead of printing it, replacing any prior block for the same alias in place.
- `--config-file <PATH>` — SSH config file for --append (default: ~/.ssh/config).

#### `avr vm start`

Start a stopped VM.

```sh
avr vm start [OPTIONS] VM_ID
```

Start a stopped VM. Boots a fresh disk and returns a one-time password.

**Arguments**

- `VM_ID`

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `--wait` — Wait until RUNNING, then print a ready-to-paste connect command with the fresh password.
- `--wait-timeout <INTEGER>` — Seconds to wait when --wait is set. _(default: `300`)_
- `--json` — Emit the raw API response as JSON.

#### `avr vm stop`

Stop a running VM.

```sh
avr vm stop [OPTIONS] VM_ID
```

Stop a running VM. The ephemeral disk is discarded.

**Arguments**

- `VM_ID`

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `--wait` — Wait until the VM reaches STOPPED before returning.
- `--wait-timeout <INTEGER>` — Seconds to wait when --wait is set. _(default: `300`)_
- `--json` — Emit the raw API response as JSON.

#### `avr vm update`

Update a VM's name, TTL, or SSH keys, or rotate its password.

```sh
avr vm update [OPTIONS] VM_ID
```

Power state is controlled separately with avr vm start / avr vm stop.

**Arguments**

- `VM_ID`

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `--name <TEXT>` — New display name.
- `--ttl <TEXT>` — Extend the auto-stop window from now (e.g. 8h, 7d). Max 7d.
- `--ssh-key <TEXT>` — Replace stored SSH public keys (literal or @path). Repeatable. Applies live on a RUNNING VM, otherwise at next start. _(repeatable)_
- `--rotate-password` — Provision a fresh one-time password (returned in the response).
- `--egress-rules <TEXT>` — Replace the per-VM egress rules with this JSON array (or @path to a file).
- `--json` — Emit the raw API response as JSON.

#### `avr vm usage`

Show usage metering (runtime / vCPU / memory seconds) per VM.

```sh
avr vm usage [OPTIONS]
```

Each power-on cycle's window is clipped to the requested period and summed.
Deleted VMs are included: usage survives deletion.

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `--start <DATETIME>` — Inclusive period start (default: 30 days ago).
- `--end <DATETIME>` — Exclusive period end (default: now).
- `--json` — Emit the usage report as JSON.

#### `avr vm vnc`

Open a VNC desktop on a macOS VM (Screen Sharing) over an SSH tunnel.

```sh
avr vm vnc [OPTIONS] VM_ID
```

Forwards a local port to the guest's Screen Sharing service (:5900) through
the VM's SSH endpoint, so the desktop is never exposed publicly. Holds the
tunnel open until Ctrl-C; pass --launch to also open Screen Sharing.

**Arguments**

- `VM_ID`

**Options**

- `--org <TEXT>` — Organization ID. Uses default org if not specified (see: avr config set org).
- `--local-port <INTEGER RANGE>` — Local port to bind (default: an unused port).
- `-i, --identity <PATH>` — Private key file to pass to ssh as -i.
- `--launch / --no-launch` — Also start a local VNC client (macOS Screen Sharing), instead of just printing the connect command.
- `--print` — Print the tunnel and client commands and exit, without opening the tunnel.

### `avr workflow`

List and view workflow definitions.

```sh
avr workflow [OPTIONS] COMMAND [ARGS]...
```

#### `avr workflow list`

List workflows with aggregate run stats.

```sh
avr workflow list [OPTIONS]
```

```sh
Examples:
    avr workflow list
    avr workflow list --repo acme/web
    avr workflow list --since 7d
    avr workflow list --limit 5
    avr workflow list --since all
    avr workflow list --json name,runs,median_duration_seconds
```

```sh
JSON FIELDS
    completed_runs, failure_count, flaked_count, median_duration_seconds, name,
    path, platform_workflow_id, repository, runs, workflow_id
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--repo <TEXT>` — Filter by repository (org/repo or rep-xxx ID, repeatable). _(repeatable)_
- `--since <TEXT>` — Time window: '30d', '7d', '24h', or 'all'. _(default: `30d`)_
- `-L, --limit <INTEGER RANGE>` — Max workflows to show. _(default: `20`)_
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr workflow run`

Trigger a workflow_dispatch event.

```sh
avr workflow run [OPTIONS] WORKFLOW
```

WORKFLOW can be an Avrea workflow ID (wfl-...), a GitHub numeric
workflow ID, a workflow filename (build.yml), or the workflow's
display name.

```sh
Examples:
    avr workflow run build.yml
    avr workflow run "Build and Deploy" --ref feat/x
    avr workflow run wfl-abc123 -f env=prod -f region=eu
    echo '{"env":"prod"}' | avr workflow run build.yml --json
    avr workflow run build.yml --watch --exit-status
```

**Arguments**

- `WORKFLOW_IDENTIFIER`

**Options**

- `--org <TEXT>` — Organization ID or slug.
- `--repo <TEXT>` — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- `-r, --ref <TEXT>` — Branch or tag to run at. Defaults to the repository's default branch.
- `-f, --raw-field <TEXT>` — Workflow input: key=value (repeatable). _(repeatable)_
- `--json` — Read a JSON object of inputs from stdin.
- `-w, --watch / -W, --no-watch` — Poll for the new run and watch it until completion. Pass --no-watch / -W to return immediately. _(default: `True`)_
- `--exit-status` — With --watch, exit non-zero if the run failed.
- `--interval <INTEGER>` — With --watch, refresh interval in seconds. _(default: `3`)_

#### `avr workflow view`

View a workflow with aggregate stats and per-job breakdown.

```sh
avr workflow view [OPTIONS] WORKFLOW
```

WORKFLOW can be an Avrea workflow ID (wfl-...), a GitHub numeric
workflow ID (the integer in the GH URL), a workflow filename
(build.yml), or the workflow's display name. All forms except wfl-...
need a repository — pass --repo or run from inside the repo's git
checkout.

```sh
Examples:
    avr workflow view wfl-abc123
    avr workflow view 200589168
    avr workflow view ci.yml
    avr workflow view "Build and Deploy" --since 7d
    avr workflow view ci --json runs,median_duration_seconds
    avr workflow view wfl-abc123 --json '*' --jq '.jobs[].job.name'
```

```sh
JSON FIELDS
    completed_runs, failure_count, flaked_count, jobs, median_duration_seconds,
    name, p95_duration_seconds, path, platform_workflow_id, repository, runs,
    workflow_id
```

**Arguments**

- `WORKFLOW_IDENTIFIER`

**Options**

- `--org <TEXT>` — Organization ID or slug.
- `--repo <TEXT>` — Repository (org/repo or rep-xxx). Auto-detected from git remote when WORKFLOW is a filename or display name.
- `--since <TEXT>` — Time window: '30d', '7d', '24h', or 'all'. _(default: `30d`)_
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.
- `--web` — Open in browser.

### `avr cache`

Inspect and manage the Avrea build cache.

```sh
avr cache [OPTIONS] COMMAND [ARGS]...
```

#### `avr cache delete`

Delete cache entries by key name or all entries.

```sh
avr cache delete [OPTIONS]
```

Exactly one of --key or --all must be provided.
When using --key, --type is required to scope the deletion.

```sh
Examples:
    avr cache delete --repo rep-abc123 --type gha --key "node_modules" --ref refs/heads/main --yes
    avr cache delete --repo rep-abc123 --all --yes
```

**Options**

- `--repo <TEXT>` — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--type <TEXT>` — Cache type (required with --key, e.g. gha, bazel, sccache).
- `--key <TEXT>` — Delete entries matching this cache key name.
- `--ref <TEXT>` — Ref to narrow deletion scope (used by gha).
- `--all` — Delete ALL cache entries for the repository.
- `--yes, -y` — Skip confirmation prompt.

#### `avr cache list`

List cache entries for a repository.

```sh
avr cache list [OPTIONS]
```

```sh
Examples:
    avr cache list --repo rep-abc123
    avr cache list --repo rep-abc123 --type gha --limit 50
    avr cache list --repo rep-abc123 --key "node_modules" --ref refs/heads/main
    avr cache list --repo rep-abc123 --json key,size_bytes,created_at
```

```sh
JSON FIELDS
    cache_type, created_at, hit_count, key, last_accessed_at, ref, size_bytes, version
```

**Options**

- `--repo <TEXT>` — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--type <TEXT>` — Filter by cache type (e.g. gha, bazel, turbo, rclone).
- `--key <TEXT>` — Filter by key prefix.
- `--ref <TEXT>` — Filter by exact ref match.
- `-L, --limit <INTEGER RANGE>` — Max entries to return. _(default: `100`)_
- `--offset <INTEGER RANGE>` — Number of entries to skip. _(default: `0`)_
- `--order <CHOICE>` — Sort order. _(choices: `created_at.desc`, `created_at.asc` · default: `created_at.desc`)_
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.
- `--web` — Open in browser.

#### `avr cache usage`

Show cache usage summary for a repository.

```sh
avr cache usage [OPTIONS]
```

```sh
Examples:
    avr cache usage --repo rep-abc123
    avr cache usage --repo rep-abc123 --json '*'
```

```sh
JSON FIELDS
    by_type, over_quota, quota_bytes, total_size_bytes
```

**Options**

- `--repo <TEXT>` — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

### `avr log`

Search across runner execution logs.

```sh
avr log [OPTIONS] COMMAND [ARGS]...
```

#### `avr log search`

Search logs for a repository.

```sh
avr log search [OPTIONS]
```

Performs full-text search when --query is provided, otherwise returns
logs sorted by line number. Results are filtered to logs from repositories
you have access to.

```sh
Examples:
    avr log search --repo acme/web --query "error"
    avr log search --repo rep-abc123 --level error --limit 50
    avr log search --repo rep-abc123 --vm-id vm-xyz --stream stderr
    avr log search --repo rep-abc123 --query "OOM" --json content,timestamp,vm_id
```

```sh
JSON FIELDS
    content, group_name, id, level, line_number, repository_id, score,
    step_name, step_record_id, stream, timestamp, vm_id
```

**Options**

- `--repo <TEXT>` — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--query <TEXT>` — Full-text search query
- `--stream <CHOICE>` — Filter by output stream _(choices: `stdout`, `stderr`)_
- `--level <CHOICE>` — Filter by log level _(choices: `debug`, `info`, `warning`, `error`)_
- `--vm-id <TEXT>` — Filter by execution/VM ID
- `-L, --limit <INTEGER>` — Maximum results to return _(default: `100`)_
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

### `avr auth`

Authenticate and manage credentials.

```sh
avr auth [OPTIONS] COMMAND [ARGS]...
```

#### `avr auth login`

Authenticate via browser and store credentials.

```sh
avr auth login [OPTIONS]
```

**Options**

- `--provider <CHOICE>` — OAuth provider to use for CLI login. _(choices: `google`, `github` · default: `github`)_
- `--email <TEXT>` — Work email. Routes through your company's SSO if its domain requires it, ignoring --provider.

#### `avr auth logout`

Revoke the current API key and remove stored credentials.

```sh
avr auth logout [OPTIONS]
```

#### `avr auth status`

Display the authenticated user and connection state.

```sh
avr auth status [OPTIONS]
```

**Options**

- `--show-token` — Display the auth token in plain text.
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr auth switch`

Switch the default host used when AVR_HOST isn't set.

```sh
avr auth switch [OPTIONS] [HOST]
```

```sh
Examples:
    avr auth switch                     # show current default + all hosts
    avr auth switch https://api.avrea.com
```

**Arguments**

- `[HOST]`

### `avr config`

View and manage CLI configuration.

```sh
avr config [OPTIONS] COMMAND [ARGS]...
```

#### `avr config get`

Print the value of a configuration key.

```sh
avr config get [OPTIONS] {org}
```

```sh
Available keys:
  org   Active organization ID
```

**Arguments**

- `KEY` _(choices: `org`)_

#### `avr config list`

Show the active CLI configuration (host, auth, org, default repo).

```sh
avr config list [OPTIONS]
```

#### `avr config set`

Set a CLI configuration value.

```sh
avr config set [OPTIONS] {org} VALUE
```

```sh
Available keys:
  org   Active organization (ID or slug)
```

```sh
Examples:
    avr config set org org-abc123
    avr config set org acme
```

**Arguments**

- `KEY` _(choices: `org`)_
- `VALUE`

#### `avr config unset`

Remove a configuration override.

```sh
avr config unset [OPTIONS] {org}
```

```sh
Available keys:
  org   Drop the stored default organization for the active host
```

```sh
Examples:
    avr config unset org
```

**Arguments**

- `KEY` _(choices: `org`)_

### `avr settings`

View and toggle cache and runner settings.

```sh
avr settings [OPTIONS] COMMAND [ARGS]...
```

#### `avr settings list`

List settings with their current values and source.

```sh
avr settings list [OPTIONS]
```

Inside a connected checkout the repository is auto-detected from the git
remote and its effective values are shown. Pass --org to see organization
values (this suppresses auto-detection), or --repo / AVR_REPO to target a
repository.

```sh
Examples:
    avr settings list --org org-abc123
    avr settings list --org org-abc123 --repo rep-xyz789
    avr settings list --repo acme/web
    avr settings list --prefix cache.
    avr settings list --json key,value,source
```

```sh
JSON FIELDS
    key, source, value
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--repo <TEXT>` — Repository (org/repo or rep-xxx). Auto-detected from git unless --org given.
- `--prefix <TEXT>` — Filter by key prefix (e.g. 'cache.').
- `--web` — Open in browser.
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr settings reset`

Remove a setting override, reverting to the inherited or default value.

```sh
avr settings reset [OPTIONS] KEY
```

Clears the repository override when run inside a connected checkout (the
repo is auto-detected), when --repo is given, or when AVR_REPO is set. Pass
--org to clear the organization-scoped value (this suppresses auto-detection).

```sh
Examples:
    avr settings reset cache.gha.enabled --repo rep-xyz789
    avr settings reset cache.packages.enabled --org org-abc123
```

**Arguments**

- `KEY`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--repo <TEXT>` — Repository (org/repo or rep-xxx). Auto-detected from git unless --org given.

#### `avr settings schema`

List available setting definitions.

```sh
avr settings schema [OPTIONS]
```

```sh
Examples:
    avr settings schema
    avr settings schema --prefix cache. --scope repository
    avr settings schema --json '*'
```

```sh
JSON FIELDS
    choices, default, description, inherits, key, max_value, min_value,
    scopes, value_type
```

**Options**

- `--prefix <TEXT>` — Filter by key prefix (e.g. 'cache.').
- `--scope <CHOICE>` — Filter by scope. _(choices: `repository`, `organization`)_
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr settings set`

Set a setting value.

```sh
avr settings set [OPTIONS] KEY VALUE
```

VALUE is parsed as a boolean (true/false) or integer when possible,
otherwise treated as a string.

Writes a repository override when run inside a connected checkout (the repo
is auto-detected), when --repo is given, or when AVR_REPO is set. Otherwise
targets org scope (the default org, or --org); org-only settings must be set
with --org. A checkout whose repo isn't connected to the org is an error
rather than a silent org-wide write.

```sh
Examples:
    avr settings set cache.gha.enabled false --org org-abc123
    avr settings set cache.packages.enabled true --repo rep-xyz789
```

**Arguments**

- `KEY`
- `VALUE`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--repo <TEXT>` — Repository (org/repo or rep-xxx). Auto-detected from git unless --org given.

### `avr firewall`

Manage the egress firewall rule list for orgs and repositories.

```sh
avr firewall [OPTIONS] COMMAND [ARGS]...
```

#### `avr firewall add`

Add a rule.

```sh
avr firewall add [OPTIONS]
```

Add a rule. Exactly one of --cidr, --fqdn, --any must be specified.

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--repo <TEXT>` — Repository ID. If provided, adds at repo scope.
- `--action <CHOICE>` _(choices: `allow`, `deny` · required)_
- `--cidr <TEXT>` — Destination CIDR (e.g. 10.0.0.0/8 or 1.2.3.4/32).
- `--fqdn <TEXT>` — Destination hostname (e.g. api.example.com).
- `--any` — Catch-all (default) rule.
- `--proto <CHOICE>` _(choices: `tcp`, `udp`, `icmp`, `any` · default: `any`)_
- `--ports <TEXT>` — Port or port range (e.g. 443 or 30000-39999).
- `--position <INTEGER>` — Insert at a specific 0-indexed position.

#### `avr firewall delete`

Delete a rule by ID.

```sh
avr firewall delete [OPTIONS] RULE_ID
```

**Arguments**

- `RULE_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--repo <TEXT>` — Repository ID. If provided, deletes a repo-level rule.

#### `avr firewall flow-summaries`

Show per-VM network activity summaries captured at VM stop.

```sh
avr firewall flow-summaries [OPTIONS]
```

Each row is the totals + top-N destinations for one VM run. The Blocked
column counts both per-rule packet drops and DNS queries the firewall
refused to resolve. Use ``--with-drops`` to triage what the firewall
blocked after editing a rule or ``--job`` to include every execution
attempt for a job.

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--repo <TEXT>` — Repository ID. _(required)_
- `--job, --job-id <TEXT>` — Filter to every VM execution attempt for a job ID.
- `--with-drops` — Show only summaries where the firewall blocked at least one flow.
- `-L, --limit <INTEGER RANGE>` — Max summaries to return. _(default: `20`)_
- `--offset <INTEGER RANGE>` — Number of summaries to skip. _(default: `0`)_
- `--from, --start-after <TEXT>` — Only include summaries that started at or after this ISO-8601 timestamp.
- `--to, --end-before <TEXT>` — Only include summaries that ended at or before this ISO-8601 timestamp.
- `--json` — Emit raw JSON instead of a table.

#### `avr firewall list`

List egress firewall rules for the given scope.

```sh
avr firewall list [OPTIONS]
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--repo <TEXT>` — Repository ID. If provided, shows the repo-level list.
- `--json` — Output rules as JSON instead of a table.

#### `avr firewall move`

Move a rule to a new position (rewrites the full ordering atomically).

```sh
avr firewall move [OPTIONS] RULE_ID
```

**Arguments**

- `RULE_ID`

**Options**

- `--to <INTEGER>` — Target 0-indexed position. _(required)_
- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--repo <TEXT>` — Repository ID. If provided, moves a repo-level rule.

#### `avr firewall set-default`

Set (or replace) the catch-all (default) rule for the scope.

```sh
avr firewall set-default [OPTIONS]
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--repo <TEXT>` — Repository ID. If provided, sets the repo-level default.
- `--action <CHOICE>` _(choices: `allow`, `deny` · required)_

#### `avr firewall show`

Show the resolved (merged) firewall rule list for a repository.

```sh
avr firewall show [OPTIONS]
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified.
- `--repo <TEXT>` — Repository ID. _(required)_
- `--json` — Output resolved rules as JSON instead of a table.

### `avr billing`

Manage billing, invoices, and payment methods.

```sh
avr billing [OPTIONS] COMMAND [ARGS]...
```

#### `avr billing invoices`

Manage invoices.

```sh
avr billing invoices [OPTIONS] COMMAND [ARGS]...
```

##### `avr billing invoices download`

Download an invoice PDF.

```sh
avr billing invoices download [OPTIONS] INVOICE_ID
```

**Arguments**

- `INVOICE_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--out <TEXT>` — Output file path. Defaults to &lt;invoice_id&gt;.pdf.

##### `avr billing invoices list`

List invoices.

```sh
avr billing invoices list [OPTIONS]
```

```sh
JSON FIELDS
    created_at, currency, has_pdf, invoice_id, period_end, period_start,
    status, subtotal_cents, tax_cents, total_cents
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `-L, --limit <INTEGER RANGE>` — Max invoices to return. _(default: `50`)_
- `--cursor <TEXT>` — Pagination cursor from a previous response.
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr billing invoices show`

Show details for a single invoice.

```sh
avr billing invoices show [OPTIONS] INVOICE_ID
```

```sh
JSON FIELDS
    created_at, currency, has_pdf, invoice_id, line_items, period_end,
    period_start, status, subtotal_cents, tax_cents, total_cents
```

**Arguments**

- `INVOICE_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr billing payment-methods`

Manage payment methods.

```sh
avr billing payment-methods [OPTIONS] COMMAND [ARGS]...
```

##### `avr billing payment-methods add`

Add a credit card as a payment method.

```sh
avr billing payment-methods add [OPTIONS]
```

Card details are sent directly to Stripe and never touch Avrea servers.

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--number <TEXT>` — Credit card number. Prefer prompting over --number to avoid shell history. _(required)_
- `--exp-month <INTEGER RANGE>` — Card expiration month. _(required)_
- `--exp-year <INTEGER>` — Card expiration year. _(required)_
- `--cvc <TEXT>` — Card CVC/CVV code. Prefer prompting over --cvc to avoid shell history. _(required)_

##### `avr billing payment-methods list`

List payment methods.

```sh
avr billing payment-methods list [OPTIONS]
```

```sh
JSON FIELDS
    card_brand, card_exp_month, card_exp_year, card_last4, is_default,
    payment_method_id
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr billing payment-methods remove`

Remove a payment method.

```sh
avr billing payment-methods remove [OPTIONS] PM_ID
```

**Arguments**

- `PM_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--yes` — Skip confirmation prompt.

##### `avr billing payment-methods set-default`

Set a payment method as the default.

```sh
avr billing payment-methods set-default [OPTIONS] PM_ID
```

**Arguments**

- `PM_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).

#### `avr billing settings`

Show billing settings.

```sh
avr billing settings [OPTIONS]
```

```sh
JSON FIELDS
    billing_address, billing_emails, metronome_customer_id,
    stripe_customer_id, tax_id
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr billing summary`

Show billing summary for the organization.

```sh
avr billing summary [OPTIONS]
```

```sh
JSON FIELDS
    billing_emails, default_payment_method, has_billing
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr billing update-settings`

Update billing settings.

```sh
avr billing update-settings [OPTIONS]
```

```sh
Examples:
    avr billing update-settings --email billing@example.com
    avr billing update-settings --tax-id EU123456789
    avr billing update-settings --email a@example.com,b@example.com --tax-id FI12345678
```

```sh
JSON FIELDS
    billing_address, billing_emails, metronome_customer_id,
    stripe_customer_id, tax_id
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--email <TEXT>` — Billing email address(es), comma-separated.
- `--tax-id <TEXT>` — Tax ID (e.g. VAT number).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

### `avr audit-events`

View audit events for organization writes.

```sh
avr audit-events [OPTIONS] COMMAND [ARGS]...
```

#### `avr audit-events list`

List audit events for the organization.

```sh
avr audit-events list [OPTIONS]
```

```sh
JSON FIELDS
    acting_api_key_id, action, actor_type, actor_user_id, client_ip,
    created_at, event_data, event_id, resource_id, resource_type
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--resource-type <TEXT>` — Filter by resource type (e.g. api_key, user).
- `--action <TEXT>` — Filter by action (CREATE, UPDATE, DELETE, ...).
- `--actor-user-id <TEXT>` — Filter by acting user id.
- `--from, --created-after <TEXT>` — ISO-8601 lower bound (inclusive) on created_at.
- `--to, --created-before <TEXT>` — ISO-8601 upper bound (exclusive) on created_at.
- `-L, --limit <INTEGER RANGE>` — Max events to return. _(default: `100`)_
- `--cursor <TEXT>` — Opaque cursor from a previous response's next_cursor.
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

### `avr repo`

Manage repositories and public mirrors.

```sh
avr repo [OPTIONS] COMMAND [ARGS]...
```

#### `avr repo list`

List repositories you can access in an organization.

```sh
avr repo list [OPTIONS]
```

```sh
Examples:
    avr repo list
    avr repo list --json full_name,repository_id
    avr repo list --json '*' -q '.[].full_name'
```

```sh
JSON FIELDS
    full_name, platform, platform_repository_id, repository_id
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `-L, --limit <INTEGER RANGE>` — Max repositories to return. _(default: `100`)_
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr repo public-mirror`

Request and browse mirrors of public GitHub repositories.

```sh
avr repo public-mirror [OPTIONS] COMMAND [ARGS]...
```

##### `avr repo public-mirror cancel`

Cancel one pending public-mirror request.

```sh
avr repo public-mirror cancel [OPTIONS] REQUEST_ID
```

An approved mirror is global and cannot be withdrawn by a requester.

```sh
Examples:
    avr repo public-mirror cancel pmr-0123456789abcdef0123456789abcdef
    avr repo public-mirror cancel pmr-0123456789abcdef0123456789abcdef --yes
```

```sh
JSON FIELDS
    approval_state, created_at, github_snapshot, public_access_expires_at,
    reason, repository_full_name, repository_id, request_id,
    requester_organization_id, requester_user_id, review_note, reviewed_at,
    reviewed_by_user_id, status, updated_at
```

**Arguments**

- `REQUEST_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--yes, -y` — Skip the confirmation prompt.
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr repo public-mirror check`

Check whether a public GitHub repository mirror is available.

```sh
avr repo public-mirror check [OPTIONS] FULL_NAME
```

FULL_NAME must be an owner/repository name. This performs an exact lookup;
the global public-mirror catalog cannot be listed.

A repository that is not mirrored is an answer, not a failure: the command
reports ``Available: no`` (``available: false`` under --json) and exits 0.
Only a real failure — denied, malformed name, server error — exits non-zero.

```sh
Examples:
    avr repo public-mirror check rust-lang/rust
    avr repo public-mirror check rust-lang/rust --json '*'
    avr repo public-mirror check rust-lang/rust --json available,default_branch
```

```sh
JSON FIELDS
    approval_state, available, default_branch, https_clone_url,
    installation_kind, is_archived, is_disabled, is_fork, mirror_enabled,
    platform_owner_id, platform_owner_login, platform_owner_type,
    platform_pushed_at, platform_repository_id, platform_size_kb,
    public_access_expires_at, public_metadata_verified_at,
    repository_full_name, repository_id
```

**Arguments**

- `FULL_NAME`

**Options**

- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr repo public-mirror request`

Request a mirror of a public GitHub repository.

```sh
avr repo public-mirror request [OPTIONS] FULL_NAME
```

FULL_NAME is the canonical GitHub owner/repository name. Avrea validates
the repository with GitHub before recording the request.

```sh
Examples:
    avr repo public-mirror request rust-lang/rust
    avr repo public-mirror request rust-lang/rust --reason "Build dependency"
    avr repo public-mirror request rust-lang/rust --org acme --json '*'
```

```sh
JSON FIELDS
    approval_state, created_at, github_snapshot, public_access_expires_at,
    reason, repository_full_name, repository_id, request_id,
    requester_organization_id, requester_user_id, review_note, reviewed_at,
    reviewed_by_user_id, status, updated_at
```

**Arguments**

- `FULL_NAME`

**Options**

- `--reason <TEXT>` — Why your organization needs this repository mirrored.
- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr repo public-mirror requests`

List your organization's public-mirror requests.

```sh
avr repo public-mirror requests [OPTIONS]
```

```sh
Examples:
    avr repo public-mirror requests
    avr repo public-mirror requests --org acme
    avr repo public-mirror requests --json request_id,status,repository_full_name
```

```sh
JSON FIELDS
    approval_state, created_at, github_snapshot, public_access_expires_at,
    reason, repository_full_name, repository_id, request_id,
    requester_organization_id, requester_user_id, review_note, reviewed_at,
    reviewed_by_user_id, status, updated_at
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr repo public-mirror view`

View one public-mirror request.

```sh
avr repo public-mirror view [OPTIONS] REQUEST_ID
```

```sh
Examples:
    avr repo public-mirror view pmr-0123456789abcdef0123456789abcdef
    avr repo public-mirror view pmr-0123456789abcdef0123456789abcdef --json '*'
```

```sh
JSON FIELDS
    approval_state, created_at, github_snapshot, public_access_expires_at,
    reason, repository_full_name, repository_id, request_id,
    requester_organization_id, requester_user_id, review_note, reviewed_at,
    reviewed_by_user_id, status, updated_at
```

**Arguments**

- `REQUEST_ID`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

### `avr org`

Manage organizations and installations.

```sh
avr org [OPTIONS] COMMAND [ARGS]...
```

#### `avr org create`

Create a new organization.

```sh
avr org create [OPTIONS] NAME
```

```sh
JSON FIELDS
    name, organization_id, role, slug
```

**Arguments**

- `NAME`

**Options**

- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr org email-domain`

Claim and verify organization email domains.

```sh
avr org email-domain [OPTIONS] COMMAND [ARGS]...
```

##### `avr org email-domain claim`

Claim a company domain using DNS ownership verification (admin only).

```sh
avr org email-domain claim [OPTIONS] DOMAIN
```

The domain does not need to match your GitHub or Avrea account email.
Publish the returned TXT record, then run ``email-domain verify``.

```sh
Examples:
    avr org email-domain claim example.com
    avr org email-domain claim corp.example.com --org acme
```

```sh
JSON FIELDS
    created_at, dns_record_name, dns_record_value, domain,
    organization_email_domain_id, verified, verified_at
```

**Arguments**

- `DOMAIN`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr org email-domain list`

List claimed organization email domains (admin only).

```sh
avr org email-domain list [OPTIONS]
```

```sh
JSON FIELDS
    created_at, dns_record_name, dns_record_value, domain,
    organization_email_domain_id, verified, verified_at
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr org email-domain set`

Set email domains for automatic org membership (admin only).

```sh
avr org email-domain set [OPTIONS] DOMAINS...
```

Replaces all existing domains — a typo wipes the org's auto-membership
policy. Confirms before applying; pass --yes to skip the prompt (required
when stdout isn't a TTY, e.g. in CI).

```sh
Examples:
    avr org email-domain set example.com
    avr org email-domain set example.com corp.example.com --yes
```

**Arguments**

- `DOMAINS...`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--yes, -y` — Skip confirmation prompt.

##### `avr org email-domain verify`

Check a claimed domain's DNS TXT record (admin only).

```sh
avr org email-domain verify [OPTIONS] DOMAIN
```

Each invocation performs a fresh DNS lookup. If DNS has not propagated,
wait and run the command again.

```sh
Examples:
    avr org email-domain verify example.com
    avr org email-domain verify corp.example.com --org acme
```

```sh
JSON FIELDS
    created_at, dns_record_name, dns_record_value, domain,
    organization_email_domain_id, verified, verified_at
```

**Arguments**

- `DOMAIN`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr org install`

Manage GitHub App installations.

```sh
avr org install [OPTIONS] COMMAND [ARGS]...
```

##### `avr org install add`

Start the GitHub App installation flow.

```sh
avr org install add [OPTIONS]
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--no-browser` — Do not open browser automatically.
- `--wait-seconds <INTEGER>` — Seconds to wait for detection. _(default: `120`)_

##### `avr org install list`

List accessible installations across all your organizations.

```sh
avr org install list [OPTIONS]
```

```sh
JSON FIELDS
    created_at, platform_installation_id, installation_id, organization_name,
    organization_slug, state, target_name
```

**Options**

- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr org install remove`

Remove/suspend a GitHub installation.

```sh
avr org install remove [OPTIONS]
```

Confirms before suspending; pass --yes to skip the prompt (required when
stdout isn't a TTY, e.g. in CI).

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--installation-id <TEXT>` — Installation ID to remove (ins-xxx format) _(required)_
- `--yes, -y` — Skip confirmation prompt.

#### `avr org list`

List organizations you belong to.

```sh
avr org list [OPTIONS]
```

```sh
Examples:
    avr org list
    avr org list --json slug,role
    avr org list --json '*' -q '.[] | select(.role == "admin")'
```

```sh
JSON FIELDS
    name, organization_id, role, slug
```

**Options**

- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr org members`

List organization members (admin only).

```sh
avr org members [OPTIONS]
```

```sh
Examples:
    avr org members
    avr org members --org org-abc123
    avr org members --json name,role
```

```sh
JSON FIELDS
    joined_at, name, role, user_id
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

#### `avr org saml`

Configure SAML single sign-on for an organization.

```sh
avr org saml [OPTIONS] COMMAND [ARGS]...
```

##### `avr org saml configure`

Create or replace SAML configuration from IdP metadata (admin only).

```sh
avr org saml configure [OPTIONS] METADATA
```

METADATA is an IdP metadata XML file; pass - to read it from stdin.
Reconfiguring requires the complete metadata document again.

```sh
Examples:
    avr org saml configure idp-metadata.xml
    cat idp-metadata.xml | avr org saml configure - --org acme
    avr org saml configure idp.xml --email-attribute mail \
        --given-name-attribute firstName --family-name-attribute lastName
```

```sh
JSON FIELDS
    allow_idp_initiated, attr_email, attr_family_name, attr_given_name,
    attr_groups, created_at, default_role, idp_entity_id, idp_slo_url,
    idp_sso_url, is_enforced, jit_provisioning, name_id_format,
    organization_id, organization_saml_config_id, updated_at
```

**Arguments**

- `METADATA`

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--email-attribute, --attr-email <TEXT>` — IdP attribute carrying the member email. _(default: `email`)_
- `--given-name-attribute, --attr-given-name <TEXT>` — IdP given-name attribute.
- `--family-name-attribute, --attr-family-name <TEXT>` — IdP family-name attribute.
- `--groups-attribute, --attr-groups <TEXT>` — IdP groups attribute.
- `--default-role <CHOICE>` — Role assigned to JIT-provisioned members. _(choices: `user`, `admin`, `billing_admin` · default: `user`)_
- `--jit-provisioning / --no-jit-provisioning` — Allow SAML to provision new members. _(default: `True`)_
- `--allow-idp-initiated / --no-allow-idp-initiated` — Allow sign-in initiated from the IdP.
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr org saml enforcement`

Enable or disable mandatory SAML sign-in (admin only).

```sh
avr org saml enforcement [OPTIONS] {on|off}
```

Enabling requires a configured SAML connection and at least one verified
company domain.

```sh
Examples:
    avr org saml enforcement on
    avr org saml enforcement off --org acme
```

**Arguments**

- `STATE` _(choices: `on`, `off`)_

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr org saml remove`

Remove the organization's SAML configuration (admin only).

```sh
avr org saml remove [OPTIONS]
```

Pass --yes to skip the confirmation prompt (required when prompts are
disabled for automation).

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--yes, -y` — Skip confirmation prompt.

##### `avr org saml show`

Show the current SAML configuration (admin only).

```sh
avr org saml show [OPTIONS]
```

```sh
JSON FIELDS
    allow_idp_initiated, attr_email, attr_family_name, attr_given_name,
    attr_groups, created_at, default_role, idp_entity_id, idp_slo_url,
    idp_sso_url, is_enforced, jit_provisioning, name_id_format,
    organization_id, organization_saml_config_id, updated_at
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.

##### `avr org saml sp-metadata`

Print Avrea's SAML service-provider metadata XML.

```sh
avr org saml sp-metadata [OPTIONS]
```

Redirect stdout to a file for import into your identity provider.

```sh
Examples:
    avr org saml sp-metadata > avrea-sp.xml
    avr org saml sp-metadata --org acme
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).

##### `avr org saml test`

Test the SAML connection in a browser (admin only).

```sh
avr org saml test [OPTIONS]
```

The test performs a real IdP sign-in and displays the parsed assertion
without creating a new Avrea session.

```sh
Examples:
    avr org saml test
    avr org saml test --org acme --no-browser
```

**Options**

- `--org <TEXT>` — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- `--no-browser` — Print the test URL without opening a browser.

### `avr health`

Check Avrea platform status.

```sh
avr health [OPTIONS]
```

```sh
Examples:
    avr health
    avr health --json status
    avr health --json '*' -q '.status'
```

**Options**

- `--json <TEXT>` — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- `-q, --jq <TEXT>` — Filter --json output through a jq expression.
