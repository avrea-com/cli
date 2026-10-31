# Go CLI preview

This preview implements public authentication, CI-run/job inspection, logs,
job SSH/watch/metrics, retained job test results, run rerun/cancel/diagnose, workflow list/view/run, PR listing, the status dashboard, artifacts, cache management, log search, SBOM management, CLI configuration, scoped tokens, settings, organization setup, mirrors, billing, firewall, and VM creation/inspection/lifecycle/update and SSH sessions/configuration/tunnels/bootstrap and native reference generation in Go,
with the shared commands composed into a minimal Go `avr-admin`. It does not
replace the installed Python `avr`, migrate admin-only command families, or
modify repo-intel. The remaining migration is described in
[PLAN-go-cli-migration.md](PLAN-go-cli-migration.md).

VM start/stop preserve Python's legacy PATCH desired_state request. That field
is absent from the pinned current API schema, so those two commands still need
live integration verification. They do not silently substitute pause/resume or
delete, which change disk and password semantics.

VM bootstrap preserves Python's scripts and credential forwarding. Its
--install-avr step still installs the Python package in the VM; selecting a Go
release artifact belongs to the later release-packaging work.

## Native workflow dispatch (origin/main refresh, 2026-10-31)

The Go migration is locally rebased onto `df6d102` and its Python oracle is
pinned to that commit. The public tree still contains 151 command/group nodes;
this upstream slice extends `workflow run`, adding `--idempotency-key` and
native Avrea CI dispatch rather than a new command group.

`workflow run` now resolves native workflow IDs, full `.avrea/workflows/` paths,
and display names. Bare YAML filenames keep their GitHub Actions behavior.
Native `-f` values use declared workflow types; JSON preserves scalar types and
exact decimal values. Duplicate keys keep the last value before validation.

A native submission prints an invocation key and receipt with the accepted ref
and commit. Pending `--no-watch` submissions print a shell-quoted replay command
that freezes the host, org, repo, workflow, ref, typed inputs, and key. A lost
response gets one identical retry. HTTP failures are not automatically reposted;
retry guidance accompanies 429/5xx and malformed receipts. Go's implicit
transport POST replay is disabled for this operation so it cannot add retries
outside that budget. Receipt polling retries transient reads for at most 90
seconds, checks receipt identity, and watches only the exact admitted run.

The native POST has a 60-second timeout for the server's source/permission
checks without changing the shared client's default. UUID parsing reuses the
existing `google/uuid` dependency. New endpoint adapters validate receipt and
metadata shapes explicitly; the pinned generated API schema is unchanged.
Existing bounded/cancellable stdin parsing and terminal rendering are reused.

The native fixture suite covers 76 paired cases, including exact decimals,
replay commands, lost responses, transient receipt reads, permission errors,
invalid receipts, duplicate inputs, native/GitHub selection, and terminal
styles. Go tests exercise the printed replay against the same server, enforce
identical bodies/keys, check timeouts and cancellation, and guard the retry
limit on reused connections. All dispatches in validation target loopback
fixtures; no workflows were dispatched in staging or production.

The existing strict-JSON boundary remains: Go rejects non-standard JSON
`NaN`/`Infinity` before lookup; Python rejects them later during native input
validation. Both refuse submission. Ordinary JSON numbers, including large
finite exponents and decimals beyond binary float precision, are preserved.

## Live read-only output validation

`scripts/compare_staging_readonly.py` compares the checkout Python CLI and the
Go binary against existing staging data. The October 31 audit exercises 51
read-only command paths in 193 paired cases: pipes, 120-column terminals,
JSON where supported, successful/failed/native runs and jobs, completed log
following, bounded live watches, and an SBOM download into a private temporary
directory. The 151-node command inventory and local fixture suites cover the
rest of the public command tree, including aliases and help.
The completed audit passed 7,591 public fixture pairs and 42 shared usage-error
pairs in the real `avr-admin` binary, with no unaccepted differences in the
193 live pairs. Permission-denied and unavailable paths are listed below.

