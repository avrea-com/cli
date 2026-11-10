# Native release preparation

The Go version is defined in `internal/version/VERSION`.
`pyproject.toml` configures development tools only; the retired Python CLI is
available as the published 0.6.0 package.
Native builds use Go 1.27.3 or newer and run with CGO disabled.

## Build and validate packages

Run `make go-package` to build the current worktree into `build/go/packages/`.
Alternatively, run `python3 scripts/go_package.py --go /path/to/go` with
Python 3.14. Use `--out DIRECTORY` to change the destination.
The default version comes from `internal/version/VERSION`; `--version VERSION`
overrides the package label. Uncommitted sources carry a `.dirty` revision.

Archives are `avr_VERSION_TARGET.tar.gz` on Unix and `.zip` on Windows.
They include the executable, main README, license notices, completion sources,
reference docs, man pages and a per-file SHA-256 manifest. Extract the whole
archive into a separate directory and run `./avr --help` (`.\avr.exe --help`
in PowerShell). Building and validating do not replace your installed command.

Revalidate an archive with its adjacent checksum sidecar:

```sh
release_version=$(cat internal/version/VERSION)
python3 scripts/go_package.py --verify "build/go/packages/avr_${release_version}_darwin_arm64.tar.gz"
```

Validation checks safe extraction, checksums, payload files, embedded build
settings, bundled README discovery, help, local config, completions and jq.
Executable checks use an isolated home, empty PATH and a loopback API fixture;
they do not use real credentials or Avrea services. Checksums detect corruption;
published release provenance is verified separately as described in the README.

## CI and installation validation

`go-package.yml` prepares these artifacts on macOS ARM64, Linux AMD64/ARM64
and Windows AMD64 runners:

- Versioned archives, checksums, native wheels, and validation reports.
- A replacement `avr.rb` with actual archive checksums and Go completion paths.
- A checksum-pinned `mise.toml`, shell/PowerShell installers and version file.
- CycloneDX and SPDX SBOMs from the native distribution, plus `SHA256SUMS`.

The reusable workflow runs from native CI or manual dispatch. Fresh runners
download and validate the exact artifacts without rebuilding, checking the
manifest against the workflow commit. Manual package validation does not run
the full regression suite or publish a release.

Fresh-runner validation installs wheels through pip, uv and pipx and runs the
native installer against a loopback release fixture. The pip test upgrades
from the published, checksum-pinned Python 0.6.0 wheel and rolls back. Neither path contacts
Avrea services or uses real credentials. Installer regression fixtures cover
upgrade, rollback, checksum failure, unmanaged destinations and paths with spaces.
Linux AMD64 and ARM64 additionally run the installer tests and binary smoke
checks in Alpine. Alpine uses standalone binaries, not PyPI wheels.

Run the portable tests and native build locally:

```sh
release_version=$(cat internal/version/VERSION)
python -m unittest scripts.test_go_package scripts.test_go_distribution -v
python scripts/go_package.py --out build/go/install-check
python -m scripts.go_distribution \
  --archive "build/go/install-check/avr_${release_version}_darwin_arm64.tar.gz" \
  --out build/go/install-check
python -m scripts.validate_go_install --directory build/go/install-check
```

Substitute the archive name on Linux/Windows. The validator requires uv;
pipx checks run when pipx is available, and CI installs it explicitly.
Go and Python executable overrides are supported by the underlying scripts.

## Publishing

Before publishing:

1. Require green native regression/race/lint checks and all native package/install
   validations for the exact release commit. Review generated notices/SBOMs.
   Run `make go-api-check AVREA_CORE=/path/to/avrea-core` against current core
   OpenAPI; see [the API check's coverage and limits](../api/README.md#request-compatibility-check).
2. Validate the CI artifacts before publishing a stable release; no published RC is
   required. Exercise installers, package upgrades, credentials, completion and
   rollback on clean machines. Confirm GitHub-backed mise installation once
   the separately authorized release assets exist.
3. Apply the generated
   stable `avr.rb` in avrea-com/homebrew-tap after the matching assets exist;
   test `brew install`, `brew upgrade avr`, and the formula test there.
4. With separate release authorization, set repository variable
   `AVR_NATIVE_RELEASE_ENABLED=true` and push the signed version tag. The gate
   is unset by default. A manual Release workflow run never publishes, even
   when the gate is enabled. Tag version must match `internal/version/VERSION`;
   both tooling CI and the complete Go/package workflow must pass.
   Publication uses the exact tested wheels and archives, without rebuilding.
   RC tags create prereleases and do not replace GitHub's latest stable release.
   Keep the `pypi` environment and Trusted Publisher configured for release.yml.
5. Publish only with separate release authorization. No release/tag is created
   by PR validation, and the tap is never edited automatically.

The PyPI wheel format places native binaries in `.data/scripts`; pip and uv
install those executables directly, with no Python launcher. See the
[wheel specification](https://packaging.python.org/en/latest/specifications/binary-distribution-format/).
The public command and the `avr-cli` alias are both included. Unsupported
platforms have no 0.7 wheel; the release has no fallback sdist.

Publication is resumable: stage GitHub assets in a draft, verify existing asset
and PyPI wheel digests, upload missing wheels, then publish the GitHub release.
A mismatch stops recovery; reruns never replace existing release bytes.
`--verify-tag` checks tag existence, not its cryptographic signature.
