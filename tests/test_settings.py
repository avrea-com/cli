"""Unit tests for CLI settings commands."""

from avrea_cli.main import cli
import json

SAMPLE_SETTINGS = [
    {"key": "cache.gha.enabled", "value": True, "source": "default"},
    {"key": "cache.bazel-remote.enabled", "value": False, "source": "organization"},
    {"key": "cache.turbo-cache.enabled", "value": True, "source": "default"},
    {"key": "cache.rclone-webdav.enabled", "value": True, "source": "default"},
    {"key": "cache.packages.enabled", "value": True, "source": "default"},
]

SAMPLE_SCHEMA = [
    {
        "key": "cache.gha.enabled",
        "value_type": "boolean",
        "default": True,
        "scopes": ["repository", "organization"],
        "inherits": True,
        "description": "Enable GitHub Actions cache proxy",
    },
    {
        "key": "cache.packages.enabled",
        "value_type": "boolean",
        "default": True,
        "scopes": ["repository", "organization"],
        "inherits": True,
        "description": "Enable package manager caching",
    },
]


class TestSettingsList:
    def test_org_level_table(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_SETTINGS,
        )
        result = runner.invoke(cli, ["settings", "list"])
        assert result.exit_code == 0
        assert "cache.gha.enabled" in result.output
        assert "yes" in result.output
        assert "no" in result.output

    def test_repo_level(self, runner, monkeypatch):
        captured = {}

        def mock_get(self, path, params=None):
            captured["path"] = path
            return SAMPLE_SETTINGS

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        result = runner.invoke(cli, ["settings", "list", "--repo", "rep-xyz"])
        assert result.exit_code == 0
        assert "/repos/rep-xyz/settings" in captured["path"]

    def test_prefix_filter(self, runner, monkeypatch):
        captured = {}

        def mock_get(self, path, params=None):
            captured["params"] = params
            return SAMPLE_SETTINGS

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        result = runner.invoke(cli, ["settings", "list", "--prefix", "cache."])
        assert result.exit_code == 0
        assert captured["params"]["prefix"] == "cache."

    def test_json_output(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_SETTINGS,
        )
        result = runner.invoke(cli, ["settings", "list", "--json", "*"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert len(data) == 5
        assert data[0]["key"] == "cache.gha.enabled"


class TestSettingsSet:
    def test_set_org_boolean(self, runner, monkeypatch):
        captured = {}

        def mock_put(self, path, json=None):
            assert json is not None
            captured["path"] = path
            captured["json"] = json
            return {"key": json["key"], "value": json["value"], "source": "organization"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "cache.gha.enabled", "false"])
        assert result.exit_code == 0
        assert captured["json"]["value"] is False
        assert "Set cache.gha.enabled = False" in result.output

    def test_set_repo_boolean(self, runner, monkeypatch):
        captured = {}

        def mock_put(self, path, json=None):
            assert json is not None
            captured["path"] = path
            return {"key": json["key"], "value": json["value"], "source": "repository"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "cache.gha.enabled", "true", "--repo", "rep-xyz"])
        assert result.exit_code == 0
        assert "/repos/rep-xyz/settings" in captured["path"]
        assert "repo" in result.output

    def test_set_true_variants(self, runner, monkeypatch):
        values = []

        def mock_put(self, path, json=None):
            assert json is not None
            values.append(json["value"])
            return {"key": json["key"], "value": json["value"], "source": "organization"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        for v in ("true", "yes", "on", "True", "YES"):
            runner.invoke(cli, ["settings", "set", "cache.gha.enabled", v])
        assert all(v is True for v in values)

    def test_set_false_variants(self, runner, monkeypatch):
        values = []

        def mock_put(self, path, json=None):
            assert json is not None
            values.append(json["value"])
            return {"key": json["key"], "value": json["value"], "source": "organization"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        for v in ("false", "no", "off", "False", "NO"):
            runner.invoke(cli, ["settings", "set", "cache.gha.enabled", v])
        assert all(v is False for v in values)

    def test_set_integer(self, runner, monkeypatch):
        captured = {}

        def mock_put(self, path, json=None):
            assert json is not None
            captured["json"] = json
            return {"key": json["key"], "value": json["value"], "source": "organization"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "some.int.setting", "42"])
        assert result.exit_code == 0
        assert captured["json"]["value"] == 42

    def test_set_string(self, runner, monkeypatch):
        captured = {}

        def mock_put(self, path, json=None):
            assert json is not None
            captured["json"] = json
            return {"key": json["key"], "value": json["value"], "source": "organization"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "some.str.setting", "hello"])
        assert result.exit_code == 0
        assert captured["json"]["value"] == "hello"


class TestSettingsReset:
    def test_reset_org(self, runner, monkeypatch):
        captured = {}

        def mock_delete(self, path, params=None):
            captured["path"] = path
            return None

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
        result = runner.invoke(cli, ["settings", "reset", "cache.gha.enabled"])
        assert result.exit_code == 0
        assert "cache.gha.enabled" in captured["path"]
        assert "Reset" in result.output

    def test_reset_repo(self, runner, monkeypatch):
        captured = {}

        def mock_delete(self, path, params=None):
            captured["path"] = path
            return None

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
        result = runner.invoke(cli, ["settings", "reset", "cache.gha.enabled", "--repo", "rep-xyz"])
        assert result.exit_code == 0
        assert "/repos/rep-xyz/settings/" in captured["path"]

    def test_reset_passes_key_in_path(self, runner, monkeypatch):
        captured = {}

        def mock_delete(self, path, params=None):
            captured["path"] = path
            return None

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
        runner.invoke(cli, ["settings", "reset", "cache.gha.enabled"])
        assert captured["path"].endswith("/settings/cache.gha.enabled")


class TestSettingsSchema:
    def test_table_output(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_SCHEMA,
        )
        result = runner.invoke(cli, ["settings", "schema"])
        assert result.exit_code == 0
        assert "cache.gha.enabled" in result.output
        assert "boolean" in result.output

    def test_json_output(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_SCHEMA,
        )
        result = runner.invoke(cli, ["settings", "schema", "--json", "*"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert len(data) == 2

    def test_prefix_and_scope_filters(self, runner, monkeypatch):
        captured = {}

        def mock_get(self, path, params=None):
            captured["params"] = params
            return SAMPLE_SCHEMA

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        result = runner.invoke(cli, ["settings", "schema", "--prefix", "cache.", "--scope", "repository"])
        assert result.exit_code == 0
        assert captured["params"]["prefix"] == "cache."
        assert captured["params"]["scope"] == "repository"


class TestSettingsAuth:
    def test_list_requires_auth(self, runner, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        result = runner.invoke(cli, ["settings", "list"])
        assert result.exit_code != 0
        assert "avr auth login" in result.stderr

    def test_set_requires_auth(self, runner, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        result = runner.invoke(cli, ["settings", "set", "cache.gha.enabled", "false"])
        assert result.exit_code != 0
        assert "avr auth login" in result.stderr

    def test_reset_requires_auth(self, runner, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        result = runner.invoke(cli, ["settings", "reset", "cache.gha.enabled"])
        assert result.exit_code != 0
        assert "avr auth login" in result.stderr
