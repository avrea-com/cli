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

## Public-mirror batch

Added repo public-mirror check/request/requests/view/cancel and the repos
alias, shared with avr-admin. Installation checkpoints: public 8c824c4,
admin 1b87dc84e2.

Decisions:
- Preserve Python's GitHub catalog lookup route. A catalog 404 is an
  availability answer with exit 0, including JSON nulls for absent fields.
- Keep aligned records, bordered request tables, nullable review fields and
  UTC timestamps. JSON preserves large integers and nested snapshots.
- Cancellation keeps default-no confirmation, --yes/-y and disabled-prompt
  refusal. Empty successful responses use the supplied request ID.
- Escape request IDs and repository name segments; reject dot segments for
  catalog lookups. Mutations check cancellation and are not retried.
- No new dependencies or live mirror requests/cancellations.

Validation: 132 paired cases per binary, including seven terminal cases,
cover all five commands, selection and jq, request bodies, organization
resolution, HTTP errors, confirmations and empty cancellation responses.
A focused Go test prevents both mutation paths after cancellation. Go race
tests, lint, generation checks, admin composition checks, fixture typing and
workflow validation pass. Both reports match the tested binary hashes.

## Git-mirror management batch

Added repo mirror create/status/enable/disable/place/unplace/locations, shared
with avr-admin. Public-mirror checkpoints: public 463d94d, admin 038bc118f4.

Decisions:
- Keep source-aware repository resolution and server-owned feature gating.
  GitHub creation accepts owner/repo; GitLab creation accepts nested namespaces.
- Fetch every location page, reject malformed/repeated cursors, prefer exact
  cluster IDs, and reject ambiguous display-name/datacenter matches.
- Creation resolves the location before writing, skips enabling an already
  enabled mirror, and preserves partial-success diagnostics when placement
  fails. No automatic mutation retries are introduced.
- Disable and unplace retain default-no confirmations, --yes/-y and disabled
  prompt handling. Empty successful removal responses retain Python output.
- Preserve aligned records, bordered placement/location tables, nullable
  fields, UTC timestamps, JSON/jq and terminal behavior.
- Two failing paired cases exposed shared resolution-message drift: GitLab
  connection guidance and alternate-organization suggestions. Both are fixed
  with broad inspection regressions in each binary.
- No new dependencies or live mirror changes.

Validation: 191 paired cases per binary, including eight terminal cases, cover
all seven commands and their options, pagination, resolution, partial failures,
prompts and exit codes. Another 143 inspection cases per binary pass. Two Go
tests check uncertain mutation responses without retries or private-detail
leaks, and cancellation between enable and placement. Race tests, lint, schema
generation, admin composition checks, fixture typing, command inventory and
workflow validation pass. All four report hashes match the tested binaries.

## Repository clone batch

Added repo clone with explicit/nearest mirror selection, native Git execution
and a persistent Go credential helper, shared with avr-admin. Mirror-management
checkpoints: public 7771b8d, admin b435f235ae.

Decisions:
- Keep name/ID discovery independent of organization selection. Preserve
  .git suffix handling, --placement, --branch/-b, --depth, optional destination,
  native Git exit codes, human diagnostics and terminal output.
- Race authenticated Git advertisement headers with a five-second total
  bound. Reject redirects and non-Git responses; cancel losing probes.
  Validate server-advertised origins and repository routes before minting.
- Persist the current native executable's absolute path as the repository's
  credential helper, rather than selecting a possibly Python avr on PATH.
  This intentional difference keeps subsequent fetch/pull native and also
  works when cloned through avr-admin. The helper pins the original API host.
- Preserve reset of inherited credential helpers for the exact mirror URL,
  useHttpPath and redirect refusal. JWTs stay out of argv and Git config;
  tracing and askpass are disabled for the clone child.
- Reuse shared token issuance/validation for probes and the Git credential
  adapter. A failing Go regression demonstrated that a malformed IDNA hostname
  could mint a token through the adapter; shared hostname validation now rejects
  it first, with malformed/valid encoded-host comparisons against Python.
- Add golang.org/x/net/idna v0.61.0 and indirect x/text v0.43.0 (BSD-3-Clause).
  Both releases date to 2026-10-09, satisfying the age gate. Build notices
  include their licenses. Govulncheck reports no affected symbols or imported
  packages; module-only HTML notices GO-2026-6866 and GO-2026-6860 are outside
  the imported IDNA package. Their v0.62.0 fix was released 2026-10-27 19:00 UTC
  and had not reached the three-day age gate at this validation.
