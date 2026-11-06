"""Schema fetching is explicit and never replaces a valid input on failure."""

from scripts import fetch_go_schema as fetcher
from unittest.mock import Mock
import io
import json
import pytest
import subprocess


def document():
    return json.dumps({"openapi": "3.1.0", "paths": {"/health": {"get": {"operationId": "health"}}}}).encode()


def test_core_revision_ignores_uncommitted_working_schema(tmp_path):
    def git(*args):
        return subprocess.check_output(["git", "-C", str(tmp_path), *args])

    git("init", "-q")
    source = tmp_path / "control/openapi/public.json"
    source.parent.mkdir(parents=True)
    source.write_bytes(document())
    git("add", ".")
    git(
        "-c",
        "user.name=Fixture",
        "-c",
        "user.email=fixture@example.test",
        "-c",
        "commit.gpgsign=false",
        "commit",
        "-qm",
        "fixture",
    )
    source.write_text("uncommitted older or invalid schema")
    destination = tmp_path / "build/schema.json"
    fetcher.fetch(core=tmp_path, core_ref="HEAD", output=destination)
    assert destination.read_bytes() == document()


def test_https_download_and_failed_refresh_preserves_input(tmp_path, monkeypatch):
    destination = tmp_path / "schema.json"

    class response(io.BytesIO):
        def geturl(self):
            return "https://api.example.test/openapi.json"

    opener = Mock(return_value=response(document()))
    monkeypatch.setattr(fetcher.urllib.request, "urlopen", opener)
    fetcher.fetch(url="https://api.example.test/openapi.json", output=destination)
    assert destination.read_bytes() == document()
    opener.assert_called_once_with("https://api.example.test/openapi.json", timeout=60)
    for raw in [b"<html>not a schema</html>", b"{}", b"x" * (fetcher.MAX_BYTES + 1)]:
        opener.return_value = response(raw)
        with pytest.raises(ValueError):
            fetcher.fetch(url="https://api.example.test/openapi.json", output=destination)
        assert destination.read_bytes() == document()
    opener.side_effect = TimeoutError("offline")
    with pytest.raises(TimeoutError):
        fetcher.fetch(url="https://api.example.test/openapi.json", output=destination)
    assert destination.read_bytes() == document()


@pytest.mark.parametrize(
    "url", ["http://example.test/schema", "file:///tmp/schema", "https://name:secret@example.test/schema"]
)
def test_invalid_source_is_not_requested(url, tmp_path, monkeypatch):
    opener = Mock()
    monkeypatch.setattr(fetcher.urllib.request, "urlopen", opener)
    with pytest.raises(ValueError):
        fetcher.fetch(url=url, output=tmp_path / "schema")
    opener.assert_not_called()
