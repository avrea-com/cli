"""Organization CLI commands with install and email-domain subgroups."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
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
from urllib.parse import quote
import click
import httpx
import time
import webbrowser

_ORG_FIELDS = make_schema("organization_id", "name", "slug", "role")
_MEMBER_FIELDS = make_schema("user_id", "name", "role", "joined_at")
_EMAIL_DOMAIN_FIELDS = make_schema(
    "organization_email_domain_id",
    "domain",
    "created_at",
    "verified",
    "verified_at",
    "dns_record_name",
    "dns_record_value",
)
_SAML_FIELDS = make_schema(
    "organization_saml_config_id",
    "organization_id",
    "idp_entity_id",
    "idp_sso_url",
    "idp_slo_url",
    "name_id_format",
    "attr_email",
    "attr_given_name",
    "attr_family_name",
    "attr_groups",
    "is_enforced",
    "jit_provisioning",
    "allow_idp_initiated",
    "default_role",
    "created_at",
    "updated_at",
)
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
    """Claim and verify organization email domains."""
    ensure_ctx(ctx)


@email_domain.command("list")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@json_options
@click.pass_context
def email_domain_list(ctx, org_id, json_fields, jq_expr):
    """List claimed organization email domains (admin only).

    \b
    JSON FIELDS
        created_at, dns_record_name, dns_record_value, domain,
        organization_email_domain_id, verified, verified_at
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
        d["status"] = "Verified" if d.get("verified") else "Pending"
        if d.get("dns_record_name") and d.get("dns_record_value"):
            d["dns_record"] = f"{d['dns_record_name']} TXT {d['dns_record_value']}"
        else:
            d["dns_record"] = "-"

    output_list(
        domains,
        columns=["domain", "status", "dns_record", "organization_email_domain_id", "created_display"],
        column_labels=["Domain", "Status", "DNS Record", "ID", "Created"],
    )


@email_domain.command("claim")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.argument("domain")
@json_options
@click.pass_context
def email_domain_claim(ctx, org_id, domain, json_fields, jq_expr):
    """Claim a company domain using DNS ownership verification (admin only).

    The domain does not need to match your GitHub or Avrea account email.
    Publish the returned TXT record, then run ``email-domain verify``.

    \b
    Examples:
        avr org email-domain claim example.com
        avr org email-domain claim corp.example.com --org acme

    \b
    JSON FIELDS
        created_at, dns_record_name, dns_record_value, domain,
        organization_email_domain_id, verified, verified_at
    """
    if handle_json_meta(json_fields, jq_expr, _EMAIL_DOMAIN_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_post(f"/orgs/{org_id}/email-domains/claim", json={"domain": domain})
        result = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "claim email domain")

    if json_fields is not None:
        emit_json_record(result, split_fields(json_fields, _EMAIL_DOMAIN_FIELDS), _EMAIL_DOMAIN_FIELDS, jq_expr)
        return

    click.echo(
        format_key_value(
            {
                "Domain": result["domain"],
                "Status": "Verified" if result.get("verified") else "Pending",
                "DNS record name": result.get("dns_record_name") or "-",
                "DNS record type": "TXT" if result.get("dns_record_name") else "-",
                "DNS record value": result.get("dns_record_value") or "-",
            }
        )
    )
    if not result.get("verified"):
        click.echo(f"\nAfter publishing the TXT record, run:\n  avr org email-domain verify {result['domain']}")


