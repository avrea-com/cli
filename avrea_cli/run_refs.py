"""Parse and resolve workflow-run references accepted by CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import CliConfig
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_verified_org_slug
from avrea_cli.helpers import is_scoped_token
from dataclasses import dataclass
from enum import StrEnum
from typing import Any
from urllib.parse import SplitResult
from urllib.parse import urlsplit
import click
import re

_AVREA_RUN_ID_RE = re.compile(r"(?:run|awr)-[0-9a-f]{32}")
_ASCII_DECIMAL_RE = re.compile(r"[0-9]+")
_GITHUB_NAME_RE = re.compile(r"[A-Za-z0-9_.-]+")
_ORG_SLUG_RE = re.compile(r"[A-Za-z0-9-]+")
_GITHUB_STEP_FRAGMENT_RE = re.compile(r"step:[0-9]+:[0-9]+")
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
    platform_job_id: int | None = None


class InvalidRunReference(click.ClickException):
    """The input does not have a supported run-reference shape."""


def _invalid_reference() -> InvalidRunReference:
    return InvalidRunReference(
        "Invalid run reference. Expected an Avrea run ID, a positive GitHub run ID, "
        "a GitHub Actions run or job URL, or an Avrea console run URL."
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
    ):
        raise _invalid_reference()
    return parts


def _parse_github_url(value: str, parts: SplitResult) -> RunReference:
    path = parts.path.removesuffix("/").split("/")
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
    platform_job_id = None
    if len(path) == 8:
        segment, raw_value = path[6], path[7]
        if segment not in {"attempts", "job"} or not _ASCII_DECIMAL_RE.fullmatch(raw_value):
            raise _invalid_reference()
        try:
            parsed_value = int(raw_value)
        except ValueError:
            raise _invalid_reference() from None
        if parsed_value < 1:
            raise _invalid_reference()
        if segment == "attempts":
            attempt = parsed_value
        else:
            platform_job_id = parsed_value

    # GitHub adds #step:N:M when a log line is selected on a job page.
    if parts.fragment and (platform_job_id is None or not _GITHUB_STEP_FRAGMENT_RE.fullmatch(parts.fragment)):
        raise _invalid_reference()

    return RunReference(
        raw=value,
        kind=RunReferenceKind.GITHUB_URL,
        platform_run_id=platform_run_id,
        attempt=attempt,
        repository_full_name=f"{owner}/{repository}",
        platform_job_id=platform_job_id,
    )


def _console_host_for_api_url(api_url: str) -> str | None:
    api_host = urlsplit(api_url).hostname
    if api_host is None or not api_host.startswith("api."):
        return None
    return f"console.{api_host.removeprefix('api.')}"


def _parse_avrea_url(value: str, parts: SplitResult, *, api_url: str) -> RunReference:
    if parts.fragment:
        raise _invalid_reference()
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

    path = parts.path.removesuffix("/").split("/")
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
    if value.startswith("job-"):
        raise click.BadParameter("This is a job ID, not a run ID. Use 'avr job view JOB_ID'.")
    if value.startswith("https://gitlab.com/"):
        raise click.BadParameter(
            "Run commands support GitHub workflow runs. Use 'avr job view JOB_ID' for GitLab jobs."
        )
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


def is_platform_job(job: Any, platform_job_id: int) -> bool:
    """Whether an API job record is the GitHub job with this ID, however the API encodes the ID."""
    return isinstance(job, dict) and str(job.get("platform_job_id")) == str(platform_job_id)


def _run_jobs(run: Any) -> list[Any]:
    jobs = run.get("jobs") if isinstance(run, dict) else None
    return jobs if isinstance(jobs, list) else []


def _attempt_containing_job(
    client: ApiClient,
    org_id: str,
    attempts: list[Any],
    reference: RunReference,
    include: list[str] | None,
) -> dict[str, Any]:
    """Fetch attempts newest first, with their jobs, until one lists the GitHub job.

    Jobs carry full step arrays, so only the attempts searched pay for them; a job link
    usually points at the newest attempt. A re-run gives its jobs new IDs, so a job ID
    normally belongs to one attempt, and the newest attempt wins if an ID ever repeats.
    Never falls back to another attempt: the output would describe the wrong jobs."""
    platform_run_id, platform_job_id = reference.platform_run_id, reference.platform_job_id
    if platform_job_id is None:
        raise _invalid_reference()
    if not attempts:
        raise click.ClickException(f"GitHub run {platform_run_id} has no visible attempts in this organization.")
    params = {"include": [*(include or []), *([] if "jobs" in (include or []) else ["jobs"])]}
    saw_jobs = False
    for attempt in attempts:
        run_id = attempt.get("run_id") if isinstance(attempt, dict) else None
        if not isinstance(run_id, str):
            continue
        run = _unwrap_run(client.public_get(f"/orgs/{org_id}/workflow-runs/{run_id}", params=params))
        jobs = _run_jobs(run)
        saw_jobs = saw_jobs or bool(jobs)
        if any(is_platform_job(job, platform_job_id) for job in jobs):
            return run
    if not saw_jobs:
        raise click.ClickException(
            f"Avrea returned no job data for GitHub run {platform_run_id}, so job {platform_job_id} "
            f"cannot be matched to an attempt. Pass the run instead: avr run view {platform_run_id}"
        )
    raise click.ClickException(
        f"GitHub job {platform_job_id} is not listed in any visible attempt of run {platform_run_id} "
        f"in this organization. Avrea may not track that job. "
        f"To see the latest attempt, pass the run instead: avr run view {platform_run_id}"
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
    if include is not None:
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

    if reference.attempt is None and reference.platform_job_id is None:
        response = client.public_get(
            f"/orgs/{org_id}/workflow-runs/by-platform-id/{platform_run_id}",
            params=params or None,
        )
        run = _unwrap_run(response)
    else:
        if reference.platform_job_id is not None and include is not None:
            # The attempt list only locates the attempt; its jobs are fetched per attempt.
            params["include"] = [name for name in include if name != "jobs"]
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
        if reference.platform_job_id is not None:
            run = _attempt_containing_job(client, org_id, runs, reference, include)
        else:
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


def resolve_run_org(
    client: ApiClient,
    config: CliConfig,
    reference: RunReference,
    org_id: str | None,
) -> str:
    """Resolve and verify the organization of a parsed run reference."""
    if reference.organization_slug is not None and is_scoped_token(config.auth_token):
        raise click.ClickException("A scoped token cannot resolve a run URL's organization. Pass the run ID instead.")
    if reference.organization_slug is not None and org_id is None:
        org_id = reference.organization_slug
    resolved_org_id = get_org_id(config, org_id, client=client)

    if reference.organization_slug is not None:
        active_org_slug = get_verified_org_slug(client, resolved_org_id)
        if active_org_slug.casefold() != reference.organization_slug.casefold():
            raise click.ClickException(
                f"Avrea run URL organization {reference.organization_slug!r} does not match "
                f"the selected organization {active_org_slug!r}."
            )
    return resolved_org_id


def filter_run_jobs(jobs: list[dict[str, Any]], name: str | None) -> list[dict[str, Any]]:
    """Match job names as run view and run logs do: case-insensitive substrings."""
    if not name:
        return jobs
    needle = name.lower()
    return [job for job in jobs if needle in (job.get("job_name") or "").lower()]
