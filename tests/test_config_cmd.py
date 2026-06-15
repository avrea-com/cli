"""Unit tests for `avr config` commands."""

from avrea_cli.main import cli

SAMPLE_ORGS = {
    "data": [
        {"organization_id": "org-a", "name": "Alpha Inc", "slug": "alpha"},
        {"organization_id": "org-b", "name": "Beta LLC", "slug": "beta"},
    ]
}


class TestConfigSetOrg:
    def test_set_by_slug_stores_resolved_id(self, runner, monkeypatch):
        stored = {}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_ORGS,
        )
        monkeypatch.setattr(
            "avrea_cli.auth.store_default_org",
            lambda org_id, *, host: stored.update(org_id=org_id, host=host),
        )
        result = runner.invoke(cli, ["config", "set", "org", "beta"])
        assert result.exit_code == 0
        assert stored["org_id"] == "org-b"
        assert "Beta LLC (org-b)" in result.output

    def test_set_by_id_stores_id(self, runner, monkeypatch):
        stored = {}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_ORGS,
        )
        monkeypatch.setattr(
            "avrea_cli.auth.store_default_org",
            lambda org_id, *, host: stored.update(org_id=org_id),
        )
        result = runner.invoke(cli, ["config", "set", "org", "org-a"])
        assert result.exit_code == 0
        assert stored["org_id"] == "org-a"

    def test_set_unknown_aborts_without_storing(self, runner, monkeypatch):
        stored = {}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_ORGS,
        )
        monkeypatch.setattr(
            "avrea_cli.auth.store_default_org",
            lambda org_id, *, host: stored.update(org_id=org_id),
        )
        result = runner.invoke(cli, ["config", "set", "org", "gamma"])
        assert result.exit_code != 0
        assert not stored
        assert "not found" in result.output
