# Go migration plan for avr and avr-admin

Status: public implementation rebased onto origin/main; refreshed local parity validation complete. Release/admin-only phases remain.
Updated: 2026-10-30.
Worktree branch: `cli/go-migration`.
Initial public CLI baseline: `99e74d1`, fetched from `origin/main`.
Current public/Python baseline: `96d1ccb987032f35b10c7bb0b9c90816da1323a0` from `origin/main`.
Current admin reference baseline: `85e8b2af0d66f55da8989d0718aeb35887e43349` from `origin/main`.

Rewrite the public `avr` CLI in Go, expose a small reusable command and client
surface, and compose `avr-admin` from it. Target roughly 99% practical compatibility
with familiar commands, useful terminal output, and normal workflows. This is a
product target, not a byte-equality requirement or a literal test-pass threshold.
Use differential validation against Python to identify meaningful regressions.

The first release milestone is a fully Go public CLI and a Go admin entry point
that runs public commands directly and explicitly delegates remaining admin
commands to Python. Retire temporary legacy CLI dispatch as commands migrate.
`repo-intel` stays untouched and Python-backed, including the existing invocation
path needed to use it. Removing every Python dependency is not a completion goal.

The current Go admin preview composes the public command tree only. Admin-only
commands and overrides still require the existing Python admin executable.
Legacy dispatch and native release packaging are later phases.

Implementation was subsequently authorized. Publishing and deployment remain
separate steps. Current implementation decisions and evidence are maintained in
[GO-MIGRATION-EXECUTION.md](GO-MIGRATION-EXECUTION.md); the dated checkpoints
below describe earlier stages.

## Motivation and success criteria

Go should simplify distribution and execution: remove PyApp's first-run Python
dependency downloads, make the public CLI easy to place on VMs, improve Windows
installation, and reduce startup overhead where measurements support it.

Measure the current released Python/PyApp build and Go build on the same supported
platforms: first-run setup and network activity, cold and warm help/version
latency, representative command startup, binary size, and memory. A native
public release must run help, version, and completions after offline unpacking
without installing a runtime or fetching dependencies. API commands still
require network access. Establish latency targets from the measured baseline
before claiming performance improvements.

## Auth preview checkpoint (2026-10-29)

The first implementation checkpoint is the standalone Go auth preview:
login, status, switch, logout, their existing aliases, and the hidden Git
credential helper. It includes shared exported packages, direct gojq, a pinned
Python-versus-Go auth runner, and build/test/lint/parity CI. See
[GO-AUTH-PREVIEW.md](GO-AUTH-PREVIEW.md) for usage, validation, dependencies, and
explicit differences.

This was the basic-auth checkpoint, not completion of Phase 1. The inspection
checkpoint below adds the schema gate and actual admin composition. Release
cutover remains later work. Python and repo-intel are unchanged.

## CI inspection checkpoint (2026-10-29)

The Go preview now includes `org list`, `repo list`, `run list`, `run view`,
and `run watch`. Shared client boundaries use generated public wire types.
The released oapi-codegen 2.8.1 gate passed against both complete OpenAPI 3.1
schemas from core commit `d08ef5b1aaf7e521c51068b68ea116e3f54ddfc8`.
Preparation hoists referenced nested definitions, preserves implicit open
objects, and uses float64 for unformatted numbers; it does not downgrade
the schema. Generation drift is checked in public CI.

The fixture runner compares data and types, requests, exit codes, selected
terminal information, logs, polling events, retries, interruption, and closed
consumers. Raw captures and comparison reports live under ignored `build/go`.
Terminal layouts, status symbols, colors, underlines, emphasis, and truncation
follow Python closely. Harmless trailing padding and redraw sequences may differ. Go uses direct child-process argument parsing
for configured pagers instead of evaluating shell expressions. Run references
use canonical UUIDv7 IDs; retired short IDs are not supported.

The minimal admin executable lives in core's `cli-go` module on
`cli/go-inspection-admin`. It composes fresh public commands, preserves admin
environment resolution, and shares the execution/exit-code boundary. Since
the public Go module is not published yet, the preview requires an explicit
local workspace:

```sh
make -C /path/to/avrea-core cli-go-check AVR_CLI_DIR=/path/to/cli
/path/to/avrea-core/cli-go/build/avr-admin run list --json '*'
```

