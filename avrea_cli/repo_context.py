"""Repository resolution — detect repo from git remote or resolve org/repo names."""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import CliConfig
from avrea_cli.helpers import handle_http_error
from typing import Literal
from typing import NoReturn
from typing import overload
from urllib.parse import urlparse
import click
import httpx
import os
import re
import subprocess
import sys

_SCP_RE = re.compile(r"^[\w.-]+@[\w.-]+:([\w._-]+/[\w._-]+?)(?:\.git)?$")

_HINT_EMITTED: set[str] = set()
"""Hints emitted this process. Lives for one CLI invocation; the hint
fires the first time per repo and stays quiet for follow-up commands
in the same shell pipeline (e.g. `avr cache list && avr run list`)."""


def _warn_if_case_folded(input_name: str, canonical: str | None) -> None:
    """Surface the canonical name when the resolver matched on case-folded
    input. Without this, ``--repo Acme/X`` silently targets ``acme/x`` and
    later commands replayed from terminal history don't match what ran.

    Goes through ``_emit_using_repo_hint`` so it inherits TTY/AVR_REPO
    suppression — agents capturing stderr in CI don't accumulate noise."""
    if canonical and canonical != input_name:
        _emit_using_repo_hint(f"{input_name} → {canonical}")


def _emit_using_repo_hint(repo: str) -> None:
    """Print the auto-detect / env-override hint to stderr.

    Suppressed when:
    - stderr is non-TTY: caller redirected hints to a file or captured them
      in CI, so we'd be writing to a log nobody reads. Stdout being piped
      (``avr run list | jq``) is precisely when stderr context is *most*
      useful, so don't gate on it.
    - ``AVR_REPO`` is set and matches: the user already opted in by setting
      the env var, so re-announcing it on every command is just noise.

    The explicit ``--repo`` caller never reaches this code path."""
    if not sys.stderr.isatty():
        return
    env_repo = os.environ.get("AVR_REPO")
    if env_repo and env_repo == repo:
        return
    if repo in _HINT_EMITTED:
        return
    _HINT_EMITTED.add(repo)
    click.echo(click.style(f"(using repo: {repo})", dim=True), err=True)


class _RepoNotInOrgError(Exception):
    """Repo is syntactically valid but not part of the org. Distinct from
    ``click.Abort`` (which also fires on auth/transport errors), so soft-detect
    callers can fall back to org-wide queries without swallowing real failures.

    Carries the server's structured 404 hint payload (nearby names + other org
    memberships) so the caller can render "did you mean?" without any extra
    round-trips."""

    def __init__(
        self,
        repo: str,
        *,
        is_structured: bool,
        message: str | None = None,
        nearby: list[str],
        other_orgs: list[dict[str, str]],
    ):
        super().__init__(repo)
        self.repo = repo
        # ``is_structured`` distinguishes the resolve endpoint's "real miss"
        # payload from a free-form/unparsable 404. The soft-detect path
        # (``resolve_repo_or_detect``) falls back regardless; only the abort
        # renderer needs the flag, to gate the "Connect this repo" hint.
        self.is_structured = is_structured
        self.message = message
        self.nearby = nearby
        self.other_orgs = other_orgs


def resolve_repo(client: ApiClient, config: CliConfig, org_id: str, repo: str) -> str:
    """Resolve a repo identifier to an Avrea repository ID.

    Accepts:
      - Avrea repo ID (rep-...): returned as-is
      - GitHub full name (org/repo): resolved via API

    Aborts with a user-facing error if the repo isn't in the org. Callers
    that want to handle "not in org" specially should use
    :func:`_resolve_repo_strict` and catch :class:`_RepoNotInOrgError`.
    """
    try:
        return _resolve_repo_strict(client, config, org_id, repo)
    except _RepoNotInOrgError as exc:
        _abort_repo_not_found(
            exc.repo,
            org_id,
            is_structured=exc.is_structured,
            message=exc.message,
            nearby=exc.nearby,
            other_orgs=exc.other_orgs,
        )


