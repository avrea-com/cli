"""Tests for organization email-domain claim and verification commands."""

from avrea_cli.main import cli
import httpx
import json

PENDING_DOMAIN = {
    "organization_email_domain_id": "oed-123",
    "organization_id": "org-default",
    "domain": "realdomain.com",
    "created_at": "2026-07-24T18:00:00Z",
    "verified": False,
    "verified_at": None,
    "dns_record_name": "_avrea-verification.realdomain.com",
    "dns_record_value": "avrea-verification=tok123",
}


def test_claim_domain_prints_dns_challenge(runner, monkeypatch):
    captured = {}

    def mock_post(self, path, json=None, timeout=None):
        captured["path"] = path
        captured["json"] = json
        return {"data": PENDING_DOMAIN}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)

    result = runner.invoke(cli, ["org", "email-domain", "claim", "realdomain.com"])

    assert result.exit_code == 0, result.output
    assert captured == {
        "path": "/orgs/org-default/email-domains/claim",
        "json": {"domain": "realdomain.com"},
    }
    assert "_avrea-verification.realdomain.com" in result.output
    assert "avrea-verification=tok123" in result.output
    assert "avr org email-domain verify realdomain.com" in result.output


def test_claim_domain_supports_json_output(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_post",
        lambda self, path, json=None, timeout=None: {"data": PENDING_DOMAIN},
    )

    result = runner.invoke(
        cli,
        ["org", "email-domain", "claim", "realdomain.com", "--json", "domain,verified,dns_record_value"],
    )

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == {
        "domain": "realdomain.com",
        "verified": False,
        "dns_record_value": "avrea-verification=tok123",
    }


def test_verify_domain_performs_recheck(runner, monkeypatch):
    captured = {}
    verified = {
        **PENDING_DOMAIN,
        "verified": True,
        "verified_at": "2026-07-24T18:05:00Z",
        "dns_record_name": None,
        "dns_record_value": None,
    }

    def mock_post(self, path, json=None, timeout=None):
        captured["path"] = path
        return {"data": verified}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)

    result = runner.invoke(cli, ["org", "email-domain", "verify", "realdomain.com"])

    assert result.exit_code == 0, result.output
    assert captured["path"] == "/orgs/org-default/email-domains/realdomain.com/verify"
    assert "Verified" in result.output
    assert "2026-07-24T18:05:00.00Z" in result.output


def test_verify_domain_can_be_retried_after_dns_miss(runner, monkeypatch):
    calls = 0

    def mock_post(self, path, json=None, timeout=None):
        nonlocal calls
        calls += 1
        if calls == 1:
            request = httpx.Request("POST", f"https://api.avrea.com{path}")
            response = httpx.Response(
                400,
                request=request,
                json={"detail": "DNS verification record not found. Publish the TXT record and try again."},
            )
            raise httpx.HTTPStatusError("400", request=request, response=response)
        return {"data": {**PENDING_DOMAIN, "verified": True, "verified_at": "2026-07-24T18:05:00Z"}}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)

    first = runner.invoke(cli, ["org", "email-domain", "verify", "realdomain.com"])
    second = runner.invoke(cli, ["org", "email-domain", "verify", "realdomain.com"])

    assert first.exit_code == 1
    assert "DNS verification record not found" in first.output
    assert second.exit_code == 0, second.output
    assert calls == 2


def test_list_shows_pending_dns_challenge(runner, monkeypatch):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {"data": [PENDING_DOMAIN]},
    )

    result = runner.invoke(cli, ["org", "email-domain", "list"])

    assert result.exit_code == 0, result.output
    assert "Pending" in result.output
    assert "_avrea-verification.realdomain.com TXT avrea-verification=tok123" in result.output