The final public CLI and real admin executable each passed all 143 paired
inspection cases with zero meaningful differences. The public auth suite passed
all 65 cases. The refreshed fixture reports record hashes matching the rebuilt
binaries. Five read-only staging comparisons passed at the original inspection
checkpoint for organization/repository listing and completed-run list, view, and
watch across Python and both Go executables; those staging captures predate the
terminal styling follow-up.

The admin module is wired into core's root Make test/lint graph. The
`Go admin preview` workflow accepts an explicit public CLI ref and runs the
paired checks; it has passed actionlint locally but has not been dispatched.
The generated local workspace is replaced atomically when AVR_CLI_DIR changes.

This milestone is complete locally. A terminal-fidelity follow-up adds 33 PTY
presentation cases, including color-enabled output and no-color opt-outs, and
comparator regressions for missing styles and changed layouts. A live comparison
against installed Python 0.4.1 matched all 42 repo rows in visible layout and glyph
styles. Terminal coverage includes visible labels,
run/branch hyperlinks and opt-outs, colored log readability, early pipe closure,
and pager early exit and graceful interruption. Pager interruption exits 0;
watch interruption exits 130. Local source review and final executable validation
are complete. Public Go checks (race tests, lint, generation, differential checks,
and vulnerability scan), core cli-go-check, Python lint, and all 1,560 Python
tests pass. See GO-AUTH-PREVIEW.md for the handoff and intentional differences.

Native three-OS CI remains a release gate and has not run remotely. CodeRabbit
is installed but signed out, so no external CodeRabbit review has run. No
release cutover, publishing, or server changes have occurred. The full Phase 1
gate still requires an admin-only extension and native packaging/platform
validation. Job SSH now exercises the representative process/connection boundary.

## Job inspection checkpoint (2026-10-29)

The preview adds `job list`, `job view`, `job logs --follow`, `run logs`,
and `job ssh` to both executables through the shared public command tree.
Generated wire models now include the pinned job list/detail and SSH forwarding
operations. No additional runtime dependency was needed.

The job corpus compares both executables with the pinned Python reference:
79 scenarios, including 21 color-enabled/no-color terminal presentation cases.
The scope includes JSON field aliases, status/filter/pagination requests,
long-name truncation, links, grouped logs, raw cursors through hidden diagnostics,
completion and delayed log draining, real interruption, and stubbed SSH process
arguments/exit codes. Go tests additionally cover child-process cleanup and
invalid endpoint/control-character handling. Static log bodies preserve tabs
and newlines while escaping unsafe controls.

OpenSSH is invoked with the existing Python job-SSH options. The compatibility
tests do not establish a real runner connection or alter forwarding on staging.
Read-only staging validation covers job listing, job details, and static logs.
Native remote CI and a real interactive runner SSH session remain unverified.

## Decisions and boundaries

- Use `itchyny/gojq` directly for embedded jq evaluation. This is an accepted
  user-visible change from invoking the user's system jq: no jq installation is
  required, and expression semantics follow the bundled gojq version. Do not
  automatically fall back to system jq.
- Leave `repo-intel` unchanged. Do not port, refactor, or redesign that project
  or its workflow as part of this migration; retain its existing Python path.
- Keep the public module in `avrea-com/cli`. Keep admin code and internal API
  schemas in `avrea-core`.
- Reuse public commands by composing fresh command objects with explicit
  dependencies, replacing Python inheritance and mutation of shared groups.
- Preserve the existing command surface, credential format, and configuration
  precedence unless an intentional difference is recorded and accepted.
- Build the Python-versus-Go comparison harness before porting command families.
  A migrated command is complete when meaningful differences have been resolved
  or accepted. Formatting-only differences do not require individual exceptions.
- Learn architecture and tooling ideas from gh, Depot, and Namespace. Implement
  Avrea behavior independently; do not copy their application source.
- Treat new CI authentication methods, protocol changes, new command syntax,
  and new interactive features as separate proposals.

The current admin CLI imports the public registry, subclasses its configuration
and HTTP client, replaces cache/settings/billing/audit-events/health commands,
and extends nested groups such as org and workflow. It also contains cloud,
SSH, database, provisioning, installation, and shared Python workflows. Those
dependencies make a staged migration preferable to simultaneous replacement.

