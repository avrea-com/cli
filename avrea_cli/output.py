"""Shared output formatting utilities for CLI commands."""

from collections.abc import Sequence
from datetime import UTC
from datetime import datetime
from prettytable import PrettyTable
from typing import Any
import click


def format_key_value(data: dict[str, Any], title: str | None = None) -> str:
    """Format dict as aligned key-value pairs.

    Args:
        data: Dictionary to format
        title: Optional title to display above the data

    Returns:
        Formatted string with aligned keys and values
    """
    if not data:
        return "No data."
    max_key_len = max(len(str(k)) for k in data.keys())
    lines = []
    if title:
        lines.append(title)
        lines.append("-" * len(title))
    for key, value in data.items():
        lines.append(f"{key:<{max_key_len + 1}} {value}")
    return "\n".join(lines)


def output_list(
    data: Sequence[dict[str, Any]],
    columns: Sequence[str],
    column_labels: Sequence[str] | None = None,
) -> None:
    """Render a sequence of records as a left-aligned ASCII table. Empty
    input prints ``No data found.`` Non-table formats (JSON, compact) are
    handled by the schema-projecting ``json_output`` module instead."""
    if not data:
        click.echo("No data found.")
        return
    labels = list(column_labels or columns)
    table = PrettyTable(labels)
    table.align = "l"
    for item in data:
        table.add_row([item.get(c, "N/A") for c in columns])
    click.echo(table.get_string())


def format_timestamp(value: str | None) -> str:
    """Format an ISO timestamp as absolute UTC string."""
    if not value:
        return "unknown"
    try:
        dt = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return value
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=UTC)
    else:
        dt = dt.astimezone(UTC)
    fractional = dt.microsecond // 10000  # two decimals
    return f"{dt:%Y-%m-%dT%H:%M:%S}.{fractional:02d}Z"


def format_relative_timestamp(value: str | None) -> str:
    """Format an ISO timestamp as a relative age (e.g. '5m ago', '2h ago')."""
    if not value:
        return "unknown"
    try:
        dt = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return value
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=UTC)
    now = datetime.now(UTC)
    total = int((now - dt).total_seconds())
    if total < 0:
        return "just now"
    if total < 60:
        return f"{total}s ago"
    mins = total // 60
    if mins < 60:
        return f"{mins}m ago"
    hours = mins // 60
    if hours < 24:
        rem = mins % 60
        return f"{hours}h ago" if rem == 0 else f"{hours}h{rem}m ago"
    days = hours // 24
    return f"{days}d ago"


def short_id(full_id: str, *, keep: int = 8) -> str:
    """Shorten a prefixed ID for display (e.g. 'job-abc123def456...' -> 'job-abc123de')."""
    if "-" in full_id:
        prefix, rest = full_id.split("-", 1)
        if len(rest) > keep:
            return f"{prefix}-{rest[:keep]}"
    return full_id
