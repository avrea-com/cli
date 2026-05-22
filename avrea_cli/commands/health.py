"""Health check command."""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import CliConfig
from avrea_cli.json_output import emit_json_record
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import json_options
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from urllib.parse import urlparse
import click
import httpx

# Single-key schema for now; add explicit entries as the endpoint grows new
# keys (build hash, etc.) so `--json '*'` stays in sync with the typed surface.
_HEALTH_FIELDS = make_schema("status")


@click.command("health")
@json_options
@click.pass_context
def health(ctx, json_fields, jq_expr):
    """Check Avrea platform status.

    \b
    Examples:
        avr health
        avr health --json status
        avr health --json '*' -q '.status'
    """
    if handle_json_meta(json_fields, jq_expr, _HEALTH_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    host = urlparse(config.public_api_url).hostname or config.public_api_url
    is_json = json_fields is not None

    if not is_json:
        click.echo("Checking API health...")

    try:
        result = client.public_get("/health")
    except (httpx.HTTPStatusError, httpx.ConnectError, httpx.TimeoutException) as exc:
        reason = _humanize_health_failure(exc, host)
        if is_json:
            # Same JSON shape as success — schema-projected, no extra keys.
            # Humans get the friendly reason on stderr; consumers get the
            # stable {"status": "unreachable"} record.
            click.echo(f"API unreachable: {reason}", err=True)
            emit_json_record(
                {"status": "unreachable"},
                split_fields(json_fields, _HEALTH_FIELDS),
                _HEALTH_FIELDS,
                jq_expr,
            )
        else:
            click.echo(f"✗ API: {reason}", err=True)
        raise click.Abort() from None

    if is_json:
        emit_json_record(result, split_fields(json_fields, _HEALTH_FIELDS), _HEALTH_FIELDS, jq_expr)
        return

    status = result.get("status", "unknown")
    click.echo(f"✓ API: {status}")


def _humanize_health_failure(exc: Exception, host: str) -> str:
    if isinstance(exc, httpx.ConnectError):
        return f"couldn't reach {host} — check your network connection."
    if isinstance(exc, httpx.TimeoutException):
        return f"{host} took too long to respond."
    if isinstance(exc, httpx.HTTPStatusError):
        return f"HTTP {exc.response.status_code} from {host}."
    return f"{host} unreachable."
