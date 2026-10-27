"""List, download and delete private workflow artifacts."""

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
from avrea_cli.output import output_list
from avrea_cli.repo_context import resolve_repo_or_detect
from collections.abc import Callable
from pathlib import Path
from typing import Any
from typing import NoReturn
import click
import hashlib
import httpx
import os
import re
import tempfile

_FIELDS = make_schema(
    "id",
    "name",
    "repository_id",
    "workflow_run_id",
    "run_kind",
    "run_attempt",
    "size_bytes",
    "digest_sha256",
    "file_name",
    "file_count",
    "archived",
    "state",
    "created_at",
    "completed_at",
    "expires_at",
)
_DOWNLOAD_FIELDS = make_schema("id", "path", "size_bytes", "digest_sha256")
_DELETE_FIELDS = make_schema("id", "deleted")


@click.group(cls=GhGroup)
@click.pass_context
def artifact(ctx: click.Context) -> None:
    """Manage workflow artifacts."""
    ensure_ctx(ctx)


def _scope_options(func: Callable) -> Callable:
    func = click.option("--repo", help="Repository name or ID; defaults to the current git repository.")(func)
    return click.option("--org", help="Organization ID or slug; defaults to the configured organization.")(func)


def _scope(ctx: click.Context, org: str | None, repo: str | None) -> tuple[ApiClient, str]:
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo, required=True)
    return client, f"/orgs/{org_id}/repos/{repo_id}/artifacts"


def _artifact_id(value: str) -> str:
    if re.fullmatch(r"art-[0-9a-f]{32}", value) is None:
        raise click.BadParameter("Expected an Avrea art- ID")
    return value


def _artifact_error(exc: httpx.HTTPError) -> NoReturn:
    if isinstance(exc, httpx.HTTPStatusError):
        handle_http_error(exc, "access workflow artifact")
    raise click.ClickException("Artifact request failed: connection or stream interrupted") from None


def _record(record: dict[str, Any], fields: str | None, jq: str | None, schema: dict[str, str]) -> None:
    if fields:
        emit_json_record(record, split_fields(fields, schema), schema, jq)
    else:
        click.echo(format_key_value(record))


