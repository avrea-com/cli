"""Unit tests for CLI cache management commands."""

from avrea_cli.main import cli
import json

SAMPLE_ENTRIES = [
    {
        "cache_type": "gha",
        "key": "node_modules-abc",
        "ref": "refs/heads/main",
        "version": "v1",
        "size_bytes": 1048576,
        "created_at": "2025-06-01T12:00:00Z",
        "last_accessed_at": None,
        "hit_count": 0,
    },
    {
        "cache_type": "bazel",
        "key": "bazel-cache-key",
        "ref": None,
        "version": None,
        "size_bytes": 524288000,
        "created_at": "2025-06-02T10:30:00Z",
        "last_accessed_at": "2025-06-03T08:00:00Z",
        "hit_count": 7,
    },
]


SAMPLE_USAGE = {
    "data": {
        "total_size_bytes": 525336576,
        "quota_bytes": 26843545600,
        "over_quota": False,
        "by_type": [
            {"cache_type": "gha", "size_bytes": 1048576, "entry_count": 1},
            {"cache_type": "bazel", "size_bytes": 524288000, "entry_count": 1},
        ],
        "last_crawled_at": None,
    }
}


class TestCacheList:
    def test_table_output(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": SAMPLE_ENTRIES, "total": 2},
        )
        result = runner.invoke(cli, ["cache", "list", "--repo", "rep-123"])
        assert result.exit_code == 0
        assert "node_modules-abc" in result.output
        assert "bazel-cache-key" in result.output
        assert "gha" in result.output
        assert "bazel" in result.output

    def test_json_output(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": SAMPLE_ENTRIES, "total": 2},
        )
        result = runner.invoke(cli, ["cache", "list", "--repo", "rep-123", "--json", "key,size_bytes,cache_type"])
        assert result.exit_code == 0, result.output
        data = json.loads(result.output)
        assert len(data) == 2
        assert set(data[0].keys()) == {"key", "size_bytes", "cache_type"}

    def test_type_filter_passed(self, runner, monkeypatch):
        captured_params = {}

        def mock_get(self, path, params=None):
            captured_params.update(params or {})
            return {"data": [], "total": 0}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        result = runner.invoke(cli, ["cache", "list", "--repo", "rep-123", "--type", "gha"])
        assert result.exit_code == 0
        assert captured_params.get("cache_type") == "gha"

    def test_pagination_offset_displayed(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": SAMPLE_ENTRIES, "total": 50},
        )
        result = runner.invoke(cli, ["cache", "list", "--repo", "rep-123"])
        assert result.exit_code == 0
        assert "--offset 2" in result.stderr

    def test_empty_list(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": [], "total": 0},
        )
        result = runner.invoke(cli, ["cache", "list", "--repo", "rep-123"])
        assert result.exit_code == 0

    def test_key_and_ref_filters_passed(self, runner, monkeypatch):
        captured_params = {}

        def mock_get(self, path, params=None):
            captured_params.update(params or {})
            return {"data": [], "total": 0}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        result = runner.invoke(
            cli, ["cache", "list", "--repo", "rep-123", "--key", "node_modules", "--ref", "refs/heads/main"]
        )
        assert result.exit_code == 0
        assert captured_params.get("key") == "node_modules"
        assert captured_params.get("ref") == "refs/heads/main"


