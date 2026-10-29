"""Protect status window validation and opt-in moving-query fixtures."""

from copy import deepcopy
from datetime import UTC
from datetime import datetime
from datetime import timedelta
from http.server import ThreadingHTTPServer
from scripts.compare_auth import USER
from scripts.compare_auth import Fixture
from scripts.compare_status import compare
from tests.test_go_inspection_parity import capture
from typing import Any
from typing import cast
from urllib.parse import urlencode
from urllib.request import urlopen
import json
import threading


def window_capture(days=7):
    result = capture({})
    current = datetime.now(UTC) - timedelta(days=days)
    previous = current - timedelta(days=days)
    result["requests"] = [
        {"method": "GET", "path": "/orgs/test/" + endpoint + "?" + urlencode(params), "body": None}
        for endpoint in ("workflow-runs/aggregate", "jobs/aggregate")
        for params in [
            {"time_bucket": "total", "created_after": current.isoformat()},
            {"time_bucket": "total", "created_after": previous.isoformat(), "created_before": current.isoformat()},
        ]
    ]
    return result


CASE = {"name": "status windows", "args": ["status"], "windows": True, "window_seconds": 7 * 86400}


def test_clock_normalization_accepts_independent_correct_windows():
    assert not compare(CASE, window_capture(), window_capture())


def test_equal_but_wrong_lookbacks_are_rejected():
    wrong = window_capture(days=3)
    assert "wrong current lookback" in compare(CASE, wrong, deepcopy(wrong))
    assert "wrong previous lookback" in compare(CASE, wrong, deepcopy(wrong))


def test_missing_period_is_not_normalized_away():
    python = window_capture()
    go = deepcopy(python)
    go["requests"].pop()
    assert "missing aggregate window" in compare(CASE, python, go)


def test_query_fixture_is_opt_in_and_exact_route_takes_precedence():
    server = cast(Any, ThreadingHTTPServer(("127.0.0.1", 0), Fixture))
    server.requests = []
    server.responses = {("GET", "/metrics?window=exact"): (200, {"kind": "exact"})}
    server.dynamic_responses = {("GET", "/metrics"): [(200, {"kind": "first"}), (200, {"kind": "second"})]}
    server.encoding = None
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    paths = ["/metrics?window=exact", "/metrics?window=1", "/metrics?window=2", "/other?window=3"]
    values = []
    try:
        for path in paths:
            with urlopen(f"http://127.0.0.1:{server.server_port}{path}", timeout=5) as response:
                values.append(json.load(response))
    finally:
        server.shutdown()
        server.server_close()
        worker.join()
    assert values == [{"kind": "exact"}, {"kind": "first"}, {"kind": "second"}, USER]
    assert [request["path"] for request in server.requests] == paths
