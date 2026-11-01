# Native reference generation

The package renders the existing public reference schema and Markdown,
Starlight and man-page layouts. It uses the live Cobra tree for command and
flag membership, aliases, arity, repeatability and defaults. It retains the
migration's documentation prose, examples, parameter names, choices, environment
annotations and semantic type labels in `metadata.json`; those details cannot
all be recovered from Cobra's string flags and manual validators.

During the Python migration, reproduce that input with:

```sh
uv run --extra dev python -m scripts.generate_go_doc_metadata
make go-command-check
```

The metadata is migrated from this repository's pinned Python implementation,
not from an external CLI. Changes to prose, choices or semantic parameter
metadata need an explicit metadata update together with the corresponding
command change. The pinned-input drift check is a migration gate, not a
requirement for a future Go-only development workflow.

The compiled command needs no Python installation. `make go-docs` renders with
the native binary and passes the repository README explicitly. A packaged
binary looks for its optional default README beside the executable. Both avr
and avr-admin generate the public avr reference, as the existing Python
composition does.

Executable parity compares every generated file to the Python oracle. It
compares JSON by structure, and permits the Starlight comment to name the
command tree without the old Click implementation name. Other page contents
are compared after normalizing platform newlines.

Terminal help uses the same migrated input for prose, parameter labels and
section ordering, and the live Go registry for visible commands and flags.
Metadata is loaded lazily when help is requested. The native formatter retains
the Python layout, wrapping and styles; no Python process is started at runtime.
The executable help suite checks every command and both help flags, plus bare
groups, aliases, welcome text, terminal colors and representative widths.

Completion uses the same lazy metadata for parameter types and short
descriptions. It resolves the live command/flag registry without callbacks,
credential loading or API requests. The existing _AVR_COMPLETE protocol and
Bash/Zsh/Fish setup commands remain supported; avr-admin also accepts
_AVR_ADMIN_COMPLETE. Setup scripts are native templates, and shell integration
tests exercise registration, first-use autoloading and actual suggestions.