def _resolve_repo_strict(client: ApiClient, config: CliConfig, org_id: str, repo: str) -> str:
    """Inner implementation: raises :class:`_RepoNotInOrgError` for missing
    repos instead of aborting. Auth/transport failures still raise
    ``click.Abort`` so the user sees the real cause.

    The server's 404 body now carries ``nearby_full_names`` + ``other_orgs``,
    so a single round-trip is enough — no client-side list-and-filter needed.
    """
    if repo.startswith("rep-"):
        return repo

    if "/" not in repo:
        click.echo(f'Error: Invalid repository format: "{repo}"', err=True)
        click.echo("Use org/repo (e.g. acme/web) or a repo ID (rep-...).", err=True)
        raise click.Abort()

    try:
        result = client.public_get(f"/orgs/{org_id}/repos/resolve", params={"name": repo})
        data = result.get("data") or {}
        _warn_if_case_folded(repo, data.get("full_name"))
        return data["repository_id"]
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code != 404:
            handle_http_error(exc, "resolve the repository")
        is_miss, message, nearby, other_orgs = _parse_resolve_404_detail(exc)
        # Always raise _RepoNotInOrgError on 404 so the soft-detect path
        # (resolve_repo_or_detect) can fall back to org-wide queries. Whether
        # the 404 carried the structured "miss" payload or a free-form detail
        # is captured in ``is_structured`` and only affects how the strict
        # abort path renders the error (no "Connect this repo" hint when the
        # server's reason is something else, e.g. archived repo).
        raise _RepoNotInOrgError(
            repo,
            is_structured=is_miss,
            message=message,
            nearby=nearby,
            other_orgs=other_orgs,
        ) from None


def _parse_resolve_404_detail(
    exc: httpx.HTTPStatusError,
) -> tuple[bool, str | None, list[str], list[dict[str, str]]]:
    """Extract whether the 404 body is the structured "miss" payload + its
    fields.

    Returns ``(is_structured_miss, message, nearby, other_orgs)``:
      - ``is_structured_miss`` is True only when ``detail`` is the structured
        object the resolve endpoint returns for a real "repo not in org" hit.
        Callers must NOT treat False as a "repo not in org" signal — it could
        be an archived repo, installation removed, upstream proxy outage, etc.
      - ``message`` is the server's free-form ``detail`` string when the body
        is a non-structured 404. ``None`` when the body is structured or
        unparsable.
      - ``nearby`` / ``other_orgs`` are populated only when the payload is the
        structured miss.

    Defensive on unparsable bodies — likely an upstream proxy serving a
    non-JSON maintenance page; surface a hint so the user doesn't read
    "repo not found" as the real cause."""
    try:
        body = exc.response.json()
    except ValueError, AttributeError:
        click.echo(
            click.style(
                "  (server returned a non-JSON 404 — likely an upstream proxy or outage, not a missing repo)",
                dim=True,
            ),
            err=True,
        )
        return False, None, [], []
    detail = body.get("detail") if isinstance(body, dict) else None
    if isinstance(detail, str):
        return False, detail, [], []
    if not isinstance(detail, dict):
        return False, None, [], []
    nearby = detail.get("nearby_full_names") or []
    other_orgs = detail.get("other_orgs") or []
    return (
        True,
        None,
        [n for n in nearby if isinstance(n, str)],
        [o for o in other_orgs if isinstance(o, dict)],
    )


def _abort_repo_not_found(
    repo: str,
    org_id: str,
    *,
    is_structured: bool,
    message: str | None = None,
    nearby: list[str],
    other_orgs: list[dict[str, str]],
) -> NoReturn:
    """Render a "repository not found" error from the resolve 404 body.

    Trusts the caller-supplied ``nearby`` / ``other_orgs`` lists verbatim —
    no client-side filtering or lookup. (The server is responsible for
    not leaking repos the user can't see.)

    Output shape depends on ``is_structured``:
      - True: this is the resolve endpoint's "real miss" payload — we know the
        repo isn't in the org. Render typo suggestions, sibling org hints, and
        the "Connect this repo" prompt as a fallback.
      - False: 404 with a free-form ``detail`` (archived repo, installation
        removed) or unparsable body. Render the server's message verbatim;
        suppress the "Connect this repo" hint, since the user adding the repo
        wouldn't fix what the server actually said is wrong."""
    if is_structured:
        click.echo(f'Error: Repository "{repo}" is not in org "{org_id}".', err=True)
    else:
        click.echo(f"Error: Failed to resolve repository {repo!r} (HTTP 404).", err=True)

    if message:
        click.echo(f"  {message}", err=True)

    if nearby:
        click.echo("\n  Did you mean one of these?", err=True)
        for name in nearby:
            click.echo(f"    {name}", err=True)

    if other_orgs:
        click.echo("\n  You also belong to:", err=True)
        for org in other_orgs:
            slug = org.get("slug") or org.get("organization_id") or ""
            oid = org.get("organization_id") or ""
            click.echo(f"    {slug}  ({oid})", err=True)
        click.echo("  Try `--org <id>` for one-shot, or `avr config set org <slug>` to switch default.", err=True)
    elif is_structured and message is None:
        click.echo("\n  Connect this repo: open the Avrea console and add it to the org.", err=True)
    raise click.Abort()


