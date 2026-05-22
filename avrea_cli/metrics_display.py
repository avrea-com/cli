"""Render job VM metrics as terminal gauges.

The control-plane endpoint at /orgs/{org_id}/jobs/{job_id}/metrics/{source}
returns one or more time series plus a unit. Counter sources additionally
return per-scrape rates (bytes/sec, ops/sec). We collapse the series into a
single rolled-up time series, then render avg/peak/last with a fixed-width bar.

Default mode: bar fill = avg over the window, peak shown as a ▲ marker. In
``--live`` mode the headline switches to "now" so a refreshing pane shows the
latest sample as the bar fill. Saturation-bounded sources (cpu / filesystem /
memory-with-known-RAM) get a green/yellow/red color tier so high-pressure runs
jump out at a glance.
"""

from typing import Any
from typing import Final
import click

GAUGE_WIDTH: Final[int] = 20
BAR_FILL: Final[str] = "█"  # █
BAR_EMPTY: Final[str] = "░"  # ░
PEAK_MARKER: Final[str] = "▲"

# Painted explicitly so the ▲ peak marker shares the same background as
# surrounding empty cells; otherwise it pops off the bar instead of sitting
# on it (the ░ glyph implies gray, but ▲ doesn't).
_TRACK_BG: Final[str] = "bright_black"

# Saturation thresholds for the green / yellow / red color tier. Numbers in
# 0..1; only applied to sources whose ratio has an absolute meaning (cpu,
# filesystem, memory when VM RAM is known).
_SAT_GREEN_TO_YELLOW: Final[float] = 0.60
_SAT_YELLOW_TO_RED: Final[float] = 0.85


def _saturation_color(ratio: float) -> str | None:
    """Pick a color tier for a 0..1 saturation ratio. ``None`` if uncolored."""
    if ratio != ratio:  # NaN guard
        return None
    if ratio >= _SAT_YELLOW_TO_RED:
        return "red"
    if ratio >= _SAT_GREEN_TO_YELLOW:
        return "yellow"
    return "green"


# Source → (kind, value_scale).
# kind: "gauge" reads `values`; "counter" reads `rates` (bytes/sec, ops/sec).
# scale: how the numbers should be formatted and how to derive a 0..1 bar fill.
SOURCE_KIND: Final[dict[str, tuple[str, str]]] = {
    "cpu": ("gauge", "ratio"),
    "memory": ("gauge", "bytes"),
    "filesystem": ("gauge", "ratio"),
    "load": ("gauge", "count"),
    "disk-io": ("counter", "bytes"),
    "disk-ops": ("counter", "count"),
    "network": ("counter", "bytes"),
}

ALL_SOURCES: Final[tuple[str, ...]] = tuple(SOURCE_KIND)


def aggregate_values(series: list[dict[str, Any]]) -> list[tuple[int, float]]:
    """Sum series at each timestamp into a single rolled-up time series.

    Use only for sources where summing every series at the same timestamp
    is meaningful (filesystem, load, disk-io counters, etc.). CPU and memory
    have their own aggregators because naive summing crosses state labels
    (idle/user/cached/free/...) and silently inflates the result."""
    bucket: dict[int, float] = {}
    for s in series:
        for ts, val in s.get("values", []) or []:
            bucket[int(ts)] = bucket.get(int(ts), 0.0) + float(val)
    return sorted(bucket.items())


def aggregate_cpu_utilization(series: list[dict[str, Any]]) -> list[tuple[int, float]]:
    """Average CPU utilization across cores at each timestamp, returned 0..1.

    `system.cpu.utilization` returns (cpu, state) labelled series where each
    state is a fraction of that CPU's time (user, system, idle, iowait, ...).
    All states for one CPU sum to 1.0. We compute `1 - idle` per CPU (catching
    every busy state without enumerating them) and then average across CPUs so
    the gauge stays in 0..1 regardless of vCPU count.
    """
    # (ts, cpu) → idle fraction, plus a set of CPUs we saw at that ts.
    idle_by_cpu_ts: dict[tuple[int, str], float] = {}
    cpus_by_ts: dict[int, set[str]] = {}
    for s in series:
        labels = s.get("labels") or {}
        cpu = labels.get("cpu", "cpu0")
        state = labels.get("state", "")
        for ts, val in s.get("values", []) or []:
            ts_i = int(ts)
            cpus_by_ts.setdefault(ts_i, set()).add(cpu)
            if state == "idle":
                idle_by_cpu_ts[(ts_i, cpu)] = float(val)

    out: list[tuple[int, float]] = []
    for ts, cpus in sorted(cpus_by_ts.items()):
        utilizations = [1.0 - idle_by_cpu_ts.get((ts, c), 0.0) for c in cpus]
        out.append((ts, sum(utilizations) / len(utilizations)))
    return out


