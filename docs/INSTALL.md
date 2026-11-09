# Install and update AVR

AVR **0.7.0 and later** use a native Go executable. Install from
[GitHub Releases](https://github.com/avrea-com/cli/releases), PyPI, Homebrew or
mise. The public command remains `avr`; Python is no longer needed at runtime.

Supported native targets: macOS 13+ on Apple Silicon, Linux AMD64/ARM64, and
Windows AMD64. Intel macOS builds are not included. PyPI
wheels support macOS 13+, glibc Linux AMD64/ARM64 and Windows AMD64. Alpine uses the standalone Linux binary and shell installer; install `curl` and
`ca-certificates` first. Alpine PyPI wheels and Intel macOS builds are not shipped.
On platforms without a compatible wheel, an unpinned PyPI upgrade may retain
Python 0.6.0. An exact request such as `uv tool install 'avr-cli==0.7.0'`
fails clearly if no compatible wheel exists. There is no fallback source distribution.

## Keep your existing installation method

| Installed with | Install | Update |
| --- | --- | --- |
| Homebrew | `brew install avrea-com/tap/avr` | `brew upgrade avr` |
| uv | `uv tool install avr-cli` | `uv tool upgrade avr-cli` |
| pipx | `pipx install avr-cli` | `pipx upgrade avr-cli` |
| pip, in an environment | `python -m pip install avr-cli` | `python -m pip install --upgrade avr-cli` |
| mise | `mise use -g github:avrea-com/cli@latest` | `mise upgrade github:avrea-com/cli` (change exact pins explicitly) |

The PyPI package contains native executables named `avr` and `avr-cli`.
Python is needed by the package manager, but is not launched when running
AVR. The wheels have no Python runtime dependencies and require Python 3.9+
for installation. `uvx avr-cli` remains available.

## mise

Use mise's built-in
[GitHub backend](https://mise.jdx.dev/dev-tools/backends/github.html):

```sh
mise use -g github:avrea-com/cli@latest
mise exec github:avrea-com/cli@latest -- avr --version
```

Omit `-g` to configure AVR in the current project's `mise.toml`. Project
configuration can override the global version. Update with
`mise upgrade github:avrea-com/cli`. For a reproducible installation, replace
`latest` with the desired release version; change that pin explicitly when
upgrading.

Version discovery may exclude very recent releases according to mise's
[minimum-release-age setting](https://mise.jdx.dev/configuration/settings.html#minimum_release_age).

If an installed `avr` is not found, compare `mise which avr` with `type -a avr`
and check your [mise shell setup](https://mise.jdx.dev/shell-setup.html).

Each release also includes `mise.toml` with an exact version, platform asset
names and checksums. Merge its `[tools]` entry into an existing project
configuration if you want these explicit download settings.

## Homebrew tap trust

The tap is updated separately from GitHub and PyPI. Use
`brew info avrea-com/tap/avr` to check its available version.

Installing with the full name, `brew install avrea-com/tap/avr`, trusts that
formula. If Homebrew ignores an existing untrusted tap during an upgrade,
trust only AVR and retry:

```sh
brew trust --formula avrea-com/tap/avr
brew upgrade avr
```

This is separate from mise activation. You do not need to trust the Avrea tap
when installing AVR through mise. See [Homebrew tap trust](https://docs.brew.sh/Tap-Trust).

## Native installation without Python

Download `install.sh` or `install.ps1` from the desired
[GitHub release](https://github.com/avrea-com/cli/releases),
inspect it, then run it. Installers fetch versioned archives and verify their
SHA-256 sidecars before changing the destination. They do not request root,
modify shell profiles, or overwrite an installation belonging to a package
manager. Check `command -v avr` (PowerShell: `Get-Command avr`) if an older
installation is earlier on PATH.

Linux or macOS:

```sh
sh install.sh --version 0.7.0 --bin-dir "$HOME/.local/bin"
```

Windows PowerShell:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1 -Version 0.7.0 -BinDir "$env:LOCALAPPDATA\Avrea\bin"
```

The execution-policy override applies only to this process; organization policy still takes precedence. Inspect the downloaded script before running it. Both Windows PowerShell 5.1 and PowerShell 7 are tested.

Omitting the version selects the latest stable release.
Re-running the installer upgrades its own installation;
passing an older version rolls it back. Add the printed directory to PATH.
An executable currently running on Windows may need to be closed first.

For manual installation, extract `avr_VERSION_TARGET.tar.gz` or `.zip`,
verify the checksum, and add its directory to PATH. Full archives also contain
completions, man pages, reference documentation and license notices. Installers
install only the executable. Generate completion source with
`_AVR_COMPLETE=bash_source avr`, `zsh_source`, or `fish_source`, and install it
using your shell's completion-directory conventions.

`avr vm bootstrap VM --install-avr` uses the same native shell installer to
install the release matching the local CLI's source version into the VM's
`~/.local/bin`. It needs curl, tar and SHA-256 tooling, but no Python. That
version must already have published release assets. The installer prints PATH
instructions and refuses to overwrite a package-managed installation; move or
remove that installation using its original package manager first.

## Credentials, rollback and admin

The command stays `avr`, using the existing configuration and credentials.
Installation does not log in, log out, or rewrite `hosts.json`.

Use your original package manager for rollback, for example
`uv tool install --force 'avr-cli==0.6.0'` or
`python -m pip install 'avr-cli==0.6.0'`. Python 0.6.0 requires Python 3.14+.
A native installer cannot install Python 0.6.0; use its former package manager.

`avr-admin` still imports Python `avrea_cli` and remains pinned to
`avr-cli==0.6.0` in avrea-core. Install admin and
public AVR in separate environments. Existing admin installations need to
consume the updated dependency metadata; an older unbounded requirement is
not changed retroactively. Native wheels intentionally omit `avrea_cli`.

## JSON and jq

AVR uses embedded gojq, like GitHub CLI. Object keys are sorted,
`keys_unsorted` is unavailable, and there is no system-jq fallback. When column
order matters, name fields explicitly: `.[] | [.status,.conclusion,.run_id] | @tsv`.

## Update notices

Successful interactive commands may print an update notice on stderr. Checks
use the public GitHub stable-release feed and a local 24-hour cache. They are
best effort, have a two-second network deadline, and never delay command
completion. No Avrea credentials are sent and no update is installed.
The attempt timestamp is saved before the request. Short commands may exit
before it finishes; that attempt still suppresses another check for 24 hours.

Notices and checks are disabled for help, version, completion, hidden commands,
structured output, redirected stdout/stderr, CI and automation. Set
`AVR_NO_UPDATE_NOTIFIER=1` to disable them explicitly. Notices suggest Homebrew,
uv, pipx or mise commands when their installation path is recognizable;
otherwise they link to these instructions. Custom installation directories may
require choosing the appropriate package manager yourself.
