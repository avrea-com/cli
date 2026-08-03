"""Organization CLI commands with install and email-domain subgroups."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.commands.org_install_runner import GITLAB_WRITE_TIMEOUT_SECONDS
from avrea_cli.commands.org_install_runner import runner
from avrea_cli.config import CliConfig
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import ensure_prompts_allowed
from avrea_cli.helpers import exit_on_transport_error
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_org_slug
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import read_secret
from avrea_cli.json_output import emit_committed_record
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
    "platform",
    "platform_settings_url",
    "target_id",
    "target_type",
    "target_name",
    "organization_name",
    "organization_slug",
    "state",
    "created_at",
    "configured_runner_count",
    "degraded_runner_count",
)
_GITLAB_INSTALLATION_FIELDS = make_schema(
    "installation_id",
    "organization_id",
    "gitlab_namespace_id",
    "gitlab_namespace_full_path",
    "target_type",
    "state",
    "credential_kind",
    "access_token_expires_on",
    "access_token_last_validated_at",
    "access_token_invalid_at",
    "webhook_secret_rotated_at",
    "configured_runner_count",
    "degraded_runner_count",
    "project_selection_mode",
    "selected_project_count",
    "gitlab_project_ids",
    "created_at",
    "updated_at",
)
_GITLAB_INSTALLATION_WRITE_FIELDS = make_schema(*_GITLAB_INSTALLATION_FIELDS, "webhook_setup")
_INSTALL_REMOVE_FIELDS = make_schema(
    "installation_id",
    "organization_id",
    "platform",
    action="_local:action",
)
_RUNNER_COUNT_FIELDS = ("configured_runner_count", "degraded_runner_count")
_GITLAB_NOT_ENABLED_HINT = (
    "GitLab is not enabled for this organization, the installation belongs to another organization, "
    "or the ID is not a GitLab installation. Check it with 'avr org install list'."
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
    """Manage GitHub and GitLab installations."""
    ensure_ctx(ctx)


install.add_command(runner)


@install.command("list")
@json_options
@click.pass_context
def install_list(ctx, json_fields, jq_expr):
    """List accessible installations across all your organizations.

    \b
    JSON FIELDS
        created_at, installation_id, organization_name, organization_slug,
        platform, platform_installation_id, platform_settings_url, state,
        target_id, target_name, target_type, configured_runner_count, degraded_runner_count
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
    except httpx.RequestError as exc:
        exit_on_transport_error(exc, "fetch installations")

    # The runner counts live on the org-scoped GitLab listing; skip those
    # round-trips when the caller's --json fields would discard them anyway.
    wants_counts = json_fields is None or any(
        f in _RUNNER_COUNT_FIELDS for f in split_fields(json_fields, _INSTALL_FIELDS)
    )
    if wants_counts:
        summaries = _gitlab_runner_summaries(client, installations)
        for row in installations:
            summary = summaries.get(row["installation_id"], {})
            for field in _RUNNER_COUNT_FIELDS:
                row[field] = summary.get(field)

    if json_fields is not None:
        emit_json(installations, split_fields(json_fields, _INSTALL_FIELDS), _INSTALL_FIELDS, jq_expr)
        return

    for inst in installations:
        inst["org_name"] = inst.get("organization_name") or inst.get("organization_slug") or "-"
        inst["created_display"] = format_timestamp(inst.get("created_at"))
        inst["platform_display"] = {"github": "GitHub", "gitlab": "GitLab"}.get(
            str(inst.get("platform", "")).lower(), inst.get("platform") or "-"
        )
        inst["provider_id"] = inst.get("platform_installation_id") or inst.get("target_id") or "-"
        for field in _RUNNER_COUNT_FIELDS:
            # A GitHub row has no runner counts; a None cell would print as "None".
            inst[f"{field}_display"] = "-" if inst.get(field) is None else inst[field]

    columns = [
        "installation_id",
        "platform_display",
        "provider_id",
        "target_name",
        "org_name",
        "state",
        "created_display",
    ]
    labels = ["Installation ID", "Platform", "Provider ID", "Target", "Organization", "State", "Created"]
    if any(_platform(row) == "gitlab" for row in installations):
        columns += [f"{field}_display" for field in _RUNNER_COUNT_FIELDS]
        labels += ["Configured runners", "Degraded runners"]
    output_list(installations, columns=columns, column_labels=labels)


