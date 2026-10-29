# Go migration execution record

The target is the full public Python avr command surface, reused by avr-admin.
Repo-intel remains excluded. No Python fallback counts as a native implementation.
Commits are unsigned as requested. No release, push, or deployment is implied.

## Authoritative scope

`go-python-command-contract.json` records the pinned Python command tree:
142 nodes, including 115 executable leaf commands, nested groups, hidden
commands, aliases, options, defaults, choices, and positional arguments.
Regenerate with `python -m scripts.inventory_python_commands`.
The snapshot is a denominator, not proof of behavior parity.

The first unsigned checkpoints are public `e014a76` and admin `d8005a4668`.

## Run lifecycle batch

Added `run rerun` and `run cancel`, inherited by avr-admin.

Decisions:
- Retain Python's default-no confirmation, `--yes/-y`, and
  `AVR_PROMPT_DISABLED` behavior.
- Resolve IDs and URLs with the existing shared resolver. Cancel accepts a
  canonical Avrea ID without an unnecessary preflight GET; rerun reads the
  previous attempt before writing.
- Send the same request bodies as Python. Use generated public API models.
- After rerun, poll for the new attempt for 30 seconds. Retry transient
  failures, surface authorization/client errors, and honor cancellation.
- Preserve success messages, terminal colors, stderr hints and console URLs.
- Exercise mutations against isolated local fixtures; do not rerun or cancel
  customer jobs as a migration test.

Validation:
- 46 Python/Go differential cases, including 7 terminal presentation cases.
- Both public and admin executables use the same suite.
- Go tests cover the poll deadline, cancellation and decline without writes.
- Race tests, vet/lint, schema generation checks and Python tooling checks.

## Workflow inspection batch

Added workflow list/view and the workflows alias, shared with avr-admin.
The run-control checkpoints are public efa6ea0 and admin cf8eb2ac4d.

Decisions:
- Preserve sorting before limiting, nested JSON aliases, missing/null values,
  large platform IDs, repository detection, and the 50-page resolution bound.
- Match Python's fixed-width tables in terminals and pipes, including colors,
  underlined headers, per-job sorting, recent runs, hyperlinks and hints.
- Treat a failed recent-runs fetch as an auxiliary warning after displaying the
  primary aggregate statistics, as Python does.
- Keep elapsed-time tolerance limited to the relative cutoff itself; validate
  that it represents the requested window before normalizing it.

Validation: 72 Python/Go cases per executable, including 12 presentation cases,
repository lookup errors, Git context, pagination, ambiguous names, empty
results, field discovery, jq, sorting/limits, relative windows and browser mode.

## Workflow dispatch batch

Added workflow run with all Python options, including raw fields, JSON stdin,
ref/default-branch selection, watch/no-watch toggles, exit-status and interval.
Workflow inspection checkpoints: public 1bbbc77, admin 78f1192d67.

Decisions:
- Preserve default watch behavior and last-option-wins for -w/-W and
  --watch/--no-watch.
- Retain Python string conversion for JSON input values, including booleans,
  null, integers, floats and nested containers with insertion order.
- Validate inputs before network access. Bound JSON input to 16 MiB and 100
  nested levels, and allow interruption while reading stdin.
- Never guess a default branch. Require an explicit ref when it is unknown.
- Poll for up to 90 seconds using creation time with Python's five-second
  clock-skew allowance and the platform workflow ID when known.
- Retry transient network failures during dispatch polling, following the
  Python helper's documented intent; its current body only catches HTTP errors.
- Reuse the existing run watcher, preserving the human-readable piped output
  of workflow run. Fixed cursor/title escapes leaking into that pipe.
- All dispatch mutations in validation target local fixtures.

Validation: 43 differential cases per executable, including six terminal
cases. Go tests cover temporal matching, poll deadlines, cancellation,
stdin limits and refusal to read JSON from a terminal. The existing inspection
parity suite is rerun because the shared watch renderer changed.

## Remaining work

Run diagnose, job metrics/watch, PR/status, artifacts,
cache/log search/SBOM, configuration/tokens/settings, organization and repository
management, billing/audit/firewall, VM commands, hidden docs, root/help/completion,
all aliases, and a final per-command flag/argument/behavior coverage audit.
Existing implemented commands also remain subject to that exhaustive audit;
the prior suites are substantial evidence, not a claim that every flag combination
has already been checked. Packaging and native OS CI remain separate release gates.
