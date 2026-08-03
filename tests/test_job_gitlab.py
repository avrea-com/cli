from avrea_cli.main import cli
from avrea_cli.run_refs import parse_run_reference
from click.testing import CliRunner
from pytest import MonkeyPatch
import click
import json
import pytest


@pytest.mark.parametrize("platform", ["github", "gitlab"])
def test_job_view_platform_link(runner: CliRunner, monkeypatch: MonkeyPatch, platform: str) -> None:
    job = {
        "job_id": "job-abc123",
        "platform": platform,
        "repository_full_name": "group/project",
        "platform_job_id": 42,
        "platform_run_id": 7,
    }
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda *args, **kwargs: {"data": job})
    monkeypatch.setattr("avrea_cli.commands.job.get_org_slug", lambda *args: "group")
    result = runner.invoke(cli, ["job", "view", "job-abc123", "--web"])
    assert result.exit_code == 0, result.output
    expected = (
        "GitLab: https://gitlab.com/group/project/-/jobs/42"
        if platform == "gitlab"
        else ("GitHub: https://github.com/group/project/actions/runs/7/job/42")
    )
    assert expected in result.output
    result = runner.invoke(cli, ["job", "view", "job-abc123", "--json", "platform"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.output)["platform"] == platform


@pytest.mark.parametrize("value", ["job-abc123", "https://gitlab.com/group/project/-/jobs/42"])
def test_run_commands_explain_gitlab_job_scope(value: str) -> None:
    with pytest.raises(click.BadParameter, match="Use 'avr job view JOB_ID'"):
        parse_run_reference(value, api_url="https://api.avrea.com")
