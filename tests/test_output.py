"""Tests for output formatting utilities."""

from avrea_cli.output import format_key_value
from avrea_cli.output import format_relative_timestamp
from avrea_cli.output import format_timestamp
from avrea_cli.output import output_list
from avrea_cli.output import short_id
from datetime import UTC
from datetime import datetime
from datetime import timedelta


class TestFormatKeyValue:
    def test_basic(self) -> None:
        result = format_key_value({"Name": "Alice", "Age": 30})
        assert "Name" in result
        assert "Alice" in result
        assert "Age" in result
        assert "30" in result

    def test_with_title(self) -> None:
        result = format_key_value({"Key": "val"}, title="Details")
        lines = result.splitlines()
        assert lines[0] == "Details"
        assert lines[1].startswith("-")

    def test_empty_dict(self) -> None:
        assert format_key_value({}) == "No data."

    def test_alignment(self) -> None:
        result = format_key_value({"Short": "a", "LongerKey": "b"})
        lines = result.splitlines()
        # Values should start at the same column
        val_positions = [line.index("a") if "a" in line else line.index("b") for line in lines]
        assert val_positions[0] == val_positions[1]


class TestOutputList:
    def test_table_format(self, capsys) -> None:
        data = [{"id": "1", "name": "Alice"}, {"id": "2", "name": "Bob"}]
        output_list(data, columns=["id", "name"])
        out = capsys.readouterr().out
        assert "Alice" in out
        assert "Bob" in out

    def test_empty_data_table(self, capsys) -> None:
        output_list([], columns=["id"])
        out = capsys.readouterr().out
        assert "No data found" in out

    def test_missing_key_shows_na(self, capsys) -> None:
        data = [{"id": "1"}]
        output_list(data, columns=["id", "missing"])
        out = capsys.readouterr().out
        assert "N/A" in out

    def test_custom_column_labels(self, capsys) -> None:
        data = [{"id": "1", "name": "Alice"}]
        output_list(data, columns=["id", "name"], column_labels=["ID", "Full Name"])
        out = capsys.readouterr().out
        assert "Full Name" in out


class TestFormatTimestamp:
    def test_iso_z(self) -> None:
        assert format_timestamp("2025-06-01T12:00:00Z") == "2025-06-01T12:00:00.00Z"

    def test_none(self) -> None:
        assert format_timestamp(None) == "unknown"

    def test_bad_value(self) -> None:
        assert format_timestamp("not-a-date") == "not-a-date"


class TestFormatRelativeTimestamp:
    _FIXED_NOW = datetime(2025, 6, 15, 12, 0, 0, tzinfo=UTC)

    def test_seconds_ago(self, monkeypatch) -> None:
        monkeypatch.setattr(
            "avrea_cli.output.datetime",
            type(
                "dt",
                (),
                {
                    "fromisoformat": datetime.fromisoformat,
                    "now": classmethod(lambda cls, tz=None: TestFormatRelativeTimestamp._FIXED_NOW),
                },
            ),
        )
        ts = (self._FIXED_NOW - timedelta(seconds=30)).isoformat()
        result = format_relative_timestamp(ts)
        assert result == "30s ago"

    def test_minutes_ago(self, monkeypatch) -> None:
        monkeypatch.setattr(
            "avrea_cli.output.datetime",
            type(
                "dt",
                (),
                {
                    "fromisoformat": datetime.fromisoformat,
                    "now": classmethod(lambda cls, tz=None: TestFormatRelativeTimestamp._FIXED_NOW),
                },
            ),
        )
        ts = (self._FIXED_NOW - timedelta(minutes=5)).isoformat()
        result = format_relative_timestamp(ts)
        assert result == "5m ago"

    def test_hours_ago(self, monkeypatch) -> None:
        monkeypatch.setattr(
            "avrea_cli.output.datetime",
            type(
                "dt",
                (),
                {
                    "fromisoformat": datetime.fromisoformat,
                    "now": classmethod(lambda cls, tz=None: TestFormatRelativeTimestamp._FIXED_NOW),
                },
            ),
        )
        ts = (self._FIXED_NOW - timedelta(hours=2, minutes=15)).isoformat()
        result = format_relative_timestamp(ts)
        assert result == "2h15m ago"

    def test_days_ago(self, monkeypatch) -> None:
        monkeypatch.setattr(
            "avrea_cli.output.datetime",
            type(
                "dt",
                (),
                {
                    "fromisoformat": datetime.fromisoformat,
                    "now": classmethod(lambda cls, tz=None: TestFormatRelativeTimestamp._FIXED_NOW),
                },
            ),
        )
        ts = (self._FIXED_NOW - timedelta(days=3)).isoformat()
        result = format_relative_timestamp(ts)
        assert result == "3d ago"

    def test_none(self) -> None:
        assert format_relative_timestamp(None) == "unknown"

    def test_future_returns_just_now(self, monkeypatch) -> None:
        monkeypatch.setattr(
            "avrea_cli.output.datetime",
            type(
                "dt",
                (),
                {
                    "fromisoformat": datetime.fromisoformat,
                    "now": classmethod(lambda cls, tz=None: TestFormatRelativeTimestamp._FIXED_NOW),
                },
            ),
        )
        ts = (self._FIXED_NOW + timedelta(minutes=5)).isoformat()
        assert format_relative_timestamp(ts) == "just now"


class TestShortId:
    def test_truncates_long_id(self) -> None:
        assert short_id("job-abc123def456ghi789") == "job-abc123de"

    def test_preserves_short_id(self) -> None:
        assert short_id("job-short") == "job-short"

    def test_no_prefix(self) -> None:
        assert short_id("noprefixhere") == "noprefixhere"

    def test_custom_keep(self) -> None:
        assert short_id("job-abc123def456", keep=4) == "job-abc1"
