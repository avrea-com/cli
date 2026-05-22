"""Unit tests for metrics_display — series aggregation and gauge rendering."""

from avrea_cli.metrics_display import GAUGE_WIDTH
from avrea_cli.metrics_display import aggregate_rates
from avrea_cli.metrics_display import aggregate_values
from avrea_cli.metrics_display import format_bytes
from avrea_cli.metrics_display import format_rate
from avrea_cli.metrics_display import render_bar
from avrea_cli.metrics_display import render_gauge_line
import click
import pytest


def _plain(line: str) -> str:
    """Strip ANSI escape codes so tests assert on the visible text only."""
    return click.unstyle(line)


class TestAggregateValues:
    """Series sum at matching timestamps — pre-condition for the gauge math."""

    def test_single_series_passthrough(self):
        s = [{"labels": {"state": "user"}, "values": [(100, 0.4), (200, 0.5)]}]
        assert aggregate_values(s) == [(100, 0.4), (200, 0.5)]

    def test_multi_series_sums_at_matching_ts(self):
        # cpu source has one series per state — total utilization is the sum.
        s = [
            {"labels": {"state": "user"}, "values": [(100, 0.30)]},
            {"labels": {"state": "system"}, "values": [(100, 0.10)]},
            {"labels": {"state": "iowait"}, "values": [(100, 0.05)]},
        ]
        assert aggregate_values(s) == [(100, pytest.approx(0.45))]

    def test_disjoint_timestamps_kept_separate(self):
        s = [
            {"values": [(100, 1.0)]},
            {"values": [(200, 2.0)]},
        ]
        assert aggregate_values(s) == [(100, 1.0), (200, 2.0)]

    def test_empty_series_returns_empty(self):
        assert aggregate_values([]) == []
        assert aggregate_values([{"values": []}]) == []


class TestAggregateRates:
    """Counter sources expose `rates` (len(values)-1 points)."""

    def test_sums_rates_across_series(self):
        s = [
            {"labels": {"direction": "rx"}, "rates": [(110, 100.0), (120, 200.0)]},
            {"labels": {"direction": "tx"}, "rates": [(110, 50.0), (120, 75.0)]},
        ]
        assert aggregate_rates(s) == [(110, 150.0), (120, 275.0)]

    def test_missing_rates_treated_as_empty(self):
        # gauge sources omit `rates` from the response — agg should not crash.
        s = [{"values": [(100, 0.5)]}]
        assert aggregate_rates(s) == []

    def test_null_rates_treated_as_empty(self):
        s = [{"rates": None, "values": []}]
        assert aggregate_rates(s) == []


class TestRenderBar:
    """Bar fill is clamped to [0, 1]."""

    def test_zero_fill_is_all_empty(self):
        bar = _plain(render_bar(0.0))
        assert bar == "░" * GAUGE_WIDTH

    def test_full_fill_is_all_filled(self):
        bar = _plain(render_bar(1.0))
        assert bar == "█" * GAUGE_WIDTH

    def test_half_fill(self):
        bar = _plain(render_bar(0.5))
        assert bar.count("█") == GAUGE_WIDTH // 2
        assert bar.count("░") == GAUGE_WIDTH - GAUGE_WIDTH // 2

    @pytest.mark.parametrize("oob", [-0.5, 1.5, 99.0])
    def test_out_of_range_is_clamped(self, oob):
        bar = _plain(render_bar(oob))
        # Either fully empty (negative) or fully filled (>1.0) — never crash.
        assert len(bar) == GAUGE_WIDTH
        assert set(bar) <= {"█", "░"}

    def test_nan_is_treated_as_zero(self):
        """A NaN slipping through (e.g. 0/0 from peak == 0) must not blow up."""
        bar = _plain(render_bar(float("nan")))
        assert bar == "░" * GAUGE_WIDTH