@overload
def resolve_repo_or_detect(
    client: ApiClient,
    config: CliConfig,
    org_id: str,
    repo: str | None,
    *,
    required: Literal[True],
    detect_git: bool = True,
    strict_detected: bool = False,
) -> str: ...


@overload
def resolve_repo_or_detect(
    client: ApiClient,
    config: CliConfig,
    org_id: str,
    repo: str | None,
    *,
    required: Literal[False] = False,
    detect_git: bool = True,
    strict_detected: bool = False,
) -> str | None: ...


def resolve_repo_or_detect(
    client: ApiClient,
    config: CliConfig,
    org_id: str,
    repo: str | None,
    *,
    required: bool = False,
    detect_git: bool = True,
    strict_detected: bool = False,
) -> str | None:
    """Resolve a repo with this precedence:

        1. ``--repo`` flag (highest)
        2. ``AVR_REPO`` env var (via ``config.repo_override``)
        3. Auto-detect from the local git ``origin`` remote

    Returns the Avrea repo_id, or None if nothing was found and ``required``
    is False. The auto-detect / env-override hint goes to stderr so JSON or
    script output stays clean.

    Pass ``detect_git=False`` to disable step 3. An explicit ``--org`` uses
    this so an org-scoped command isn't silently narrowed to the checkout's
    repo just because the user happens to be standing in one.

    Pass ``strict_detected=True`` to abort (instead of soft-falling back to
    None) when a git-detected repo isn't in the org, while still returning
    None when no repo is detected at all. Writes use this: a checkout whose
    repo isn't connected is a mistake worth surfacing, but having no repo
    context at all just means org scope.
    """
    if repo:
        return resolve_repo(client, config, org_id, repo)
    if config.repo_override:
        _emit_using_repo_hint(config.repo_override)
        return resolve_repo(client, config, org_id, config.repo_override)
    if not detect_git:
        if required:
            raise click.ClickException(_no_repo_error_message())
        return None
    detected = detect_repo_from_git()
    if detected:
        _emit_using_repo_hint(detected)
        if required or strict_detected:
            return resolve_repo(client, config, org_id, detected)
        # Soft-detect: don't abort the command if the auto-detected repo
        # isn't in the org. Caller asked for a repo but is fine without one
        # (e.g. `avr status` falls back to org-wide), so treat the mismatch
        # as "no repo". Auth/transport failures still raise click.Abort
        # so the user isn't silently routed to org-wide queries with a
        # broken token.
        try:
            return _resolve_repo_strict(client, config, org_id, detected)
        except _RepoNotInOrgError:
            click.echo(
                click.style(
                    f"  (auto-detected {detected} isn't in this org — showing org-wide results.)",
                    dim=True,
                ),
                err=True,
            )
            return None
    if required:
        raise click.ClickException(_no_repo_error_message())
    return None


