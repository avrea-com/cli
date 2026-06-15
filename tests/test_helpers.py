"""Tests for shared CLI helper functions."""

from avrea_cli.config import CliConfig
from avrea_cli.helpers import format_size
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import match_org
from avrea_cli.helpers import parse_since
from datetime import UTC
from datetime import datetime
from datetime import timedelta
from unittest.mock import MagicMock
import click
import httpx
import pytest


class TestGetOrgId:
    def test_returns_explicit_option(self) -> None:
        config = MagicMock(spec=CliConfig)
        config.default_org = None
        assert get_org_id(config, "org-explicit") == "org-explicit"

    def test_returns_default_org(self) -> None:
        config = MagicMock(spec=CliConfig)
        config.default_org = "org-default"
        assert get_org_id(config, None) == "org-default"

    def test_option_overrides_default(self) -> None:
        config = MagicMock(spec=CliConfig)
        config.default_org = "org-default"
        assert get_org_id(config, "org-override") == "org-override"

    def test_auto_selects_single_org(self) -> None:
        config = MagicMock(spec=CliConfig)
        config.default_org = None
        client = MagicMock()
        client.public_get.return_value = {"data": [{"organization_id": "org-only", "slug": "my-org"}]}
        result = get_org_id(config, None, client=client)
        assert result == "org-only"

    def test_multiple_orgs_aborts_with_list(self) -> None:
        config = MagicMock(spec=CliConfig)
        config.default_org = None
        client = MagicMock()
        client.public_get.return_value = {
            "data": [
                {"organization_id": "org-a", "slug": "alpha"},
                {"organization_id": "org-b", "slug": "beta"},
            ]
        }
        with pytest.raises(click.Abort):
            get_org_id(config, None, client=client)

    def test_zero_orgs_aborts(self) -> None:
        config = MagicMock(spec=CliConfig)
        config.default_org = None
        client = MagicMock()
        client.public_get.return_value = {"data": []}
        with pytest.raises(click.Abort):
            get_org_id(config, None, client=client)

    def test_no_client_aborts(self) -> None:
        config = MagicMock(spec=CliConfig)
        config.default_org = None
        with pytest.raises(click.Abort):
            get_org_id(config, None)

    def test_resolves_slug_to_id(self) -> None:
        config = MagicMock(spec=CliConfig)
        config.default_org = None
        client = MagicMock()
        client.public_get.return_value = {
            "data": [
                {"organization_id": "org-a", "slug": "alpha"},
                {"organization_id": "org-b", "slug": "beta"},
            ]
        }
        assert get_org_id(config, "beta", client=client) == "org-b"

    def test_resolves_slug_case_insensitively(self) -> None:
        config = MagicMock(spec=CliConfig)
        config.default_org = None
        client = MagicMock()
        client.public_get.return_value = {"data": [{"organization_id": "org-a", "slug": "alpha"}]}
        assert get_org_id(config, "ALPHA", client=client) == "org-a"

    def test_resolves_default_org_slug(self) -> None:
        # AVR_ORG can hold a slug; config.default_org flows through the same path.
        config = MagicMock(spec=CliConfig)
        config.default_org = "alpha"
        client = MagicMock()
        client.public_get.return_value = {"data": [{"organization_id": "org-a", "slug": "alpha"}]}
        assert get_org_id(config, None, client=client) == "org-a"

    def test_org_id_skips_resolution_round_trip(self) -> None:
        config = MagicMock(spec=CliConfig)
        config.default_org = None
        client = MagicMock()
        assert get_org_id(config, "org-x", client=client) == "org-x"
        client.public_get.assert_not_called()

    def test_unknown_slug_aborts(self) -> None:
        config = MagicMock(spec=CliConfig)
        config.default_org = None
        client = MagicMock()
        client.public_get.return_value = {"data": [{"organization_id": "org-a", "slug": "alpha"}]}
        with pytest.raises(click.Abort):
            get_org_id(config, "nope", client=client)

    def test_slug_without_client_passes_through(self) -> None:
        # No client means no resolution; the raw value goes to the backend.
        config = MagicMock(spec=CliConfig)
        config.default_org = None
        assert get_org_id(config, "alpha") == "alpha"


class TestMatchOrg:
    ORGS = [
        {"organization_id": "org-a", "slug": "alpha"},
        {"organization_id": "org-b", "slug": "beta"},
    ]

    def test_matches_by_id(self) -> None:
        assert match_org(self.ORGS, "org-b") == self.ORGS[1]

    def test_matches_by_slug(self) -> None:
        assert match_org(self.ORGS, "alpha") == self.ORGS[0]

    def test_matches_slug_case_folded(self) -> None:
        assert match_org(self.ORGS, "Beta") == self.ORGS[1]

    def test_no_match_returns_none(self) -> None:
        assert match_org(self.ORGS, "gamma") is None

    def test_id_takes_precedence_over_slug(self) -> None:
        # A value that is some org's ID resolves to that org even if it could
        # never collide with a slug (slugs aren't ``org-`` prefixed).
        assert match_org(self.ORGS, "org-a") == self.ORGS[0]


