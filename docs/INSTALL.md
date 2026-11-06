# Install and update AVR

The native Go transition targets **0.7.0**, starting with **0.7.0-rc.1**.
These instructions describe the prepared release paths. Until that release
is published, normal package-manager installs still obtain Python 0.6.0.
PR artifacts can be downloaded and tested without changing your current `avr`.

Supported native targets: macOS 13+ on Apple Silicon, Linux AMD64, and
Windows AMD64. Linux ARM64 and Intel macOS builds are not included. PyPI
wheels support macOS 13+, glibc Linux AMD64 and Windows AMD64. There is no
source distribution or automatic Python fallback for an unsupported platform;
pin `avr-cli==0.6.0` there until a native build is available.

## Keep your existing installation method

| Installed with | Install | Update |
| --- | --- | --- |
| Homebrew | `brew install avrea-com/tap/avr` | `brew upgrade avr` |
| uv | `uv tool install avr-cli` | `uv tool upgrade avr-cli` |
| pipx | `pipx install avr-cli` | `pipx upgrade avr-cli` |
| pip, in an environment | `python -m pip install avr-cli` | `python -m pip install --upgrade avr-cli` |
| mise | `mise use -g github:avrea-com/cli@0.7.0` | Update your exact pin, or use `mise upgrade github:avrea-com/cli` with a floating version |

The PyPI package contains native executables named `avr` and `avr-cli`.
Python is needed by the package manager, but is not launched when running
AVR. The wheels have no Python runtime dependencies and require Python 3.9+
for installation. `uvx avr-cli` remains available.

To opt into the release candidate explicitly:

```sh
uv tool install 'avr-cli==0.7.0rc1'
# Or, in a pip environment:
python -m pip install 'avr-cli==0.7.0rc1'
```

A release candidate does not become the stable update channel. `0.7.0rc1`
is the PyPI spelling of GitHub/native version `0.7.0-rc.1`.

For mise, the release includes `mise.toml` with an exact version, platform
asset names and checksums. Its built-in GitHub backend is sufficient; no AVR
plugin is required. Merge the `[tools]` entry into your existing configuration.
The GitHub-backed install can be tested end to end only after release assets
exist. [mise backend documentation](https://mise.jdx.dev/dev-tools/backends/github.html).

## Native installation without Python

Download `install.sh` or `install.ps1` from the desired GitHub release,
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
.\install.ps1 -Version 0.7.0 -BinDir "$env:LOCALAPPDATA\Avrea\bin"
```

Omitting the version selects the latest stable release, once a native stable
release exists. Re-running the installer upgrades its own installation;
passing an older version rolls it back. Add the printed directory to PATH.
An executable currently running on Windows may need to be closed first.

For manual installation, extract `avr_VERSION_TARGET.tar.gz` or `.zip`,
verify the checksum, and add its directory to PATH. Full archives also contain
completions, man pages, reference documentation and license notices. Installers
install only the executable. Generate completion source with
`_AVR_COMPLETE=bash_source avr`, `zsh_source`, or `fish_source`, and install it
using your shell's completion-directory conventions.

## Credentials, rollback and admin

The command stays `avr`, using the existing configuration and credentials.
Installation does not log in, log out, or rewrite `hosts.json`.

Use your original package manager for rollback, for example
`uv tool install --force 'avr-cli==0.6.0'` or
`python -m pip install 'avr-cli==0.6.0'`. Python 0.6.0 requires Python 3.14+.
A native installer cannot install Python 0.6.0; use its former package manager.

`avr-admin` still imports Python `avrea_cli`. Its dependency must be pinned to
`avr-cli==0.6.0` in avrea-core before the native PyPI release. Install admin and
public AVR in separate environments. Existing admin installations need to
consume the updated dependency metadata; an older unbounded requirement is
not changed retroactively. Native wheels intentionally omit `avrea_cli`.

## Update notices

Successful interactive commands may print an update notice on stderr. Checks
use the public GitHub stable-release feed and a local 24-hour cache. They are
best effort, have a two-second network deadline, and never delay command
completion. No Avrea credentials are sent and no update is installed.
Short commands may exit before the asynchronous check finishes.

Notices and checks are disabled for help, version, completion, hidden commands,
structured output, redirected stdout/stderr, CI and automation. Set
`AVR_NO_UPDATE_NOTIFIER=1` to disable them explicitly. Notices suggest Homebrew,
uv, pipx or mise commands when their installation path is recognizable;
otherwise they link to these instructions. Custom installation directories may
require choosing the appropriate package manager yourself.
