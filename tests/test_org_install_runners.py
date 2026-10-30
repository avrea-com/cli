from avrea_cli.main import cli
from click.testing import CliRunner
from typing import Any
import httpx
import pytest

RUNNER = {
    "machine_type": "linux-2-vcpu",
    "tag": "avrea-linux-2-vcpu",
    "status": "not_configured",
    "configured": False,
    "runner_id": None,
    "token_expires_at": None,
    "verified_at": None,
    "description": "Avrea linux-2-vcpu",
    "run_untagged": False,
    "paused": False,
}
SETUP = {
    "group_id": 42,
    "group_full_path": "acme",
    "protected_branches": "Customer choice",
    "fallback_image": "Set image in your pipeline",
    "job_example": "job:\n  tags: [avrea-linux-2-vcpu]",
    "documentation_url": "https://docs.avrea.com/gitlab-runners/",
}


def test_runner_list_and_setup(runner: CliRunner, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get", lambda self, path: {"data": [RUNNER], "setup": SETUP}
    )
    result = runner.invoke(cli, ["org", "install", "runner", "list", "--installation-id", "ins-test"])
    assert result.exit_code == 0, result.output
    assert "avrea-linux-2-vcpu" in result.output
    assert "not_configured" in result.output
    assert "run_untagged=False" in result.output
    assert "Customer choice" in result.output


def test_runner_list_ignores_retired_execution_flag(runner: CliRunner, monkeypatch: pytest.MonkeyPatch) -> None:
    """Runner rows may carry fields this CLI does not know; they are neither shown nor fatal."""
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path: {"data": [{**RUNNER, "execution_available": False}], "setup": SETUP},
    )
    result = runner.invoke(cli, ["org", "install", "runner", "list", "--installation-id", "ins-test"])
    assert result.exit_code == 0, result.output
    assert "avrea-linux-2-vcpu" in result.output
    assert "execution_available" not in result.output


@pytest.mark.parametrize("stdin", [True, False])
def test_runner_set_keeps_token_out_of_output(runner: CliRunner, monkeypatch: pytest.MonkeyPatch, stdin: bool) -> None:
    captured: dict[str, Any] = {}

    def put(self: Any, path: str, json: dict[str, Any], timeout: float) -> dict[str, Any]:
        captured.update(path=path, body=json, timeout=timeout)
        return {**RUNNER, "configured": True, "status": "configured"}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", put)
    args = ["org", "install", "runner", "set", "linux-2-vcpu", "--installation-id", "ins-test"]
    if stdin:
        args.append("--runner-token-stdin")
    result = runner.invoke(cli, args, input="glrt-private-token\n")
    assert result.exit_code == 0, result.output
    assert captured == {
        "path": "/orgs/org-default/gitlab-installations/ins-test/runners/linux-2-vcpu",
        "body": {"runner_token": "glrt-private-token"},
        "timeout": 70.0,
    }
    assert "glrt-private-token" not in result.output


def test_runner_delete_requires_confirmation(runner: CliRunner, monkeypatch: pytest.MonkeyPatch) -> None:
    calls: list[str] = []
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", lambda self, path: calls.append(path))
    args = ["org", "install", "runner", "delete", "linux-2-vcpu", "--installation-id", "ins-test"]
    result = runner.invoke(cli, args, input="n\n")
    assert result.exit_code != 0
    assert not calls
    result = runner.invoke(cli, args + ["--yes"])
    assert result.exit_code == 0, result.output
    assert calls == ["/orgs/org-default/gitlab-installations/ins-test/runners/linux-2-vcpu"]


