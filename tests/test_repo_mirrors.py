"""Unit tests for customer-facing repository mirror commands."""

from avrea_cli.main import cli
import httpx
import json

REPO_ID = "rep-abc123"

SAMPLE_MIRROR = {
    "repository_id": REPO_ID,
    "full_name": "acme/widgets",
    "enabled": True,
    "placements": [
        {
            "cluster_id": "git-eu-hel1",
            "role": "primary",
            "last_sync_at": "2026-07-31T12:00:00Z",
            "last_sync_status": "ok",
        },
        {
            "cluster_id": "git-us-ash1",
            "role": "mirror",
            "last_sync_at": None,
            "last_sync_status": None,
        },
    ],
}

SAMPLE_NOT_MIRRORED = {
    "repository_id": REPO_ID,
    "full_name": "acme/widgets",
    "enabled": False,
    "placements": [],
}


def _raise_status(status, detail):
    def mock_call(self, path, *args, **kwargs):
        request = httpx.Request("GET", "https://api.avrea.com" + path)
        response = httpx.Response(status, request=request, json={"detail": detail})
        raise httpx.HTTPStatusError(str(status), request=request, response=response)

    return mock_call


def test_create_posts_to_mirror_endpoint(runner, monkeypatch):
    captured = {}

    def mock_post(self, path, json=None, timeout=None, **kwargs):
        captured["path"] = path
        captured["json"] = json
        return SAMPLE_MIRROR.copy()

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)

    result = runner.invoke(cli, ["repo", "mirror", "create", "--repo", REPO_ID])

    assert result.exit_code == 0, result.output
    assert captured["path"] == f"/orgs/org-default/repos/{REPO_ID}/mirror"
    assert captured["json"] is None
    assert "Created mirror for acme/widgets" in result.output
    assert "git-eu-hel1" in result.output
    assert "primary" in result.output


def test_create_json_outputs_record(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_post",
        lambda self, path, json=None, timeout=None, **kwargs: SAMPLE_MIRROR.copy(),
    )

    result = runner.invoke(cli, ["repo", "mirror", "create", "--repo", REPO_ID, "--json", "*"])

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == SAMPLE_MIRROR


def test_create_404_surfaces_feature_hint(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_post",
        _raise_status(404, "Not found"),
    )

    result = runner.invoke(cli, ["repo", "mirror", "create", "--repo", REPO_ID])

    assert result.exit_code == 1
    assert "Git mirrors may not be enabled" in result.stderr


def test_status_gets_mirror_state(runner, monkeypatch):
    captured = {}

    def mock_get(self, path, params=None):
        captured["path"] = path
        return SAMPLE_MIRROR.copy()

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)

    result = runner.invoke(cli, ["repo", "mirror", "status", "--repo", REPO_ID])

    assert result.exit_code == 0, result.output
    assert captured["path"] == f"/orgs/org-default/repos/{REPO_ID}/mirror"
    assert "yes" in result.output
    assert "git-eu-hel1" in result.output
    assert "git-us-ash1" in result.output


def test_status_reports_not_mirrored(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: SAMPLE_NOT_MIRRORED.copy(),
    )

    result = runner.invoke(cli, ["repo", "mirror", "status", "--repo", REPO_ID])

    assert result.exit_code == 0, result.output
    assert "no" in result.output


def test_status_json_projects_fields(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: SAMPLE_MIRROR.copy(),
    )

    result = runner.invoke(cli, ["repo", "mirror", "status", "--repo", REPO_ID, "--json", "enabled,full_name"])

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == {"enabled": True, "full_name": "acme/widgets"}


def test_delete_requires_confirmation(runner, monkeypatch):
    called = {}

    def mock_delete(self, path, params=None):
        called["path"] = path
        return SAMPLE_NOT_MIRRORED.copy()

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)

    result = runner.invoke(cli, ["repo", "mirror", "delete", "--repo", REPO_ID], input="n\n")

    assert result.exit_code == 1
    assert not called


def test_delete_with_yes_disables_mirror(runner, monkeypatch):
    captured = {}

    def mock_delete(self, path, params=None):
        captured["path"] = path
        return SAMPLE_NOT_MIRRORED.copy()

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)

    result = runner.invoke(cli, ["repo", "mirror", "delete", "--repo", REPO_ID, "--yes"])

    assert result.exit_code == 0, result.output
    assert captured["path"] == f"/orgs/org-default/repos/{REPO_ID}/mirror"
    assert "Disabled mirror for acme/widgets" in result.output


def test_delete_tolerates_empty_response(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_delete",
        lambda self, path, params=None: None,
    )

    result = runner.invoke(cli, ["repo", "mirror", "delete", "--repo", REPO_ID, "--yes"])

    assert result.exit_code == 0, result.output
    assert f"Disabled mirror for {REPO_ID}" in result.output


def test_json_meta_makes_no_api_call(runner, monkeypatch):
    def mock_get(self, path, params=None):
        raise AssertionError("no API call expected for --json '?'")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)

    result = runner.invoke(cli, ["repo", "mirror", "status", "--json", "?"])

    assert result.exit_code == 0, result.output
    assert "enabled" in result.output
    assert "placements" in result.output
