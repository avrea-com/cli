"""Customer-facing Avrea CLI.

``LAZY_COMMANDS`` and ``ALIASES`` are the public command surface; downstream
CLIs (e.g. avr-admin) import these to inherit the full set rather than
maintaining a parallel whitelist that drifts when new commands ship.

Command modules are registered lazily — none of them import at module load
time. ``--help`` renders from the cached ``short_help`` strings; the actual
implementation modules import only when the user invokes the command.
``tests/test_lazy_registry.py`` keeps the cached strings honest.
"""

from avrea_cli import __version__
from avrea_cli.click_ext import AliasGroup
from avrea_cli.version import IS_RELEASE_BUILD
import click
import os
import sys

_VERSION_MESSAGE = "%(prog)s version %(version)s" if IS_RELEASE_BUILD else "%(prog)s version %(version)s (development)"

# Each tuple: (name, import_path, attr, short_help_or_None, section_or_None).
# section=None marks hidden commands (no help row) — they remain invokable.
LazyCommandSpec = tuple[str, str, str, str | None, str | None]

LAZY_COMMANDS: tuple[LazyCommandSpec, ...] = (
    (
        "status",
        "avrea_cli.commands.status",
        "status",
        "Show recent runs, performance stats, and cache health.",
        "Core Commands",
    ),  # noqa: E501
    ("run", "avrea_cli.commands.run", "run", "View and manage GitHub workflow runs.", "Core Commands"),
    ("job", "avrea_cli.commands.job", "job", "Inspect Avrea job VMs (SSH, metrics, logs).", "Core Commands"),
    ("workflow", "avrea_cli.commands.workflow", "workflow", "List and view workflow definitions.", "Core Commands"),
    ("cache", "avrea_cli.commands.cache", "cache", "Inspect and manage the Avrea build cache.", "Core Commands"),
    ("log", "avrea_cli.commands.log", "log", "Search across runner execution logs.", "Core Commands"),
    ("auth", "avrea_cli.commands.auth_cmd", "auth_group", "Authenticate and manage credentials.", "Setup & Config"),
    ("config", "avrea_cli.commands.config_cmd", "config", "View and manage CLI configuration.", "Setup & Config"),
    (
        "settings",
        "avrea_cli.commands.settings",
        "settings",
        "View and toggle cache and runner settings.",
        "Setup & Config",
    ),  # noqa: E501
    (
        "firewall",
        "avrea_cli.firewall",
        "firewall",
        "Manage the egress firewall rule list for orgs and repositories.",
        "Setup & Config",
    ),  # noqa: E501
    ("billing", "avrea_cli.billing", "billing", "Manage billing, invoices, and payment methods.", "Setup & Config"),
    ("audit-events", "avrea_cli.audit", "audit_events", "View audit events for organization writes.", "Setup & Config"),
    ("repo", "avrea_cli.commands.repo", "repo", "List repositories connected to Avrea.", "Additional Commands"),
    ("org", "avrea_cli.commands.org", "org", "Manage organizations and installations.", "Additional Commands"),
    ("health", "avrea_cli.commands.health", "health", "Check Avrea platform status.", "Additional Commands"),
    ("login", "avrea_cli.commands.auth_cmd", "login_alias", None, None),
    ("logout", "avrea_cli.commands.auth_cmd", "logout_alias", None, None),
    ("internal", "avrea_cli.commands.internal", "internal", None, None),
)

ALIASES: dict[str, str] = {
    "jobs": "job",
    "repos": "repo",
    "orgs": "org",
    "logs": "log",
    "workflows": "workflow",
}

_KNOWN_DEBUG_CATEGORIES = frozenset({"api"})
# AVR_DEBUG accepts categories (`api`) or a generic truthy value (`1`, `true`,
# `yes`, `on`) that enables every category. The truthy form matches what most
# users reach for first; named categories let scripts opt into a subset.
_TRUTHY_DEBUG_VALUES = frozenset({"1", "true", "yes", "on"})

# Top-level Click options accepted anywhere in argv (Click natively only
# honors them before the subcommand path). Pre-shuffling argv side-steps
# that. All listed flags are bare — never consume a following token.
_GLOBAL_FLAGS_NO_VALUE = frozenset({"--verbose", "-v", "--no-color", "--links", "--no-links"})


