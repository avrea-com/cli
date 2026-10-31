"""Protect the typed native request/recovery contract in differential validation."""

from copy import deepcopy
from scripts import compare_native_dispatch as native
import json
import pytest


def capture():
    raw = '{"workflow_id":"fixture","ref":"main","inputs":{"count":1.00000000000000001}}'
    return {
        "exit": 0,
        "stdout": "",
        "stderr": "Invocation key: " + native.KEY + "\n",
        "hosts": {},
        "browser": None,
        "mode": None,
        "requests": [
            {
                "method": "POST",
                "path": "/workflow-dispatches",
                "authorization": "Bearer fixture",
                "body": json.loads(raw),
                "raw_body": raw,
                "idempotency_key": native.KEY,
            }
        ],
    }


def scenario():
    return {"name": "fixture native dispatch", "args": ["workflow", "run", "fixture"]}


def test_native_comparator_checks_decimal_precision_beyond_float_resolution():
    python = capture()
    go = deepcopy(python)
    assert not native.compare(scenario(), python, go)
    go["requests"][0]["raw_body"] = go["requests"][0]["raw_body"].replace("1.00000000000000001", "1.0")
    assert "requests" in native.compare(scenario(), python, go)


@pytest.mark.parametrize("change", ["key", "retry", "auth", "type"])
def test_native_comparator_checks_key_binding_retry_body_auth_and_input_types(change):
    python = capture()
    go = deepcopy(python)
    request = go["requests"][0]
    if change == "key":
        request["idempotency_key"] = "a" * 32
    elif change == "retry":
        second = deepcopy(request)
        second["raw_body"] = second["raw_body"].replace('"ref":"main"', '"ref":"other"')
        go["requests"].append(second)
    elif change == "auth":
        request["authorization"] = "Bearer wrong"
    elif change == "type":
        request["raw_body"] = request["raw_body"].replace("1.00000000000000001", '"1.00000000000000001"')
    assert native.compare(scenario(), python, go)