- Extend the fixture server with opt-in loopback endpoint substitution and
  explicit content types. Capture native Git invocation without remote clones.
  The comparator permits the executable-path and shell-quoting differences,
  while checking actual helper executable, repository scope and environment.

Validation: 94 paired clone/credential cases per binary, including three
terminal cases. Four Go tests cover losing-probe cancellation, redirect refusal,
parent cancellation and malformed IDNA rejection before token issuance. Five
Python integration/comparator tests per binary include a real local smart-HTTP
clone, subsequent fetches with fresh credentials, pinned API origin, refusal
after token issuance fails, unchanged credential storage, and comparator guards.
The full auth regression suites also passed (65 public, 64 admin with the
documented host-selection difference); all Git protocol cases are included in
the final clone suite. Go race tests, lint, generation and dependency scanning,
admin checks, fixture typing and workflow validation pass. Both final clone
report hashes match the tested binaries. No live mirror cloning or API changes.

## Audit events and health batch

Added audit-events list and health, shared with avr-admin. Clone checkpoints:
public dca7046, admin 8a00d27359.

Decisions:
- Preserve audit filters, date aliases and last-option-wins behavior, bounded
  limits, cursor validation and organization selection. Human output retains
  the next-page hint; JSON emits only the selected event records.
- Retain API member order and compact ASCII JSON in the audit Data column,
  including nested values and large integers. JSON projection uses the raw
  typed values independently of human rendering.
- Health requires no login. Preserve human progress/status lines, host-based
  HTTP/network/timeout diagnostics, and exit 1 after an unreachable response.
  JSON failures still emit the stable selected status record, including jq.
- Keep error text and status icons consistent with Python in terminals and
  pipes. No dependencies or live writes are added.

Validation: 73 paired cases per binary, including five terminal cases, cover
filters and aliases, limits/cursors, empty and nullable data, ordered event
payloads, JSON/jq and HTTP errors. Failure JSON is structurally compared even
on nonzero exits. A focused Go test covers unauthenticated connection/timeout
failures, retained JSON, and sanitized network diagnostics. Go race tests,
lint, schema generation, admin composition, fixture typing and workflow
validation pass. Both parity report hashes match the tested executables.

## Billing inspection batch

Added billing summary/settings, invoices list/show and payment-methods list.
Audit/health checkpoints: public 62fd22c, admin 85b6b39804.

Preserve Python's aligned records, bordered tables, nullable card fields,
currency symbols and zero-decimal currencies, raw invoice dates and timestamps,
line items, pagination hints and JSON/jq projections. Invoice JSON retains large
integers and unknown nested fields. Organization resolution and error semantics
are shared with the existing commands. No new dependencies or live writes.

Validation: 131 paired cases per binary, including six terminal cases, passed
against Python. Coverage includes all five commands, organization precedence,
authentication and HTTP errors, field discovery, limits/cursors, empty data,
currencies and invoice detail variants. Public Go race tests/lint/build,
generation checks, admin composition checks, fixture typing and workflow
validation passed. Both parity report hashes match the tested binaries.

## Billing settings and payment-method changes

Added update-settings, payment-methods set-default and payment-methods remove.
Inspection checkpoints: public 4006691, admin ed5cd864c9.

Settings writes preserve omitted versus explicitly empty fields, comma-separated
email trimming, repeated-option precedence, and JSON discovery before auth.
Payment-method removal retains the default-no confirmation, prompt-disable
behavior and long --yes option (Python has no -y alias here). Default selection
needs no confirmation. Requests use the pinned generated billing request model;
writes are not retried, and no live billing records were changed.

Validation: 71 paired scenarios per binary, including four terminal cases, pass.
The fixtures compare exact request bodies/counts, selected organization, output,
JSON/jq, empty responses, refusal/EOF and HTTP failures. Public Go race tests,
lint/build, schema generation, admin composition, fixture typing and workflow
checks pass. Both report hashes match the tested executables.

## Invoice PDF downloads

Added billing invoices download, shared with avr-admin. Billing write
checkpoints: public ff11a00, admin 3e95ddaa72.

Downloads finish into a private spool before opening the destination, preserving
existing files on network failure without buffering entire PDFs in memory.
SBOM downloads now use that same spool helper. Final writes preserve Python's
ordinary file permissions, replacement and symlink semantics; --out - is a
literal filename for this command, not stdout.

Invoice downloads alone follow bounded HTTP redirects. Same-origin redirects
retain auth; leaving the origin permanently drops credentials for that chain.
HTTPS downgrades and userinfo destinations are refused. Other API/artifact
redirect policies remain unchanged. Deliberate differences: slash-containing
invoice identifiers stay one escaped URL segment; Go decodes zstd PDFs while
Python's standalone httpx path saves unsupported compressed bytes. Both zstd
results are explicitly asserted, not normalized away.

