"""Unit tests for `avr workflow run` — workflow dispatch + filename resolution.

Covers the surface that previously had no test coverage: identifier resolution
(filename / wfl-id / display name / ambiguous / missing), raw-field and JSON
input handling, and the dispatch + poll handoff to the watch loop."""

from avrea_cli.commands.workflow import _resolve_workflow_filename
from avrea_cli.main import cli
from click.testing import CliRunner
from unittest.mock import MagicMock
import httpx
import pytest


@pytest.fixture()
def runner(monkeypatch):
    monkeypatch.setenv("AVR_TOKEN", "test-token")
    monkeypatch.setenv("AVR_ORG", "org-default")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.helpers.get_org_slug", lambda *a, **kw: "my-org")
    return CliRunner()


def _mock_dispatch_response(platform_run_id: int | None = 12345) -> dict:
    data = {"workflow": "build.yml", "ref": "main"}
    if platform_run_id is not None:
        data["platform_run_id"] = platform_run_id
    return {"data": data}


def _fake_get_for_dispatch(self, path: str, **kw):
    """Path-dispatching fake for the dispatch tests:

    - ``/orgs/.../repos/{repo_id}`` returns a single-repo object (used by
      :func:`_resolve_default_branch` and :func:`_resolve_repo_full_name`).
    - ``/orgs/.../workflow-runs`` returns the polling response shape.
    """
    if "/repos/rep-" in path and "/workflow-runs" not in path:
        return {"data": {"repository_id": "rep-foo", "full_name": "acme/svc", "default_branch": "main"}}
    return {"data": [{"run_id": "run-new", "platform_run_id": 12345}]}


class TestResolveWorkflowFilename:
    """`_resolve_workflow_filename` must accept a filename, an Avrea wfl-id,
    or a display name. Filename passthrough avoids a network round-trip."""

    def test_filename_passthrough_skips_lookup(self):
        client = MagicMock()
        result = _resolve_workflow_filename(client, "org-1", "rep-1", "build.yml")
        assert result.filename == "build.yml"
        # Filename passthrough never hits the API → no display name available.
        assert result.display_name is None
        client.public_get.assert_not_called()

    def test_resolves_wfl_id_to_filename(self):
        client = MagicMock()
        client.public_get.return_value = {
            "data": [
                {"workflow_id": "wfl-abc", "name": "Build", "path": ".github/workflows/build.yml"},
                {"workflow_id": "wfl-xyz", "name": "Deploy", "path": ".github/workflows/deploy.yml"},
            ]
        }
        result = _resolve_workflow_filename(client, "org-1", "rep-1", "wfl-abc")
        assert result.filename == "build.yml"
        assert result.display_name == "Build"

    def test_wfl_id_not_found_raises(self):
        client = MagicMock()
        client.public_get.return_value = {"data": []}
        import click

        with pytest.raises(click.ClickException, match="not found"):
            _resolve_workflow_filename(client, "org-1", "rep-1", "wfl-missing")

    def test_resolves_display_name_case_insensitive(self):
        client = MagicMock()
        client.public_get.return_value = {
            "data": [{"workflow_id": "wfl-1", "name": "Build and Deploy", "path": ".github/workflows/cicd.yml"}]
        }
        result = _resolve_workflow_filename(client, "org-1", "rep-1", "build and deploy")
        assert result.filename == "cicd.yml"
        # Surfacing the canonical name lets the caller echo "Triggered Build
        # and Deploy (cicd.yml)" so a fuzzy match is auditable.
        assert result.display_name == "Build and Deploy"

    def test_falls_back_to_filename_match_without_extension(self):
        """`avr workflow run ci` should match a workflow at `.github/workflows/ci.yml`
        when the display name doesn't match — saves typing the .yml suffix."""
        client = MagicMock()
        client.public_get.return_value = {
            "data": [{"workflow_id": "wfl-1", "name": "Continuous Integration", "path": ".github/workflows/ci.yml"}]
        }
        result = _resolve_workflow_filename(client, "org-1", "rep-1", "ci")
        assert result.filename == "ci.yml"
        assert result.display_name == "Continuous Integration"

    def test_ambiguous_name_raises(self):
        client = MagicMock()
        client.public_get.return_value = {
            "data": [
                {"workflow_id": "wfl-1", "name": "Build", "path": ".github/workflows/build.yml"},
                {"workflow_id": "wfl-2", "name": "Build", "path": ".github/workflows/build-old.yml"},
            ]
        }
        import click

        with pytest.raises(click.ClickException, match="Ambiguous"):
            _resolve_workflow_filename(client, "org-1", "rep-1", "Build")

    def test_unknown_identifier_raises(self):
        client = MagicMock()
        client.public_get.return_value = {"data": []}
        import click

        with pytest.raises(click.ClickException, match="not found"):
            _resolve_workflow_filename(client, "org-1", "rep-1", "nonexistent")

    def test_null_path_raises_explicit(self):
        """A workflow row without a `path` (disabled / mid-rename) must
        raise with an actionable hint instead of dispatching workflow="
        which the GitHub endpoint rejects with an opaque 422."""
        import click

        client = MagicMock()
        client.public_get.return_value = {
            "data": [{"workflow_id": "wfl-1", "name": "Build", "path": None}],
        }

        with pytest.raises(click.ClickException, match="no file path on disk"):
            _resolve_workflow_filename(client, "org-1", "rep-1", "wfl-1")

    def test_empty_string_path_raises_same(self):
        """Same shape: empty string ``path`` is treated as 'not on disk'."""
        import click

        client = MagicMock()
        client.public_get.return_value = {
            "data": [{"workflow_id": "wfl-1", "name": "Build", "path": ""}],
        }

        with pytest.raises(click.ClickException, match="no file path on disk"):
            _resolve_workflow_filename(client, "org-1", "rep-1", "wfl-1")


