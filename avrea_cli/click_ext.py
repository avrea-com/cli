"""Click extensions for sectioned help formatting and command aliases."""

from contextlib import contextmanager
import click
import importlib

SECTION_ORDER = ["Core Commands", "Setup & Config", "Additional Commands"]

LEARN_MORE = 'Use "avr <command> <subcommand> --help" for more information about a command.\nRead the docs at https://docs.avrea.com/cli'


def _heading(text: str) -> str:
    """Bold-uppercase section heading. ANSI codes are stripped automatically
    by click.echo when stdout is not a TTY (e.g., piped to less)."""
    return click.style(text.upper(), bold=True)


def _write_section(formatter: click.HelpFormatter, heading: str, body_fn) -> None:
    """Write a section with a bold uppercase heading (no trailing colon)."""
    formatter.write(f"\n{_heading(heading)}\n")
    formatter.indent()
    body_fn()
    formatter.dedent()


def _write_text_section(formatter: click.HelpFormatter, heading: str, text: str) -> None:
    """Write a section with plain text, properly indented."""
    indent = " " * (formatter.current_indent + 2)
    formatter.write(f"\n{_heading(heading)}\n")
    for line in text.splitlines():
        formatter.write(f"{indent}{line}\n")


class LazyGroup(click.Group):
    """``click.Group`` with lazy command registration.

    ``add_lazy_command`` records ``name -> (import_path, attr, short_help)`` so
    ``--help`` can render the row from cached metadata without importing the
    implementation module. The module is imported on first ``get_command``
    (i.e. when a user actually invokes the command), at which point the lazy
    spec is consumed and the resolved Click command is registered eagerly.

    Aliases (``AliasGroup``) sit above this layer and rewrite argv to the
    canonical name before lookup, so they work transparently with lazy entries.
    """

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        # name -> (import_path, attr, short_help_or_None).
        self._lazy_specs: dict[str, tuple[str, str, str | None]] = {}
        # name -> section_or_None. Hidden commands carry None.
        self._lazy_section: dict[str, str | None] = {}

    def add_lazy_command(
        self,
        name: str,
        import_path: str,
        attr: str,
        short_help: str | None,
        section: str | None = None,
    ) -> None:
        """Register a command without importing its module.

        ``short_help`` is the row text shown in ``--help``; pass ``None`` for
        hidden commands (they're listed for resolution but skipped from help).
        ``section`` defaults to the group's active ``with section(...)`` block,
        matching ``add_command``'s auto-tagging.
        """
        # An eager registration with the same name takes precedence — drop it
        # so this lazy registration wins. Mirrors ``add_command``'s
        # replace-by-name semantics.
        self.commands.pop(name, None)
        self._lazy_specs[name] = (import_path, attr, short_help)
        active = section if section is not None else getattr(self, "_current_section", None)
        self._lazy_section[name] = active

    def add_command(self, cmd, name=None):
        # Eager registration wins over any pending lazy registration with the
        # same name; drop the lazy entry so help/dispatch see only the new one.
        resolved = name or cmd.name
        self._lazy_specs.pop(resolved, None)
        self._lazy_section.pop(resolved, None)
        super().add_command(cmd, name)

    def list_commands(self, ctx):
        return sorted({*super().list_commands(ctx), *self._lazy_specs})

    def get_command(self, ctx, cmd_name):
        spec = self._lazy_specs.pop(cmd_name, None)
        if spec is None:
            return super().get_command(ctx, cmd_name)
        import_path, attr, _ = spec
        section = self._lazy_section.pop(cmd_name, None)
        module = importlib.import_module(import_path)
        cmd = getattr(module, attr)
        # Skip our own ``add_command`` (it would just no-op the lazy popping
        # we already did) and bypass any section auto-tagging by going
        # straight to ``click.Group`` — we apply the cached section
        # explicitly so it survives the load.
        click.Group.add_command(self, cmd, cmd_name)
        if section is not None:
            cmd.help_section = section
        return cmd


