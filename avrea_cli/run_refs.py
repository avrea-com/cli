"""Parse and resolve workflow-run references accepted by CLI commands."""

from avrea_cli.api_client import ApiClient
from dataclasses import dataclass
from enum import StrEnum
from typing import Any
from urllib.parse import SplitResult
from urllib.parse import urlsplit
import click
import re

_AVREA_RUN_ID_RE = re.compile(r"run-[A-Za-z0-9]+")
_ASCII_DECIMAL_RE = re.compile(r"[0-9]+")
_GITHUB_NAME_RE = re.compile(r"[A-Za-z0-9_.-]+")
_ORG_SLUG_RE = re.compile(r"[A-Za-z0-9-]+")
_PRODUCTION_CONSOLE_HOST = "console.avrea.com"
_PRODUCTION_API_URL = "https://api.avrea.com"


class RunReferenceKind(StrEnum):
    """Supported external and internal run-reference forms."""

    AVREA_ID = "avrea_id"
    GITHUB_ID = "github_id"
    GITHUB_URL = "github_url"
    AVREA_URL = "avrea_url"


@dataclass(frozen=True)
class RunReference:
    """Normalized workflow-run reference."""

    raw: str
    kind: RunReferenceKind
    run_id: str | None = None
    platform_run_id: int | None = None
    attempt: int | None = None
    repository_full_name: str | None = None
    organization_slug: str | None = None


def _invalid_reference() -> click.ClickException:
    return click.ClickException(
        "Invalid run reference. Expected an Avrea run ID, a positive GitHub run ID, "
        "a GitHub Actions run URL, or an Avrea console run URL."
    )


def _parse_trusted_https_url(value: str) -> SplitResult:
    if any(ord(character) < 0x20 or ord(character) == 0x7F for character in value):
        raise _invalid_reference()
    try:
        parts = urlsplit(value)
    except ValueError:
        raise _invalid_reference() from None
    try:
        port = parts.port
    except ValueError:
        raise _invalid_reference() from None
    if (
        parts.scheme != "https"
        or not parts.hostname
        or parts.username is not None
        or parts.password is not None
        or port is not None
        or parts.query
        or parts.fragment
    ):
        raise _invalid_reference()
    return parts


def _parse_github_url(value: str, parts: SplitResult) -> RunReference:
    path = parts.path.split("/")
    if len(path) not in {6, 8} or path[0] or path[3:5] != ["actions", "runs"]:
        raise _invalid_reference()

    owner, repository, raw_platform_run_id = path[1], path[2], path[5]
    if not _GITHUB_NAME_RE.fullmatch(owner) or not _GITHUB_NAME_RE.fullmatch(repository):
        raise _invalid_reference()
    if not _ASCII_DECIMAL_RE.fullmatch(raw_platform_run_id):
        raise _invalid_reference()
    try:
        platform_run_id = int(raw_platform_run_id)
    except ValueError:
        raise _invalid_reference() from None
    if platform_run_id < 1:
        raise _invalid_reference()

    attempt = None
    if len(path) == 8:
        raw_attempt = path[7]
        if path[6] != "attempts" or not _ASCII_DECIMAL_RE.fullmatch(raw_attempt):
            raise _invalid_reference()
        try:
            attempt = int(raw_attempt)
        except ValueError:
            raise _invalid_reference() from None
        if attempt < 1:
            raise _invalid_reference()

    return RunReference(
        raw=value,
        kind=RunReferenceKind.GITHUB_URL,
        platform_run_id=platform_run_id,
        attempt=attempt,
        repository_full_name=f"{owner}/{repository}",
    )


def _console_host_for_api_url(api_url: str) -> str | None:
    api_host = urlsplit(api_url).hostname
    if api_host is None or not api_host.startswith("api."):
        return None
    return f"console.{api_host.removeprefix('api.')}"


