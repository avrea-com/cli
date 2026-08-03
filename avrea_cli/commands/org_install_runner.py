"""Configure a dedicated GitLab group runner for each Avrea machine type."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import EXIT_INTERRUPTED
from avrea_cli.click_ext import GhGroup
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import ensure_prompts_allowed
from avrea_cli.helpers import exit_on_transport_error
from avrea_cli.helpers import extract_detail
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import read_secret
from avrea_cli.json_output import emit_json_record
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import json_options
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from avrea_cli.output import format_key_value
from avrea_cli.output import output_list
from typing import Any
from urllib.parse import quote
import click
import httpx

_RUNNER_FIELDS = make_schema(
    "machine_type",
    "tag",
    "architecture",
    "cpu_count",
    "memory_mb",
    "disk_gb",
    "description",
    "run_untagged",
    "paused",
    "configured",
    "status",
    "runner_id",
    "token_expires_at",
    "verified_at",
    "invalid_at",
)
_COLLECTION_FIELDS = make_schema("data", "setup")
_DELETE_FIELDS = make_schema("installation_id", "machine_type", "deleted")
# GitLab writes (installation registration, credential rotation, runner
# configuration) validate against gitlab.com under a 60-second server
# deadline; the client waits a little longer so the server's own answer,
# not a client timeout, reports the outcome. ``org.py`` imports this.
GITLAB_WRITE_TIMEOUT_SECONDS = 70.0
_GITLAB_RUNNER_CREATE_URL = "https://gitlab.com/api/v4/user/runners"


def _scope(ctx: click.Context, org: str | None, installation: str) -> tuple[ApiClient, str]:
    ensure_authenticated(ctx.obj["config"])
    client = ctx.obj["client"]
    org_id = get_org_id(ctx.obj["config"], org, client=client)
    return client, f"/orgs/{quote(org_id, safe='')}/gitlab-installations/{quote(installation, safe='')}/runners"


def _token(from_stdin: bool, description: str, stdin_flag: str) -> str:
    return read_secret(from_stdin, description, disabled_hint=f"{description} requires a hidden prompt or {stdin_flag}")


class _InterruptedCreateError(click.ClickException):
    """Ctrl-C after a GitLab runner was created: same message, exit 130."""

    exit_code = EXIT_INTERRUPTED


def _unconfigured_runner_hint(runner_id: int) -> str:
    return (
        f" GitLab runner {runner_id} exists but is not configured in Avrea: "
        "remove it in GitLab, or reset its token there and use 'runner set'."
    )


def _avrea_put_error(exc: httpx.HTTPStatusError, runner_id: int, machine_type: str) -> click.ClickException:
    """The error for an Avrea PUT that failed after GitLab created the runner.

    A 4xx is a definite rejection, so the message says so and carries the
    server's detail; a 5xx may have committed on the server, so it keeps the
    uncertain wording of a transport failure."""
    status = exc.response.status_code
    if status >= 500:
        return click.ClickException(_unconfirmed_message(runner_id, machine_type))
    detail = extract_detail(exc.response)
    detail_suffix = f": {detail}" if detail else "."
    if status == 403:
        lead = (
            f"You don't have permission to configure GitLab runners for this installation in Avrea "
            f"(HTTP 403){detail_suffix}"
        )
    else:
        lead = f"Avrea rejected the runner configuration for {machine_type} (HTTP {status}){detail_suffix}"
    return click.ClickException(lead + _unconfigured_runner_hint(runner_id))


def _unconfirmed_message(runner_id: int, machine_type: str) -> str:
    return (
        f"GitLab runner {runner_id} was created for {machine_type}. "
        "Avrea configuration could not be confirmed. "
        "Check Avrea before retrying; if unconfigured, reset its token in GitLab and use 'runner set'."
    )


@click.group("runner", cls=GhGroup)
@click.pass_context
def runner(ctx: click.Context) -> None:
    """Manage GitLab runners, one per Linux machine type."""
    ensure_ctx(ctx)


@runner.command("list")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--installation-id", required=True, help="GitLab installation ID (ins-xxx format).")
@json_options
@click.pass_context
def runner_list(
    ctx: click.Context, org_id: str | None, installation_id: str, json_fields: str | None, jq_expr: str | None
) -> None:
    """List all machine types, runner status, and exact GitLab setup instructions.

    \b
    JSON FIELDS
        data, setup
    """
    if handle_json_meta(json_fields, jq_expr, _COLLECTION_FIELDS):
        return
    client, path = _scope(ctx, org_id, installation_id)
    try:
        result = client.public_get(path)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list GitLab runners")
    except httpx.RequestError as exc:
        exit_on_transport_error(exc, "list GitLab runners")
    if json_fields is not None:
        emit_json_record(result, split_fields(json_fields, _COLLECTION_FIELDS), _COLLECTION_FIELDS, jq_expr)
        return
    rows = result["data"]
    output_list(
        rows,
        columns=["machine_type", "tag", "status", "runner_id", "token_expires_at", "verified_at"],
        column_labels=["Machine type", "Tag", "Status", "Runner ID", "Token expiry", "Last verified"],
    )
    setup = result["setup"]
    click.echo(f"\nCreate group runners in {setup['group_full_path']} itself, one per required machine type.")
    click.echo("Dedicate these runners to Avrea. Do not register their tokens on your own runner managers.")
    click.echo("Use exactly the listed tag on each runner and exactly one tag on each CI job.")
    for row in rows:
        click.echo(
            f"  {row['machine_type']}: description={row['description']}; "
            f"run_untagged={row['run_untagged']}; paused={row['paused']}"
        )
    click.echo(setup["protected_branches"])
    click.echo(setup["fallback_image"])
    click.echo(f"\n.gitlab-ci.yml example:\n{setup['job_example']}")
    click.echo(f"Documentation: {setup['documentation_url']}")


@runner.command("set")
@click.argument("machine_type")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--installation-id", required=True, help="GitLab installation ID (ins-xxx format).")
@click.option("--runner-token-stdin", is_flag=True, help="Read the runner authentication token from standard input.")
@json_options
@click.pass_context
def runner_set(
    ctx: click.Context,
    machine_type: str,
    org_id: str | None,
    installation_id: str,
    runner_token_stdin: bool,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Verify and configure MACHINE_TYPE, replacing its previous runner if present.

    Read the token from a hidden prompt or --runner-token-stdin.

    \b
    JSON FIELDS
        architecture, configured, cpu_count, description, disk_gb,
        invalid_at, machine_type, memory_mb, paused, run_untagged,
        runner_id, status, tag, token_expires_at, verified_at
    """
    if handle_json_meta(json_fields, jq_expr, _RUNNER_FIELDS):
        return
    client, path = _scope(ctx, org_id, installation_id)
    token = _token(runner_token_stdin, "GitLab runner authentication token", "--runner-token-stdin")
    try:
        result = client.public_put(
            f"{path}/{quote(machine_type, safe='')}", json={"runner_token": token}, timeout=GITLAB_WRITE_TIMEOUT_SECONDS
        )
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "configure GitLab runner")
    except httpx.RequestError as exc:
        exit_on_transport_error(exc, "configure GitLab runner")
    if json_fields is not None:
        emit_json_record(result, split_fields(json_fields, _RUNNER_FIELDS), _RUNNER_FIELDS, jq_expr)
        return
    click.echo(
        format_key_value(
            {"Machine type": result["machine_type"], "Tag": result["tag"], "Status": result["status"]},
            "Runner configured",
        )
    )


