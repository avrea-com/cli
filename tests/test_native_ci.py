"""Native CI records use public reads without fabricated provider IDs."""

from avrea_cli.commands.job import _print_jobs_table
from avrea_cli.main import cli
from avrea_cli.run_refs import RunReferenceKind
from avrea_cli.run_refs import parse_run_reference
from click.testing import CliRunner
from pathlib import Path
from pytest import MonkeyPatch
from types import SimpleNamespace
from unittest.mock import MagicMock
import json
import pytest

_RUN = "awr-019e5a2e474e71c297e452279d4bcdea"
_JOB = "awj-019e5a2e474e71c297e452279d4bcdea"
_RUN_DATA = {
    "run_id": _RUN,
    "ci_provider": "avrea_ci",
    "display_title": "Native build",
    "workflow_name": "Native build",
    "workflow_path": ".avrea/workflows/build.yml",
    "platform_run_id": None,
    "platform_workflow_id": None,
    "run_number": None,
    "run_attempt": None,
    "status": "completed",
    "conclusion": "success",
    "workflow": None,
    "jobs": [],
    "repository": {"full_name": "acme/widgets"},
}
_JOB_DATA = {
    "job_id": _JOB,
    "run_id": _RUN,
    "ci_provider": "avrea_ci",
    "job_name": "build",
    "platform_job_id": None,
    "platform_run_id": None,
    "run_attempt": None,
    "state": "completed",
    "conclusion": "success",
    "running_on_avrea": True,
    "workflow_run": {"run_id": _RUN, "run_number": None, "run_attempt": None},
}


@pytest.mark.parametrize("job_id", [_JOB, "job-019e5a2e474e71c297e452279d4bcdea"])
@pytest.mark.parametrize("exit_code", [0, 23])
def test_job_ssh_pins_the_returned_host_key(
    runner: CliRunner, monkeypatch: MonkeyPatch, job_id: str, exit_code: int
) -> None:
    response = {
        "host": "127.0.0.1",
        "port": 2222,
        "username": "runner",
        "host_key": "ssh-ed25519 AAAA",
        "ssh_keys_installed": True,
    }
    post = MagicMock(return_value=response)
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", post)
    paths: list[Path] = []

    def connect(argv: list[str]) -> SimpleNamespace:
        assert "StrictHostKeyChecking=yes" in argv
        path = Path(next(arg.split("=", 1)[1] for arg in argv if arg.startswith("UserKnownHostsFile=")))
        assert path.read_text() == "[127.0.0.1]:2222 ssh-ed25519 AAAA\n"
        paths.append(path)
        return SimpleNamespace(returncode=exit_code)

    monkeypatch.setattr("avrea_cli.commands.job.subprocess.run", connect)
    result = runner.invoke(cli, ["job", "ssh", job_id])
    assert result.exit_code == exit_code, result.exception
    post.assert_called_once_with("/vms/ssh-forwarding", json={"job_id": job_id}, timeout=60.0)
    assert paths and not paths[0].exists()


def test_native_job_ssh_refuses_a_missing_host_key(runner: CliRunner, monkeypatch: MonkeyPatch) -> None:
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_post",
        lambda *args, **kwargs: {
            "host": "127.0.0.1",
            "port": 2222,
            "username": "runner",
            "ssh_keys_installed": True,
        },
    )
    connect = MagicMock()
    monkeypatch.setattr("avrea_cli.commands.job.subprocess.run", connect)
    result = runner.invoke(cli, ["job", "ssh", _JOB])
    assert result.exit_code == 1
    assert "refusing an unverified connection" in result.output
    connect.assert_not_called()


@pytest.mark.parametrize("reference", [_RUN, f"https://console.avrea.com/org/acme/runs/{_RUN}"])
def test_native_run_reference(reference: str) -> None:
    parsed = parse_run_reference(reference, api_url="https://api.avrea.com")
    assert parsed.run_id == _RUN
    assert parsed.kind in (RunReferenceKind.AVREA_ID, RunReferenceKind.AVREA_URL)


