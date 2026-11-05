"""Device flow contracts: HTTP, polling, output, and credential preservation."""

from avrea_cli import auth
from avrea_cli import device_auth
from avrea_cli.commands import auth_cmd
from avrea_cli.config import CliConfig
from avrea_cli.main import cli
from click.testing import CliRunner
from io import StringIO
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock
from urllib.parse import parse_qs
import click
import httpx
import os
import pytest
import segno
import subprocess
import sys

API = "https://api.example.com"
VERIFY = "https://console.example.com/device"
LINK = VERIFY + "?user_code=BCDF-GHJK"
DEVICE_SECRET = "private-device-secret"
ACCESS_TOKEN = "avk_" + "a" * 43
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
        start_duration=0.0,
        poll_duration=0.0,
        discovery_error=None,
    )

    def sleep(seconds: float) -> None:
        assert seconds >= 0
        state.sleeps.append(seconds)
        state.now += seconds

    def handle(request: httpx.Request) -> httpx.Response:
        state.requests.append(request)
        assert "authorization" not in request.headers
        if request.url.path == "/.well-known/oauth-authorization-server":
            assert request.method == "GET"
            if state.discovery_error:
                raise state.discovery_error
            return httpx.Response(state.discovery_status, json=state.metadata)
        if str(request.url) == state.metadata["device_authorization_endpoint"]:
            assert request.method == "POST"
            state.now += state.start_duration
            assert parse_qs(request.content.decode()) == {"client_id": ["avr-cli"], "scope": ["avrea"]}
            return httpx.Response(state.start_status, json=state.start)
        assert str(request.url) == state.metadata["token_endpoint"]
        assert request.method == "POST"
        assert parse_qs(request.content.decode()) == {
            "client_id": ["avr-cli"],
            "grant_type": [device_auth.GRANT_TYPE],
            "device_code": [DEVICE_SECRET],
        }
        state.polls.append(state.now)
        state.now += state.poll_duration
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
    captured = capsys.readouterr()
    output = captured.out
    assert DEVICE_SECRET not in captured.err and ACCESS_TOKEN not in captured.err
    assert f"  {LINK}" in output.splitlines()
    assert "Code:" not in output
    assert "For manual code entry:" not in output
    assert "600 seconds" in output
    assert DEVICE_SECRET not in output and ACCESS_TOKEN not in output
    assert "█" not in output


def test_default_interval_and_missing_complete_url(flow: SimpleNamespace, capsys: pytest.CaptureFixture[str]) -> None:
    flow.start.pop("interval")
    flow.start.pop("verification_uri_complete")
    flow.responses = [httpx.Response(200, json=TOKEN)]
    assert device_auth.login(API) == ACCESS_TOKEN
    assert flow.polls == [5]
    output = capsys.readouterr().out
    assert VERIFY in output
    assert "Code: BCDF-GHJK" in output


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
    "failure",
    [
        httpx.ConnectTimeout("secret"),
        httpx.ReadTimeout("secret"),
        httpx.ConnectError("secret"),
        httpx.ReadError("secret"),
        httpx.WriteError("secret"),
        httpx.RemoteProtocolError("secret"),
        httpx.ProxyError("secret"),
    ],
)
def test_network_failures_back_off_until_success(flow: SimpleNamespace, failure: Exception) -> None:
    flow.responses = [failure, error("authorization_pending"), httpx.Response(200, json=TOKEN)]
    assert device_auth.login(API) == ACCESS_TOKEN
    assert flow.polls == [5, 15, 20]


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


