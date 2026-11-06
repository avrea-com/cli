"""A partial release resumes without replacing any published bytes."""

from pathlib import Path
from scripts import go_publish as publish
from typing import Any
import io
import json
import pytest


def test_partial_github_and_pypi_upload_resume(tmp_path, monkeypatch):
    (tmp_path / "avr-version.txt").write_text("0.7.0\n")
    wheel = tmp_path / "avr_cli-0.7.0-py3-none-win_amd64.whl"
    wheel.write_bytes(b"validated wheel")
    state: dict[str, Any] = {"created": False, "assets": {}, "fail_upload": True, "starter": None}

    def gh(*args):
        if args[0] == "api":
            if "DELETE" in args:
                assert args[-1].endswith("/42") and state["starter"]
                state["starter"] = None
                return ""
            assert "/tags/" not in args[-1], "drafts require lookup by release ID"
            if args[-1].endswith("/releases/123"):
                assets = [
                    {"name": name, "state": "uploaded", "size": len(data)} for name, data in state["assets"].items()
                ]
                if state["starter"]:
                    assets.append({"name": state["starter"], "state": "starter", "size": 0, "id": 42})
                return json.dumps({"assets": assets})
            return json.dumps([[{"tag_name": "v0.7.0", "id": 123}] if state["created"] else []])
        assert args[0] == "release"
        if args[1] == "create":
            assert not state["created"] and "--draft" in args
            state["created"] = True
        elif args[1] == "download":
            (Path(args[-1]) / args[4]).write_bytes(state["assets"][args[4]])
        elif args[1] == "upload":
            path = Path(args[3])
            assert path.name not in state["assets"]
            assert state["starter"] != path.name
            if state["assets"] and state["fail_upload"]:
                state["fail_upload"] = False
                state["starter"] = path.name
                raise RuntimeError("upload interrupted")
            state["assets"][path.name] = path.read_bytes()
        else:
            pytest.fail(f"unexpected mutation: {args}")
        return ""

    monkeypatch.setattr(publish, "gh", gh)
    # PyPI already received this wheel on an earlier attempt.
    remote_wheel = {"filename": wheel.name, "digests": {"sha256": publish.digest(wheel)}}
    monkeypatch.setattr(publish, "urlopen", lambda *a, **k: io.BytesIO(json.dumps({"urls": [remote_wheel]}).encode()))
    with pytest.raises(RuntimeError, match="interrupted"):
        publish.prepare(tmp_path, "v0.7.0")
    publish.prepare(tmp_path, "v0.7.0")
    publish.prepare(tmp_path, "v0.7.0")
    assert state["assets"] == {path.name: path.read_bytes() for path in tmp_path.iterdir()}
    wheel.write_bytes(b"different rebuild")
    with pytest.raises(ValueError, match="differs"):
        publish.prepare(tmp_path, "v0.7.0")


def test_existing_pypi_bytes_must_match(tmp_path):
    wheel = tmp_path / "avr.whl"
    wheel.write_bytes(b"tested")
    for name, checksum in [(wheel.name, "wrong"), ("unexpected.whl", publish.digest(wheel))]:
        with pytest.raises(ValueError, match="different artifacts"):
            publish.verify_pypi([wheel], [{"filename": name, "digests": {"sha256": checksum}}])
