"""Clone from a nearby mirror and supply short-lived credentials to native Git."""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import require_secure_host
from avrea_cli.helpers import handle_http_error
from datetime import UTC
from datetime import datetime
from urllib.parse import quote
from urllib.parse import urlsplit
import asyncio
import click
import httpx
import ipaddress
import os
import re
import shlex
import shutil
import subprocess

_PROBE_TIMEOUT = 5.0


def validate_repo_name(value: str) -> str:
    """Accept a repository route, never a URL or a traversal path."""
    parts = value.split("/")
    if len(parts) < 2 or any(part in {"", ".", ".."} or not re.fullmatch(r"[A-Za-z0-9_.-]+", part) for part in parts):
        raise click.BadParameter("must be an owner/repository name", param_hint="REPOSITORY")
    return value


def mirror_endpoint(value: str) -> str:
    """Only server-advertised secure origins may receive a Git JWT."""
    try:
        value = require_secure_host(value).rstrip("/")
        parts = urlsplit(value)
        if parts.path or "?" in value or "#" in value or parts.username is not None or "\\" in value:
            raise ValueError
        host = parts.hostname or ""
        if ":" in host:
            if "%" in host:
                raise ValueError
            ipaddress.IPv6Address(host)
            authority = f"[{host}]"
        else:
            # DNS names are ASCII LDH labels. Validate A-labels as well: a
            # malformed xn-- label can otherwise raise inside httpx's probe.
            if len(host) > 253 or not all(
                re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?", label)
                for label in host.rstrip(".").split(".")
            ):
                raise ValueError
            host.encode("ascii").decode("idna")
            authority = host
        if parts.port is not None:
            authority += f":{parts.port}"
        if parts.netloc.lower() != authority:
            raise ValueError
    except ValueError, click.ClickException:
        raise click.ClickException("The API returned an invalid or insecure mirror endpoint.") from None
    return value


def git_token(client: ApiClient, full_name: str) -> tuple[str, datetime]:
    """The issuer owns the TTL: one hour, or less for scoped API keys."""
    try:
        response = client.public_post("/tokens/git", json={"repos": [full_name]})
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "issue a Git token")
    except httpx.HTTPError:
        raise click.ClickException("Could not reach Avrea to issue a Git token.") from None
    try:
        token = response["token"]
        expires_at = datetime.fromisoformat(response["expires_at"])
        if (
            not isinstance(token, str)
            or not re.fullmatch(r"[A-Za-z0-9_.-]+", token)
            or expires_at.tzinfo is None
            or expires_at <= datetime.now(UTC)
        ):
            raise ValueError
    except KeyError, TypeError, ValueError:
        # Never render an invalid response: it can contain a credential.
        raise click.ClickException("Avrea returned an invalid or expired Git token.") from None
    return token, expires_at


def clone_url(endpoint: str, full_name: str) -> str:
    return f"{endpoint}/{quote(full_name, safe='/')}.git"