def _platform(row: dict) -> str:
    return str(row.get("platform") or "").lower()


def _gitlab_runner_summaries(client: ApiClient, installations: list[dict]) -> dict[str, dict]:
    """Runner counts per GitLab installation, keyed by installation ID.

    ``/users/me/installations`` carries no runner counts, so each organization
    with a GitLab installation is asked for its own listing. The counts are an
    optional add-on to a list already in hand, so a failed listing only leaves
    that organization's counts empty: silently when this user may not read it
    (403) or it no longer exists (404), with a warning on stderr for anything
    else. Only a 401 is fatal, since every later request would fail the same way."""
    gitlab_orgs = {
        row["organization_id"] for row in installations if _platform(row) == "gitlab" and row.get("organization_id")
    }
    summaries: dict[str, dict] = {}
    for organization_id in sorted(gitlab_orgs):
        try:
            details = client.public_get(f"/orgs/{quote(organization_id, safe='')}/gitlab-installations")
        except httpx.HTTPStatusError as exc:
            status = exc.response.status_code
            if status == 401:
                handle_http_error(exc, "fetch GitLab runner counts")
            if status not in (403, 404):
                _warn_runner_counts_skipped(organization_id, f"HTTP {status}")
            continue
        except httpx.RequestError:
            _warn_runner_counts_skipped(organization_id, "Avrea could not be reached")
            continue
        summaries.update({row["installation_id"]: row for row in details["data"]})
    return summaries


def _warn_runner_counts_skipped(organization_id: str, reason: str) -> None:
    click.echo(f"Warning: runner counts for {organization_id} are unavailable ({reason}).", err=True)