def resolve_repos_or_detect(
    client: ApiClient,
    config: CliConfig,
    org_id: str,
    repos: tuple[str, ...] | list[str],
    *,
    soft_detect: bool = False,
) -> list[str]:
    """Multi-repo variant of :func:`resolve_repo_or_detect`. Same precedence:
    explicit args > ``AVR_REPO`` > git auto-detect. Returns an empty list
    when nothing is found (meaning: don't filter).

    With ``soft_detect=True``, an unrecognized git-detected repo warns
    instead of aborting — for org-wide commands like ``run list`` /
    ``workflow list`` that should still work even when the user happens
    to be in a checkout the org doesn't track.
    """
    if repos:
        return [resolve_repo(client, config, org_id, r) for r in repos]
    if config.repo_override:
        _emit_using_repo_hint(config.repo_override)
        return [resolve_repo(client, config, org_id, config.repo_override)]
    detected = detect_repo_from_git()
    if not detected:
        return []
    _emit_using_repo_hint(detected)
    if soft_detect:
        try:
            return [_resolve_repo_strict(client, config, org_id, detected)]
        except _RepoNotInOrgError:
            click.echo(
                click.style(
                    f"  (auto-detected {detected} isn't in this org — showing org-wide results.)",
                    dim=True,
                ),
                err=True,
            )
            return []
    return [resolve_repo(client, config, org_id, detected)]


def resolve_repo_named(
    client: ApiClient,
    config: CliConfig,
    org_id: str,
    repo: str | None,
) -> tuple[str, str]:
    """Like :func:`resolve_repo_or_detect` but also returns the human label
    (full_name or rep-id) the user would recognize. Useful for confirmation
    prompts so a destructive op shows ``acme/web`` instead of
    the opaque ``rep-019d…`` they never typed."""
    if repo:
        rid = resolve_repo(client, config, org_id, repo)
        return rid, repo
    if config.repo_override:
        _emit_using_repo_hint(config.repo_override)
        rid = resolve_repo(client, config, org_id, config.repo_override)
        return rid, config.repo_override
    detected = detect_repo_from_git()
    if detected:
        _emit_using_repo_hint(detected)
        rid = resolve_repo(client, config, org_id, detected)
        return rid, detected
    raise click.ClickException(_no_repo_error_message())


def detect_repo_from_git() -> str | None:
    """Detect owner/repo from the current git directory's origin remote.

    Returns 'owner/repo' string or None if detection fails.
    """
    try:
        result = subprocess.run(
            ["git", "remote", "get-url", "origin"],
            capture_output=True,
            text=True,
            timeout=5,
        )
        if result.returncode != 0:
            return None
        return parse_remote_url(result.stdout.strip())
    except subprocess.TimeoutExpired, FileNotFoundError:
        return None


def _list_git_remotes() -> list[str]:
    """Return the list of remote names in the current git directory.

    Empty list if not in a git directory, git is missing, or the command
    fails. Used purely for diagnostics — the auto-detect path stays pinned
    to ``origin`` so behavior is predictable."""
    try:
        result = subprocess.run(
            ["git", "remote"],
            capture_output=True,
            text=True,
            timeout=5,
        )
    except subprocess.TimeoutExpired, FileNotFoundError:
        return []
    if result.returncode != 0:
        return []
    return [line.strip() for line in result.stdout.splitlines() if line.strip()]


def _no_repo_error_message() -> str:
    """Build the 'could not determine repository' error with diagnostic
    detail when we're inside a git directory that has remotes — just not
    one named ``origin``. Saves the user from staring at the generic
    error wondering why the CLI doesn't see the repo it's clearly in."""
    remotes = _list_git_remotes()
    if remotes and "origin" not in remotes:
        return (
            f"Found git remotes [{', '.join(remotes)}] but no 'origin'. "
            "Pass --repo <org/name>, set AVR_REPO, or rename a remote to 'origin'."
        )
    return "Could not determine repository. Run from a git directory, pass --repo <org/name>, or set AVR_REPO."


def parse_remote_url(url: str) -> str | None:
    """Extract owner/repo from a git remote URL.

    Supports:
      - SSH SCP-like:  git@github.com:org/repo.git
      - SSH explicit:  ssh://git@github.com/org/repo.git
      - HTTPS:         https://github.com/org/repo.git
      - git+ssh/https: git+ssh://git@github.com/org/repo.git
    """
    if not url:
        return None

    if url.startswith("git+"):
        url = url[4:]

    m = _SCP_RE.match(url)
    if m:
        return m.group(1)

    try:
        parsed = urlparse(url)
    except ValueError:
        return None

    if parsed.scheme not in ("ssh", "https", "http", "git"):
        return None

    path = parsed.path.lstrip("/")
    if path.endswith(".git"):
        path = path[:-4]

    parts = path.split("/")
    if len(parts) != 2 or not parts[0] or not parts[1]:
        return None

    return path