class TestCacheUsage:
    def test_table_output(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_USAGE,
        )
        result = runner.invoke(cli, ["cache", "usage", "--repo", "rep-123"])
        assert result.exit_code == 0
        assert "Quota" in result.output
        assert "no" in result.output  # over_quota = no
        assert "gha" in result.output
        assert "bazel" in result.output

    def test_json_output(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_USAGE,
        )
        result = runner.invoke(cli, ["cache", "usage", "--repo", "rep-123", "--json", "*"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        # Modern --json contract: schema-projected, top-level snake_case keys
        # for the wire names. `--json '*'` returns every defined field.
        assert data["total_size_bytes"] == 525336576

    def test_over_quota(self, runner, monkeypatch):
        over_quota_response = {
            "data": {
                "total_size_bytes": 30_000_000_000,
                "quota_bytes": 26_843_545_600,
                "over_quota": True,
                "by_type": [{"cache_type": "gha", "size_bytes": 30_000_000_000, "entry_count": 500}],
                "last_crawled_at": None,
            }
        }
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: over_quota_response,
        )
        result = runner.invoke(cli, ["cache", "usage", "--repo", "rep-123"])
        assert result.exit_code == 0
        assert "yes" in result.output  # over_quota = yes


class TestCacheDelete:
    def test_delete_by_key_with_confirm(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete",
            lambda self, path, params=None: {"deleted_count": 3},
        )
        result = runner.invoke(
            cli, ["cache", "delete", "--repo", "rep-123", "--type", "gha", "--key", "node_modules"], input="y\n"
        )
        assert result.exit_code == 0
        assert "Deleted 3 cache entries" in result.output

    def test_delete_by_key_skip_confirm(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete",
            lambda self, path, params=None: {"deleted_count": 3},
        )
        result = runner.invoke(
            cli, ["cache", "delete", "--repo", "rep-123", "--type", "gha", "--key", "node_modules", "--yes"]
        )
        assert result.exit_code == 0
        assert "Deleted 3 cache entries" in result.output

    def test_delete_by_key_singular(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete",
            lambda self, path, params=None: {"deleted_count": 1},
        )
        result = runner.invoke(
            cli, ["cache", "delete", "--repo", "rep-123", "--type", "gha", "--key", "exact-key", "--yes"]
        )
        assert result.exit_code == 0
        assert "Deleted 1 cache entry." in result.output

    def test_delete_by_key_requires_type(self, runner):
        """--key without --type should error."""
        result = runner.invoke(cli, ["cache", "delete", "--repo", "rep-123", "--key", "node_modules", "--yes"])
        assert result.exit_code != 0
        assert "--type" in result.output or "--type" in result.stderr

    def test_delete_all_with_confirm(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete",
            lambda self, path, params=None: {"deleted_count": 15},
        )
        result = runner.invoke(cli, ["cache", "delete", "--repo", "rep-123", "--all"], input="y\n")
        assert result.exit_code == 0
        assert "Purged 15 cache entries" in result.output

    def test_error_both_key_and_all(self, runner):
        result = runner.invoke(
            cli, ["cache", "delete", "--repo", "rep-123", "--type", "gha", "--key", "prefix", "--all"]
        )
        assert result.exit_code != 0

    def test_error_neither_key_nor_all(self, runner):
        result = runner.invoke(cli, ["cache", "delete", "--repo", "rep-123"])
        assert result.exit_code != 0
        combined = result.output + result.stderr
        assert "--key" in combined or "--all" in combined

    def test_delete_not_found(self, runner, monkeypatch):
        import httpx

        def mock_delete(self, path, params=None):
            request = httpx.Request("DELETE", path)
            response = httpx.Response(404, json={"detail": "Cache entries not found"}, request=request)
            raise httpx.HTTPStatusError("Not Found", request=request, response=response)

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
        result = runner.invoke(
            cli, ["cache", "delete", "--repo", "rep-123", "--type", "gha", "--key", "missing", "--yes"]
        )
        assert result.exit_code != 0
        assert "not found" in result.stderr.lower() or "not found" in result.output.lower()

    def test_delete_by_key_passes_ref(self, runner, monkeypatch):
        """--ref is forwarded as a query param to the API."""
        captured_params = {}

        def mock_delete(self, path, params=None):
            captured_params.update(params or {})
            return {"deleted_count": 1}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
        result = runner.invoke(
            cli,
            [
                "cache",
                "delete",
                "--repo",
                "rep-123",
                "--type",
                "gha",
                "--key",
                "node_modules",
                "--ref",
                "refs/heads/main",
                "--yes",
            ],
        )
        assert result.exit_code == 0
        assert captured_params.get("ref") == "refs/heads/main"
        assert captured_params.get("cache_type") == "gha"
        assert captured_params.get("key") == "node_modules"

    def test_delete_confirm_abort(self, runner, monkeypatch):
        """User answers 'n' to confirmation prompt — no delete happens."""
        delete_called = False

        def mock_delete(self, path, params=None):
            nonlocal delete_called
            delete_called = True
            return {"deleted_count": 1}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
        result = runner.invoke(
            cli, ["cache", "delete", "--repo", "rep-123", "--type", "gha", "--key", "test-key"], input="n\n"
        )
        assert result.exit_code != 0
        assert not delete_called


class TestCacheAuth:
    def test_list_requires_auth(self, runner, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        result = runner.invoke(cli, ["cache", "list", "--repo", "rep-123"])
        assert result.exit_code != 0
        assert "avr auth login" in result.stderr

    def test_usage_requires_auth(self, runner, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        result = runner.invoke(cli, ["cache", "usage", "--repo", "rep-123"])
        assert result.exit_code != 0
        assert "avr auth login" in result.stderr

    def test_delete_requires_auth(self, runner, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        result = runner.invoke(cli, ["cache", "delete", "--repo", "rep-123", "--type", "gha", "--key", "k", "--yes"])
        assert result.exit_code != 0
        assert "avr auth login" in result.stderr
