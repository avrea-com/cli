"""Repository SBOM CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import format_size
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import retry_after_seconds
from avrea_cli.helpers import validate_cursor
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import emit_json_record
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import json_options
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from avrea_cli.output import format_key_value
from avrea_cli.output import format_timestamp
from avrea_cli.output import output_list
from avrea_cli.repo_context import resolve_repo_or_detect
from dataclasses import dataclass
from typing import Any
from typing import NoReturn
import click
import httpx
import re
import shlex
import time

_COMMIT_SHA_RE = re.compile(r"^(?:[0-9a-f]{40}|[0-9a-f]{64})$")
_COMMIT_PREFIX_RE = re.compile(r"^[0-9a-f]{7,64}$")
_HISTORY_PAGE_LIMIT = 1000

_ARTIFACT_FILENAMES = {
    "cyclonedx": "bom.cyclonedx.json",
    "spdx": "bom.spdx.json",
    "inventory": "inventory.json",
}

_TERMINAL_TASK_STATUSES = frozenset({"COMPLETED", "FAILED"})
_WAIT_DEFAULT_TIMEOUT = 900
_WAIT_POLL_SECONDS = 5.0

_NO_SBOM_HINT = "No SBOM is recorded for this repository or commit yet. Run `avr sbom generate` to create one."

_repo_option = click.option(
    "--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted."
)
_org_option = click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)


def _validate_commit(_ctx: click.Context, _param: click.Parameter, value: str | None) -> str | None:
    if value is None:
        return None
    sha = value.strip().lower()
    if not _COMMIT_PREFIX_RE.match(sha):
        raise click.BadParameter("must be a hex commit SHA or a prefix of at least 7 characters.")
    return sha


def _strip_ref(_ctx: click.Context, _param: click.Parameter, value: str | None) -> str | None:
    """Trim a pasted ref; git ref names cannot contain whitespace, so this never alters a real one."""
    if value is None:
        return None
    ref = value.strip()
    if not ref:
        raise click.BadParameter("must not be empty.")
    return ref


_commit_argument = click.argument("commit", required=False, callback=_validate_commit)
_commit_option = click.option(
    "--commit",
    "commit_option",
    callback=_validate_commit,
    help="Same as the COMMIT argument.",
)


def _requested_commit(commit: str | None, commit_option: str | None) -> str | None:
    if commit and commit_option and commit != commit_option:
        raise click.UsageError("Pass the commit either as the COMMIT argument or with --commit, not both.")
    return commit or commit_option


def _resolve_commit(client: ApiClient, org_id: str, repo_id: str, commit: str) -> str:
    """The full SHA for ``commit``, a full SHA or a prefix of one.

    The API addresses SBOMs by full SHA only, so a prefix is matched against the
    recorded history. Matching stops at a second hit, which already makes the
    prefix ambiguous."""
    if _COMMIT_SHA_RE.match(commit):
        return commit
    matches: list[str] = []
    params: dict[str, Any] = {"limit": _HISTORY_PAGE_LIMIT}
    while len(matches) < 2:
        try:
            response = client.public_get(_sbom_path(org_id, repo_id), params=params)
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "list SBOMs")
        for row in response.get("data", []):
            sha = row.get("commit_sha") or ""
            if sha.startswith(commit) and sha not in matches:
                matches.append(sha)
        next_cursor = (response.get("pagination") or {}).get("next_cursor")
        if not next_cursor:
            break
        params = {"limit": _HISTORY_PAGE_LIMIT, "cursor": next_cursor}

    if not matches:
        raise click.ClickException(
            f"No recorded SBOM matches commit {commit}. Run `avr sbom list` to see the recorded commits."
        )
    if len(matches) > 1:
        raise click.ClickException(
            f"Commit {commit} is ambiguous; it matches {matches[0]} and {matches[1]}. Use a longer prefix."
        )
    return matches[0]


def _resolve_scope(ctx: click.Context, org_id: str | None, repo_id: str | None) -> tuple[ApiClient, str, str]:
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id, required=True)
    return client, org_id, repo_id


def _sbom_path(org_id: str, repo_id: str) -> str:
    return f"/orgs/{org_id}/repos/{repo_id}/analysis/sbom"


def _fetch_snapshot(client: ApiClient, org_id: str, repo_id: str, commit_sha: str | None) -> dict[str, Any]:
    suffix = f"/commits/{commit_sha}" if commit_sha else "/latest"
    try:
        return client.public_get(f"{_sbom_path(org_id, repo_id)}{suffix}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "get SBOM", hint=_NO_SBOM_HINT)


def _count(value: int | None) -> str:
    return "?" if value is None else str(value)


def _summary_count(summary: dict[str, Any], key: str) -> int | None:
    """A count from the opaque summary, or None when it is absent or not an int.

    Mirrors the server's list projection: a missing count is unknown, never 0,
    which covers the oversized/malformed summary replacements."""
    value = summary.get(key)
    if isinstance(value, bool) or not isinstance(value, int):
        return None
    return value


def _copyleft_count(summary: dict[str, Any]) -> int | None:
    """Listed copyleft names plus the truncated remainder, or None when unknown."""
    names = summary.get("copyleft")
    if not isinstance(names, list):
        return None
    return len(names) + (_summary_count(summary, "copyleft_omitted") or 0)


def _count_breakdown(value: object) -> list[tuple[str, int]]:
    """A name -> count map from the opaque summary, largest first.

    The server passes the summary through unvalidated, so a map that is not a
    dict yields nothing and an entry whose count is not an int is dropped."""
    if not isinstance(value, dict):
        return []
    counts = [
        (str(name), count) for name, count in value.items() if isinstance(count, int) and not isinstance(count, bool)
    ]
    return sorted(counts, key=lambda kv: (-kv[1], kv[0]))


@click.group(cls=GhGroup)
@click.pass_context
def sbom(ctx):
    """View, generate, and download repository SBOMs."""
    ensure_ctx(ctx)


_SBOM_LIST_FIELDS = make_schema(
    "commit_sha",
    "branch",
    "is_release",
    "schema_version",
    "generated_at",
    "recorded_at",
    "total_dependencies",
    "unresolved",
    "copyleft_count",
    "artifacts",
)


@sbom.command("list")
@_repo_option
@_org_option
@click.option("-L", "--limit", type=click.IntRange(1, 1000), default=50, show_default=True, help="Max SBOMs to return.")
@click.option("--cursor", default=None, help="Opaque cursor from a previous response's next_cursor.")
@json_options
@click.pass_context
def sbom_list(ctx, repo_id, org_id, limit, cursor, json_fields, jq_expr):
    """List the repository's recorded SBOMs, newest first.

    Counts shown as "?" were not recorded by the SBOM's schema version.

    \b
    Examples:
        avr sbom list --repo acme/api
        avr sbom list --repo acme/api --json commit_sha,copyleft_count

    \b
    JSON FIELDS
        artifacts, branch, commit_sha, copyleft_count, generated_at,
        is_release, recorded_at, schema_version, total_dependencies,
        unresolved
    """
    if handle_json_meta(json_fields, jq_expr, _SBOM_LIST_FIELDS):
        return
    cursor = validate_cursor(cursor)
    client, org_id, repo_id = _resolve_scope(ctx, org_id, repo_id)

    params: dict[str, Any] = {"limit": limit}
    if cursor:
        params["cursor"] = cursor
    try:
        response = client.public_get(_sbom_path(org_id, repo_id), params=params)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list SBOMs")

    rows = response.get("data", [])
    next_cursor = (response.get("pagination") or {}).get("next_cursor")
    if json_fields is not None:
        emit_json(rows, split_fields(json_fields, _SBOM_LIST_FIELDS), _SBOM_LIST_FIELDS, jq_expr)
        if next_cursor:
            click.echo(f"next_cursor: {shlex.quote(next_cursor)}", err=True)
        return

    for row in rows:
        row["commit_display"] = (row.get("commit_sha") or "")[:12]
        row["release_display"] = "yes" if row.get("is_release") else "no"
        row["deps_display"] = _count(row.get("total_dependencies"))
        row["unresolved_display"] = _count(row.get("unresolved"))
        row["copyleft_display"] = _count(row.get("copyleft_count"))
        row["recorded_display"] = format_timestamp(row.get("recorded_at"))

    output_list(
        rows,
        columns=[
            "commit_display",
            "branch",
            "release_display",
            "deps_display",
            "unresolved_display",
            "copyleft_display",
            "recorded_display",
        ],
        column_labels=["Commit", "Branch", "Release", "Deps", "Unresolved", "Copyleft", "Recorded"],
    )

    if next_cursor:
        click.echo(f"\nMore results available. Re-run with --cursor {shlex.quote(next_cursor)}", err=True)


_SBOM_VIEW_FIELDS = make_schema(
    "commit_sha",
    "branch",
    "is_release",
    "schema_version",
    "generated_at",
    "recorded_at",
    "artifacts",
    "summary",
)


@sbom.command("view")
@_commit_argument
@_repo_option
@_org_option
@_commit_option
@json_options
@click.pass_context
def sbom_view(ctx, commit, repo_id, org_id, commit_option, json_fields, jq_expr):
    """Show an SBOM's summary: dependency counts, licences, and artifacts.

    COMMIT is the commit of a recorded SBOM, as a full SHA or a unique prefix
    of at least 7 characters such as the one `avr sbom list` shows. Defaults to
    the latest SBOM.

    \b
    Examples:
        avr sbom view --repo acme/api
        avr sbom view 3f2c9a1b7d04 --repo acme/api
        avr sbom view --repo acme/api --json summary --jq .summary.licenses

    \b
    JSON FIELDS
        artifacts, branch, commit_sha, generated_at, is_release, recorded_at,
        schema_version, summary
    """
    if handle_json_meta(json_fields, jq_expr, _SBOM_VIEW_FIELDS):
        return
    commit_sha = _requested_commit(commit, commit_option)
    client, org_id, repo_id = _resolve_scope(ctx, org_id, repo_id)
    if commit_sha is not None:
        commit_sha = _resolve_commit(client, org_id, repo_id, commit_sha)
    snapshot = _fetch_snapshot(client, org_id, repo_id, commit_sha)

    if json_fields is not None:
        emit_json_record(snapshot, split_fields(json_fields, _SBOM_VIEW_FIELDS), _SBOM_VIEW_FIELDS, jq_expr)
        return

    summary = snapshot.get("summary")
    if not isinstance(summary, dict):
        summary = {}
    copyleft = summary.get("copyleft") if isinstance(summary.get("copyleft"), list) else []
    inventory_hint = shlex.join(
        [
            "avr",
            "sbom",
            "download",
            snapshot.get("commit_sha", ""),
            "--org",
            org_id,
            "--repo",
            repo_id,
            "--format",
            "inventory",
        ]
    )
    click.echo(
        format_key_value(
            {
                "Commit": snapshot.get("commit_sha", ""),
                "Branch": snapshot.get("branch", ""),
                "Release": "yes" if snapshot.get("is_release") else "no",
                "Generated": format_timestamp(snapshot.get("generated_at")),
                "Dependencies": _count(_summary_count(summary, "total_dependencies")),
                "Unresolved": _count(_summary_count(summary, "unresolved")),
                "Copyleft": _count(_copyleft_count(summary)),
            }
        )
    )
    omitted_reason = summary.get("omitted")
    if omitted_reason:
        click.echo(f"\nSummary unavailable ({omitted_reason}); run `{inventory_hint}` for the full dependency list.")

    licenses = _count_breakdown(summary.get("licenses"))
    if licenses:
        click.echo()
        output_list(
            [{"license": name, "count": count} for name, count in licenses],
            columns=["license", "count"],
            column_labels=["License", "Count"],
        )

    ecosystems = _count_breakdown(summary.get("ecosystems"))
    if ecosystems:
        click.echo()
        output_list(
            [{"ecosystem": name, "count": count} for name, count in ecosystems],
            columns=["ecosystem", "count"],
            column_labels=["Ecosystem", "Count"],
        )

    if copyleft:
        click.echo("\nCopyleft dependencies:")
        for name in copyleft:
            click.echo(f"  {name}")
        omitted = _summary_count(summary, "copyleft_omitted")
        if omitted:
            click.echo(f"  … and {omitted} more (see `{inventory_hint}`)")

    artifacts = snapshot.get("artifacts") or []
    if artifacts:
        click.echo()
        for artifact in artifacts:
            artifact["size_display"] = format_size(artifact.get("raw_bytes", 0))
        output_list(
            artifacts,
            columns=["filename", "media_type", "size_display"],
            column_labels=["Artifact", "Media Type", "Size"],
        )


@sbom.command("download")
@_commit_argument
@_repo_option
@_org_option
@_commit_option
@click.option(
    "--format",
    "artifact_format",
    type=click.Choice(sorted(_ARTIFACT_FILENAMES)),
    default="cyclonedx",
    show_default=True,
    help="Artifact to download.",
)
@click.option(
    "--out",
    "out_path",
    default=None,
    help='Output file path, or "-" for stdout. Defaults to the artifact filename in the current directory.',
)
@click.pass_context
def sbom_download(ctx, commit, repo_id, org_id, commit_option, artifact_format, out_path):
    """Download an SBOM artifact (CycloneDX, SPDX, or dependency inventory).

    COMMIT is the commit of a recorded SBOM, as a full SHA or a unique prefix
    of at least 7 characters such as the one `avr sbom list` shows. Defaults to
    the latest SBOM.

    \b
    Examples:
        avr sbom download --repo acme/api
        avr sbom download --repo acme/api --format spdx --out api.spdx.json
        avr sbom download 3f2c9a1b7d04 --repo acme/api --out - | jq .components
    """
    commit_sha = _requested_commit(commit, commit_option)
    client, org_id, repo_id = _resolve_scope(ctx, org_id, repo_id)
    if commit_sha is None:
        commit_sha = _fetch_snapshot(client, org_id, repo_id, None)["commit_sha"]
    else:
        commit_sha = _resolve_commit(client, org_id, repo_id, commit_sha)

    filename = _ARTIFACT_FILENAMES[artifact_format]
    try:
        content = client.public_get_bytes(f"{_sbom_path(org_id, repo_id)}/commits/{commit_sha}/artifacts/{filename}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, f"download {filename}", hint=_NO_SBOM_HINT)

    if out_path == "-":
        click.echo(content, nl=False)
        return

    target = out_path or filename
    try:
        with open(target, "wb") as f:
            f.write(content)
    except OSError as exc:
        click.echo(f"Error writing file: {exc}", err=True)
        raise click.Abort() from None
    click.echo(f"Saved {filename} for {commit_sha[:12]} to {target} ({format_size(len(content))}).")


_SBOM_GENERATE_FIELDS = make_schema("ai_task_id", "status", "task_status", "error_code", "commit_sha", "recorded_at")

# A 429 during a wait backs off for its Retry-After, capped so a large or
# hostile value cannot stall the loop past several polls.
_RETRY_AFTER_CAP_SECONDS = 30
# Floor for a poll request's timeout, so the check at the deadline can finish.
_MIN_POLL_REQUEST_SECONDS = 1.0
_TRANSIENT_STATUSES = frozenset({408, 429})


def _commit_snapshot_or_none(client: ApiClient, org_id: str, repo_id: str, commit_sha: str) -> dict[str, Any] | None:
    try:
        return client.public_get(f"{_sbom_path(org_id, repo_id)}/commits/{commit_sha}")
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code == 404:
            return None
        handle_http_error(exc, "get SBOM")


class _TransientPollError(Exception):
    def __init__(self, backoff: float):
        super().__init__(backoff)
        self.backoff = backoff


class _Poller:
    """Reads for a --wait loop, bounded by its deadline and retried when transient.

    Each request may take at most the time left before the deadline, so a hung
    connection cannot outlive the wait. Network failures, 408, 429 and 5xx
    raise _TransientPollError with a backoff (a 429's Retry-After, capped) for
    the loop to retry; any other HTTP error is permanent and exits now through
    handle_http_error rather than hiding behind the rest of the wait. Only
    reads are retried: the generation request itself is never repeated."""

    def __init__(self, client: ApiClient, deadline: float):
        self._client = client
        self._deadline = deadline
        self.last_error: str | None = None

    def get(
        self, path: str, *, action: str, params: dict[str, Any] | None = None, missing_ok: bool = False
    ) -> dict[str, Any] | None:
        remaining = self._deadline - time.monotonic()
        timeout = min(self._client.timeout, max(remaining, _MIN_POLL_REQUEST_SECONDS))
        try:
            body = self._client.public_get(path, params=params, timeout=timeout)
        except httpx.HTTPStatusError as exc:
            status = exc.response.status_code
            if missing_ok and status == 404:
                self.last_error = None
                return None
            if status < 500 and status not in _TRANSIENT_STATUSES:
                handle_http_error(exc, action)
            retry_after = retry_after_seconds(exc.response) if status == 429 else None
            backoff = min(retry_after, _RETRY_AFTER_CAP_SECONDS) if retry_after is not None else _WAIT_POLL_SECONDS
            self._transient(f"HTTP {status}", action, backoff)
        except httpx.TransportError as exc:
            self._transient(type(exc).__name__, action, _WAIT_POLL_SECONDS)
        self.last_error = None
        return body

    def _transient(self, description: str, action: str, backoff: float) -> NoReturn:
        self.last_error = description
        click.echo(f"Warning: could not {action} ({description}); retrying.", err=True)
        raise _TransientPollError(backoff)

    def sleep(self, backoff: float) -> bool:
        """Sleep until the next poll, never past the deadline. False once it has passed."""
        remaining = self._deadline - time.monotonic()
        if remaining <= 0:
            return False
        time.sleep(min(backoff, remaining))
        return True


class _TaskTracker:
    """Follows one analysis task through the repository's latest-task state.

    The state endpoint reports only the newest task. One analysis runs per
    repository at a time, so once this task has been seen, a different id means
    it finished and was superseded before its outcome could be read; a state
    for a different task before this one appears is an older run."""

    def __init__(self, poller: _Poller, org_id: str, repo_id: str, task_id: str):
        self._poller = poller
        self._path = f"/orgs/{org_id}/repos/{repo_id}/ai-task/state"
        self.task_id = task_id
        self.status: str | None = None
        self.error_code: str | None = None
        self.superseded_by: str | None = None

    @property
    def settled(self) -> bool:
        return self.status in _TERMINAL_TASK_STATUSES or self.superseded_by is not None

    @property
    def task_status(self) -> str:
        if self.superseded_by is not None or self.status is None:
            return "unknown"
        return self.status.lower()

    def poll(self) -> None:
        if self.settled:
            return
        state = self._poller.get(self._path, params={"scope": "sbom"}, action="check SBOM generation status") or {}
        observed = state.get("ai_task_id")
        if observed == self.task_id:
            self.status = state.get("status")
            self.error_code = state.get("error_code")
        elif self.status is not None and observed:
            self.superseded_by = observed


@sbom.command("generate")
@click.argument("ref", required=False, callback=_strip_ref)
@_repo_option
@_org_option
@click.option("--ref", "ref_option", default=None, callback=_strip_ref, help="Same as the REF argument.")
@click.option("--wait", is_flag=True, default=False, help="Wait until generation finishes before returning.")
@click.option(
    "--wait-timeout",
    default=_WAIT_DEFAULT_TIMEOUT,
    show_default=True,
    type=click.IntRange(min=1),
    help="Seconds to wait when --wait is set.",
)
@json_options
@click.pass_context
def sbom_generate(ctx, ref, repo_id, org_id, ref_option, wait, wait_timeout, json_fields, jq_expr):
    """Start SBOM generation for a repository.

    REF is the branch, tag, or commit SHA to analyse; an abbreviated SHA such
    as the one `avr sbom list` shows works too. Defaults to the default-branch
    tip. Without --wait, commit_sha is the full SHA of the commit the run
    analyses when REF names one.

    One analysis runs per repository at a time. A request for the same ref as
    the analysis already running joins it; a request for a different ref is
    rejected (HTTP 409) until that analysis finishes. Once an SBOM has been
    recorded for the repository, a new run is refused (HTTP 429) for a
    cooldown period, and the error says how many seconds remain.

    With --wait and a REF that names a commit, success means a new SBOM for
    that commit is downloadable, whatever the analysis task's own outcome
    (task_status); commit_sha and recorded_at identify it. A failed analysis
    can still deliver its SBOM later, so such a wait runs to --wait-timeout
    before reporting failure. For a branch, tag, or the default branch the API
    does not say which commit the run resolved, so --wait reports the analysis
    task's outcome only. Transient errors while waiting are retried until
    --wait-timeout; a wait that times out leaves the analysis running.

    \b
    Examples:
        avr sbom generate --repo acme/api
        avr sbom generate v1.4.0 --repo acme/api
        avr sbom generate 3f2c9a1b7d04 --repo acme/api --wait

    \b
    JSON FIELDS
        ai_task_id, commit_sha, error_code, recorded_at, status, task_status
    """
    if handle_json_meta(json_fields, jq_expr, _SBOM_GENERATE_FIELDS):
        return
    if ref and ref_option and ref != ref_option:
        raise click.UsageError("Pass the ref either as the REF argument or with --ref, not both.")
    ref = ref or ref_option
    output = _JsonOutput(
        fields=split_fields(json_fields, _SBOM_GENERATE_FIELDS) if json_fields is not None else [],
        jq_expr=jq_expr,
        enabled=json_fields is not None,
    )
    client, org_id, repo_id = _resolve_scope(ctx, org_id, repo_id)

    commit_sha = ref.lower() if ref and _COMMIT_SHA_RE.match(ref.lower()) else None
    baseline = _commit_snapshot_or_none(client, org_id, repo_id, commit_sha) if wait and commit_sha else None
    deadline = time.monotonic() + wait_timeout

    body: dict[str, Any] = {"scope": "sbom"}
    if ref:
        body["ref"] = commit_sha or ref
    try:
        response = client.public_post(f"{_sbom_path(org_id, repo_id)}/generate", json=body)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "generate SBOM")

    task_id = response["ai_task_id"]
    result: dict[str, Any] = {
        "ai_task_id": task_id,
        "status": response.get("status"),
        "task_status": None,
        "error_code": None,
        "commit_sha": None,
        "recorded_at": None,
    }

    expanded_sha = response.get("commit_sha")
    expanded = commit_sha is None and isinstance(expanded_sha, str) and _COMMIT_SHA_RE.match(expanded_sha)
    if expanded:
        commit_sha = expanded_sha

    if not wait:
        result["commit_sha"] = commit_sha
        if output.emit(result):
            return
        target = f" for {commit_sha[:12]}" if commit_sha else ""
        if result["status"] == "already_running":
            click.echo(f"SBOM generation already running{target} (task {task_id}); joined it.")
        else:
            click.echo(f"SBOM generation started{target} (task {task_id}).")
        click.echo("Generation takes a few minutes; run `avr sbom view` afterwards to see the result.", err=True)
        return

    click.echo(f"Waiting for SBOM generation (task {task_id})…", err=True)
    poller = _Poller(client, deadline)
    tracker = _TaskTracker(poller, org_id, repo_id, task_id)
    waiter = _Wait(poller, tracker, wait_timeout, result, output)
    if commit_sha is None:
        _wait_for_task_outcome(waiter)
        return
    snapshot_path = f"{_sbom_path(org_id, repo_id)}/commits/{commit_sha}"
    if expanded:
        # The API expanded an abbreviated SHA, so the baseline is read only now,
        # after the run has started; retry it like the wait's own reads.
        baseline = _snapshot_after_request(waiter, snapshot_path, commit_sha)
    baseline_recorded_at = baseline.get("recorded_at") if baseline else None
    view_command = shlex.join(["avr", "sbom", "view", commit_sha, "--org", org_id, "--repo", repo_id])
    _wait_for_commit_delivery(waiter, snapshot_path, commit_sha, baseline_recorded_at, view_command)


@dataclass(frozen=True)
class _JsonOutput:
    fields: list[str]
    jq_expr: str | None
    enabled: bool

    def emit(self, result: dict[str, Any]) -> bool:
        """Write ``result`` as the requested JSON record; False when --json is off."""
        if self.enabled:
            emit_json_record(result, self.fields, _SBOM_GENERATE_FIELDS, self.jq_expr)
        return self.enabled


@dataclass(frozen=True)
class _Wait:
    poller: _Poller
    tracker: _TaskTracker
    timeout: int
    result: dict[str, Any]
    output: _JsonOutput

    def record_task(self, status: str | None = None) -> None:
        self.result["task_status"] = self.tracker.task_status
        self.result["error_code"] = self.tracker.error_code
        if status is not None:
            self.result["status"] = status

    def time_out(self, pending: str) -> NoReturn:
        """Report a wait that ran out of time while ``pending``; the task keeps running."""
        self.record_task("timeout")
        if not self.output.emit(self.result):
            last_error = f" (last error: {self.poller.last_error})" if self.poller.last_error else ""
            click.echo(
                f"Error: Timed out after {self.timeout}s waiting for task {self.tracker.task_id}{last_error}; "
                f"{pending}. The analysis was not cancelled and may still finish; check `avr sbom list` later.",
                err=True,
            )
        raise SystemExit(1)


def _snapshot_after_request(waiter: _Wait, snapshot_path: str, commit_sha: str) -> dict[str, Any] | None:
    """The commit's current snapshot, or None; transient errors retry until the deadline."""
    while True:
        try:
            return waiter.poller.get(snapshot_path, action="check for the commit's SBOM", missing_ok=True)
        except _TransientPollError as exc:
            if not waiter.poller.sleep(exc.backoff):
                waiter.time_out(f"the SBOM for {commit_sha[:12]} could not be checked")


def _wait_for_task_outcome(waiter: _Wait) -> None:
    """Report the analysis task's own outcome; no SBOM can be attributed to it."""
    tracker = waiter.tracker
    while True:
        backoff = _WAIT_POLL_SECONDS
        try:
            tracker.poll()
        except _TransientPollError as exc:
            backoff = exc.backoff
        if tracker.settled:
            break
        if not waiter.poller.sleep(backoff):
            waiter.time_out("its outcome is not known yet")

    waiter.record_task(tracker.task_status)
    if not waiter.output.emit(waiter.result):
        _echo_task_outcome(tracker)
    if waiter.result["status"] != "completed":
        raise SystemExit(1)


def _echo_task_outcome(tracker: _TaskTracker) -> None:
    task_id = tracker.task_id
    if tracker.superseded_by is not None:
        click.echo(
            f"Error: Analysis task {task_id} was superseded by task {tracker.superseded_by} before its outcome "
            "could be read; check `avr sbom list`.",
            err=True,
        )
    elif tracker.status == "FAILED":
        click.echo(
            f"Error: Analysis task {task_id} failed ({tracker.error_code or 'unknown error'}). A joined full "
            "analysis can still record its SBOM; check `avr sbom list`.",
            err=True,
        )
    else:
        click.echo(
            f"Analysis task {task_id} completed. Its SBOM can take a moment to appear in `avr sbom list`; "
            "pass --ref <commit-sha> with --wait to wait for a specific commit's SBOM."
        )


def _wait_for_commit_delivery(
    waiter: _Wait, snapshot_path: str, commit_sha: str, baseline_recorded_at: str | None, view_command: str
) -> None:
    """Wait for the commit's SBOM to be recorded after the baseline.

    Delivery is the only success signal. Task completion precedes artifact
    delivery, a joined full analysis can fail after its SBOM is built and still
    deliver it on a later retry, and a newer task can hide this one's state, so
    the commit snapshot is checked on every tick for the whole budget whatever
    the task reports. Reading the commit-pinned snapshot rather than the latest
    keeps another run's delayed delivery from answering for this one;
    recorded_at also moves when a re-scan replaces an unchanged commit's row."""
    tracker = waiter.tracker
    short_sha = commit_sha[:12]
    while True:
        backoff = _WAIT_POLL_SECONDS
        try:
            snapshot = waiter.poller.get(snapshot_path, action="check for the commit's SBOM", missing_ok=True)
            if snapshot is not None and snapshot.get("recorded_at") != baseline_recorded_at:
                break
            tracker.poll()
        except _TransientPollError as exc:
            backoff = exc.backoff
        if not waiter.poller.sleep(backoff):
            if not tracker.settled:
                waiter.time_out(f"no new SBOM for {short_sha} yet")
            waiter.record_task("failed")
            if not waiter.output.emit(waiter.result):
                _echo_undelivered(tracker, short_sha, waiter.timeout)
            raise SystemExit(1)

    waiter.record_task("completed")
    waiter.result["commit_sha"] = snapshot.get("commit_sha")
    waiter.result["recorded_at"] = snapshot.get("recorded_at")
    if waiter.output.emit(waiter.result):
        return
    if tracker.status == "FAILED":
        click.echo(
            f"Note: analysis task {tracker.task_id} failed ({tracker.error_code or 'unknown error'}) after "
            "producing the SBOM.",
            err=True,
        )
    click.echo(f"SBOM generated for {short_sha}. Run `{view_command}` to see it.")


def _echo_undelivered(tracker: _TaskTracker, short_sha: str, wait_timeout: int) -> None:
    """Explain a settled task whose commit SBOM never arrived within the wait."""
    task_id = tracker.task_id
    if tracker.superseded_by is not None:
        click.echo(
            f"Error: Analysis task {task_id} was superseded by task {tracker.superseded_by} and no new SBOM for "
            f"{short_sha} was delivered within {wait_timeout}s.",
            err=True,
        )
    elif tracker.status == "FAILED":
        click.echo(
            f"Error: Analysis task {task_id} failed ({tracker.error_code or 'unknown error'}) and no new SBOM for "
            f"{short_sha} was delivered within {wait_timeout}s.",
            err=True,
        )
    else:
        click.echo(
            f"Error: Analysis task {task_id} completed, but no new SBOM for {short_sha} was available within "
            f"{wait_timeout}s. Artifact delivery may still be retrying; check `avr sbom list` later.",
            err=True,
        )
