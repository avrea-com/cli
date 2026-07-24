"""Tests for organization-admin SAML configuration commands."""

from avrea_cli.main import cli
import json

SAML_CONFIG = {
    "organization_saml_config_id": "osc-123",
    "organization_id": "org-default",
    "idp_entity_id": "https://idp.example.com/metadata",
    "idp_sso_url": "https://idp.example.com/sso",
    "idp_slo_url": "https://idp.example.com/slo",
    "name_id_format": "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress",
    "attr_email": "mail",
    "attr_given_name": "firstName",
    "attr_family_name": "lastName",
    "attr_groups": None,
    "is_enforced": False,
    "jit_provisioning": True,
    "allow_idp_initiated": False,
    "default_role": "user",
    "created_at": "2026-07-24T18:00:00Z",
    "updated_at": "2026-07-24T18:05:00Z",
}


def test_configure_saml_uploads_raw_metadata_and_options(runner, monkeypatch, tmp_path):
    metadata_path = tmp_path / "idp.xml"
    metadata_path.write_bytes(b"<EntityDescriptor>idp</EntityDescriptor>")
    captured = {}

    def mock_post(
        self,
        path,
        json=None,
        timeout=None,
        *,
        content=None,
        params=None,
        content_type=None,
    ):
        captured.update(
            path=path,
            json=json,
            content=content,
            params=params,
            content_type=content_type,
        )
        return {"data": SAML_CONFIG}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)

    result = runner.invoke(
        cli,
        [
            "org",
            "saml",
            "configure",
            str(metadata_path),
            "--email-attribute",
            "mail",
            "--given-name-attribute",
            "firstName",
            "--family-name-attribute",
            "lastName",
        ],
    )

    assert result.exit_code == 0, result.output
    assert captured["path"] == "/orgs/org-default/saml"
    assert captured["json"] is None
    assert captured["content"] == b"<EntityDescriptor>idp</EntityDescriptor>"
    assert captured["content_type"] == "application/xml"
    assert captured["params"] == {
        "attr_email": "mail",
        "attr_given_name": "firstName",
        "attr_family_name": "lastName",
        "default_role": "user",
        "jit_provisioning": True,
        "allow_idp_initiated": False,
    }
    assert "https://idp.example.com/metadata" in result.output
    assert "JIT provisioning" in result.output


def test_configure_saml_reads_metadata_from_stdin(runner, monkeypatch):
    captured = {}

    def mock_post(
        self,
        path,
        json=None,
        timeout=None,
        *,
        content=None,
        params=None,
        content_type=None,
    ):
        captured["content"] = content
        return {"data": SAML_CONFIG}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)

    result = runner.invoke(
        cli,
        ["org", "saml", "configure", "-"],
        input="<EntityDescriptor>stdin</EntityDescriptor>",
    )

    assert result.exit_code == 0, result.output
    assert captured["content"] == b"<EntityDescriptor>stdin</EntityDescriptor>"


def test_show_saml_supports_json(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {"data": SAML_CONFIG},
    )

    result = runner.invoke(cli, ["org", "saml", "show", "--json", "idp_entity_id,is_enforced"])

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == {
        "idp_entity_id": "https://idp.example.com/metadata",
        "is_enforced": False,
    }


def test_saml_enforcement_on(runner, monkeypatch):
    captured = {}

    def mock_post(self, path, json=None, timeout=None, **kwargs):
        captured["path"] = path
        captured["json"] = json
        return {"data": {**SAML_CONFIG, "is_enforced": True}}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)

    result = runner.invoke(cli, ["org", "saml", "enforcement", "on"])

    assert result.exit_code == 0, result.output
    assert captured == {
        "path": "/orgs/org-default/saml/enforcement",
        "json": {"enforce": True},
    }
    assert "enabled" in result.output


def test_saml_remove_with_yes(runner, monkeypatch):
    captured = {}

    def mock_delete(self, path, params=None):
        captured["path"] = path
        return {"data": {"deleted": True}}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)

    result = runner.invoke(cli, ["org", "saml", "remove", "--yes"])

    assert result.exit_code == 0, result.output
    assert captured["path"] == "/orgs/org-default/saml"
    assert "Removed SAML configuration" in result.output


def test_saml_sp_metadata_prints_xml(runner, monkeypatch):
    def mock_get(self, path, params=None):
        assert path == "/users/me/organizations"
        return {
            "data": [
                {
                    "organization_id": "org-default",
                    "slug": "acme",
                }
            ]
        }

    captured = {}

    def mock_get_text(self, path, params=None):
        captured["path"] = path
        return "<EntityDescriptor>sp</EntityDescriptor>"

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get_text", mock_get_text)

    result = runner.invoke(cli, ["org", "saml", "sp-metadata"])

    assert result.exit_code == 0, result.output
    assert captured["path"] == "/saml/acme/metadata"
    assert result.output == "<EntityDescriptor>sp</EntityDescriptor>\n"


def test_saml_sp_metadata_rejects_unresolved_slug(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {"data": []},
    )

    def unexpected_metadata_request(self, path, params=None):
        raise AssertionError(f"metadata endpoint must not receive an unresolved slug: {path}")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get_text", unexpected_metadata_request)

    result = runner.invoke(cli, ["org", "saml", "sp-metadata"])

    assert result.exit_code == 1
    assert "Could not resolve organization slug for org-default" in result.output


def test_saml_test_prints_browser_url_without_opening(runner, monkeypatch):
    def mock_get(self, path, params=None):
        assert path == "/users/me/organizations"
        return {
            "data": [
                {
                    "organization_id": "org-default",
                    "slug": "acme",
                }
            ]
        }

    def unexpected_browser_open(url):
        raise AssertionError(f"browser should not open for --no-browser: {url}")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
    monkeypatch.setattr("avrea_cli.commands.org.webbrowser.open", unexpected_browser_open)

    result = runner.invoke(cli, ["org", "saml", "test", "--no-browser"])

    assert result.exit_code == 0, result.output
    assert result.output == "https://api.avrea.com/saml/acme/test/login\n"


def test_saml_test_rejects_unresolved_slug(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {"data": []},
    )

    def unexpected_browser_open(url):
        raise AssertionError(f"browser must not receive an unresolved slug: {url}")

    monkeypatch.setattr("avrea_cli.commands.org.webbrowser.open", unexpected_browser_open)

    result = runner.invoke(cli, ["org", "saml", "test"])

    assert result.exit_code == 1
    assert "Could not resolve organization slug for org-default" in result.output
    assert "/saml/org-default/" not in result.output