def test_create_helper_sends_personal_token_only_to_gitlab(runner: CliRunner, monkeypatch: pytest.MonkeyPatch) -> None:
    avrea_calls: list[dict[str, Any]] = []
    gitlab_calls: list[dict[str, Any]] = []
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get", lambda self, path: {"data": [RUNNER], "setup": SETUP}
    )

    def post(self: Any, url: str, *, headers: dict[str, str], json: dict[str, Any], timeout: float) -> httpx.Response:
        gitlab_calls.append({"url": url, "headers": headers, "body": json})
        return httpx.Response(201, json={"id": 99, "token": "glrt-created"})

    def put(self: Any, path: str, *, json: dict[str, Any], timeout: float) -> dict[str, Any]:
        avrea_calls.append(json)
        return {**RUNNER, "configured": True, "status": "configured", "runner_id": 99}

    monkeypatch.setattr("httpx.Client.post", post)
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", put)
    result = runner.invoke(
        cli,
        [
            "org",
            "install",
            "runner",
            "create",
            "--installation-id",
            "ins-test",
            "--machine-type",
            "linux-2-vcpu",
            "--machine-type",
            "linux-2-vcpu",
            "--protected",
            "--personal-access-token-stdin",
        ],
        input="glpat-personal\n",
    )
    assert result.exit_code == 0, result.output
    assert avrea_calls == [{"runner_token": "glrt-created"}]
    assert gitlab_calls == [
        {
            "url": "https://gitlab.com/api/v4/user/runners",
            "headers": {"PRIVATE-TOKEN": "glpat-personal"},
            "body": {
                "runner_type": "group_type",
                "group_id": 42,
                "tag_list": "avrea-linux-2-vcpu",
                "description": RUNNER["description"],
                "run_untagged": False,
                "paused": False,
                "access_level": "ref_protected",
            },
        }
    ]
    assert "glpat-personal" not in result.output
    assert "glrt-created" not in result.output


@pytest.mark.parametrize("failure", ["gitlab", "avrea"])
def test_create_failure_does_not_echo_credentials(
    runner: CliRunner, monkeypatch: pytest.MonkeyPatch, failure: str
) -> None:
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get", lambda self, path: {"data": [RUNNER], "setup": SETUP}
    )
    monkeypatch.setattr(
        "httpx.Client.post",
        lambda *args, **kwargs: httpx.Response(
            400 if failure == "gitlab" else 201, json={"id": 99, "token": "glrt-created", "error": "glpat-personal"}
        ),
    )

    def put(*args: Any, **kwargs: Any) -> None:
        raise httpx.ReadTimeout("glrt-created glpat-personal")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", put)
    result = runner.invoke(
        cli,
        [
            "org",
            "install",
            "runner",
            "create",
            "--installation-id",
            "ins-test",
            "--machine-type",
            "linux-2-vcpu",
            "--personal-access-token-stdin",
        ],
        input="glpat-personal\n",
    )
    assert result.exit_code != 0
    assert "glpat-personal" not in result.output
    assert "glrt-created" not in result.output
    if failure == "avrea":
        assert "runner 99" in result.output


def test_install_list_loads_counts_from_gitlab_summary_only(runner: CliRunner, monkeypatch: pytest.MonkeyPatch) -> None:
    paths: list[str] = []

    def get(self: Any, path: str) -> dict[str, Any]:
        paths.append(path)
        if path == "/users/me/installations":
            return {"data": [{"installation_id": "ins-gitlab", "platform": "gitlab", "organization_id": "org-default"}]}
        return {"data": [{"installation_id": "ins-gitlab", "configured_runner_count": 2, "degraded_runner_count": 1}]}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", get)
    result = runner.invoke(cli, ["org", "install", "list", "--json", "configured_runner_count,degraded_runner_count"])
    assert result.exit_code == 0, result.output
    assert '"configured_runner_count": 2' in result.output
    assert '"degraded_runner_count": 1' in result.output
    assert paths == ["/users/me/installations", "/orgs/org-default/gitlab-installations"]


def _create_args(*extra: str) -> list[str]:
    return [
        "org",
        "install",
        "runner",
        "create",
        "--installation-id",
        "ins-test",
        "--machine-type",
        "linux-2-vcpu",
        "--personal-access-token-stdin",
        *extra,
    ]