@artifact.command("list")
@click.argument("repo", metavar="REPO")
@click.option("--org", help="Organization ID or slug.")
@click.option("--run-id", help="Numeric GitHub run ID, run- ID, or native awr- ID.")
@click.option(
    "--limit", type=click.IntRange(1, 1000), default=50, show_default=True, help="Maximum artifacts per page."
)
@click.option("--cursor", help="Continue from a previous page's cursor.")
@json_options
@click.pass_context
def artifact_list(
    ctx: click.Context,
    repo: str,
    org: str | None,
    run_id: str | None,
    limit: int,
    cursor: str | None,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """List artifacts in REPO, optionally restricted to one run."""
    if handle_json_meta(json_fields, jq_expr, _FIELDS):
        return
    client, base = _scope(ctx, org, repo)
    params: dict[str, Any] = {"limit": limit}
    if run_id:
        params["run_id"] = f"eq.{run_id}"
    if cursor:
        params["cursor"] = cursor
    try:
        page = client.public_get(base, params=params)
    except httpx.HTTPError as exc:
        _artifact_error(exc)
        return
    if json_fields:
        emit_json(page["data"], split_fields(json_fields, _FIELDS), _FIELDS, jq_expr)
    else:
        output_list(page["data"], ["id", "name", "workflow_run_id", "size_bytes", "state", "expires_at"])
    if next_cursor := page.get("pagination", {}).get("next_cursor"):
        click.echo(f"More artifacts available. Continue with --cursor {next_cursor}", err=True)


@artifact.command("get")
@click.argument("artifact_id", metavar="ART_ID")
@_scope_options
@json_options
@click.pass_context
def artifact_get(
    ctx: click.Context,
    artifact_id: str,
    org: str | None,
    repo: str | None,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Get metadata for ART_ID in the selected repository."""
    if handle_json_meta(json_fields, jq_expr, _FIELDS):
        return
    client, base = _scope(ctx, org, repo)
    try:
        row = client.public_get(f"{base}/{_artifact_id(artifact_id)}")["data"]
    except httpx.HTTPError as exc:
        _artifact_error(exc)
        return
    _record(row, json_fields, jq_expr, _FIELDS)


@artifact.command("download")
@click.argument("artifact_id", metavar="ART_ID")
@_scope_options
@click.option(
    "--path",
    type=click.Path(path_type=Path),
    default=Path("."),
    show_default=True,
    help="Destination directory or filename. ZIPs stay archived.",
)
@json_options
@click.pass_context
def artifact_download(
    ctx: click.Context,
    artifact_id: str,
    org: str | None,
    repo: str | None,
    path: Path,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Download ART_ID and verify its size and SHA-256 before replacing the destination."""
    if handle_json_meta(json_fields, jq_expr, _DOWNLOAD_FIELDS):
        return
    client, base = _scope(ctx, org, repo)
    endpoint = f"{base}/{_artifact_id(artifact_id)}"
    try:
        row = client.public_get(endpoint)["data"]
        destination = _download(client, endpoint + "/download", row, path.expanduser())
    except httpx.HTTPError as exc:
        _artifact_error(exc)
        return
    except (OSError, ValueError) as exc:
        raise click.ClickException(str(exc)) from None
    _record(
        {
            "id": artifact_id,
            "path": str(destination),
            "size_bytes": row["size_bytes"],
            "digest_sha256": row["digest_sha256"],
        },
        json_fields,
        jq_expr,
        _DOWNLOAD_FIELDS,
    )


def _download(client: ApiClient, endpoint: str, row: dict[str, Any], destination: Path) -> Path:
    size, expected = row.get("size_bytes"), row.get("digest_sha256")
    if (
        row.get("state") != "complete"
        or not isinstance(size, int)
        or not 0 <= size <= 10 * 1024**3
        or not isinstance(expected, str)
        or re.fullmatch(r"[0-9a-f]{64}", expected) is None
    ):
        raise ValueError("Artifact is unavailable or has invalid integrity metadata")
    name = row["file_name"]
    if not isinstance(name, str) or name in {"", ".", ".."} or any(c in name for c in "/\\\x00\r\n"):
        raise ValueError("Artifact has an invalid filename")
    if destination.is_dir():
        destination /= name
    destination = destination.absolute()
    destination.parent.mkdir(parents=True, exist_ok=True)
    digest, received = hashlib.sha256(), 0
    temporary: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(dir=destination.parent, prefix=".avr-artifact-", delete=False) as output:
            temporary = Path(output.name)
            for chunk in client.public_download(endpoint):
                received += len(chunk)
                if received > size:
                    raise ValueError("Artifact exceeds its recorded size")
                digest.update(chunk)
                output.write(chunk)
        if received != size or digest.hexdigest() != expected:
            raise ValueError("Artifact size or SHA-256 verification failed")
        os.replace(temporary, destination)
        temporary = None
        return destination
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


@artifact.command("delete")
@click.argument("artifact_id", metavar="ART_ID")
@_scope_options
@click.option("--yes", "-y", is_flag=True, help="Skip confirmation.")
@json_options
@click.pass_context
def artifact_delete(
    ctx: click.Context,
    artifact_id: str,
    org: str | None,
    repo: str | None,
    yes: bool,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Delete ART_ID from the selected repository."""
    if handle_json_meta(json_fields, jq_expr, _DELETE_FIELDS):
        return
    client, base = _scope(ctx, org, repo)
    artifact_id = _artifact_id(artifact_id)
    if not yes:
        ensure_prompts_allowed("deleting an artifact")
        click.confirm(f"Delete {artifact_id}?", abort=True)
    try:
        client.public_delete(f"{base}/{artifact_id}")
    except httpx.HTTPError as exc:
        _artifact_error(exc)
        return
    _record({"id": artifact_id, "deleted": True}, json_fields, jq_expr, _DELETE_FIELDS)
