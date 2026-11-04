"""Device flow contracts: HTTP, polling, output, and credential preservation."""

from avrea_cli import auth
from avrea_cli import device_auth
from avrea_cli.commands import auth_cmd
from avrea_cli.main import cli
from click.testing import CliRunner
from io import StringIO
from types import SimpleNamespace
from unittest.mock import Mock
from urllib.parse import parse_qs
import click
import httpx
import os
import pytest
import segno

API = "https://api.example.com"
VERIFY = "https://console.example.com/device"
LINK = VERIFY + "?user_code=BCDF-GHJK"
DEVICE_SECRET = "private-device-secret"
ACCESS_TOKEN = "avk_private-access-token"
METADATA = {
    "issuer": API,
    "device_authorization_endpoint": API + "/oauth/device_authorization",
    "token_endpoint": API + "/oauth/token",
}
START = {
    "device_code": DEVICE_SECRET,
    "user_code": "BCDF-GHJK",
    "verification_uri": VERIFY,
    "verification_uri_complete": LINK,
    "expires_in": 600,
    "interval": 5,
}
TOKEN = {"access_token": ACCESS_TOKEN, "token_type": "Bearer", "expires_in": 7776000, "scope": "avrea"}


@pytest.fixture
def flow(monkeypatch: pytest.MonkeyPatch) -> SimpleNamespace:
    state = SimpleNamespace(
        now=0.0,
        sleeps=[],
        requests=[],
        polls=[],
        responses=[],
        metadata=METADATA.copy(),
        start=START.copy(),
        discovery_status=200,
        start_status=200,
    )

    def sleep(seconds: float) -> None:
        state.sleeps.append(seconds)
        state.now += seconds

    def handle(request: httpx.Request) -> httpx.Response:
        state.requests.append(request)
        assert "authorization" not in request.headers
        if request.url.path == "/.well-known/oauth-authorization-server":
            return httpx.Response(state.discovery_status, json=state.metadata)
        if request.url.path == "/oauth/device_authorization":
            assert parse_qs(request.content.decode()) == {"client_id": ["avr-cli"], "scope": ["avrea"]}
            return httpx.Response(state.start_status, json=state.start)
        assert request.url.path == "/oauth/token"
        assert parse_qs(request.content.decode()) == {
            "client_id": ["avr-cli"],
            "grant_type": [device_auth.GRANT_TYPE],
            "device_code": [DEVICE_SECRET],
        }
        state.polls.append(state.now)
        result = state.responses.pop(0)
        if isinstance(result, Exception):
            raise result
        return result

    client = httpx.Client
    monkeypatch.setattr(
        device_auth.httpx, "Client", lambda **kw: client(**(kw | {"transport": httpx.MockTransport(handle)}))
    )
    monkeypatch.setattr(device_auth.time, "monotonic", lambda: state.now)
    monkeypatch.setattr(device_auth.time, "sleep", sleep)
    return state


def error(code: str, status: int = 400, **headers: str) -> httpx.Response:
    return httpx.Response(status, json={"error": code}, headers=headers)


def test_pending_then_success_displays_public_information_only(
    flow: SimpleNamespace, capsys: pytest.CaptureFixture[str]
) -> None:
    flow.responses = [error("authorization_pending"), httpx.Response(200, json=TOKEN)]
    assert device_auth.login(API) == ACCESS_TOKEN
    assert flow.polls == [5, 10]
    output = capsys.readouterr().out
    assert LINK in output and VERIFY in output and "BCDF-GHJK" in output
    assert "600 seconds" in output
    assert DEVICE_SECRET not in output and ACCESS_TOKEN not in output
    assert "█" not in output


def test_default_interval_and_missing_complete_url(flow: SimpleNamespace, capsys: pytest.CaptureFixture[str]) -> None:
    flow.start.pop("interval")
    flow.start.pop("verification_uri_complete")
    flow.responses = [httpx.Response(200, json=TOKEN)]
    assert device_auth.login(API) == ACCESS_TOKEN
    assert flow.polls == [5]
    assert VERIFY in capsys.readouterr().out


def test_slow_down_increases_all_subsequent_intervals(flow: SimpleNamespace) -> None:
    flow.responses = [
        error("slow_down"),
        error("authorization_pending"),
        error("slow_down"),
        httpx.Response(200, json=TOKEN),
    ]
    assert device_auth.login(API) == ACCESS_TOKEN
    assert flow.polls == [5, 15, 25, 40]


@pytest.mark.parametrize(
    "failure", [httpx.ConnectTimeout("secret"), httpx.ReadTimeout("secret"), httpx.ConnectError("secret")]
)
def test_network_failures_back_off_until_success(flow: SimpleNamespace, failure: Exception) -> None:
    flow.responses = [failure, error("authorization_pending"), httpx.Response(200, json=TOKEN)]
    assert device_auth.login(API) == ACCESS_TOKEN
    assert flow.polls == [5, 15, 25]