## Libraries and tooling

| Area | Selection | Use |
| --- | --- | --- |
| Commands | Cobra and pflag | Command trees, aliases, flags, help, completion, documentation |
| JSON filtering | itchyny/gojq directly | In-process jq expressions |
| HTTP | Go net/http | Shared transport, authentication, deadlines, streaming |
| API generation | oapi-codegen, subject to the schema gate | Generated API clients and wire types |
| Compression | klauspost/compress/zstd | Preserve Zstandard API response support |
| Terminal capabilities | golang.org/x/term | TTY detection, terminal size and state |
| Styling | Small Avrea output layer | Match Python tables, colors, emphasis, links and underlines |
| Prompts | Standard library and x/term | Existing confirmations, selection and hidden-input behavior |
| Interactive views | Shared native polling and terminal rendering | Existing public watch commands; admin fleet remains later work |
| Releases | GoReleaser, planned | Native archives, checksums, build metadata, Homebrew publication |
| Tests | testing, httptest and pytest differential harness | Unit, HTTP contract, terminal and executable behavior tests |
| CI checks | golangci-lint v2 and govulncheck | Align with avrea-core lint conventions, including relevant govet and Staticcheck checks |

The implemented public commands did not require Lip Gloss, Huh, Bubble Tea,
go-cmp or testscript. Keep those out of the dependency graph until a concrete
need arises. Terminal width uses go-runewidth, shell argument parsing uses
go-shellquote, and Unicode handling uses x/text. go-gh is not a dependency.

Use explicit configuration structs and loaders rather than Viper. Own a small
Avrea output layer rather than importing gh internals or adopting go-gh as the
application runtime. Use standard-library process execution and preserve the
existing external SSH/client behavior where appropriate; do not introduce a new
SSH implementation merely because the CLI language changes.

Select and pin toolchain, library, and generator versions at implementation
kickoff. Align the Go toolchain with avrea-core's Go 1.27 baseline, choosing an
eligible patch release. Reuse its root golangci-lint v2 configuration for admin
and align public CLI rules with it. Wire the new module into the actual lint
and test graph; configuration discovery alone does not provide CI coverage.

Apply the three-day dependency-age rule manually in dependency PR and release
checks initially, recording version/release-date evidence. A new Go age-gate
service or CI implementation is outside this migration. Retain third-party
license notices in distributions.

Both inspected Avrea API specifications are OpenAPI 3.1.0. The generator gate
must test a released, eligible oapi-codegen version against the actual public
and internal schemas, including nullability, omitted values, unions, enums,
and additional properties. Current development documentation is not proof
that a particular release supports all required constructs. If the gate fails,
resolve the generator choice before broad command migration; do not silently
weaken or downgrade schemas.

Generate only the API surface needed by the CLI where practical. Commit
generated output and reproducible generation inputs/configuration. Check for
generation drift in CI. Keep generated wire types behind a small handwritten
client boundary so generator details do not spread through command handlers.

## Architecture

Proposed public module layout:

```text
cmd/avr/          Thin executable entry point
client/           Public API client and transport
config/           Host, credentials, organization, repository resolution
operations/       Operations shared by multiple commands
commands/         Public command constructors and typed options
output/           Tables, JSON, jq, progress, terminal behavior
internal/         Implementation details not consumed by admin
```

Proposed initial admin module layout in `avrea-core/cli-go`:

```text
cmd/avr-admin/    Admin executable
adminclient/      Internal API client
adminconfig/      Admin environment resolution
commands/         Admin commands and extensions
legacy/           Explicit dispatch to the Python implementation
```

Admin imports a tagged public module version. Packages needed across repositories
must be exported rather than placed under Go's internal directory. The public
module must not depend on the admin module.

Generate API wire types from the schemas rather than translating shared Python
models by hand. For domain helpers genuinely needed by several Go services,
first reuse the existing services/shared or shared modules after checking import
boundaries. Public ID/config helpers belong in the public CLI module when admin
also consumes them; internal fleet/scheduler wire types belong with the internal
generated client. Do not collect unrelated cross-service logic under admin just
because it is the first consumer.

Update the admin CLI style guide with Cobra examples and framework-independent
output/argument conventions before migrating admin command families.