Validation: 29 paired PDF cases (two terminal cases) and 34 SBOM regression
cases per binary pass. Four focused Go tests cover credential stripping across
origins and back, downgrade/loop refusal, preserving existing files on broken
streams, and escaped-ID/basename handling. Go race tests, lint/build, generation,
admin composition, fixture typing and workflow checks pass. All four report
hashes match the tested executables. No live invoice downloads or billing writes.

## Card addition

Added billing payment-methods add. PDF checkpoints: public 0aaa251,
admin 86bf10adeb. Billing now has all ten Python leaf commands.

Preserve required card flags and prompts, month validation, organization
selection, setup-intent creation, Stripe confirmation, attachment and human
output. Hidden terminal input suppresses echo and restores it on interruption.
Numeric prompts retry with Python's diagnostics. As in Python, these Click-style
required prompts are not bypassed by AVR_PROMPT_DISABLED.

Card details go only to the fixed Stripe HTTPS origin with the publishable key.
Avrea receives only the resulting Stripe payment-method ID. Stripe requests
have a deadline and response bound, never inherit API credentials/cookies,
never follow redirects, never enter API trace/timing output and are not retried.
Transport errors are sanitized; echoed input secrets are redacted from Stripe
error messages. Malformed setup-intent IDs cannot change the request path.
These are deliberate improvements over exposing raw network errors or accepting
path-shaped IDs. No new runtime dependencies.

Validation per binary: 26 paired native pre-Stripe cases, plus 55 paired cases
(four terminal cases) against the public/admin command roots with an injected
loopback HTTP transport. The latter builds test-only entry points and patches
only the archived Python oracle's Stripe transport; production binaries gain no
endpoint override. Reports distinguish native and injected validation and record
the native binary hash. Request bodies/order, Basic auth, prompt/error output
and absence of secret output are checked. No live Stripe or billing writes.
Two real-PTY tests per binary compare hidden input against Python and verify
echo restoration on Ctrl-C. Two Go tests cover four Stripe failure variants,
secret redaction, redirect refusal, response bounds and malformed intent IDs.
The 131 billing inspection and 71 billing write cases per binary still pass.
Go race tests, lint/build, schema generation, admin composition, fixture typing
and workflow checks pass; all native report hashes match tested executables.

## Firewall batch

Added all seven firewall commands: list, show, add, delete, move, set-default
and flow-summaries. Card checkpoints: public d108b47, admin 5c8d23b469.

Preserve explicit --repo scope (ambient AVR_REPO does not apply here), boolean
--json without projection/jq, raw rule/flow JSON, bordered tables, flow filters
and aliases, pagination hints, and required option/argument behavior. Add keeps
the original CIDR text after validation, supports IPv4/IPv6 addresses, netmasks
and hostmasks, and retains explicit null matcher/port fields. Ports are validated
before auth; non-TCP/UDP protocols cannot have ports.

Move sends one full-order request after reading the scope. Set-default patches
an existing default in place; it never deletes first or creates after updating.
Delete has no prompt, matching Python. Writes are not retried. All validation
uses local fixtures; no live firewall policies were changed.

Validation: 175 paired cases per binary, including seven terminal cases, pass.
Coverage includes org/repo selection, all commands, request order/bodies, raw
JSON, null/empty fields, CIDR/port variants, move boundaries, default creation
versus update, aliases, HTTP failures and flow summaries. A focused Go test
first reproduced GET/PATCH/POST after a malformed default-update response; the
fix now stops after GET/PATCH with an error, including HTTP failure coverage.
The reorder fixture was corrected to the pinned API's 200 JSON-array response.
Go race tests, lint/build, generation, admin composition, fixture typing and
workflow checks pass. Both report hashes match the tested executables.

## VM inspection batch

Added vm list, show and usage, shared with avr-admin. Firewall checkpoints:
public 4555d14, admin ff09039024.

Preserve VM's boolean --json interface, pagination/cursor stderr hints,
organization selection, raw JSON fields/integers and API error guidance. Human
output retains table borders, ordered detail labels, SSH key counts, preload
and fetch-depth details, snapshots, egress rules and platform-specific RDP
connection guidance. Multi-line connection hints align with the value column;
external control characters remain escaped. Usage accepts Python's date/date-time
forms and sends no implicit period when the flags are omitted.

