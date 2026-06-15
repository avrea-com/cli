"""Organization CLI commands with install and email-domain subgroups."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import ensure_prompts_allowed
from avrea_cli.helpers import get_org_id
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
import click
import httpx
import time
import webbrowser

_ORG_FIELDS = make_schema("organization_id", "name", "slug", "role")
_MEMBER_FIELDS = make_schema("user_id", "name", "role", "joined_at")
_EMAIL_DOMAIN_FIELDS = make_schema("organization_email_domain_id", "domain", "created_at")
_INSTALL_FIELDS = make_schema(
    "installation_id",
    "platform_installation_id",
    "target_name",
    "organization_name",
    "organization_slug",
    "state",
    "created_at",
)


@click.group(cls=GhGroup)
@click.pass_context
def org(ctx):
    """Manage organizations and installations."""
    ensure_ctx(ctx)


# ---------------------------------------------------------------------------
# Top-level org commands
# ---------------------------------------------------------------------------


@org.command("list")
@json_options
@click.pass_context
def org_list(ctx, json_fields, jq_expr):
    """List organizations you belong to.

    \b
    Examples:
        avr org list
        avr org list --json slug,role
        avr org list --json '*' -q '.[] | select(.role == "admin")'

    \b
    JSON FIELDS
        name, organization_id, role, slug
    """
    if handle_json_meta(json_fields, jq_expr, _ORG_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    try:
        response = client.public_get("/users/me/organizations")
        organizations = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch organizations")

    if json_fields is not None:
        emit_json(organizations, split_fields(json_fields, _ORG_FIELDS), _ORG_FIELDS, jq_expr)
        return

    default_org = config.default_org
    for o in organizations:
        o["default"] = "yes" if o["organization_id"] == default_org else ""

    output_list(
        organizations,
        columns=["organization_id", "name", "slug", "role", "default"],
        column_labels=["ID", "Name", "Slug", "Role", "Default"],
    )


@org.command("create")
@click.argument("name")
@json_options
@click.pass_context
def org_create(ctx, name: str, json_fields, jq_expr):
    """Create a new organization.

    \b
    JSON FIELDS
        name, organization_id, role, slug
    """
    if handle_json_meta(json_fields, jq_expr, _ORG_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    try:
        result = client.public_post("/users/me/organizations", json={"name": name})
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "create organization")

    if json_fields is not None:
        emit_json_record(result, split_fields(json_fields, _ORG_FIELDS), _ORG_FIELDS, jq_expr)
        return

    click.echo(
        format_key_value(
            {
                "Organization ID": result["organization_id"],
                "Name": result["name"],
                "Slug": result["slug"],
                "Role": result["role"],
            }
        )
    )


@org.command("members")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@json_options
@click.pass_context
def org_members(ctx, org_id, json_fields, jq_expr):
    """List organization members (admin only).

    \b
    Examples:
        avr org members
        avr org members --org org-abc123
        avr org members --json name,role

    \b
    JSON FIELDS
        joined_at, name, role, user_id
    """
    if handle_json_meta(json_fields, jq_expr, _MEMBER_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(f"/orgs/{org_id}/members")
        members = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list members")

    if json_fields is not None:
        emit_json(members, split_fields(json_fields, _MEMBER_FIELDS), _MEMBER_FIELDS, jq_expr)
        return

    for m in members:
        m["joined_display"] = format_timestamp(m.get("joined_at"))

    output_list(
        members,
        columns=["user_id", "name", "role", "joined_display"],
        column_labels=["User ID", "Name", "Role", "Joined"],
    )


# ---------------------------------------------------------------------------
# email-domain subgroup
# ---------------------------------------------------------------------------


@org.group("email-domain", cls=GhGroup)
@click.pass_context
def email_domain(ctx):
    """Manage email domains for automatic org membership."""
    ensure_ctx(ctx)


@email_domain.command("list")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@json_options
@click.pass_context
def email_domain_list(ctx, org_id, json_fields, jq_expr):
    """List email domains for automatic org membership (admin only).

    \b
    JSON FIELDS
        created_at, domain, organization_email_domain_id
    """
    if handle_json_meta(json_fields, jq_expr, _EMAIL_DOMAIN_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(f"/orgs/{org_id}/email-domains")
        domains = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list email domains")

    if json_fields is not None:
        emit_json(domains, split_fields(json_fields, _EMAIL_DOMAIN_FIELDS), _EMAIL_DOMAIN_FIELDS, jq_expr)
        return

    for d in domains:
        d["created_display"] = format_timestamp(d.get("created_at"))

    output_list(
        domains,
        columns=["domain", "organization_email_domain_id", "created_display"],
        column_labels=["Domain", "ID", "Created"],
    )


@email_domain.command("set")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--yes", "-y", "confirmed", is_flag=True, help="Skip confirmation prompt.")
@click.argument("domains", nargs=-1, required=True)
@click.pass_context
def email_domain_set(ctx, org_id, confirmed, domains):
    """Set email domains for automatic org membership (admin only).

    Replaces all existing domains — a typo wipes the org's auto-membership
    policy. Confirms before applying; pass --yes to skip the prompt (required
    when stdout isn't a TTY, e.g. in CI).

    \b
    Examples:
        avr org email-domain set example.com
        avr org email-domain set example.com corp.example.com --yes
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    if not confirmed:
        ensure_prompts_allowed("email-domain set replaces ALL existing domains")
        domain_list = ", ".join(domains)
        click.confirm(
            f"Replace ALL email domains for {org_id} with [{domain_list}]?",
            abort=True,
        )

    try:
        response = client.public_post(f"/orgs/{org_id}/email-domains", json={"domains": list(domains)})
        result = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "set email domains")

    click.echo(f"Set {len(result)} email domain(s) for organization {org_id}:")
    for d in result:
        click.echo(f"  {d['domain']}")