@install.command("add")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option(
    "--platform",
    type=click.Choice(["github", "gitlab"], case_sensitive=False),
    default="github",
    show_default=True,
    help="Source platform to install.",
)
@click.option("--group-full-path", help="GitLab group full path, such as acme/platform.")
@click.option(
    "--project-selection",
    type=click.Choice(["all", "selected"], case_sensitive=False),
    help="GitLab project selection mode.",
)
@click.option(
    "--project-id",
    "project_ids",
    type=click.IntRange(min=1),
    multiple=True,
    help="GitLab numeric project ID to select (repeatable, up to 1,000).",
)
@click.option(
    "--access-token-stdin",
    is_flag=True,
    help="Read the GitLab group access token from stdin instead of prompting.",
)
@click.option("--no-browser", is_flag=True, help="Do not open browser automatically.")
@click.option("--wait-seconds", type=int, default=120, show_default=True, help="Seconds to wait for detection.")
@json_options
@click.pass_context
def install_add(
    ctx,
    org_id: str | None,
    platform: str,
    group_full_path: str | None,
    project_selection: str | None,
    project_ids: tuple[int, ...],
    access_token_stdin: bool,
    no_browser: bool,
    wait_seconds: int,
    json_fields: str | None,
    jq_expr: str | None,
):
    """Add a GitHub or GitLab installation.

    GitHub is the default and retains the browser-based GitHub App flow.

    For GitLab, create an expiring gitlab.com group access token with Reporter
    role, api, and read_repository. The token is read from a hidden prompt
    or stdin and is never accepted as a command-line value.

    \b
    Examples:
        avr org install add
        avr org install add --platform gitlab --group-full-path acme/platform --project-selection all
        printf '%s\\n' "$GITLAB_TOKEN" | avr org install add --platform gitlab \\
            --group-full-path acme/platform --project-selection selected \\
            --project-id 123 --access-token-stdin

    With --json, GitHub emits the installations detected within --wait-seconds
    as a list and keeps progress messages on stderr; GitLab emits the
    registered installation, and the field list or jq filter must keep
    webhook_setup, which carries the one-time webhook secret.

    \b
    JSON FIELDS (GitHub)
        created_at, installation_id, organization_name, organization_slug,
        platform, platform_installation_id, platform_settings_url, state,
        target_id, target_name, target_type, configured_runner_count, degraded_runner_count

    \b
    JSON FIELDS (GitLab)
        access_token_expires_on, access_token_invalid_at,
        access_token_last_validated_at, created_at, credential_kind,
        gitlab_namespace_full_path, gitlab_namespace_id, gitlab_project_ids,
        installation_id, organization_id, project_selection_mode, configured_runner_count,
        degraded_runner_count, selected_project_count, state, target_type,
        updated_at, webhook_secret_rotated_at, webhook_setup
    """
    platform = platform.lower()
    if platform == "github":
        json_schema = _INSTALL_FIELDS
    elif platform == "gitlab":
        json_schema = _GITLAB_INSTALLATION_WRITE_FIELDS
    else:
        raise ValueError(f"Unhandled platform: {platform}")
    if handle_json_meta(json_fields, jq_expr, json_schema):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    if platform == "gitlab":
        _validate_gitlab_add_options(
            ctx,
            group_full_path=group_full_path,
            project_selection=project_selection,
            project_ids=project_ids,
            no_browser=no_browser,
        )
        assert group_full_path is not None
        selection_body = _gitlab_selection_body(project_selection, project_ids)
        org_id = get_org_id(config, org_id, client=client)
        write_fields = _gitlab_write_fields(json_fields)
        access_token = _read_gitlab_access_token(access_token_stdin)
        try:
            result = client.public_post(
                f"/orgs/{quote(org_id, safe='')}/gitlab-installations",
                json={
                    "group_full_path": group_full_path.strip(),
                    "access_token": access_token,
                    **selection_body,
                },
                timeout=GITLAB_WRITE_TIMEOUT_SECONDS,
            )
        except httpx.HTTPStatusError as exc:
            handle_http_error(
                exc,
                "register GitLab installation",
                hints={404: "GitLab is not enabled for this organization."},
            )
        except httpx.RequestError as exc:
            exit_on_transport_error(exc, "register GitLab installation")

        if write_fields is not None:
            _emit_gitlab_write_json(result, write_fields, jq_expr, "The GitLab installation was registered")
            return
        _print_gitlab_write_result(result, "GitLab installation configured")
        return
    if platform != "github":
        raise ValueError(f"Unhandled platform: {platform}")

    invalid_gitlab_options = []
    if group_full_path is not None:
        invalid_gitlab_options.append("--group-full-path")
    if project_selection is not None:
        invalid_gitlab_options.append("--project-selection")
    if project_ids:
        invalid_gitlab_options.append("--project-id")
    if access_token_stdin:
        invalid_gitlab_options.append("--access-token-stdin")
    if invalid_gitlab_options:
        raise click.UsageError(f"{', '.join(invalid_gitlab_options)} can only be used with --platform gitlab")
    if json_fields is not None and wait_seconds <= 0:
        raise click.UsageError("--json needs a positive --wait-seconds with --platform github")
    # Under --json, stdout carries only the detected installations.
    progress_to_stderr = json_fields is not None

    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get("/users/me/installations")
        before = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch installations")

    install_url = f"{config.public_api_url}/oauth/github/install?organization_id={org_id}"

    click.echo(f"GitHub installation URL: {install_url}", err=progress_to_stderr)
    if not no_browser:
        if webbrowser.open(install_url):
            click.echo("Opened browser for GitHub installation flow.", err=progress_to_stderr)
        else:
            click.echo("Unable to open browser automatically. Please open the URL manually.", err=True)

    if wait_seconds <= 0:
        click.echo("Skipping wait for installation detection. Re-run 'avr org install list' to verify.")
        return

    click.echo("Waiting for installation to appear...", err=progress_to_stderr)
    seen_ids = {
        inst["platform_installation_id"]
        for inst in before
        if str(inst.get("platform", "github")).lower() == "github" and inst.get("platform_installation_id") is not None
    }
    deadline = time.time() + wait_seconds
    while time.time() < deadline:
        time.sleep(5)
        try:
            response = client.public_get("/users/me/installations")
            current = response["data"]
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "poll installations")
        current_ids = {
            inst["platform_installation_id"]
            for inst in current
            if str(inst.get("platform", "github")).lower() == "github"
            and inst.get("platform_installation_id") is not None
        }
        added = current_ids - seen_ids
        if added:
            if json_fields is not None:
                emit_json(
                    [inst for inst in current if inst.get("platform_installation_id") in added],
                    split_fields(json_fields, _INSTALL_FIELDS),
                    _INSTALL_FIELDS,
                    jq_expr,
                )
                return
            click.echo(f"Detected new installation(s): {', '.join(str(gh_id) for gh_id in sorted(added))}")
            return

    click.echo(
        "Installation not detected yet. Complete the GitHub flow and rerun 'avr org install list' to verify.",
        err=True,
    )
    if json_fields is not None:
        emit_json([], split_fields(json_fields, _INSTALL_FIELDS), _INSTALL_FIELDS, jq_expr)


