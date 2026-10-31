"""Native CLI dispatch follows its receipt and preserves scalar input types."""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import CliConfig
from avrea_cli.main import cli
from avrea_cli.native_dispatch import NativeDispatch
from avrea_cli.native_dispatch import follow_native_dispatch
from click.testing import CliRunner
from decimal import Decimal
from typing import Any
from unittest.mock import MagicMock
import click
import httpx
import json
import pytest
import shlex

WORKFLOW = "awf-019d9533fc2f7b59948243a10b312d2e"
DISPATCH = "wap-019d9533fc2f7b59948243a10b312d2e"
RUN = "awr-019d9533fc2f7b59948243a10b312d2e"
KEY = "019d9533fc2f7b59948243a10b312d2e"
ORIGIN = {"ref": "refs/heads/main", "commit_sha": "a" * 40}
BASE = "/orgs/org-default/repos/rep-019d9533fc2f7b59948243a10b312d2e"
ARGS = ["workflow", "run", WORKFLOW, "--repo", BASE.rsplit("/", 1)[-1], "--ref", "main"]


@pytest.fixture
def native_api(monkeypatch: pytest.MonkeyPatch) -> tuple[MagicMock, MagicMock, MagicMock]:
    def get(path: str, **kwargs: Any) -> dict:
        if path.endswith("/workflows"):
            return {
                "data": [
                    {
                        "workflow_id": WORKFLOW,
                        "ci_provider": "avrea_ci",
                        "name": "Build",
                        "path": ".avrea/workflows/build.yml",
                        "platform_id": None,
                    }
                ]
            }
        if path.endswith("/dispatch-metadata"):
            return {
                "data": {
                    "ref": "refs/heads/trunk",
                    "can_dispatch": True,
                    "dispatchable": True,
                    "inputs": [
                        {"name": "enabled", "type": "boolean"},
                        {"name": "count", "type": "number"},
                        {"name": "label", "type": "string"},
                    ],
                }
            }
        if path == f"{BASE}/workflow-dispatches/{DISPATCH}":
            return {"data": {**ORIGIN, "dispatch_id": DISPATCH, "state": "admitted", "reason": None, "run_ids": [RUN]}}
        if path == "/orgs/org-default":
            return {"data": {"slug": "my-org"}}
        raise AssertionError(f"Unexpected GET {path}")

    get_mock = MagicMock(side_effect=get)
    post_mock = MagicMock(
        return_value={
            "data": {
                "dispatch_id": DISPATCH,
                "state": "pending",
                "reason": None,
                "ref": "refs/heads/main",
                "commit_sha": "a" * 40,
                "run_ids": [],
            }
        }
    )
    watch = MagicMock()
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", get_mock)
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", post_mock)
    monkeypatch.setattr("avrea_cli.commands.workflow.watch_run_loop", watch)
    monkeypatch.setattr("avrea_cli.commands.workflow.get_org_slug", lambda *_: "my-org")
    return get_mock, post_mock, watch


def test_json_dispatch_preserves_types_and_exact_decimals(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock]
) -> None:
    get, post, watch = native_api
    result = runner.invoke(
        cli,
        [*ARGS, "--json", "--no-watch", "--idempotency-key", KEY],
        input='{"enabled":false,"count":0,"label":"false","precise":1.00000000000000001}',
    )
    assert result.exit_code == 0, result.output
    assert post.call_args.args == (f"{BASE}/workflow-dispatches",)
    body = json.loads(post.call_args.kwargs["content"], parse_float=Decimal)
    assert body == {
        "workflow_id": WORKFLOW,
        "ref": "main",
        "inputs": {"enabled": False, "count": 0, "label": "false", "precise": Decimal("1.00000000000000001")},
    }
    assert post.call_args.kwargs["idempotency_key"] == KEY
    assert DISPATCH in result.output
    watch.assert_not_called()
    assert all("workflow-runs" not in call.args[0] for call in get.call_args_list)


def test_raw_fields_follow_declared_types_and_watch_exact_run(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock]
) -> None:
    get, post, watch = native_api
    result = runner.invoke(cli, [*ARGS, "-f", "enabled=false", "-f", "count=0", "-f", "label=false"])
    assert result.exit_code == 0, result.output
    assert json.loads(post.call_args.kwargs["content"])["inputs"] == {"enabled": False, "count": 0, "label": "false"}
    assert watch.call_args.args[2] == RUN
    assert RUN in result.output
    assert all("workflow-runs" not in call.args[0] for call in get.call_args_list)


