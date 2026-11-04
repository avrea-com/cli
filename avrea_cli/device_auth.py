"""Browserless CLI login using RFC 8628 device authorization."""

from avrea_cli.version import USER_AGENT
from email.utils import parsedate_to_datetime
from io import StringIO
from pydantic import BaseModel
from pydantic import Field
from pydantic import ValidationError
from pydantic import field_validator
import click
import httpx
import math
import os
import segno
import shutil
import sys
import time

CLIENT_ID = "avr-cli"
GRANT_TYPE = "urn:ietf:params:oauth:grant-type:device_code"
REQUEST_TIMEOUT = 15.0


class AuthorizationServer(BaseModel):
    issuer: str
    device_authorization_endpoint: str
    token_endpoint: str


class DeviceRequest(BaseModel):
    device_code: str = Field(min_length=1, repr=False)
    user_code: str = Field(min_length=1, max_length=32)
    verification_uri: str
    verification_uri_complete: str | None = None
    expires_in: int = Field(gt=0, strict=True)
    # RFC 8628 sections 3.2 and 3.5 specify this default, the slow_down
    # increment, and timeout backoff: https://www.rfc-editor.org/rfc/rfc8628.html#section-3.5
    interval: int = Field(default=5, gt=0, strict=True)

    @field_validator("user_code")
    @classmethod
    def printable_code(cls, value: str) -> str:
        if not value.isascii() or not value.isprintable():
            raise ValueError("User code must be printable ASCII")
        return value


class DeviceToken(BaseModel):
    access_token: str = Field(min_length=1, repr=False)
    token_type: str
    expires_in: int = Field(gt=0, strict=True)
    scope: str

    @field_validator("token_type")
    @classmethod
    def bearer_token(cls, value: str) -> str:
        if value.lower() != "bearer":
            raise ValueError("Expected a bearer token")
        return value

    @field_validator("scope")
    @classmethod
    def avrea_scope(cls, value: str) -> str:
        if value != "avrea":
            raise ValueError("Unexpected scope")
        return value


class OAuthError(BaseModel):
    error: str


def _safe_url(value: str) -> httpx.URL:
    try:
        if any(char.isspace() or ord(char) < 32 or ord(char) == 127 for char in value):
            raise ValueError("Invalid URL characters")
        url = httpx.URL(value)
        secure = url.scheme == "https" or (url.scheme == "http" and url.host in {"localhost", "127.0.0.1", "::1"})
        if not secure or not url.host or url.userinfo or url.fragment:
            raise ValueError("Unsafe URL")
    except ValueError, httpx.InvalidURL:
        raise click.ClickException("The server returned an unsafe device-login URL.") from None
    return url


def _origin(url: httpx.URL) -> tuple[str, str, int | None]:
    return url.scheme, url.host, url.port


def _parse_response[T: BaseModel](response: httpx.Response, model: type[T]) -> T:
    try:
        return model.model_validate_json(response.content)
    except ValidationError:
        # Validation errors can include device secrets and access tokens.
        raise click.ClickException("The server returned an invalid device-login response.") from None


def _failure(response: httpx.Response) -> click.ClickException:
    try:
        error = OAuthError.model_validate_json(response.content).error
    except ValidationError:
        error = ""
    messages = {
        "access_denied": "Device access was denied. Start a new login if needed.",
        "expired_token": "The login code expired. Run avr auth device-login again.",
        "invalid_grant": "This login request is no longer usable. Run avr auth device-login again.",
        "invalid_client": "This server does not accept this CLI for device login.",
        "invalid_scope": "This server does not support the requested Avrea access.",
        "invalid_request": "The server rejected the device-login request.",
        "unsupported_grant_type": "This server does not support device login.",
    }
    if response.status_code == 429:
        return click.ClickException("Too many login attempts. Wait a minute before trying again.")
    return click.ClickException(messages.get(error, f"Device login failed (HTTP {response.status_code}). Try again."))