Command constructors receive the capabilities they need: clients, configuration,
input/output streams, clocks, browser opening, and process execution. Avoid
global configuration and shared mutable command objects. Keep simple commands
simple rather than adding an operation abstraction for every API call.

Provide explicit registration points for admin overrides and nested extensions.
Keep authentication and host selection separate from presentation. Separate
authenticated API traffic from external downloads so credentials are not attached
to unrelated hosts.

Centralize output policy: stdout versus stderr, JSON projection, colors, OSC
hyperlinks, prompts, progress, and terminal capabilities. Treat external text
such as logs separately from application-authored terminal controls. Preserve
raw content for downloads and structured output where required.

## Compatibility contract

Human-facing terminal output is the primary comparison surface: the same useful
information, understandable statuses and errors, recognizable tables and prompts,
and comparable workflows. Match normal layouts, colors, underlines, emphasis,
and truncation closely. Exact escape bytes, trailing padding, wrapping during
resizing, progress animation, and redraw sequences may differ. Do not build
exhaustive resize, mouse-event, or suspend/resume parity suites. Use representative
interactive smoke checks for usability, interruption, and leaving the terminal
usable after exit.

Keep stricter checks where programs depend on results: structured data, documented
exit codes, credential-helper messages, spawn-controller behavior, and persisted
configuration. Small presentation differences must not hide missing information,
wrong outcomes, or broken automation.

Inventory the complete public and admin command trees, including hidden commands,
aliases, nested extensions, and overrides. For each command record:

- Arguments, flags, defaults, validation order, and global-flag placement.
- Configuration paths, environment precedence, host-specific credentials,
  organization/repository selection, and existing hosts.json behavior.
- API calls, query parameters, payloads, and validation before mutations.
- Output fields, types, null handling, meaningful ordering, errors, exit codes,
  stdout/stderr placement, and noninteractive behavior.
- Prompts, colors, hyperlinks, streaming, interruption, and process cleanup.
- Files written, downloads, permissions, and external executable invocation.
- User-Agent identity: retain the avr-cli product token and version/commit data;
  changing runtime metadata from Python to Go is an expected difference.

The Python implementation is the comparison baseline, not unquestionable truth.
Known defects and intentional changes need explicit contracts and separate tests.

### Existing consumers and upgrade contracts

| Consumer | Required migration work |
| --- | --- |
| Control API contract test | Replace Python source-scanning dependence before it becomes stale; preserve server-versus-released-CLI compatibility checks |
| docs-public | Preserve Starlight reference paths, frontmatter, sidebar module/export shape, and hidden docs command behavior, or coordinate equivalent consumer changes |
| Git credential helper | Test avr as the callee using Git's stdin/stdout protocol, capability negotiation, host/path scoping, and failure behavior |
| Spawn controller | Preserve spawn input/idempotency behavior and exit meanings: 0 submitted, 1 retryable, 2 refused |
| Existing mirror clones | Exercise Git configuration created by Python before replacing or moving the installed CLI |
| Homebrew and release tooling | Preserve the expected binary/completions archive layout, SBOMs, provenance, and release procedure |

Mirror clones store the resolved executable path in Git configuration. Prefer
preserving that path through a stable launcher when switching distributions.
Where installation methods cannot preserve it, provide an explicit repair path
for user-selected repositories and a documented upgrade step. Do not silently
scan and rewrite arbitrary clones. Validate a Python-created clone can fetch
after the supported upgrade path.

Generation drift is not a complete replacement for control's CLI contract test.
Pin the schema inputs for reproducible generation, and keep server-side checks
against the released CLI's required endpoints, fields, and parameters. Use
request-level tests for handwritten behavior. Retain the existing Python check
until its replacement covers the supported clients; remove control's Python
CLI dev dependency only when no remaining consumer needs it.

### Direct gojq behavior

Preserve field selection before jq evaluation, existing raw scalar output
conventions, and clean separation of output and errors. Pin and report the
system jq version used by the Python baseline so comparisons are reproducible.
The Go side always uses its pinned embedded library. Test the accepted difference
that Go jq filtering works even when the system jq executable is absent.
Decode numbers without unnecessary float64 precision loss. Support cancellation
during evaluation.

