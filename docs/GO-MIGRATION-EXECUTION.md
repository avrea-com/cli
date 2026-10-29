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

## Remaining work

Workflow list/view/run, run diagnose, job metrics/watch, PR/status, artifacts,
cache/log search/SBOM, configuration/tokens/settings, organization and repository
management, billing/audit/firewall, VM commands, hidden docs, root/help/completion,
all aliases, and a final per-command flag/argument/behavior coverage audit.
Existing implemented commands also remain subject to that exhaustive audit;
the prior suites are substantial evidence, not a claim that every flag combination
has already been checked. Packaging and native OS CI remain separate release gates.
