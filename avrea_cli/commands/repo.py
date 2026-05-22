"""Repository CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.display import DIM_FG
from avrea_cli.display import get_console_url
from avrea_cli.display import hyperlink
from avrea_cli.display import is_piped
from avrea_cli.display import print_piped_header
from avrea_cli.display import print_piped_row
from avrea_cli.display import repo_url
from avrea_cli.display import truncate
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_org_slug
from avrea_cli.helpers import handle_http_error
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import json_options
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from typing import Any
import click
import httpx

_REPO_LIST_FIELDS = make_schema("repository_id", "full_name", "platform", "platform_repository_id")

# Sized so common values fit without truncation. 50 chars covers typical
# "org/name" lengths with headroom. Platform IDs are GitHub repo numeric
# IDs (10 digits today; 12 leaves room for growth).
_REPO_TABLE_W = {"name": 50, "id": 36, "platform": 10, "platform_id": 12}


def _hdr_cell(label: str, width: int) -> str:
    return click.style(f"{label:{width}s}", fg=DIM_FG, underline=True)


def _print_repos_table(repos: list[dict[str, Any]], *, console_url: str = "", slug: str = "") -> None:
    """Sectioned-table renderer matching `avr job list` / `avr run list`.

    When ``console_url`` and ``slug`` are non-empty, the repository name and
    id cells are wrapped in OSC 8 hyperlinks to the console activity feed
    filtered to that repo. Caller passes them only when
    ``ctx.obj['links_enabled']`` is true.

    Switches to tab-separated output (header row + data rows, no color, no
    truncation) when stdout isn't a TTY — the standard scriptability
    convention."""
    if is_piped():
        print_piped_header(["repository", "repository_id", "platform", "platform_repository_id"])
        for r in repos:
            print_piped_row(
                [
                    r.get("full_name", ""),
                    r.get("repository_id", ""),
                    r.get("platform", ""),
                    r.get("platform_repository_id", ""),
                ]
            )
        return

    w = _REPO_TABLE_W
    s = " "
    click.echo(
        f"  {_hdr_cell('REPOSITORY', w['name'])}{s}"
        f"{_hdr_cell('ID', w['id'])}{s}"
        f"{_hdr_cell('PLATFORM', w['platform'])}{s}"
        f"{_hdr_cell('PLATFORM ID', w['platform_id'])}"
    )
    for r in repos:
        name = f"{truncate(r.get('full_name', ''), w['name'] - 2):{w['name']}s}"
        repo_id = f"{r.get('repository_id', ''):{w['id']}s}"
        platform = f"{r.get('platform', ''):{w['platform']}s}"
        platform_id = str(r.get("platform_repository_id") or "")
        # Pad-then-style for the bold name cell, OSC 8 wrap last so click's
        # padding doesn't count escape bytes. Repo_id sits inside the table
        # so it also needs its width preserved before linking.
        name_cell = click.style(name, bold=True)
        repo_id_cell = click.style(repo_id, fg="cyan")
        platform_id_cell = click.style(platform_id, dim=True)
        if console_url and slug and r.get("repository_id"):
            url = repo_url(console_url, slug, r["repository_id"])
            name_cell = hyperlink(name_cell, url)
            repo_id_cell = hyperlink(repo_id_cell, url)
        if console_url and slug and r.get("platform") == "github" and r.get("full_name") and platform_id:
            platform_id_cell = hyperlink(platform_id_cell, f"https://github.com/{r['full_name']}")
        click.echo(f"  {name_cell}{s}{repo_id_cell}{s}{click.style(platform, dim=True)}{s}{platform_id_cell}")


@click.group(cls=GhGroup)
@click.pass_context
def repo(ctx):
    """List repositories connected to Avrea."""
    ensure_ctx(ctx)


@repo.command("list")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "-L",
    "--limit",
    type=click.IntRange(1, 1000),
    default=100,
    show_default=True,
    help="Max repositories to return.",
)
@json_options
@click.pass_context
def repo_list(ctx, org_id, limit, json_fields, jq_expr):
    """List repositories you can access in an organization.

    \b
    Examples:
        avr repo list
        avr repo list --json full_name,repository_id
        avr repo list --json '*' -q '.[].full_name'

    \b
    JSON FIELDS
        full_name, platform, platform_repository_id, repository_id
    """
    if handle_json_meta(json_fields, jq_expr, _REPO_LIST_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(f"/orgs/{org_id}/repos", params={"limit": limit})
        data = response.get("data") or []
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list repositories")

    if json_fields is not None:
        emit_json(data, split_fields(json_fields, _REPO_LIST_FIELDS), _REPO_LIST_FIELDS, jq_expr)
        return

    if not data:
        click.echo("No repositories found.")
        return

    # Skip slug lookup when piped — `_print_repos_table` writes plain TSV
    # without OSC 8 wrapping in that mode, so the API call would be wasted.
    links_enabled = ctx.obj.get("links_enabled", False) and not is_piped()
    link_console_url = get_console_url(config.public_api_url) if links_enabled else ""
    link_slug = get_org_slug(client, org_id) if links_enabled else ""
    _print_repos_table(data, console_url=link_console_url, slug=link_slug)
