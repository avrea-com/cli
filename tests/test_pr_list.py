"""Tests for ``avr pr list``."""

from avrea_cli.main import cli
import json

PULL = {
    "number": 42,
    "title": "Keep PR reads inside Avrea",
    "state": "open",
    "draft": False,
    "merged": False,
    "author_login": "octocat",
    "base_ref": "main",
    "head_ref": "pr-list",
    "head_sha": "a" * 40,
    "base_sha": "b" * 40,
    "created_at": "2026-08-16T10:00:00Z",
    "updated_at": "2026-08-17T10:00:00Z",
    "comment_count": 3,
    "unresolved_thread_count": 1,
    "check_status": "success",
    "mergeability": {"status": "mergeable", "base_sha": "b" * 40, "head_sha": "a" * 40},
    "repository_id": "rep-widgets",
    "repository_full_name": "acme/widgets",
}


def test_default_list_uses_the_org_endpoint(runner, monkeypatch):
    captured = {}

    def fake_get(self, path, params=None):
        captured["path"] = path
        captured["params"] = params
        return {"data": [PULL], "pagination": {"next_cursor": None}}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

    result = runner.invoke(cli, ["pr", "list"])

    assert result.exit_code == 0, result.output
    assert captured == {
        "path": "/orgs/org-default/pull-requests",
        "params": {"scope": "all", "state": "open", "limit": 20},
    }
    assert "acme/widgets" in result.output
    assert "mergeable" in result.output
    assert result.output.index("PR") < result.output.index("REPOSITORY")


def test_filters_repositories_scope_state_and_cursor(runner, monkeypatch):
    captured = {}
    monkeypatch.setattr(
        "avrea_cli.commands.pr.resolve_repos_or_detect",
        lambda client, config, org_id, repos, *, soft_detect: ["rep-one", "rep-two"],
    )

    def fake_get(self, path, params=None):
        captured.update(params or {})
        return {"data": [], "pagination": {"next_cursor": None}}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

    result = runner.invoke(
        cli,
        [
            "pr",
            "list",
            "--repo",
            "acme/one",
            "--repo",
            "acme/two",
            "--scope",
            "involved",
            "--state",
            "all",
            "--cursor",
            "next-page",
            "-L",
            "7",
        ],
    )

    assert result.exit_code == 0, result.output
    assert captured == {
        "scope": "involved",
        "limit": 7,
        "repository_ids": ["rep-one", "rep-two"],
        "cursor": "next-page",
    }


def test_json_preserves_nested_mergeability(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {"data": [PULL], "pagination": {}},
    )

    result = runner.invoke(cli, ["pr", "list", "--json", "number,repository_full_name,mergeability"])

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == [
        {
            "number": 42,
            "repository_full_name": "acme/widgets",
            "mergeability": PULL["mergeability"],
        }
    ]


def test_next_cursor_is_reported_on_stderr(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {"data": [PULL], "pagination": {"next_cursor": "page-two"}},
    )

    result = runner.invoke(cli, ["prs", "list"])

    assert result.exit_code == 0, result.output
    assert "Next page: --cursor page-two" in result.output


def test_unknown_scope_is_rejected_before_api_call(runner, monkeypatch):
    called = False

    def fake_get(self, path, params=None):
        nonlocal called
        called = True
        return {"data": []}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

    result = runner.invoke(cli, ["pr", "list", "--scope", "mine"])

    assert result.exit_code == 2
    assert not called


def test_open_pull_without_mergeability_renders_unknown(runner, monkeypatch):
    pull = {**PULL, "mergeability": None}
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {"data": [pull], "pagination": {}},
    )

    result = runner.invoke(cli, ["pr", "list"])

    assert result.exit_code == 0, result.output
    assert "?" in result.output


def test_single_repository_output_omits_redundant_repository_column(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.commands.pr.resolve_repos_or_detect",
        lambda client, config, org_id, repos, *, soft_detect: ["rep-widgets"],
    )
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {"data": [PULL], "pagination": {}},
    )

    result = runner.invoke(cli, ["pr", "list", "--repo", "acme/widgets"])

    assert result.exit_code == 0, result.output
    assert "REPOSITORY" not in result.output


def test_pull_without_checks_renders_no_checks_not_unknown(runner, monkeypatch):
    pull = {**PULL, "check_status": None}
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {"data": [pull], "pagination": {}},
    )

    result = runner.invoke(cli, ["pr", "list"])

    assert result.exit_code == 0, result.output
    assert "?" not in result.output
