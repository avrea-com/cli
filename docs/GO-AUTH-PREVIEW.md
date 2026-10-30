# Go CLI preview

This preview implements public authentication, CI-run/job inspection, logs,
job SSH/watch/metrics, run rerun/cancel/diagnose, workflow list/view/run, PR listing, the status dashboard, artifacts, cache management, log search, and SBOM list/view/download in Go,
with the shared commands composed into a minimal Go `avr-admin`. It does not
replace the installed Python `avr`, migrate admin-only command families, or
modify repo-intel. The remaining migration is described in
[PLAN-go-cli-migration.md](PLAN-go-cli-migration.md).

## Build and try

Use Go 1.27.3 (the module records the toolchain), Python 3.14, and the repository's
pinned uv version.

```sh
make go-build
./build/go/avr auth --help
./build/go/avr auth status
./build/go/avr auth status --json email,host --jq .email
./build/go/avr org list
./build/go/avr status --repo OWNER/REPO
./build/go/avr status --since 24h --json
./build/go/avr repo list --org ORG
./build/go/avr sbom list --repo OWNER/REPO
./build/go/avr sbom view COMMIT_PREFIX --repo OWNER/REPO --full
./build/go/avr sbom download COMMIT_PREFIX --repo OWNER/REPO --format inventory --out inventory.json
./build/go/avr sbom generate COMMIT --repo OWNER/REPO --wait --wait-timeout 900
./build/go/avr config
./build/go/avr config get org
./build/go/avr config set org ORG_SLUG
./build/go/avr config unset org
./build/go/avr log search --repo OWNER/REPO --query "error" --level error
./build/go/avr log search --repo OWNER/REPO --json content,line_number
./build/go/avr cache list --repo OWNER/REPO --type gha
./build/go/avr cache usage --repo OWNER/REPO
./build/go/avr cache delete --repo OWNER/REPO --type gha --key node_modules
./build/go/avr artifact list OWNER/REPO
./build/go/avr artifact get ART_ID --repo OWNER/REPO
./build/go/avr artifact download ART_ID --repo OWNER/REPO --path ./downloads/
./build/go/avr artifact delete ART_ID --repo OWNER/REPO
./build/go/avr pr list --scope authored
./build/go/avr pr list --repo OWNER/REPO --state all --json number,title
./build/go/avr run list --repo OWNER/REPO
./build/go/avr run view RUN --steps
./build/go/avr run view RUN --log-failed
./build/go/avr run diagnose RUN
./build/go/avr run diagnose RUN --json
./build/go/avr run watch RUN --exit-status
./build/go/avr run watch RUN --ndjson
./build/go/avr job list --status failure
./build/go/avr job watch --repo OWNER/REPO
./build/go/avr job watch --ndjson
./build/go/avr job view JOB_ID
./build/go/avr job metrics JOB_ID
./build/go/avr job metrics JOB_ID --watch
./build/go/avr job metrics JOB_ID --source cpu --source network --json
./build/go/avr job logs JOB_ID --follow
./build/go/avr run logs RUN --job test
./build/go/avr job ssh JOB_ID --print-command
./build/go/avr workflow list --repo OWNER/REPO
./build/go/avr workflow view build.yml --repo OWNER/REPO
./build/go/avr workflow run build.yml --repo OWNER/REPO --ref main -f env=test
./build/go/avr run rerun RUN --failed
./build/go/avr run cancel RUN
```

The preview uses the same credential store as Python by default. Use a separate
directory when trying login/logout without changing your existing login:

```sh
AVR_CONFIG_DIR="$PWD/build/go/preview-config" ./build/go/avr auth login
AVR_CONFIG_DIR="$PWD/build/go/preview-config" ./build/go/avr auth status
AVR_CONFIG_DIR="$PWD/build/go/preview-config" ./build/go/avr auth logout
```

Logout revokes the selected API key on the server. An `AVR_TOKEN` override is
used for revocation, just as in Python. Set or unset it deliberately when doing
a manual smoke test.

Implemented commands:

- `auth login [--provider github|google] [--email EMAIL]`: browser OAuth or
  email-based SSO, localhost:8765 callback, CSRF/session exchange for a 90-day
  API key, temporary-session logout, single-organization default selection.
- `auth status [--show-token] [--json FIELDS] [--jq EXPRESSION]`: masked human
  output, field discovery with `--json '?'`, projection, direct embedded gojq,
  and local-only status for scoped tokens.
