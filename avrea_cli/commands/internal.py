"""Hidden ``avr internal`` command group.

Houses maintenance commands that ship with the CLI but aren't part of the
public surface — listed here so the published ``avr`` binary can dogfood
its own docs generator (``avr internal docs``) without polluting the
top-level help text.
"""

from avrea_cli.docs_gen import docs_command
import click


@click.group("internal", hidden=True)
def internal() -> None:
    """Maintenance commands (hidden)."""


internal.add_command(docs_command)
