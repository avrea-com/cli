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

## Active-job watch batch

Added job watch and the jobs alias, shared with avr-admin.
Workflow dispatch checkpoints: public 87f4b55, admin 83ebbb86ae.

Decisions:
- Preserve the default five-second interval, active-state query, repeated
  repository/name filters, Git resolution and one JSON snapshot per refresh.
- Stream snapshots automatically when piped, or explicitly with --ndjson.
  Reuse the job-list table, colors, underlines and links in terminal mode.
- Retry transient HTTP/network failures until interrupted; propagate other
  client errors with the existing auth/exit-code contract.
- Retain the previous terminal frame during a retry. Exact title/redraw events
  are outside the compatibility target.
- Preserve commands' explicit Ctrl-C handling at the shared exit boundary.
  It no longer adds a redundant Interrupted message after Stopped watching.
- Fix ordered --on-avrea/--shadowing flags so the last occurrence wins.
- Normalize only query-key ordering in API diagnostic URLs; every parameter,
  repeated value and value order remains checked.

Validation: 28 differential cases per executable, including eight terminal
cases, real process interruption, snapshots, filters, retries and both toggle
orders. Go tests cover blocked-request cancellation, transport retries and
integer preservation. Three comparator tests prove that semantic differences
still fail. The existing 79 job cases are rerun for both executables.

## Job metrics batch

Added job metrics, shared with avr-admin.
Active-job watch checkpoints: public 08ced1c, admin aa09500499.

Decisions:
- Preserve all seven sources, repeated source order, start/end bounds
  (including zero), default CPU/memory selection and raw JSON output.
- Match CPU idle-based utilization, memory used-state aggregation, counter
  rates, runner-spec scaling, saturation colors and peak-marker backgrounds.
- Refresh logs every second and metrics every five seconds in live mode;
  retain the last 12 visible lines and use the raw pagination cursor.
- Preserve Python's completed-job fallback, partial 404 placeholders,
  transient retry footers, terminal client errors and silent Ctrl-C exit 130.
- Keep the Python behavior that live job metadata is fetched once. The
  watcher does not auto-exit when a running job subsequently completes.
- Extend terminal parity to background colors. Regression tests first proved
  that missing, wrong and reset backgrounds went undetected.
- Keep exact redraw sequencing outside the compatibility target. No new
  dependencies and no remote mutations.

Validation: 55 differential cases per executable, including ten terminal
cases, all source types, scaling, empty data, JSON, bounds, errors, live
pagination/retries and real interruption. Go tests cover cadence, bounded
tails, raw cursors, blocked-request cancellation and sanitized transport errors.
The stronger style comparator also passes 186 previously captured presentation
cases across both binaries. The 23 comparator tests, Go race tests, lint,
generation checks and paired admin build pass.

## Run diagnostics batch

Added run diagnose, shared with avr-admin.
Job metrics checkpoints: public d62000a, admin 38910a6b3f.

Decisions:
- Reuse validated run references, including GitHub numeric IDs and URLs.
- Match Python's bounds: 100 jobs, 1,000 steps across the report, ten failed
  jobs with logs, 100 lines and 25 KiB of UTF-8 log content per failed job.
- Fetch jobs, metrics and the prior-success baseline concurrently, then fetch
  eligible log excerpts concurrently. Preserve deterministic report order.
- Keep auxiliary HTTP failures and malformed data as structured warnings.
  A jobs fetch failure remains fatal. Cancellation never becomes a successful
  partial report. Already-started fetches share cancellation and timeouts.
- Preserve queue/execution/start-offset timing, clock-skew warnings, p95
  interpolation and counter-reset totals. Keep raw API fields and numeric
  precision, including whole-number float values in the generated report.
- Retain raw page envelopes so the diagnostics baseline can distinguish
  missing data from a valid empty result.
- Normalize only independently verified elapsed-time fields, generation time
  and concurrent request arrival order in parity checks. Request bodies,
  counts, authorization and all other evidence remain strict.
- No new dependencies or remote mutations.

Validation: 53 differential cases per executable, including four terminal
cases. Coverage includes source failures, malformed data, truncation, UTF-8
bounds, clock skew, active timing, references and partial-report colors.
Two malformed-response regressions were reproduced before fixing them.
Five comparator tests protect evidence and request checking. Go tests cover
concurrent requests, cancellation and a generation-based baseline window;
Go race tests, lint, generation checks and paired admin checks pass.

## Remaining work

PR/status, artifacts,
cache/log search/SBOM, configuration/tokens/settings, organization and repository
management, billing/audit/firewall, VM commands, hidden docs, root/help/completion,
remaining aliases, and a final per-command flag/argument/behavior coverage audit.
Existing implemented commands also remain subject to that exhaustive audit;
the prior suites are substantial evidence, not a claim that every flag combination
has already been checked. Packaging and native OS CI remain separate release gates.
