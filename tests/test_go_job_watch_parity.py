"""Keep job-watch normalization narrow enough to catch real differences."""

from copy import deepcopy
from scripts.compare_job_watch import compare


def result():
    return {
        "exit": 1,
        "hosts": None,
        "requests": [],
        "browser": None,
        "stdout": '{"data":[{"job_id":"job-fixture","platform_job_id":9007199254740993}]}\n',
        "stderr": "Error: rejected\n  API: https://api.example.test/jobs?states=queued&states=in_progress&limit=50\n",
    }


CASE = {"name": "job watch fixture", "args": ["job", "watch"], "exit": 1, "snapshots": True}


def test_query_order_and_json_key_order_are_immaterial():
    python = result()
    go = deepcopy(python)
    go["stdout"] = '{"data":[{"platform_job_id":9007199254740993,"job_id":"job-fixture"}]}\n'
    go["stderr"] = "Error: rejected\n  API: https://api.example.test/jobs?limit=50&states=queued&states=in_progress\n"
    assert compare(CASE, python, go) == []


def test_repeated_query_value_order_is_retained():
    python = result()
    go = deepcopy(python)
    go["stderr"] = go["stderr"].replace("states=queued&states=in_progress", "states=in_progress&states=queued")
    assert "stderr" in compare(CASE, python, go)


def test_snapshot_integer_and_escape_changes_fail():
    python = result()
    go = deepcopy(python)
    go["stdout"] = go["stdout"].replace("9007199254740993", "9007199254740992")
    assert "stdout" in compare(CASE, python, go)
    go = deepcopy(python)
    go["stdout"] += "\x1b[2J"
    assert "terminal escapes in snapshot stream" in compare(CASE, python, go)