Validation: 98 paired scenarios per binary, including seven terminal cases,
pass. They cover list filters/limits/cursors, empty/nullable data, JSON/error
paths, SSH/RDP/VNC endpoint variants, Windows/Linux/macOS guest presentation,
snapshots, preload state, egress tables, large integers and usage date validation.
Host-platform-specific client hints were exercised on macOS; native Linux and
Windows runs remain CI gates. Go race tests, lint/build, generation, admin
composition, fixture typing and workflow checks pass. Both report hashes match
the tested executables. No VM mutations or connections were made.

## VM lifecycle and wait batch

Added start, stop, pause, resume and delete, including --wait and timeout flags.
Inspection checkpoints: public 9578e79, admin 6aeb1d5433.

Preserve mutation requests, full-envelope JSON before waiting, final-record JSON
after waiting, one-time password output/colors, default-no delete confirmation,
memory/discard-memory flags and human progress. Polling retries transport
failures, 408/429 and 5xx, caps Retry-After at 30 seconds and surfaces permanent
4xx immediately. Delete treats 404 as complete. Pause/resume detect operation-
specific failed transitions; start waits for required connection endpoints.
Poll sleeps are bounded by the remaining deadline. Mutations are never retried.
Human start output prints a fresh password before waiting so interruption does
not lose it; JSON waits merge it into the final record.

Contract gap: the Python oracle's start/stop PATCH desired_state field is absent
from the pinned current API schema. A small explicit legacy request type retains
that wire contract. No automatic substitution with pause/resume/delete is made,
because those operations have different disk/password behavior. These two
commands need live integration verification; fixture parity does not establish
current-server support. No live VM operations were used in validation.

Validation: 172 paired cases per binary, including eight terminal cases, pass.
Coverage includes normal and empty responses, all mutation/error paths, ready/
failed/timeout waits, endpoint readiness, permanent poll errors, 408/429/503
retries, deletion by 404, confirmations, password quoting and styled output.
Nonzero-exit JSON is structurally compared. Three Go tests cover bounded retry
delays/permanent errors, no mutation retries across all five operations, and
password retention on Ctrl-C. Go race tests, lint/build, schema generation,
admin composition, fixture typing and workflow checks pass. Both report hashes
match the tested executables.

## VM update batch

Added name/TTL changes, repeated literal or file-based SSH keys, password
rotation and inline/file egress rules. Lifecycle checkpoints: public 137307b,
admin ee16cd568f.

Preserve omitted versus explicitly empty values, key order and duplicates,
raw egress rule properties and large integers, full-envelope JSON, human
VM details and one-time password colors. Private keys are rejected before
authentication or requests. File reads are bounded to 16 MiB and invalid UTF-8
gets a concise error instead of Python's traceback. Rule shape remains server
validated. The comparison caught and fixed a successful null-response mismatch.

Validation: 76 paired cases per binary, including three terminal cases, pass.
These cover input files, private-key rejection, TTL bounds, API errors,
organization selection, empty responses and all update flags. Race tests,
Go lint/build, admin composition, fixture typing and workflow validation pass;
both report hashes match the tested binaries. No live VM updates were made.

## VM creation and spawn-script batch

Added VM create with every Python option: OS/version/size choices, ephemeral
acknowledgement, SSH keys, desktop toggles, TTL, egress rules, repository/ref/
fetch depth, cache overrides, waiting and Claude Code spawn-script mode.
Update checkpoints: public a29c322, admin 22f6c169e4.

Preserve omitted versus empty values, server validation of OS/size pairings
and cache membership, sorted cache notices, one-time password formatting,
precheckout notes, final wait JSON and progress. Create is never retried after
an uncertain transport outcome. Spawn mode uses the order ID as the idempotency
key, forwards only the per-session credential and maps HTTP failures to the
vendor's retry/refusal exit codes. It never reads the environment-wide key.
Errors redact echoed credentials; work-order reads are bounded to 16 MiB and
reject invalid UTF-8 cleanly. These are intentional safeguards beyond Python.

The pinned schema deprecates ephemeral as an ignored request field; the Python
command's required acknowledgement remains for compatibility. The generated
optional bootstrap union has no exported constructor, so the request uses the
generated concrete ClaudeCodeBootstrap model through a narrow envelope override.
No handwritten schema model or new dependency was added.

Validation: 140 paired cases per binary, including six terminal cases, pass.
Coverage includes every create flag, file inputs, OS/size/version choices,
cache and branch validation, password quoting, ready/failed/timeout/retry waits,
spawn credentials and status mapping. Extra-argument diagnostics retain the
same substantive message; Click/Cobra usage banners follow the existing
accepted difference. Three Go tests cover credential boundaries/redaction,
no create retries and invalid/oversized credential files. Go race tests,
lint/build, schema generation, admin composition, fixture typing and workflow
validation pass; both report hashes match the tested executables. Validation
used local fixtures only, with no live provisioning or vendor calls.

