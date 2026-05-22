"""Log search CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import handle_http_error
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import json_options
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from avrea_cli.repo_context import resolve_repo_or_detect
import click
import httpx


@click.group(cls=GhGroup)
@click.pass_context
def log(ctx):
    """Search across runner execution logs."""
    ensure_ctx(ctx)


_LOG_SEARCH_FIELDS = make_schema(
    "id",
    "vm_id",
    "line_number",
    "content",
    "stream",
    "level",
    "timestamp",
    "score",
    "repository_id",
    "group_name",
    "step_name",
    "step_record_id",
)


@log.command("search")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified.")
@click.option("--query", help="Full-text search query")
@click.option(
    "--stream",
    type=click.Choice(["stdout", "stderr"], case_sensitive=False),
    help="Filter by output stream",
)
@click.option(
    "--level",
    type=click.Choice(["debug", "info", "warning", "error"], case_sensitive=False),
    help="Filter by log level",
)
@click.option("--vm-id", help="Filter by execution/VM ID")
@click.option("-L", "--limit", type=int, default=100, show_default=True, help="Maximum results to return")
@json_options
@click.pass_context
def log_search(
    ctx,
    repo_id: str,
    org_id: str | None,
    query: str | None,
    stream: str | None,
    level: str | None,
    vm_id: str | None,
    limit: int,
    json_fields: str | None,
    jq_expr: str | None,
):
    """Search logs for a repository.

    Performs full-text search when --query is provided, otherwise returns
    logs sorted by line number. Results are filtered to logs from repositories
    you have access to.

    \b
    Examples:
        avr log search --repo acme/web --query "error"
        avr log search --repo rep-abc123 --level error --limit 50
        avr log search --repo rep-abc123 --vm-id vm-xyz --stream stderr
        avr log search --repo rep-abc123 --query "OOM" --json content,timestamp,vm_id

    \b
    JSON FIELDS
        content, group_name, id, level, line_number, repository_id, score,
        step_name, step_record_id, stream, timestamp, vm_id
    """
    if handle_json_meta(json_fields, jq_expr, _LOG_SEARCH_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id, required=True)

    payload: dict = {
        "repository_ids": [repo_id],
        "limit": limit,
    }
    if query:
        payload["query"] = query
    if stream:
        payload["stream"] = stream.lower()
    if level:
        payload["level"] = level.lower()
    if vm_id:
        payload["vm_id"] = vm_id

    try:
        result = client.public_post("/logs/search", json=payload)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "search logs")

    results = result.get("results", [])

    if json_fields is not None:
        emit_json(results, split_fields(json_fields, _LOG_SEARCH_FIELDS), _LOG_SEARCH_FIELDS, jq_expr)
        return

    if not results:
        click.echo("No logs found.")
        return

    total = result.get("total_estimated", len(results))
    has_more = result.get("has_more", False)
    click.echo(f"Found {total} log entries{' (more available)' if has_more else ''}\n")

    for entry in results:
        ts = entry.get("timestamp", "")[:19]
        level_str = entry.get("level", "info").upper()[:5].ljust(5)
        stream_str = entry.get("stream", "stdout")[:6].ljust(6)
        line_no = entry.get("line_number", 0)
        content = entry.get("content", "")

        level_colors = {"ERROR": "red", "WARNI": "yellow", "DEBUG": "cyan"}
        level_color = level_colors.get(level_str, None)
        if level_color:
            level_str = click.style(level_str, fg=level_color)

        prefix = f"[{ts}] {level_str} {stream_str} L{line_no:>6}"
        click.echo(f"{prefix}: {content}")