@email_domain.command("verify")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.argument("domain")
@json_options
@click.pass_context
def email_domain_verify(ctx, org_id, domain, json_fields, jq_expr):
    """Check a claimed domain's DNS TXT record (admin only).

    Each invocation performs a fresh DNS lookup. If DNS has not propagated,
    wait and run the command again.

    \b
    Examples:
        avr org email-domain verify example.com
        avr org email-domain verify corp.example.com --org acme

    \b
    JSON FIELDS
        created_at, dns_record_name, dns_record_value, domain,
        organization_email_domain_id, verified, verified_at
    """
    if handle_json_meta(json_fields, jq_expr, _EMAIL_DOMAIN_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        encoded_domain = quote(domain, safe="")
        response = client.public_post(f"/orgs/{org_id}/email-domains/{encoded_domain}/verify")
        result = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "verify email domain")

    if json_fields is not None:
        emit_json_record(result, split_fields(json_fields, _EMAIL_DOMAIN_FIELDS), _EMAIL_DOMAIN_FIELDS, jq_expr)
        return

    click.echo(
        format_key_value(
            {
                "Domain": result["domain"],
                "Status": "Verified" if result.get("verified") else "Pending",
                "Verified at": format_timestamp(result.get("verified_at")),
            }
        )
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
# SAML subgroup
# ---------------------------------------------------------------------------


def _echo_saml_config(config: dict) -> None:
    click.echo(
        format_key_value(
            {
                "IdP entity ID": config["idp_entity_id"],
                "IdP SSO URL": config["idp_sso_url"],
                "IdP SLO URL": config.get("idp_slo_url") or "-",
                "NameID format": config["name_id_format"],
                "Email attribute": config["attr_email"],
                "Given-name attribute": config.get("attr_given_name") or "-",
                "Family-name attribute": config.get("attr_family_name") or "-",
                "Groups attribute": config.get("attr_groups") or "-",
                "Default role": config["default_role"],
                "JIT provisioning": "enabled" if config.get("jit_provisioning") else "disabled",
                "IdP-initiated login": "enabled" if config.get("allow_idp_initiated") else "disabled",
                "SSO enforcement": "enabled" if config.get("is_enforced") else "disabled",
                "Updated": format_timestamp(config.get("updated_at")),
            }
        )
    )


@org.group("saml", cls=GhGroup)
@click.pass_context
def saml(ctx):
    """Configure SAML single sign-on for an organization."""
    ensure_ctx(ctx)


@saml.command("sp-metadata")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.pass_context
def saml_sp_metadata(ctx, org_id):
    """Print Avrea's SAML service-provider metadata XML.

    Redirect stdout to a file for import into your identity provider.

    \b
    Examples:
        avr org saml sp-metadata > avrea-sp.xml
        avr org saml sp-metadata --org acme
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    org_slug = get_org_slug(client, org_id)
    if org_slug == org_id:
        raise click.ClickException(f"Could not resolve organization slug for {org_id}.")

    try:
        metadata = client.public_get_text(f"/saml/{quote(org_slug, safe='')}/metadata")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch SAML SP metadata")

    click.echo(metadata, nl=not metadata.endswith("\n"))


@saml.command("show")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@json_options
@click.pass_context
def saml_show(ctx, org_id, json_fields, jq_expr):
    """Show the current SAML configuration (admin only).

    \b
    JSON FIELDS
        allow_idp_initiated, attr_email, attr_family_name, attr_given_name,
        attr_groups, created_at, default_role, idp_entity_id, idp_slo_url,
        idp_sso_url, is_enforced, jit_provisioning, name_id_format,
        organization_id, organization_saml_config_id, updated_at
    """
    if handle_json_meta(json_fields, jq_expr, _SAML_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(f"/orgs/{org_id}/saml")
        result = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch SAML configuration")

    if json_fields is not None:
        emit_json_record(result, split_fields(json_fields, _SAML_FIELDS), _SAML_FIELDS, jq_expr)
        return
    _echo_saml_config(result)


@saml.command("test")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--no-browser", is_flag=True, help="Print the test URL without opening a browser.")
@click.pass_context
def saml_test(ctx, org_id, no_browser):
    """Test the SAML connection in a browser (admin only).

    The test performs a real IdP sign-in and displays the parsed assertion
    without creating a new Avrea session.

    \b
    Examples:
        avr org saml test
        avr org saml test --org acme --no-browser
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    org_slug = get_org_slug(client, org_id)
    if org_slug == org_id:
        raise click.ClickException(f"Could not resolve organization slug for {org_id}.")
    test_url = f"{config.public_api_url}/saml/{quote(org_slug, safe='')}/test/login"

    click.echo(test_url)
    if no_browser:
        return
    if webbrowser.open(test_url):
        click.echo("Opened browser for SAML connection test.")
    else:
        click.echo("Unable to open browser automatically. Please open the URL manually.", err=True)


@saml.command("configure")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option(
    "--email-attribute",
    "--attr-email",
    "attr_email",
    default="email",
    show_default=True,
    help="IdP attribute carrying the member email.",
)
@click.option("--given-name-attribute", "--attr-given-name", "attr_given_name", help="IdP given-name attribute.")
@click.option("--family-name-attribute", "--attr-family-name", "attr_family_name", help="IdP family-name attribute.")
@click.option("--groups-attribute", "--attr-groups", "attr_groups", help="IdP groups attribute.")
@click.option(
    "--default-role",
    type=click.Choice(["user", "admin", "billing_admin"], case_sensitive=False),
    default="user",
    show_default=True,
    help="Role assigned to JIT-provisioned members.",
)
@click.option(
    "--jit-provisioning/--no-jit-provisioning",
    default=True,
    show_default=True,
    help="Allow SAML to provision new members.",
)
@click.option(
    "--allow-idp-initiated/--no-allow-idp-initiated",
    default=False,
    show_default=True,
    help="Allow sign-in initiated from the IdP.",
)
@click.argument("metadata", type=click.File("rb"))
@json_options
@click.pass_context
def saml_configure(
    ctx,
    org_id,
    attr_email,
    attr_given_name,
    attr_family_name,
    attr_groups,
    default_role,
    jit_provisioning,
    allow_idp_initiated,
    metadata,
    json_fields,
    jq_expr,
):
    """Create or replace SAML configuration from IdP metadata (admin only).

    METADATA is an IdP metadata XML file; pass - to read it from stdin.
    Reconfiguring requires the complete metadata document again.

    \b
    Examples:
        avr org saml configure idp-metadata.xml
        cat idp-metadata.xml | avr org saml configure - --org acme
        avr org saml configure idp.xml --email-attribute mail \\
            --given-name-attribute firstName --family-name-attribute lastName

    \b
    JSON FIELDS
        allow_idp_initiated, attr_email, attr_family_name, attr_given_name,
        attr_groups, created_at, default_role, idp_entity_id, idp_slo_url,
        idp_sso_url, is_enforced, jit_provisioning, name_id_format,
        organization_id, organization_saml_config_id, updated_at
    """
    if handle_json_meta(json_fields, jq_expr, _SAML_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    params = {
        "attr_email": attr_email,
        "default_role": default_role,
        "jit_provisioning": jit_provisioning,
        "allow_idp_initiated": allow_idp_initiated,
    }
    if attr_given_name:
        params["attr_given_name"] = attr_given_name
    if attr_family_name:
        params["attr_family_name"] = attr_family_name
    if attr_groups:
        params["attr_groups"] = attr_groups

    try:
        response = client.public_post(
            f"/orgs/{org_id}/saml",
            content=metadata.read(),
            params=params,
            content_type="application/xml",
        )
        result = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "configure SAML")

    if json_fields is not None:
        emit_json_record(result, split_fields(json_fields, _SAML_FIELDS), _SAML_FIELDS, jq_expr)
        return
    _echo_saml_config(result)


@saml.command("enforcement")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.argument("state", type=click.Choice(["on", "off"], case_sensitive=False))
@json_options
@click.pass_context
def saml_enforcement(ctx, org_id, state, json_fields, jq_expr):
    """Enable or disable mandatory SAML sign-in (admin only).

    Enabling requires a configured SAML connection and at least one verified
    company domain.

    \b
    Examples:
        avr org saml enforcement on
        avr org saml enforcement off --org acme
    """
    if handle_json_meta(json_fields, jq_expr, _SAML_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_post(
            f"/orgs/{org_id}/saml/enforcement",
            json={"enforce": state.lower() == "on"},
        )
        result = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "change SAML enforcement")

    if json_fields is not None:
        emit_json_record(result, split_fields(json_fields, _SAML_FIELDS), _SAML_FIELDS, jq_expr)
        return
    click.echo(f"SAML enforcement is now {'enabled' if result['is_enforced'] else 'disabled'} for {org_id}.")


@saml.command("remove")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--yes", "-y", "confirmed", is_flag=True, help="Skip confirmation prompt.")
@click.pass_context
def saml_remove(ctx, org_id, confirmed):
    """Remove the organization's SAML configuration (admin only).

    Pass --yes to skip the confirmation prompt (required when prompts are
    disabled for automation).
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    if not confirmed:
        ensure_prompts_allowed("removing SAML configuration requires confirmation")
        click.confirm(f"Remove SAML configuration for {org_id}?", abort=True)

    try:
        client.public_delete(f"/orgs/{org_id}/saml")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "remove SAML configuration")

    click.echo(f"Removed SAML configuration for {org_id}.")


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
