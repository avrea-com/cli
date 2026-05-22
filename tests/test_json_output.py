"""Unit tests for json_output helpers + the run-list integration."""

from avrea_cli.json_output import filter_with_jq
from avrea_cli.json_output import get_path
from avrea_cli.json_output import select_fields
from avrea_cli.json_output import split_fields
from avrea_cli.main import cli
from click.testing import CliRunner
import click
import json
import pytest
import shutil

SAMPLE_SCHEMA = {
    "id": "run_id",
    "status": "status",
    "branch": "head_branch",
    "workflowName": "workflow.name",
}


class TestSplitFields:
    def test_comma_separated(self):
        assert split_fields("id,status", SAMPLE_SCHEMA) == ["id", "status"]

    def test_strips_whitespace(self):
        assert split_fields("id, status , branch", SAMPLE_SCHEMA) == ["id", "status", "branch"]

    def test_drops_empty_segments(self):
        assert split_fields("id,,status", SAMPLE_SCHEMA) == ["id", "status"]

    def test_star_expands_to_all(self):
        """`*` is the 'all fields' shortcut."""
        assert split_fields("*", SAMPLE_SCHEMA) == sorted(SAMPLE_SCHEMA)


class TestGetPath:
    def test_flat(self):
        assert get_path({"a": 1}, "a") == 1

    def test_dotted_nested(self):
        assert get_path({"a": {"b": "x"}}, "a.b") == "x"

    def test_missing_returns_none(self):
        assert get_path({"a": {}}, "a.b") is None
        assert get_path({}, "anything") is None

    def test_non_dict_returns_none(self):
        """Walking into a non-dict (list, scalar) yields None rather than crashing."""
        assert get_path({"a": [1, 2]}, "a.b") is None


class TestSelectFields:
    def test_pluck_subset(self):
        records = [{"run_id": "r1", "head_branch": "main", "status": "completed"}]
        result = select_fields(records, ["id", "branch"], SAMPLE_SCHEMA)
        assert result == [{"id": "r1", "branch": "main"}]

    def test_resolves_nested_paths(self):
        records = [{"run_id": "r1", "workflow": {"name": "CI"}}]
        result = select_fields(records, ["workflowName"], SAMPLE_SCHEMA)
        assert result == [{"workflowName": "CI"}]

    def test_unknown_field_raises_with_available_list(self):
        with pytest.raises(click.ClickException, match="Available:"):
            select_fields([{}], ["bogus"], SAMPLE_SCHEMA)


@pytest.mark.skipif(shutil.which("jq") is None, reason="jq not installed in this environment")
class TestFilterWithJq:
    """Run only when jq is available — otherwise skip rather than vendor a stub."""

    def test_simple_expression(self):
        out = filter_with_jq([{"a": 1}, {"a": 2}], ".[0].a")
        assert out.strip() == "1"

    def test_jq_error_raises_click_exception(self):
        with pytest.raises(click.ClickException, match="jq error"):
            filter_with_jq([{"a": 1}], "this is not valid jq")


# ---------------------------------------------------------------------------
# Run-list integration: pin the --json fields contract end-to-end.
# ---------------------------------------------------------------------------


SAMPLE_RUNS = {
    "data": [
        {
            "run_id": "run-1",
            "platform_run_id": 12345,
            "display_title": "Build",
            "status": "completed",
            "conclusion": "success",
            "head_branch": "main",
            "head_sha": "abc",
            "event": "push",
            "run_number": 1,
            "run_attempt": 1,
            "duration_seconds": 60,
            "workflow_id": "wfl-x",
            "workflow": {"name": "CI"},
            "repository": {"full_name": "acme/cool"},
            "triggering_actor": {"login": "alice"},
        }
    ],
    "pagination": {"next_cursor": None},
}


@pytest.fixture()
def runner(monkeypatch):
    monkeypatch.setenv("AVR_TOKEN", "test-token")
    monkeypatch.setenv("AVR_ORG", "org-default")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, **kw: SAMPLE_RUNS,
    )
    return CliRunner()


class TestRunListJson:
    def test_field_subset(self, runner):
        result = runner.invoke(cli, ["run", "list", "--json", "status,head_branch,workflow"])
        assert result.exit_code == 0, result.output
        data = json.loads(result.output)
        assert data == [{"status": "completed", "head_branch": "main", "workflow": {"name": "CI"}}]

    def test_snake_case_schema(self, runner):
        """Schema keys mirror the API's snake_case wire names so users don't
        re-translate when piping `avr run list --json` into a jq pipeline
        that also reads from `/orgs/.../workflow-runs` directly."""
        result = runner.invoke(cli, ["run", "list", "--json", "head_branch,repository,triggering_actor"])
        assert result.exit_code == 0, result.output
        data = json.loads(result.output)
        assert data == [
            {
                "head_branch": "main",
                "repository": {"full_name": "acme/cool"},
                "triggering_actor": {"login": "alice"},
            }
        ]

    def test_question_mark_lists_fields(self, runner):
        result = runner.invoke(cli, ["run", "list", "--json", "?"])
        assert result.exit_code == 0
        # Locked-in copy — agents grep for this exact wording when discovering fields.
        assert "Specify one or more comma-separated fields" in result.output
        assert "head_branch" in result.output

    def test_star_includes_every_field(self, runner):
        result = runner.invoke(cli, ["run", "list", "--json", "*"])
        assert result.exit_code == 0, result.output
        data = json.loads(result.output)
        assert "run_id" in data[0]
        assert "workflow" in data[0]

    def test_jq_without_json_errors(self, runner):
        result = runner.invoke(cli, ["run", "list", "-q", "."])
        assert result.exit_code != 0
        assert "--jq requires --json" in result.output

    def test_unknown_field(self, runner):
        result = runner.invoke(cli, ["run", "list", "--json", "bogus"])
        assert result.exit_code != 0
        assert "Unknown JSON field" in result.output

    @pytest.mark.skipif(shutil.which("jq") is None, reason="jq not installed")
    def test_jq_pipes_correctly(self, runner):
        """`-q/--jq` runs jq with `-r` so string outputs are unquoted —
        matches gh's convention and lets `xargs` consume IDs without first
        stripping JSON quotes."""
        result = runner.invoke(cli, ["run", "list", "--json", "status", "-q", ".[0].status"])
        assert result.exit_code == 0
        assert result.output.strip() == "completed"
