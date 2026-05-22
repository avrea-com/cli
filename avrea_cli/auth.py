"""CLI authentication via browser OAuth flow.

Credentials live in ``hosts.json`` keyed by full API URL:

    {
      "default_host": "https://api.avrea.com",
      "hosts": {
        "https://api.avrea.com": {
          "token": "avk_...",
          "default_org": "org-..."
        }
      }
    }

``default_host`` is the URL used when ``AVR_HOST`` isn't set. It's written
on first login (when there are no other entries yet) and updated explicitly
via ``avr auth switch <url>``. Multiple host entries accumulate naturally
when the user logs in to additional API endpoints (e.g. self-hosted Avrea).
"""

from avrea_cli.paths import PATHS
from http.server import BaseHTTPRequestHandler
from http.server import HTTPServer
from pathlib import Path
from typing import Any
from typing import override
from urllib.parse import parse_qs
from urllib.parse import urlparse
import click
import html as html_module
import httpx
import json
import os
import socket
import webbrowser

# Sibling asset so the file stays byte-identical to the console's Logo.
# Update this file when the console Logo changes.
_LOGO_SVG = (Path(__file__).parent / "assets" / "logo_mark.svg").read_text(encoding="utf-8")

HOSTS_FILE = PATHS.hosts_file


def _read() -> dict[str, Any]:
    """Load the hosts file. Returns an empty wrapper if missing or malformed
    — callers treat absence as 'no credentials yet'."""
    empty: dict[str, Any] = {"default_host": None, "hosts": {}}
    if not HOSTS_FILE.exists():
        return empty
    try:
        data = json.loads(HOSTS_FILE.read_text())
    except OSError, ValueError:
        return empty
    if not isinstance(data, dict):
        return empty
    hosts = data.get("hosts")
    if not isinstance(hosts, dict):
        return empty
    default_host = data.get("default_host")
    return {
        "default_host": default_host if isinstance(default_host, str) and default_host in hosts else None,
        "hosts": hosts,
    }


def _write(payload: dict[str, Any]) -> None:
    HOSTS_FILE.parent.mkdir(parents=True, exist_ok=True)
    HOSTS_FILE.write_text(json.dumps(payload, indent=2))
    # Contains a bearer token; refuse group/other read. On Windows os.chmod
    # only toggles the read-only bit — the file inherits NTFS ACLs from
    # %USERPROFILE%, which is typically already user-private. A dedicated
    # icacls / pywin32 hardening pass is a TODO if we ship on shared Windows
    # boxes.
    HOSTS_FILE.chmod(0o600)


def list_hosts() -> list[str]:
    """All hosts the user has stored credentials for, in insertion order."""
    return list(_read()["hosts"].keys())


def load_default_host() -> str | None:
    """The pinned default URL, or None if unset.

    ``CliConfig`` falls back to ``https://api.avrea.com`` when this is None,
    so a brand-new install still has somewhere to point ``avr auth login``.
    """
    return _read()["default_host"]


def set_default_host(host: str) -> None:
    """Pin ``host`` as the default for commands without ``AVR_HOST``. The
    host must already have credentials stored; we don't auto-create empty
    entries — that would let `auth switch` silently misconfigure the file."""
    payload = _read()
    if host not in payload["hosts"]:
        raise KeyError(host)
    payload["default_host"] = host
    _write(payload)


def load_token(*, host: str) -> str | None:
    """API key for ``host``, or None if not authenticated against it."""
    entry = _read()["hosts"].get(host)
    if not isinstance(entry, dict):
        return None
    token = entry.get("token")
    return token if isinstance(token, str) and token else None


def store_token(token: str, *, host: str) -> None:
    """Persist ``token`` under ``host``. Preserves any default_org already
    there. Promotes ``host`` to ``default_host`` when no default is set —
    a first-time login should "just work" without an extra ``auth switch``.
    """
    payload = _read()
    entry = payload["hosts"].setdefault(host, {})
    if not isinstance(entry, dict):
        # Replaces a corrupted entry rather than throwing — the auth flow
        # has already issued a valid key, refusing to save would be worse.
        entry = {}
        payload["hosts"][host] = entry
    entry["token"] = token
    if not payload["default_host"]:
        payload["default_host"] = host
    _write(payload)


def load_default_org(*, host: str) -> str | None:
    entry = _read()["hosts"].get(host)
    if not isinstance(entry, dict):
        return None
    org = entry.get("default_org")
    return org if isinstance(org, str) and org else None


def store_default_org(org_id: str, *, host: str) -> None:
    payload = _read()
    entry = payload["hosts"].setdefault(host, {})
    if not isinstance(entry, dict):
        entry = {}
        payload["hosts"][host] = entry
    entry["default_org"] = org_id
    _write(payload)