@runner.command("delete")
@click.argument("machine_type")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--installation-id", required=True, help="GitLab installation ID (ins-xxx format).")
@click.option("--yes", "-y", "confirmed", is_flag=True, help="Skip the confirmation prompt.")
@json_options
@click.pass_context
def runner_delete(
    ctx: click.Context,
    machine_type: str,
    org_id: str | None,
    installation_id: str,
    confirmed: bool,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Remove MACHINE_TYPE's runner configuration from Avrea.

    The runner object in GitLab remains for the group Owner to remove.

    \b
    JSON FIELDS
        deleted, installation_id, machine_type
    """
    if handle_json_meta(json_fields, jq_expr, _DELETE_FIELDS):
        return
    client, path = _scope(ctx, org_id, installation_id)
    if not confirmed:
        ensure_prompts_allowed("runner removal requires confirmation; use --yes for scripts")
        click.confirm(f"Remove {machine_type} runner configuration from {installation_id}?", abort=True)
    try:
        client.public_delete(f"{path}/{quote(machine_type, safe='')}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "remove GitLab runner")
    except httpx.RequestError as exc:
        exit_on_transport_error(exc, "remove GitLab runner")
    if json_fields is not None:
        emit_json_record(
            {"installation_id": installation_id, "machine_type": machine_type, "deleted": True},
            split_fields(json_fields, _DELETE_FIELDS),
            _DELETE_FIELDS,
            jq_expr,
        )
        return
    click.echo(
        f"Runner configuration removed for {machine_type}. Remove its GitLab runner separately if no longer needed."
    )


def _create_runner(
    http: httpx.Client, access_token: str, group_id: int, machine_type: dict[str, Any], protected: bool
) -> tuple[int, str]:
    try:
        response = http.post(
            _GITLAB_RUNNER_CREATE_URL,
            headers={"PRIVATE-TOKEN": access_token},
            json={
                "runner_type": "group_type",
                "group_id": group_id,
                "tag_list": machine_type["tag"],
                "description": machine_type["description"],
                "run_untagged": machine_type["run_untagged"],
                "paused": machine_type["paused"],
                "access_level": "ref_protected" if protected else "not_protected",
            },
            timeout=30,
        )
    except httpx.RequestError:
        raise click.ClickException(
            "Could not reach GitLab. Check the group's runners before retrying creation."
        ) from None
    if response.status_code != 201:
        raise click.ClickException(
            f"GitLab runner creation failed (HTTP {response.status_code}). Check Owner role and create_runner scope."
        )
    try:
        result = response.json()
        runner_id, token = result["id"], result["token"]
        if type(runner_id) is not int or runner_id <= 0 or not isinstance(token, str) or not token:
            raise ValueError("Invalid runner metadata")
    except ValueError, KeyError, TypeError:
        raise click.ClickException(
            "GitLab returned invalid runner metadata. Check the group's runners before retrying."
        ) from None
    return runner_id, token


@runner.command("create")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option("--installation-id", required=True, help="GitLab installation ID (ins-xxx format).")
@click.option(
    "--machine-type",
    "machine_types",
    multiple=True,
    required=True,
    help="Machine type to create; repeat for multiple machine types.",
)
@click.option("--protected", is_flag=True, help="Restrict the runners to protected branches and tags.")
@click.option(
    "--personal-access-token-stdin",
    is_flag=True,
    help="Read your personal access token from standard input instead of a hidden prompt.",
)
@json_options
@click.pass_context
def runner_create(
    ctx: click.Context,
    org_id: str | None,
    installation_id: str,
    machine_types: tuple[str, ...],
    protected: bool,
    personal_access_token_stdin: bool,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Create group runners in GitLab and configure their machine types in Avrea.

    Supply your own personal access token with create_runner scope and Owner
    role. It is sent only to gitlab.com and is never persisted. Already
    configured machine types must be replaced with 'runner set'.

    \b
    JSON FIELDS
        data, setup
    """
    if handle_json_meta(json_fields, jq_expr, _COLLECTION_FIELDS):
        return
    client, path = _scope(ctx, org_id, installation_id)
    try:
        collection = client.public_get(path)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "read GitLab runner setup")
    except httpx.RequestError as exc:
        exit_on_transport_error(exc, "read GitLab runner setup")
    available = {row["machine_type"]: row for row in collection["data"]}
    for machine_type in machine_types:
        if machine_type not in available:
            raise click.UsageError(f"Unknown runner machine type: {machine_type}")
        if available[machine_type]["configured"]:
            raise click.UsageError(
                f"Machine type {machine_type} is already configured; use 'runner set' to replace it."
            )
    token = _token(
        personal_access_token_stdin,
        "GitLab personal access token (create_runner scope)",
        "--personal-access-token-stdin",
    )
    configured: list[dict[str, Any]] = []
    with httpx.Client(follow_redirects=False) as http:
        for machine_type in dict.fromkeys(machine_types):
            try:
                runner_id, runner_token = _create_runner(
                    http, token, collection["setup"]["group_id"], available[machine_type], protected
                )
                # From here on the runner exists in GitLab, so every exit must
                # name it: the ID is what the user needs to clean up or retry.
                try:
                    result = client.public_put(
                        f"{path}/{quote(machine_type, safe='')}",
                        json={"runner_token": runner_token},
                        timeout=GITLAB_WRITE_TIMEOUT_SECONDS,
                    )
                except httpx.HTTPStatusError as exc:
                    raise _avrea_put_error(exc, runner_id, machine_type) from None
                except httpx.RequestError:
                    raise click.ClickException(_unconfirmed_message(runner_id, machine_type)) from None
                except KeyboardInterrupt:
                    raise _InterruptedCreateError(
                        "Interrupted. " + _unconfirmed_message(runner_id, machine_type)
                    ) from None
            except click.ClickException as exc:
                # Under --json the per-type progress lines are not printed, so
                # the error must say which earlier types are already done.
                if configured and json_fields is not None:
                    done = ", ".join(row["machine_type"] for row in configured)
                    exc.message += f" Already configured before this failure: {done}."
                raise
            configured.append(result)
            if json_fields is None:
                click.echo(f"Configured {machine_type} with GitLab runner {runner_id}.")
    if json_fields is not None:
        emit_json_record(
            {"data": configured, "setup": collection["setup"]},
            split_fields(json_fields, _COLLECTION_FIELDS),
            _COLLECTION_FIELDS,
            jq_expr,
        )
        return