class TestParseSince:
    """The shared --since parser used by run/job/workflow list."""

    def test_days(self):
        before = datetime.now(UTC) - timedelta(days=7)
        result = parse_since("7d")
        # Allow a small wall-clock skew between `before` and result.
        assert abs((result - before).total_seconds()) < 5

    def test_hours(self):
        before = datetime.now(UTC) - timedelta(hours=24)
        result = parse_since("24h")
        assert abs((result - before).total_seconds()) < 5

    def test_minutes(self):
        before = datetime.now(UTC) - timedelta(minutes=30)
        result = parse_since("30m")
        assert abs((result - before).total_seconds()) < 5

    @pytest.mark.parametrize("bad", ["", "30", "all", "abc", "5x", "1.5d"])
    def test_rejects_invalid(self, bad):
        with pytest.raises(click.ClickException, match="Invalid --since"):
            parse_since(bad)


class TestHandleHttpError:
    """The body-decode in handle_http_error narrowed to ``except ValueError``
    in Batch A. JSONDecodeError is a ValueError subclass, so JSON parse
    failures fall through; non-JSON 5xx (e.g. proxy HTML error pages) used
    to be silently swallowed by the bare ``except Exception`` and now still
    are, just with a precise type. Pin both shapes."""

    def _exc(self, status: int, body: bytes, content_type: str) -> httpx.HTTPStatusError:
        req = httpx.Request("GET", "https://api.example/x")
        resp = httpx.Response(status, request=req, content=body, headers={"content-type": content_type})
        return httpx.HTTPStatusError("err", request=req, response=resp)

    def test_json_body_with_detail_surfaces_message(self, capsys):
        exc = self._exc(500, b'{"detail": "boom"}', "application/json")
        with pytest.raises(SystemExit) as excinfo:
            handle_http_error(exc, "do thing")
        assert excinfo.value.code == 1
        err = capsys.readouterr().err
        assert "Avrea is having trouble" in err
        assert "HTTP 500" in err
        assert "boom" in err

    def test_html_body_falls_through_silently(self, capsys):
        # Proxy/edge layer returns HTML on 502; helper should surface the
        # bare HTTP code without crashing on the decode.
        exc = self._exc(502, b"<html><body>Bad Gateway</body></html>", "text/html")
        with pytest.raises(SystemExit) as excinfo:
            handle_http_error(exc, "fetch run")
        assert excinfo.value.code == 1
        err = capsys.readouterr().err
        assert "Avrea is having trouble" in err
        assert "HTTP 502" in err
        # No detail, no leaked HTML in the user-facing message.
        assert "<html>" not in err

    def test_json_without_detail_field(self, capsys):
        exc = self._exc(400, b'{"other": "bar"}', "application/json")
        with pytest.raises(SystemExit):
            handle_http_error(exc, "x")
        err = capsys.readouterr().err
        assert "HTTP 400" in err
        assert ":" not in err.split("HTTP 400")[1].split("\n")[0]  # no detail suffix

    def test_empty_body(self, capsys):
        exc = self._exc(503, b"", "application/json")
        with pytest.raises(SystemExit):
            handle_http_error(exc, "x")
        err = capsys.readouterr().err
        assert "HTTP 503" in err


class TestFormatSize:
    """Pin the unit-step boundaries; the cache panel + cache list use this
    extensively, so a regression looks like 'every size shows 1024 KB'."""

    def test_zero(self):
        assert format_size(0) == "0.0 B"

    def test_under_one_kb(self):
        assert format_size(512) == "512.0 B"

    def test_exactly_one_kb_promotes_to_kb_unit(self):
        # 1024 is the boundary: shouldn't render as 1024.0 B.
        assert format_size(1024) == "1.0 KB"

    def test_just_under_one_mb(self):
        assert format_size(1024 * 1024 - 1).endswith("KB")

    def test_one_mb(self):
        assert format_size(1024 * 1024) == "1.0 MB"

    def test_one_gb(self):
        assert format_size(1024**3) == "1.0 GB"

    def test_one_tb(self):
        assert format_size(1024**4) == "1.0 TB"

    def test_above_petabyte_caps_at_pb(self):
        # Beyond TB the function falls through to the "PB" tail unit.
        assert format_size(1024**5).endswith("PB")