def _show_qr(url: str) -> None:
    stream = sys.stdout
    context = click.get_current_context(silent=True)
    if not stream.isatty() or os.environ.get("TERM") == "dumb" or (context is not None and context.color is False):
        return
    try:
        "▀▄█".encode(stream.encoding or "utf-8")
        qr = segno.make_qr(url, error="m")
    except UnicodeEncodeError, LookupError, segno.DataOverflowError:
        return
    if qr.symbol_size()[0] >= shutil.get_terminal_size().columns:
        return
    output = StringIO()
    qr.terminal(out=output, compact=True)
    click.echo("\nOr scan with your phone:")
    # Segno's compact renderer draws light modules as blocks. Explicit colors
    # keep the quiet zone white on both light and dark terminal themes.
    for line in output.getvalue().splitlines():
        click.echo(click.style(line, fg="white", bg="black"), color=True)


def _show_request(request: DeviceRequest, *, show_qr: bool) -> None:
    link = request.verification_uri_complete or request.verification_uri
    click.echo(f"Open this link in a browser on any device:\n\n  {link}\n")
    click.echo(f"Code: {request.user_code}")
    if request.verification_uri_complete:
        click.echo(f"For manual code entry: {request.verification_uri}")
    if show_qr:
        _show_qr(link)
    click.echo(f"\nWaiting for approval (expires in {request.expires_in} seconds). Press Ctrl+C to cancel.")


def _retry_after(response: httpx.Response) -> float:
    value = response.headers.get("Retry-After", "")
    try:
        seconds = float(value)
    except ValueError:
        try:
            seconds = parsedate_to_datetime(value).timestamp() - time.time()
        except ValueError, TypeError, OverflowError:
            return 0
    return max(0, seconds) if math.isfinite(seconds) else 0


def _poll(client: httpx.Client, endpoint: str, request: DeviceRequest, deadline: float) -> str:
    interval = float(request.interval)
    delay = interval
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise click.ClickException("The login code expired. Run avr auth device-login again.")
        time.sleep(min(delay, remaining))
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise click.ClickException("The login code expired. Run avr auth device-login again.")
        try:
            response = client.post(
                endpoint,
                data={"client_id": CLIENT_ID, "grant_type": GRANT_TYPE, "device_code": request.device_code},
                timeout=min(REQUEST_TIMEOUT, remaining),
            )
        except httpx.TimeoutException, httpx.ConnectError:
            # RFC 8628 section 3.5 recommends exponential timeout backoff.
            interval *= 2
            delay = interval
            continue
        if response.status_code == 200:
            return _parse_response(response, DeviceToken).access_token
        try:
            error = OAuthError.model_validate_json(response.content).error
        except ValidationError:
            error = ""
        if error == "slow_down":
            interval += 5
        elif error == "temporarily_unavailable" and response.status_code == 503:
            interval *= 2
        elif error != "authorization_pending" and response.status_code != 429:
            raise _failure(response)
        delay = max(interval, _retry_after(response))


def login(public_api_url: str, *, show_qr: bool = True) -> str:
    """Obtain a credential without launching a browser or listening locally."""
    base = _safe_url(public_api_url)
    try:
        with httpx.Client(
            timeout=REQUEST_TIMEOUT, headers={"User-Agent": USER_AGENT}, follow_redirects=False
        ) as client:
            response = client.get(f"{public_api_url}/.well-known/oauth-authorization-server")
            if response.status_code in {404, 405, 501}:
                raise click.ClickException("This server does not support device login yet. Use avr auth login instead.")
            if response.status_code != 200:
                raise _failure(response)
            server = _parse_response(response, AuthorizationServer)
            if server.issuer.rstrip("/") != public_api_url.rstrip("/"):
                raise click.ClickException("The device-login issuer does not match the configured API host.")
            for endpoint in (server.device_authorization_endpoint, server.token_endpoint):
                if _origin(_safe_url(endpoint)) != _origin(base):
                    raise click.ClickException("Device-login endpoints must use the configured API origin.")
            started = time.monotonic()
            response = client.post(
                server.device_authorization_endpoint, data={"client_id": CLIENT_ID, "scope": "avrea"}
            )
            if response.status_code != 200:
                raise _failure(response)
            request = _parse_response(response, DeviceRequest)
            verification = _safe_url(request.verification_uri)
            if request.verification_uri_complete:
                if _origin(_safe_url(request.verification_uri_complete)) != _origin(verification):
                    raise click.ClickException("The device-login links must use the same console origin.")
            _show_request(request, show_qr=show_qr)
            return _poll(client, server.token_endpoint, request, started + request.expires_in)
    except httpx.RequestError:
        raise click.ClickException(
            "Could not reach the device-login service. Check your connection and try again."
        ) from None
