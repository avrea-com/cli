"""Keep version normalization narrow and reject global-option side effects."""

from copy import deepcopy
from scripts.compare_global import compare
import pytest


def version(text="avr version 0.5.0 (development)\n"):
    return {"exit": 0, "stdout": text, "stderr": "", "requests": [], "hosts": None, "browser": None}


def test_version_allows_build_metadata_and_program_name_only():
    case = {"version_output": True}
    assert compare(case, version(), version("avr-admin version 0.5.0-go-preview+fixture\n")) == []
    for text in ["", "avr 0.5.0\n", "avr version \n", "avr version 0.5.0\nextra\n"]:
        assert compare(case, version(), version(text))


@pytest.mark.parametrize(
    "key,value",
    [
        ("exit", 1),
        ("stderr", "Warning: leaked\n"),
        ("requests", [{"method": "GET", "path": "/users/me"}]),
        ("hosts", {"default_host": "unexpected"}),
        ("browser", "https://example.test"),
    ],
)
def test_version_is_offline_and_quiet(key, value):
    changed = deepcopy(version())
    changed[key] = value
    assert compare({"version_output": True}, version(), changed)
