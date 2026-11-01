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
    ref: str
    commit_sha: str
    _retry_command: str | None = PrivateAttr(default=None)

    def pending_message(self) -> str:
        message = f"Dispatch {self.dispatch_id} was accepted and is awaiting admission."
        if self._retry_command:
            message += f"\nResume following this dispatch:\n{self._retry_command}"
        return message


class NativeDispatchInput(BaseModel):
    name: str
    type: str


class AvreaRerun(BaseModel):
    run_id: str = Field(pattern=r"^awr-[0-9a-f]{32}$")
    run_attempt: int = Field(ge=2)
    status: Literal["rerun_requested"]


def submit_avrea_rerun(
    client: ApiClient,
    organization_id: str,
    run_id: str,
    invocation_key: UUID | None,
) -> AvreaRerun:
    key = (invocation_key or uuid4()).hex
    retry = shlex.join(
        [
            "env",
            f"AVR_HOST={client.config.public_api_url}",
            "avr",
            "run",
            "rerun",
            run_id,
            "--org",
            organization_id,
            "--idempotency-key",
            key,
            "--yes",
        ]
    )
    click.echo(f"Invocation key: {key}", err=True)
    try:
        for attempt in range(2):
            try:
                response = client.public_post(
                    f"/orgs/{organization_id}/workflow-runs/{run_id}/rerun",
                    json={"failed_only": False, "invocation_key": key},
                    timeout=60,
                )
                return AvreaRerun.model_validate(response.get("data"))
            except httpx.TransportError:
                if attempt == 1:
                    raise
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code >= 500 or exc.response.status_code == 429:
            click.echo(f"Retry the same request:\n{retry}", err=True)
        handle_http_error(exc, "rerun Avrea workflow")
    except (httpx.TransportError, ValidationError) as exc:
        raise click.ClickException(f"Could not confirm the rerun. Recover the same attempt:\n{retry}") from exc
    raise click.ClickException(f"Could not confirm the rerun. Retry the same request:\n{retry}")


class NativeDispatchMetadata(BaseModel):
    ref: str
    dispatchable: bool
    can_dispatch: bool
    inputs: list[NativeDispatchInput]


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
    metadata: NativeDispatchMetadata | None = None
    if ref is None or fields:
        try:
            response = client.public_get(
                f"{base}/workflows/{workflow_id}/dispatch-metadata", params={"ref": ref} if ref else None
            )
            metadata = NativeDispatchMetadata.model_validate(response.get("data"))
        except ValidationError as exc:
            raise click.ClickException("The API returned invalid native workflow metadata.") from exc
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "read native workflow inputs")
        except httpx.TransportError as exc:
            raise click.ClickException("Could not read native workflow inputs; no dispatch was submitted.") from exc
        if not metadata.dispatchable:
            raise click.ClickException("Manual dispatch is not enabled for this native workflow at the selected ref.")
        if not metadata.can_dispatch:
            raise click.ClickException("Native workflow dispatch is not permitted for this repository and user.")
    ref = ref or (metadata.ref if metadata is not None else None)
    if not ref:
        raise click.ClickException("Could not determine the workflow's default branch. Pass --ref explicitly.")
    values: dict[str, Any]
    if raw_json is not None:
        values = json.loads(raw_json, parse_float=Decimal, parse_constant=Decimal)
    else:
        types = {item.name: item.type for item in metadata.inputs} if metadata is not None else {}
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
        for attempt in range(2):
            try:
                # The server budgets 45 seconds for source and permission checks.
                response = client.public_post(
                    f"{base}/workflow-dispatches", content=content, idempotency_key=key, timeout=60
                )
                break
            except httpx.TransportError:
                # The identical keyed request recovers a receipt if the response was lost.
                if attempt == 1:
                    raise
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
            raise click.ClickException(receipt.pending_message())
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
            # Receipt reads are safe to retry after the delay until the deadline.
            pass
        time.sleep(min(1, max(0, deadline - time.monotonic())))
