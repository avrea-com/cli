"""Typed native workflow submission and bounded receipt following."""

from avrea_cli.api_client import ApiClient
from avrea_cli.helpers import handle_http_error
from decimal import Decimal
from decimal import InvalidOperation
from pydantic import BaseModel
from pydantic import Field
from pydantic import PrivateAttr
from pydantic import ValidationError
from typing import Annotated
from typing import Any
from typing import Literal
from uuid import UUID
from uuid import uuid4
import click
import httpx
import json
import shlex
import time


class NativeDispatch(BaseModel):
    dispatch_id: str = Field(pattern=r"^wap-[0-9a-f]{32}$")
    state: Literal["pending", "admitted", "rejected"]
    reason: str | None = None
    run_ids: list[Annotated[str, Field(pattern=r"^awr-[0-9a-f]{32}$")]]
    _retry_command: str | None = PrivateAttr(default=None)


def _receipt(response: dict[str, Any]) -> NativeDispatch:
    try:
        return NativeDispatch.model_validate(response.get("data"))
    except ValidationError as exc:
        raise click.ClickException("The API returned an invalid native dispatch receipt.") from exc


def _input_json(value: Any) -> str:
    # Preserve decimal text until the API checks its numeric contract; a
    # float round-trip could turn an invalid input into a different valid one.
    if isinstance(value, Decimal):
        if not value.is_finite():
            raise click.ClickException("Native workflow numbers must be finite.")
        return str(value)
    if isinstance(value, (str, bool, int)):
        return json.dumps(value)
    raise click.ClickException("Native workflow inputs must be strings, booleans, or finite numbers.")


def submit_native_workflow(
    client: ApiClient,
    organization_id: str,
    repository_id: str,
    workflow_id: str,
    ref: str | None,
    fields: dict[str, str],
    raw_json: str | None,
    invocation_key: UUID | None,
) -> NativeDispatch:
    base = f"/orgs/{organization_id}/repos/{repository_id}"
    metadata: dict[str, Any] = {}
    if ref is None or fields:
        try:
            metadata = client.public_get(
                f"{base}/workflows/{workflow_id}/dispatch-metadata", params={"ref": ref} if ref else None
            ).get("data", {})
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "read native workflow inputs")
        except httpx.TransportError as exc:
            raise click.ClickException("Could not read native workflow inputs; no dispatch was submitted.") from exc
    ref = ref or metadata.get("ref")
    if not ref:
        raise click.ClickException("Could not determine the workflow's default branch. Pass --ref explicitly.")
    values: dict[str, Any]
    if raw_json is not None:
        values = json.loads(raw_json, parse_float=Decimal, parse_constant=Decimal)
    else:
        types = {item["name"]: item["type"] for item in metadata.get("inputs", [])}
        values = dict(fields)
        for name, value in fields.items():
            match types.get(name, "string"):
                case "boolean":
                    if value.lower() not in ("true", "false"):
                        raise click.ClickException(f"Input {name!r} must be true or false.")
                    values[name] = value.lower() == "true"
                case "number":
                    try:
                        values[name] = Decimal(value)
                    except InvalidOperation as exc:
                        raise click.ClickException(f"Input {name!r} must be a number.") from exc
                case "string" | "choice" | "environment" | "":
                    pass
                case kind:
                    raise click.ClickException(f"Unsupported workflow input type: {kind}")
    inputs = "{" + ",".join(f"{json.dumps(name)}:{_input_json(value)}" for name, value in values.items()) + "}"
    content = f'{{"workflow_id":{json.dumps(workflow_id)},"ref":{json.dumps(ref)},"inputs":{inputs}}}'
    key = (invocation_key or uuid4()).hex
    retry_command = (
        "printf '%s' "
        + shlex.quote(inputs)
        + " | "
        + shlex.join(
            [
                "env",
                f"AVR_HOST={client.config.public_api_url}",
                "avr",
                "workflow",
                "run",
                workflow_id,
                "--org",
                organization_id,
                "--repo",
                repository_id,
                "--ref",
                ref,
                "--json",
                "--idempotency-key",
                key,
            ]
        )
    )
    click.echo(f"Invocation key: {key}", err=True)
    try:
        response = client.public_post(f"{base}/workflow-dispatches", content=content, idempotency_key=key)
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code >= 500 or exc.response.status_code == 429:
            click.echo(f"Retry the exact request:\n{retry_command}", err=True)
        handle_http_error(exc, "dispatch native workflow")
    except httpx.TransportError as exc:
        raise click.ClickException(f"Dispatch response was lost. Retry the exact request:\n{retry_command}") from exc
    try:
        receipt = _receipt(response)
    except click.ClickException:
        click.echo(f"Retry the exact request:\n{retry_command}", err=True)
        raise
    receipt._retry_command = retry_command
    return receipt


def follow_native_dispatch(client: ApiClient, base: str, receipt: NativeDispatch, *, timeout: float = 90) -> str:
    dispatch_id = receipt.dispatch_id
    deadline = time.monotonic() + timeout
    while True:
        match receipt.state:
            case "admitted":
                if len(receipt.run_ids) != 1:
                    raise click.ClickException("The admitted dispatch did not identify exactly one native run.")
                return receipt.run_ids[0]
            case "rejected":
                raise click.ClickException(
                    f"Native dispatch {dispatch_id} was rejected: {receipt.reason or 'unknown reason'}"
                )
            case "pending":
                pass
            case _:
                raise click.ClickException("Unsupported native dispatch state.")
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            retry = f" Retry the exact request:\n{receipt._retry_command}" if receipt._retry_command else ""
            raise click.ClickException(f"Dispatch {dispatch_id} is still pending.{retry}")
        try:
            next_receipt = _receipt(
                client.public_get(f"{base}/workflow-dispatches/{dispatch_id}", timeout=min(client.timeout, remaining))
            )
            if next_receipt.dispatch_id != dispatch_id:
                raise click.ClickException("The API returned a different dispatch receipt.")
            next_receipt._retry_command = receipt._retry_command
            receipt = next_receipt
            if receipt.state != "pending":
                continue
        except httpx.HTTPStatusError as exc:
            if exc.response.status_code < 500 and exc.response.status_code != 429:
                handle_http_error(exc, "follow native dispatch")
        except httpx.TransportError:
            pass
        time.sleep(min(1, max(0, deadline - time.monotonic())))
