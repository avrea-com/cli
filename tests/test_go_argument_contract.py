"""Parser parity permits wording changes without accepting a different failure."""

from copy import deepcopy
from scripts.compare_argument_contract import compare
import pytest


def result(message):
    return {"exit": 2, "stdout": "", "stderr": message, "requests": [], "hosts": None, "browser": None}


def test_missing_argument_wording_can_differ():
    case = {"parser_kind": "missing_argument", "parser_flags": []}
    python = result("Usage: avr job view [OPTIONS] JOB_ID\nError: Missing argument 'JOB_ID'.\n")
    go = result("accepts 1 arg(s), received 0\n")
    assert compare(case, python, go) == []


def test_required_option_wording_can_differ():
    case = {"parser_kind": "missing_option", "parser_flags": ["--url"]}
    assert compare(case, result("Error: Missing option '--url'."), result("Error: --url and --repo are required")) == []


@pytest.mark.parametrize(
    "key,value",
    [
        ("exit", 0),
        ("stdout", "continued"),
        ("requests", [{"method": "POST", "path": "/unexpected"}]),
        ("hosts", {"token": "fixture"}),
        ("browser", "https://example.test"),
    ],
)
def test_parser_rejection_cannot_have_side_effects(key, value):
    case = {"parser_kind": "missing_value", "parser_flags": ["--limit"]}
    baseline = result("Option '--limit' requires an argument.")
    broken = deepcopy(baseline)
    broken[key] = value
    assert compare(case, baseline, broken)


def test_correct_option_and_error_category_are_both_required():
    case = {"parser_kind": "invalid_value", "parser_flags": ["--limit", "-L"]}
    baseline = result("Error: Invalid value for '--limit': 0 is not in the range.")
    for message in ["", "Error: Missing option '--limit'.", "Error: Invalid value for '--other'."]:
        assert compare(case, baseline, result(message))
