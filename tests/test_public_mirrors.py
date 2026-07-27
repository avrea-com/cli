"""Unit tests for customer-facing public Git mirror commands."""

from avrea_cli.main import cli
import httpx
import json

REQUEST_ID = "pmr-0123456789abcdef0123456789abcdef"
REPOSITORY_ID = "rep-public"

SAMPLE_REQUEST = {
    "request_id": REQUEST_ID,
    "repository_id": REPOSITORY_ID,
    "repository_full_name": "rust-lang/rust",
    "status": "PENDING",
    "requester_organization_id": "org-default",
    "requester_user_id": "usr-customer",
    "reason": "Build dependency",
    "github_snapshot": {"id": 724712},
    "created_at": "2026-07-24T08:00:00Z",
    "updated_at": "2026-07-24T08:00:00Z",
    "reviewed_at": None,
    "reviewed_by_user_id": None,
    "review_note": None,
    "approval_state": None,
    "public_access_expires_at": None,
}

SAMPLE_MIRROR = {
    "repository_id": REPOSITORY_ID,
    "platform_repository_id": 724712,
    "repository_full_name": "rust-lang/rust",
    "https_clone_url": "https://github.com/rust-lang/rust.git",
    "default_branch": "master",
    "platform_owner_id": 5430905,
    "platform_owner_type": "Organization",
    "platform_owner_login": "rust-lang",
    "is_archived": False,
    "is_disabled": False,
    "is_fork": False,
    "platform_size_kb": 4_500_000,
    "platform_pushed_at": "2026-07-24T07:30:00Z",
    "public_metadata_verified_at": "2026-07-24T08:00:00Z",
    "approval_state": "APPROVED",
    "public_access_expires_at": "2026-07-31T08:00:00Z",
    "mirror_enabled": True,
    "installation_kind": "PUBLIC_CATALOG",
}


def test_check_uses_exact_name_endpoint(runner, monkeypatch):
    captured = {}

    def mock_get(self, path, params=None):
        captured["path"] = path
        return SAMPLE_MIRROR.copy()

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)

    result = runner.invoke(cli, ["repo", "public-mirror", "check", "rust-lang/rust"])

    assert result.exit_code == 0, result.output
    assert captured["path"] == "/public-mirrors/rust-lang/rust"
    assert "rust-lang/rust" in result.output
    assert "master" in result.output
    assert "Available" in result.output


def test_check_json_projects_fields(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: SAMPLE_MIRROR.copy(),
    )

    result = runner.invoke(
        cli,
        [
            "repo",
            "public-mirror",
            "check",
            "rust-lang/rust",
            "--json",
            "repository_full_name,mirror_enabled",
        ],
    )

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == {"repository_full_name": "rust-lang/rust", "mirror_enabled": True}


def _raise_status(status, detail):
    def mock_get(self, path, params=None):
        request = httpx.Request("GET", "https://api.avrea.com" + path)
        response = httpx.Response(status, request=request, json={"detail": detail})
        raise httpx.HTTPStatusError(str(status), request=request, response=response)

    return mock_get


def test_check_reports_unavailable_instead_of_failing(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        _raise_status(404, "Public mirror not found"),
    )

    result = runner.invoke(cli, ["repo", "public-mirror", "check", "rust-lang/rust"])

    assert result.exit_code == 0, result.output
    assert "Available" in result.output
    assert "no" in result.output
    assert "rust-lang/rust" in result.output
    assert "Error" not in result.output
    assert "public-mirror request rust-lang/rust" in result.stderr


def test_check_unavailable_json_sets_available_false(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        _raise_status(404, "Public mirror not found"),
    )

    result = runner.invoke(
        cli,
        ["repo", "public-mirror", "check", "rust-lang/rust", "--json", "available,repository_full_name,default_branch"],
    )

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == {
        "available": False,
        "repository_full_name": "rust-lang/rust",
        "default_branch": None,
    }


def test_check_available_json_sets_available_true(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: SAMPLE_MIRROR.copy(),
    )

    result = runner.invoke(cli, ["repo", "public-mirror", "check", "rust-lang/rust", "--json", "available"])

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == {"available": True}


def test_check_still_fails_on_non_404_errors(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        _raise_status(422, "Invalid GitHub repository name"),
    )

    result = runner.invoke(cli, ["repo", "public-mirror", "check", "rust-lang/rust"])

    assert result.exit_code == 1
    assert "Invalid GitHub repository name" in result.stderr


def test_check_rejects_non_owner_repo_name_without_api_call(runner, monkeypatch):
    called = False

    def unexpected_get(self, path, params=None):
        nonlocal called
        called = True

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", unexpected_get)

    result = runner.invoke(cli, ["repo", "public-mirror", "check", "github.com/rust-lang/rust"])

    assert result.exit_code == 2
    assert "owner/repository" in result.output
    assert not called


def test_public_mirror_catalog_cannot_be_listed(runner, monkeypatch):
    def unexpected_get(self, path, params=None):
        raise AssertionError("API should not be called")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", unexpected_get)

    result = runner.invoke(cli, ["repo", "public-mirror", "list"])

    assert result.exit_code == 2
    assert "No such command 'list'" in result.output


def test_request_posts_repository_and_reason(runner, monkeypatch):
    captured = {}

    def mock_post(self, path, json=None, timeout=None):
        captured["path"] = path
        captured["json"] = json
        return SAMPLE_REQUEST.copy()

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)

    result = runner.invoke(
        cli,
        [
            "repo",
            "public-mirror",
            "request",
            "rust-lang/rust",
            "--reason",
            "Build dependency",
        ],
    )

    assert result.exit_code == 0, result.output
    assert captured == {
        "path": "/orgs/org-default/public-mirrors/requests",
        "json": {"full_name": "rust-lang/rust", "reason": "Build dependency"},
    }
    assert REQUEST_ID in result.output
    assert "PENDING" in result.output