Run it from this worktree with the checkout virtualenv (not an older installed
Python `avr`):

```sh
.venv/bin/python -m scripts.compare_staging_readonly \
  --targets build/go/staging-readonly/targets.json \
  --go build/go/avr \
  --report build/go/staging-readonly/verified.json
```

The private target manifest contains `HOST`, `ORG`, `REPO`, `RUN_OK`, `RUN_FAIL`,
`RUN_NATIVE`, `JOB_OK`, `JOB_FAIL`, `JOB_NATIVE`, `WORKFLOW`, and `COMMIT`.
Choose existing avrea-core records; the script never creates test data. It
requires an explicit HTTPS staging host, reads its existing credential from
`hosts.json`, and isolates local CLI configuration. Use `--only 'run watch'`
to repeat a family. Captures may contain private job logs, so reports belong
under ignored `build/` with owner-only permissions and never in a PR.

Live execution has an explicit read-only allowlist. SSH, VM sessions, browser
opening, login, credential issuance, and every mutation stay in local mock
fixtures. Log search uses the API's read-only POST query. SAML test uses
`--no-browser` to print the URL without opening it. Downloads use private
local destinations. The harness rejects side-effect flags before spawning a
CLI process; its tests also check that styles, fields, exit codes, and download
contents cannot disappear unnoticed.

Comparisons preserve visible text, table layout, colors, bold/underline styles,
JSON values, and exit codes. Accepted differences are JSON object-key ordering,
equivalent ANSI encodings, fresh timestamps/ages, URL query-key ordering,
refresh cadence, floating-point roundoff in diagnostic metrics (1e-12 relative
or absolute tolerance), and one extra blank row before a piped log completion
footer. Changing list order, repeated query-value order, missing fields, or
lost colors remains a failure. Dynamic watch data can change between the two
invocations; distinct snapshots remain visible in the report rather than being
silently removed.

The audit found and fixed shared missing-option diagnostics (usage/help
preamble and allowed choices), the Git credential helper's required options,
incompatible follow/failed flag diagnostics, and piped versus terminal run
watch interruption messages. `compare_usage_errors` checks all required-option
errors and log-flag conflicts exactly in both pipes and terminals, including
write commands, without making API requests. It runs in `make go-parity` and
the three-platform preview CI. The shared command library also carries these
fixes into `avr-admin`.

Coverage limits from this staging account must not be counted as successful
live validation: SAML configuration, audit events, and billing reads return
403; there are no selected artifacts, VMs, scoped tokens, public-mirror
requests, or GitLab installations for their detail/download success paths.
Their local fixture suites cover those branches. Default `pr list` also has
a shared Python/server compatibility issue: both CLIs send `scope=all`, which
staging rejects; explicit `--scope authored` succeeds and is compared too.

## Startup performance

Help and completion decode only the command records they need. A generated
UTF-8 byte-offset index points into the existing embedded metadata, avoiding a
full scan of the 1.1 MB documentation JSON on each invocation. The generator
updates and checks the index together with the pinned metadata. Commands and
flags still come from the live Cobra registry, including admin extensions;
each decoded help record remains independently owned.

Measured on an Apple M5, macOS 26.6.2, Go 1.27.3, against baseline `9e0ef5b`:
101 paired samples per case, five warmups, alternating executable order,
isolated home, piped output, warmed filesystem caches and fresh processes.
All output and exit codes matched between the two binaries.

| Invocation | Before median | After median | Reduction |
| --- | ---: | ---: | ---: |
| `avr --help` | 6.43 ms | 4.61 ms | 28.2% |
| `avr help` | 6.40 ms | 4.59 ms | 28.3% |
| `avr repo list --help` | 6.36 ms | 4.52 ms | 29.0% |
| Root completion | 6.27 ms | 4.59 ms | 26.8% |
| Nested completion | 6.29 ms | 4.54 ms | 27.8% |
| `avr --version` (control) | 4.23 ms | 4.26 ms | Effectively unchanged |

