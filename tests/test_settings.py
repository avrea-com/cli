"""Unit tests for CLI settings commands."""

from avrea_cli.main import cli
import httpx
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

    def test_list_autodetects_repo_when_no_org(self, runner, monkeypatch):
        """Like gh/glab: inside a checkout, a bare `list` auto-detects the repo
        and shows its effective values."""
        captured = {}

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/web")

        def mock_get(self, path, params=None):
            captured["path"] = path
            if path.endswith("/repos/resolve"):
                return {"data": {"repository_id": "rep-detected", "full_name": "acme/web"}}
            return SAMPLE_SETTINGS

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        result = runner.invoke(cli, ["settings", "list"])
        assert result.exit_code == 0
        assert captured["path"] == "/orgs/org-default/repos/rep-detected/settings"

    def test_list_explicit_org_ignores_git(self, runner, monkeypatch):
        """An explicit --org selects org scope and suppresses git auto-detection,
        even inside a checkout."""
        captured = {}

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/web")

        def mock_get(self, path, params=None):
            captured["path"] = path
            return SAMPLE_SETTINGS

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        result = runner.invoke(cli, ["settings", "list", "--org", "org-explicit"])
        assert result.exit_code == 0
        assert captured["path"] == "/orgs/org-explicit/settings"
        assert "/repos/" not in captured["path"]

    def test_list_falls_back_to_org_when_detected_repo_not_in_org(self, runner, monkeypatch):
        """Reads stay forgiving (unlike writes): when the auto-detected repo
        isn't connected, `list` shows org-wide results instead of erroring."""
        captured = {}

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/web")

        def mock_get(self, path, params=None):
            if path.endswith("/repos/resolve"):
                request = httpx.Request("GET", "https://api.avrea.com" + path)
                response = httpx.Response(404, request=request, json={"detail": "not found"})
                raise httpx.HTTPStatusError("404", request=request, response=response)
            captured["path"] = path
            return SAMPLE_SETTINGS

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        result = runner.invoke(cli, ["settings", "list"])
        assert result.exit_code == 0
        assert captured["path"] == "/orgs/org-default/settings"

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
        result = runner.invoke(cli, ["settings", "set", "cache.gha.enabled", "false", "--org", "org-default"])
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
            runner.invoke(cli, ["settings", "set", "cache.gha.enabled", v, "--org", "org-default"])
        assert all(v is True for v in values)

    def test_set_false_variants(self, runner, monkeypatch):
        values = []

        def mock_put(self, path, json=None):
            assert json is not None
            values.append(json["value"])
            return {"key": json["key"], "value": json["value"], "source": "organization"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        for v in ("false", "no", "off", "False", "NO"):
            runner.invoke(cli, ["settings", "set", "cache.gha.enabled", v, "--org", "org-default"])
        assert all(v is False for v in values)

    def test_set_integer(self, runner, monkeypatch):
        captured = {}

        def mock_put(self, path, json=None):
            assert json is not None
            captured["json"] = json
            return {"key": json["key"], "value": json["value"], "source": "organization"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "some.int.setting", "42", "--org", "org-default"])
        assert result.exit_code == 0
        assert captured["json"]["value"] == 42

    def test_set_string(self, runner, monkeypatch):
        captured = {}

        def mock_put(self, path, json=None):
            assert json is not None
            captured["json"] = json
            return {"key": json["key"], "value": json["value"], "source": "organization"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "some.str.setting", "hello", "--org", "org-default"])
        assert result.exit_code == 0
        assert captured["json"]["value"] == "hello"

    def test_set_explicit_org_ignores_git(self, runner, monkeypatch):
        """`set --org` targets org scope and ignores the git remote, even inside
        a checkout. Regression for org-only settings (e.g. some.org-scoped.setting)
        that 422'd because --org didn't suppress repo auto-detection."""
        captured = {}

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/web")

        def mock_put(self, path, json=None):
            assert json is not None
            captured["path"] = path
            return {"key": json["key"], "value": json["value"], "source": "organization"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "some.org-scoped.setting", "true", "--org", "org-explicit"])
        assert result.exit_code == 0
        assert captured["path"] == "/orgs/org-explicit/settings"
        assert "/repos/" not in captured["path"]
        assert "(org)" in result.output

    def test_set_autodetects_repo_when_no_org(self, runner, monkeypatch):
        """Like gh/glab: a bare `set` inside a checkout writes a repo override."""
        captured = {}

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/web")

        def mock_get(self, path, params=None):
            return {"data": {"repository_id": "rep-detected", "full_name": "acme/web"}}

        def mock_put(self, path, json=None):
            assert json is not None
            captured["path"] = path
            return {"key": json["key"], "value": json["value"], "source": "repository"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "cache.gha.enabled", "true"])
        assert result.exit_code == 0
        assert captured["path"] == "/orgs/org-default/repos/rep-detected/settings"
        assert "(repo)" in result.output

    def test_set_404_does_not_show_scope_hint(self, runner, monkeypatch):
        """The org-only-scope hint is a 422 concern — it must not leak onto other
        errors such as a 404 on a repo-scoped write."""

        def mock_put(self, path, json=None):
            request = httpx.Request("PUT", "https://api.avrea.com" + path)
            response = httpx.Response(404, request=request, json={"detail": "nope"})
            raise httpx.HTTPStatusError("404", request=request, response=response)

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "cache.gha.enabled", "true", "--repo", "rep-xyz"])
        assert result.exit_code != 0
        assert "org scope" not in result.stderr

    def test_set_422_explicit_repo_hint(self, runner, monkeypatch):
        """An org-only key with explicit --repo hints to drop --repo."""

        def mock_put(self, path, json=None):
            request = httpx.Request("PUT", "https://api.avrea.com" + path)
            response = httpx.Response(
                422,
                request=request,
                json={"detail": "Setting 'some.org-scoped.setting' is not allowed for scope 'repository'"},
            )
            raise httpx.HTTPStatusError("422", request=request, response=response)

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "some.org-scoped.setting", "true", "--repo", "rep-xyz"])
        assert result.exit_code != 0
        assert "HTTP 422" in result.stderr
        assert "--org" in result.stderr
        assert "drop --repo" in result.stderr

    def test_set_422_autodetected_repo_hint(self, runner, monkeypatch):
        """An org-only key hit via an auto-detected repo hints to use --org
        (the user never typed --repo, so 'drop --repo' would be wrong)."""
        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/web")

        def mock_get(self, path, params=None):
            return {"data": {"repository_id": "rep-detected", "full_name": "acme/web"}}

        def mock_put(self, path, json=None):
            request = httpx.Request("PUT", "https://api.avrea.com" + path)
            response = httpx.Response(
                422,
                request=request,
                json={"detail": "Setting 'some.org-scoped.setting' is not allowed for scope 'repository'"},
            )
            raise httpx.HTTPStatusError("422", request=request, response=response)

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "some.org-scoped.setting", "true"])
        assert result.exit_code != 0
        assert "HTTP 422" in result.stderr
        assert "--org" in result.stderr
        assert "drop --repo" not in result.stderr

    def test_set_errors_when_detected_repo_not_in_org(self, runner, monkeypatch):
        """A bare write must not silently fall back to org scope when the
        auto-detected repo isn't connected to the org — it errors instead."""
        put_called = []

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/web")

        def mock_get(self, path, params=None):
            request = httpx.Request("GET", "https://api.avrea.com" + path)
            response = httpx.Response(404, request=request, json={"detail": "not found"})
            raise httpx.HTTPStatusError("404", request=request, response=response)

        def mock_put(self, path, json=None):
            put_called.append(path)
            return {}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "cache.gha.enabled", "true"])
        assert result.exit_code != 0
        assert not put_called  # never wrote anything

    def test_set_falls_back_to_org_outside_repo(self, runner, monkeypatch):
        """With a default org and no repo context, a bare write targets org scope
        (uses the default org) instead of erroring."""
        captured = {}

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: None)

        def mock_put(self, path, json=None):
            assert json is not None
            captured["path"] = path
            return {"key": json["key"], "value": json["value"], "source": "organization"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "cache.gha.enabled", "true"])
        assert result.exit_code == 0
        assert captured["path"] == "/orgs/org-default/settings"
        assert "(org)" in result.output

    def test_set_explicit_org_overrides_avr_repo(self, runner, monkeypatch):
        """An explicit --org targets org scope even when AVR_REPO pins a repo —
        an explicit flag outranks the ambient env default."""
        monkeypatch.setenv("AVR_REPO", "acme/web")
        captured = {}

        def mock_put(self, path, json=None):
            assert json is not None
            captured["path"] = path
            return {"key": json["key"], "value": json["value"], "source": "organization"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "cache.gha.enabled", "true", "--org", "org-explicit"])
        assert result.exit_code == 0
        assert captured["path"] == "/orgs/org-explicit/settings"
        assert "/repos/" not in captured["path"]

    def test_set_uses_avr_repo_when_no_org(self, runner, monkeypatch):
        """Without --org, AVR_REPO targets repo scope."""
        monkeypatch.setenv("AVR_REPO", "acme/web")
        captured = {}

        def mock_get(self, path, params=None):
            return {"data": {"repository_id": "rep-env", "full_name": "acme/web"}}

        def mock_put(self, path, json=None):
            assert json is not None
            captured["path"] = path
            return {"key": json["key"], "value": json["value"], "source": "repository"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_put", mock_put)
        result = runner.invoke(cli, ["settings", "set", "cache.gha.enabled", "true"])
        assert result.exit_code == 0
        assert captured["path"] == "/orgs/org-default/repos/rep-env/settings"


class TestSettingsReset:
    def test_reset_org(self, runner, monkeypatch):
        captured = {}

        def mock_delete(self, path, params=None):
            captured["path"] = path
            return None

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
        result = runner.invoke(cli, ["settings", "reset", "cache.gha.enabled", "--org", "org-default"])
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
        runner.invoke(cli, ["settings", "reset", "cache.gha.enabled", "--org", "org-default"])
        assert captured["path"].endswith("/settings/cache.gha.enabled")

    def test_reset_explicit_org_ignores_git(self, runner, monkeypatch):
        """`reset --org` targets org scope and ignores the git remote."""
        captured = {}

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/web")

        def mock_delete(self, path, params=None):
            captured["path"] = path
            return None

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
        result = runner.invoke(cli, ["settings", "reset", "some.org-scoped.setting", "--org", "org-explicit"])
        assert result.exit_code == 0
        assert captured["path"] == "/orgs/org-explicit/settings/some.org-scoped.setting"
        assert "/repos/" not in captured["path"]

    def test_reset_autodetects_repo_when_no_org(self, runner, monkeypatch):
        """A bare `reset` inside a checkout clears the repo override."""
        captured = {}

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/web")

        def mock_get(self, path, params=None):
            return {"data": {"repository_id": "rep-detected", "full_name": "acme/web"}}

        def mock_delete(self, path, params=None):
            captured["path"] = path
            return None

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
        result = runner.invoke(cli, ["settings", "reset", "cache.gha.enabled"])
        assert result.exit_code == 0
        assert captured["path"] == "/orgs/org-default/repos/rep-detected/settings/cache.gha.enabled"

    def test_reset_falls_back_to_org_outside_repo(self, runner, monkeypatch):
        """With a default org and no repo context, a bare `reset` targets org
        scope instead of erroring."""
        captured = {}

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: None)

        def mock_delete(self, path, params=None):
            captured["path"] = path
            return None

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
        result = runner.invoke(cli, ["settings", "reset", "cache.gha.enabled"])
        assert result.exit_code == 0
        assert captured["path"] == "/orgs/org-default/settings/cache.gha.enabled"

    def test_reset_errors_when_detected_repo_not_in_org(self, runner, monkeypatch):
        """A bare `reset` errors when the auto-detected repo isn't connected,
        rather than clearing the org-wide value by surprise."""
        delete_called = []

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/web")

        def mock_get(self, path, params=None):
            request = httpx.Request("GET", "https://api.avrea.com" + path)
            response = httpx.Response(404, request=request, json={"detail": "not found"})
            raise httpx.HTTPStatusError("404", request=request, response=response)

        def mock_delete(self, path, params=None):
            delete_called.append(path)
            return None

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
        result = runner.invoke(cli, ["settings", "reset", "cache.gha.enabled"])
        assert result.exit_code != 0
        assert not delete_called


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