@install.command("set-selection")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--installation-id", required=True, help="GitLab installation ID (ins-xxx format).")
@click.option(
    "--project-selection",
    required=True,
    type=click.Choice(["all", "selected"], case_sensitive=False),
    help="Project selection mode.",
)
@click.option(
    "--project-id",
    "project_ids",
    type=click.IntRange(min=1),
    multiple=True,
    help="GitLab numeric project ID to select (repeatable, up to 1,000).",
)
@json_options
@click.pass_context
def install_set_selection(ctx, org_id, installation_id, project_selection, project_ids, json_fields, jq_expr):
    """Replace a GitLab installation's project selection.

    Use `--project-selection selected` without `--project-id` to select no
    projects. This updates the selection only; the next project sync applies
    it.

    \b
    JSON FIELDS
        access_token_expires_on, access_token_invalid_at,
        access_token_last_validated_at, created_at, credential_kind,
        gitlab_namespace_full_path, gitlab_namespace_id, gitlab_project_ids,
        installation_id, organization_id, project_selection_mode, configured_runner_count,
        degraded_runner_count, selected_project_count, state, target_type,
        updated_at, webhook_secret_rotated_at
    """
    if handle_json_meta(json_fields, jq_expr, _GITLAB_INSTALLATION_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    selection_body = _gitlab_selection_body(project_selection, project_ids)
    org_id = get_org_id(config, org_id, client=client)

    try:
        result = client.public_patch(
            _gitlab_installation_path(org_id, installation_id),
            json=selection_body,
            timeout=GITLAB_WRITE_TIMEOUT_SECONDS,
        )
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "update GitLab project selection", hints={404: _GITLAB_NOT_ENABLED_HINT})
    except httpx.RequestError as exc:
        exit_on_transport_error(exc, "update GitLab project selection")

    if result is None:
        raise click.ClickException("GitLab project selection update returned no installation details")
    if json_fields is not None:
        emit_json_record(
            result,
            split_fields(json_fields, _GITLAB_INSTALLATION_FIELDS),
            _GITLAB_INSTALLATION_FIELDS,
            jq_expr,
        )
        return
    click.echo(
        format_key_value(
            {
                "Installation ID": result["installation_id"],
                "GitLab group": result["gitlab_namespace_full_path"],
                "Project selection": result["project_selection_mode"],
                "Selected project count": result["selected_project_count"],
                "GitLab project IDs": ", ".join(str(project_id) for project_id in result["gitlab_project_ids"])
                or "none",
            },
            "GitLab project selection updated",
        )
    )