# ---------------------------------------------------------------------------
# install subgroup
# ---------------------------------------------------------------------------


@org.group("install", cls=GhGroup)
@click.pass_context
def install(ctx):
    """Manage GitHub App installations."""
    ensure_ctx(ctx)


@install.command("list")
@json_options
@click.pass_context
def install_list(ctx, json_fields, jq_expr):
    """List accessible installations across all your organizations.

    \b
    JSON FIELDS
        created_at, platform_installation_id, installation_id, organization_name,
        organization_slug, state, target_name
    """
    if handle_json_meta(json_fields, jq_expr, _INSTALL_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    try:
        response = client.public_get("/users/me/installations")
        installations = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch installations")

    if json_fields is not None:
        emit_json(installations, split_fields(json_fields, _INSTALL_FIELDS), _INSTALL_FIELDS, jq_expr)
        return

    for inst in installations:
        inst["org_name"] = inst.get("organization_name") or inst.get("organization_slug") or "-"
        inst["created_display"] = format_timestamp(inst.get("created_at"))

    output_list(
        installations,
        columns=["installation_id", "platform_installation_id", "target_name", "org_name", "state", "created_display"],
        column_labels=["Installation ID", "GitHub ID", "Target", "Organization", "State", "Created"],
    )


@install.command("add")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--no-browser", is_flag=True, help="Do not open browser automatically.")
@click.option("--wait-seconds", type=int, default=120, show_default=True, help="Seconds to wait for detection.")
@click.pass_context
def install_add(ctx, org_id: str | None, no_browser: bool, wait_seconds: int):
    """Start the GitHub App installation flow."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get("/users/me/installations")
        before = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch installations")

    install_url = f"{config.public_api_url}/oauth/github/install?organization_id={org_id}"

    click.echo(f"GitHub installation URL: {install_url}")
    if not no_browser:
        if webbrowser.open(install_url):
            click.echo("Opened browser for GitHub installation flow.")
        else:
            click.echo("Unable to open browser automatically. Please open the URL manually.", err=True)

    if wait_seconds <= 0:
        click.echo("Skipping wait for installation detection. Re-run 'avr org install list' to verify.")
        return

    click.echo("Waiting for installation to appear...")
    seen_ids = {inst["platform_installation_id"] for inst in before}
    deadline = time.time() + wait_seconds
    while time.time() < deadline:
        time.sleep(5)
        try:
            response = client.public_get("/users/me/installations")
            current = response["data"]
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "poll installations")
        current_ids = {inst["platform_installation_id"] for inst in current}
        added = current_ids - seen_ids
        if added:
            click.echo(f"Detected new installation(s): {', '.join(str(gh_id) for gh_id in sorted(added))}")
            return

    click.echo(
        "Installation not detected yet. Complete the GitHub flow and rerun 'avr org install list' to verify.",
        err=True,
    )


@install.command("remove")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--installation-id", required=True, help="Installation ID to remove (ins-xxx format)")
@click.option("--yes", "-y", "confirmed", is_flag=True, help="Skip confirmation prompt.")
@click.pass_context
def install_remove(ctx, org_id: str | None, installation_id: str, confirmed: bool):
    """Remove/suspend a GitHub installation.

    Confirms before suspending; pass --yes to skip the prompt (required when
    stdout isn't a TTY, e.g. in CI).
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    if not confirmed:
        ensure_prompts_allowed("installation remove suspends the GitHub App for this org")
        click.confirm(f"Suspend installation {installation_id} for {org_id}?", abort=True)

    try:
        client.public_delete(f"/orgs/{org_id}/installations/{installation_id}")
        click.echo(f"Installation {installation_id} suspended.")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "remove installation")
