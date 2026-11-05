"""Billing failures preserve the upstream rejection when details are unusable."""

from avrea_cli.main import cli
from click.testing import CliRunner
from pytest import MonkeyPatch
from unittest.mock import MagicMock
import httpx
import pytest


@pytest.mark.parametrize(
    ("body", "detail"),
    [
        (b'{"error":{"message":"Card declined"}}', " (Card declined)"),
        (b"not JSON", ""),
        (b"null", ""),
        (b"[]", ""),
        (b'{"error":null}', ""),
        (b'{"error":[]}', ""),
        (b"{}", ""),
    ],
)
def test_rejected_card_handles_missing_error_details(
    runner: CliRunner, monkeypatch: MonkeyPatch, body: bytes, detail: str
) -> None:
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        MagicMock(return_value={"data": {"stripe_key": "pk_test_example"}}),
    )
    post = MagicMock(return_value={"data": {"client_secret": "seti_example_secret_test"}})
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", post)
    monkeypatch.setattr(
        "avrea_cli.billing.httpx.post",
        MagicMock(
            return_value=httpx.Response(
                402,
                content=body,
                request=httpx.Request("POST", "https://api.stripe.com/v1/setup_intents/seti_example/confirm"),
            )
        ),
    )

    result = runner.invoke(
        cli,
        [
            "billing",
            "payment-methods",
            "add",
            "--number",
            "4242424242424242",
            "--exp-month",
            "12",
            "--exp-year",
            "2030",
            "--cvc",
            "123",
        ],
    )

    assert result.exit_code == 1
    assert f"Error: Stripe rejected the card details.{detail}" in result.output
    post.assert_called_once_with("/orgs/org-default/billing/setup-intent")
