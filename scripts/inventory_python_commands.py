"""Export the complete Python command contract, including hidden commands.

This records the migration denominator; it does not claim behavior coverage.
Run from the repository root with the project's Python environment.
"""

from avrea_cli.main import ALIASES
from avrea_cli.main import cli
from functools import lru_cache
from pathlib import Path
from scripts.compare_auth import BASELINE
import argparse
import click
import json
import subprocess


def inventory(command=cli, path="avr"):
    ctx = click.Context(command, info_name=command.name)
    parameters = []
    for param in command.get_params(ctx):
        entry = {
            "name": param.name,
            "kind": "option" if isinstance(param, click.Option) else "argument",
            "opts": param.opts,
            "secondary_opts": param.secondary_opts,
            "required": param.required,
            "nargs": param.nargs,
            "multiple": param.multiple,
            "type": param.type.to_info_dict(),
            "default": param.default,
            "envvar": param.envvar,
        }
        if isinstance(param, click.Option):
            entry.update(is_flag=param.is_flag, hidden=param.hidden, count=param.count)
        parameters.append(entry)
    result = {"path": path, "hidden": command.hidden, "parameters": parameters}
    if isinstance(command, click.Group):
        result["commands"] = [
            inventory(command.get_command(ctx, name), path + " " + name) for name in command.list_commands(ctx)
        ]
    return result


def stable_value(value):
    if callable(value):
        return value.__module__ + "." + value.__qualname__
    return str(value)


@lru_cache
def contract():
    """Build the oracle from unchanged Python; never read a committed snapshot."""
    subprocess.run(
        ["git", "diff", "--exit-code", BASELINE, "--", "avrea_cli"],
        check=True,
        stdout=subprocess.DEVNULL,
    )
    return json.loads(
        json.dumps(
            {"baseline": BASELINE, "aliases": ALIASES, "root": inventory()},
            default=stable_value,
        )
    )


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, default=Path("build/go/python-command-contract.json"))
    args = parser.parse_args()
    payload = contract()
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    def count(node):
        return 1 + sum(count(child) for child in node.get("commands", []))

    print(f"Recorded {count(payload['root'])} command/group nodes in {args.out}")
