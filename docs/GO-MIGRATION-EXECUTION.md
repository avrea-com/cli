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

## Pull-request listing batch

Added pr list and the prs alias, shared with avr-admin.
Diagnostics checkpoints: public 86260ef, admin 8053fde819.

Decisions:
- Preserve the organization feature-availability check before repository
  resolution and cursor validation, including the distinct unknown/unsupported
  messages.
- Match case-insensitive scope/state choices, repeated repository filters,
  Git soft detection, limits, Unicode cursor-length bounds and JSON/jq.
- Preserve exact TSV columns/escaping and terminal widths, truncation, cyan
  PR numbers, dim ages, underlined headers and PR/title links. Hide the
  repository column only when exactly one repository was resolved.
- Keep native transport diagnostics sanitized while retaining the PR-specific
  failure context.
- Fix shared repository hints to use Python's dim styling and missing relative
  timestamps to display unknown. Both differences were caught by parity
  before the fixes. Add run/workflow terminal-hint regression cases.
- No new dependencies or remote mutations.

Validation: 61 differential cases per executable, including 12 terminal cases
(two are shared run/workflow hint regressions), plus 104 existing presentation
cases per executable. Go race tests, lint, generated-code checks, paired admin
checks, fixture typing and workflow validation pass. Report hashes match the
tested executables.

## Status dashboard batch

Added status, shared with avr-admin.
Pull-request checkpoints: public c28778e, admin 59ae4cae54.

Decisions:
- Preserve org/repository/Git selection and the four current/previous aggregate
  queries. Keep the recent-run query independent of the stats window.
- Retain success-only medians, minimum sample counts, stable sorting and
  five-item panel limits. JSON retains all current aggregates; human slowest
  panels filter them, matching Python.
- Keep auxiliary aggregate/cache/metadata HTTP and transport failures as
  visible warnings. Run-list failure remains fatal, and cancellation exits 130.
- Match panel widths, alignment, colors, underlines, links, cache sizes,
  repository footer and status legend, including piped human output.
- Validate the current lookback, previous-period length, shared workflow/job
  cutoffs and contiguous boundaries before normalizing clock differences.
- Add explicit dynamic-query fixture endpoints only for moving-window tests.
  Exact routes take precedence and every original request query is retained.
  Existing fixtures retain their prior matching behavior.
- No new dependencies, remote mutations or request-parallelism change.

Validation: 44 Python/Go cases per executable, including nine terminal cases.
Focused tests cover transport warnings, fixed-clock bounds and cancellation;
four new comparator/fixture tests reject wrong windows and dropped periods.
All 42 comparator regression tests, Go race tests, lint, schema generation,
fixture typing, workflow validation and paired admin checks pass.

## Artifact batch

Added artifact list/get/download/delete, shared with avr-admin.
Status checkpoints: public 5b6e159, admin e46f28d6d9.

Decisions:
- Preserve positional repository selection for list; required repository/Git
  context for get/download/delete; ID checks, filters, pagination, JSON/jq,
  field discovery, confirmation and prompt-disable behavior.
- Keep metadata member order and Python-style null/boolean/container strings
  in human output. JSON keeps exact numbers and values.
- Stream downloads with an inactivity timeout, a 10 GiB metadata limit and
  bounded error responses. Require identity encoding and refuse redirects.
- Verify exact size and SHA-256 in a private temporary file, then replace the
  destination. Preserve existing files and remove temporary files on failure
  or cancellation. Do not extract archives.
- Retain terminal-control escaping as an intentional safety improvement for
  untrusted human-readable cells; JSON retains original values.