## VM SSH configuration batch

Added ssh-config with identity, alias, known-hosts path, append and config-file
options. Creation checkpoints: public ddf48d7, admin 970597bace.

Preserve known-host entries for other endpoints, replace the selected pin,
update managed alias blocks in place, preserve unmanaged config and reject
unterminated managed blocks without modifying the config. Files are atomically
replaced at 0600; new parent directories use 0700. Warn about earlier Host *
settings that can override an appended pin, and about trust-on-first-use when
the endpoint has no host key. Paths with whitespace are quoted.

Malformed endpoint addresses/usernames are rejected before any pin write,
where Python can write a pin before rejecting the config. This intentional
difference has explicit no-file-write assertions. Host/user/port validation
also rejects option-like or out-of-range values. SSH file reads are bounded
to 16 MiB and invalid UTF-8 produces a concise error. File comparisons normalize
only each isolated fixture's home-directory prefix; content and modes otherwise
remain compared.

Validation: 59 paired cases per binary, including five terminal cases, pass.
Four Go tests cover private permissions, idempotent updates, preservation of
unterminated configs and unrelated pins, new-directory modes and malformed
endpoints. The local OpenSSH parser accepts the generated block and resolves
the expected host/user/port/identity/pinning settings. Go race tests, lint/build,
admin composition, fixture typing and workflow validation pass. Both report
hashes match the binaries. No remote SSH connections or user SSH files were
used by validation.

## VM SSH session batch

Added ssh with identity, print, login-shell and tmux-session options and
unprocessed remote arguments. SSH config checkpoints: public a5ead4d,
admin ebe039ea04.

Preserve Python shell quoting, unknown-option passthrough, known options before
the -- separator, session validation, child exit status and temporary host-key
pinning at 0600. Printed commands use accept-new because they cannot reference
an ephemeral pin; live connections warn when no key is available. Temporary
pins are removed on child success, failure, launch errors and cancellation.
Endpoint validation reuses the SSH config safeguards.

Both executable entrypoints now use InterruptContext. Ordinary commands still
cancel on Ctrl-C; an attached VM SSH child owns terminal interrupts until it
exits, while external context cancellation remains effective. No signal policy
is installed merely by composing the command library. The code preserves
Python's signal-derived subprocess exit values on Unix.

Validation: 70 paired cases per binary, including five terminal cases, pass.
The stub inspects the real pin's contents/mode and verifies removal after the
parent exits. A paired PTY test sends SIGINT to the parent/child process group,
proves the child receives it while the parent stays alive, then checks the
child's exit code and pin cleanup. Existing card-prompt PTY tests confirm normal
cancellation and echo restoration still work, three terminal tests per binary.
Go tests also cover pin cleanup on launch failure/external cancellation and a
reproduced, fixed usage-error exit regression. Go race tests, lint/build, admin
composition, fixture typing and workflow validation pass; both parity report
hashes match the tested executables. No remote SSH connections were made.

## VM TCP and desktop tunnel batch

Added port-forward, rdp and vnc with every Python flag, including repeated
[LOCAL:]GUEST ports, local-port overrides, identity, print and launch toggles.
SSH checkpoints: public e775689, admin 23a5699613.

Preserve loopback-only bindings, port range/duplicate validation, reservation
of explicit ports before auto-selection, SSH keepalive/forward-failure options,
temporary host-key pinning, platform-specific desktop commands, ready messages
and trust-on-first-use warnings. Printed commands never open SSH or a desktop.
Live tunnels check every forward for up to 60 seconds before reporting ready,
surface unexpected SSH exits, and close on Ctrl-C with exit 130. Processes are
reaped, with bounded graceful termination on Unix before forced cleanup.

A missing desktop client leaves the tunnel available for a manual connection.
A waitable desktop client no longer keeps the command hanging after SSH has
already failed; the child is cleaned up when the tunnel ends. Auto-selection
also bounds repeated local-port collisions instead of looping indefinitely.
These are intentional lifecycle safeguards beyond the Python implementation.

Validation: 107 paired scenarios per binary, including five terminal cases,
pass. Auto-selected local ports are normalized by role; explicit local ports,
guest ports, order, arguments and requests remain compared. Twelve paired
local lifecycle tests per binary exercise all three commands through real
loopback listeners and SSH/client stubs: ready, clean exit, dropped connection,
Ctrl-C, desktop launch and missing-client fallback. They verify freed ports,
child reaping, pin content/mode and removal. Three Go tests cover explicit-port
reservation, all-port readiness/deadline handling and desktop command selection
for Windows/macOS/Linux hosts. Native lifecycle tests ran on macOS; other native
OS runs remain CI gates. Go race tests, lint/build, admin composition, fixture
typing and workflow validation pass; report hashes match both executables.
No live VM connections or real desktop applications were used.

