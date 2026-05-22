"""CLI configuration commands (avr config)."""

from avrea_cli import auth
from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import get_org_slug
from avrea_cli.helpers import handle_http_error
from avrea_cli.repo_context import detect_repo_from_git
from urllib.parse import urlparse
import click
import httpx
import os


def _fetch_me(client: ApiClient) -> dict | None:
    """Best-effort fetch of /users/me; returns None on any error."""
    try:
        response = client.public_get("/users/me")
        return response.get("data") or response
    except httpx.HTTPStatusError, httpx.ConnectError, httpx.TimeoutException:
        return None


def _src(label: str) -> str:
    return click.style(f"(from {label})", dim=True)


def _print_config_status(ctx) -> None:
    cfg: CliConfig = ctx.obj["config"]
    client: ApiClient = ctx.obj["client"]

    # Header: API host with source annotation
    host = urlparse(cfg.public_api_url).hostname or cfg.public_api_url
    if os.getenv("AVR_HOST"):
        host_src = _src("AVR_HOST")
    elif auth.load_default_host():
        host_src = _src("hosts.json")
    else:
        host_src = _src("default")
    click.echo(f"{click.style(host, bold=True)} {host_src}")

    # Auth status line
    if cfg.auth_token:
        me = _fetch_me(client)
        token_src = _src("AVR_TOKEN" if os.getenv("AVR_TOKEN") else "hosts.json")
        if me and me.get("email"):
            click.echo(f"  {click.style('✓', fg='green')} authenticated as {me['email']} {token_src}")
        elif me:
            click.echo(f"  {click.style('✓', fg='green')} authenticated {token_src}")
        else:
            click.echo(f"  {click.style('!', fg='yellow')} token set but /users/me failed {token_src}")
    else:
        click.echo(f"  {click.style('✗', fg='red')} not authenticated (run: avr auth login)")

    # Active org — show slug if we can resolve it
    if cfg.default_org:
        if cfg.auth_token:
            slug = get_org_slug(client, cfg.default_org)
            if slug != cfg.default_org:
                active_org = f"{click.style(slug, bold=True)} ({cfg.default_org})"
            else:
                active_org = click.style(cfg.default_org, bold=True)
        else:
            active_org = click.style(cfg.default_org, bold=True)
        org_src = _src("AVR_ORG" if os.getenv("AVR_ORG") else "hosts.json")
        active_org = f"{active_org} {org_src}"
    else:
        active_org = click.style("(not set)", dim=True)

    # Default repo — explicit override or auto-detected from git remote
    if cfg.repo_override:
        default_repo = f"{click.style(cfg.repo_override, bold=True)} {_src('AVR_REPO')}"
    else:
        detected = detect_repo_from_git()
        if detected:
            default_repo = f"{click.style(detected, bold=True)} {_src('git remote')}"
        else:
            default_repo = click.style("(no git remote; pass --repo explicitly)", dim=True)

    click.echo(f"  - Active org:   {active_org}")
    click.echo(f"  - Default repo: {default_repo}")


@click.group(cls=GhGroup, invoke_without_command=True)
@click.pass_context
def config(ctx):
    """View and manage CLI configuration."""
    ensure_ctx(ctx)
    if ctx.invoked_subcommand is None:
        _print_config_status(ctx)


@config.command("set")
@click.argument("key", type=click.Choice(["org"]))
@click.argument("value")
@click.pass_context
def config_set(ctx, key: str, value: str):
    """Set a CLI configuration value.

    \b
    Available keys:
      org   Active organization ID

    \b
    Examples:
        avr config set org org-abc123
    """
    client: ApiClient = ctx.obj["client"]
    cfg: CliConfig = ctx.obj["config"]
    ensure_authenticated(cfg)

    if key == "org":
        # Verify the org exists and user has access
        try:
            response = client.public_get("/users/me/organizations")
            organizations = response["data"]
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "fetch organizations")

        org_ids = [o["organization_id"] for o in organizations]
        if value not in org_ids:
            click.echo(f"Error: Organization '{value}' not found or you don't have access.", err=True)
            click.echo("Available organizations:", err=True)
            for o in organizations:
                click.echo(f"  {o['organization_id']} ({o['name']})", err=True)
            raise click.Abort()

        auth.store_default_org(value, host=cfg.public_api_url)
        org_name = next((o["name"] for o in organizations if o["organization_id"] == value), value)
        click.echo(f"Default organization set to: {org_name} ({value})")


@config.command("get")
@click.argument("key", type=click.Choice(["org"]))
@click.pass_context
def config_get(ctx, key: str):
    """Print the value of a configuration key.

    \b
    Available keys:
      org   Active organization ID
    """
    cfg: CliConfig = ctx.obj["config"]
    values = {"org": cfg.default_org or ""}
    click.echo(values[key])


@config.command("list")
@click.pass_context
def config_list(ctx):
    """Show the active CLI configuration (host, auth, org, default repo)."""
    _print_config_status(ctx)


@config.command("unset")
@click.argument("key", type=click.Choice(["org"]))
@click.pass_context
def config_unset(ctx, key: str):
    """Remove a configuration override.

    \b
    Available keys:
      org   Drop the stored default organization for the active host

    \b
    Examples:
        avr config unset org
    """
    cfg: CliConfig = ctx.obj["config"]
    if key == "org":
        # Match the brevity of `config set`: the host is implied by AVR_HOST
        # / the active default. Showing it would be redundant in the common
        # single-host install.
        if auth.clear_default_org(host=cfg.public_api_url):
            click.echo("Cleared default organization.")
        else:
            click.echo("No default organization was set.")