def test_retry_after_http_date_is_ignored(flow: SimpleNamespace, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(device_auth.time, "time", lambda: 0)
    flow.responses = [
        httpx.Response(429, headers={"Retry-After": "Thu, 01 Jan 1970 00:01:00 GMT"}),
        httpx.Response(200, json=TOKEN),
    ]
    assert device_auth.login(API) == ACCESS_TOKEN
    assert flow.polls == [5, 10]


def test_expiry_bounds_sleep_and_prevents_more_requests(flow: SimpleNamespace) -> None:
    flow.start["expires_in"] = 12
    flow.responses = [httpx.Response(429, headers={"Retry-After": "60"})]
    with pytest.raises(click.ClickException, match="expired"):
        device_auth.login(API)
    assert flow.polls == [5]
    assert flow.sleeps == [5, 7]


def test_final_poll_can_finish_after_code_expiry(flow: SimpleNamespace) -> None:
    flow.start["expires_in"] = 7
    flow.poll_duration = 3
    flow.responses = [httpx.Response(200, json=TOKEN)]
    device_auth.login(API)
    assert flow.requests[-1].extensions["timeout"]["read"] == 15
    assert flow.now == 8


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
    "field,value,message",
    [
        ("verification_uri", "javascript:alert(1)", "unsafe device-login URL"),
        ("verification_uri_complete", "https://other.example/device", "same console origin"),
        ("verification_uri_complete", "https://console.example.com/\nspoof", "unsafe device-login URL"),
        ("user_code", "BCDF\x1bGHJK", "invalid device-login response"),
        ("interval", 0, "invalid device-login response"),
        ("interval", True, "invalid device-login response"),
        ("expires_in", -1, "invalid device-login response"),
        ("device_code", "", "invalid device-login response"),
    ],
)
def test_bad_start_response_is_not_displayed_or_polled(
    flow: SimpleNamespace, field: str, value: object, message: str, capsys: pytest.CaptureFixture[str]
) -> None:
    flow.start[field] = value
    if field == "verification_uri":
        flow.start.pop("verification_uri_complete")
    with pytest.raises(click.ClickException, match=message):
        device_auth.login(API)
    assert not flow.polls
    assert capsys.readouterr().out == ""


@pytest.mark.parametrize(
    "payload",
    [
        {"access_token": ACCESS_TOKEN},
        {**TOKEN, "token_type": "mac"},
        {**TOKEN, "scope": "other"},
        {**TOKEN, "access_token": ""},
    ],
)
def test_bad_token_responses_do_not_expose_secrets(
    flow: SimpleNamespace, payload: dict[str, object], capsys: pytest.CaptureFixture[str]
) -> None:
    flow.responses = [httpx.Response(200, json=payload)]
    with pytest.raises(click.ClickException) as caught:
        device_auth.login(API)
    captured = capsys.readouterr()
    output = captured.out + captured.err + str(caught.value)
    assert ACCESS_TOKEN not in output
    assert DEVICE_SECRET not in output


class Terminal(StringIO):
    @property
    def encoding(self) -> str:
        return "utf-8"

    def isatty(self) -> bool:
        return True


def test_qr_contains_complete_link_with_quiet_zone(monkeypatch: pytest.MonkeyPatch) -> None:
    stream = Terminal()
    monkeypatch.setattr(device_auth.sys, "stdout", stream)
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
    assert "\x1b[37m\x1b[40m" in stream.getvalue()


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
    flow: SimpleNamespace, monkeypatch: pytest.MonkeyPatch, runner: CliRunner, failure: str, tmp_path: Path
) -> None:
    monkeypatch.setenv("AVR_HOST", API)
    monkeypatch.setattr(auth, "HOSTS_FILE", tmp_path / "hosts.json")
    auth.store_token("existing-token", host=API)
    if failure == "cancelled":
        monkeypatch.setattr(device_auth.time, "sleep", Mock(side_effect=KeyboardInterrupt()))
    else:
        flow.responses = [error("access_denied")]
    result = runner.invoke(cli, ["auth", "device-login"])
    assert result.exit_code != 0
    assert "Aborted" in result.output if failure == "cancelled" else "denied" in result.output
    assert auth._read()["hosts"][API]["token"] == "existing-token"


@pytest.mark.parametrize("interval", [1, 7, 13])
def test_uses_discovered_paths_and_interval(flow: SimpleNamespace, interval: int) -> None:
    flow.metadata["device_authorization_endpoint"] = API + "/custom/start"
    flow.metadata["token_endpoint"] = API + "/custom/token"
    flow.start["interval"] = interval
    flow.responses = [error("authorization_pending"), httpx.Response(200, json=TOKEN)]
    assert device_auth.login(API) == ACCESS_TOKEN
    assert flow.polls == [interval, interval * 2]


