"""Verify artifact checks reject corruption, permission drift and leftover temp files."""

from copy import deepcopy
from http.server import ThreadingHTTPServer
from pathlib import Path
from scripts.compare_artifact import compare
from scripts.compare_artifact import file_record
from scripts.compare_auth import Fixture
from scripts.compare_auth import run
from tests.test_go_inspection_parity import capture
from typing import Any
from typing import cast
from urllib.request import urlopen
import sys
import threading


def test_artifact_comparator_checks_actual_files_even_when_both_sides_agree():
    result = capture({})
    result["files"] = {"downloads/build.zip": file_record(b"corrupt")}
    scenario = {
        "name": "artifact files",
        "args": ["artifact", "download"],
        "expected_files": {"downloads/build.zip": file_record(b"correct")},
    }
    differences = compare(scenario, result, deepcopy(result))
    assert "Python file effects" in differences
    assert "Go file effects" in differences


def test_artifact_comparator_catches_leftover_temporary_file():
    result = capture({})
    result["files"] = {"downloads/build.zip": file_record(b"correct")}
    go = deepcopy(result)
    go["files"]["downloads/.avr-artifact-leftover"] = file_record(b"partial")
    scenario = {"name": "artifact files", "args": ["artifact", "download"], "expected_files": result["files"]}
    assert "Go file effects" in compare(scenario, result, go)


def test_artifact_capture_checks_seeded_file_and_normalizes_only_home(tmp_path):
    scenario = {
        "args": [
            "-c",
            "from pathlib import Path; p=Path('downloads/new.zip'); p.write_bytes(b'new'); print(p.absolute())",
        ],
        "fixture_files": {"downloads/old.zip": b"old"},
        "capture_files": ["downloads"],
    }
    result = run([sys.executable], scenario, tmp_path, "http://127.0.0.1")
    assert result["exit"] == 0
    assert "<home>" in result["stdout"] and result["stdout"].rstrip().endswith(str(Path("downloads/new.zip")))
    assert result["files"] == {
        "downloads/old.zip": file_record(b"old", 0o644),
        "downloads/new.zip": file_record(b"new", 0o644),
    }


def test_binary_fixture_preserves_bytes_and_extra_headers():
    server = cast(Any, ThreadingHTTPServer(("127.0.0.1", 0), Fixture))
    server.requests = []
    server.responses = {("GET", "/download"): (200, b"\x00\xffarchive", {"X-Fixture": "binary"})}
    server.encoding = None
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    try:
        with urlopen(f"http://127.0.0.1:{server.server_port}/download", timeout=5) as response:
            assert response.read() == b"\x00\xffarchive"
            assert response.headers["X-Fixture"] == "binary"
            assert response.headers["Content-Type"] == "application/octet-stream"
    finally:
        server.shutdown()
        server.server_close()
        worker.join()


def test_browser_capture_does_not_start_login_callback(tmp_path):
    scenario = {
        "args": ["-c", "import webbrowser; webbrowser.open('https://example.test/cache')"],
        "browser_capture": True,
    }
    result = run([sys.executable], scenario, tmp_path, "http://127.0.0.1")
    assert result["exit"] == 0
    assert result["browser"] == "https://example.test/cache"
