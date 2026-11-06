# Native release preparation

The Go version is `internal/version/VERSION`, currently `0.7.0-rc.1`.
Python's `pyproject.toml` remains at 0.6.0 for the oracle and admin transition.
Native builds use Go 1.27.3 or newer and run with CGO disabled.

`go-package.yml` prepares these artifacts on all three native runners:

- Versioned archives, checksums, native wheels, and validation reports.
- A replacement `avr.rb` with actual archive checksums and Go completion paths.
- A checksum-pinned `mise.toml`, shell/PowerShell installers and version file.
- CycloneDX and SPDX SBOMs from the native distribution, plus `SHA256SUMS`.

Fresh-runner validation installs wheels through pip, uv and pipx and runs the
native installer against a loopback release fixture. The pip test upgrades
from the unchanged Python 0.6.0 wheel and rolls back. Neither path contacts
Avrea services or uses real credentials. Installer regression fixtures cover
upgrade, rollback, checksum failure, unmanaged destinations and paths with spaces.

Run the portable tests and native build locally:

```sh
python -m unittest scripts.test_go_package scripts.test_go_distribution -v
python scripts/go_package.py --out build/go/install-check
python -m scripts.go_distribution \
  --archive build/go/install-check/avr_0.7.0-rc.1_darwin_arm64.tar.gz \
  --out build/go/install-check
python -m scripts.validate_go_install --directory build/go/install-check
```

Substitute the archive name on Linux/Windows. The validator requires uv;
pipx checks run when pipx is available, and CI installs it explicitly.
Go and Python executable overrides are supported by the underlying scripts.

Before publishing:

1. Land the avrea-core admin dependency pin and update admin installation
   metadata. Keep its Python environment separate from the public Go CLI.
2. Require green full parity/race/lint checks and all native package/install
   validations for the exact release commit. Review generated notices/SBOMs.
3. Run the release candidate first. Confirm GitHub-backed mise installation,
   PyPI upgrade commands, credentials, completion and rollback on clean machines.
4. For stable 0.7.0, update the Go version and rebuild. Apply the generated
   stable `avr.rb` in avrea-com/homebrew-tap after the matching assets exist;
   test `brew install`, `brew upgrade avr`, and the formula test there.
5. With separate release authorization, set repository variable
   `AVR_NATIVE_RELEASE_ENABLED=true` and push the signed version tag. The gate
   is unset by default. A manual Release workflow run never publishes, even
   when the gate is enabled. Tag version must match `internal/version/VERSION`;
   both Python CI and the complete Go/parity/package workflow must pass.
   Publication uses the exact tested wheels and archives, without rebuilding.
   RC tags create prereleases and do not replace GitHub's latest stable release.
   Keep the `pypi` environment and Trusted Publisher configured for release.yml.
6. Publish only with separate release authorization. No release/tag is created
   by PR validation, and the tap is never edited automatically.

The PyPI wheel format places native binaries in `.data/scripts`; pip and uv
install those executables directly, with no Python launcher. See the
[wheel specification](https://packaging.python.org/en/latest/specifications/binary-distribution-format/).
The public command and the `avr-cli` alias are both included. Unsupported
platforms have no 0.7 wheel; the release has no fallback sdist.
