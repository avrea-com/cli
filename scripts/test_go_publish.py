"""A partial release resumes without replacing any published bytes."""

from pathlib import Path
from scripts import go_publish as publish
from typing import Any
import io
import json
import pytest
import subprocess

RELEASE_ID = 123


def fake_github(monkeypatch, tag, *, listed=None, tag_exists=True, pypi=()):
    """Replace gh and PyPI with one release whose state the test can inspect.

    `listed` overrides what the release list returns, for a list that lags
    behind a new draft or holds several releases for the tag.
    """
    state: dict[str, Any] = {"created": 0, "assets": {}, "fail_upload": False, "starter": None}

    def assets():
        found = [{"name": name, "state": "uploaded", "size": len(data)} for name, data in state["assets"].items()]
        if state["starter"]:
            found.append({"name": state["starter"], "state": "starter", "size": 0, "id": 42})
        return found

    def api(args):
        assert not any("/releases/tags/" in arg for arg in args), "drafts require lookup by release ID"
        if "DELETE" in args:
            assert args[-1].endswith("/42") and state["starter"]
            state["starter"] = None
            return ""
        if "POST" in args:
            assert not state["created"] and "draft=true" in args and f"tag_name={tag}" in args
            state["created"] += 1
            return json.dumps({"id": RELEASE_ID, "assets": []})
        if args[-1].endswith(f"/git/ref/tags/{tag}"):
            if not tag_exists:
                raise subprocess.CalledProcessError(1, "gh", stderr="HTTP 404")
            return "{}"
        if args[-1].endswith(f"/releases/{RELEASE_ID}"):
            return json.dumps({"assets": assets()})
        if listed is not None:
            return json.dumps([listed])
        return json.dumps([[{"tag_name": tag, "id": RELEASE_ID}] if state["created"] else []])

    def gh(*args):
        if args[0] == "api":
            return api(args)
        assert args[0] == "release"
        if args[1] == "download":
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
    monkeypatch.setattr(publish, "urlopen", lambda *a, **k: io.BytesIO(json.dumps({"urls": list(pypi)}).encode()))
    return state


def test_partial_github_and_pypi_upload_resume(tmp_path, monkeypatch):
    (tmp_path / "avr-version.txt").write_text("0.7.0\n")
    wheel = tmp_path / "avr_cli-0.7.0-py3-none-win_amd64.whl"
    wheel.write_bytes(b"validated wheel")
    # PyPI already received this wheel on an earlier attempt.
    remote_wheel = {"filename": wheel.name, "digests": {"sha256": publish.digest(wheel)}}
    state = fake_github(monkeypatch, "v0.7.0", pypi=[remote_wheel])
    state["fail_upload"] = True
    with pytest.raises(RuntimeError, match="interrupted"):
        publish.prepare(tmp_path, "v0.7.0")
    publish.prepare(tmp_path, "v0.7.0")
    publish.prepare(tmp_path, "v0.7.0")
    assert state["created"] == 1
    assert state["assets"] == {path.name: path.read_bytes() for path in tmp_path.iterdir()}
    wheel.write_bytes(b"different rebuild")
    with pytest.raises(ValueError, match="differs"):
        publish.prepare(tmp_path, "v0.7.0")


def test_new_draft_is_filled_while_the_release_list_lags(tmp_path, monkeypatch):
    version_file = tmp_path / "avr-version.txt"
    version_file.write_text("0.9.0\n")
    state = fake_github(monkeypatch, "v0.9.0", listed=[])
    publish.prepare(tmp_path, "v0.9.0")
    assert state["created"] == 1
    assert state["assets"] == {version_file.name: version_file.read_bytes()}


def test_several_releases_for_the_tag_stop_before_any_change(tmp_path, monkeypatch):
    (tmp_path / "avr-version.txt").write_text("0.9.0\n")
    drafts = [{"tag_name": "v0.9.0", "id": RELEASE_ID}, {"tag_name": "v0.9.0", "id": 456}]
    state = fake_github(monkeypatch, "v0.9.0", listed=drafts)
    with pytest.raises(ValueError, match="Several GitHub releases"):
        publish.prepare(tmp_path, "v0.9.0")
    assert not state["created"] and not state["assets"]


def test_draft_is_not_created_for_a_tag_that_was_not_pushed(tmp_path, monkeypatch):
    (tmp_path / "avr-version.txt").write_text("0.9.0\n")
    state = fake_github(monkeypatch, "v0.9.0", tag_exists=False)
    with pytest.raises(subprocess.CalledProcessError):
        publish.prepare(tmp_path, "v0.9.0")
    assert not state["created"] and not state["assets"]


def test_existing_pypi_bytes_must_match(tmp_path):
    wheel = tmp_path / "avr.whl"
    wheel.write_bytes(b"tested")
    for name, checksum in [(wheel.name, "wrong"), ("unexpected.whl", publish.digest(wheel))]:
        with pytest.raises(ValueError, match="different artifacts"):
            publish.verify_pypi([wheel], [{"filename": name, "digests": {"sha256": checksum}}])
