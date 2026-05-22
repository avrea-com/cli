"""Field-selecting JSON output with optional jq post-filter.

The contract:

- ``--json`` with no argument prints the available fields and exits.
- ``--json a,b,c`` returns only those fields per record (Avrea-side schema
  decides the wire-name → API-path mapping).
- ``-q/--jq`` pipes the resulting JSON through the system ``jq``. We shell out
  rather than vendor a Python jq port so users get the same expression syntax
  they'd use against any other JSON CLI.
"""

from typing import Any
import click
import json
import subprocess
import sys


def json_options(func):
    """Decorator: add the standard ``--json`` and ``-q/--jq`` options.

    Identical contract across the CLI: comma-separated fields, ``*`` for
    all, ``?`` to list available, and an optional jq post-filter. Use this
    on commands that don't need any custom JSON wiring; commands with extra
    flags (like ``--web``) can still declare ``--json`` and ``--jq``
    inline if they prefer to control argument order."""
    func = click.option("-q", "--jq", "jq_expr", default=None, help="Filter --json output through a jq expression.")(
        func
    )
    func = click.option(
        "--json",
        "json_fields",
        default=None,
        help='Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.',
    )(func)
    return func


def make_schema(*fields: str, **aliases: str) -> dict[str, str]:
    """Build a wire-name → response-path schema.

    Positional names map 1:1 (wire-name == response path). Keyword arguments
    add or override entries where the path differs from the wire name. Lets
    callers write the common case (`make_schema("key", "value", "source")`)
    without spelling out identity mappings, while still supporting cross-key
    indirection (`make_schema("email", user_id="id")`).
    """
    schema: dict[str, str] = {f: f for f in fields}
    schema.update(aliases)
    return schema


def split_fields(raw: str, schema: dict[str, str]) -> list[str]:
    """Parse the comma-separated value of --json into a clean field list.
    `*` expands to every field defined in the schema."""
    if raw.strip() == "*":
        return sorted(schema)
    return [f.strip() for f in raw.split(",") if f.strip()]


def get_path(obj: Any, path: str) -> Any:
    """Walk a dotted path through nested dicts. Missing keys → None.

    Convention: callers that need to mix server fields with locally-derived
    values (see ``commands/auth_cmd.py``) prefix synthetic keys with
    ``_local:`` and place them at the top level of the record. The colon
    keeps them out of the dotted-path namespace and the prefix signals
    intent — don't introduce real API fields starting with ``_local:``."""
    for part in path.split("."):
        if not isinstance(obj, dict):
            return None
        obj = obj.get(part)
    return obj


def select_fields(records: list[dict[str, Any]], fields: list[str], schema: dict[str, str]) -> list[dict[str, Any]]:
    """Pluck the requested fields from each record using the schema's path
    mapping. Unknown fields raise a ClickException listing available ones."""
    unknown = [f for f in fields if f not in schema]
    if unknown:
        avail = ", ".join(sorted(schema))
        raise click.ClickException(f"Unknown JSON field(s): {', '.join(unknown)}. Available: {avail}")
    return [{f: get_path(record, schema[f]) for f in fields} for record in records]


def handle_json_meta(json_fields: str | None, jq_expr: str | None, schema: dict[str, str]) -> bool:
    """Validate ``--jq`` requires ``--json`` and handle ``--json '?'`` discovery.

    Returns True when the caller should ``return`` immediately (the ``?``
    discovery path printed and is done). Otherwise returns False so the
    command proceeds. Centralizes a preamble that was duplicated at every
    ``--json``-bearing command's entry point."""
    if jq_expr and json_fields is None:
        raise click.UsageError("--jq requires --json")
    if json_fields == "?":
        print_available_fields(schema)
        return True
    return False


def reject_web_with_json(json_fields: str | None, web: bool) -> None:
    """Refuse `--web` + `--json` together. Either the user wants a browser
    (--web) or machine-readable output (--json); doing both runs the action
    and produces output that nobody asked for."""
    if web and json_fields is not None:
        raise click.UsageError("--web and --json are mutually exclusive.")


def print_available_fields(schema: dict[str, str]) -> None:
    """``--json ?`` was passed — print what fields the caller can pick.

    The list goes to stdout: it *is* the answer to a structured query, so
    `avr ... --json '?' | grep workflow` (or feeding into a TUI picker)
    should just work."""
    click.echo("Specify one or more comma-separated fields for `--json`:")
    for name in sorted(schema):
        click.echo(f"  {name}")


def filter_with_jq(data: Any, expr: str) -> str:
    """Pipe ``data`` through `jq -r <expr>` and return jq's stdout. Raises a
    ClickException if jq isn't on PATH or the expression is invalid.

    ``-r`` (raw) makes string outputs unquoted (``run-abc`` instead of
    ``"run-abc"``). Numbers, booleans, objects and arrays are unaffected.
    Callers who want a quoted string can wrap their expression in ``tojson``.
    """
    try:
        result = subprocess.run(
            ["jq", "-r", expr],
            input=json.dumps(data),
            capture_output=True,
            text=True,
            timeout=10,
        )
    except FileNotFoundError:
        raise click.ClickException(
            "`jq` is required for --jq but was not found on PATH. Install from https://stedolan.github.io/jq/"
        ) from None
    if result.returncode != 0:
        raise click.ClickException(f"jq error: {result.stderr.strip()}")
    return result.stdout


def emit_json(records: list[dict[str, Any]], fields: list[str], schema: dict[str, str], jq_expr: str | None) -> None:
    """Apply the field projection, optionally pipe through jq, write to stdout."""
    projected = select_fields(records, fields, schema)
    _write(projected, jq_expr)


def emit_json_record(
    record: dict[str, Any],
    fields: list[str],
    schema: dict[str, str],
    jq_expr: str | None,
) -> None:
    """Single-record variant for `view` commands. Output is a JSON object,
    not an array — matches what users expect from `<thing> view --json`."""
    projected = select_fields([record], fields, schema)[0]
    _write(projected, jq_expr)


def _write(data: Any, jq_expr: str | None) -> None:
    if jq_expr:
        sys.stdout.write(filter_with_jq(data, jq_expr))
    else:
        sys.stdout.write(json.dumps(data, indent=2, default=str))
        sys.stdout.write("\n")