The full process did not reach the requested 50% reduction. In-process root
help work fell from 1.94 ms to 0.17 ms (about 91%), with allocations falling
from roughly 1.18 MB to 0.54 MB. The old full metadata decode alone cost
1.73 ms; command-tree construction cost 0.11 ms. A separate probe that imports
the CLI but runs no command still takes about 3.9 ms end to end on this host.
These measurements include process-launch/exit and runtime/package startup;
they do not isolate individual OS costs or simulate a cold machine.

Changing CGO or stripping debug symbols did not improve startup materially.
Skipping signal setup saved less than 0.1 ms, so signal handling and build
settings are preserved. Network-bound commands retain their separate HTTP
latency; this measurement makes no claim of halving their total duration.

Reproduce the measurements using baseline and candidate executables built
with the same flags and version labels:

```sh
.venv/bin/python -m scripts.benchmark_startup --before /path/to/avr-before --after /path/to/avr-after
# Separate command work from process startup:
go test ./commands -run '^$' -bench BenchmarkStartup -benchmem -count=3
```

The benchmark writes raw samples, medians, p90 values and binary hashes to
`build/go/startup/comparison.json`. Timing is diagnostic, not a flaky CI
threshold. Help, completion, Unicode offsets, independent records and live
command composition are covered by regression and Python parity tests.

### Further startup and context work

Command-specific regular expressions and VM bootstrap templates now initialize
on first use with `sync.OnceValue`/`sync.OnceValues`. Config-only operations no
longer construct an API client. Config and Git providers remain on-demand;
there is no process-global credential/config cache. `auth status --json ?`
retains Python's config-loading/error ordering, but does not construct a client.

Successful organization ID/slug resolution is reused only inside one
`ExecuteRoot` invocation and is keyed by host, credential and organization ID.
A subsequent invocation always starts fresh; lookup failures are not cached.
For example, the linked `job view --log` fixture makes one organization-list
request instead of Python's three while preserving text, styles and links.
The differential comparator permits only removal of identical duplicate
organization GETs with a fixed successful fixture response. All other requests
(including read-only POST log searches and their bodies) remain ordered and
strict; moving responses do not qualify and writes are never removed. Reports
keep the raw calls.

Opt-in CPU profiling works through both `Execute` and `ExecuteRoot`:

```sh
AVR_CPU_PROFILE=/tmp/avr-cpu.pprof ./build/go/avr --help
go tool pprof -top /tmp/avr-cpu.pprof
# Package initialization happens before the CPU profile starts:
GODEBUG=inittrace=1 ./build/go/avr --version
```

Choose a new profile filename: existing files are never overwritten. Profiles
are private (0600 on POSIX) and flushed on success, error and handled interrupt.
Completion ignores profiling. Profiling adds sampling/flush overhead, so do not
use a profiled invocation as a latency benchmark; very short commands may have
no CPU samples. The standalone profile includes command construction; embedded
`ExecuteRoot` consumers begin profiling after constructing their own tree.

`Dependencies.UpdateNotices` is a disabled-by-default notifier scaffold. There
is no version lookup, cache, producer, background task or updater. A future
producer can offer a buffered notice tied to the invocation context. The CLI
only reads an already-ready notice, never waits for one at shutdown, and writes
to stderr only after successful interactive commands. Help, version,
completion, JSON/format output, pipes, hidden commands, CI, automation and
`AVR_NO_UPDATE_NOTIFIER` suppress notices. No telemetry was added.

CI now records fresh-process startup samples, hashes, p90/median timings,
package init traces and command allocation benchmarks on macOS, Linux and
Windows. These are uploaded with the preview artifacts; noisy wall-clock
measurements do not impose a hard timing threshold. For a single-build report:

```sh
python scripts/benchmark_startup.py --binary build/go/avr --report build/go/startup/current.json
```

Follow-up measurements on the same host used 101 paired samples after the
validation workload finished. Relative to the earlier help-index optimization:

| Invocation | Prior optimized median | Current median | Further reduction |
| --- | ---: | ---: | ---: |
| `avr --help` | 4.98 ms | 4.88 ms | 2.0% |
| `avr help` | 5.09 ms | 4.89 ms | 3.9% |
| Nested help | 4.86 ms | 4.71 ms | 3.0% |
| `avr --version` | 4.73 ms | 4.52 ms | 4.3% |
| JSON field discovery | 4.77 ms | 4.50 ms | 5.6% |
| Root completion | 5.14 ms | 4.90 ms | 4.6% |

A separate paired comparison against the original baseline measured root help
at 6.94 → 4.89 ms (29.6% cumulative improvement). Absolute times vary between
batches; compare paired results rather than subtracting medians from different
runs. The 50% end-to-end goal remains unmet. Package init tracing shows command
initialization at about 0.006 ms / 9,088 bytes / 115 allocations, versus the
previous 0.20 ms / 383,424 bytes / 3,970 allocations. go-runewidth initialization
is now about 0.008 ms with zero allocations. Init-trace timings are diagnostic,
separate from the fresh-process wall-clock samples.

Local validation completed 7,495 public and 549 actual admin paired cases with
zero unaccepted differences. These include CJK, combining marks, emoji and
invisible format characters in piped and TTY tables. Race tests, lint,
vulnerability checks, generated API/help checks, the 151-node command audit,
workflow validation and Linux/Windows amd64 cross-builds pass. A real CLI CPU
profile was read successfully by `go tool pprof`; short help had zero samples,
as expected from sampling a sub-millisecond command body. CI changes are local;
no new remote run, release or publication was initiated.

Raw reports: `build/go/startup/followup-final.json`, `cumulative-final.json`,
`current.json`/`current.inittrace.txt`, and `validation-summary.json` in that
same directory. The original job/option-gaps run detected the intended removed
organization requests; targeted reruns after the bounded comparator update are
recorded in `job-parity-final.json` and `option-gaps-parity-final.json`.

### Go dependency policy

Prefer current published versions and pin them in `go.mod`/`go.sum` for
reproducible builds. Check updates for runtime and tooling modules, run output
parity and startup checks, and document concrete incompatibilities. Weekly
Dependabot checks cover both modules. This policy does not imply taking
prereleases or changing major module APIs without validation.

As of 2026-10-31, all 17 retained runtime requirements resolve to their latest
published versions. Updates include go-runewidth 0.0.31, compress 1.20.3,
pflag 1.0.10, x/net 0.62.0, uax29/v2 2.7.0 and timefmt-go 0.1.9.
go-runewidth supplies terminal-column widths for bordered tables, including
combining characters, emoji and CJK. The previous 0.0.24 pin predated the
upstream startup regression; no compatibility exception was needed to update.

The top-level development tools are current (golangci-lint 2.15.0,
oapi-codegen 2.8.1, govulncheck/x-vuln 1.9.0). These transitive exceptions were
confirmed by failed compiler checks and are annotated in `tools/go.mod`:

| Dependency retained | Reason newer versions cannot currently be used |
| --- | --- |
| gobwas/glob 0.2.3 | 1.1.0 removes `glob.Glob`, required by protogetter 1.0.1. |
| depguard/v2 2.2.1 | 2.3.0 requires that incompatible glob 1.1.0. |
| speakeasy-api/openapi 1.25.6 | 1.26.x overlay methods use `go.yaml.in/yaml/v3.Node`; oapi-codegen passes `gopkg.in/yaml.v3.Node`. |
| speakeasy-api/jsonpath 0.6.3 | 0.7.0 changes YAML node packages in the generator dependency chain. |
| dprotaso/go-yit revision 9ba8df137936 | Newer revisions change YAML node packages; vmware-labs/yaml-jsonpath still requires `gopkg.in/yaml.v3`. |

Revisit these exceptions when the consuming tools adopt the changed APIs.

## Build and try

Use Go 1.27.3 (the module records the toolchain), Python 3.14, and the repository's
pinned uv version.