def _created_in_gitlab(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get", lambda self, path: {"data": [RUNNER], "setup": SETUP}
    )
    monkeypatch.setattr(
        "httpx.Client.post", lambda *args, **kwargs: httpx.Response(201, json={"id": 99, "token": "glrt-created"})
    )


def _put_status(monkeypatch: pytest.MonkeyPatch, status: int, detail: str) -> None:
    def put(*args: Any, **kwargs: Any) -> None:
        request = httpx.Request("PUT", "https://api.avrea.com/runners/linux-2-vcpu")
        response = httpx.Response(status, request=request, json={"detail": detail})
        raise httpx.HTTPStatusError(str(status), request=request, response=response)

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", put)


@pytest.mark.parametrize(
    ("status", "detail", "expected"),
    [
        (403, "missing capability", "don't have permission to configure GitLab runners"),
        (409, "installation disconnected", "Avrea rejected the runner configuration for linux-2-vcpu (HTTP 409)"),
        (422, "token already registered", "Avrea rejected the runner configuration for linux-2-vcpu (HTTP 422)"),
    ],
)
def test_create_reports_a_definite_rejection_with_the_detail_and_runner(
    runner: CliRunner, monkeypatch: pytest.MonkeyPatch, status: int, detail: str, expected: str
) -> None:
    """A 4xx from Avrea after GitLab created the runner is a definite outcome:
    the message says Avrea rejected it, repeats the detail and names the
    GitLab runner that now has no Avrea configuration."""
    _created_in_gitlab(monkeypatch)
    _put_status(monkeypatch, status, detail)
    result = runner.invoke(cli, _create_args(), input="glpat-personal\n")
    assert result.exit_code == 1
    assert expected in result.output
    assert detail in result.output
    assert "GitLab runner 99 exists but is not configured in Avrea" in result.output
    assert "could not be confirmed" not in result.output
    assert "glrt-created" not in result.output


def test_create_keeps_the_uncertain_wording_for_a_server_error(
    runner: CliRunner, monkeypatch: pytest.MonkeyPatch
) -> None:
    _created_in_gitlab(monkeypatch)
    _put_status(monkeypatch, 503, "upstream")
    result = runner.invoke(cli, _create_args(), input="glpat-personal\n")
    assert result.exit_code == 1
    assert "GitLab runner 99 was created for linux-2-vcpu" in result.output
    assert "could not be confirmed" in result.output
    assert "rejected" not in result.output


def test_create_interrupted_during_avrea_put_names_the_runner_and_exits_130(
    runner: CliRunner, monkeypatch: pytest.MonkeyPatch
) -> None:
    """Ctrl-C after GitLab created the runner must not end in a bare
    "Aborted!": the runner ID is what the user needs to clean up or retry."""
    _created_in_gitlab(monkeypatch)

    def put(*args: Any, **kwargs: Any) -> None:
        raise KeyboardInterrupt

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", put)
    result = runner.invoke(cli, _create_args("--json", "data"), input="glpat-personal\n")
    assert result.exit_code == 130
    assert "Interrupted." in result.output
    assert "GitLab runner 99 was created for linux-2-vcpu" in result.output
    assert "glrt-created" not in result.output


@pytest.mark.parametrize(
    ("args", "flag"),
    [
        (["set", "linux-2-vcpu"], "--runner-token-stdin"),
        (["create", "--machine-type", "linux-2-vcpu"], "--personal-access-token-stdin"),
    ],
)
def test_prompt_disabled_refusal_names_the_stdin_flag(
    runner: CliRunner, monkeypatch: pytest.MonkeyPatch, args: list[str], flag: str
) -> None:
    """The refusal names the flag the command actually has, not --yes."""
    monkeypatch.setenv("AVR_PROMPT_DISABLED", "1")
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get", lambda self, path: {"data": [RUNNER], "setup": SETUP}
    )
    result = runner.invoke(cli, ["org", "install", "runner", *args, "--installation-id", "ins-test"])
    assert result.exit_code == 1
    assert "AVR_PROMPT_DISABLED" in result.output
    assert flag in result.output
    assert "--yes" not in result.output
