"""Unit tests for customer-facing git-mirror commands (avr repo mirror)."""

from avrea_cli.main import cli
from copy import deepcopy
import httpx
import json
import pytest

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


def _create_api(monkeypatch, *, enabled=False, clusters=SAMPLE_CLUSTERS, fail_at=None):
    calls = []

    def get(self, path, params=None):
        calls.append(("GET", path, params))
        if path.endswith("/repos/resolve"):
            assert params is not None
            return {"data": {"repository_id": REPO_ID, "full_name": params["name"]}}
        if path.endswith("/git-clusters"):
            return clusters
        assert path == MIRROR_PATH
        return {**json.loads(json.dumps(SAMPLE_MIRROR)), "enabled": enabled}

    def put(self, path, json=None):
        calls.append(("PUT", path, json))
        if fail_at == path:
            return _raise_status(503, "Temporary failure")(self, path, json)
        return deepcopy(SAMPLE_MIRROR)

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", get)
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", put)
    return calls


@pytest.mark.parametrize(
    ("repository", "source_args", "platform"),
    [
        ("acme/widgets", [], "github"),
        ("acme/widgets", ["--source", "github"], "github"),
        ("acme/widgets", ["--source", "gitlab"], "gitlab"),
        ("acme/team/widgets", ["--source", "gitlab"], "gitlab"),
        ("rep-team/widgets", [], "github"),
    ],
)
def test_create_resolves_source_and_enables_then_places(runner, monkeypatch, repository, source_args, platform):
    calls = _create_api(monkeypatch)
    result = runner.invoke(cli, ["repo", "mirror", "create", repository, "--location", "gsc-fi", *source_args])

    assert result.exit_code == 0, result.output
    assert calls == [
        ("GET", f"/orgs/{ORG_ID}/repos/resolve", {"name": repository, "platform": platform}),
        ("GET", f"/orgs/{ORG_ID}/git-clusters", {"limit": 1000, "order": "cluster_id.asc"}),
        ("GET", MIRROR_PATH, None),
        ("PUT", MIRROR_PATH, {"enabled": True}),
        ("PUT", MIRROR_PATH + "/placements/gsc-fi", None),
    ]
    assert "Sync runs asynchronously" in result.output
    assert f"status --repo {REPO_ID} --org {ORG_ID}" in result.output


@pytest.mark.parametrize("location", ["gsc-fi", "dc-fi", "Finland", "finland"])
def test_create_accepts_location_identifiers_and_skips_enabled_declaration(runner, monkeypatch, location):
    calls = _create_api(monkeypatch, enabled=True)
    result = runner.invoke(cli, ["repo", "mirror", "create", "acme/widgets", "--location", location])
    assert result.exit_code == 0, result.output
    assert [c for c in calls if c[0] == "PUT"] == [("PUT", MIRROR_PATH + "/placements/gsc-fi", None)]


@pytest.mark.parametrize("location", ["missing", "Finland"])
def test_create_invalid_or_ambiguous_location_does_not_mutate(runner, monkeypatch, location):
    clusters = {
        "data": [
            {"cluster_id": "gsc-one", "name": "Finland"},
            {"cluster_id": "gsc-two", "name": "Finland"},
        ],
        "pagination": {"next_cursor": None},
    }
    calls = _create_api(monkeypatch, clusters=clusters)
    result = runner.invoke(cli, ["repo", "mirror", "create", "acme/widgets", "--location", location])
    assert result.exit_code != 0
    assert (
        "Unknown mirror location" in result.output
        if location == "missing"
        else "Ambiguous mirror location" in result.output
    )
    assert not any(c[0] == "PUT" for c in calls)


@pytest.mark.parametrize("failure", [MIRROR_PATH, MIRROR_PATH + "/placements/gsc-fi"])
def test_create_failure_reports_partial_state_and_never_claims_success(runner, monkeypatch, failure):
    calls = _create_api(monkeypatch, fail_at=failure)
    result = runner.invoke(cli, ["repo", "mirror", "create", "acme/widgets", "--location", "gsc-fi"])
    assert result.exit_code != 0
    assert "Mirror configured" not in result.output
    if failure == MIRROR_PATH:
        assert calls[-1] == ("PUT", MIRROR_PATH, {"enabled": True})
        assert "Mirroring is enabled" not in result.output
    else:
        assert "Mirroring is enabled, but placement failed" in result.output
        assert "rerun this create command" in result.output


@pytest.mark.parametrize("json_fields", ["unknown", "?"])
def test_create_json_validation_precedes_network(runner, monkeypatch, json_fields):
    calls = _create_api(monkeypatch)
    result = runner.invoke(
        cli, ["repo", "mirror", "create", "acme/widgets", "--location", "gsc-fi", "--json", json_fields]
    )
    assert result.exit_code == (0 if json_fields == "?" else 1), result.output
    assert calls == []


