"""Tests for the unified time-window flags on `avr run list`."""

from avrea_cli.main import cli

SAMPLE_EMPTY = {"data": [], "pagination": {"next_cursor": None}}


class TestSinceFlag:
    def test_since_resolves_to_created_after(self, runner, monkeypatch):
        captured = {}

        def fake_get(self, path, params=None, **kw):
            captured["params"] = params or {}
            return SAMPLE_EMPTY

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)
        result = runner.invoke(cli, ["run", "list", "--since", "7d"])
        assert result.exit_code == 0, result.output
        assert "created_after" in captured["params"]

    def test_since_combined_with_created_after_errors(self, runner, monkeypatch):
        """Hard-error rather than last-wins — accidental double-spec is usually a mistake."""
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_EMPTY,
        )
        result = runner.invoke(
            cli,
            ["run", "list", "--since", "7d", "--created-after", "2026-01-01T00:00:00Z"],
        )
        assert result.exit_code != 0
        assert "cannot be combined" in result.output

    def test_since_combined_with_created_before_errors(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_EMPTY,
        )
        result = runner.invoke(
            cli,
            ["run", "list", "--since", "7d", "--created-before", "2026-01-01T00:00:00Z"],
        )
        assert result.exit_code != 0
        assert "cannot be combined" in result.output


class TestLimitShortFlag:
    def test_minus_L_alias_for_limit(self, runner, monkeypatch):
        captured = {}

        def fake_get(self, path, params=None, **kw):
            captured["params"] = params or {}
            return SAMPLE_EMPTY

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)
        result = runner.invoke(cli, ["run", "list", "-L", "5"])
        assert result.exit_code == 0, result.output
        assert captured["params"]["limit"] == 5
