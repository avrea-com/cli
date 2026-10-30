"""Test the input audit parser without mistaking help or negative cases for execution."""

from scripts import command_cases as catalog
from scripts.compare_option_aliases import replace_option


def test_option_values_that_look_like_commands_are_not_commands():
    result = catalog.invocation(["run", "list", "--branch", "view", "--json", "*"])
    assert result.path == "avr run list"
    assert result.arguments == []
    assert [(o.spelling, o.value) for o in result.options] == [("--branch", "view"), ("--json", "*")]


def test_scoped_short_options_and_inherited_globals():
    result = catalog.invocation(["-v", "workflow", "run", "build", "-f", "key=value", "-r", "branch", "--no-color"])
    assert result.path == "avr workflow run"
    assert result.arguments == ["build"]
    assert [(o.path, o.spelling) for o in result.options] == [
        ("avr", "-v"),
        ("avr workflow run", "-f"),
        ("avr workflow run", "-r"),
        ("avr", "--no-color"),
    ]


def test_inline_values_short_clusters_and_aliases():
    result = catalog.invocation(["repos", "list", "-L2", "--json=*", "-q.[0]"])
    assert result.path == "avr repo list"
    assert [(o.spelling, o.value, o.index) for o in result.options] == [
        ("-L", "2", 2),
        ("--json", "*", 3),
        ("-q", ".[0]", 4),
    ]
    cluster = catalog.invocation(["-vV"])
    assert [o.spelling for o in cluster.options] == ["-v", "-V"]
    assert all(o.cluster for o in cluster.options)


def test_separator_keeps_passthrough_flags_as_arguments():
    result = catalog.invocation(["job", "ssh", "123", "--", "sh", "-v", "-c", "echo hi"])
    assert result.path == "avr job ssh"
    assert result.options == []
    assert result.arguments == ["123", "sh", "-v", "-c", "echo hi"]


def test_discovery_and_errors_are_not_counted_as_successful_usage():
    for args, code in [
        (["job", "list", "--org", "acme", "--help"], 0),
        (["job", "list", "--org", "acme", "--json", "?"], 0),
        (["job", "list", "--org", "missing"], 1),
        (["--version"], 0),
    ]:
        assert not catalog.operational({"exit": code}, catalog.invocation(args))
    assert catalog.operational({}, catalog.invocation(["job", "list", "--org", "acme"]))


def test_option_replacement_preserves_literal_values():
    args = ["repo", "list", "--json=*", "-q", "-leading"]
    parsed = catalog.invocation(args)
    assert replace_option(args, parsed.options[0], "--json") == ["repo", "list", "--json", "*", "-q", "-leading"]
    assert replace_option(args, parsed.options[1], "--jq") == ["repo", "list", "--json=*", "--jq", "-leading"]
    assert args == ["repo", "list", "--json=*", "-q", "-leading"]


def test_derived_aliases_keep_command_arguments_and_option_meaning():
    from scripts.compare_option_aliases import scenarios

    nodes = catalog.flatten(catalog.contract()["root"])

    def meaning(option):
        parameter = catalog.option_map(nodes[option.path])[option.spelling]
        return (option.path, option.parameter, option.value, option.spelling in parameter["secondary_opts"])

    sources = {
        (suite, case["name"]): case
        for suite, _, cases in catalog.executable_suites(include_derived=False)
        for case in cases
    }
    for derived in scenarios():
        original = sources[(derived["alias_suite"], derived["alias_original_name"])]
        before = catalog.invocation(original["args"])
        after = catalog.invocation(derived["args"])
        assert before.path == after.path
        assert before.arguments == after.arguments
        assert [meaning(option) for option in before.options] == [meaning(option) for option in after.options]
        assert original["args"] != derived["args"]