async def nearest_mirror(endpoints: list[str], full_name: str, token: str) -> str:
    """Race authenticated Git advertisements; empty or unhealthy replicas cannot win."""
    failures: dict[str, str] = {}
    async with httpx.AsyncClient(timeout=_PROBE_TIMEOUT, follow_redirects=False) as client:

        async def probe(endpoint: str) -> str | None:
            try:
                # Smart HTTP stays on the advertised cluster URL: avrea-git
                # forwards authenticated Git requests to its owner internally.
                # Only read headers, since large repositories have large adverts.
                async with client.stream(
                    "GET",
                    f"{clone_url(endpoint, full_name)}/info/refs",
                    params={"service": "git-upload-pack"},
                    headers={"Authorization": f"Bearer {token}"},
                ) as response:
                    if response.status_code == 200 and response.headers.get("content-type", "").split(";")[0] == (
                        "application/x-git-upload-pack-advertisement"
                    ):
                        return endpoint
                    failures[endpoint] = f"HTTP {response.status_code}"
            except httpx.HTTPError as exc:
                failures[endpoint] = type(exc).__name__
            return None

        tasks = [asyncio.create_task(probe(endpoint)) for endpoint in endpoints]
        try:
            async with asyncio.timeout(_PROBE_TIMEOUT):
                for result in asyncio.as_completed(tasks):
                    if endpoint := await result:
                        return endpoint
        except TimeoutError:
            # Report the unfinished probes as timed out in the failure summary below.
            pass
        finally:
            for task in tasks:
                task.cancel()
            await asyncio.gather(*tasks, return_exceptions=True)
    detail = "; ".join(f"{endpoint}: {failures.get(endpoint, 'timed out')}" for endpoint in endpoints)
    reasons = set(failures.values())
    hints = []
    if reasons & {"HTTP 401", "HTTP 403"}:
        hints.append("Check your Avrea login and repository permissions.")
    if "HTTP 404" in reasons:
        hints.append("Check `avr repo mirror status` and retry after the repository has synced.")
    if len(failures) < len(endpoints) or any(
        reason.startswith("HTTP 5") or not reason.startswith("HTTP ") for reason in reasons
    ):
        hints.append("Some mirrors are unreachable or unavailable; check connectivity and `avr health`.")
    if not hints:
        hints.append("The mirrors did not return a Git advertisement; check `avr health`.")
    raise click.ClickException(f"No mirror could serve this repository ({detail}). {' '.join(hints)}")


def select_placement(endpoints: list[str], placement: str) -> str:
    matches = [ep for ep in endpoints if placement.rstrip("/") in {ep, urlsplit(ep).netloc}]
    if len(matches) == 1:
        return matches[0]
    raise click.BadParameter(
        "must match an advertised mirror URL or hostname. Available placements: " + ", ".join(endpoints),
        param_hint="--placement",
    )


def credential_config(api_host: str, url: str, full_name: str) -> list[str]:
    """Persist only a helper command, scoped to this mirror and repository."""
    executable = shutil.which("avr")
    # Git invokes helpers from the checkout, not the directory of `repo clone`.
    executable = os.path.abspath(executable) if executable else "avr"
    command = f"AVR_HOST={shlex.quote(api_host)} " + shlex.join(
        [executable, "auth", "git-credential", "--url", url, "--repo", full_name]
    )
    # Also fail closed if the installed executable has since moved or startup
    # fails before the credential adapter runs. Git appends the operation.
    helper = f'!f() {{ {command} "$@" || {{ printf "quit=1\\n\\n"; exit 1; }}; }}; f'
    return [
        # Reset inherited helpers for this URL, so they neither supply stale
        # JWTs nor store the temporary credentials returned by our helper.
        f"credential.{url}.helper=",
        f"credential.{url}.helper={helper}",
        f"credential.{url}.useHttpPath=true",
        f"http.{url}.followRedirects=false",
    ]


def clone_mirror(
    api_host: str,
    endpoint: str,
    full_name: str,
    directory: str | None,
    *,
    branch: str | None,
    depth: int | None,
) -> int:
    url = clone_url(endpoint, full_name)
    env = os.environ.copy()
    # Secrets must not appear in argv, Git config, or user-enabled trace logs.
    for key in list(env):
        if key.startswith("GIT_TRACE") or key == "GIT_CURL_VERBOSE":
            del env[key]
    env["GIT_TERMINAL_PROMPT"] = "0"
    env["GIT_ASKPASS"] = ""
    env["SSH_ASKPASS"] = ""
    argv = ["git", "-c", f"http.{url}.extraHeader=", "clone"]
    for setting in credential_config(api_host, url, full_name):
        argv.extend(["--config", setting])
    if branch is not None:
        argv.extend(["--branch", branch])
    if depth is not None:
        argv.extend(["--depth", str(depth)])
    argv.extend(["--", url, directory if directory is not None else full_name.rsplit("/", 1)[1]])
    try:
        return subprocess.run(argv, env=env).returncode
    except FileNotFoundError:
        raise click.ClickException("Git is required. Install git and try again.") from None
    except OSError:
        raise click.ClickException("Could not start git clone.") from None