def test_lost_dispatch_response_exposes_reusable_key(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock]
) -> None:
    _, post, _ = native_api
    post.side_effect = httpx.ReadTimeout("lost response")
    result = runner.invoke(cli, [*ARGS, "--no-watch", "--idempotency-key", KEY])
    assert result.exit_code != 0
    assert KEY in result.output
    assert "--idempotency-key" in result.output
    assert post.call_count == 2
    assert post.call_args_list[0] == post.call_args_list[1]


def test_lost_response_recovers_once_with_the_same_request(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock]
) -> None:
    _, post, _ = native_api
    post.side_effect = [httpx.ReadTimeout("lost response"), post.return_value]
    result = runner.invoke(cli, [*ARGS, "--no-watch", "--idempotency-key", KEY])
    assert result.exit_code == 0, result.output
    assert post.call_count == 2
    assert post.call_args_list[0] == post.call_args_list[1]
    assert post.call_args.kwargs["timeout"] == 60


def test_no_watch_can_resume_the_same_pending_dispatch_and_report_rejection(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock]
) -> None:
    get, post, watch = native_api
    result = runner.invoke(cli, [*ARGS, "--no-watch", "-f", "count=1", "--idempotency-key", KEY])
    assert result.exit_code == 0, result.output
    assert "accepted and is awaiting admission" in result.output
    command = result.output.split("Resume following this dispatch:\n")[1].strip()
    words = shlex.split(command)
    assert all("/workflow-dispatches/" not in call.args[0] for call in get.call_args_list)
    watch.assert_not_called()
    post.return_value["data"].update(state="rejected", reason="writer_access_revoked")
    resumed = runner.invoke(cli, words[7:], input=words[2], env={"AVR_HOST": words[5].split("=", 1)[1]})
    assert resumed.exit_code != 0
    assert "writer_access_revoked" in resumed.output
    assert post.call_args_list[0] == post.call_args_list[1]


@pytest.mark.parametrize("run_ids", [[RUN], []])
def test_no_watch_admitted_receipt_requires_one_run(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock], run_ids: list[str]
) -> None:
    get, post, watch = native_api
    post.return_value["data"].update(state="admitted", run_ids=run_ids)
    result = runner.invoke(cli, [*ARGS, "--no-watch"])
    if run_ids:
        assert result.exit_code == 0, result.output
        assert RUN in result.output
    else:
        assert result.exit_code != 0
        assert "did not identify exactly one" in result.output
    assert "awaiting admission" not in result.output
    assert all("/workflow-dispatches/" not in call.args[0] for call in get.call_args_list)
    watch.assert_not_called()


@pytest.mark.parametrize("status", [429, 503, 403])
def test_post_http_errors_retry_hint_and_no_automatic_repost(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock], status: int
) -> None:
    _, post, _ = native_api
    request = httpx.Request("POST", "https://api.example/dispatch")
    post.side_effect = httpx.HTTPStatusError(
        "refused", request=request, response=httpx.Response(status, request=request, json={"detail": "refused"})
    )
    result = runner.invoke(cli, [*ARGS, "--no-watch", "--idempotency-key", KEY])
    assert result.exit_code != 0
    assert ("Retry the exact request:" in result.output) is (status in {429, 503})
    assert post.call_count == 1


def test_retry_command_freezes_default_ref_and_typed_inputs(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock]
) -> None:
    get, post, _ = native_api
    receipt = post.return_value
    post.side_effect = [httpx.ReadTimeout("lost response"), httpx.ReadTimeout("lost replay"), receipt]
    result = runner.invoke(cli, [*ARGS[:-2], "--no-watch", "-f", "count=1"])
    assert result.exit_code != 0
    command = result.output.split("Retry the exact request:\n")[1].strip()
    words = shlex.split(command)
    assert words[:2] == ["printf", "%s"]
    assert words[3:5] == ["|", "env"]
    assert words[5].startswith("AVR_HOST=")
    assert words[6] == "avr"
    original_get = get.side_effect

    def retry_get(path: str, **kwargs: Any) -> dict:
        if path.endswith("/dispatch-metadata"):
            pytest.fail("Recovery must not rediscover metadata")
        return original_get(path, **kwargs)

    get.side_effect = retry_get
    retry = runner.invoke(cli, [*words[7:], "--no-watch"], input=words[2], env={"AVR_HOST": words[5].split("=", 1)[1]})
    assert retry.exit_code == 0, retry.output
    first, automatic_retry, second = post.call_args_list
    assert first == automatic_retry
    assert first.kwargs["content"] == second.kwargs["content"]
    assert first.kwargs["idempotency_key"] == second.kwargs["idempotency_key"]


