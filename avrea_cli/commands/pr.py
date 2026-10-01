"""Pull request CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.display import DIM_FG
from avrea_cli.display import get_console_url
from avrea_cli.display import hyperlink
from avrea_cli.display import is_piped
from avrea_cli.display import pr_url
from avrea_cli.display import print_piped_header
from avrea_cli.display import print_piped_row
from avrea_cli.display import truncate
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_org_slug
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import validate_cursor
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from avrea_cli.output import format_relative_timestamp
from avrea_cli.repo_context import resolve_repos_or_detect
from typing import Any
import click
import httpx

_PR_LIST_FIELDS = make_schema(
    "number",
    "title",
    "state",
    "draft",
    "merged",
    "author_login",
    "base_ref",
    "head_ref",
    "head_sha",
    "base_sha",
    "created_at",
    "updated_at",
    "comment_count",
    "unresolved_thread_count",
    "check_status",
    "mergeability",
    "repository_id",
    "repository_full_name",
)

_SCOPES = ["all", "authored", "involved"]
_STATES = ["open", "closed", "merged", "all"]
_PR_LIST_FEATURE_FLAG = "feature.org-pull-requests.enabled"
_PR_LIST_UNSUPPORTED_MESSAGE = "Sorry, pull request listing is not yet supported for this organization. Coming soon."
_PR_LIST_AVAILABILITY_UNKNOWN_MESSAGE = (
    "Sorry, we could not find this organization or confirm pull request listing support. "
    "Organizations without Avrea Git are not supported yet. Coming soon."
)


@click.group(cls=GhGroup)
@click.pass_context
def pr(ctx):
    """View pull requests."""
    ensure_ctx(ctx)


@pr.command("list")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option(
    "--repo",
    "repository_ids",
    multiple=True,
    help=(
        "Filter by repository (org/repo or rep-xxx). Pass --repo more than once to filter multiple repositories. "
        "Auto-detected from git remote if omitted."
    ),
)
@click.option(
    "--scope",
    type=click.Choice(_SCOPES, case_sensitive=False),
    default="all",
    show_default=True,
    help="List every readable PR, PRs you authored, or PRs you are involved in.",
)
@click.option(
    "--state",
    type=click.Choice(_STATES, case_sensitive=False),
    default="open",
    show_default=True,
    help="Filter by pull request state. 'all' removes the state filter.",
)
@click.option("-L", "--limit", type=click.IntRange(1, 200), default=20, show_default=True, help="Max PRs to return.")
@click.option("--cursor", default=None, help="Pagination cursor from a previous response.")
@click.option(
    "--json",
    "json_fields",
    default=None,
    help='Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.',
)
@click.option("-q", "--jq", "jq_expr", default=None, help="Filter --json output through a jq expression.")
@click.pass_context
def pr_list(
    ctx,
    org_id,
    repository_ids,
    scope,
    state,
    limit,
    cursor,
    json_fields,
    jq_expr,
):
    """List pull requests across repositories.

    \b
    Examples:
        avr pr list
        avr pr list --scope authored
        avr pr list --repo acme/widgets --state merged
        avr pr list --json number,title,mergeability
        avr pr list --json '?'           # list available fields
        avr pr list --json '*'           # all fields

    \b
    JSON FIELDS
        author_login, base_ref, base_sha, check_status, comment_count, created_at,
        draft, head_ref, head_sha, mergeability, merged, number, repository_full_name,
        repository_id, state, title, unresolved_thread_count, updated_at
    """
    if handle_json_meta(json_fields, jq_expr, _PR_LIST_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        feature = client.public_get(f"/orgs/{org_id}/feature-flags/{_PR_LIST_FEATURE_FLAG}")
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code == 404:
            raise click.ClickException(_PR_LIST_AVAILABILITY_UNKNOWN_MESSAGE) from None
        handle_http_error(exc, "check pull request listing availability")
    except httpx.TransportError as exc:
        raise click.ClickException(f"Could not list pull requests because the request to Avrea failed: {exc}") from None

    if feature.get("enabled") is not True:
        raise click.ClickException(_PR_LIST_UNSUPPORTED_MESSAGE)

    resolved_repositories = resolve_repos_or_detect(
        client,
        config,
        org_id,
        repository_ids,
        soft_detect=True,
    )

    params: dict[str, Any] = {
        "scope": scope.lower(),
        "limit": limit,
    }
    if state.lower() != "all":
        params["state"] = state.lower()
    if resolved_repositories:
        params["repository_ids"] = resolved_repositories
    cursor = validate_cursor(cursor)
    if cursor:
        params["cursor"] = cursor

    try:
        response = client.public_get(f"/orgs/{org_id}/pull-requests", params=params)
        pulls = response.get("data") or []
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list pull requests")
    except httpx.TransportError as exc:
        raise click.ClickException(f"Could not list pull requests because the request to Avrea failed: {exc}") from None

    if json_fields is not None:
        emit_json(list(pulls), split_fields(json_fields, _PR_LIST_FIELDS), _PR_LIST_FIELDS, jq_expr)
        return

    if not pulls:
        click.echo("No pull requests found.")
        return

    if is_piped():
        _print_piped(pulls)
    else:
        _print_table(
            ctx,
            client,
            config,
            org_id,
            pulls,
            show_repository=len(resolved_repositories) != 1,
        )

    next_cursor = (response.get("pagination") or {}).get("next_cursor")
    if next_cursor:
        click.echo(f"\nMore results available. Next page: --cursor {next_cursor}", err=True)


def _display_state(pull: dict[str, Any]) -> str:
    if pull.get("merged"):
        return "merged"
    if pull.get("draft") and pull.get("state") != "closed":
        return "draft"
    return str(pull.get("state") or "unknown")


def _merge_status(pull: dict[str, Any]) -> str:
    mergeability = pull.get("mergeability")
    if isinstance(mergeability, dict) and mergeability.get("status"):
        return str(mergeability["status"])
    if pull.get("merged") or pull.get("draft") or pull.get("state") == "closed":
        return "-"
    return "?"


def _print_piped(pulls: list[dict[str, Any]]) -> None:
    print_piped_header(
        [
            "number",
            "repository",
            "state",
            "title",
            "author",
            "head_ref",
            "base_ref",
            "check_status",
            "mergeability",
            "comment_count",
            "unresolved_threads",
            "head_sha",
            "updated_at",
        ]
    )
    for pull in pulls:
        print_piped_row(
            [
                pull.get("number"),
                pull.get("repository_full_name") or pull.get("repository_id"),
                _display_state(pull),
                pull.get("title"),
                pull.get("author_login"),
                pull.get("head_ref"),
                pull.get("base_ref"),
                pull.get("check_status"),
                (pull.get("mergeability") or {}).get("status"),
                pull.get("comment_count"),
                pull.get("unresolved_thread_count"),
                pull.get("head_sha"),
                pull.get("updated_at"),
            ]
        )


def _print_table(
    ctx: click.Context,
    client: ApiClient,
    config: CliConfig,
    org_id: str,
    pulls: list[dict[str, Any]],
    *,
    show_repository: bool,
) -> None:
    widths = {
        "repo": 30,
        "pr": 8,
        "state": 8,
        "checks": 11,
        "merge": 12,
        "unres": 7,
        "title": 48,
        "author": 18,
        "age": 10,
    }

    def cell(value: object, width: int) -> str:
        return f"{truncate(str(value), width - 1):{width}s}"

    def header(value: str, width: int) -> str:
        return click.style(cell(value, width), fg=DIM_FG, underline=True)

    click.echo(
        "  "
        + header("PR", widths["pr"])
        + (header("REPOSITORY", widths["repo"]) if show_repository else "")
        + header("STATE", widths["state"])
        + header("CHECKS", widths["checks"])
        + header("MERGE", widths["merge"])
        + header("UNRES", widths["unres"])
        + header("TITLE", widths["title"])
        + header("AUTHOR", widths["author"])
        + header("UPDATED", widths["age"])
    )

    links_enabled = ctx.obj.get("links_enabled", False)
    console_url = get_console_url(config.public_api_url) if links_enabled else ""
    slug = get_org_slug(client, org_id) if links_enabled else ""

    for pull in pulls:
        repository = pull.get("repository_full_name") or pull.get("repository_id") or "?"
        number = pull.get("number")
        pr_label = f"#{number}" if number is not None else "?"
        state = _display_state(pull)
        checks = pull.get("check_status") or "-"
        merge = _merge_status(pull)
        unresolved = pull.get("unresolved_thread_count")
        unresolved = "?" if unresolved is None else unresolved
        title = pull.get("title") or ""
        author = pull.get("author_login") or "?"
        age = format_relative_timestamp(pull.get("updated_at"))

        repository_cell = cell(repository, widths["repo"])
        pr_cell = click.style(cell(pr_label, widths["pr"]), fg="cyan")
        title_cell = cell(title, widths["title"])
        repo_id = pull.get("repository_id")
        if links_enabled and repo_id and isinstance(number, int):
            url = pr_url(console_url, slug, str(repo_id), number)
            pr_cell = hyperlink(pr_cell, url)
            title_cell = hyperlink(title_cell, url)

        click.echo(
            "  "
            + pr_cell
            + (repository_cell if show_repository else "")
            + cell(state, widths["state"])
            + cell(checks, widths["checks"])
            + cell(merge, widths["merge"])
            + cell(unresolved, widths["unres"])
            + title_cell
            + cell(author, widths["author"])
            + click.style(cell(age, widths["age"]), dim=True)
        )