@pytest.mark.parametrize("field", ["device_authorization_endpoint", "token_endpoint"])
def test_different_endpoint_port_is_rejected(flow: SimpleNamespace, field: str) -> None:
    flow.metadata[field] = API + ":8443/custom"
    with pytest.raises(click.ClickException, match="configured API origin"):
        device_auth.login(API)
    assert len(flow.requests) == 1


def test_custom_api_port_is_supported(flow: SimpleNamespace) -> None:
    api = API + ":8443"
    flow.metadata = {"issuer": api, "device_authorization_endpoint": api + "/start", "token_endpoint": api + "/token"}
    flow.responses = [httpx.Response(200, json=TOKEN)]
    assert device_auth.login(api) == ACCESS_TOKEN
    assert all(request.url.port == 8443 for request in flow.requests)


@pytest.mark.parametrize("field", ["interval", "expires_in"])
def test_oversized_timing_is_rejected_before_display(
    flow: SimpleNamespace, capsys: pytest.CaptureFixture[str], field: str
) -> None:
    flow.start[field] = 10**400
    with pytest.raises(click.ClickException, match="invalid device-login response"):
        device_auth.login(API)
    assert not flow.polls
    assert capsys.readouterr().out == ""


@pytest.mark.parametrize(
    "token", ["avk_fake\nsecret", "avk_fake\rsecret", "avk_fake\tsecret", "avk_fakeésecret", "avk_" + "a" * 44]
)
def test_invalid_api_key_is_not_returned(flow: SimpleNamespace, capsys: pytest.CaptureFixture[str], token: str) -> None:
    flow.responses = [httpx.Response(200, json={**TOKEN, "access_token": token})]
    with pytest.raises(click.ClickException, match="invalid device-login response") as caught:
        device_auth.login(API)
    captured = capsys.readouterr()
    assert token not in captured.out + captured.err + str(caught.value)


@pytest.mark.parametrize("field", ["verification_uri", "verification_uri_complete"])
@pytest.mark.parametrize("character", ["\x9b", "\u202e"])
def test_normalized_link_is_printed_and_encoded(
    flow: SimpleNamespace,
    monkeypatch: pytest.MonkeyPatch,
    capsys: pytest.CaptureFixture[str],
    field: str,
    character: str,
) -> None:
    raw = VERIFY + "/" + character + "link"
    flow.start[field] = raw
    if field == "verification_uri":
        flow.start.pop("verification_uri_complete")
    qr = Mock()
    monkeypatch.setattr(device_auth, "_show_qr", qr)
    flow.responses = [httpx.Response(200, json=TOKEN)]
    device_auth.login(API)
    output = capsys.readouterr().out
    assert character not in output
    assert str(httpx.URL(raw)) in output
    qr.assert_called_once_with(str(httpx.URL(raw)))


@pytest.mark.parametrize("field", ["verification_uri", "verification_uri_complete"])
@pytest.mark.parametrize("value", ["http://console.example.com/device", "https://console.example.com/\nspoof"])
def test_unsafe_links_produce_no_output(
    flow: SimpleNamespace, capsys: pytest.CaptureFixture[str], field: str, value: str
) -> None:
    flow.start[field] = value
    if field == "verification_uri":
        flow.start.pop("verification_uri_complete")
    with pytest.raises(click.ClickException, match="unsafe device-login URL"):
        device_auth.login(API)
    assert capsys.readouterr().out == ""
    assert not flow.polls


def test_loopback_http_login(flow: SimpleNamespace) -> None:
    api = "http://localhost:8080"
    flow.metadata = {"issuer": api, "device_authorization_endpoint": api + "/start", "token_endpoint": api + "/token"}
    flow.start["verification_uri"] = "http://127.0.0.1:3000/device"
    flow.start.pop("verification_uri_complete")
    flow.responses = [httpx.Response(200, json=TOKEN)]
    assert device_auth.login(api) == ACCESS_TOKEN