def clear_default_org(*, host: str) -> bool:
    """Drop the stored default org for ``host``. Returns True if the entry
    existed. Leaves the rest of the host record (token, etc.) intact."""
    payload = _read()
    entry = payload["hosts"].get(host)
    if not isinstance(entry, dict) or "default_org" not in entry:
        return False
    del entry["default_org"]
    _write(payload)
    return True


def clear(*, host: str) -> bool:
    """Remove the host's entry. Returns True if anything was removed.

    If the cleared host was ``default_host``, promote another stored host
    to default (insertion order) so the CLI keeps working without a manual
    ``auth switch``. If no hosts remain, unlink the file entirely."""
    payload = _read()
    if host not in payload["hosts"]:
        return False
    payload["hosts"].pop(host)
    if payload["default_host"] == host:
        remaining = list(payload["hosts"].keys())
        payload["default_host"] = remaining[0] if remaining else None
    if payload["hosts"]:
        _write(payload)
    else:
        # Empty file would still satisfy `hosts.json exists`; remove it so a
        # fresh `auth login` starts from a clean state.
        try:
            HOSTS_FILE.unlink()
        except FileNotFoundError:
            pass
    return True


# ---------------------------------------------------------------------------
# Browser OAuth flow
# ---------------------------------------------------------------------------


class OAuthCallbackHandler(BaseHTTPRequestHandler):
    """Handle OAuth callback and extract session cookie and CSRF token."""

    session_cookie = None
    csrf_token = None
    # Set by login() before the server runs so the handler can call /users/me
    # with the just-issued session cookie and surface the email on the page.
    public_api_url: str | None = None

    @override
    def log_message(self, format, *args):
        pass

    def do_GET(self):
        # Query-param path covers localhost redirects; the cookie-header path
        # covers same-domain redirects.
        query = parse_qs(urlparse(self.path).query)
        if "session" in query:
            OAuthCallbackHandler.session_cookie = query["session"][0]
        if "csrf_token" in query:
            OAuthCallbackHandler.csrf_token = query["csrf_token"][0]
        if not OAuthCallbackHandler.session_cookie:
            cookie_header = self.headers.get("Cookie", "")
            if "avrea_session=" in cookie_header:
                for cookie in cookie_header.split(";"):
                    if "avrea_session=" in cookie:
                        OAuthCallbackHandler.session_cookie = cookie.split("=", 1)[1].strip()

        email = self._fetch_email()

        self.send_response(200)
        self.send_header("Content-Type", "text/html")
        self.end_headers()
        self.wfile.write(_render_success_html(email).encode("utf-8"))

    def _fetch_email(self) -> str | None:
        """Best-effort lookup so the page can show 'leo@acme.com' instead of
        'CLI session connected'. Failures are silent — the page still renders."""
        if not OAuthCallbackHandler.session_cookie or not OAuthCallbackHandler.public_api_url:
            return None
        try:
            r = httpx.get(
                f"{OAuthCallbackHandler.public_api_url}/users/me",
                cookies={"avrea_session": OAuthCallbackHandler.session_cookie},
                timeout=3.0,
            )
            r.raise_for_status()
            return r.json().get("email")
        except httpx.HTTPError, ValueError, KeyError:
            return None


def _render_success_html(email: str | None) -> str:
    """Render the post-login confirmation page. Mirrors the console's
    ``NotLoggedIn`` layout (LogoMark, Sorts Mill Goudy headline, theme palette).
    Self-contained: SVG and palette are inlined; the only network fetch is the
    Google Font."""
    email_block = f'<p class="email">{html_module.escape(email)}</p>' if email else ""
    return f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Authenticated · Avrea</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Sorts+Mill+Goudy&amp;display=swap" rel="stylesheet">
<style>
:root {{
  color-scheme: dark;
  --bg: linear-gradient(306deg, #0e1018 8.16%, #1a1622 89.66%);
  --fg: #f1f1f1;
  --muted: #a09aae;
  --card: #26222e;
  --card-border: rgba(255, 255, 255, 0.08);
  --accent: #b7bdeb;
  --code-bg: #1b1623;
}}
* {{ box-sizing: border-box; }}
html, body {{ margin: 0; padding: 0; min-height: 100dvh; }}
body {{
  background: var(--bg);
  color: var(--fg);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
}}
.box {{
  background: var(--card);
  border: 1px solid var(--card-border);
  border-radius: 12px;
  padding: 32px;
  width: 100%;
  max-width: 480px;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.4);
}}
.logo {{
  display: block;
}}
.logo svg {{
  height: 32px;
  width: auto;
  display: block;
}}
.headline {{
  font-family: "Sorts Mill Goudy", Iowan Old Style, "Palatino Linotype", Georgia, serif;
  font-size: 30px;
  line-height: 1.1;
  margin: 24px 0 0;
  color: var(--fg);
  font-weight: 400;
}}
.subhead {{
  margin: 16px 0 0;
  font-size: 15px;
  color: var(--fg);
}}
.email {{
  margin: 6px 0 0;
  font-size: 14px;
  color: var(--muted);
}}
hr {{
  border: 0;
  border-top: 1px solid var(--card-border);
  margin: 28px 0 24px;
  width: 100%;
}}
.next, .close {{
  margin: 0;
  font-size: 14px;
  color: var(--muted);
}}
.code-row {{
  margin: 12px 0 24px;
}}
code {{
  display: inline-block;
  background: var(--code-bg);
  color: var(--accent);
  padding: 8px 12px;
  border-radius: 8px;
  font-family: "SF Mono", Menlo, Consolas, "Liberation Mono", monospace;
  font-size: 13px;
}}
</style>
</head>
<body>
  <div class="box">
    <span class="logo">{_LOGO_SVG}</span>
    <h1 class="headline">You're all set.</h1>
    <p class="subhead">Your CLI session is now connected.</p>
    {email_block}
    <hr>
    <p class="next">Head back to your terminal and try:</p>
    <p class="code-row"><code>&gt; avr status</code></p>
    <p class="close">You can close this tab now.</p>
  </div>