Test syntax errors, evaluation errors, empty streams, multiple results, strings,
booleans, nulls, large integers, and representative existing expressions.
Document intentional differences from system jq, including regex and numeric
semantics. Do not broadly allow all jq-output differences merely because the
engine changed.

## Python versus Go differential validation

This is a release gate and a per-command completion requirement, judged against
the practical compatibility scope above rather than byte-for-byte equality.

Build this as a separate, bounded workstream. Start with a minimal paired runner,
isolated homes, fixture server, terminal captures, and readable difference report
for a few representative commands. Add process stubs and specialized comparison
rules only when an actual command requires them. Re-estimate after this first
working harness rather than treating harness development as free setup.

### Harness execution

For each scenario, run the same invocation against pinned Python and Go builds.
Each run gets its own temporary directory, isolated configuration/home locations,
and an independent copy of the same initial filesystem fixtures. Match working
directory, environment, locale, timezone, and terminal mode.

Use a controlled HTTP fixture server to return equivalent responses to each
implementation and record requests. Reset scenario state between runs. Control
time where necessary, and use fake external executables for process contract
checks. Capture human-readable output from each implementation and show it side
by side. Add a small set of real-terminal and real-process smoke checks for
important workflows; do not require exact terminal-event reproduction.

Do not use real developer credentials or production mutation targets. Fixtures
and reports must not contain live tokens or sensitive customer data.

### Comparison dimensions

| Dimension | Compare |
| --- | --- |
| Invocation | Parsing, aliases, defaults, validation before side effects |
| Exit behavior | Exit code, timeout, cancellation |
| Structured output | Fields, types, values, null versus missing, array order, numeric precision |
| Human output | Information shown, status/error meaning, useful table fields, understandable prompts, stdout/stderr placement, and readability; exact rendering may differ |
| API behavior | Method, path, query, relevant headers, body, call count and sequence |
| Local effects | Configuration, credential fixtures, downloads, permissions |
| Process behavior | Executable and arguments, environment, working directory, signals, cleanup |

Compare parsed JSON structurally. Object-key order may be ignored unless an
explicit text-output contract requires it; array order and null/missing
differences remain significant. Preserve numeric precision in the comparator.

For human output, compare normalized visible text and key information, and review
representative rendered captures for readability. Compare colors, bold/dim and
underline attributes on visible text, plus representative layouts and truncation.
Normalize equivalent ANSI encodings, trailing padding, and moving clocks. Keep original
captures available so normalization cannot conceal lost or unreadable content.
Human wording may differ if meaning and useful detail remain equivalent. Reserve
exact output assertions for actual protocols and narrowly justified contracts.

For machine-visible data and other behavior, normalize only named nondeterministic
values such as fixture ports, temporary paths, generated request IDs, and
intentionally variable timestamps. Scope
normalization to known fields or locations. Do not remove entire output sections,
sort all arrays, or discard meaningful headers to make comparisons pass.

For concurrent requests, compare required ordering constraints and the request
set rather than incidental scheduling. Any intentional change in request count,
retry behavior, or ordering must still be explained.

### Required scenarios

- Successful reads and mutations; empty and multiple results; pagination.
- Invalid arguments, unknown flags, conflicting options, and missing credentials.
- Host/environment selection and existing credential/configuration loading.
- API validation failures, authentication failures, rate limits, server errors,
  malformed responses, timeouts, and connection failures.
- JSON field discovery/projection and jq success/failure behavior.
- Representative TTY and piped output, noninteractive prompts, and readable
  no-color output; normal terminal styling and layouts should closely match.
- Downloads, streaming, interrupted watches, broken pipes, and child cleanup.
- Public commands invoked through admin, admin overrides, nested extensions,
  and legacy routing.

Every command requires representative success and relevant failure cases.
Shared components receive the deeper boundary matrix; avoid mechanically
duplicating every transport case across every command.

### Difference disposition and reports

Classify meaningful differences after applying the agreed presentation policy:

1. Go regression: fix before that command is released.
2. Intentional change: record rationale, expected behavior, affected consumers,
   and an explicit test.
3. Python defect: establish correct behavior independently and add a regression
   test; document why Go intentionally differs.

The presentation policy is a shared allowance; do not create a waiver or test
for every harmless trailing space, equivalent escape encoding, or resize wrap. Behavior and machine-contract
exceptions must remain narrow, explained, and tested. No blanket allowance for
missing information, incorrect results, or broken workflows.