@install.command("rotate")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--installation-id", required=True, help="GitLab installation ID (ins-xxx format).")
@click.option(
    "--access-token-stdin",
    is_flag=True,
    help="Read the GitLab group access token from stdin instead of prompting.",
)
@json_options
@click.pass_context
def install_rotate(ctx, org_id, installation_id, access_token_stdin, json_fields, jq_expr):
    """Rotate a GitLab installation's access token and webhook secret.

    Create an expiring gitlab.com group access token with Reporter role,
    api, and read_repository. The new token is read from a hidden prompt
    or stdin and is never accepted as a command-line value.

    With --json, the field list or jq filter must keep webhook_setup, which
    carries the one-time webhook secret.

    \b
    JSON FIELDS
        access_token_expires_on, access_token_invalid_at,
        access_token_last_validated_at, created_at, credential_kind,
        gitlab_namespace_full_path, gitlab_namespace_id, gitlab_project_ids,
        installation_id, organization_id, project_selection_mode, configured_runner_count,
        degraded_runner_count, selected_project_count, state, target_type,
        updated_at, webhook_secret_rotated_at, webhook_setup
    """
    if handle_json_meta(json_fields, jq_expr, _GITLAB_INSTALLATION_WRITE_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    write_fields = _gitlab_write_fields(json_fields)
    access_token = _read_gitlab_access_token(access_token_stdin)

    try:
        result = client.public_post(
            f"{_gitlab_installation_path(org_id, installation_id)}/credential-rotations",
            json={"access_token": access_token},
            timeout=GITLAB_WRITE_TIMEOUT_SECONDS,
        )
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "rotate GitLab installation credentials", hints={404: _GITLAB_NOT_ENABLED_HINT})
    except httpx.RequestError as exc:
        exit_on_transport_error(exc, "rotate GitLab installation credentials")

    if write_fields is not None:
        _emit_gitlab_write_json(result, write_fields, jq_expr, "The GitLab credentials were rotated")
        return
    _print_gitlab_write_result(result, "GitLab credentials rotated")


@install.command("remove")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--installation-id", required=True, help="Installation ID to remove (ins-xxx format)")
@click.option("--yes", "-y", "confirmed", is_flag=True, help="Skip confirmation prompt.")
@json_options
@click.pass_context
def install_remove(ctx, org_id: str | None, installation_id: str, confirmed: bool, json_fields, jq_expr):
    """Remove an installation using its provider-specific lifecycle.

    GitHub installations are suspended. GitLab installations are permanently
    disconnected, including their Avrea history; use `avr org install rotate`
    for a credential problem instead. Pass --yes to skip confirmation.

    \b
    JSON FIELDS
        action, installation_id, organization_id, platform
    """
    if handle_json_meta(json_fields, jq_expr, _INSTALL_REMOVE_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    installation_path = f"/orgs/{quote(org_id, safe='')}/installations/{quote(installation_id, safe='')}"
    try:
        response = client.public_get(installation_path)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch installation before removal")
    installation = response.get("data", response)

    platform = _platform(installation)
    if platform == "gitlab":
        action = "disconnected"
        if not confirmed:
            ensure_prompts_allowed("permanent GitLab installation disconnect requires confirmation")
            click.echo(
                "This permanently disconnects the GitLab installation and removes its Avrea history. "
                "It cannot be undone. Use 'avr org install rotate' instead for credential problems.",
                err=True,
            )
            click.confirm(f"Permanently disconnect installation {installation_id} from {org_id}?", abort=True)
    elif platform == "github":
        action = "suspended"
        if not confirmed:
            ensure_prompts_allowed("installation remove suspends the GitHub App for this org")
            click.confirm(f"Suspend installation {installation_id} for {org_id}?", abort=True)
    else:
        raise click.ClickException(f"Installation {installation_id} has unsupported platform {platform or 'unknown'}")

    try:
        client.public_delete(installation_path)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "remove installation")

    if json_fields is not None:
        installation["_local:action"] = action
        emit_json_record(
            installation,
            split_fields(json_fields, _INSTALL_REMOVE_FIELDS),
            _INSTALL_REMOVE_FIELDS,
            jq_expr,
        )
        return
    if platform == "gitlab":
        click.echo(f"Installation {installation_id} permanently disconnected.")
        click.echo(
            "Remove the Avrea webhook, the group access token and the Avrea group runners from GitLab "
            "if they still exist."
        )
    elif platform == "github":
        click.echo(f"Installation {installation_id} suspended.")
    else:
        raise ValueError(f"Unhandled platform: {platform}")


