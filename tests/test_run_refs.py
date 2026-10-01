"""Run-reference parsing and resolution tests."""

from avrea_cli.run_refs import RunReferenceKind
from avrea_cli.run_refs import parse_run_reference
from avrea_cli.run_refs import resolve_run_reference
from unittest.mock import MagicMock
import click
import pytest


@pytest.mark.parametrize(
    ("value", "api_url", "kind", "run_id", "platform_run_id", "attempt"),
    [
        ("run-abc123", "https://api.avrea.com", RunReferenceKind.AVREA_ID, "run-abc123", None, None),
        ("123456789", "https://api.avrea.com", RunReferenceKind.GITHUB_ID, None, 123456789, None),
        (
            "https://github.com/acme/widgets/actions/runs/123456789",
            "https://api.avrea.com",
            RunReferenceKind.GITHUB_URL,
            None,
            123456789,
            None,
        ),
        (
            "https://github.com/acme/widgets/actions/runs/123456789/?any=url&paremerts=true",
            "https://api.avrea.com",
            RunReferenceKind.GITHUB_URL,
            None,
            123456789,
            None,
        ),
        (
            "https://github.com/acme/widgets/actions/runs/123456789/attempts/2",
            "https://api.avrea.com",
            RunReferenceKind.GITHUB_URL,
            None,
            123456789,
            2,
        ),
        (
            "https://console.avrea.com/org/acme/runs/run-abc123",
            "https://api.avrea.com",
            RunReferenceKind.AVREA_URL,
            "run-abc123",
            None,
            None,
        ),
        (
            "https://console.avrea.com/org/acme/runs/run-abc123/?any=url&paremerts=true",
            "https://api.avrea.com",
            RunReferenceKind.AVREA_URL,
            "run-abc123",
            None,
            None,
        ),
        (
            "https://console.test.example.com/org/acme/runs/run-def456",
            "https://api.test.example.com",
            RunReferenceKind.AVREA_URL,
            "run-def456",
            None,
            None,
        ),
    ],
)
def test_parse_supported_references(value, api_url, kind, run_id, platform_run_id, attempt):
    parsed = parse_run_reference(value, api_url=api_url)

    assert parsed.kind is kind
    assert parsed.run_id == run_id
    assert parsed.platform_run_id == platform_run_id
    assert parsed.attempt == attempt


@pytest.mark.parametrize(
    "value",
    [
        "0",
        "١٢٣",
        "9" * 5000,
        "https://[",
        "https://github.com/acme/widgets/actions/runs/123\n",
        "https://github.example.com/acme/widgets/actions/runs/123",
        "http://github.com/acme/widgets/actions/runs/123",
        "https://user@github.com/acme/widgets/actions/runs/123",
        "https://github.com:443/acme/widgets/actions/runs/123",
        "https://github.com/acme/widgets/actions/runs/123/attempts/0",
        "https://github.com/acme/widgets/actions/runs/123/extra",
        "https://pr-12.console.test.example.com/org/acme/runs/run-abc123",
        "https://console.local.test.example.com/org/acme/runs/run-abc123",
        "https://console.avrea.com/org/acme/jobs/job-abc123",
    ],
)
def test_parse_rejects_malformed_or_untrusted_references(value):
    with pytest.raises(click.ClickException):
        parse_run_reference(value, api_url="https://api.avrea.com")


def test_avrea_url_must_match_active_environment():
    with pytest.raises(click.ClickException, match=r"avr auth switch https://api\.avrea\.com"):
        parse_run_reference(
            "https://console.avrea.com/org/acme/runs/run-abc123",
            api_url="https://api.test.example.com",
        )


def test_resolve_numeric_id_uses_direct_platform_endpoint():
    client = MagicMock()
    client.public_get.return_value = {"data": {"run_id": "run-new", "run_attempt": 3}}
    reference = parse_run_reference("123456789", api_url="https://api.avrea.com")

    result = resolve_run_reference(client, "org-1", reference, include=["jobs", "workflow"])

    assert result["run_id"] == "run-new"
    client.public_get.assert_called_once_with(
        "/orgs/org-1/workflow-runs/by-platform-id/123456789",
        params={"include": ["jobs", "workflow"]},
    )


def test_resolve_exact_attempt_uses_bounded_platform_filter():
    client = MagicMock()
    client.public_get.return_value = {
        "data": [
            {
                "run_id": "run-new",
                "run_attempt": 3,
                "repository": {"full_name": "acme/widgets"},
            },
            {
                "run_id": "run-wanted",
                "run_attempt": 2,
                "repository": {"full_name": "acme/widgets"},
            },
        ]
    }
    reference = parse_run_reference(
        "https://github.com/acme/widgets/actions/runs/123456789/attempts/2",
        api_url="https://api.avrea.com",
    )

    result = resolve_run_reference(client, "org-1", reference, include=["jobs"])

    assert result["run_id"] == "run-wanted"
    client.public_get.assert_called_once_with(
        "/orgs/org-1/workflow-runs",
        params={
            "platform_run_id": 123456789,
            "limit": 1000,
            "order": "created_at.desc",
            "include": ["jobs"],
        },
    )


def test_resolve_github_url_rejects_repository_mismatch():
    client = MagicMock()
    client.public_get.return_value = {"data": {"run_id": "run-abc123", "repository": {"full_name": "other/widgets"}}}
    reference = parse_run_reference(
        "https://github.com/acme/widgets/actions/runs/123456789",
        api_url="https://api.avrea.com",
    )

    with pytest.raises(click.ClickException, match="does not match"):
        resolve_run_reference(client, "org-1", reference)


def test_avrea_url_exposes_org_slug_for_command_context_validation():
    parsed = parse_run_reference(
        "https://console.avrea.com/org/acme/runs/run-abc123",
        api_url="https://api.avrea.com",
    )

    assert parsed.organization_slug == "acme"
