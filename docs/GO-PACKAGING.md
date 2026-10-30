# Public Go preview packaging

Build and validate the native package locally, from the repository root:

```sh
make go-package
# Or, without uv (Python 3.14 and Go 1.27.3 on PATH):
python3 scripts/go_package.py
# An explicit Go binary is supported:
python3 scripts/go_package.py --go /path/to/go
```

The script writes an archive, a SHA-256 sidecar, and a validation JSON report
under `build/go/packages/`. It builds the current worktree, labels uncommitted
sources with `.dirty`, and records the full base commit and Go toolchain.
Use `--out DIRECTORY` to choose another destination or `--version VERSION`
to override the default project-version-plus-`-go-preview` label.

Archives are `avr-go_VERSION_TARGET.tar.gz` on Unix and `.zip` on Windows.
Each has one enclosing directory with an executable named `avr` or `avr.exe`.
Extract the archive into a separate directory and invoke that executable
directly. The bundled README explains optional PATH and completion setup.
Building and validating do not install or replace a command.

Revalidate an existing archive together with its checksum sidecar:

```sh
python3 scripts/go_package.py --verify build/go/packages/avr-go_0.5.0-go-preview_darwin_arm64.tar.gz
python3 -m unittest scripts.test_go_package -v
```

Validation checks the archive checksum, safe extraction, required files, and
the per-file manifest. It executes the extracted binary from a path containing
spaces, with a clean temporary home and empty PATH. It checks version/revision,
root and command help, local config, Bash/Zsh/Fish completion source equality,
bundled README discovery, and embedded jq against a local HTTP fixture.
No real credentials or service calls are needed. This does not impose an OS
network sandbox. Timings in the report are informational single samples.

The integrity tests reject corruption, mismatched contents, duplicate paths,
traversal and symlinks, and exercise both tar and ZIP extraction. Checksums
detect corruption; these preview artifacts are not signed release assets.

`.github/workflows/go-package.yml` is reusable through `workflow_call` and
can also be started manually. CI calls it alongside the existing Go validation
jobs, so artifact checks can finish even when a separate parity check fails. It builds and validates natively on macOS ARM64, Linux AMD64
(Ubuntu 22.04), and Windows AMD64, then uploads `avr-go-package-TARGET`
artifacts containing the archive, checksum and report. A manual invocation
runs package checks; it does not run the full CI suite. No release is published.

This first slice does not implement a system installer, automatic upgrades,
signing/notarization, provenance, or release SBOM generation. Existing Python
release behavior is unchanged.

## Production VM trial — 2026-10-30

The same source snapshot and uncommitted packaging overlay were built through
`avr vm` on two dedicated production VMs in the Avrea organization. Each VM
had 4 vCPUs, 15 GiB RAM, a 150 GB ephemeral disk, and a one-hour auto-stop TTL.
The production host was selected per command with
`AVR_HOST=https://api.avrea.com`; the local staging default was not changed.

| Native target | Guest | Python | Go | Build, package and validate | Archive bytes |
| --- | --- | --- | --- | --- | --- |
| linux_amd64 | Ubuntu 26.04.1 LTS | 3.14.4 | 1.27.3 | 11.72 s | 11,779,509 |
| windows_amd64 | Windows Server 2025 | 3.14.8 | 1.27.3 | 16.55 s | 12,218,311 |

These are single-run observations, not benchmarks. They include module
downloads and use fresh per-trial Go build/module caches; they exclude initial
Go toolchain setup, source transfer, and the seven integrity tests. Linux
maximum resident memory reported by `time -v` was 443,576 KiB.

Both targets passed all **7 integrity tests** and **12 extracted-executable
checks**, including empty-PATH operation, version/revision, help, config,
completion source equality, bundled README lookup, and embedded jq against a
loopback HTTP fixture. The package implementation needed no changes for these
platforms. Full command parity and real interactive shell tests were not rerun
on these VMs.

The artifacts and logs are retained under `build/go/prod-vm-trial/`:
`linux/packages/` and `windows/packages/` contain native archives, checksum
sidecars and validation reports. `results.json` records both results, and
`source-manifest.json` records the exact overlay hashes over base commit
`d7193e4829794461e48ecfcfcd30811925071d73`. Both binaries identify themselves
as `0.5.0-go-preview+d7193e482979.dirty`. After download, archive checksums and
every payload hash were independently checked locally.

Operational findings:

- Production rejected an explicit Ubuntu 22.04 creation request with HTTP 422.
  Its default Linux image was Ubuntu 26.04. This validates the Linux AMD64
  package process, but Ubuntu 22.04 execution still belongs to the CI matrix.
- Stock Go versions were older (Linux 1.26.9, Windows 1.24.13). Setting
  `GOTOOLCHAIN=go1.27.3` fetched the required toolchain successfully. Windows
  Python 3.14.8 was available in the hosted tool cache despite PATH selecting
  Python 3.12.10.