## VM bootstrap batch

Added bootstrap with all Python flags: identity, GitHub setup toggle, repeated
agent installs and environment values, credential forwarding, avr installation,
repository/ref, dotfiles, inline/file scripts and print-only plans. Tunnel
checkpoints: public fc656a8, admin a46833a6cb.

Preserve the existing shell scripts and step order: environment, GitHub,
repository, dotfiles, agents, avr, custom script. Print mode needs no auth or
network and never invokes local credential helpers. Real runs resolve the
endpoint, silently probe SSH with five attempts, stream each step, and stop at
the first failed step. Environment and GitHub credentials use SSH stdin, not
command arguments or local terminal output. User scripts can still print their
own input; this is not a remote-output redaction mechanism.

Credential forwarding preserves explicit overrides, ordered/deduplicated
agents, per-agent warnings and the optional Claude setup-token/hidden-paste
prompt. Unicode environment-name validation uses the existing pinned x/text
normalizer, promoted from indirect to direct without a version change.
Run-script reads reuse the 16 MiB/UTF-8 guard and Python newline normalization.
GitHub helper output is bounded to 1 MiB and its diagnostics are suppressed;
cancellation and timeouts return concise errors. SSH pins are private and
removed on completion, failure, timeout and cancellation.

Decision: --install-avr retains Python's pipx/pip package installation inside
the VM until Go release packaging exists. No upstream CLI code was copied;
the script templates come from this repository's pinned Python implementation.

Validation: 92 paired cases per binary, including five terminal-output cases,
pass. They compare printed scripts, actual SSH arguments/stdin, helper order,
retry/failure behavior, credential overrides, API errors and pin content/mode/
cleanup. Seven paired real-terminal prompt scenarios plus one Go interrupt
check pass for each binary; hidden input is not echoed and terminal echo is
restored. Three Go tests cover execution of the environment setup script in an
isolated home, idempotent login hooks, private files, helper output bounds and
process timeout/cancellation cleanup. Public race tests, lint, generated API
checks, admin composition, fixture typing and both workflow validations pass.
Report hashes match both tested binaries. All helpers and API calls are local
fixtures; no VM connections, real authentication helpers or installations were
performed. Native terminal tests ran on macOS; native Linux/Windows remain CI.

## Command registration audit and plural aliases

Bootstrap checkpoints: public 41ba123, admin 28fbde8f66.

Added a live Go command inventory and an audit against the pinned Python tree.
It checks command presence/visibility, every primary and secondary flag name,
flag/value arity, repeatability, explicit non-null defaults, declared positional
count/required/variadic syntax, and plural aliases. The detailed report retains
both parameter definitions for the remaining behavior review. pflag is now a
direct development-tool import at its existing pinned version.

The first audit found 140 Go nodes versus 142 Python nodes: only the hidden
internal group and internal docs command are missing. It also reproduced a
missing vms alias. That alias is fixed; a Go regression test reads expectations
from the pinned Python inventory, and eight executable alias scenarios pass
for each binary. Public race tests/lint, admin composition, fixture lint/typing
and both workflows pass. The alias suites are included in both CI workflows.

The audit intentionally still exits nonzero for the missing docs nodes. It is
not yet a completion gate or a behavior-coverage claim: absent/default values,
enum/range/path validation, argument execution, terminal help and shell
completion still need the final executable coverage audit.

## Hidden documentation command

Registration/alias checkpoints: public 88ec80b, admin 2d2348280e.

Added internal docs with format, out, stdout and readme, preserving all five
format choices: Markdown, structured JSON, Starlight, man, and all. The native
command renders the public reference in both executables, matching Python's
public-docs behavior when composed into admin. It never needs Python at runtime
or calls the API.

Migrated the existing documentation prose, examples and Click-specific type/
metavar metadata from the pinned Python source. The Go renderer walks the live
public command tree for command/flag membership, aliases, flag arity, repeated
values and defaults. Tests cover adding/removing commands and flags, changed
optional and array defaults, hidden commands, and preservation of the existing
documentation schema. The reference version identifies the public package;
it is independent of the embedding admin executable's build identifier.
Reference defaults are stable even when AVR_LINKS overrides an invocation.