def test_retry_after_and_service_outage(flow: SimpleNamespace) -> None:
    flow.responses = [
        error("slow_down", 429, **{"Retry-After": "60"}),
        error("temporarily_unavailable", 503),
        httpx.Response(200, json=TOKEN),
    ]
    assert device_auth.login(API) == ACCESS_TOKEN
    assert flow.polls == [5, 65, 85]


@pytest.mark.parametrize("retry_after", ["invalid", "NaN", "Infinity", "-1"])
def test_invalid_retry_after_does_not_create_a_busy_loop(flow: SimpleNamespace, retry_after: str) -> None:
    flow.responses = [httpx.Response(429, headers={"Retry-After": retry_after}), httpx.Response(200, json=TOKEN)]
    assert device_auth.login(API) == ACCESS_TOKEN
    assert flow.polls == [5, 10]


def test_retry_after_http_date(flow: SimpleNamespace, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(device_auth.time, "time", lambda: 0)
    flow.responses = [
        httpx.Response(429, headers={"Retry-After": "Thu, 01 Jan 1970 00:01:00 GMT"}),
        httpx.Response(200, json=TOKEN),
    ]
    assert device_auth.login(API) == ACCESS_TOKEN
    assert flow.polls == [5, 65]


def test_expiry_bounds_sleep_and_prevents_more_requests(flow: SimpleNamespace) -> None:
    flow.start["expires_in"] = 12
    flow.responses = [httpx.Response(429, headers={"Retry-After": "60"})]
    with pytest.raises(click.ClickException, match="expired"):
        device_auth.login(API)
    assert flow.polls == [5]
    assert flow.sleeps == [5, 7]


def test_timeout_is_bounded_by_remaining_lifetime(flow: SimpleNamespace) -> None:
    flow.start["expires_in"] = 7
    flow.responses = [httpx.Response(200, json=TOKEN)]
    device_auth.login(API)
    assert flow.requests[-1].extensions["timeout"]["read"] == 2


@pytest.mark.parametrize(
    "code, message",
    [
        ("access_denied", "denied"),
        ("expired_token", "expired"),
        ("invalid_grant", "no longer usable"),
        ("invalid_client", "does not accept"),
        ("invalid_scope", "requested Avrea access"),
        ("invalid_request", "rejected"),
        ("unsupported_grant_type", "does not support"),
        ("unknown", "HTTP 400"),
    ],
)
def test_terminal_errors_stop_polling(flow: SimpleNamespace, code: str, message: str) -> None:
    flow.responses = [error(code)]
    with pytest.raises(click.ClickException, match=message):
        device_auth.login(API)
    assert flow.polls == [5]


@pytest.mark.parametrize("status", [404, 405, 501])
def test_old_servers_offer_existing_login(flow: SimpleNamespace, status: int) -> None:
    flow.discovery_status = status
    with pytest.raises(click.ClickException, match="Use avr auth login instead"):
        device_auth.login(API)
    assert len(flow.requests) == 1


@pytest.mark.parametrize(
    "field,value",
    [
        ("issuer", "https://other.example"),
        ("token_endpoint", "https://other.example/token"),
        ("device_authorization_endpoint", "http://api.example.com/start"),
        ("token_endpoint", "https://user:secret@api.example.com/token"),
        ("token_endpoint", "https://api.example.com/token\x1b[31m"),
    ],
)
def test_unsafe_discovery_never_receives_device_secrets(flow: SimpleNamespace, field: str, value: str) -> None:
    flow.metadata[field] = value
    with pytest.raises(click.ClickException):
        device_auth.login(API)
    assert len(flow.requests) == 1


def test_redirect_is_not_followed(flow: SimpleNamespace) -> None:
    flow.responses = [httpx.Response(307, headers={"Location": "https://other.example/token"})]
    with pytest.raises(click.ClickException, match="HTTP 307"):
        device_auth.login(API)
    assert len(flow.requests) == 3


@pytest.mark.parametrize(
    "field,value",
    [
        ("verification_uri", "javascript:alert(1)"),
        ("verification_uri_complete", "https://other.example/device"),
        ("verification_uri_complete", "https://console.example.com/\nspoof"),
        ("user_code", "BCDF\x1bGHJK"),
        ("interval", 0),
        ("interval", True),
        ("expires_in", -1),
        ("device_code", ""),
    ],
)
def test_bad_start_response_is_not_displayed_or_polled(
    flow: SimpleNamespace, field: str, value: object, capsys: pytest.CaptureFixture[str]
) -> None:
    flow.start[field] = value
    with pytest.raises(click.ClickException):
        device_auth.login(API)
    assert not flow.polls
    assert DEVICE_SECRET not in capsys.readouterr().out


@pytest.mark.parametrize(
    "payload",
    [
        {"access_token": ACCESS_TOKEN},
        {**TOKEN, "token_type": "mac"},
        {**TOKEN, "scope": "other"},
        {**TOKEN, "access_token": ""},
    ],
)
def test_bad_token_responses_do_not_expose_secrets(flow: SimpleNamespace, payload: dict[str, object]) -> None:
    flow.responses = [httpx.Response(200, json=payload)]
    with pytest.raises(click.ClickException) as caught:
        device_auth.login(API)
    assert ACCESS_TOKEN not in str(caught.value)
    assert DEVICE_SECRET not in str(caught.value)


class Terminal(StringIO):
    @property
    def encoding(self) -> str:
        return "utf-8"

    def isatty(self) -> bool:
        return True


def test_qr_contains_complete_link_with_quiet_zone(monkeypatch: pytest.MonkeyPatch) -> None:
    stream = Terminal()
    monkeypatch.setattr(device_auth.sys, "stdout", stream)
    monkeypatch.setattr(click, "echo", lambda message, **kw: stream.write(str(message) + "\n"))
    monkeypatch.setattr(device_auth.shutil, "get_terminal_size", lambda **kwargs: os.terminal_size((100, 40)))
    monkeypatch.setenv("TERM", "xterm-256color")
    device_auth._show_qr(LINK)
    output = click.unstyle(stream.getvalue())
    rows = [row for row in output.splitlines() if "█" in row or "▀" in row or "▄" in row]
    # Reconstruct pixels from the displayed halves and compare with the real
    # encoder's modules, including its four-module quiet zone.
    blocks = {" ": (1, 1), "▀": (0, 1), "▄": (1, 0), "█": (0, 0)}
    matrix = []
    for row in rows:
        matrix.extend([[blocks[c][0] for c in row], [blocks[c][1] for c in row]])
    expected = list(segno.make_qr(LINK, error="m").matrix_iter())
    assert matrix[: len(expected)] == [list(row) for row in expected]
    assert "\x1b[" in stream.getvalue()


def test_no_color_skips_qr(monkeypatch: pytest.MonkeyPatch) -> None:
    stream = Terminal()
    monkeypatch.setattr(device_auth.sys, "stdout", stream)
    monkeypatch.setattr(device_auth.shutil, "get_terminal_size", lambda **kwargs: os.terminal_size((100, 40)))
    monkeypatch.setenv("TERM", "xterm")
    with click.Context(click.Command("test"), color=False):
        device_auth._show_qr(LINK)
    assert stream.getvalue() == ""


@pytest.mark.parametrize("columns,term", [(20, "xterm"), (100, "dumb")])
def test_unsuitable_terminal_skips_qr(monkeypatch: pytest.MonkeyPatch, columns: int, term: str) -> None:
    stream = Terminal()
    monkeypatch.setattr(device_auth.sys, "stdout", stream)
    monkeypatch.setattr(device_auth.shutil, "get_terminal_size", lambda **kwargs: os.terminal_size((columns, 40)))
    monkeypatch.setenv("TERM", term)
    device_auth._show_qr(LINK)
    assert stream.getvalue() == ""


def test_no_qr_preserves_link_and_code(
    flow: SimpleNamespace, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    qr = Mock()
    monkeypatch.setattr(device_auth, "_show_qr", qr)
    flow.responses = [httpx.Response(200, json=TOKEN)]
    device_auth.login(API, show_qr=False)
    qr.assert_not_called()
    assert LINK in capsys.readouterr().out


def test_command_saves_to_selected_host_and_reuses_login_completion(
    flow: SimpleNamespace, monkeypatch: pytest.MonkeyPatch, runner: CliRunner
) -> None:
    monkeypatch.setenv("AVR_HOST", API)
    store = Mock()
    monkeypatch.setattr(auth, "store_token", store)
    monkeypatch.setattr(auth_cmd, "_fetch_email", lambda *args: "alex@example.com")
    monkeypatch.setattr(auth_cmd, "_maybe_auto_pin_default_org", lambda *args: None)
    old_login = Mock(side_effect=AssertionError("Old login must not run"))
    monkeypatch.setattr(auth, "login", old_login)
    flow.responses = [httpx.Response(200, json=TOKEN)]
    result = runner.invoke(cli, ["auth", "device-login", "--no-qr"])
    assert result.exit_code == 0, result.output
    store.assert_called_once_with(ACCESS_TOKEN, host=API)
    assert "Logged in as alex@example.com" in result.output
    assert "Try: avr status" in result.output
    old_login.assert_not_called()


@pytest.mark.parametrize("failure", ["denied", "cancelled"])
def test_failure_preserves_existing_credentials(
    flow: SimpleNamespace, monkeypatch: pytest.MonkeyPatch, runner: CliRunner, failure: str
) -> None:
    monkeypatch.setenv("AVR_HOST", API)
    store = Mock()
    monkeypatch.setattr(auth, "store_token", store)
    if failure == "cancelled":
        monkeypatch.setattr(device_auth.time, "sleep", Mock(side_effect=KeyboardInterrupt()))
    else:
        flow.responses = [error("access_denied")]
    result = runner.invoke(cli, ["auth", "device-login"])
    assert result.exit_code != 0
    assert "Aborted" in result.output if failure == "cancelled" else "denied" in result.output
    store.assert_not_called()
