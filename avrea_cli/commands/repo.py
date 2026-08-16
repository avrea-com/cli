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
from avrea_cli.helpers import ensure_prompts_allowed
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_org_slug
from avrea_cli.helpers import handle_http_error
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import emit_json_record
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import json_options
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from avrea_cli.output import format_key_value
from avrea_cli.output import format_timestamp
from avrea_cli.output import output_list
from avrea_cli.repo_context import resolve_repo_or_detect
from typing import Any
from urllib.parse import quote
import click
import httpx

_REPO_LIST_FIELDS = make_schema("repository_id", "full_name", "platform", "platform_repository_id")
_PUBLIC_MIRROR_REQUEST_FIELDS = make_schema(
    "request_id",
    "repository_id",
    "repository_full_name",
    "status",
    "requester_organization_id",
    "requester_user_id",
    "reason",
    "github_snapshot",
    "created_at",
    "updated_at",
    "reviewed_at",
    "reviewed_by_user_id",
    "review_note",
    "approval_state",
    "public_access_expires_at",
)
_GIT_MIRROR_FIELDS = make_schema(
    "repository_id",
    "full_name",
    "enabled",
    "placements",
)
_GIT_CLUSTER_FIELDS = make_schema("cluster_id", "datacenter_id", "name")
_PUBLIC_MIRROR_CATALOG_FIELDS = make_schema(
    # Locally derived: false when the catalog lookup 404s. Every other field is
    # null in that case, so this is the one field a script can always branch on.
    "available",
    "repository_id",
    "platform_repository_id",
    "repository_full_name",
    "https_clone_url",
    "default_branch",
    "platform_owner_id",
    "platform_owner_type",
    "platform_owner_login",
    "is_archived",
    "is_disabled",
    "is_fork",
    "platform_size_kb",
    "platform_pushed_at",
    "public_metadata_verified_at",
    "approval_state",
    "public_access_expires_at",
    "mirror_enabled",
    "installation_kind",
)

# Sized so common values fit without truncation. 50 chars covers typical
# "org/name" lengths with headroom. Platform IDs are GitHub repo numeric
# IDs (10 digits today; 12 leaves room for growth).
_REPO_TABLE_W = {"name": 50, "id": 36, "platform": 10, "platform_id": 12}


def _hdr_cell(label: str, width: int) -> str:
    return click.style(f"{label:{width}s}", fg=DIM_FG, underline=True)


def _public_mirror_lookup_path(full_name: str) -> str:
    """Build a safe exact-name lookup path from an owner/repository name."""
    if full_name.count("/") != 1:
        raise click.BadParameter("must be an owner/repository name", param_hint="FULL_NAME")
    owner, repository = full_name.split("/")
    # quote() leaves "." and ".." intact — both are unreserved — and httpx
    # collapses dot segments when it builds the URL, so "../rust" would send an
    # authenticated GET to a different endpoint entirely. Reject them here; a
    # slash inside a segment is already neutralised by safe=''.
    if owner in {"", ".", ".."} or repository in {"", ".", ".."}:
        raise click.BadParameter("must be an owner/repository name", param_hint="FULL_NAME")
    return f"/public-mirrors/{quote(owner, safe='')}/{quote(repository, safe='')}"


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
    """Manage repositories, git mirrors, and public mirrors."""
    ensure_ctx(ctx)


@repo.command("list")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
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


@repo.group("public-mirror", cls=GhGroup)
@click.pass_context
def public_mirror(ctx):
    """Request and browse mirrors of public GitHub repositories."""
    ensure_ctx(ctx)