@pytest.mark.parametrize("phase", ["discovery", "start", "poll"])
@pytest.mark.parametrize("status", [404, 405, 501])
def test_unsupported_endpoint_uses_consistent_message(flow: SimpleNamespace, phase: str, status: int) -> None:
    if phase == "discovery":
        flow.discovery_status = status
    elif phase == "start":
        flow.start_status = status
    else:
        flow.responses = [httpx.Response(status)]
    with pytest.raises(click.ClickException, match="Use avr auth login instead"):
        device_auth.login(API)


@pytest.mark.parametrize(
    "status,code,message",
    [
        (429, "", "Too many"),
        (503, "", "HTTP 503"),
        (400, "invalid_client", "does not accept"),
        (400, "invalid_scope", "requested Avrea access"),
        (400, "invalid_request", "rejected"),
    ],
)
def test_start_errors(flow: SimpleNamespace, status: int, code: str, message: str) -> None:
    flow.start_status = status
    flow.start = {"error": code}
    with pytest.raises(click.ClickException, match=message):
        device_auth.login(API)
    assert not flow.polls


@pytest.mark.parametrize("status,message", [(429, "Too many"), (500, "HTTP 500")])
def test_discovery_http_errors(flow: SimpleNamespace, status: int, message: str) -> None:
    flow.discovery_status = status
    with pytest.raises(click.ClickException, match=message):
        device_auth.login(API)
    assert len(flow.requests) == 1


@pytest.mark.parametrize(
    "failure,message", [(httpx.ConnectError("private"), "Could not reach"), (httpx.ReadTimeout("private"), "Timed out")]
)
def test_discovery_transport_errors(flow: SimpleNamespace, failure: Exception, message: str) -> None:
    flow.discovery_error = failure
    with pytest.raises(click.ClickException, match=message) as caught:
        device_auth.login(API)
    assert "private" not in str(caught.value)
    assert len(flow.requests) == 1


def test_start_response_time_counts_towards_expiry(flow: SimpleNamespace) -> None:
    flow.start_duration = 601
    with pytest.raises(click.ClickException, match="expired"):
        device_auth.login(API)
    assert not flow.sleeps and not flow.polls


def test_poll_uses_normal_timeout(flow: SimpleNamespace) -> None:
    flow.responses = [httpx.Response(200, json=TOKEN)]
    device_auth.login(API)
    assert flow.requests[-1].extensions["timeout"]["read"] == 15


def test_transient_failure_warns_without_secrets(flow: SimpleNamespace, capsys: pytest.CaptureFixture[str]) -> None:
    flow.start["expires_in"] = 12
    flow.responses = [httpx.ReadError(DEVICE_SECRET + ACCESS_TOKEN)]
    with pytest.raises(click.ClickException, match="ReadError") as caught:
        device_auth.login(API)
    captured = capsys.readouterr()
    assert "retrying" in captured.err
    assert DEVICE_SECRET not in captured.out + captured.err + str(caught.value)
    assert ACCESS_TOKEN not in captured.out + captured.err + str(caught.value)


def test_recovery_preserves_slow_down_interval(flow: SimpleNamespace) -> None:
    flow.responses = [
        error("slow_down"),
        httpx.ReadTimeout("secret"),
        error("authorization_pending"),
        httpx.Response(200, json=TOKEN),
    ]
    device_auth.login(API)
    assert flow.polls == [5, 15, 35, 45]


def test_envelopeless_server_error_is_retried(flow: SimpleNamespace) -> None:
    flow.responses = [httpx.Response(502), httpx.Response(200, json=TOKEN)]
    assert device_auth.login(API) == ACCESS_TOKEN


def test_secret_models_hide_credentials() -> None:
    assert DEVICE_SECRET not in repr(device_auth.DeviceRequest.model_validate(START))
    assert ACCESS_TOKEN not in repr(device_auth.DeviceToken.model_validate(TOKEN))


@pytest.mark.parametrize("no_qr", [False, True])
def test_command_qr_flag(
    flow: SimpleNamespace, monkeypatch: pytest.MonkeyPatch, runner: CliRunner, no_qr: bool
) -> None:
    monkeypatch.setenv("AVR_HOST", API)
    qr = Mock()
    monkeypatch.setattr(device_auth, "_show_qr", qr)
    monkeypatch.setattr(auth_cmd, "_complete_login", Mock())
    flow.responses = [httpx.Response(200, json=TOKEN)]
    result = runner.invoke(cli, ["auth", "device-login"] + (["--no-qr"] if no_qr else []))
    assert result.exit_code == 0, result.output
    if no_qr:
        qr.assert_not_called()
    else:
        qr.assert_called_once_with(LINK)
    assert DEVICE_SECRET not in result.output and ACCESS_TOKEN not in result.output