class TestFormatBytes:
    @pytest.mark.parametrize(
        "value,expected",
        [
            (0, "0 B"),
            (512, "512 B"),
            (1024, "1.0 KB"),
            (5 * 1024**2, "5.0 MB"),
            (4 * 1024**3, "4.0 GB"),
        ],
    )
    def test_unit_thresholds(self, value, expected):
        assert format_bytes(float(value)) == expected


class TestFormatRate:
    def test_bytes_per_sec(self):
        assert format_rate(1024.0, "bytes/sec") == "1.0 KB/s"

    def test_ops_per_sec_small(self):
        assert format_rate(42.0, "operations/sec") == "42 ops/s"

    def test_ops_per_sec_large(self):
        assert format_rate(2500.0, "operations/sec") == "2.5k ops/s"


class TestRenderGaugeLine:
    """Wire the pieces together — pin the response shape we accept."""

    def test_cpu_uses_one_minus_idle_per_cpu(self):
        """Single-CPU case: utilization = 1 - idle. States other than idle
        are ignored even when they sum to >1; the OTel CPU contract is one
        series per (cpu, state) and all states sum to exactly 1.0 per CPU."""
        response = {
            "unit": "ratio",
            "series": [
                {"labels": {"cpu": "cpu0", "state": "user"}, "values": [(100, 0.30), (110, 0.40)]},
                {"labels": {"cpu": "cpu0", "state": "system"}, "values": [(100, 0.10), (110, 0.10)]},
                {"labels": {"cpu": "cpu0", "state": "idle"}, "values": [(100, 0.60), (110, 0.50)]},
            ],
        }
        line = _plain(render_gauge_line("cpu", response, vm_total_ram_bytes=None))
        # Default mode: bar = avg, headline = "avg X%".
        # ts=100: 1 - 0.60 = 0.40; ts=110: 1 - 0.50 = 0.50 → avg = 0.45 → 45%
        assert "avg 45%" in line
        assert "peak 50%" in line
        # `last` shows the most recent sample.
        assert "last 50%" in line

    def test_cpu_averages_across_cores(self):
        """Multi-CPU case: averaging keeps the gauge in 0..1 regardless of
        vCPU count. This was the bug behind the '800% on an 8-vCPU' report."""
        response = {
            "unit": "ratio",
            "series": [
                {"labels": {"cpu": "cpu0", "state": "idle"}, "values": [(100, 0.0)]},  # 100% busy
                {"labels": {"cpu": "cpu1", "state": "idle"}, "values": [(100, 1.0)]},  # 0% busy
            ],
        }
        line = _plain(render_gauge_line("cpu", response, vm_total_ram_bytes=None))
        # avg utilization across 2 cpus = (1.0 + 0.0) / 2 = 0.5 → 50%
        assert "50%" in line
        # With a single sample, avg == peak, so the ▲ sits at the boundary
        # of the fill. Bar shows fill_n cells of █ before the marker.
        bar_segment = line.split("cpu       ")[1]
        assert bar_segment.count("█") == GAUGE_WIDTH // 2
        assert "▲" in bar_segment  # peak still shown even when peak == avg

    def test_memory_only_counts_used_state(self):
        """Memory series are split by state (used/free/cached/buffered/...).
        Naive sum equals total RAM; we filter to state=used."""
        response = {
            "unit": "bytes",
            "series": [
                {"labels": {"state": "used"}, "values": [(100, 4 * 1024**3)]},
                {"labels": {"state": "cached"}, "values": [(100, 2 * 1024**3)]},
                {"labels": {"state": "free"}, "values": [(100, 10 * 1024**3)]},
            ],
        }
        line = _plain(render_gauge_line("memory", response, vm_total_ram_bytes=16 * 1024**3))
        # only the 'used' 4 GB counts
        assert "4.0 GB / 16.0 GB" in line

    def test_memory_with_known_vm_ram(self):
        """Memory bar is scaled to VM RAM (from runner specs) when available."""
        response = {
            "unit": "bytes",
            "series": [{"labels": {"state": "used"}, "values": [(100, 4 * 1024**3)]}],
        }
        line = _plain(render_gauge_line("memory", response, vm_total_ram_bytes=8 * 1024**3))
        assert "4.0 GB / 8.0 GB" in line
        # 4/8 = 0.5 → half-filled bar
        bar_segment = line.split("memory    ")[1]
        assert bar_segment.count("█") == GAUGE_WIDTH // 2

    def test_memory_without_known_vm_ram_uses_peak(self):
        """No RAM hint → bar scales to in-window peak. With avg=1.5 GB and
        peak=2.0 GB, the bar fills 75% (avg/peak) and the ▲ marker sits at
        the end."""
        response = {
            "unit": "bytes",
            "series": [
                {"labels": {"state": "used"}, "values": [(100, 1.0 * 1024**3), (200, 2.0 * 1024**3)]},
            ],
        }
        line = _plain(render_gauge_line("memory", response, vm_total_ram_bytes=None))
        # avg = 1.5 GB, peak = 2.0 GB → fill = 1.5/2.0 = 0.75 → 15 blocks
        assert line.count("█") == 15
        # peak marker sits at the end of the bar
        assert "▲" in line

    def test_counter_uses_rates_not_values(self):
        """Counter sources (network) read `rates`, not `values` — values are the
        raw monotonic counter samples, which would render meaningless bars."""
        response = {
            "unit": "bytes",
            "rate_unit": "bytes/sec",
            "series": [
                {
                    "labels": {"direction": "rx"},
                    "values": [(100, 1_000_000.0), (110, 1_100_000.0)],
                    "rates": [(110, 10000.0)],
                }
            ],
        }
        line = render_gauge_line("network", response, vm_total_ram_bytes=None)
        assert "9.8 KB/s" in line  # 10_000 / 1024 ≈ 9.77

    def test_no_data_branch_for_gauge(self):
        response = {"unit": "ratio", "series": []}
        line = render_gauge_line("cpu", response, vm_total_ram_bytes=None)
        assert "no samples" in line

    def test_no_data_branch_for_counter(self):
        # A single sample → 0 rates from the API; the line should say so cleanly.
        response = {
            "unit": "bytes",
            "rate_unit": "bytes/sec",
            "series": [{"labels": {}, "values": [(100, 1000.0)], "rates": []}],
        }
        line = render_gauge_line("network", response, vm_total_ram_bytes=None)
        assert "no rate samples" in line

    def test_live_mode_swaps_to_now(self):
        """--live: the bar tracks the most recent sample, not the window average."""
        response = {
            "unit": "ratio",
            "series": [
                {"labels": {"cpu": "cpu0", "state": "idle"}, "values": [(100, 0.5), (200, 0.1)]},
            ],
        }
        live_line = _plain(render_gauge_line("cpu", response, vm_total_ram_bytes=None, live=True))
        # ts=200: 1 - 0.1 = 0.9 → "now 90%"
        assert "now 90%" in live_line
        assert "avg 70%" in live_line  # ((1-0.5) + (1-0.1)) / 2 = 0.7

    def test_color_tiers(self):
        """Saturation gradient: green < 60%, yellow 60-85%, red ≥ 85%."""
        # Red tier
        red = render_gauge_line(
            "cpu",
            {"unit": "ratio", "series": [{"labels": {"cpu": "cpu0", "state": "idle"}, "values": [(100, 0.05)]}]},
            vm_total_ram_bytes=None,
        )
        assert "\x1b[31m" in red  # red ANSI
        # Yellow tier
        yellow = render_gauge_line(
            "cpu",
            {"unit": "ratio", "series": [{"labels": {"cpu": "cpu0", "state": "idle"}, "values": [(100, 0.30)]}]},
            vm_total_ram_bytes=None,
        )
        assert "\x1b[33m" in yellow  # yellow ANSI
        # Green tier
        green = render_gauge_line(
            "cpu",
            {"unit": "ratio", "series": [{"labels": {"cpu": "cpu0", "state": "idle"}, "values": [(100, 0.85)]}]},
            vm_total_ram_bytes=None,
        )
        assert "\x1b[32m" in green  # green ANSI

    def test_peak_marker_floating_uses_fg_color(self):
        """When peak floats far from the avg fill, the ▲ is colored as a fg
        (red) on the gray empty cells — no chip bg, since the marker would
        otherwise look like an unrelated bookmark."""
        response = {
            "unit": "ratio",
            "series": [
                {"labels": {"cpu": "cpu0", "state": "idle"}, "values": [(100, 0.95), (110, 0.01)]},
            ],
        }
        line = render_gauge_line("cpu", response, vm_total_ram_bytes=None)
        # avg = (0.05 + 0.99) / 2 = 0.52 → green; peak = 0.99 → red.
        # avg cell ≈ 10, peak cell ≈ 19 → far apart → fg-only marker, no bg.
        assert "\x1b[32m" in line  # green fg on the bar fill
        assert "\x1b[31m" in line  # red fg on the ▲ marker
        assert "\x1b[41m" not in line  # no red bg chip — the ▲ floats

    def test_peak_marker_floating_uses_track_bg(self):
        """When ▲ floats in the empty region, it gets the same track bg
        (bright_black) as the surrounding empty cells. This stops the
        marker cell from showing the terminal's default bg through and
        creating a visual mismatch with the ░ glyphs around it."""
        far = render_gauge_line(
            "memory",
            {
                "unit": "bytes",
                "series": [
                    {"labels": {"state": "used"}, "values": [(100, 1.0 * 1024**3), (200, 2.88 * 1024**3)]},
                ],
            },
            vm_total_ram_bytes=16 * 1024**3,
        )
        assert "\x1b[100m" in far  # bright_black bg = same as track
        assert "\x1b[32m" in far  # green fg ▲ on the track

    def test_peak_marker_at_fill_boundary_uses_fill_bg(self):
        """When the ▲ sits at the fill boundary, it shares the bg with the
        fill cells so it reads as a natural extension. fg switches to black
        so the triangle stays visible against the same-color bg."""
        adjacent = render_gauge_line(
            "cpu",
            {
                "unit": "ratio",
                "series": [
                    # avg ≈ peak ≈ ~99% → ▲ at the fill boundary
                    {"labels": {"cpu": "cpu0", "state": "idle"}, "values": [(100, 0.05), (110, 0.0)]},
                ],
            },
            vm_total_ram_bytes=None,
        )
        assert "\x1b[41m" in adjacent  # red bg — matches the red fill
        assert "\x1b[30m" in adjacent  # black fg ▲ for contrast on the fill

    def test_peak_marker_renders_when_peak_equals_avg(self):
        """A flat workload (peak == avg) still gets a ▲ at the boundary so
        the user knows where the high-water mark was — even if it matches
        the avg exactly."""
        response = {
            "unit": "ratio",
            "series": [
                # All samples identical → avg == peak == 1 - 0.89 = 0.11
                {"labels": {"cpu": "cpu0", "state": "idle"}, "values": [(100, 0.89), (110, 0.89), (120, 0.89)]},
            ],
        }
        line = _plain(render_gauge_line("cpu", response, vm_total_ram_bytes=None))
        assert "▲" in line
        assert "avg 11%" in line
        assert "peak 11%" in line

    def test_peak_marker_position_outside_fill(self):
        """When peak exceeds avg, the ▲ sits at the peak's position (past the
        avg fill) rather than overlapping the fill region."""
        line = _plain(
            render_gauge_line(
                "cpu",
                {
                    "unit": "ratio",
                    "series": [
                        {"labels": {"cpu": "cpu0", "state": "idle"}, "values": [(100, 0.5), (200, 0.0)]},
                    ],
                },
                vm_total_ram_bytes=None,
            )
        )
        # avg = 0.75 → fill_n = 15; peak = 1.0 → peak_n = 19. ▲ at the end.
        assert "▲" in line
        bar_segment = line.split("cpu       ")[1]
        # Strictly more than 15 █ blocks would mean ▲ overlapped the fill;
        # exactly 15 means it correctly sits past the boundary.
        assert bar_segment.count("█") == 15