- `auth switch [HOST]`: list stored hosts or change the stored default.
- `auth logout`: revoke the selected key and remove its local host entry.
- Hidden `login`/`logout` aliases and `auth git-credential` for existing Git
  helpers, including exact Basic/Bearer protocol output and fail-closed behavior.

Run control and workflow discovery:

- `run rerun RUN [--failed] [--yes]`: confirm, request a rerun, and find the
  new attempt with bounded polling.
- `run cancel RUN [--yes]`: confirm cancellation and report already-finished runs.
- `workflow list`: repository filters, time window, sorted limits, aggregate
  tables, JSON projection and jq.
- `workflow view WORKFLOW`: resolve by ID, numeric platform ID, name, filename
  or stem; show summary/job statistics and recent runs, or open the browser.

- `workflow run WORKFLOW`: dispatch with raw fields or JSON stdin, resolve the
  default branch unless --ref is supplied, then watch by default. Use --no-watch
  to return after dispatch.

- `job watch`: watch active jobs in a styled table or a JSON snapshot stream;
  filter by repository/name and configure the refresh interval. `jobs` is an
  alias for the job command group.

Inspection commands:

- `status`: recent runs, slowest workflows/jobs, period-over-period slowdowns,
  cache usage and repository links, with raw JSON and styled human output.
  Auxiliary failures produce warnings while retaining the available panels.
- `pr list`: organization availability check, repository/Git selection,
  scope/state filters, pagination, fixed terminal tables and links, piped TSV,
  JSON field selection and embedded gojq. `prs` is an alias.
- `org list` and `repo list`: organization selection, scoped credentials,
  tables, JSON field selection, and embedded gojq.
- `run list`: repository IDs/names or Git remote detection, repeatable filters,
  date windows, cursor pagination, terminal links, and piped TSV.
- `run view [RUN]`: metadata, jobs/steps, job filtering, full or failed-step
  logs, paging, browser links, other attempts, JSON, and gojq. Run references
  accept canonical Avrea IDs, GitHub numeric IDs/URLs, and matching-environment
  Avrea console URLs. Without RUN, show recent runs.
- `run diagnose RUN`: bounded failed-step/log evidence, queue/execution timing,
  CPU/memory/I/O summaries, and a prior-success workflow baseline. Raw JSON and
  styled human output both expose missing or truncated evidence.
- `run watch [RUN]`: explicit or automatic run selection, terminal progress,
  NDJSON job/run events, transient retries, interruption, and failure exit codes.

Job commands:

- `job list`: repository/Git context, repeated name/state/conclusion/workflow
  filters, Avrea/shadow selection, pagination, JSON aliases, and gojq.
- `job view JOB_ID`: job/run metadata, steps, full or failed-step logs, browser
  links, JSON, and gojq.
- `job logs JOB_ID`: step/level filters, failed steps, diagnostic opt-in, paging,
  and `--follow` with retries, raw cursor progress, completion checks, late-log
  draining, and Ctrl-C.
- `run logs RUN`: job-name filtering, failed jobs, diagnostic opt-in, and paging.
  `--follow` chooses one queued/running job, falling back to the first job,
  matching Python; it does not multiplex every job.
- `job metrics JOB_ID`: resource gauges with average, peak and last values;
  repeated source selection, start/end timestamps, raw JSON, and live metrics
  with a 12-line log tail. Finished jobs use the static view even with --watch.
- `job ssh JOB_ID`: forwarding setup and system OpenSSH with inherited streams,
  child exit status, cancellation, `--print-command`, and explicit
  `--show-password`. Passwords remain masked by default and are not passed to
  the launched SSH process. Printed command behavior matches Python.

The root also provides help/version/completion and the existing auth-relevant
`--verbose/-v`, `--no-color`, `--links`, and `--no-links` flags.
Terminal output follows Python: muted underlined headers, bold names, cyan IDs,
colored status symbols, matching table widths/truncation, and bordered org tables.
Auth confirmations and hints retain their emphasis. Both --no-color and NO_COLOR
suppress styling; piped JSON/TSV remains unstyled. Terminal links are independent; `AVR_LINKS=0` or `--no-links` disables links. `AVR_DEBUG=api` enables request diagnostics without printing credentials.

## Shared boundaries and admin reuse