Produce a per-command report containing the scenario, invocation, environment
summary, baseline/build identifiers, both exit results, output differences,
HTTP/file/process differences where relevant, and mismatch disposition. Distinguish
informational rendering differences from failures. Provide a concise
summary of passed, failed, accepted-difference, and untested scenarios.

Run relevant scenarios in CI for each migrated command. Run the full migrated
surface before a release. Baseline updates must be explicit and reviewed so
upstream Python changes cannot silently alter expected behavior.

### Staging and operational validation

After deterministic fixtures pass, compare read-only commands against staging.
Use stable resources or account explicitly for changing server state; do not
treat arbitrary live differences as regressions or normalize them away.

Mutations run against fixtures or equivalent isolated resources created for each
implementation. Never blindly execute the same mutation twice against a shared
real resource. Verify resulting state as well as output.

Provisioning and installation commands require isolated environment validation
before replacing their legacy routing. Differential equality alone does not
prove infrastructure safety or successful operational outcomes.

## Hybrid admin transition

The Go admin executable owns routing. Every supported command path resolves to
exactly one implementation:

- Public commands execute through the imported Go module.
- Migrated admin commands execute in Go.
- Explicit legacy paths invoke the pinned Python implementation.
- Unknown commands return usage errors; they do not fall through to Python.

Routing operates at nested command paths where needed. For example, org list
may execute Go while org reconcile-grants executes Python.

Preserve arguments, working directory, resolved environment, terminal streams,
exit status, cancellation, and cleanup. Define ownership of public/internal
endpoint resolution so Go and Python cannot silently target different hosts.

Use checked metadata for legacy help and completion so normal command discovery
does not import Python. Keep the Python public CLI dependency pinned while legacy
admin still imports it. Give the legacy entry point a distinct name or module
invocation to prevent recursive dispatch.

At Go public cutover, move Python avr-cli to a maintenance-only line: necessary
security and compatibility fixes, no routine feature parity. Pin an exact tested
version for legacy admin and update it deliberately when fixes are required.
Do not require new public features to be implemented twice after cutover.

Separate temporary legacy CLI routing from the retained Python-backed repo-intel
invocation. The latter is an explicit scope exception, not unfinished porting.
Preserve its existing interface and dependencies without changing repo-intel.

## Phases and exit gates

| Phase | Deliverables | Exit gate |
| --- | --- | --- |
| 0 Inventory and baseline | Command/dependency matrix, compatibility scenarios, comparison harness, startup/install baseline | Every command is classified; representative Python scenarios and comparison diagnostics work |
| 1 Shared foundation | Config/auth, HTTP, output/gojq, generator validation, representative commands, minimal Go admin, native packaging, three-OS Go/differential CI alongside Python checks | Shared public commands work in both executables; meaningful comparisons and generator/platform gates pass |
| 2 Public coverage | Complete Go public command surface and existing-consumer coverage | All commands have representative coverage and no unresolved material regressions |
| 3 Public release and hybrid admin | Native public distribution and usable Go admin with legacy dispatch | Clean-machine installation, existing scripts, credentials, help/completion, and routing verified |
| 4 Admin migration | Admin command families move to Go | Each family passes differential and operational checks before routing changes |
| 5 Legacy CLI retirement | Remove replaced Python CLI handlers and temporary routing | Migrated commands no longer need legacy CLI code; repo-intel and its existing Python invocation remain supported and untouched |

Phase 1 must exercise browser login and existing credentials; a list command
with field selection and jq; a streaming/watch command with interruption; an
SSH or forwarding command; and both public reuse and an admin extension.

Port public commands in batches: read-only inspection/status; configuration and
mutations; logs/diagnostics/downloads/artifacts; then full VM lifecycle and
connection coverage. Start difficult process and transport behavior in Phase 1
even when complete command coverage comes later.

Migrate admin in dependency order: internal HTTP commands; noninteractive
diagnostics; cloud/SSH operations; then provisioning, installation, and database
commands. Schedule the fleet TUI late in Phase 4, following its current supported
workflows with broadly equivalent output. No long-lived fleet feature freeze
or exhaustive terminal-event compatibility project is required. Track meaningful
upstream changes during the port.