def _parse_avrea_url(value: str, parts: SplitResult, *, api_url: str) -> RunReference:
    if parts.hostname == _PRODUCTION_CONSOLE_HOST:
        required_api_url = _PRODUCTION_API_URL
    elif parts.hostname == _console_host_for_api_url(api_url):
        required_api_url = api_url
    else:
        raise _invalid_reference()
    active_api_host = urlsplit(api_url).hostname
    if active_api_host != urlsplit(required_api_url).hostname:
        raise click.ClickException(
            f"This run URL belongs to a different Avrea environment. "
            f"Switch with `avr auth switch {required_api_url}` and try again."
        )

    path = parts.path.split("/")
    if len(path) != 5 or path[0] or path[1] != "org" or path[3] != "runs":
        raise _invalid_reference()
    organization_slug, run_id = path[2], path[4]
    if not _ORG_SLUG_RE.fullmatch(organization_slug) or not _AVREA_RUN_ID_RE.fullmatch(run_id):
        raise _invalid_reference()

    return RunReference(
        raw=value,
        kind=RunReferenceKind.AVREA_URL,
        run_id=run_id,
        organization_slug=organization_slug,
    )


def parse_run_reference(value: str, *, api_url: str) -> RunReference:
    """Validate and normalize a CLI workflow-run argument."""
    if _AVREA_RUN_ID_RE.fullmatch(value):
        return RunReference(raw=value, kind=RunReferenceKind.AVREA_ID, run_id=value)
    if _ASCII_DECIMAL_RE.fullmatch(value):
        try:
            platform_run_id = int(value)
        except ValueError:
            raise _invalid_reference() from None
        if platform_run_id < 1:
            raise _invalid_reference()
        return RunReference(raw=value, kind=RunReferenceKind.GITHUB_ID, platform_run_id=platform_run_id)

    parts = _parse_trusted_https_url(value)
    if parts.hostname == "github.com":
        return _parse_github_url(value, parts)
    if parts.hostname == _PRODUCTION_CONSOLE_HOST or parts.hostname == _console_host_for_api_url(api_url):
        return _parse_avrea_url(value, parts, api_url=api_url)
    raise _invalid_reference()


def _unwrap_run(response: dict[str, Any]) -> dict[str, Any]:
    run = response.get("data", response)
    if not isinstance(run, dict):
        raise click.ClickException("Avrea returned an invalid workflow-run response.")
    return run


def _validate_github_repository(run: dict[str, Any], reference: RunReference) -> None:
    expected = reference.repository_full_name
    if expected is None:
        return
    repository = run.get("repository")
    actual = repository.get("full_name") if isinstance(repository, dict) else None
    if not isinstance(actual, str) or actual.casefold() != expected.casefold():
        raise click.ClickException(
            f"GitHub run URL repository {expected!r} does not match the Avrea run repository {actual!r}."
        )


def resolve_run_reference(
    client: ApiClient,
    org_id: str,
    reference: RunReference,
    *,
    include: list[str] | None = None,
) -> dict[str, Any]:
    """Resolve a normalized reference without scanning recent runs."""
    params: dict[str, Any] = {}
    if include:
        params["include"] = include

    if reference.run_id is not None:
        response = client.public_get(
            f"/orgs/{org_id}/workflow-runs/{reference.run_id}",
            params=params or None,
        )
        return _unwrap_run(response)

    platform_run_id = reference.platform_run_id
    if platform_run_id is None:
        raise _invalid_reference()

    if reference.attempt is None:
        response = client.public_get(
            f"/orgs/{org_id}/workflow-runs/by-platform-id/{platform_run_id}",
            params=params or None,
        )
        run = _unwrap_run(response)
    else:
        response = client.public_get(
            f"/orgs/{org_id}/workflow-runs",
            params={
                "platform_run_id": platform_run_id,
                "limit": 1000,
                "order": "created_at.desc",
                **params,
            },
        )
        runs = response.get("data")
        if not isinstance(runs, list):
            raise click.ClickException("Avrea returned an invalid workflow-run response.")
        run = next(
            (
                candidate
                for candidate in runs
                if isinstance(candidate, dict) and candidate.get("run_attempt") == reference.attempt
            ),
            None,
        )
        if run is None:
            raise click.ClickException(
                f"GitHub run {platform_run_id} has no visible attempt {reference.attempt} in this organization."
            )

    _validate_github_repository(run, reference)
    return run
