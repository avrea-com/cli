"""Native CI records use public reads without fabricated provider IDs."""

from avrea_cli.commands.job import _print_jobs_table
from avrea_cli.main import cli
from avrea_cli.run_refs import RunReferenceKind
from avrea_cli.run_refs import parse_run_reference
from click.testing import CliRunner
from pytest import MonkeyPatch
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