def test_native_default_ref_comes_from_metadata(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock]
) -> None:
    _, post, _ = native_api
    post.return_value["data"].update(ref="refs/heads/trunk")
    result = runner.invoke(cli, [*ARGS[:-2], "--no-watch"])
    assert result.exit_code == 0, result.output
    assert json.loads(post.call_args.kwargs["content"])["ref"] == "refs/heads/trunk"
    assert "refs/heads/trunk" in result.output
    assert ORIGIN["commit_sha"] in result.output


@pytest.mark.parametrize(
    ("metadata", "message"),
    [
        ({"ref": "", "dispatchable": False, "can_dispatch": False, "inputs": []}, "not enabled"),
        ({"ref": "main", "dispatchable": True, "can_dispatch": False, "inputs": []}, "not permitted"),
        (
            {"ref": "main", "dispatchable": True, "can_dispatch": True, "inputs": [{}]},
            "invalid native workflow metadata",
        ),
        (None, "invalid native workflow metadata"),
    ],
)
def test_metadata_refusals_precede_ref_advice_and_submission(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock], metadata: dict | None, message: str
) -> None:
    get, post, _ = native_api
    original_get = get.side_effect

    def metadata_get(path: str, **kwargs: Any) -> dict:
        return {"data": metadata} if path.endswith("/dispatch-metadata") else original_get(path, **kwargs)

    get.side_effect = metadata_get
    result = runner.invoke(cli, [*ARGS[:-2], "--no-watch"])
    assert result.exit_code != 0
    assert message in result.output
    assert "Pass --ref" not in result.output
    post.assert_not_called()


@pytest.mark.parametrize("failure, message", [("network", "native workflow inputs"), ("server", "HTTP 503")])
def test_metadata_read_failure_does_not_submit(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock], failure: str, message: str
) -> None:
    get, post, _ = native_api
    original_get = get.side_effect

    def metadata_get(path: str, **kwargs: Any) -> dict:
        if path.endswith("/dispatch-metadata"):
            request = httpx.Request("GET", "https://api.example/metadata")
            if failure == "network":
                raise httpx.ReadTimeout("unavailable")
            if failure == "server":
                raise httpx.HTTPStatusError(
                    "unavailable", request=request, response=httpx.Response(503, request=request)
                )
            raise AssertionError(f"Unknown metadata failure: {failure}")
        return original_get(path, **kwargs)

    get.side_effect = metadata_get
    result = runner.invoke(cli, [*ARGS[:-2], "--no-watch"])
    assert result.exit_code != 0
    assert message in result.output
    post.assert_not_called()


def test_github_dispatch_refuses_native_idempotency_key(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock]
) -> None:
    _, post, _ = native_api
    result = runner.invoke(cli, ["workflow", "run", "build.yml", *ARGS[3:], "--idempotency-key", KEY])
    assert result.exit_code != 0
    assert "supported only for Avrea CI" in result.output
    post.assert_not_called()


@pytest.mark.parametrize("status", [404, 422])
def test_bare_filename_failure_explains_native_workflow_selection(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock], status: int
) -> None:
    _, post, _ = native_api
    request = httpx.Request("POST", "https://api.example/github-dispatch")
    post.side_effect = httpx.HTTPStatusError(
        "refused",
        request=request,
        response=httpx.Response(status, request=request, json={"detail": "missing workflow"}),
    )
    result = runner.invoke(cli, ["workflow", "run", "build.yml", *ARGS[3:], "--no-watch"])
    assert result.exit_code != 0
    assert "missing workflow" in result.output
    assert ".avrea/workflows/" in result.output
    assert "awf-" in result.output
    assert post.call_args.args == (f"{BASE}/dispatch-workflow",)