class TestWorkflowRunDispatch:
    """End-to-end of `avr workflow run`: dispatch payload, poll, success message."""

    def test_dispatches_filename_with_default_ref(self, runner, monkeypatch):
        post_calls: list[tuple[str, dict]] = []

        def fake_post(self, path, **kw):
            post_calls.append((path, kw.get("json", {})))
            return _mock_dispatch_response()

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", fake_post)
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _fake_get_for_dispatch)

        result = runner.invoke(cli, ["workflow", "run", "build.yml", "--repo", "rep-foo", "--no-watch"])

        assert result.exit_code == 0, result.output
        assert post_calls == [
            (
                "/orgs/org-default/repos/rep-foo/dispatch-workflow",
                {"workflow": "build.yml", "ref": "main"},
            )
        ]
        assert "Triggered" in result.output
        assert "build.yml" in result.output
        # --no-watch returns immediately after dispatch; no waiting on the
        # avrea webhook to land.
        assert "GitHub run id: 12345" in result.output
        assert "avr run list" in result.output

    def test_raw_fields_become_inputs(self, runner, monkeypatch):
        post_bodies: list[dict] = []

        def fake_post(self, path, **kw):
            post_bodies.append(kw.get("json", {}))
            return _mock_dispatch_response()

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", fake_post)
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _fake_get_for_dispatch)

        result = runner.invoke(
            cli,
            [
                "workflow",
                "run",
                "build.yml",
                "--repo",
                "rep-foo",
                "-f",
                "env=prod",
                "-f",
                "region=eu",
                "--no-watch",
            ],
        )

        assert result.exit_code == 0, result.output
        assert post_bodies[0]["inputs"] == {"env": "prod", "region": "eu"}

    def test_invalid_raw_field_format_errors(self, runner, monkeypatch):
        # Both -f variants (missing = and empty key) must be rejected before any
        # network call — guarded by `_parse_raw_fields`.
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda *a, **kw: pytest.fail("dispatch should not be called"),
        )

        result_no_eq = runner.invoke(cli, ["workflow", "run", "build.yml", "--repo", "rep-foo", "-f", "noequals"])
        assert result_no_eq.exit_code != 0
        assert "Invalid -f value" in result_no_eq.output

        result_empty_key = runner.invoke(cli, ["workflow", "run", "build.yml", "--repo", "rep-foo", "-f", "=val"])
        assert result_empty_key.exit_code != 0
        assert "key is empty" in result_empty_key.output

    def test_json_inputs_from_stdin(self, runner, monkeypatch):
        post_bodies: list[dict] = []

        def fake_post(self, path, **kw):
            post_bodies.append(kw.get("json", {}))
            return _mock_dispatch_response()

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", fake_post)
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _fake_get_for_dispatch)

        result = runner.invoke(
            cli,
            ["workflow", "run", "build.yml", "--repo", "rep-foo", "--json", "--no-watch"],
            input='{"env": "prod", "region": "eu"}',
        )

        assert result.exit_code == 0, result.output
        assert post_bodies[0]["inputs"] == {"env": "prod", "region": "eu"}

    def test_raw_fields_and_json_are_mutually_exclusive(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda *a, **kw: pytest.fail("dispatch should not be called"),
        )
        result = runner.invoke(
            cli,
            ["workflow", "run", "build.yml", "--repo", "rep-foo", "-f", "k=v", "--json"],
            input="{}",
        )
        assert result.exit_code != 0
        assert "not both" in result.output

    def test_no_watch_skips_poll(self, runner, monkeypatch):
        """--no-watch returns immediately after dispatch without polling for
        the avrea run record. Pass --ref explicitly so the default-branch
        lookup doesn't fire."""
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, **kw: _mock_dispatch_response(),
        )

        poll_called = {"v": False}

        def fake_get(self, path, **kw):
            if "/workflow-runs" in path:
                poll_called["v"] = True
            return {"data": []}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

        result = runner.invoke(
            cli,
            ["workflow", "run", "build.yml", "--repo", "rep-foo", "--ref", "main", "--no-watch"],
        )

        assert result.exit_code == 0, result.output
        assert "GitHub run id: 12345" in result.output
        assert poll_called["v"] is False

    def test_dispatch_http_error_is_handled(self, runner, monkeypatch):
        """A 4xx from the dispatch endpoint should surface a friendly error,
        not a stack trace."""

        def fake_post(self, path, **kw):
            req = httpx.Request("POST", "https://api.example/dispatch")
            raise httpx.HTTPStatusError(
                "403",
                request=req,
                response=httpx.Response(403, request=req, json={"detail": "forbidden"}),
            )

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", fake_post)

        result = runner.invoke(cli, ["workflow", "run", "build.yml", "--repo", "rep-foo"])
        assert result.exit_code != 0
