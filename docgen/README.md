# Native help, completion and reference generation

Go owns command descriptions in `commands/*_description.go`, beside each command
family. A description defines prose, examples, semantic parameter names/types,
choices, declaration order and presentation details once. `docgen.Parameter`
shares that information between reference documentation, terminal help and shell
completion. There is no embedded Python metadata snapshot or runtime Python.

The live Cobra registry remains authoritative for command/flag membership,
aliases, arity, repeatability and defaults. `commands.Description` supplies the
additional semantic details lazily for the requested command. `commands.Documentation`
walks the full tree only when generating reference files. Keep descriptions and
command implementations together when adding or changing a command.

The existing native formatters retain Python's help layout, wrapping, colors,
links and section ordering. Bash/Zsh/Fish completion keeps the existing
`_AVR_COMPLETE` protocol; composed admin trees also accept `_AVR_ADMIN_COMPLETE`.
Help/completion do not load credentials or call the API.

`make go-command-check` generates its Python inventory under ignored `build/`
and checks the live Go registry. Differential help/completion/documentation
suites obtain expected output and metadata independently from Python, rather
than using Go descriptions as their expected values. Python application source
is unchanged. The suites check every command, help flags, aliases, bare groups,
terminal styles, representative widths, completion suggestions and all generated
reference formats.

`make go-docs` renders Markdown, JSON, Starlight and man pages with the native
binary and passes the README explicitly. A packaged binary looks for its optional
README beside the executable. Both public and composed admin documentation
commands generate the public reference.