def aggregate_memory_used(series: list[dict[str, Any]]) -> list[tuple[int, float]]:
    """Bytes in `state="used"` only.

    `system.memory.usage` exposes states like used/free/buffered/cached.
    Summing every state always equals total RAM — useless as a gauge.
    The OTel collector's `used` state is what corresponds to "occupied by
    running processes plus the kernel's working set", which matches the
    intuition behind the bar.
    """
    bucket: dict[int, float] = {}
    for s in series:
        if (s.get("labels") or {}).get("state") != "used":
            continue
        for ts, val in s.get("values", []) or []:
            bucket[int(ts)] = bucket.get(int(ts), 0.0) + float(val)
    return sorted(bucket.items())


def aggregate_rates(series: list[dict[str, Any]]) -> list[tuple[int, float]]:
    """Sum per-scrape rates across counter series."""
    bucket: dict[int, float] = {}
    for s in series:
        for ts, val in s.get("rates") or []:
            bucket[int(ts)] = bucket.get(int(ts), 0.0) + float(val)
    return sorted(bucket.items())


def format_bytes(b: float) -> str:
    if b < 1024:
        return f"{b:.0f} B"
    if b < 1024**2:
        return f"{b / 1024:.1f} KB"
    if b < 1024**3:
        return f"{b / 1024**2:.1f} MB"
    return f"{b / 1024**3:.1f} GB"


def format_rate(value: float, unit: str) -> str:
    if unit == "bytes/sec":
        return f"{format_bytes(value)}/s"
    if unit == "operations/sec":
        if value < 1000:
            return f"{value:.0f} ops/s"
        return f"{value / 1000:.1f}k ops/s"
    return f"{value:.2f} {unit}"


def render_bar(
    fill: float,
    peak: float | None = None,
    *,
    width: int = GAUGE_WIDTH,
    fill_color: str | None = None,
    peak_color: str | None = None,
) -> str:
    """Bar with ``fill`` (0..1) drawn as solid blocks and a ``peak`` marker
    (``▲``) overlaid at its position. Out-of-range values are clamped.

    ``fill_color`` colors the avg run (e.g. cpu saturation tier). ``peak_color``
    colors the ▲ itself; no background block is painted, so the marker blends
    into whatever cell it sits on (filled run or empty region).

    The marker is shown whenever ``peak`` is provided — including when peak
    equals avg — so a flat-loaded run (peak == avg) still has a visible ▲ at
    the boundary indicating "this was both the avg and the high-water mark".
    """
    if fill != fill:  # NaN guard
        fill = 0.0
    fill = max(0.0, min(1.0, fill))
    fill_n = round(fill * width)
    peak_n = -1
    if peak is not None and peak == peak:
        peak_clamped = max(0.0, min(1.0, peak))
        peak_n = round(peak_clamped * width) - 1
        peak_n = max(0, min(peak_n, width - 1))
        # Ensure ▲ never visibly replaces a fill cell unless the bar is at
        # the right edge with no room left for it.
        if peak_n < fill_n:
            peak_n = min(fill_n, width - 1)

    if peak_n < 0:
        return _styled_fill(fill_n, fill_color) + _styled_track(width - fill_n)

    # When ▲ falls inside the fill region (only happens at width-1 with a
    # nearly-full bar), it replaces the last fill cell.
    visible_fill = min(fill_n, peak_n) if peak_n < fill_n else fill_n
    gap_cells = max(0, peak_n - fill_n)
    suffix_n = width - peak_n - 1

    filled_run = _styled_fill(visible_fill, fill_color)
    gap_run = _styled_track(gap_cells)
    suffix_run = _styled_track(suffix_n)

    # Marker bg matches the cell it would otherwise occupy: the track bg if
    # it sits in empty space, the fill color if it sits at/inside the fill.
    # Marker fg gets a contrasting choice — peak's tier color reads well over
    # the gray track, but bleeds into a same-tier fill (green ▲ on green is
    # invisible). On the fill we draw the ▲ in black.
    if peak_n < fill_n or gap_cells == 0:
        marker_bg = fill_color  # ▲ on / inside the fill — match the fill block
        marker_fg = "black"
    else:
        marker_bg = _TRACK_BG  # ▲ floating in the track — match the gray track
        marker_fg = peak_color
    if marker_fg or marker_bg:
        marker = click.style(PEAK_MARKER, fg=marker_fg, bg=marker_bg)
    else:
        marker = PEAK_MARKER
    return filled_run + gap_run + marker + suffix_run


def _styled_fill(n: int, color: str | None) -> str:
    if n <= 0:
        return ""
    s = BAR_FILL * n
    return click.style(s, fg=color) if color else s


def _styled_track(n: int) -> str:
    if n <= 0:
        return ""
    return click.style(BAR_EMPTY * n, fg=_TRACK_BG, bg=_TRACK_BG)


def _format_summary_pair(label_a: str, value_a: str, label_b: str, value_b: str) -> str:
    """`(peak X, avg Y)` style trailing annotation."""
    return f"({label_a} {value_a}, {label_b} {value_b})"


