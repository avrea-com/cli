"""Ensure option-value comparisons detect both implementations ignoring a flag."""

from scripts.compare_option_values import compare


def result(stdout, requests=None):
    return {
        "exit": 0,
        "stdout": stdout,
        "stderr": "",
        "hosts": None,
        "browser": None,
        "requests": requests or [],
    }


def test_jq_must_transform_even_when_outputs_agree():
    case = {
        "name": "fixture",
        "origin_name": "settings schema JSON",
        "origin_suite": "settings",
        "args": ["settings", "schema", "--json", "*", "--jq", '{"kind":type,"value":.}'],
        "json": True,
        "jq_wrapper": True,
    }
    unchanged = result('[{"key":"runner.memory"}]')
    assert len(compare(case, unchanged, unchanged)) == 2
    transformed = result('{"kind":"array","value":[{"key":"runner.memory"}]}')
    assert compare(case, transformed, transformed) == []


def test_source_must_affect_repository_lookup():
    case = {
        "name": "fixture",
        "origin_name": "git mirror status JSON",
        "origin_suite": "git_mirrors",
        "args": ["repo", "mirror", "status", "--source", "gitlab"],
        "json": True,
        "source_platform": "gitlab",
    }
    unchanged = result("{}", [{"method": "GET", "path": "/orgs/fixture/repos/resolve?platform=github"}])
    assert len(compare(case, unchanged, unchanged)) == 2
    selected = result("{}", [{"method": "GET", "path": "/orgs/fixture/repos/resolve?platform=gitlab"}])
    assert compare(case, selected, selected) == []