def test_create_json_is_clean_and_jq_supported(runner, monkeypatch):
    _create_api(monkeypatch)
    result = runner.invoke(
        cli,
        [
            "repo",
            "mirror",
            "create",
            "acme/widgets",
            "--location",
            "gsc-fi",
            "--json",
            "repository_id,enabled",
            "--jq",
            ".repository_id",
        ],
    )
    assert result.exit_code == 0, result.output
    assert result.output.strip() == REPO_ID


@pytest.mark.parametrize(
    "args",
    [
        ["acme/widgets"],
        ["acme/widgets", "--location", "gsc-fi", "--source", "bitbucket"],
        ["acme/team/widgets", "--location", "gsc-fi"],
        ["acme/../widgets", "--location", "gsc-fi", "--source", "gitlab"],
        ["https://gitlab.com/acme/widgets", "--location", "gsc-fi", "--source", "gitlab"],
    ],
)
def test_create_bad_input_never_calls_api(runner, monkeypatch, args):
    calls = _create_api(monkeypatch)
    result = runner.invoke(cli, ["repo", "mirror", "create", *args])
    assert result.exit_code != 0
    assert calls == []


def test_locations_lists_all_pages_and_supports_json(runner, monkeypatch):
    cursors = []

    def get(self, path, params=None):
        assert path == f"/orgs/{ORG_ID}/git-clusters"
        assert params is not None
        cursors.append(params.get("cursor"))
        if not params.get("cursor"):
            return {"data": SAMPLE_CLUSTERS["data"][:1], "pagination": {"next_cursor": "next"}}
        return {"data": SAMPLE_CLUSTERS["data"][1:], "pagination": {"next_cursor": None}}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", get)
    result = runner.invoke(cli, ["repo", "mirror", "locations", "--json", "cluster_id,name"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == [
        {"cluster_id": "gsc-fi", "name": "Finland"},
        {"cluster_id": "gsc-rbx", "name": "Roubaix"},
    ]
    assert cursors == [None, "next"]


def test_create_gitlab_missing_repo_does_not_fall_back_to_github(runner, monkeypatch):
    calls = []

    def get(self, path, params=None):
        calls.append(params)
        return _raise_status(404, "Repository not connected")(self, path)

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", get)
    result = runner.invoke(
        cli, ["repo", "mirror", "create", "acme/widgets", "--source", "gitlab", "--location", "gsc-fi"]
    )
    assert result.exit_code != 0
    assert calls == [{"name": "acme/widgets", "platform": "gitlab"}]
    assert "Repository not connected" in result.output


def test_create_lost_placement_response_explains_safe_retry(runner, monkeypatch):
    _create_api(monkeypatch, enabled=True)

    def put(self, path, json=None):
        raise httpx.ReadTimeout("timed out")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", put)
    result = runner.invoke(cli, ["repo", "mirror", "create", "acme/widgets", "--location", "gsc-fi"])
    assert result.exit_code != 0
    assert "placement response was lost" in result.output
    assert "retry safely" in result.output


@pytest.mark.parametrize(
    "command", [["status"], ["enable"], ["disable", "--yes"], ["place", "gsc-fi"], ["unplace", "gsc-fi", "--yes"]]
)
def test_mirror_management_resolves_gitlab_names(runner, monkeypatch, command):
    calls = _create_api(monkeypatch)
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", lambda *args, **kwargs: SAMPLE_MIRROR)
    result = runner.invoke(cli, ["repo", "mirror", *command, "--repo", "acme/team/widgets", "--source", "gitlab"])
    assert result.exit_code == 0, result.output
    assert calls[0] == ("GET", f"/orgs/{ORG_ID}/repos/resolve", {"name": "acme/team/widgets", "platform": "gitlab"})


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
    responses = (
        [],
        {"data": "not-a-list", "pagination": {"next_cursor": None}},
        {"data": []},
        {"data": [], "pagination": []},
        {"data": [], "pagination": {}},
    )

    for response in responses:
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None, response=response: response,
        )

        result = runner.invoke(cli, ["repo", "mirror", "clusters"])

        assert result.exit_code != 0
        assert "Unexpected" in result.stderr
        assert "git clusters" in result.stderr


def test_json_meta_lists_fields(runner):
    result = runner.invoke(cli, ["repo", "mirror", "status", "--json", "?"])

    assert result.exit_code == 0, result.output
    listed = result.output.split()
    assert "enabled" in listed
    assert "placements" in listed
