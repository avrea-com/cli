"""Cache management CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.display import get_console_url
from avrea_cli.display import is_piped
from avrea_cli.display import open_or_print_url
from avrea_cli.display import print_piped_header
from avrea_cli.display import print_piped_row
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import ensure_prompts_allowed
from avrea_cli.helpers import format_size
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_org_slug
from avrea_cli.helpers import handle_http_error
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import emit_json_record
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import reject_web_with_json
from avrea_cli.json_output import split_fields
from avrea_cli.output import format_key_value
from avrea_cli.output import format_timestamp
from avrea_cli.output import output_list
from avrea_cli.repo_context import resolve_repo_named
from avrea_cli.repo_context import resolve_repo_or_detect
from typing import Any
import click
import httpx


@click.group(cls=GhGroup)
@click.pass_context
def cache(ctx):
    """Inspect and manage the Avrea build cache."""
    ensure_ctx(ctx)


_CACHE_LIST_FIELDS = make_schema(
    "cache_type", "key", "ref", "size_bytes", "created_at", "last_accessed_at", "version", "hit_count"
)


@cache.command("list")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option(
    "--type",
    "cache_type",
    type=str,
    help="Filter by cache type (e.g. gha, bazel, turbo, rclone).",
)
@click.option("--key", help="Filter by key prefix.")
@click.option("--ref", help="Filter by exact ref match.")
@click.option(
    "-L", "--limit", type=click.IntRange(1, 1000), default=100, show_default=True, help="Max entries to return."
)
@click.option("--offset", type=click.IntRange(min=0), default=0, show_default=True, help="Number of entries to skip.")
@click.option(
    "--order",
    type=click.Choice(["created_at.desc", "created_at.asc"]),
    default="created_at.desc",
    show_default=True,
    help="Sort order.",
)
@click.option(
    "--json",
    "json_fields",
    default=None,
    help='Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.',
)
@click.option("-q", "--jq", "jq_expr", default=None, help="Filter --json output through a jq expression.")
@click.option("--web", is_flag=True, help="Open in browser.")
@click.pass_context
def cache_list(ctx, repo_id, org_id, cache_type, key, ref, limit, offset, order, json_fields, jq_expr, web: bool):
    """List cache entries for a repository.

    \b
    Examples:
        avr cache list --repo rep-abc123
        avr cache list --repo rep-abc123 --type gha --limit 50
        avr cache list --repo rep-abc123 --key "node_modules" --ref refs/heads/main
        avr cache list --repo rep-abc123 --json key,size_bytes,created_at

    \b
    JSON FIELDS
        cache_type, created_at, hit_count, key, last_accessed_at, ref, size_bytes, version
    """
    reject_web_with_json(json_fields, web)
    if handle_json_meta(json_fields, jq_expr, _CACHE_LIST_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id, required=True)

    if web:
        slug = get_org_slug(client, org_id)
        console_url = get_console_url(config.public_api_url)
        url = f"{console_url}/org/{slug}/caches/{repo_id}"
        open_or_print_url(url)
        return

    params: dict[str, Any] = {"limit": limit, "order": order}
    if cache_type:
        params["cache_type"] = cache_type.lower()
    if key:
        params["key"] = key
    if ref:
        params["ref"] = ref
    if offset:
        params["offset"] = offset

    try:
        response = client.public_get(f"/orgs/{org_id}/repos/{repo_id}/cache/entries", params=params)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list cache entries")

    entries = response["data"]

    if json_fields is not None:
        emit_json(entries, split_fields(json_fields, _CACHE_LIST_FIELDS), _CACHE_LIST_FIELDS, jq_expr)
        return

    if is_piped():
        print_piped_header(["cache_type", "key", "ref", "size_bytes", "created_at"])
        for e in entries:
            print_piped_row(
                [
                    e.get("cache_type", ""),
                    e.get("key", ""),
                    e.get("ref", ""),
                    e.get("size_bytes", 0),
                    e.get("created_at", ""),
                ]
            )
        return

    for entry in entries:
        entry["size_display"] = format_size(entry.get("size_bytes", 0))
        entry["key_display"] = entry.get("key") or "-"
        entry["ref_display"] = entry.get("ref") or "-"
        entry["created_display"] = format_timestamp(entry.get("created_at"))

    output_list(
        entries,
        columns=["cache_type", "key_display", "ref_display", "size_display", "created_display"],
        column_labels=["Type", "Key", "Ref", "Size", "Created"],
    )

    total = response.get("total", len(entries))
    shown = offset + len(entries)
    if shown < total:
        click.echo(f"\nShowing {shown}/{total}. Next page: --offset {shown}", err=True)


_CACHE_USAGE_FIELDS = make_schema(
    total_size_bytes="data.total_size_bytes",
    quota_bytes="data.quota_bytes",
    over_quota="data.over_quota",
    by_type="data.by_type",
)


@cache.command("usage")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option(
    "--json",
    "json_fields",
    default=None,
    help='Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.',
)
@click.option("-q", "--jq", "jq_expr", default=None, help="Filter --json output through a jq expression.")
@click.pass_context
def cache_usage(ctx, repo_id, org_id, json_fields, jq_expr):
    """Show cache usage summary for a repository.

    \b
    Examples:
        avr cache usage --repo rep-abc123
        avr cache usage --repo rep-abc123 --json '*'

    \b
    JSON FIELDS
        by_type, over_quota, quota_bytes, total_size_bytes
    """
    if handle_json_meta(json_fields, jq_expr, _CACHE_USAGE_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id, required=True)

    try:
        response = client.public_get(f"/orgs/{org_id}/repos/{repo_id}/cache")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "get cache usage")

    if json_fields is not None:
        emit_json_record(response, split_fields(json_fields, _CACHE_USAGE_FIELDS), _CACHE_USAGE_FIELDS, jq_expr)
        return

    data = response["data"]
    total = data.get("total_size_bytes", 0)
    quota = data.get("quota_bytes", 0)
    pct = (total / quota * 100) if quota > 0 else 0

    click.echo(
        format_key_value(
            {
                "Total Size": format_size(total),
                "Quota": format_size(quota),
                "Usage": f"{pct:.1f}%",
                "Over Quota": "yes" if data.get("over_quota") else "no",
            }
        )
    )

    by_type = data.get("by_type", [])
    if by_type:
        click.echo()
        for bt in by_type:
            bt["size_display"] = format_size(bt.get("size_bytes", 0))

        output_list(
            by_type,
            columns=["cache_type", "size_display", "entry_count"],
            column_labels=["Type", "Size", "Count"],
        )


@cache.command("delete")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option(
    "--type",
    "cache_type",
    type=str,
    default=None,
    help="Cache type (required with --key, e.g. gha, bazel, sccache).",
)
@click.option("--key", default=None, help="Delete entries matching this cache key name.")
@click.option("--ref", default=None, help="Ref to narrow deletion scope (used by gha).")
@click.option("--all", "delete_all", is_flag=True, help="Delete ALL cache entries for the repository.")
@click.option("--yes", "-y", is_flag=True, help="Skip confirmation prompt.")
@click.pass_context
def cache_delete(ctx, repo_id, org_id, cache_type, key, ref, delete_all, yes):
    """Delete cache entries by key name or all entries.

    Exactly one of --key or --all must be provided.
    When using --key, --type is required to scope the deletion.

    \b
    Examples:
        avr cache delete --repo rep-abc123 --type gha --key "node_modules" --ref refs/heads/main --yes
        avr cache delete --repo rep-abc123 --all --yes
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    provided = sum(bool(x) for x in (key, delete_all))
    if provided != 1:
        click.echo("Error: Provide exactly one of --key or --all.", err=True)
        raise click.Abort()

    if key and not cache_type:
        click.echo("Error: --type is required when using --key.", err=True)
        raise click.Abort()

    org_id = get_org_id(config, org_id, client=client)
    repo_id, repo_label = resolve_repo_named(client, config, org_id, repo_id)

    if key:
        if not yes:
            ensure_prompts_allowed("cache delete needs confirmation")
            ref_scope = f"ref '{ref}'" if ref else "across all refs"
            click.confirm(
                f"Delete entries in {repo_label} matching type '{cache_type}', key '{key}' ({ref_scope})?",
                abort=True,
            )
        try:
            params: dict[str, str] = {"cache_type": cache_type, "key": key}
            if ref:
                params["ref"] = ref
            response = client.public_delete(
                f"/orgs/{org_id}/repos/{repo_id}/cache/entries",
                params=params,
            )
            count = response.get("deleted_count", 0) if response else 0
            click.echo(f"Deleted {count} cache {'entry' if count == 1 else 'entries'}.")
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "delete cache entries")
    else:
        if not yes:
            ensure_prompts_allowed("cache purge needs confirmation")
            click.confirm(f"Delete ALL cache entries for {repo_label}?", abort=True)
        try:
            response = client.public_delete(f"/orgs/{org_id}/repos/{repo_id}/cache")
            count = response.get("deleted_count", 0) if response else 0
            click.echo(f"Purged {count} cache {'entry' if count == 1 else 'entries'} from {repo_label}.")
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "purge cache")
