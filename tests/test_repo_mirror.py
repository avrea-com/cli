"""Unit tests for customer-facing git-mirror commands (avr repo mirror)."""

from avrea_cli.main import cli
import httpx
import json

REPO_ID = "rep-1"
ORG_ID = "org-default"

SAMPLE_MIRROR = {
    "repository_id": REPO_ID,
    "full_name": "acme/widgets",
    "enabled": True,
    "placements": [
        {
            "cluster_id": "gsc-fi",
            "role": "mirror",
            "last_sync_at": "2026-07-30T08:00:00Z",
            "last_sync_status": "success",
            "config_synced": True,
            "config_synced_at": "2026-07-30T08:00:00Z",
        }
    ],
}

SAMPLE_CLUSTERS = {
    "data": [
        {"cluster_id": "gsc-fi", "datacenter_id": "dc-fi", "name": "Finland"},
        {"cluster_id": "gsc-rbx", "datacenter_id": "dc-rbx", "name": "Roubaix"},
    ],
    "pagination": {"next_cursor": None},
}

MIRROR_PATH = f"/orgs/{ORG_ID}/repos/{REPO_ID}/git-mirrors"


def _capture(captured, key, result):
    def mock(self, path, json=None, params=None):
        captured[key] = path
        captured.setdefault("bodies", []).append(json)
        captured.setdefault("params", []).append(params)
        return result() if callable(result) else result

    return mock


def _raise_status(status, detail):
    def mock(self, path, json=None, params=None):
        request = httpx.Request("GET", "https://api.avrea.com" + path)
        response = httpx.Response(status, request=request, json={"detail": detail})
        raise httpx.HTTPStatusError(str(status), request=request, response=response)

    return mock


def test_status_hits_git_mirror_endpoint(runner, monkeypatch):
    captured = {}
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        _capture(captured, "get", lambda: json.loads(json.dumps(SAMPLE_MIRROR))),
    )

    result = runner.invoke(cli, ["repo", "mirror", "status", "--repo", REPO_ID])

    assert result.exit_code == 0, result.output
    assert captured["get"] == MIRROR_PATH
    assert "acme/widgets" in result.output
    assert "enabled" in result.output
    assert "gsc-fi" in result.output
    assert "success" in result.output


def test_status_json_projects_fields(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: json.loads(json.dumps(SAMPLE_MIRROR)),
    )

    result = runner.invoke(
        cli,
        ["repo", "mirror", "status", "--repo", REPO_ID, "--json", "enabled,full_name"],
    )

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == {"enabled": True, "full_name": "acme/widgets"}


def test_status_no_placements_hint(runner, monkeypatch):
    empty = {**json.loads(json.dumps(SAMPLE_MIRROR)), "placements": []}
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: empty,
    )

    result = runner.invoke(cli, ["repo", "mirror", "status", "--repo", REPO_ID])

    assert result.exit_code == 0, result.output
    assert "avr repo mirror place" in result.output


def test_status_surfaces_flag_off_404(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        _raise_status(404, "Not found"),
    )

    result = runner.invoke(cli, ["repo", "mirror", "status", "--repo", REPO_ID])

    assert result.exit_code != 0
    assert "Not found while trying to get git-mirror status (HTTP 404)" in result.stderr


def test_enable_puts_enabled_true(runner, monkeypatch):
    captured = {}
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_put",
        _capture(captured, "put", lambda: json.loads(json.dumps(SAMPLE_MIRROR))),
    )

    result = runner.invoke(cli, ["repo", "mirror", "enable", "--repo", REPO_ID])

    assert result.exit_code == 0, result.output
    assert captured["put"] == MIRROR_PATH
    assert captured["bodies"] == [{"enabled": True}]


def test_disable_confirms_and_puts_enabled_false(runner, monkeypatch):
    captured = {}
    disabled = {**json.loads(json.dumps(SAMPLE_MIRROR)), "enabled": False}
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_put",
        _capture(captured, "put", lambda: disabled),
    )

    result = runner.invoke(cli, ["repo", "mirror", "disable", "--repo", REPO_ID], input="y\n")

    assert result.exit_code == 0, result.output
    assert captured["bodies"] == [{"enabled": False}]
    assert "disabled" in result.output


def test_disable_yes_skips_prompt(runner, monkeypatch):
    captured = {}
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_put",
        _capture(captured, "put", lambda: json.loads(json.dumps(SAMPLE_MIRROR))),
    )

    result = runner.invoke(cli, ["repo", "mirror", "disable", "--repo", REPO_ID, "--yes"])

    assert result.exit_code == 0, result.output
    assert "Disable git mirroring" not in result.output


