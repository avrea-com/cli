# AVR Go preview package

This package contains the native public AVR CLI. Python and jq are not required
at runtime. Git and SSH workflows still need the corresponding system tools.

Extract the whole archive into a separate directory. Run `./avr --version`
(`./avr.exe --version` in PowerShell) and `./avr --help` from that directory.
The executable is named `avr` so shell completions match the command.
Nothing is installed automatically. Keep your existing AVR installation until
you choose to replace it. Normal authenticated commands use your existing AVR
configuration; package validation uses a separate temporary home.

Contents: executable, license, third-party notices, Bash/Zsh/Fish completion
sources, command reference, man pages, and a manifest recording source revision,
dirty-tree status, Go version, target, and SHA-256 of every payload file.
The adjacent archive checksum detects corruption; it is not a signature.

For Bash, add this directory to PATH and source `completions/avr.bash`.
For Zsh, add this directory to PATH and source `completions/_avr` after compinit.
For Fish, add this directory to PATH and source `completions/avr.fish`.
Remove that PATH entry and directory to stop using the preview.