- Fix bordered-table Unicode display widths after a failing differential case.
  Use MIT-licensed [go-runewidth](https://github.com/mattn/go-runewidth), pinned
  to v0.0.24, with its runtime dependencies included in generated notices.
- Extend local fixtures explicitly for binary bodies, response headers and
  file effects. Compare filenames, size, SHA-256, permissions and temporary
  cleanup against independently declared expected results, so matching bugs
  in both executables do not pass.
- No live deletions, remote mutations, archive extraction or publishing.

Validation: 92 artifact cases per executable, including eight terminal cases.
All 781 public differential cases pass after the shared transport/table changes.
The admin artifact, inspection, shared-auth and workflow-run regressions pass
(342 cases, with the intentional host-resolution exclusion described below). Go race tests, lint,
schema generation, vulnerability scan, Python comparator tests (46), fixture
typing and workflow validation pass. Report hashes match the tested binaries.

The public-auth stored-default-host scenario is intentionally inapplicable to
admin: Python AdminCliConfig and Go adminconfig both derive the host from the
admin environment when AVR_HOST is absent. Its default/override precedence is
covered by the admin configuration tests; 64 other shared-auth cases pass.
Native Windows/Linux execution remains a CI gate, not a locally verified claim.

## Cache batch

Added cache list/usage/delete, shared with avr-admin.
Artifact checkpoints: public f938f16, admin d98d4084c7.

Decisions:
- Match list's exact TSV columns in pipes and bordered human table in terminals,
  including Unicode widths, size/timestamp formatting and terminal-only offset
  pagination hints. JSON returns the schema-selected original fields.
- Preserve lowercasing for list's type filter and the original type value for
  deletion, matching Python's request behavior.
- Match usage summaries, quota percentages and per-type tables; JSON unwraps
  the data envelope before field projection.
- Require exactly one of key or all for deletion, with a type for key deletion.
  Preserve authentication/validation order, default-no confirmation, prompt
  disable behavior and the original human repository label, including Git.
- Match piped browser URLs and terminal Opening output; capture the browser
  invocation through a local stub without starting a login callback.
- Keep unknown terminal controls escaped in human cells; JSON retains raw data.
- No new dependencies and no live cache deletions.

Validation: 72 Python/Go cases per executable, including 11 terminal cases.
Checks include exact request scopes, cancellation without mutation, browser
target/output and file-free prompt refusal. Go race tests, lint, generated-code
checks, paired admin checks, fixture typing, comparator tests and workflow
validation pass. Report hashes match both tested executables.

## Log search batch

Added log search and the logs alias, shared with avr-admin.
Cache checkpoints: public 39910b3, admin bf60242a75.

Decisions:
- Reuse the existing typed log-search transport, with required repository
  context and all Python query/stream/level/VM/limit options.
- Preserve Python's case-insensitive choices and server-side limit validation;
  no new client-side bounds or automatic pagination.
- Match counts, timestamps, level and stream widths, line numbers, raw group
  annotations, multiline indentation and red/yellow/cyan level colors.
- Preserve embedded SGR color on terminals and remove it from pipes/no-color
  output. Escape other terminal control sequences; JSON retains original data.
- Retain diagnostics in search results, matching Python search rather than
  applying the job-log viewer's diagnostic filter.
- No new dependencies, schema operations or remote mutations.

Validation: 38 differential cases per executable, including five terminal cases,
cover exact request payloads, filters, aliases, numeric precision, field discovery,
errors, multiline content, annotations and color/no-color behavior. A focused
test verifies that embedded styling survives while other terminal commands are
escaped. Go race tests, lint, paired admin checks, fixture typing and workflow
validation pass; report hashes match both tested executables.

## SBOM inspection batch

Added sbom list/view, shared with avr-admin.
Log-search checkpoints: public 3a7ec1a, admin 7df4eed7f5.

Decisions:
- Preserve cursor validation, quoted pagination hints, optional commit
  argument/flag, uppercase/whitespace normalization and full SHA-1/SHA-256
  selection. Resolve unique prefixes through paginated history and report
  ambiguity; stop a repeated cursor rather than loop indefinitely.
- Match human metadata, unknown counts, license/ecosystem ordering, negative
  count handling, producer omissions and the ten-item display rule, including
  the eleven-item exception and --full.
- Keep hints pinned to the displayed commit and safely quoted for the shell.
  Escape controls in human-readable names and clip license names after escaping.
- Tolerate non-object opaque summaries as Python does, after a differential
  failure demonstrated the mismatch. JSON preserves the original summary.
- Preserve no-SBOM hints on 404/422 and avoid falsely treating missing counts
  as zero. No new dependencies or live writes.

Validation: 76 Python/Go cases per executable, including seven terminal cases,
cover history/cursor output, prefix resolution, field validation order, unknown
counts, opaque summaries, omission/truncation boundaries, escaping and JSON
preservation. Focused Go tests cover repeated history cursors and cancellation.
Go race tests, lint, schema generation, paired admin checks, fixture typing and
workflow validation pass; report hashes match the tested executables.

## SBOM download batch

Added sbom download, shared with avr-admin.
SBOM inspection checkpoints: public 9226f0d, admin b7dbe36f63.

Decisions:
- Preserve latest/full-SHA/prefix resolution, all three artifact formats,
  default/custom filenames, raw stdout output and missing-result hints.
- Receive the complete response before opening the destination or emitting
  stdout, as Python does. Use a private temporary spool rather than buffering
  the complete inventory in memory; remove it on success, error or cancellation.
- Keep normal file-open permissions, overwrite and symlink behavior. Do not
  create missing destination parents or expand a literal tilde.
- Add decoded streaming for gzip/zstd analysis artifacts without the JSON size
  limit. Workflow artifact downloads continue requiring identity encoding.
  Redirects remain refused and error bodies remain bounded.
- Preserve raw bytes; SBOM artifacts do not expose the workflow-artifact
  integrity contract, so no checksum claim is made for this command.
- No new dependencies or live remote mutations.

Validation: 34 download cases per executable, including three terminal cases,
plus 92 workflow-artifact and 76 SBOM-inspection regression cases per executable.
Focused Go tests cover an 18 MiB compressed inventory, opaque stdout bytes,
cancellation, existing-file preservation and spool cleanup. Go race tests, lint,
schema generation, paired admin checks, fixture typing and workflow validation
pass; report hashes match both binaries.

A regression capture exposed repeated CR-before-LF being mistaken for a blank
line by the presentation comparator. A failing test reproduced it; normalization
now accepts equivalent carriage returns while retaining real extra blank lines.
The original raw capture was re-evaluated with that correction and retained with
its prior difference/provenance. All 48 comparator regression tests pass.

## SBOM generation batch

Added sbom generate with optional REF/--ref, --wait, --wait-timeout and JSON/jq,
shared with avr-admin. Download checkpoints: public 0ec10c7, admin 33bfa10ae0.

Decisions:
- Preserve a single generation POST, join-existing behavior, ref normalization
  and JSON field discovery before authentication or mutation.
- Without a pinned commit, report the task outcome. With one, keep checking the
  commit snapshot until a new recorded_at appears, even after the task fails,
  completes or is superseded. Read a full-SHA baseline before the POST; read an
  API-expanded SHA baseline afterwards, matching Python.
- Retry only reads on network failures, 408, 429 and 5xx. Honor Retry-After up to
  30 seconds and bound reads/sleeps by the wait budget, including Python's final
  deadline poll. Cancellation stops the wait without cancelling the server task.
- Keep human progress, success/failure hints, nullable JSON fields and exit
  statuses. A completed wait followed by a jq error does not announce an
  interrupted wait. Escape external task IDs and error codes in terminal text.
- Preserve Python's tolerance of malformed optional commit expansion; a paired
  fixture exposed the initial typed-decoding mismatch.
- Preserve sanitized transport errors while classifying interrupted response
  bodies for retry. A failing test demonstrated a truncated poll incorrectly
  stopping the wait before this correction.
- No new dependencies or live remote mutations.

Validation: 71 paired cases per binary, including six terminal cases, cover
generation requests, refs, scopes, JSON/jq, validation ordering, joins, baseline
ordering, older/superseding tasks, delayed delivery, transient/permanent failures
and timeouts. Eight focused Go tests cover single POSTs, cancellation, bounded
backoff/read budgets, delivery after failure, jq failures and interrupted reads.
Failure JSON is compared structurally even on nonzero exits; five regression
tests ensure null/type changes and wrong exit codes are detected.
Go race tests, lint, schema generation, vulnerability scan, admin composition
checks, fixture typing and workflow validation pass. Both report hashes match
the final tested binaries.

## CLI configuration batch

Added config (status), config list, config get org, config set org VALUE and
config unset org, shared with avr-admin. SBOM generation checkpoints: public
0ca2031, admin 6f381c7c62.

Decisions:
- Match host/auth/org/repository status, best-effort profile lookup, environment
  and stored-value source annotations, Git detection, colors and bold/dim text.
- Resolve set values as exact IDs, exact slugs, then case-insensitive slugs;
  persist the resolved ID. Keep authentication and scoped-token restrictions.
- Set/unset only the current host's organization field. Preserve tokens,
  unrelated future fields, other hosts, insertion order and default_host.
  Unset does not require a login and does not create an absent hosts file.
- Reuse private atomic credential storage. Human output escapes external
  control characters; config never prints the credential.
- Admin retains its documented environment-derived host default. Tests align
  the wrapper environments for shared default/source presentation cases.
- No API schema changes, dependencies or live configuration mutations.

Validation: 55 Python/Go cases per binary, including eight terminal cases,
compare visible output/styles, requests, exit codes and resulting hosts.json.
Expected file content is independently specified for mutations and refusals.
Four Go tests cover exact-ID precedence, unknown-field preservation,
cancellation, scoped identity avoidance and absent/empty host behavior.
The existing auth regression suites pass: 65 public cases and 64 shared admin
cases, excluding the previously documented stored-host default difference.
Go race tests, lint, admin composition checks, fixture typing and both workflow
validators pass. Reports match the final binary hashes.

## Token inspection and revocation batch

Added token list/view/revoke and the tokens alias, shared with avr-admin.
Configuration checkpoints: public d6fdf0a, admin 06fde33025.

Decisions:
- Preserve 200-record page bounds, the requested total limit, empty-page
  termination, overfetch truncation and stderr hints when more records remain.
- Keep stable TSV columns and human tables, conditional owner columns,
  compact repository/VM grant summaries, creation quotas and revoked/expired
  status. Inspection never includes the one-time credential, even if an API
  response unexpectedly supplies it.
- Validate canonical token IDs before building paths. Preserve organization
  ID/slug selection, scoped-token error hints, JSON projection and jq.
- Revoke only after the default-no confirmation or --yes/-y. Preserve prompt
  disabling and idempotent success. A cancellation guard prevents starting a
  revoke request after cancellation; a failing test demonstrated the gap.
- Add the plural alias after an executable parity case exposed its omission.
  No feature-gate preflight is added: existing tokens remain inspectable and
  revocable even when token creation is unavailable.
- Server error text now uses the same terminal-control escaping as Python.
  Three failing token error cases demonstrated that the previous shared helper
  left Unicode line/direction controls unescaped and formatted C1 controls
  differently. Shared authentication regression suites cover the correction.
- No new dependencies or live token mutations.

Validation: 87 paired cases per executable, including nine terminal cases,
cover pagination, owner/grant summaries, statuses, field validation, JSON/jq,
scope selection, confirmation, errors and the plural alias. A synthetic
credential injected into inspection responses is checked for non-disclosure.
Three Go tests verify page/total bounds and cancellation before polling or
revocation. Public auth 65 cases and shared admin auth 64 cases pass.
Go race tests, lint, schema reproducibility, admin composition checks, fixture
typing and both workflow validators pass. Final report hashes match both
executables. Login parity suites must run serially because their callback URL
uses port 8765; a parallel invocation collided and the isolated rerun passed.

## Token creation batch

Added token create (also tokens create), shared with avr-admin.
Inspection checkpoints: public 4a7a6a3, admin 296cd12c69.

Decisions:
- Preserve repeatable repository/VM grants, repository name resolution,
  deduplication and repository-before-VM request ordering. Leave optional TTL
  and VM limits out when absent so server defaults apply.
- Validate grant levels, grant count, TTL bounds and credential field selection
  before creating anything. Send exactly one creation request, with no retry
  when the outcome is uncertain.
- Keep the one-time credential available despite malformed summary metadata.
  Buffer jq results and require the credential to survive the filter. On a
  failed filter, recover with unfiltered selected JSON and return failure.
- Do not put credentials or underlying transport/output errors on stderr.
  Preserve Python's recovery and revoke hints, feature/capability guidance,
  throttling details and grant-index explanations.
- Cancellation returns 130 even after local credential recovery. Broken pipes
  and buffered-output failures return nonzero and do not claim successful
  delivery. Local recovery never sends another API request.
- No new dependencies or live token creation.

Validation: 76 paired cases per binary, including two terminal cases, cover
grant combinations, optional defaults, TTL boundaries, JSON/jq recovery,
malformed success responses and HTTP errors. Three focused Go tests cover
cancellation, write/flush failures, uncertain outcomes and no duplicate POSTs.
The 87 token inspection/revocation regression cases pass in each binary.
Go race tests, lint, schema generation, admin composition checks, fixture
typing and workflow validation pass. All four reports match the tested
binary hashes. Native OS CI and real credential creation were not exercised.

## Settings batch

Added settings list/schema/set/reset, shared with the admin Go composition.
Token creation checkpoints: public 85a32a8, admin 85a79273eb.

Decisions:
- Preserve scope precedence: --repo, explicit --org, AVR_REPO, then Git.
  Explicit --org ignores ambient repo context. Disconnected detected reads
  may fall back to org results; writes stop instead of changing scope.
- Keep schema/list field discovery, projection, jq, prefix/scope filters,
  bordered tables, inherited-source labels and browser targets.
- Parse booleans and decimal integers before sending writes, retaining large
  integer precision and leaving other strings unchanged. Reset escapes the
  key as one URL path segment. Do not retry writes.
- Preserve org-only-setting guidance on repository-scoped 422 responses and
  distinguish an explicit --repo from ambient selection in that guidance.
- Correct shared missing-repository punctuation, blank lines and abort
  messages exposed by paired fixtures. Resolution and soft fallback retain
  their existing behavior.
- Extend the local parity server to handle PUT. Earlier 501s were fixture
  limitations, not application behavior; successful writes now exercise and
  compare the actual request bodies.
- No dependencies or live configuration mutations.

Validation: 114 paired cases per binary, including nine terminal/browser
cases. They cover all four commands, scope precedence, disconnected checkout
refusal, value parsing, escaped keys, JSON/jq, errors and argument diagnostics.
Three Go tests cover cancellation between resolution and mutation, explicit
org isolation with precise integers, and schema access without org/Git lookup.
All 143 inspection regression cases pass in each binary. Go race tests, lint,
schema reproducibility, admin checks, fixture typing, inventory verification
and workflow validation pass. The four reports match final binary hashes.
The admin checks validate inherited public semantics; admin-only settings
overrides remain outside this public-command migration.

## Organization and email-domain batch

Added org create/members and org email-domain list/claim/verify/set, shared
with avr-admin. Settings checkpoints: public 089a84c, admin 0cea779e05.

Decisions:
- Keep organization creation independent of current org selection. Member and
  domain commands retain explicit IDs, slug resolution and the configured org.
- Preserve table and aligned-record output, nullable member names, UTC
  timestamps, pending/verified domain status, DNS challenges and verification
  instructions. Pending verification remains a successful status inspection.
- Domain replacement keeps the default-no confirmation, --yes/-y bypass,
  disabled-prompt refusal and server-returned domain order/count.
- Send one request per mutation, guard cancellation before sending, and escape
  verification domains as a single path segment. No new feature preflights.
- Add a shared named-positional validator after five differential cases
  exposed generic Cobra diagnostics. Settings reuses it; JSON single-record
  output and aligned labels also have shared helpers.
- No new dependencies, live organization creation or DNS/domain changes.

Validation: 127 paired cases per binary, including eight terminal cases,
cover all six commands, request bodies, organization selection, pending and
verified records, empty data, JSON/jq, prompts, errors and argument diagnostics.
A Go test checks cancellation before all four new mutation paths. Twenty
settings argument/value regression cases pass in each binary. Go race tests,
lint, schema generation, admin composition checks, fixture typing and workflow
validation pass. The four reports match the final tested binary hashes.

## SAML batch

Added org saml show/sp-metadata/test/configure/enforcement/remove, shared
with avr-admin. Organization checkpoints: public 3c5b67d, admin e94c390e22.

Decisions:
- Keep metadata as opaque XML bytes. Upload file or stdin without parsing,
  re-encoding or logging it; use the authenticated, timeout-bounded transport
  with application/xml and the same redirect refusal as JSON requests.
- Preserve all attribute aliases, case-insensitive role/state choices,
  optional attribute omission and last-option-wins positive/negative toggles.
- Keep aligned configuration output, nullable fields, JSON/jq, XML newline
  handling, organization-slug resolution and browser/manual-open messages.
- Removal retains default-no confirmation, --yes/-y and prompt-disabled
  refusal. Mutation paths check cancellation and do not add retries.
- Bound metadata reads at 16 MiB and allow cancellation while stdin blocks.
  This local memory guard is above the server's 1 MiB accepted-body limit;
  ordinary validation stays server-side. Oversized local input fails before
  upload rather than allocating without a bound.
- Extend the local harness to compare XML body bytes and content type.
  Existing JSON request capture is unchanged.
- No new dependencies or live SSO changes/sign-ins.

Validation: 106 paired cases per binary, including eight terminal cases,
cover all six commands, byte-exact file/stdin uploads, options and aliases,
pending errors, field selection, browser capture, prompts and diagnostics.
Six focused Go tests cover blocked-input cancellation, the input bound,
authenticated opaque XML without diagnostic disclosure, browser failure,
subsequent JSON requests and redirect refusal. Go race tests, lint, schema
generation, admin composition checks, fixture typing and workflow validation
pass. Both parity report hashes match the final tested executables.

## GitHub installation batch

Added org install list/add/remove and the orgs alias, shared with avr-admin.
SAML checkpoints: public 9054897, admin 8114860557.

Decisions:
- Keep cross-organization installation listing, fallback organization labels,
  UTC timestamps, JSON projection and jq.
- Read the installation baseline before launching the browser. Preserve the
  no-browser option, printed URL, nonpositive wait skip and default 120 seconds.
- Preserve Python's five-second polling cadence, including its final poll
  after a shorter wait budget. Detect distinct new platform installation IDs
  against the original accessible-installations snapshot and sort them.
  Timeout remains informational with exit 0; poll errors remain failures.
- Cancellation stops polling with exit 130 and Python's abort diagnostic.
  Fake-clock tests verify the final-poll boundary without timing thresholds.
- Suspension requires confirmation or --yes/-y, honors disabled prompts and
  checks cancellation before DELETE. No write retries are added.
- Add the existing orgs alias to the entire organization tree.
- No dependencies or live installation changes; browser launches are captured
  by local fixtures.

Validation: 59 paired cases per binary, including five terminal cases and a
real process interruption, cover all three commands, polling outcomes,
browser capture, fields, errors, confirmations and the alias. Two Go tests
cover wait-budget/cancellation behavior and cancellation before suspension.
Go race tests, lint, schema generation, admin composition checks, fixture
typing, inventory verification and workflow validation pass. Both report
hashes match the tested binaries.

## Remaining work

Repository management, billing/audit/firewall, VM commands, hidden docs, root/help/completion,
remaining aliases, and a final per-command flag/argument/behavior coverage audit.
Existing implemented commands also remain subject to that exhaustive audit;
the prior suites are substantial evidence, not a claim that every flag combination
has already been checked. Packaging and native OS CI remain separate release gates.