def test_place_puts_placement(runner, monkeypatch):
    captured = {}
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_put",
        _capture(captured, "put", lambda: json.loads(json.dumps(SAMPLE_MIRROR))),
    )

    result = runner.invoke(cli, ["repo", "mirror", "place", "gsc-fi", "--repo", REPO_ID])

    assert result.exit_code == 0, result.output
    assert captured["put"] == f"{MIRROR_PATH}/placements/gsc-fi"


def test_place_quotes_cluster_id(runner, monkeypatch):
    captured = {}
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_put",
        _capture(captured, "put", lambda: json.loads(json.dumps(SAMPLE_MIRROR))),
    )

    result = runner.invoke(cli, ["repo", "mirror", "place", "a/b", "--repo", REPO_ID])

    assert result.exit_code == 0, result.output
    assert captured["put"] == f"{MIRROR_PATH}/placements/a%2Fb"


def test_place_conflict_when_not_declared(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_put",
        _raise_status(409, "Git mirroring is not enabled for this repository; enable it first"),
    )

    result = runner.invoke(cli, ["repo", "mirror", "place", "gsc-fi", "--repo", REPO_ID])

    assert result.exit_code != 0
    assert "enable it first" in result.stderr


def test_unplace_deletes_placement(runner, monkeypatch):
    captured = {}

    def mock_delete(self, path, params=None):
        captured["delete"] = path
        return json.loads(json.dumps(SAMPLE_MIRROR))

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)

    result = runner.invoke(cli, ["repo", "mirror", "unplace", "gsc-fi", "--repo", REPO_ID, "--yes"])

    assert result.exit_code == 0, result.output
    assert captured["delete"] == f"{MIRROR_PATH}/placements/gsc-fi"


def test_unplace_tolerates_empty_body(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_delete",
        lambda self, path, params=None: None,
    )

    result = runner.invoke(cli, ["repo", "mirror", "unplace", "gsc-fi", "--repo", REPO_ID, "--yes"])

    assert result.exit_code == 0, result.output
    assert "Removed the git-mirror placement in gsc-fi." in result.output


def test_clusters_lists_targets(runner, monkeypatch):
    captured = {}
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        _capture(captured, "get", lambda: json.loads(json.dumps(SAMPLE_CLUSTERS))),
    )

    result = runner.invoke(cli, ["repo", "mirror", "clusters"])

    assert result.exit_code == 0, result.output
    assert captured["get"] == f"/orgs/{ORG_ID}/git-clusters"
    assert captured["params"] == [{"limit": 1000, "order": "cluster_id.asc"}]
    assert "gsc-fi" in result.output
    assert "Roubaix" in result.output


def test_clusters_json(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: json.loads(json.dumps(SAMPLE_CLUSTERS)),
    )

    result = runner.invoke(cli, ["repo", "mirror", "clusters", "--json", "cluster_id"])

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == [{"cluster_id": "gsc-fi"}, {"cluster_id": "gsc-rbx"}]


def test_clusters_follows_pagination(runner, monkeypatch):
    calls = []

    def mock_get(self, path, params=None):
        assert params is not None
        calls.append((path, params))
        if params.get("cursor") is None:
            return {
                "data": [{"cluster_id": "gsc-fi", "datacenter_id": "dc-fi", "name": "Finland"}],
                "pagination": {"next_cursor": "next-page"},
            }
        return {
            "data": [{"cluster_id": "gsc-rbx", "datacenter_id": "dc-rbx", "name": "Roubaix"}],
            "pagination": {"next_cursor": None},
        }

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)

    result = runner.invoke(cli, ["repo", "mirror", "clusters", "--json", "cluster_id"])

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == [{"cluster_id": "gsc-fi"}, {"cluster_id": "gsc-rbx"}]
    assert calls == [
        (
            f"/orgs/{ORG_ID}/git-clusters",
            {"limit": 1000, "order": "cluster_id.asc"},
        ),
        (
            f"/orgs/{ORG_ID}/git-clusters",
            {"limit": 1000, "order": "cluster_id.asc", "cursor": "next-page"},
        ),
    ]


def test_clusters_rejects_malformed_response(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {"data": "not-a-list", "pagination": {"next_cursor": None}},
    )

    result = runner.invoke(cli, ["repo", "mirror", "clusters"])

    assert result.exit_code != 0
    assert "Unexpected response while listing git clusters" in result.stderr


def test_json_meta_lists_fields(runner):
    result = runner.invoke(cli, ["repo", "mirror", "status", "--json", "?"])

    assert result.exit_code == 0, result.output
    listed = result.output.split()
    assert "enabled" in listed
    assert "placements" in listed