class GhHelpMixin:
    """Mixin that reformats Click help output with bold uppercase sections,
    a USAGE/CORE COMMANDS/SETUP & CONFIG/ADDITIONAL COMMANDS layout, and a
    LEARN MORE footer.

    Must be mixed with click.BaseCommand or a subclass (Group, Command).
    """

    # The mixin accesses attributes from click.BaseCommand / click.Group.
    # Type checkers can't see these through the mixin, so we annotate access
    # points with type: ignore[attr-defined].

    def format_help(self, ctx, formatter):
        help_text = self.help  # type: ignore[attr-defined]
        if help_text:
            formatter.write(f"{help_text}\n")

        pieces = self.collect_usage_pieces(ctx)  # type: ignore[attr-defined]
        usage_line = f"{ctx.command_path} {' '.join(pieces)}"
        _write_text_section(formatter, "USAGE", usage_line)

        self.format_commands(ctx, formatter)  # type: ignore[attr-defined]
        self.format_options(ctx, formatter)

        _write_text_section(formatter, "LEARN MORE", LEARN_MORE)

    def format_options(self, ctx, formatter):
        """Render options as FLAGS / INHERITED FLAGS."""
        opts = []
        for param in self.get_params(ctx):  # type: ignore[attr-defined]
            rv = param.get_help_record(ctx)
            if rv is not None:
                opts.append(rv)

        if opts:
            inherited = [(n, h) for n, h in opts if n.strip().startswith("--help")]
            own = [(n, h) for n, h in opts if not n.strip().startswith("--help")]

            if own:
                _write_section(formatter, "FLAGS", lambda: formatter.write_dl(own))

            if inherited:
                _write_section(formatter, "INHERITED FLAGS", lambda: formatter.write_dl(inherited))


class AliasGroup(GhHelpMixin, LazyGroup):
    """Root CLI group with sectioned help, plural-to-singular aliases, and grouped commands.

    ``section_order`` is the list of section headings rendered, top to bottom.
    Commands tag themselves with ``cmd.help_section = "<heading>"``; commands
    without a tag are silently omitted from --help (so registering a new
    command without picking a section is loud — you notice immediately).
    Defaults to the public-CLI section list; downstream CLIs (e.g. avr-admin)
    can pass their own.
    """

    def __init__(self, *args, aliases=None, section_order=None, **kwargs):
        super().__init__(*args, **kwargs)
        self._aliases = aliases or {}
        self._section_order = list(section_order) if section_order is not None else SECTION_ORDER
        self._current_section: str | None = None

    @contextmanager
    def section(self, name: str):
        """Tag every command registered inside the ``with`` block with ``name``.

        Lets the caller register commands in batches without re-listing the
        names afterwards just to assign ``help_section``::

            with cli.section("Public Commands"):
                cli.add_command(auth_group, name="auth")
                cli.add_command(jobs)

            with cli.section("Admin Commands"):
                cli.add_command(ai)

        Nests cleanly — outer section is restored on exit.
        """
        prev = self._current_section
        self._current_section = name
        try:
            yield
        finally:
            self._current_section = prev

    def add_command(self, cmd, name=None):
        super().add_command(cmd, name)
        if self._current_section is not None:
            # Auto-tag with the active section. We always overwrite — when
            # the same command object is registered to two CLIs (avr-admin
            # inheriting from avr) each registration's section context wins
            # for its own group. Different processes anyway, so cross-CLI
            # state collisions aren't a real concern.
            cmd.help_section = self._current_section

    def get_command(self, ctx, cmd_name):
        rv = super().get_command(ctx, cmd_name)
        if rv is not None:
            return rv
        canonical = self._aliases.get(cmd_name)
        if canonical:
            return super().get_command(ctx, canonical)
        return None

    def resolve_command(self, ctx, args):
        cmd_name = args[0] if args else None
        if cmd_name in self._aliases:
            args = [self._aliases[cmd_name], *args[1:]]
        return super().resolve_command(ctx, args)

    def format_commands(self, ctx, formatter):
        commands_by_section: dict[str, list[tuple[str, str]]] = {}
        for name in self.list_commands(ctx):
            # Lazy entries render straight from cached metadata — never import.
            if name in self._lazy_specs:
                short = self._lazy_specs[name][2]
                section = self._lazy_section.get(name)
                if short is None or section is None:
                    continue
            else:
                cmd = self.get_command(ctx, name)
                if cmd is None or cmd.hidden:
                    continue
                section = getattr(cmd, "help_section", None)
                if section is None:
                    continue
                short = cmd.get_short_help_str(limit=formatter.width)
            commands_by_section.setdefault(section, []).append((name, short))

        for section in self._section_order:
            cmds = commands_by_section.get(section)
            if not cmds:
                continue
            rows = [(f"{name}:", short) for name, short in cmds]
            _write_section(formatter, section, lambda r=rows: formatter.write_dl(r))


class GhGroup(GhHelpMixin, LazyGroup):
    """Command group with sectioned help (no aliases, no command-grouping)."""

    def format_commands(self, ctx, formatter):
        rows: list[tuple[str, str]] = []
        for name in self.list_commands(ctx):
            if name in self._lazy_specs:
                short = self._lazy_specs[name][2]
                if short is None:
                    continue
                rows.append((f"{name}:", short))
                continue
            cmd = self.get_command(ctx, name)
            if cmd is None or cmd.hidden:
                continue
            rows.append((f"{name}:", cmd.get_short_help_str(limit=formatter.width)))

        if rows:
            _write_section(formatter, "AVAILABLE COMMANDS", lambda: formatter.write_dl(rows))
