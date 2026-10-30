"""Export the complete Python command contract, including hidden commands.

This records the migration denominator; it does not claim behavior coverage.
Run from the repository root with the project's Python environment.
"""

from avrea_cli.main import ALIASES
from avrea_cli.main import cli
from pathlib import Path
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


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    subprocess.run(
        ["git", "diff", "--exit-code", "96d1ccb987032f35b10c7bb0b9c90816da1323a0", "--", "avrea_cli"],
        check=True,
        stdout=subprocess.DEVNULL,
    )

    payload = {"baseline": "96d1ccb987032f35b10c7bb0b9c90816da1323a0", "aliases": ALIASES, "root": inventory()}
    target = Path("docs/go-python-command-contract.json")
    rendered = json.dumps(payload, indent=2, sort_keys=True, default=stable_value) + "\n"
    if args.check:
        if target.read_text() != rendered:
            raise SystemExit("Python command contract is stale; regenerate it.")
    else:
        target.write_text(rendered)

    def count(node):
        return 1 + sum(count(child) for child in node.get("commands", []))

    print(f"Recorded {count(payload['root'])} command/group nodes in {target}")
