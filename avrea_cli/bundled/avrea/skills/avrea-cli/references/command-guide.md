# Avrea CLI command guide

Read only the sections needed for the current task. The installed CLI's `--help` output is the
source of truth when it differs from this guide.

## Preflight and discovery

```sh
avr config
avr auth status
avr status
avr workflow list
avr run list --limit 10
```

Most commands auto-detect the repository from the current checkout. Outside a checkout, add
`--repo org/name` or an Avrea repository ID.

## Test a commit in CI

For normal push-triggered CI:

1. Create a coherent commit and push its branch through the repository's usual process.
2. Find the run for the exact commit:

```sh
avr run list --branch feat/x --since 1h \
  --json run_id,head_sha,status,conclusion,workflow \
  --jq '.[] | select(.head_sha == "<full-commit-sha>")'
```

3. Watch the selected run and propagate its result:

```sh
avr run watch <run-id> --exit-status
```

For a manually dispatchable workflow on an already-pushed branch or tag:

```sh
avr workflow run ci.yml --ref feat/x --watch --exit-status
avr workflow run ci.yml --ref feat/x -f target=integration --watch --exit-status
```

`avr workflow run --ref` accepts a branch or tag. Do not assume it accepts a raw commit SHA.

## Triage and retry

```sh
avr run view <run-id> --log-failed
avr run logs <run-id> --failed
avr run logs <run-id> --job test --follow
avr job view <job-id> --log-failed
avr job logs <job-id> --step "Build" --level error
avr job metrics <job-id> --source cpu --source memory --source disk-io
avr job metrics <job-id> --watch
avr job ssh <job-id>
avr run rerun <run-id> --failed
```

Use failed logs before full logs. Check metrics when symptoms include timeouts, OOMs, slow builds,
disk pressure, or unexplained stalls. `avr job ssh` works only while the job VM remains reachable.

## Create and use a sandbox VM

Create an ephemeral Linux sandbox with repository caches and an automatic stop deadline:

```sh
avr vm create --name agent-feat-x --os linux --size 8-vcpu \
  --ssh-key @~/.ssh/id_ed25519.pub --repo org/project --ref feat/x \
  --ttl 4h --ephemeral --wait
```

Choose the OS and size for the workload; do not mechanically use `8-vcpu`. Repository preload and
cache inheritance require `--repo`. `--ref` accepts a branch, not a tag, pull-request ref, or raw
SHA. If an exact SHA is needed, preload its branch and check out the SHA after connecting.

Bootstrap a newly started VM:

```sh
avr vm bootstrap <vm-id> --setup-github --install codex --install-avr \
  --repo https://github.com/org/project --ref feat/x
```

Add `--forward-agent-creds` or `--env KEY` only when required. Preview a sensitive or complicated
bootstrap plan with `--print`.

Work interactively or run one command:

```sh
avr vm ssh <vm-id> --session work
avr vm ssh <vm-id> -- uname -a
avr vm ssh <vm-id> --login -- codex --version
```

One-off commands use a non-login shell. Add `--login` when bootstrap-forwarded environment values
or login-shell setup is needed. Use a named session for long-running work that must survive a
dropped connection.

Inspect and clean up:

```sh
avr vm show <vm-id>
avr vm usage
avr vm stop <vm-id> --wait
avr vm delete <vm-id> --yes --wait
```

Stopping discards the ephemeral disk. Copy out or commit artifacts before stopping. Deleting also
removes the VM record from normal listings, though usage metering remains available.

## Structured output and scripting

```sh
avr run list --json '?'
avr run list --json run_id,head_sha,status,conclusion
avr run view <run-id> --json conclusion,jobs --jq '.jobs[]'
avr workflow list --json name,runs,median_duration_seconds \
  --jq 'sort_by(.median_duration_seconds) | reverse | .[:5]'
```

`--jq` filters the projected JSON directly. When stdout is not a TTY, watch commands can emit
NDJSON; use `--ndjson` to make that choice explicit.

For non-interactive commands:

```sh
AVR_PROMPT_DISABLED=1 avr run watch <run-id> --exit-status
```

Handle exit status `4` as “authentication required,” `2` as a usage error, and `1` as a general
failure.

## Cache and performance inspection

```sh
avr status --since 7d
avr workflow view ci.yml --since 30d
avr cache usage --repo org/project
avr cache list --repo org/project
```

Use workflow medians/p95s, job metrics, and cache usage to explain performance. Do not delete cache
entries merely to experiment: cache deletion affects shared repository performance and requires a
specific reason.
