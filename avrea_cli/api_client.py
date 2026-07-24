"""HTTP client for Avrea APIs."""

from avrea_cli.config import CliConfig
from avrea_cli.transport import CompressingTransport
from typing import Any
import click
import httpx


class ApiClient:
    """Simple HTTP client for Avrea APIs."""

    def __init__(self, config: CliConfig, http_client: httpx.Client | None = None, *, verbose: bool = False):
        self.config = config
        self.timeout = 30.0
        self.verbose = verbose
        self._http = http_client if http_client is not None else httpx.Client(transport=CompressingTransport())

    def _log(self, method: str, url: str, status: int | str) -> None:
        """Print one request to stderr when verbose is on. Single-line so it
        composes with grep/tee. ``status`` may be a numeric HTTP code or a
        short error label for transport failures (timeout, connect error)."""
        if not self.verbose:
            return
        styled_method = click.style(method, fg="cyan")
        if isinstance(status, int):
            if 200 <= status < 300:
                styled_status = click.style(str(status), fg="green")
            elif status >= 400:
                styled_status = click.style(str(status), fg="red")
            else:
                styled_status = str(status)
        else:
            styled_status = click.style(status, fg="red")
        click.echo(f"  → {styled_method} {url} [{styled_status}]", err=True)

    def public_get(self, path: str, params: dict[str, Any] | None = None) -> dict[str, Any]:
        """GET request to public API."""
        url = f"{self.config.public_api_url}{path}"
        try:
            response = self._http.get(url, headers=self.config.get_api_headers(), timeout=self.timeout, params=params)
        except httpx.HTTPError as exc:
            self._log("GET", url, type(exc).__name__)
            raise
        self._log("GET", str(response.request.url), response.status_code)
        response.raise_for_status()
        return response.json()

    def public_get_text(self, path: str, params: dict[str, Any] | None = None) -> str:
        """GET a non-JSON public API response, such as SAML SP metadata."""
        url = f"{self.config.public_api_url}{path}"
        try:
            response = self._http.get(url, headers=self.config.get_api_headers(), timeout=self.timeout, params=params)
        except httpx.HTTPError as exc:
            self._log("GET", url, type(exc).__name__)
            raise
        self._log("GET", str(response.request.url), response.status_code)
        response.raise_for_status()
        return response.text

    def public_post(
        self,
        path: str,
        json: dict[str, Any] | None = None,
        timeout: float | None = None,
        *,
        content: str | bytes | None = None,
        params: dict[str, Any] | None = None,
        content_type: str | None = None,
    ) -> dict[str, Any]:
        """POST request to public API."""
        if json is not None and content is not None:
            raise ValueError("public_post accepts either json or content, not both")
        url = f"{self.config.public_api_url}{path}"
        effective_timeout = timeout if timeout is not None else self.timeout
        headers = self.config.get_api_headers()
        if content_type is not None:
            headers = {**headers, "Content-Type": content_type}
        try:
            response = self._http.post(
                url,
                headers=headers,
                timeout=effective_timeout,
                json=json,
                content=content,
                params=params,
            )
        except httpx.HTTPError as exc:
            self._log("POST", url, type(exc).__name__)
            raise
        self._log("POST", str(response.request.url), response.status_code)
        response.raise_for_status()
        return response.json()

    def public_put(self, path: str, json: dict[str, Any] | None = None) -> dict[str, Any]:
        """PUT request to public API."""
        url = f"{self.config.public_api_url}{path}"
        try:
            response = self._http.put(url, headers=self.config.get_api_headers(), timeout=self.timeout, json=json)
        except httpx.HTTPError as exc:
            self._log("PUT", url, type(exc).__name__)
            raise
        self._log("PUT", str(response.request.url), response.status_code)
        response.raise_for_status()
        return response.json()

    def public_patch(self, path: str, json: dict[str, Any] | None = None) -> dict[str, Any] | None:
        """PATCH request to public API.

        Mirrors ``public_delete``'s empty-response handling: a 204 No
        Content or an empty body returns ``None`` rather than raising
        a JSON-decode error. Existing PATCH endpoints all return the
        updated row today, but new ones may legitimately not, and the
        non-JSON crash on the client would mask the actual outcome.
        """
        url = f"{self.config.public_api_url}{path}"
        try:
            response = self._http.patch(url, headers=self.config.get_api_headers(), timeout=self.timeout, json=json)
        except httpx.HTTPError as exc:
            self._log("PATCH", url, type(exc).__name__)
            raise
        self._log("PATCH", str(response.request.url), response.status_code)
        response.raise_for_status()
        if response.status_code == 204 or not response.content:
            return None
        return response.json()

    def public_delete(self, path: str, params: dict[str, Any] | None = None) -> dict[str, Any] | None:
        """DELETE request to public API."""
        url = f"{self.config.public_api_url}{path}"
        try:
            response = self._http.delete(
                url, headers=self.config.get_api_headers(), timeout=self.timeout, params=params
            )
        except httpx.HTTPError as exc:
            self._log("DELETE", url, type(exc).__name__)
            raise
        self._log("DELETE", str(response.request.url), response.status_code)
        response.raise_for_status()
        if response.status_code == 204 or not response.content:
            return None
        return response.json()