Keep the existing file layout, success messages and stale .md/.1 page cleanup.
Cleanup does not traverse directory symlinks. Markdown preserves code while
escaping prose placeholders; troff neutralizes request-looking lines. JSON
object-key ordering/escaping can differ while the decoded structure matches.
The Starlight generated comment now says command tree instead of Click tree.

Decision: default README lookup uses README.md beside the native executable,
where Python uses its package parent. An explicit --readme behaves as before.
make go-docs passes the source-tree README explicitly. A paired package test
copies the binary beside a fixture README and confirms the same overview page
as Python. Source metadata regeneration/checking needs the pinned Python input
during migration; installed native docs generation does not.

Validation: 42 paired scenarios per binary, including two terminal cases,
compare every generated page, file membership/mode, JSON structure, stdout,
errors, README variants and stale-file removal. Six Go tests cover registry
changes, escaping, cancellation and cleanup; the bundled-README check passes
for both binaries. The registration audit now passes all 142 command/group
nodes and every declared Python flag, with no missing plural aliases. Public
race tests/lint/build, generated API checks, metadata drift checks, admin
composition, fixture lint/typing and both workflows pass. Report hashes match
the rebuilt executables. The registration gate and docs suites are wired into
CI. This does not replace the remaining executable flag/argument/help/completion
audit, and native Linux/Windows runs remain CI gates.

## Terminal help and bare command behavior

Native docs checkpoints: public a4164f0, admin 8621ef25d9.

Added native help using the migrated public help prose, option labels and
section metadata. The formatter reads the live composed tree for visible
commands and flags, so admin extensions and hidden/removed entries remain
accurate. Metadata loads only when help is requested, without a Python process
or credential/API lookup. Independent command trees do not share mutable state.

Preserve uppercase bold sections for the groups that use them, plain leaf
help, option/default spacing, literal examples, aliases, inherited -h/--help,
and representative terminal widths. Bare unauthenticated avr shows the Python
welcome; authenticated avr shows help. Bare groups print help to stderr and
exit 2. Explicit help remains stdout/exit 0, including help placed before a
subcommand. Preserve Python's existing eager top-level help color behavior;
subcommand help and the welcome respect no-color settings. Cobra's additional
completion command is not advertised; compatible shell completion is next.

Validation: 392 Python/Go cases per executable, including 69 terminal cases,
pass with no API requests. The comparator checks visible layout and glyph
styles, allowing equivalent ANSI encodings, trailing spaces and the admin
program name. Six comparator regression tests detect style, layout, content,
exit and accidental-request changes. Two Go tests cover bare invocation
contracts, live composition and independent trees. Public race tests/lint,
metadata and registration gates, admin composition, Python tooling and both
workflows pass. The help suite is wired into public/admin CI. The 42 native
docs scenarios also pass per executable after extending shared metadata.
Report hashes match the tested binaries; native Linux/Windows remain CI gates.

## Native shell completion

Help checkpoints: public f54d0c0, admin 885cbe388e.

Implemented the existing _AVR_COMPLETE source/complete interface for Bash,
Zsh and Fish. Admin uses _AVR_ADMIN_COMPLETE and accepts the shared public
variable as a compatibility fallback. Existing documented setup commands
continue to work. Completion resolves the live composed command registry
before argument parsing, credential loading or handlers, and makes no API
requests. Timing diagnostics are suppressed for completion.

Migrated short descriptions and the parameter type fields needed for choices,
files and directories. The generator excludes runtime function representations
so metadata is reproducible. Command names, visible flags, aliases, used versus
repeatable options, option values, positional/variadic arguments and incomplete
shell quoting are handled natively. Hidden commands remain addressable without
being advertised. No additional runtime dependency was added.

The setup scripts consume the compatibility protocol directly. Shell tests
caught and fixed Zsh argument quoting and first/subsequent-tab autoload behavior.
Generated scripts are evaluated only in isolated test shells; no user profile
or completion installation was modified.

Validation: 1,847 differential completion scenarios per executable cover every
command's option/argument completions, supplied options, choice/path values,
aliases, quoting, separators, diagnostics and unsupported modes. Four Go tests
cover offline dispatch, live composition, safe tokenization and quiet timing.
Six comparator tests enforce record boundaries, ordering, types and absence
of API calls while allowing platform newlines. Bash and Zsh integration tests
pass for both executables, including actual Zsh tab insertion and directory
paths with spaces. Fish protocol comparisons pass locally; the actual Fish
shell is not installed here, so that integration test is a Linux CI gate.
Both workflows install Fish/Zsh and run the shell suite.