</body>
</html>
"""


def login(public_api_url: str, *, provider: str = "github") -> str:
    """
    Perform browser OAuth login and return the API key.
    Raises click.Abort on failure.
    """
    # Control plane 302s here after the upstream OAuth callback completes;
    # localhost is not registered with the upstream provider directly —
    # see oauth.py allowed_redirect_hosts.
    port = 8765
    callback_uri = f"http://localhost:{port}/callback"

    # Handler class state must be cleared per-login: BaseHTTPRequestHandler
    # is instantiated by HTTPServer per-request so we can't pass per-call
    # values through the constructor, and a prior aborted run would
    # otherwise leave stale session/csrf values behind.
    OAuthCallbackHandler.session_cookie = None
    OAuthCallbackHandler.csrf_token = None
    OAuthCallbackHandler.public_api_url = public_api_url

    try:
        server = HTTPServer(("localhost", port), OAuthCallbackHandler)
    except OSError as e:
        click.echo(f"Error: Cannot bind to port {port}: {e}", err=True)
        click.echo("Ensure no other process is using this port and try again.")
        raise click.Abort() from None

    if provider == "google":
        auth_url = f"{public_api_url}/oauth/google/login?final_redirect={callback_uri}"
    elif provider == "github":
        auth_url = f"{public_api_url}/oauth/github/login?next={callback_uri}"
    else:
        raise click.ClickException(f"Unsupported auth provider '{provider}'")
    click.echo(f"Opening browser to: {auth_url}")
    click.echo("Waiting for authentication...")

    # AVR_BROWSER takes precedence over BROWSER. Python's webbrowser honors
    # BROWSER directly, so promote AVR_BROWSER into BROWSER for the open() call.
    avr_browser = os.environ.get("AVR_BROWSER")
    saved_browser = os.environ.get("BROWSER")
    if avr_browser:
        os.environ["BROWSER"] = avr_browser
    try:
        opened = webbrowser.open(auth_url)
    finally:
        if avr_browser:
            if saved_browser is None:
                os.environ.pop("BROWSER", None)
            else:
                os.environ["BROWSER"] = saved_browser

    if not opened:
        click.echo("Failed to open browser. Please visit:", err=True)
        click.echo(auth_url)

    server.timeout = 300
    try:
        server.handle_request()
    finally:
        # Release the listening socket so a retry in the same process (or a
        # follow-up TUI flow) doesn't hit "Address already in use" while
        # CPython GC catches up.
        server.server_close()

    if not OAuthCallbackHandler.session_cookie:
        click.echo("Error: Authentication failed - no session received", err=True)
        raise click.Abort()

    # Security note: the session token arrives as a query parameter to localhost.
    # Query parameters may appear in browser history. The session is short-lived
    # and exchanged immediately for an API key, then invalidated below.
    try:
        headers = {}
        if OAuthCallbackHandler.csrf_token:
            headers["X-CSRF-Token"] = OAuthCallbackHandler.csrf_token
        response = httpx.post(
            f"{public_api_url}/users/me/api-keys",
            cookies={"avrea_session": OAuthCallbackHandler.session_cookie},
            headers=headers,
            json={"name": f"CLI - {socket.gethostname()}", "expires_days": 90},
            timeout=10.0,
        )
        response.raise_for_status()
        api_key = response.json()["api_key"]

        try:
            logout_headers: dict[str, str] = {}
            if OAuthCallbackHandler.csrf_token:
                logout_headers["X-CSRF-Token"] = OAuthCallbackHandler.csrf_token
            httpx.post(
                f"{public_api_url}/users/me/logout",
                cookies={"avrea_session": OAuthCallbackHandler.session_cookie},
                headers=logout_headers,
                timeout=5.0,
            )
        except httpx.HTTPError:
            pass  # Best-effort; the session will expire on its own

        return api_key
    except httpx.HTTPError as e:
        click.echo(f"Error: Failed to create API key: {e}", err=True)
        raise click.Abort() from None
