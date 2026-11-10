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
`_AVR_COMPLETE` protocol.
Help/completion do not load credentials or call the API.

Native Go regression tests cover command help, styles, sanitization, completion
and reference generation. `make go-integration` additionally exercises installed
shell completion and bundled README discovery. Migration-only Python comparison
tools were retired after validating the transition; their code and results remain
in Git history at `f5688c1`. New behavior is tested against the native contract.

`make go-docs` renders Markdown, JSON, Starlight and man pages with the native
binary and passes the README explicitly. A packaged binary looks for its optional
README beside the executable. Documentation commands generate the public reference.

Generated references omit the release version so version-only releases do not
rewrite documentation. Use `avr --version` to identify an installed build;
release archives and package manifests retain their versions.