`commands.NewRoot`, `NewAuth`, `NewOrg`, `NewRepo`, `NewRun`, and `NewJob` create fresh
Cobra command objects with explicit dependencies. `ExecuteRoot` applies the
shared exit-code contract to an admin-composed tree. Only executable entry
points call `os.Exit`; they opt into closed-pipe handling.

`config`, `client`, `output`, and `commands` are public packages.
Core's `cli-go` module preserves admin API environment precedence and host-keyed
credentials without mutating the process environment. Build its preview against
this checkout while the public Go module is unpublished:

```sh
make -C /path/to/avrea-core cli-go-check AVR_CLI_DIR=/path/to/cli
/path/to/avrea-core/cli-go/build/avr-admin run list --json '*'
```

Admin-only commands have not migrated; unknown commands return a usage error.
There is no implicit Python fallback in this minimal executable.

Inspection uses generated wire models behind a handwritten client boundary,
retaining raw JSON for precise projection. The released generator passed both
complete public and internal OpenAPI 3.1 schemas. Committed public generation
inputs and version provenance live under `api/`; private schemas remain in
core and ignored build artifacts. Run `make go-schema-gate AVREA_CORE=/path/to/core`
to repeat the full gate.

## Validation

```sh
make go-check
```

This runs race tests, vet, golangci-lint, auth and inspection executable
differential validation, generation drift checks, and govulncheck. The existing Python `make test`/`make lint` targets remain intact.

The paired runner extracts the Python source from commit
`99e74d119d83c7cef2887c2dd04a724719e6b009` using `git archive`. Python dependencies
come from the existing frozen uv lock. It downloads the official checksum-pinned
jq 1.7.1 binary into `build/go/oracle` for the Python reference; Go never invokes
that binary. A locally installed jq 1.7.1 or Apple's 1.7.1 variant is also
accepted when running the script directly.

Each implementation gets a separate temporary home, configuration, and working
directory. The fixture server resets between runs. All credentials are synthetic,
API requests stay on loopback, and the browser is a local stub. The real
localhost callback and executable boundaries are exercised without contacting
OAuth providers or production.

`build/go/auth-parity.json` contains paired raw stdout/stderr, exit status,
parsed credential files, permissions, browser URLs, request sequences, and
differences, plus the tested binary SHA-256 and interpreter/build versions. Human output compares visible text with harmless whitespace and
ANSI styling removed. Named CLI/parser/jq diagnostics compare their meaningful
terms and retain the full captures. JSON compares types and values, including
null/missing and numeric precision. Git protocol stdout is compared exactly.
No failing scenario is waived as part of a numerical "99%" threshold.

The inspection runner saves `build/go/inspection-parity.json`. It checks 143
scenarios on macOS, including JSON types/values, requests, paginated logs,
retry resets and limits, real Ctrl-C, early pipe closure, terminal columns,
hyperlink targets, and link opt-outs. Event timestamps are validated but their
exact values are excluded from comparisons. Formatting comparisons require
visible data; a hidden hyperlink target cannot substitute for a missing label.
Go tests additionally exercise pager selection, early exit, and cancellation
with a real child process.

The suite includes 33 presentation cases with real PTYs and color enabled,
covering auth, org/repo lists, run lists/details/logs/watch, long cells, status
palettes, and color opt-outs. Visible glyphs are compared with their foreground,
bold, dim, and underline attributes; representative static layouts also compare
indentation, columns, and truncation. Equivalent SGR encodings, trailing padding,
and moving clocks are normalized. Exact redraw/resize behavior remains outside
scope. Regression tests verify that stripping an underline, changing a color,
or losing truncation fails the comparator.

To compare the actual admin executable through the same fixture corpus:

```sh
uv run --python 3.14 --frozen --extra dev python -m scripts.compare_inspection \
  --go /path/to/core/cli-go/build/avr-admin \
  --report build/go/admin-inspection-parity.json
```

The auth matrix covers success, aliases, host/env selection, token masking and
explicit disclosure, scoped-token behavior, organization pinning, browser/SSO
flows and failures, HTTP errors, gzip/zstd, jq success/error/empty/halt behavior,
and Git credentials. POSIX systems also run real-PTY status and host-list checks.
Deeper Go tests cover cancellation, timeout/listener cleanup, invalid responses,
credential persistence, and blocking credential-bearing redirects.