@pytest.mark.parametrize("command,identity,data", [("run", _RUN, _RUN_DATA), ("job", _JOB, _JOB_DATA)])
def test_native_public_view(
    runner: CliRunner, monkeypatch: MonkeyPatch, command: str, identity: str, data: dict
) -> None:
    paths: list[str] = []

    def get(self: object, path: str, **kwargs: object) -> dict:
        paths.append(path)
        return {"data": data}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", get)
    result = runner.invoke(cli, [command, "view", identity])
    assert result.exit_code == 0, result.output
    assert "avrea_ci" in result.output
    assert "#None" not in result.output
    assert [path for path in paths if path != "/users/me/organizations"] == [
        f"/orgs/org-default/{'workflow-runs' if command == 'run' else 'jobs'}/{identity}"
    ]


@pytest.mark.parametrize("command,data", [("run", _RUN_DATA), ("job", _JOB_DATA)])
def test_native_list_json_provider(runner: CliRunner, monkeypatch: MonkeyPatch, command: str, data: dict) -> None:
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda *args, **kwargs: {"data": [data]})
    result = runner.invoke(cli, [command, "list", "--json", "ci_provider"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == [{"ci_provider": "avrea_ci"}]


@pytest.mark.parametrize(
    "args,data,feature",
    [
        (["job", "logs", _JOB], _JOB_DATA, "Logs"),
        (["job", "logs", _JOB, "--failed"], _JOB_DATA, "Logs"),
        (["job", "logs", _JOB, "--follow"], _JOB_DATA, "Logs"),
        (["job", "view", _JOB, "--log"], _JOB_DATA, "Logs"),
        (["job", "view", _JOB, "--log-failed"], _JOB_DATA, "Logs"),
        (["job", "metrics", _JOB], _JOB_DATA, "Metrics"),
        (["run", "logs", _RUN], _RUN_DATA, "Logs"),
        (["run", "logs", _RUN, "--failed"], _RUN_DATA, "Logs"),
        (["run", "view", _RUN, "--log"], _RUN_DATA, "Logs"),
        (["run", "view", _RUN, "--log-failed"], _RUN_DATA, "Logs"),
        (["run", "diagnose", _RUN], _RUN_DATA, "Diagnostics"),
        (["run", "rerun", _RUN, "--yes"], _RUN_DATA, "Reruns"),
    ],
)
def test_native_unsupported_operations_are_explicit(
    runner: CliRunner, monkeypatch: MonkeyPatch, args: list[str], data: dict, feature: str
) -> None:
    get = MagicMock(return_value={"data": data})
    post = MagicMock(return_value={"data": []})
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", get)
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", post)
    result = runner.invoke(cli, args)
    assert result.exit_code == 1, result.output
    assert f"{feature} are not available for Avrea CI yet" in result.output
    assert get.call_count == 1
    post.assert_not_called()


def test_native_job_view_does_not_offer_unavailable_logs(runner: CliRunner, monkeypatch: MonkeyPatch) -> None:
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda *args, **kwargs: {"data": _JOB_DATA})
    result = runner.invoke(cli, ["job", "view", _JOB])
    assert result.exit_code == 0, result.output
    assert "avr job logs" not in result.output


@pytest.mark.parametrize("platform,provider", [("github", "github_actions"), ("gitlab", "gitlab_ci")])
def test_job_list_identifies_provider_from_old_control(
    platform: str, provider: str, capsys: pytest.CaptureFixture[str]
) -> None:
    _print_jobs_table([{"job_id": "job-019e5a2e474e71c297e452279d4bcdea", "platform": platform, "state": "completed"}])
    assert provider in capsys.readouterr().out


@pytest.mark.parametrize("command", ["list", "view"])
def test_native_runs_do_not_guess_github_source_links(
    runner: CliRunner, monkeypatch: MonkeyPatch, command: str
) -> None:
    data = {**_RUN_DATA, "head_branch": "main", "head_sha": "a" * 40}
    monkeypatch.setattr("click.testing._NamedTextIOWrapper.isatty", lambda self: True)
    monkeypatch.setattr("avrea_cli.commands.run.hyperlink", lambda text, url: f"{text}<{url}>")
    monkeypatch.setattr("avrea_cli.commands.run.get_org_slug", lambda *args: "acme")
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda *args, **kwargs: {"data": [data] if command == "list" else data},
    )
    result = runner.invoke(cli, ["--links", "run", command] + ([_RUN] if command == "view" else []))
    assert result.exit_code == 0, result.output
    assert "https://github.com/" not in result.output
    assert "https://console.avrea.com/" in result.output