def test_key_is_saved_before_optional_requests(
    flow: SimpleNamespace, monkeypatch: pytest.MonkeyPatch, runner: CliRunner, tmp_path: Path
) -> None:
    monkeypatch.setenv("AVR_HOST", API)
    monkeypatch.setattr(auth, "HOSTS_FILE", tmp_path / "hosts.json")

    def interrupt(*args):
        assert auth._read()["hosts"][API]["token"] == ACCESS_TOKEN
        raise KeyboardInterrupt

    monkeypatch.setattr(auth_cmd, "_fetch_email", interrupt)
    flow.responses = [httpx.Response(200, json=TOKEN)]
    result = runner.invoke(cli, ["auth", "device-login", "--no-qr"])
    assert result.exit_code != 0
    assert auth._read()["hosts"][API]["token"] == ACCESS_TOKEN


def test_save_failure_has_recovery_message(
    flow: SimpleNamespace, monkeypatch: pytest.MonkeyPatch, runner: CliRunner, tmp_path: Path
) -> None:
    monkeypatch.setenv("AVR_HOST", API)
    path = tmp_path / "hosts.json"
    monkeypatch.setattr(auth, "HOSTS_FILE", path)
    monkeypatch.setattr(auth, "store_token", Mock(side_effect=OSError("storage-error-secret")))
    fetch = Mock()
    monkeypatch.setattr(auth_cmd, "_fetch_email", fetch)
    flow.responses = [httpx.Response(200, json=TOKEN)]
    result = runner.invoke(cli, ["auth", "device-login", "--no-qr"])
    assert result.exit_code == 1
    assert str(path) in result.output
    assert "Fix" in result.output and "avr auth device-login" in result.output
    assert "Traceback" not in result.output and "storage-error-secret" not in result.output
    fetch.assert_not_called()


@pytest.mark.parametrize("api,visible", [(CliConfig.DEFAULT_API_URL, False), (API, True)])
def test_api_host_only_shown_for_non_default(
    flow: SimpleNamespace, capsys: pytest.CaptureFixture[str], api: str, visible: bool
) -> None:
    flow.metadata = {"issuer": api, "device_authorization_endpoint": api + "/start", "token_endpoint": api + "/token"}
    flow.responses = [httpx.Response(200, json=TOKEN)]
    device_auth.login(api)
    assert ("API: " + api in capsys.readouterr().out) is visible


def test_invalid_local_host_is_not_blamed_on_server() -> None:
    with pytest.raises(click.ClickException, match="Invalid API host.*configuration"):
        device_auth.login(API + "#fragment")


def test_closed_stdout_skips_qr(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(device_auth.sys, "stdout", None)
    device_auth._show_qr(LINK)


def test_latin1_terminal_skips_qr(monkeypatch: pytest.MonkeyPatch) -> None:
    class LatinTerminal(Terminal):
        @property
        def encoding(self) -> str:
            return "latin-1"

    stream = LatinTerminal()
    monkeypatch.setattr(device_auth.sys, "stdout", stream)
    device_auth._show_qr(LINK)
    assert stream.getvalue() == ""


def test_oversized_link_skips_qr(monkeypatch: pytest.MonkeyPatch) -> None:
    stream = Terminal()
    monkeypatch.setattr(device_auth.sys, "stdout", stream)
    monkeypatch.setenv("TERM", "xterm")
    device_auth._show_qr(VERIFY + "?code=" + "a" * 10000)
    assert stream.getvalue() == ""


def test_other_auth_commands_do_not_import_qr_support() -> None:
    result = subprocess.run(
        [sys.executable, "-c", "import sys; import avrea_cli.commands.auth_cmd; print('segno' in sys.modules)"],
        capture_output=True,
        text=True,
        check=True,
        timeout=15,
    )
    assert result.stdout.strip() == "False"