def render_gauge_line(
    source: str, response: dict[str, Any], vm_total_ram_bytes: int | None, *, live: bool = False
) -> str:
    """Render one source as a labeled bar + summary line.

    The bar fill defaults to the window average so a static read shows
    "how loaded was this VM overall?" at a glance. ``live=True`` switches
    to the most recent sample (the natural choice for a refreshing pane).
    """
    kind, scale = SOURCE_KIND[source]
    series = response.get("series", []) or []
    label = f"{source:<10}"

    if kind == "counter":
        rates = aggregate_rates(series)
        if not rates:
            return f"{label} (no rate samples yet)"
        return _render_counter(label, rates, response.get("rate_unit", ""), live=live)

    if source == "cpu":
        values = aggregate_cpu_utilization(series)
    elif source == "memory":
        values = aggregate_memory_used(series)
    else:
        values = aggregate_values(series)
    if not values:
        return f"{label} (no samples yet)"

    last = values[-1][1]
    peak = max(v[1] for v in values)
    avg = sum(v[1] for v in values) / len(values)

    if live:
        primary, primary_lbl = last, "now"
        secondary, secondary_lbl = avg, "avg"
    else:
        primary, primary_lbl = avg, "avg"
        secondary, secondary_lbl = last, "last"

    if scale == "ratio":
        return _render_ratio_gauge(label, primary, peak, secondary, primary_lbl, secondary_lbl)
    if scale == "bytes" and vm_total_ram_bytes and source == "memory":
        return _render_memory_with_total(
            label, primary, peak, secondary, vm_total_ram_bytes, primary_lbl, secondary_lbl
        )
    if scale == "bytes":
        return _render_bytes_no_total(label, primary, peak, secondary, primary_lbl, secondary_lbl)
    # scale == "count" (load) — no saturation bound, scale bar to in-window peak
    return _render_count(label, primary, peak, secondary, primary_lbl, secondary_lbl)


def _render_counter(label: str, rates: list[tuple[int, float]], unit: str, *, live: bool) -> str:
    last = rates[-1][1]
    peak = max(r[1] for r in rates)
    avg = sum(r[1] for r in rates) / len(rates)
    if live:
        primary, primary_lbl, secondary, secondary_lbl = last, "now", avg, "avg"
    else:
        primary, primary_lbl, secondary, secondary_lbl = avg, "avg", last, "last"
    fill_ratio = primary / peak if peak > 0 else 0.0
    bar = render_bar(fill_ratio, 1.0)  # peak is the bar's own max, mark sits at the end
    summary = _format_summary_pair("peak", format_rate(peak, unit), secondary_lbl, format_rate(secondary, unit))
    return f"{label} {bar}  {primary_lbl} {format_rate(primary, unit)}  {summary}"


def _render_ratio_gauge(
    label: str, primary: float, peak: float, secondary: float, primary_lbl: str, secondary_lbl: str
) -> str:
    fill_color = _saturation_color(primary)
    peak_color = _saturation_color(peak)
    bar = render_bar(primary, peak, fill_color=fill_color, peak_color=peak_color)
    primary_str = f"{primary * 100:.0f}%"
    primary_str_styled = click.style(primary_str, fg=fill_color) if fill_color else primary_str
    summary = _format_summary_pair("peak", f"{peak * 100:.0f}%", secondary_lbl, f"{secondary * 100:.0f}%")
    return f"{label} {bar}  {primary_lbl} {primary_str_styled}  {summary}"


def _render_memory_with_total(
    label: str,
    primary: float,
    peak: float,
    secondary: float,
    total_bytes: int,
    primary_lbl: str,
    secondary_lbl: str,
) -> str:
    primary_ratio = primary / total_bytes
    peak_ratio = peak / total_bytes
    fill_color = _saturation_color(primary_ratio)
    peak_color = _saturation_color(peak_ratio)
    bar = render_bar(primary_ratio, peak_ratio, fill_color=fill_color, peak_color=peak_color)
    primary_str = f"{format_bytes(primary)} / {format_bytes(total_bytes)}"
    primary_str_styled = click.style(primary_str, fg=fill_color) if fill_color else primary_str
    summary = _format_summary_pair("peak", format_bytes(peak), secondary_lbl, format_bytes(secondary))
    return f"{label} {bar}  {primary_lbl} {primary_str_styled}  {summary}"


def _render_bytes_no_total(
    label: str, primary: float, peak: float, secondary: float, primary_lbl: str, secondary_lbl: str
) -> str:
    fill_ratio = primary / peak if peak > 0 else 0.0
    bar = render_bar(fill_ratio, 1.0)
    summary = _format_summary_pair("peak", format_bytes(peak), secondary_lbl, format_bytes(secondary))
    return f"{label} {bar}  {primary_lbl} {format_bytes(primary)}  {summary}"


def _render_count(
    label: str, primary: float, peak: float, secondary: float, primary_lbl: str, secondary_lbl: str
) -> str:
    fill_ratio = primary / peak if peak > 0 else 0.0
    bar = render_bar(fill_ratio, 1.0)
    summary = _format_summary_pair("peak", f"{peak:.2f}", secondary_lbl, f"{secondary:.2f}")
    return f"{label} {bar}  {primary_lbl} {primary:.2f}  {summary}"