def _validate_gitlab_add_options(
    ctx,
    *,
    group_full_path: str | None,
    project_selection: str | None,
    project_ids: tuple[int, ...],
    no_browser: bool,
) -> None:
    if group_full_path is None or not group_full_path.strip():
        raise click.UsageError("--group-full-path is required with --platform gitlab")
    if project_selection is None:
        raise click.UsageError("--project-selection is required with --platform gitlab")
    incompatible = []
    if no_browser:
        incompatible.append("--no-browser")
    if ctx.get_parameter_source("wait_seconds") == click.core.ParameterSource.COMMANDLINE:
        incompatible.append("--wait-seconds")
    if incompatible:
        raise click.UsageError(f"{', '.join(incompatible)} cannot be used with --platform gitlab")


def _gitlab_selection_body(project_selection: str | None, project_ids: tuple[int, ...]) -> dict:
    if project_selection is None:
        raise click.UsageError("--project-selection is required")
    if len(project_ids) > 1000:
        raise click.UsageError("--project-id may be supplied at most 1,000 times")
    mode = project_selection.upper()
    if mode == "ALL" and project_ids:
        raise click.UsageError("--project-id cannot be used with --project-selection all")
    return {
        "project_selection_mode": mode,
        "gitlab_project_ids": list(project_ids),
    }


def _read_gitlab_access_token(from_stdin: bool) -> str:
    return read_secret(
        from_stdin,
        "GitLab group access token",
        disabled_hint="GitLab access token input requires a hidden prompt or --access-token-stdin",
    )


def _gitlab_installation_path(org_id: str, installation_id: str) -> str:
    return f"/orgs/{quote(org_id, safe='')}/gitlab-installations/{quote(installation_id, safe='')}"


def _gitlab_write_fields(json_fields: str | None) -> list[str] | None:
    """Resolve the ``--json`` field list of a registration or rotation before
    the request is sent. Both commit a one-time webhook secret on the server,
    so a projection that drops ``webhook_setup`` is refused here, while
    refusing is still free, and not after the old secret has been invalidated."""
    if json_fields is None:
        return None
    fields = split_fields(json_fields, _GITLAB_INSTALLATION_WRITE_FIELDS)
    if "webhook_setup" not in fields:
        raise click.UsageError(
            "--json must include webhook_setup: it carries the webhook secret, which is shown only once."
        )
    return fields


def _emit_gitlab_write_json(result: dict, fields: list[str], jq_expr: str | None, committed: str) -> None:
    """Emit a registration or rotation result the server has already committed.

    A jq filter that drops the one-time webhook secret, or fails on it, must
    not lose it: the record then goes to stdout unfiltered and jq's own
    diagnostic, which may quote the secret, is not repeated (see ``token create``)."""
    secret = (result.get("webhook_setup") or {}).get("secret") or None
    emit_committed_record(
        result,
        fields,
        _GITLAB_INSTALLATION_WRITE_FIELDS,
        jq_expr,
        required_value=secret,
        committed=committed,
        save_what="webhook secret",
    )


def _print_gitlab_write_result(result: dict, title: str) -> None:
    click.echo(
        format_key_value(
            {
                "Installation ID": result["installation_id"],
                "GitLab group": result["gitlab_namespace_full_path"],
                "Token expires": result["access_token_expires_on"],
                "Project selection": result["project_selection_mode"],
                "Selected project count": result["selected_project_count"],
            },
            title,
        )
    )
    setup = result["webhook_setup"]
    click.echo("\nConfigure the GitLab group webhook with these exact settings:")
    click.echo(
        format_key_value(
            {
                "URL": setup["url"],
                "Secret header": setup["secret_header"],
                "Secret (shown once)": setup["secret"],
                "Enabled triggers": ", ".join(setup["enabled_triggers"]),
                "SSL verification": "enabled" if setup["ssl_verification"] else "disabled",
                "Unlisted triggers": "enabled" if setup["unlisted_triggers_enabled"] else "disabled",
            }
        )
    )
    click.echo("Save the webhook secret now: Avrea cannot show it again.", err=True)
    click.echo(setup["warning"], err=True)