Public race tests, lint, deterministic metadata and command-registration gates,
admin composition, Python tooling and workflow validation pass. The existing
392 help comparisons per executable passed during this batch. Completion
reports identify the exact tested binaries. Native Linux/Windows execution and
the real Fish shell remain CI validation, not locally observed results.

## Explicit option-use validation

Completion checkpoints: public d629eb5, admin c46e3ab1f2.

Added 91 paired cases per executable for explicit organization ID/slug
precedence and unknown organizations across 24 operations; job relative-time
filters and invalid values; job JSON jq projection; all log levels; pager
bypass with and without color; run-list browser output; and the hidden login
email alias. All writes and browser/login operations use isolated fixtures.

The first run reproduced two presentation differences: run list --web omitted
Python's terminal Opening: prefix, and invalid --since errors used different
capitalization. Both are fixed; piped browser output remains the plain URL.

All 91 cases pass for both public and admin binaries, with report hashes
matching the tested executables. Public race tests/lint/build, admin composition
tests/lint/build, fixture lint/types and workflow validation pass. The suite
is now part of both CI workflows and the public go-parity target. This closes
the initial option-use gaps, not the remaining aliases and argument audit.

## Option aliases and value effects

Option-use checkpoints: public 320acda, admin 3d26d97dd8.

Added a typed input catalog that resolves command aliases, option scopes,
attached values, short clusters and argument separators against the pinned
Python command contract. It distinguishes fixture presence from zero-exit
non-discovery inputs. Neither category is a claim that behavior passes.

Derived 72 missing equivalent-spelling cases from existing behavioral fixtures.
The derivation keeps positive and negative boolean forms separate and reuses
each source suite's comparator and assertions. Added 25 direct cases for jq
object transformations on artifact deletion/download, run listing and settings
schema; named mirror resolution on GitHub and GitLab; run relative-time
filters; explicit login providers; and workflow-watch interval/exit status.
These exercise effects on output, API requests, files and local login state.
No live account, mirror, artifact or workflow is changed by the validation.

All 72 alias and 25 value cases pass for both binaries. Reports match the
tested binary hashes. Nine parser/generator/comparator tests pass, including
checks that agreement between two implementations ignoring jq or --source
cannot pass. Fixture lint/types and both workflow checks pass. Both new
suites run in public/admin CI and the public go-parity target.

The catalog contains 3,699 non-help/completion fixture inputs. Every leaf and
positional argument has an input, and only the root version spellings lack
a direct scenario. Some job/run watch options have no zero-exit case because
their fixtures deliberately end with an API failure, interruption or a failed
run's --exit-status. Their existing stream/output and exit comparisons remain
required evidence. Root/global behavior and the argument/default validation
audit remain open; input presence does not prove those requirements.

## Root flags and environment behavior

Option alias/value checkpoints: public 150d6ec, admin 3d41d283a2.

Added 42 root/global cases per executable: both version spellings, offline
version output with malformed environment settings, verbose flags at each
command position, debug categories, link environment booleans, explicit
overrides, and repeated positive/negative link flags.

The first comparisons reproduced missing warnings for unknown debug categories,
invalid link settings reaching the API, and --no-links always winning over
a later --links. Additional boundary coverage reproduced whitespace-only
AVR_LINKS behavior. Fixed all four: root command setup validates link settings,
warnings retain Python's sorted/deduplicated category presentation, and both
link flags share one ordered boolean value. Explicit flags override environment
values. Root version/help remain eager, and the native-only timing diagnostic
category remains supported without being reported as unknown.

Version comparison permits the intentional native build/version identifier and
admin program name while requiring a single correctly formed line, no stderr,
exit zero and no API/browser/config writes. Two Go tests and six comparator
tests guard flag order, validation before handlers, warnings and version side
effects. All 42 paired cases and the 392 help regressions pass for both binaries;
their reports match the rebuilt binaries. Public race/lint/build, metadata and
registration checks, admin checks, Python tooling and both workflows pass.
The global suite is wired into both CI workflows and public go-parity.

The input catalog now contains 3,740 cases, with a fixture for every declared
option spelling, every leaf and every positional argument. This is still input
coverage. Complete the argument/default/error audit and broader fresh regression
run before treating the entire public surface as verified.

## Remaining work

Finish the per-command argument/default/error behavior audit and broader
fresh differential regression run.
Existing implemented commands also remain subject to that exhaustive audit;
the prior suites are substantial evidence, not a claim that every flag combination
has already been checked. Packaging and native OS CI remain separate release gates.

The initial explicit option-use and alias gaps are closed by the suites above.
Continue with defaults and argument validation before claiming the full public
surface complete.