@pytest.mark.parametrize("value", ['{"x":null}', '{"x":[]}', '{"x":{}}', '{"x":NaN}', '{"x":Infinity}'])
def test_native_json_rejects_non_scalar_or_nonfinite_inputs(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock], value: str
) -> None:
    _, post, _ = native_api
    result = runner.invoke(cli, [*ARGS, "--json", "--no-watch"], input=value)
    assert result.exit_code != 0
    assert "must be" in result.output
    post.assert_not_called()


@pytest.mark.parametrize("field", ["enabled=yes", "count=no", "count=NaN"])
def test_native_fields_refuse_invalid_typed_values(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock], field: str
) -> None:
    _, post, _ = native_api
    result = runner.invoke(cli, [*ARGS, "-f", field, "--no-watch"])
    assert result.exit_code != 0
    post.assert_not_called()


@pytest.mark.parametrize("watch", ["--watch", "--no-watch"])
def test_rejected_receipt_reports_reason(
    runner: CliRunner, native_api: tuple[MagicMock, MagicMock, MagicMock], watch: str
) -> None:
    _, post, loop = native_api
    post.return_value["data"].update(state="rejected", reason="membership_revoked")
    result = runner.invoke(cli, [*ARGS, watch])
    assert result.exit_code != 0
    assert "membership_revoked" in result.output
    loop.assert_not_called()


def test_post_sends_idempotency_header_without_changing_auth(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AVR_TOKEN", "test-token")

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.headers["Idempotency-Key"] == KEY
        assert request.headers["Authorization"] == "Bearer test-token"
        assert request.headers["Content-Type"] == "application/json"
        assert request.content == b'{"enabled":false}'
        return httpx.Response(202, json={"data": {"ok": True}})

    client = ApiClient(CliConfig(), http_client=httpx.Client(transport=httpx.MockTransport(handler)))
    assert client.public_post("/dispatch", content='{"enabled":false}', idempotency_key=KEY) == {"data": {"ok": True}}


@pytest.mark.parametrize("failure", ["network", "server", "rate_limit"])
def test_receipt_follow_retries_transient_reads(monkeypatch: pytest.MonkeyPatch, failure: str) -> None:
    client = MagicMock(spec=ApiClient)
    client.timeout = 30
    request = httpx.Request("GET", "https://api.example/receipt")
    error = (
        httpx.ConnectError("unavailable")
        if failure == "network"
        else httpx.HTTPStatusError(
            "retry", request=request, response=httpx.Response(503 if failure == "server" else 429, request=request)
        )
    )
    client.public_get.side_effect = [
        error,
        {"data": {**ORIGIN, "dispatch_id": DISPATCH, "state": "admitted", "run_ids": [RUN]}},
    ]
    monkeypatch.setattr("avrea_cli.native_dispatch.time.sleep", lambda _: None)
    assert (
        follow_native_dispatch(
            client, BASE, NativeDispatch(**ORIGIN, dispatch_id=DISPATCH, state="pending", run_ids=[])
        )
        == RUN
    )
    assert client.public_get.call_count == 2


def test_receipt_timeout_does_not_select_a_different_run(monkeypatch: pytest.MonkeyPatch) -> None:
    client = MagicMock(spec=ApiClient)
    monkeypatch.setattr("avrea_cli.native_dispatch.time.monotonic", MagicMock(side_effect=[0, 1]))
    with pytest.raises(click.ClickException, match="accepted and is awaiting admission"):
        follow_native_dispatch(
            client, BASE, NativeDispatch(**ORIGIN, dispatch_id=DISPATCH, state="pending", run_ids=[]), timeout=1
        )
    client.public_get.assert_not_called()


@pytest.mark.parametrize(
    "data",
    [
        {"dispatch_id": "wap-" + "a" * 32, "state": "admitted", "run_ids": [RUN]},
        {"dispatch_id": DISPATCH, "state": "admitted", "run_ids": []},
        {"dispatch_id": DISPATCH, "state": "future", "run_ids": []},
    ],
)
def test_receipt_follow_refuses_changed_or_invalid_receipts(data: dict) -> None:
    client = MagicMock(spec=ApiClient)
    client.timeout = 30
    client.public_get.return_value = {"data": {**ORIGIN, **data}}
    with pytest.raises(click.ClickException):
        follow_native_dispatch(
            client, BASE, NativeDispatch(**ORIGIN, dispatch_id=DISPATCH, state="pending", run_ids=[])
        )