CI is configured for native build/test/parity on Linux, macOS, and Windows, retaining preview
binaries, notices, and reports. Windows runs piped-output checks; the small PTY
smoke subset runs on POSIX. Cross-compilation is useful additional evidence but
does not substitute for those native jobs. Actual provider login with a human
browser remains a manual smoke check before a user-facing release.

The job batch has its own `build/go/job-parity.json` report and runs under
`make go-check` and native preview CI. Its 79 paired cases include 21 terminal
presentation cases, typed JSON aliases, exact piped TSV, paginated/filtered logs,
delayed final draining, real Ctrl-C, and stubbed SSH argument/exit checks.
Go behavior tests cover blocked-request cancellation, cursor-loop rejection,
late log pages, secret masking, invalid SSH endpoints, and real child-process
I/O and cleanup. The same corpus runs against the admin binary as
`build/go/admin-job-parity.json`. SSH validation uses a stub executable and
synthetic forwarding responses, not a live runner session.

To rerun only that batch:

```sh
uv run --python 3.14 --frozen --extra dev python -m scripts.compare_jobs --go build/go/avr
```

## Deliberate differences and bounded safeguards

- gojq v0.12.19 replaces the user's system jq. It preserves integer precision and
  runs in-process. Query errors use fixed diagnostics so a failing expression
  cannot echo credentials to stderr. No fallback to system jq. Tests cover raw
  strings, multiple/empty results, errors without partial output, halt, large
  integers, and cancellation.
- Each JSON API response (including a page of log entries) and jq output is
  bounded at 16 MiB; jq execution is bounded at ten seconds. Log following
  fetches successive pages rather than treating the whole stream as one response.
- Credential writes use private temporary files and atomic replacement.
  Existing per-host fields, insertion order, default promotion, and 0600 Unix
  permissions remain compatible.
- Invalid API/SSO responses return a clean error instead of a Python traceback.
  Callback credentials and CSRF headers are validated without reflecting secrets.
- Only GET /callback completes login. Incidental requests such as /favicon.ico
  no longer consume the login attempt. The callback page reports failure when
  no session is present. HTML escapes server-provided account text.
- Authenticated HTTP never follows redirects. Login session traffic uses
  explicit cookies, never an existing API key. External browser launching uses
  argument parsing, without shell evaluation or command substitution.
- Help and Go build/runtime metadata differ. Human terminal layouts and styling
  follow Python closely; fine-grained terminal-event parity remains outside scope.
  Logs preserve ANSI SGR styling on colored terminals and escape other external
  control sequences. Piped/no-color logs strip styling. Configured pagers use argument parsing rather than shell
  evaluation; early pager exit and pager interruption complete successfully,
  while interrupting a watch exits 130.

## Pinned libraries and tools

All direct/runtime pins below were checked against Go proxy version metadata on
2026-10-29 and satisfy the three-day minimum age. go.sum and tools/go.sum lock
the resolved graphs. These dates are manual dependency-review evidence, not a
new age-gate service.

| Dependency | Pin | Release date (UTC) | License |
| --- | --- | --- | --- |
| Go toolchain | 1.27.3 | 2026-10-19 | BSD-3-Clause |
| Cobra | 1.10.2 | 2025-12-03 | Apache-2.0 |
| pflag | 1.0.9 | 2025-09-01 | BSD-3-Clause |
| gojq | 0.12.19 | 2026-04-01 | MIT |
| timefmt-go | 0.1.8 | 2026-04-01 | MIT |
| klauspost/compress | 1.20.1 | 2026-09-25 | BSD-3-Clause and bundled notices |
| go-shellquote | 95032a82bc51 | 2018-04-28 | MIT |
| x/term | 0.47.0 | 2026-10-09 | BSD-3-Clause |
| x/sys | 0.49.0 | 2026-10-06 | BSD-3-Clause |
| mousetrap (Windows/Cobra) | 1.1.0 | 2022-11-27 | Apache-2.0 |
| golangci-lint | 2.13.2 | 2026-08-27 | GPL-3.0, development tool only |
| govulncheck (x/vuln) | 1.9.0 | 2026-10-11 | BSD-3-Clause |
| x/tools (analyzer compatibility) | 0.52.0 | 2026-10-10 | BSD-3-Clause |
| honnef.co/go/tools | 0.8.1 | 2026-08-21 | MIT |

