"""The live comparator tolerates incidental differences, not missing data or styles."""

from copy import deepcopy
from scripts.compare_staging_readonly import compare
from scripts.compare_staging_readonly import frames
from scripts.compare_staging_readonly import json_equal
from scripts.compare_staging_readonly import snapshots
from scripts.compare_staging_readonly import stable_urls
from scripts.compare_staging_readonly import validate_invocation
import pytest


def result(stdout="", stderr="", exit=0):
    return {"stdout": stdout, "stderr": stderr, "exit": exit, "bounded": False}


def case(path, mode="pipe", args=None):
    return {"path": path, "mode": mode, "args": args or path.split()}


def test_query_order_only_preserves_repeated_values():
    assert stable_urls("Error: https://api.test/x?b=2&a=1&a=3") == stable_urls("Error: https://api.test/x?a=1&a=3&b=2")
    assert stable_urls("https://api.test/x?a=1&a=3") != stable_urls("https://api.test/x?a=3&a=1")


@pytest.mark.parametrize("changed", ['{"count":true}', '{"count":2}', "{}"])
def test_json_information_is_preserved(changed):
    assert compare(case("repo list", "json"), result('{"count":1}'), result(changed))


def test_float_tolerance_is_tight_and_opt_in():
    assert json_equal(1.0000000000000002, 1.0, floating=True)
    assert not json_equal(1.0000000000000002, 1.0)
    assert not json_equal(1.01, 1.0, floating=True)
    assert not json_equal(True, 1, floating=True)
    assert not json_equal(9007199254740993, 9007199254740992, floating=True)


def test_colors_and_layout_remain_contracts():
    original = result("\x1b[31mFailed\x1b[0m\n  job\n")
    assert compare(case("job view", "tty"), original, result("\x1b[32mFailed\x1b[0m\n  job\n"))
    assert compare(case("job view", "tty"), original, result("\x1b[31mFailed\x1b[0m\njob\n"))
    assert not compare(
        case("job view", "tty"), result("\x1b[31;1mFailed\x1b[0m"), result("\x1b[31m\x1b[1mFailed\x1b[0m")
    )


def test_watch_cadence_can_differ_but_data_and_final_messages_cannot():
    frame = "\x1b[2J\x1b[HActive jobs\n\x1b[31mfailed\x1b[0m\n"
    assert frames(frame * 2) == frames(frame * 3)
    assert frames(frame) != frames(frame.replace("31m", "32m"))
    assert frames(frame + "Stopped watching.") != frames(frame)
    assert snapshots('{"data":[1]}\n' * 2) == snapshots('{"data":[1]}\n')
    assert snapshots('{"data":[1]}\n') != snapshots('{"data":[2]}\n')
    assert not json_equal(snapshots('{"data":[1]}\n'), snapshots('{"data":[true]}\n'))
    with pytest.raises(ValueError):
        snapshots('{"timestamp":"not-a-time","event":"job_completed"}', events=True)


def test_only_fresh_saml_expiry_is_ignored():
    a = result('<Entity validUntil="2026-11-02T12:00:00Z"/>')
    b = result('<Entity validUntil="2026-11-02T12:00:01Z"/>')
    assert not compare(case("org saml sp-metadata"), a, b)
    b["stdout"] = b["stdout"].replace("12:00", "13:00")
    assert compare(case("org saml sp-metadata"), a, b)


def test_exit_and_download_mismatch_are_always_detected():
    a = result()
    b = deepcopy(a) | {"exit": 1, "download": {"sha256": "changed"}}
    assert compare(case("sbom download"), a, b) == ["exit", "download contents"]


@pytest.mark.parametrize(
    "path,args",
    [
        ("run cancel", ["run", "cancel", "run-fixture"]),
        ("job ssh", ["job", "ssh", "job-fixture"]),
        ("repo list", ["repo", "mirror", "create", "fixture"]),
        ("org saml test", ["org", "saml", "test"]),
        ("auth status", ["auth", "status", "--show-token=true"]),
        ("run view", ["run", "view", "--web"]),
    ],
)
def test_live_capture_rejects_writes_sessions_and_side_effect_flags(path, args):
    with pytest.raises(ValueError):
        validate_invocation(path, args)


def test_explicit_no_browser_saml_inspection_is_allowed():
    validate_invocation("org saml test", ["org", "saml", "test", "--no-browser"])