def _hoist_global_flags(argv: list[str]) -> list[str]:
    """Return ``argv`` with any global flags moved to the front.

    Stops at ``--`` (POSIX end-of-options) so values that happen to look
    like a flag aren't reordered. Order among the hoisted flags is
    preserved so duplicates behave the same as before.
    """
    if not argv:
        return argv
    hoisted: list[str] = []
    rest: list[str] = []
    end_of_opts = False
    for arg in argv:
        if end_of_opts:
            rest.append(arg)
            continue
        if arg == "--":
            end_of_opts = True
            rest.append(arg)
            continue
        if arg in _GLOBAL_FLAGS_NO_VALUE:
            hoisted.append(arg)
        else:
            rest.append(arg)
    return hoisted + rest


def main() -> None:
    """Entry point: hoists global flags so they work after subcommands.

    Click only honors group options before the subcommand path. Reordering
    argv before Click parses lets `avr run list --verbose` work the same
    as `avr --verbose run list`."""
    sys.argv[1:] = _hoist_global_flags(sys.argv[1:])
    cli()


@click.group(
    cls=AliasGroup,
    aliases=ALIASES,
    invoke_without_command=True,
    context_settings={"help_option_names": ["-h", "--help"]},
)
@click.version_option(
    __version__,
    "--version",
    "-V",
    prog_name="avr",
    message=_VERSION_MESSAGE,
)
@click.option(
    "--no-color",
    is_flag=True,
    default=False,
    help="Disable colored output. Also honors NO_COLOR=1.",
)
@click.option(
    "--verbose",
    "-v",
    is_flag=True,
    default=False,
    help="Show debug information including HTTP requests.",
)
@click.option(
    "--links/--no-links",
    "links",
    default=True,
    show_default=True,
    envvar="AVR_LINKS",
    help="Make IDs clickable via OSC 8 hyperlinks. Auto-disabled off-TTY. Also honors AVR_LINKS=0.",
)
@click.pass_context
def cli(ctx, no_color: bool, verbose: bool, links: bool):
    """Avrea on the command line."""
    # Imported here (not at module load) so ``--help`` and shell completion
    # don't pay for the auth/config import chain — Click's ``--help`` is
    # eager and never invokes the group callback.
    from avrea_cli.api_client import ApiClient  # noqa: PLC0415
    from avrea_cli.config import CliConfig  # noqa: PLC0415

    ctx.ensure_object(dict)
    # NO_COLOR convention: any non-empty value disables color (https://no-color.org/).
    if no_color or os.environ.get("NO_COLOR"):
        ctx.color = False
    # OSC 8 hyperlinks: force-disabled off-TTY because click's strip_ansi
    # doesn't strip OSC sequences, so they'd leak as visible garbage to pipes.
    ctx.obj["links_enabled"] = links and sys.stdout.isatty()
    raw = os.environ.get("AVR_DEBUG", "").strip().lower()
    debug_categories: set[str] = set()
    if raw:
        if raw in _TRUTHY_DEBUG_VALUES:
            debug_categories = set(_KNOWN_DEBUG_CATEGORIES)
        else:
            debug_categories = {c.strip() for c in raw.split(",") if c.strip()}
    if "api" in debug_categories:
        verbose = True
    unknown = debug_categories - _KNOWN_DEBUG_CATEGORIES
    if unknown:
        click.echo(
            f"Warning: AVR_DEBUG ignored unknown categor{'ies' if len(unknown) > 1 else 'y'}: "
            f"{', '.join(sorted(unknown))}. Known: {', '.join(sorted(_KNOWN_DEBUG_CATEGORIES))}.",
            err=True,
        )
    ctx.obj["config"] = CliConfig()
    ctx.obj["client"] = ApiClient(ctx.obj["config"], verbose=verbose)

    # Bare `avr` (no subcommand): greet a never-authenticated user with an
    # actionable next step instead of dumping the full --help block. Already
    # logged-in users still get the help screen.
    if ctx.invoked_subcommand is None:
        if not ctx.obj["config"].auth_token:
            check = click.style("→", fg="cyan")
            click.echo("Welcome to Avrea CLI.\n")
            click.echo(f"  {check} Run {click.style('avr auth login', bold=True)} to sign in.")
            click.echo(f"  {check} Run {click.style('avr --help', bold=True)} to see all commands.")
            return
        click.echo(ctx.get_help())


for _name, _import_path, _attr, _short_help, _section in LAZY_COMMANDS:
    if _section is not None:
        with cli.section(_section):
            cli.add_lazy_command(_name, _import_path, _attr, _short_help)
    else:
        cli.add_lazy_command(_name, _import_path, _attr, _short_help)


if __name__ == "__main__":
    main()
