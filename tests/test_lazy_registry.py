"""Drift guard for ``LAZY_COMMANDS`` in ``avrea_cli.main``.

Each entry caches the command's ``short_help`` so ``--help`` can render
without importing the implementation module. If a command's docstring or
``short_help=`` decorator changes, the cached string in the registry rots
silently. This test imports each module, resolves the attribute, and
asserts the registry agrees with the live command.
"""

from avrea_cli.main import LAZY_COMMANDS
import importlib
import pytest


@pytest.mark.parametrize(
    ("name", "import_path", "attr", "short_help", "section"),
    LAZY_COMMANDS,
    ids=[spec[0] for spec in LAZY_COMMANDS],
)
def test_lazy_spec_matches_live_command(name, import_path, attr, short_help, section):
    module = importlib.import_module(import_path)
    cmd = getattr(module, attr)

    assert cmd.name == name, (
        f"LAZY_COMMANDS entry name {name!r} != live command name {cmd.name!r} ({import_path}:{attr})"
    )

    if section is None:
        # Hidden command — no help row, so short_help is unused. Live command
        # should also be hidden so a stray section=None on a visible command
        # doesn't silently disappear from --help.
        assert cmd.hidden, f"{name!r} has section=None but cmd.hidden is False"
        return

    assert not cmd.hidden, f"{name!r} has a section but cmd.hidden is True"
    live_short = cmd.get_short_help_str(limit=120)
    assert live_short == short_help, (
        f"LAZY_COMMANDS short_help for {name!r} is stale: registry={short_help!r} live={live_short!r}"
    )