@public_mirror.command("check")
@click.argument("full_name")
@json_options
@click.pass_context
def public_mirror_check(ctx, full_name, json_fields, jq_expr):
    """Check whether a public GitHub repository mirror is available.

    FULL_NAME must be an owner/repository name. This performs an exact lookup;
    the global public-mirror catalog cannot be listed.

    A repository that is not mirrored is an answer, not a failure: the command
    reports ``Available: no`` (``available: false`` under --json) and exits 0.
    Only a real failure — denied, malformed name, server error — exits non-zero.

    \b
    Examples:
        avr repo public-mirror check rust-lang/rust
        avr repo public-mirror check rust-lang/rust --json '*'
        avr repo public-mirror check rust-lang/rust --json available,default_branch

    \b
    JSON FIELDS
        approval_state, available, default_branch, https_clone_url,
        installation_kind, is_archived, is_disabled, is_fork, mirror_enabled,
        platform_owner_id, platform_owner_login, platform_owner_type,
        platform_pushed_at, platform_repository_id, platform_size_kb,
        public_access_expires_at, public_metadata_verified_at,
        repository_full_name, repository_id
    """
    if handle_json_meta(json_fields, jq_expr, _PUBLIC_MIRROR_CATALOG_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    try:
        mirror = client.public_get(_public_mirror_lookup_path(full_name))
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code != 404:
            handle_http_error(exc, "check public-mirror availability")
        # 404 is the catalog saying "not mirrored" — the question was answered.
        # Echo back the queried name so a --json consumer keeps the subject.
        mirror = None

    record = {"available": mirror is not None, **(mirror or {"repository_full_name": full_name})}

    if json_fields is not None:
        emit_json_record(
            record,
            split_fields(json_fields, _PUBLIC_MIRROR_CATALOG_FIELDS),
            _PUBLIC_MIRROR_CATALOG_FIELDS,
            jq_expr,
        )
        return

    if mirror is None:
        click.echo(format_key_value({"Available": "no", "Repository": full_name}))
        click.echo(f"\nHint: request one with `avr repo public-mirror request {full_name}`.", err=True)
        return

    click.echo(
        format_key_value(
            {
                "Available": "yes",
                "Repository": mirror["repository_full_name"],
                "Repository ID": mirror["repository_id"],
                "Default branch": mirror.get("default_branch") or "-",
                "Public until": format_timestamp(mirror.get("public_access_expires_at")),
            }
        )
    )


@public_mirror.command("request")
@click.argument("full_name")
@click.option("--reason", help="Why your organization needs this repository mirrored.")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@json_options
@click.pass_context
def public_mirror_request(ctx, full_name, reason, org_id, json_fields, jq_expr):
    """Request a mirror of a public GitHub repository.

    FULL_NAME is the canonical GitHub owner/repository name. Avrea validates
    the repository with GitHub before recording the request.

    \b
    Examples:
        avr repo public-mirror request rust-lang/rust
        avr repo public-mirror request rust-lang/rust --reason "Build dependency"
        avr repo public-mirror request rust-lang/rust --org acme --json '*'

    \b
    JSON FIELDS
        approval_state, created_at, github_snapshot, public_access_expires_at,
        reason, repository_full_name, repository_id, request_id,
        requester_organization_id, requester_user_id, review_note, reviewed_at,
        reviewed_by_user_id, status, updated_at
    """
    if handle_json_meta(json_fields, jq_expr, _PUBLIC_MIRROR_REQUEST_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    body = {"full_name": full_name}
    if reason is not None:
        body["reason"] = reason

    try:
        result = client.public_post(f"/orgs/{org_id}/public-mirrors/requests", json=body)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "request a public mirror")

    if json_fields is not None:
        emit_json_record(
            result,
            split_fields(json_fields, _PUBLIC_MIRROR_REQUEST_FIELDS),
            _PUBLIC_MIRROR_REQUEST_FIELDS,
            jq_expr,
        )
        return

    click.echo(
        format_key_value(
            {
                "Request ID": result["request_id"],
                "Repository": result["repository_full_name"],
                "Status": result["status"],
                "Organization": result["requester_organization_id"],
                "Created": format_timestamp(result.get("created_at")),
            }
        )
    )


@public_mirror.command("requests")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@json_options
@click.pass_context
def public_mirror_requests(ctx, org_id, json_fields, jq_expr):
    """List your organization's public-mirror requests.

    \b
    Examples:
        avr repo public-mirror requests
        avr repo public-mirror requests --org acme
        avr repo public-mirror requests --json request_id,status,repository_full_name

    \b
    JSON FIELDS
        approval_state, created_at, github_snapshot, public_access_expires_at,
        reason, repository_full_name, repository_id, request_id,
        requester_organization_id, requester_user_id, review_note, reviewed_at,
        reviewed_by_user_id, status, updated_at
    """
    if handle_json_meta(json_fields, jq_expr, _PUBLIC_MIRROR_REQUEST_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        requests = client.public_get(f"/orgs/{org_id}/public-mirrors/requests")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list public-mirror requests")

    if json_fields is not None:
        emit_json(
            requests,
            split_fields(json_fields, _PUBLIC_MIRROR_REQUEST_FIELDS),
            _PUBLIC_MIRROR_REQUEST_FIELDS,
            jq_expr,
        )
        return

    for request in requests:
        request["created_display"] = format_timestamp(request.get("created_at"))
        request["reviewed_display"] = (
            format_timestamp(request.get("reviewed_at")) if request.get("reviewed_at") else "-"
        )

    output_list(
        requests,
        columns=["request_id", "repository_full_name", "status", "created_display", "reviewed_display"],
        column_labels=["Request ID", "Repository", "Status", "Created", "Reviewed"],
    )


@public_mirror.command("view")
@click.argument("request_id")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@json_options
@click.pass_context
def public_mirror_view(ctx, request_id, org_id, json_fields, jq_expr):
    """View one public-mirror request.

    \b
    Examples:
        avr repo public-mirror view pmr-0123456789abcdef0123456789abcdef
        avr repo public-mirror view pmr-0123456789abcdef0123456789abcdef --json '*'

    \b
    JSON FIELDS
        approval_state, created_at, github_snapshot, public_access_expires_at,
        reason, repository_full_name, repository_id, request_id,
        requester_organization_id, requester_user_id, review_note, reviewed_at,
        reviewed_by_user_id, status, updated_at
    """
    if handle_json_meta(json_fields, jq_expr, _PUBLIC_MIRROR_REQUEST_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        result = client.public_get(f"/orgs/{org_id}/public-mirrors/requests/{request_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "get public-mirror request")

    if json_fields is not None:
        emit_json_record(
            result,
            split_fields(json_fields, _PUBLIC_MIRROR_REQUEST_FIELDS),
            _PUBLIC_MIRROR_REQUEST_FIELDS,
            jq_expr,
        )
        return

    click.echo(
        format_key_value(
            {
                "Request ID": result["request_id"],
                "Repository": result["repository_full_name"],
                "Repository ID": result["repository_id"],
                "Status": result["status"],
                "Reason": result.get("reason") or "-",
                "Created": format_timestamp(result.get("created_at")),
                "Updated": format_timestamp(result.get("updated_at")),
                "Reviewed": format_timestamp(result.get("reviewed_at")) if result.get("reviewed_at") else "-",
                "Review note": result.get("review_note") or "-",
                "Approval": result.get("approval_state") or "-",
                "Public until": (
                    format_timestamp(result.get("public_access_expires_at"))
                    if result.get("public_access_expires_at")
                    else "-"
                ),
            }
        )
    )


@public_mirror.command("cancel")
@click.argument("request_id")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--yes", "-y", is_flag=True, help="Skip the confirmation prompt.")
@json_options
@click.pass_context
def public_mirror_cancel(ctx, request_id, org_id, yes, json_fields, jq_expr):
    """Cancel one pending public-mirror request.

    An approved mirror is global and cannot be withdrawn by a requester.

    \b
    Examples:
        avr repo public-mirror cancel pmr-0123456789abcdef0123456789abcdef
        avr repo public-mirror cancel pmr-0123456789abcdef0123456789abcdef --yes

    \b
    JSON FIELDS
        approval_state, created_at, github_snapshot, public_access_expires_at,
        reason, repository_full_name, repository_id, request_id,
        requester_organization_id, requester_user_id, review_note, reviewed_at,
        reviewed_by_user_id, status, updated_at
    """
    if handle_json_meta(json_fields, jq_expr, _PUBLIC_MIRROR_REQUEST_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    if not yes:
        ensure_prompts_allowed("public-mirror cancellation needs confirmation")
        click.confirm(f"Cancel public-mirror request {request_id}?", abort=True)

    try:
        result = client.public_delete(f"/orgs/{org_id}/public-mirrors/requests/{request_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "cancel public-mirror request")

    # public_delete returns None on a 204/empty body, which is still a success.
    # Seed the ID we already have so both output paths name the cancelled
    # request instead of raising on a missing key.
    result = result or {"request_id": request_id}

    if json_fields is not None:
        emit_json_record(
            result,
            split_fields(json_fields, _PUBLIC_MIRROR_REQUEST_FIELDS),
            _PUBLIC_MIRROR_REQUEST_FIELDS,
            jq_expr,
        )
        return

    full_name = result.get("repository_full_name")
    click.echo(f"Cancelled public-mirror request {result['request_id']}{f' ({full_name})' if full_name else ''}.")


# --- Git mirrors (feature.git-mirrors.enabled) -----------------------------
#
# Customer-managed avrea-git mirrors of the org's own repositories: declare a
# repo mirrored and place the mirror in git clusters. The API 403s while the
# org's launch flag is off — handle_http_error surfaces the server's detail
# ("avrea-git mirror management is not enabled for this organization"), so no
# client-side gating is needed. Reads are member-level; writes need org admin.

_GIT_MIRROR_BASE = "/api/v1/orgs/{org_id}/repos/{repo_id}/git-mirror"


def _git_mirror_view(client: ApiClient, org_id: str, repo_id: str, action: str) -> dict[str, Any]:
    try:
        return client.public_get(_GIT_MIRROR_BASE.format(org_id=org_id, repo_id=repo_id))
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, action)


def _print_git_mirror(mirror: dict[str, Any]) -> None:
    click.echo(
        format_key_value(
            {
                "Repository": mirror.get("full_name") or mirror["repository_id"],
                "Repository ID": mirror["repository_id"],
                "Mirroring": "enabled" if mirror.get("enabled") else "disabled",
            }
        )
    )
    placements = mirror.get("placements") or []
    if not placements:
        click.echo("\nNo placements. Add one with `avr repo mirror place CLUSTER_ID`.")
        return
    for p in placements:
        p["synced_display"] = format_timestamp(p.get("last_sync_at")) if p.get("last_sync_at") else "-"
        p["sync_status_display"] = p.get("last_sync_status") or "pending"
        p["config_display"] = "yes" if p.get("config_synced") else "pending"
    click.echo()
    output_list(
        placements,
        columns=["cluster_id", "role", "sync_status_display", "synced_display", "config_display"],
        column_labels=["Cluster", "Role", "Last sync", "Synced at", "Config pushed"],
    )


@repo.group("mirror", cls=GhGroup)
@click.pass_context
def mirror(ctx):
    """Manage this repository's avrea-git mirror (feature-flagged)."""
    ensure_ctx(ctx)


@mirror.command("status")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@json_options
@click.pass_context
def mirror_status(ctx, repo_id, org_id, json_fields, jq_expr):
    """Show the repository's git-mirror declaration and placements.

    \b
    Examples:
        avr repo mirror status
        avr repo mirror status --repo acme/widgets
        avr repo mirror status --json enabled,placements

    \b
    JSON FIELDS
        enabled, full_name, placements, repository_id
    """
    if handle_json_meta(json_fields, jq_expr, _GIT_MIRROR_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id, required=True)

    mirror = _git_mirror_view(client, org_id, repo_id, "get git-mirror status")

    if json_fields is not None:
        emit_json_record(mirror, split_fields(json_fields, _GIT_MIRROR_FIELDS), _GIT_MIRROR_FIELDS, jq_expr)
        return

    _print_git_mirror(mirror)


def _set_git_mirror_enabled(ctx, repo_id, org_id, json_fields, jq_expr, *, enabled: bool, yes: bool = True):
    """Shared body of ``mirror enable`` / ``mirror disable``."""
    if handle_json_meta(json_fields, jq_expr, _GIT_MIRROR_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id, required=True)

    if not yes:
        ensure_prompts_allowed("disabling the git mirror needs confirmation")
        click.confirm(f"Disable git mirroring for {repo_id}?", abort=True)

    try:
        mirror = client.public_put(
            _GIT_MIRROR_BASE.format(org_id=org_id, repo_id=repo_id),
            json={"enabled": enabled},
        )
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "enable git mirroring" if enabled else "disable git mirroring")

    if json_fields is not None:
        emit_json_record(mirror, split_fields(json_fields, _GIT_MIRROR_FIELDS), _GIT_MIRROR_FIELDS, jq_expr)
        return

    _print_git_mirror(mirror)


@mirror.command("enable")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@json_options
@click.pass_context
def mirror_enable(ctx, repo_id, org_id, json_fields, jq_expr):
    """Declare the repository mirrored into avrea-git.

    Enabling makes existing placements active again; a freshly declared
    repository still needs at least one placement (`avr repo mirror place`)
    before anything is synced. Requires the organization admin role.

    \b
    Examples:
        avr repo mirror enable
        avr repo mirror enable --repo acme/widgets

    \b
    JSON FIELDS
        enabled, full_name, placements, repository_id
    """
    _set_git_mirror_enabled(ctx, repo_id, org_id, json_fields, jq_expr, enabled=True)


@mirror.command("disable")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--yes", "-y", is_flag=True, help="Skip the confirmation prompt.")
@json_options
@click.pass_context
def mirror_disable(ctx, repo_id, org_id, yes, json_fields, jq_expr):
    """Stop mirroring the repository into avrea-git.

    Placements are kept but become inert, so re-enabling restores them.
    Requires the organization admin role.

    \b
    Examples:
        avr repo mirror disable
        avr repo mirror disable --repo acme/widgets --yes

    \b
    JSON FIELDS
        enabled, full_name, placements, repository_id
    """
    _set_git_mirror_enabled(ctx, repo_id, org_id, json_fields, jq_expr, enabled=False, yes=yes)


@mirror.command("place")
@click.argument("cluster_id")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@json_options
@click.pass_context
def mirror_place(ctx, cluster_id, repo_id, org_id, json_fields, jq_expr):
    """Place the repository's mirror in a git cluster.

    CLUSTER_ID is one of the ids from `avr repo mirror clusters`. Placing is
    idempotent; a new placement syncs from the upstream platform copy.
    Mirroring must be enabled first. Requires the organization admin role.

    \b
    Examples:
        avr repo mirror place gsc-fi
        avr repo mirror place gsc-fi --repo acme/widgets

    \b
    JSON FIELDS
        enabled, full_name, placements, repository_id
    """
    if handle_json_meta(json_fields, jq_expr, _GIT_MIRROR_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id, required=True)

    try:
        mirror = client.public_put(
            f"{_GIT_MIRROR_BASE.format(org_id=org_id, repo_id=repo_id)}/placements/{quote(cluster_id, safe='')}"
        )
    except httpx.HTTPStatusError as exc:
        handle_http_error(
            exc,
            "place the git mirror",
            hint="run `avr repo mirror clusters` to list valid cluster ids",
        )

    if json_fields is not None:
        emit_json_record(mirror, split_fields(json_fields, _GIT_MIRROR_FIELDS), _GIT_MIRROR_FIELDS, jq_expr)
        return

    _print_git_mirror(mirror)


@mirror.command("unplace")
@click.argument("cluster_id")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--yes", "-y", is_flag=True, help="Skip the confirmation prompt.")
@json_options
@click.pass_context
def mirror_unplace(ctx, cluster_id, repo_id, org_id, yes, json_fields, jq_expr):
    """Remove the repository's mirror from a git cluster.

    The mirrored data in that cluster is dropped; other placements are
    unaffected. Requires the organization admin role.

    \b
    Examples:
        avr repo mirror unplace gsc-fi
        avr repo mirror unplace gsc-fi --repo acme/widgets --yes

    \b
    JSON FIELDS
        enabled, full_name, placements, repository_id
    """
    if handle_json_meta(json_fields, jq_expr, _GIT_MIRROR_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id, required=True)

    if not yes:
        ensure_prompts_allowed("removing a git-mirror placement needs confirmation")
        click.confirm(f"Remove the git-mirror placement in {cluster_id}?", abort=True)

    try:
        mirror = client.public_delete(
            f"{_GIT_MIRROR_BASE.format(org_id=org_id, repo_id=repo_id)}/placements/{quote(cluster_id, safe='')}"
        )
    except httpx.HTTPStatusError as exc:
        handle_http_error(
            exc,
            "remove the git-mirror placement",
            hint="run `avr repo mirror status` to see current placements",
        )

    if json_fields is not None:
        emit_json_record(mirror or {}, split_fields(json_fields, _GIT_MIRROR_FIELDS), _GIT_MIRROR_FIELDS, jq_expr)
        return

    if mirror is None:
        click.echo(f"Removed the git-mirror placement in {cluster_id}.")
        return
    _print_git_mirror(mirror)


@mirror.command("clusters")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@json_options
@click.pass_context
def mirror_clusters(ctx, org_id, json_fields, jq_expr):
    """List the git clusters a mirror can be placed in.

    \b
    Examples:
        avr repo mirror clusters
        avr repo mirror clusters --json cluster_id,datacenter_id

    \b
    JSON FIELDS
        cluster_id, datacenter_id, name
    """
    if handle_json_meta(json_fields, jq_expr, _GIT_CLUSTER_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        clusters = client.public_get(f"/api/v1/orgs/{org_id}/git-clusters")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list git clusters")

    if json_fields is not None:
        emit_json(clusters, split_fields(json_fields, _GIT_CLUSTER_FIELDS), _GIT_CLUSTER_FIELDS, jq_expr)
        return

    if not clusters:
        click.echo("No git clusters available.")
        return

    output_list(
        clusters,
        columns=["cluster_id", "datacenter_id", "name"],
        column_labels=["Cluster", "Datacenter", "Name"],
    )
