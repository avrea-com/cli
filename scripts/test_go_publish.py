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


def lagging_release_list(tmp_path, monkeypatch, stale_lookups):
    """Fake GitHub whose release list omits a new draft for `stale_lookups` reads."""
    (tmp_path / "avr-version.txt").write_text("0.9.0\n")
    state: dict[str, Any] = {"created": 0, "stale": 0, "uploaded": [], "sleeps": []}

    def gh(*args):
        if args[0] == "api" and args[-1].endswith("/releases/123"):
            return json.dumps({"assets": []})
        if args[0] == "api":
            if not state["created"]:
                return json.dumps([[]])
            if state["stale"] < stale_lookups:
                state["stale"] += 1
                return json.dumps([[]])
            return json.dumps([[{"tag_name": "v0.9.0", "id": 123}]])
        if args[:2] == ("release", "create"):
            state["created"] += 1
        elif args[:2] == ("release", "upload"):
            state["uploaded"].append(Path(args[3]).name)
        else:
            pytest.fail(f"unexpected mutation: {args}")
        return ""

    monkeypatch.setattr(publish, "gh", gh)
    monkeypatch.setattr(publish, "urlopen", lambda *a, **k: io.BytesIO(json.dumps({"urls": []}).encode()))
    return state


def test_new_draft_missing_from_release_list_is_awaited(tmp_path, monkeypatch):
    state = lagging_release_list(tmp_path, monkeypatch, stale_lookups=2)
    publish.prepare(tmp_path, "v0.9.0", sleep=state["sleeps"].append)
    assert state["created"] == 1
    assert state["uploaded"] == ["avr-version.txt"]
    assert len(state["sleeps"]) == 2


def test_draft_that_never_appears_fails_after_bounded_lookups(tmp_path, monkeypatch):
    state = lagging_release_list(tmp_path, monkeypatch, stale_lookups=1000)
    with pytest.raises(ValueError, match="could not be found"):
        publish.prepare(tmp_path, "v0.9.0", sleep=state["sleeps"].append)
    assert state["created"] == 1 and not state["uploaded"]
    assert 0 < len(state["sleeps"]) < 100