The separate tools module pins a Go 1.27-capable analyzer graph; the linter's
unmodified transitive graph could not read Go 1.27 export data. Tool dependencies
are not linked into the CLI. Preview builds collect runtime license files and
the Go license into `build/go/THIRD-PARTY-NOTICES.txt`. Distribution/release
automation and SBOM/provenance changes remain in the later release phase.

No application source was copied from gh, Depot, or Namespace. The implementation
uses Avrea's existing Python behavior and the selected reusable libraries.

## Initial auth checkpoint evidence

Validated on macOS arm64 on 2026-10-29:

- `make go-check`: race tests, vet, golangci-lint (zero issues),
  paired executable checks, and govulncheck (no vulnerabilities).
- 65 Python-versus-Go scenarios passed, including two real-PTY checks.
- 118 selected Python auth/config/JSON and comparator-regression tests passed.
- Repository-wide Python lint, format, and type checks passed.
- Final sources cross-compiled for Linux amd64 and Windows amd64.
- Help/version and collected runtime license notices were inspected.

Native Linux/Windows CI execution and a manual login against a real OAuth/SSO
provider are not claimed by these local results. CodeRabbit was signed out, so
no remote CodeRabbit review was performed.

## Opt-in HTTP timing

To inspect an individual request without changing structured stdout:

```sh
AVR_DEBUG=timing ./build/go/avr auth status --json email,host --jq .email
```

Stderr receives newline-delimited JSON prefixed with `avr-timing `. HTTP records
contain offsets for connection acquisition, DNS, TCP attempts, TLS, request write,
first response byte, response headers, body read/decompression, and completion.
A command record measures the command dispatcher through output/error handling.
These records omit URLs, addresses, headers, error text, credentials, and response
bodies. Timing is separate from `--verbose` and disabled unless explicitly requested.

For repeated measurements against the current configured account:

```sh
uv run --python 3.14 --frozen --extra dev python scripts/benchmark_auth_timing.py
```

This performs read-only auth status requests in 20 fresh instrumented processes
and 20 alternating controls, after two warm-ups per mode. It saves numeric traces
and summary statistics to `build/go/auth-timing.json`, checks that account output
does not change between runs, and never saves that output. Connection reuse is
not possible across these separate CLI processes. Multiple/retried or overlapping
events are retained for manual analysis rather than forced into an additive table.

Measured on macOS arm64, 2026-10-29, with HTTP/2 and exactly one TCP attempt in
each instrumented sample:

| Phase | Median |
| --- | ---: |
| DNS | 2.67 ms |
| TCP connection | 6.45 ms |
| TLS handshake | 17.29 ms |
| Request written to first response byte | 71.55 ms |
| Response headers after first byte | 0.06 ms |
| Body read and decompression | 0.22 ms |
| JSON decode and request cleanup | 0.12 ms |
| Other HTTP setup | 0.31 ms |
| Command work outside HTTP, including projection/jq | 0.38 ms |
| Process launch/runtime/shutdown and parent measurement overhead | 11.02 ms |

Instrumented wall median: 113.06 ms; control wall median: 105.89 ms. HTTP median:
100.11 ms. Per-phase medians do not sum exactly to the median total. The dominant
tail is also the request-to-first-byte interval (p90 174.25 ms).

The request-to-first-byte interval includes the network round trip, edge/proxy
handling, and API processing; client instrumentation cannot separate those.
It is not a measurement of server CPU time. The next investigation for that
interval is a correlated server trace of GET /users/me, rather than changes to
JSON formatting or jq.

A separate loopback benchmark with the same reused connection measured a median
33.72 microseconds/request without tracing versus 35.65 with tracing across three
one-second benchmark runs. This isolates hooks and serialization (output goes to
io.Discard, so it excludes actual stderr I/O). It does not establish that the
7 ms live median difference is tracing overhead; live samples contain substantial
response-wait variation.

Reproduce the local overhead check with:

```sh
go test ./client -run '^$' -bench BenchmarkRequestTiming -benchtime=1s -count=3
```

Artifact downloads require complete metadata with a valid size and SHA-256.
They stream into a private temporary file next to the destination and replace
the destination only after verification. Existing directories receive the
artifact's filename; a nonexistent --path is treated as a filename, matching
Python. ZIP artifacts remain archived. Redirects and compressed responses to
the identity download request are rejected. Delete requires confirmation unless
--yes is supplied.