repo-intel is excluded from migration work. Keeping its existing Python-backed
invocation does not prevent Phase 5 from completing.

## Release and quality gates

- Required differential suite passes with no unresolved material regressions;
  equivalent terminal presentation is sufficient.
- Focused unit and HTTP tests cover behavior that executable comparisons cannot.
- Supported platform smoke tests exercise real subcommands and process behavior.
- Clean-machine installation works; help, version, and completion work offline.
- Documentation and completion generation are reproducible, including docs-public
  Starlight pages/sidebar and the hidden docs command consumed by tooling.
- Git credential and spawn-controller protocol tests pass, including the supported
  upgrade path for existing mirror clones.
- Control API contract checks continue covering released CLI requirements.
- Existing hosts.json and environment conventions work across the transition.
- Startup time, memory, binary size, and first-run behavior are measured against
  the baseline. Performance claims must be based on measurements.
- GoReleaser produces native artifacts, checksums, and required license notices.
  Preserve Homebrew's per-target binary/completions layout and Windows ZIP format.
- Generate release-matched CycloneDX and SPDX SBOMs using oss-report's Go support,
  and preserve SLSA build-provenance attestations for distributed artifacts.
- Revise the existing release procedure and avrea-cli-release skill for native
  builds, maintenance-only Python releases, completion packaging, and provenance.
- Validate CGO_ENABLED=0 where supported by the selected dependency graph.
- CI keeps existing Python and docs checks during transition and adds Go lint,
  tests, generation checks, and differential validation on Linux, macOS, Windows.

Stage native Go releases alongside the available Python distribution before
switching defaults. Define the uvx/pipx transition and executable-path preservation
or explicit clone repair before cutover. Homebrew installations continue to
update through Homebrew; do not add competing update
mechanisms during the rewrite.

Keep Python available while hybrid admin depends on it. Advance the public
module version used by admin through explicit dependency updates and joint
compatibility checks. Track upstream command changes during the migration and
update the inventory and baseline deliberately.

## Effort and remaining decisions

The earlier 6-12 engineer-week estimate for the public Go CLI plus hybrid admin
is a provisional envelope, not a committed schedule. Estimate the differential
harness separately from public-command porting, admin routing, existing-consumer
updates, and release engineering. Re-estimate after the minimal Phase 0 harness,
then again after the representative Phase 1 Go slice.

Scope later admin work by command family after those milestones. The earlier
2-4+ engineer-month estimate for complete admin conversion is not a promise of
removing all Python. repo-intel work and exhaustive terminal-event parity are
excluded from both scope and estimates.

Resolve during the initial phases:

- The released generator/toolchain/library versions and actual schema coverage.
- Required release platforms beyond the existing distribution targets.
- The uvx/pipx transition and duration of Python distribution support.
- Individual intentional differences, including gojq compatibility cases.
- Scheduling and estimates for remaining admin families, excluding repo-intel.
- The specific stable launcher or explicit repair path for each supported
  installation-method transition.

## References

- [Public CLI registry](https://github.com/avrea-com/cli/blob/99e74d1/avrea_cli/main.py)
- [Admin CLI composition](https://github.com/avrea-com/avrea-core/blob/4c74b3309689151f133447fd9a98e4ff0874732b/cli/avrea_admin_cli/main.py)
- [gh dependency injection](https://github.com/cli/cli/blob/trunk/pkg/cmdutil/factory.go)
- [gh output handling](https://github.com/cli/cli/blob/trunk/pkg/iostreams/iostreams.go)
- [Depot progress modes](https://github.com/depot/cli/blob/main/pkg/progresshelper/mode.go)
- [Depot release configuration](https://github.com/depot/cli/blob/main/.goreleaser.yml)
- [Namespace installer source](https://get.namespace.so/devbox/install.sh)
- [gojq behavior differences](https://github.com/itchyny/gojq#difference-to-jq)
- [oapi-codegen OpenAPI support](https://github.com/oapi-codegen/oapi-codegen#does-oapi-codegen-support-openapi-31)
- [testscript](https://github.com/rogpeppe/go-internal/tree/master/testscript)

Namespace's installer was read only, not executed. Its public Devbox repository
did not expose the binary implementation at the time of planning. Recommendations
from that reference concern installer behavior, not unverified CLI internals.