- The local 1Password SSH agent offered too many identities before the explicit
  key. A task-local SSH wrapper selected `IdentitiesOnly=yes` and
  `IdentityAgent=none`, retaining AVR's API-provided host-key pin.
- Windows PowerShell's SSH-stdin `ReadToEnd()` transfer stalled. SCP with the
  same API-provided host-key pin worked for source and artifact transfer.
- The installed Python `avr vm stop` command returned HTTP 422. The checked-in
  Go implementation preserves the same legacy `desired_state` request shape,
  already noted as absent from the pinned API schema. This is a separate VM
  lifecycle compatibility issue to resolve before automating cleanup through
  `stop`; this trial does not change that runtime contract.

Both trial VMs were deleted successfully through `avr vm delete --yes`.
Follow-up reads returned VM-not-found (HTTP 404) for both IDs. The pre-existing
macOS VM was untouched. No preview release was published.

## Production Linux ARM64 attempt — 2026-10-30

Temporarily enabled `linux_arm64` for this trial; Ruff checks and all seven
local archive-integrity tests passed. Following the blocked attempt, Linux
ARM64 was deferred and removed from the allowed packaging targets. The current
scope remains macOS ARM64, Linux AMD64 and Windows AMD64.

The API schema exposes `architecture: arm64`, but the current Python and Go
`avr vm create` commands do not expose an architecture selector. For this
trial, AVR's existing Python API client created two successive dedicated
Linux ARM64 VMs (4 vCPU, 15 GiB RAM, 150 GB disk, one-hour TTL). SSH and deletion
used `avr vm`.

Both VMs reached RUNNING, but strict SSH host-key verification failed before
any source transfer or build. Refreshing API metadata did not fix the first
VM, and one fresh replacement reproduced the problem:

| VM | API-advertised SHA-256 fingerprint | SSH-presented SHA-256 fingerprint |
| --- | --- | --- |
| `cvm-01a18e31f4c0731aa33d93022a6adbf4` | `0Cx9gGgxRC0VIx55WznXWXUty9r2OK6Ug9INFfrIiT8` | `Tczkp+DmxebfHTWx06wZdSpyEve5m71dAvyT2IYDmgE` |
| `cvm-01a18e33e2ef7e38b2d3b43ecee75736` | `0Cx9gGgxRC0VIx55WznXWXUty9r2OK6Ug9INFfrIiT8` | `2/FD4EA1vt+v+I8IEgfP+gt2HlqZzaCyELv151a5vHo` |

The API advertised the same key for both guests while the presented keys
differed from it and from each other. That observation does not establish
the underlying cause. No observed key was trusted as a replacement, host-key
verification was not bypassed, and no ARM build or archive validation ran.

Both temporary VMs were deleted. Evidence and the prepared source snapshot
are retained under `build/go/prod-vm-arm64-trial/`, including
`results.json`, `preflight.log`, `retry-preflight.log`, and `cleanup.json`.
Before revisiting Linux ARM64, resolve the production VM key-reporting/access
mismatch, restore the packaging target, and rerun native validation.

## CI artifact round trip

After uploading the three native packages, the reusable workflow downloads
each artifact on a fresh runner of the matching platform. It repeats archive
and executable validation, checks the target and exact workflow commit, and
requires a clean source tree. The resulting reports are uploaded separately
as `avr-go-download-validation-TARGET`. This tests the downloaded bytes and
their native execution without rebuilding the executable on the validation
runner. Neither job publishes a release.

## CI portability findings — 2026-10-30

The first draft-PR runs exposed host-specific fixture assumptions. Windows now
uses LF checkouts, explicit UTF-8 metadata, and native fixture seeds that do not
require POSIX PTYs. The SSH test checks Unix mode bits only on Unix, while every
platform still validates pinned host contents, arguments, and cleanup. The HTTP
timing test delays the first body read, so a busy runner cannot consume its
server-side sleep before recording response headers.

A macOS traceback located the Python login stall in
`HTTPServer.server_bind -> socket.getfqdn`. The login oracle wrapper supplies
`localhost` only for loopback reverse-name lookups. It delegates all other names
to the normal resolver, and the pinned Python code still binds a real socket,
handles callback HTTP, and writes credentials. Focused tests cover this boundary;
all 67 auth parity scenarios pass locally. This fixture does not establish that
stock Python login avoids reverse-DNS delays on hosted macOS.

Windows parity invocations use `--json=*` instead of separate `--json` and
`*` arguments: Click otherwise glob-expands the wildcard into local filenames
before parsing it. Both executables receive the same equivalent option. The
capture helper sends UTF-8 stdin bytes directly, preventing subprocess text
mode from converting LF Git-protocol fixtures into CRLF; output is decoded as
UTF-8 with normal terminal newline normalization. These boundaries have focused
tests. Go does not emulate Click's Windows filesystem glob expansion.