def test_request_omits_absent_reason_and_supports_json(runner, monkeypatch):
    captured = {}

    def mock_post(self, path, json=None, timeout=None):
        captured["json"] = json
        return SAMPLE_REQUEST.copy()

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)

    result = runner.invoke(
        cli,
        ["repo", "public-mirror", "request", "rust-lang/rust", "--json", "request_id,status"],
    )

    assert result.exit_code == 0, result.output
    assert captured["json"] == {"full_name": "rust-lang/rust"}
    assert json.loads(result.output) == {"request_id": REQUEST_ID, "status": "PENDING"}


def test_requests_lists_org_scoped_requests(runner, monkeypatch):
    captured = {}

    def mock_get(self, path, params=None):
        captured["path"] = path
        return [SAMPLE_REQUEST.copy()]

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)

    result = runner.invoke(cli, ["repo", "public-mirror", "requests"])

    assert result.exit_code == 0, result.output
    assert captured["path"] == "/orgs/org-default/public-mirrors/requests"
    assert REQUEST_ID in result.output
    assert "rust-lang/rust" in result.output


def test_view_fetches_org_scoped_request(runner, monkeypatch):
    captured = {}

    def mock_get(self, path, params=None):
        captured["path"] = path
        return SAMPLE_REQUEST.copy()

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)

    result = runner.invoke(cli, ["repo", "public-mirror", "view", REQUEST_ID])

    assert result.exit_code == 0, result.output
    assert captured["path"] == f"/orgs/org-default/public-mirrors/requests/{REQUEST_ID}"
    assert "Build dependency" in result.output
    assert "rust-lang/rust" in result.output


def test_cancel_confirms_then_deletes(runner, monkeypatch):
    captured = {}
    cancelled = {**SAMPLE_REQUEST, "status": "CANCELLED"}

    def mock_delete(self, path, params=None):
        captured["path"] = path
        return cancelled

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)

    result = runner.invoke(cli, ["repo", "public-mirror", "cancel", REQUEST_ID], input="y\n")

    assert result.exit_code == 0, result.output
    assert captured["path"] == f"/orgs/org-default/public-mirrors/requests/{REQUEST_ID}"
    assert "Cancelled public-mirror request" in result.output


def test_cancel_abort_does_not_delete(runner, monkeypatch):
    called = False

    def mock_delete(self, path, params=None):
        nonlocal called
        called = True
        return SAMPLE_REQUEST.copy()

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)

    result = runner.invoke(cli, ["repo", "public-mirror", "cancel", REQUEST_ID], input="n\n")

    assert result.exit_code != 0
    assert not called


def test_cancel_yes_supports_json(runner, monkeypatch):
    cancelled = {**SAMPLE_REQUEST, "status": "CANCELLED"}
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_delete",
        lambda self, path, params=None: cancelled,
    )

    result = runner.invoke(
        cli,
        ["repo", "public-mirror", "cancel", REQUEST_ID, "--yes", "--json", "request_id,status"],
    )

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == {"request_id": REQUEST_ID, "status": "CANCELLED"}


def test_request_surfaces_api_validation_detail(runner, monkeypatch):
    def mock_post(self, path, json=None, timeout=None):
        request = httpx.Request("POST", "https://api.avrea.com" + path)
        response = httpx.Response(
            422,
            request=request,
            json={"detail": "Only public GitHub repositories can be requested"},
        )
        raise httpx.HTTPStatusError("422", request=request, response=response)

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)

    result = runner.invoke(cli, ["repo", "public-mirror", "request", "private/project"])

    assert result.exit_code == 1
    assert "Only public GitHub repositories can be requested" in result.stderr


def test_cancel_tolerates_empty_204_body(runner, monkeypatch):
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", lambda self, path, params=None: None)

    result = runner.invoke(cli, ["repo", "public-mirror", "cancel", REQUEST_ID, "--yes"])

    assert result.exit_code == 0, result.output
    assert f"Cancelled public-mirror request {REQUEST_ID}." in result.output


def test_cancel_json_tolerates_empty_204_body(runner, monkeypatch):
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", lambda self, path, params=None: None)

    result = runner.invoke(
        cli,
        ["repo", "public-mirror", "cancel", REQUEST_ID, "--yes", "--json", "request_id,status"],
    )

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == {"request_id": REQUEST_ID, "status": None}


def test_check_rejects_dot_segments_without_api_call(runner, monkeypatch):
    def unexpected_get(self, path, params=None):
        raise AssertionError("API should not be called")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", unexpected_get)

    # httpx collapses dot segments, so these would otherwise retarget the request.
    for name in ["../rust", "rust-lang/..", "./rust", "rust-lang/."]:
        result = runner.invoke(cli, ["repo", "public-mirror", "check", name])
        assert result.exit_code == 2, f"{name}: {result.output}"
        assert "owner/repository" in result.output


def test_json_field_discovery_does_not_call_api(runner, monkeypatch):
    def unexpected_get(self, path, params=None):
        raise AssertionError("API should not be called")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", unexpected_get)

    result = runner.invoke(cli, ["repo", "public-mirror", "check", "rust-lang/rust", "--json", "?"])

    assert result.exit_code == 0, result.output
    assert "repository_full_name" in result.output
    assert "mirror_enabled" in result.output