```sh
make go-build
./build/go/avr internal docs --format markdown --stdout
./build/go/avr auth --help
./build/go/avr auth status
./build/go/avr auth status --json email,host --jq .email
./build/go/avr tests JOB_ID
./build/go/avr tests list JOB_ID --limit 100 --json data,pagination
./build/go/avr tests JOB_ID --json data --jq '.data[] | select(.outcome == "failed")'
./build/go/avr org list
./build/go/avr org create 'My Team'
./build/go/avr org members --org ORG
./build/go/avr health
./build/go/avr vm list --org ORG
./build/go/avr vm create --name devbox --os linux --size 4-vcpu --ephemeral --wait
./build/go/avr vm show VM_ID --org ORG
./build/go/avr vm update VM_ID --name devbox --ttl 8h
./build/go/avr vm ssh-config VM_ID --host-alias devbox --append
./build/go/avr vm ssh VM_ID --session dev --login -- claude
./build/go/avr vm port-forward VM_ID --port 8080
./build/go/avr vm rdp VM_ID --launch
./build/go/avr vm vnc VM_ID --print
./build/go/avr vm bootstrap VM_ID --install claude,codex --repo https://github.com/me/project --print
./build/go/avr vm pause VM_ID --org ORG --memory --wait
./build/go/avr vm resume VM_ID --org ORG --wait
./build/go/avr vm delete VM_ID --org ORG --wait
./build/go/avr vm usage --org ORG --start 2026-10-01 --end 2026-10-30
./build/go/avr firewall list --org ORG
./build/go/avr firewall show --org ORG --repo REPO_ID
./build/go/avr firewall flow-summaries --org ORG --repo REPO_ID --with-drops
./build/go/avr firewall add --org ORG --action allow --fqdn api.example.com --proto tcp --ports 443
./build/go/avr firewall move RULE_ID --org ORG --to 0
./build/go/avr firewall set-default --org ORG --action deny
./build/go/avr firewall delete RULE_ID --org ORG
./build/go/avr billing summary --org ORG
./build/go/avr billing settings --org ORG
./build/go/avr billing update-settings --org ORG --email billing@example.com
./build/go/avr billing payment-methods set-default PM_ID --org ORG
./build/go/avr billing payment-methods remove PM_ID --org ORG
./build/go/avr billing invoices list --org ORG
./build/go/avr billing invoices show INVOICE_ID --org ORG
./build/go/avr billing invoices download INVOICE_ID --org ORG --out invoice.pdf
./build/go/avr billing payment-methods list --org ORG
./build/go/avr billing payment-methods add --org ORG
./build/go/avr health --json status
./build/go/avr audit-events list --org ORG --resource-type api_key
./build/go/avr repo clone acme/web
./build/go/avr repo clone acme/web checkout --depth 1 --branch main
./build/go/avr repo mirror locations
./build/go/avr repo mirror create acme/web --location gsc-fi
./build/go/avr repo mirror status --repo acme/web
./build/go/avr repo mirror enable --repo acme/web
./build/go/avr repo mirror disable --repo acme/web
./build/go/avr repo mirror place gsc-fi --repo acme/web
./build/go/avr repo mirror unplace gsc-fi --repo acme/web
./build/go/avr repo public-mirror check rust-lang/rust
./build/go/avr repo public-mirror request rust-lang/rust --reason 'Build dependency'
./build/go/avr repo public-mirror requests
./build/go/avr repo public-mirror view REQUEST_ID
./build/go/avr repo public-mirror cancel REQUEST_ID
./build/go/avr org install list
./build/go/avr org install add --org ORG --no-browser --wait-seconds 0
./build/go/avr org install remove --org ORG --installation-id INSTALLATION_ID
./build/go/avr org saml show --org ORG
./build/go/avr org saml sp-metadata --org ORG
./build/go/avr org saml configure idp-metadata.xml --org ORG --email-attribute mail
./build/go/avr org saml test --org ORG --no-browser
./build/go/avr org saml enforcement on --org ORG
./build/go/avr org saml remove --org ORG
./build/go/avr org email-domain list --org ORG
./build/go/avr org email-domain claim example.com --org ORG
./build/go/avr org email-domain verify example.com --org ORG
./build/go/avr org email-domain set example.com corp.example.com --org ORG
./build/go/avr status --repo OWNER/REPO
./build/go/avr status --since 24h --json
./build/go/avr repo list --org ORG
./build/go/avr sbom list --repo OWNER/REPO
./build/go/avr sbom view COMMIT_PREFIX --repo OWNER/REPO --full
./build/go/avr sbom download COMMIT_PREFIX --repo OWNER/REPO --format inventory --out inventory.json
./build/go/avr sbom generate COMMIT --repo OWNER/REPO --wait --wait-timeout 900
./build/go/avr settings list
./build/go/avr settings schema --scope repository
./build/go/avr settings set cache.gha.enabled false --repo OWNER/REPO
./build/go/avr settings reset cache.gha.enabled --org ORG
./build/go/avr config
./build/go/avr config get org
./build/go/avr config set org ORG_SLUG
./build/go/avr config unset org
./build/go/avr token list
./build/go/avr token view TOKEN_ID
./build/go/avr token revoke TOKEN_ID
./build/go/avr token create --name ci-read --repo OWNER/REPO --ttl 8h --json token,id
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

## Retained job test results

`tests JOB` and `tests list JOB` accept the `job-` or `awj-` IDs from `job list`.
The default repository comes from that job; `--repo` overrides it. `--limit`
counts test occurrences, including repeated tests, and short or empty pages
are followed up to 20 requests. A remaining cursor is printed on stderr for
resumption with `--cursor`. Failed tests do not cause a command error.

All pages must describe the same confirmed execution. API errors, malformed
pages or a changed snapshot fail without partial output. Missing and expired
reports do not establish that no tests ran, and complete reporting does not
establish that every expected test ran. The displayed caveats match Python.
The migration's Python reference is pinned to public main at `56a09f5`.

Run the dedicated terminal/API comparison with:

```sh
.venv/bin/python -m scripts.compare_tests --go build/go/avr
```

## Shared boundaries and admin reuse

`commands.NewRoot`, `NewAuth`, `NewOrg`, `NewRepo`, `NewRun`, `NewJob`, and `NewTests` create fresh
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
does not substitute for those native jobs. Hidden-prompt Windows fixtures select
Python's standard piped-input fallback, because Windows getpass otherwise reads
the real console despite redirected stdin. Secret validation, API calls, and
leak checks still run. Path fixtures use explicit home paths to keep shell/Click
tilde expansion outside the command comparison; JSON wildcards use `--json=*`.
The loopback server has a 128-connection backlog for concurrent diagnostic requests.
SSH text-file comparisons accept LF/CRLF differences; their contents and file effects
remain checked. Binary downloads and Git protocol output remain byte-exact.
Windows clone probe cases use native Git, including its successful empty-repository
result for the HTTP-200 fixture; POSIX stub cases verify child exit propagation.
Actual provider login with a human
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

## JSON ordering and embedded jq compatibility

Go AVR follows GitHub CLI's object-ordering behavior: plain JSON keys are
sorted, and the order in `--json status,conclusion,run_id` selects fields but
does not prescribe their printed or iterated order. The embedded gojq engine
uses unordered Go objects and sorted object iteration. Python AVR preserves
insertion order, so this is an intentional migration difference.

For stable CSV/TSV columns, name the fields explicitly:

```sh
avr run list --json status,conclusion,run_id \
  --jq '.[] | [.status, .conclusion, .run_id] | @tsv'
```

Do not depend on `[.[]]` or `to_entries` retaining requested-field or API
member order. Array order remains significant and is preserved. gojq does not
provide `keys_unsorted`; unsupported expressions fail rather than invoking an
external jq. Tests cover sorted objects, explicit column ordering, missing
function errors, and exact TSV row/column boundaries.

This choice matches [GitHub CLI's exporter](https://github.com/cli/cli/blob/trunk/pkg/cmdutil/json_flags.go)
and [gojq's documented differences](https://github.com/itchyny/gojq/blob/v0.12.19/README.md).
It does not claim complete compatibility with every external jq expression.

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
