"""Tests for provider-neutral and GitLab installation commands."""

from avrea_cli.main import cli
import click
import httpx
import json
import pytest

GITLAB_INSTALLATION = {
    "installation_id": "ins-gitlab",
    "organization_id": "org-default",
    "gitlab_namespace_id": 4242,
    "gitlab_namespace_full_path": "acme/platform",
    "target_type": "Group",
    "state": "ACTIVE",
    "credential_kind": "GROUP_ACCESS_TOKEN",
    "access_token_expires_on": "2027-08-13",
    "access_token_last_validated_at": "2026-08-13T12:00:00Z",
    "access_token_invalid_at": None,
    "webhook_secret_rotated_at": "2026-08-13T12:00:00Z",
    "configured_runner_count": 0,
    "degraded_runner_count": 0,
    "project_selection_mode": "SELECTED",
    "selected_project_count": 2,
    "gitlab_project_ids": [101, 202],
    "created_at": "2026-08-13T12:00:00Z",
    "updated_at": "2026-08-13T12:00:00Z",
}
GITLAB_WRITE_RESULT = {
    **GITLAB_INSTALLATION,
    "webhook_setup": {
        "url": "https://api.avrea.com/webhooks/gitlab/installations/ins-gitlab",
        "secret": "one-time-webhook-secret",
        "secret_header": "X-Gitlab-Token",
        "enabled_triggers": ["push_events", "merge_request_events", "job_events", "project_events"],
        "ssl_verification": True,
        "unlisted_triggers_enabled": False,
        "warning": "Rotating credentials or re-registering invalidates the previous webhook secret.",
    },
}


def test_install_add_defaults_to_github(runner, monkeypatch):
    requests = []

    def mock_get(self, path, params=None):
        requests.append(path)
        return {"data": []}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
    monkeypatch.setattr("avrea_cli.commands.org.webbrowser.open", lambda url: pytest.fail(f"opened {url}"))

    result = runner.invoke(cli, ["org", "install", "add", "--no-browser", "--wait-seconds", "0"])

    assert result.exit_code == 0, result.output
    assert requests == ["/users/me/installations"]
    assert "oauth/github/install?organization_id=org-default" in result.output


def test_install_add_github_json_emits_detected_installations_only_on_stdout(runner, monkeypatch):
    detected = {
        "installation_id": "ins-github",
        "platform": "github",
        "platform_installation_id": 77,
        "target_name": "acme",
        "state": "ACTIVE",
    }
    responses = iter([{"data": []}, {"data": [detected]}])
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: next(responses))
    monkeypatch.setattr("avrea_cli.commands.org.time.sleep", lambda seconds: None)

    result = runner.invoke(
        cli,
        ["org", "install", "add", "--no-browser", "--wait-seconds", "5", "--json", "installation_id,platform"],
    )

    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout) == [{"installation_id": "ins-github", "platform": "github"}]
    assert "GitHub installation URL" in result.stderr
    assert "Waiting for installation" in result.stderr


def test_install_add_github_json_without_detection_emits_empty_list(runner, monkeypatch):
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: {"data": []})
    clock = iter([0.0, 0.0, 10.0, 10.0])
    monkeypatch.setattr("avrea_cli.commands.org.time.time", lambda: next(clock))
    monkeypatch.setattr("avrea_cli.commands.org.time.sleep", lambda seconds: None)

    result = runner.invoke(cli, ["org", "install", "add", "--no-browser", "--wait-seconds", "5", "--json", "*"])

    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout) == []
    assert "not detected yet" in result.stderr


def test_install_add_github_json_needs_a_wait(runner):
    result = runner.invoke(cli, ["org", "install", "add", "--no-browser", "--wait-seconds", "0", "--json", "*"])

    assert result.exit_code != 0
    assert "--json needs a positive --wait-seconds" in result.output


def test_install_add_gitlab_reads_token_from_stdin_and_prints_webhook_setup(runner, monkeypatch):
    captured = {}

    def mock_post(self, path, json=None, timeout=None, **kwargs):
        captured.update(path=path, json=json, timeout=timeout)
        return GITLAB_WRITE_RESULT

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)
    result = runner.invoke(
        cli,
        [
            "org",
            "install",
            "add",
            "--platform",
            "gitlab",
            "--group-full-path",
            " acme/platform ",
            "--project-selection",
            "selected",
            "--project-id",
            "101",
            "--project-id",
            "202",
            "--access-token-stdin",
        ],
        input="glpat-registration-secret\n",
    )

    assert result.exit_code == 0, result.output
    assert captured == {
        "path": "/orgs/org-default/gitlab-installations",
        "json": {
            "group_full_path": "acme/platform",
            "access_token": "glpat-registration-secret",
            "project_selection_mode": "SELECTED",
            "gitlab_project_ids": [101, 202],
        },
        "timeout": 70.0,
    }
    assert "glpat-registration-secret" not in result.output
    assert "one-time-webhook-secret" in result.output
    assert "X-Gitlab-Token" in result.output
    assert "push_events, merge_request_events, job_events, project_events" in result.output
    assert "Avrea cannot show it again" in result.output


def test_install_add_gitlab_uses_hidden_prompt_and_all_selection(runner, monkeypatch):
    captured = {}

    def mock_post(self, path, json=None, timeout=None, **kwargs):
        captured["json"] = json
        return {
            **GITLAB_WRITE_RESULT,
            "project_selection_mode": "ALL",
            "selected_project_count": 0,
            "gitlab_project_ids": [],
        }

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)
    result = runner.invoke(
        cli,
        [
            "org",
            "install",
            "add",
            "--platform",
            "gitlab",
            "--group-full-path",
            "acme/platform",
            "--project-selection",
            "all",
        ],
        input="glpat-hidden-secret\n",
    )

    assert result.exit_code == 0, result.output
    assert captured["json"]["access_token"] == "glpat-hidden-secret"
    assert captured["json"]["gitlab_project_ids"] == []
    assert "glpat-hidden-secret" not in result.output
    assert "GitLab group access token" in result.output


@pytest.mark.parametrize(
    ("extra_args", "message"),
    [
        ([], "--group-full-path is required"),
        (["--group-full-path", "acme/platform"], "--project-selection is required"),
        (
            [
                "--group-full-path",
                "acme/platform",
                "--project-selection",
                "all",
                "--project-id",
                "101",
            ],
            "--project-id cannot be used",
        ),
        (
            ["--group-full-path", "acme/platform", "--project-selection", "all", "--wait-seconds", "120"],
            "--wait-seconds cannot be used",
        ),
    ],
)
def test_install_add_gitlab_validates_conditional_options(runner, extra_args, message):
    result = runner.invoke(cli, ["org", "install", "add", "--platform", "gitlab", *extra_args])

    assert result.exit_code == 2
    assert message in result.output


def test_install_add_rejects_gitlab_options_for_github(runner):
    result = runner.invoke(cli, ["org", "install", "add", "--group-full-path", "acme/platform"])

    assert result.exit_code == 2
    assert "--group-full-path can only be used with --platform gitlab" in result.output


def test_install_add_gitlab_hidden_prompt_honors_prompt_disabled(runner, monkeypatch):
    monkeypatch.setenv("AVR_PROMPT_DISABLED", "1")

    result = runner.invoke(
        cli,
        [
            "org",
            "install",
            "add",
            "--platform",
            "gitlab",
            "--group-full-path",
            "acme/platform",
            "--project-selection",
            "all",
        ],
    )

    assert result.exit_code == 1
    assert "AVR_PROMPT_DISABLED" in result.output
    assert "--access-token-stdin" in result.output


def test_install_add_rejects_more_than_one_thousand_project_ids(runner):
    args = [
        "org",
        "install",
        "add",
        "--platform",
        "gitlab",
        "--group-full-path",
        "acme/platform",
        "--project-selection",
        "selected",
    ]
    for project_id in range(1, 1002):
        args.extend(("--project-id", str(project_id)))

    result = runner.invoke(cli, args)

    assert result.exit_code == 2
    assert "at most 1,000 times" in result.output


def test_install_set_selection_allows_empty_selected_set_and_uses_long_timeout(runner, monkeypatch):
    captured = {}

    def mock_patch(self, path, json=None, timeout=None):
        captured.update(path=path, json=json, timeout=timeout)
        return {
            **GITLAB_INSTALLATION,
            "project_selection_mode": "SELECTED",
            "selected_project_count": 0,
            "gitlab_project_ids": [],
        }

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_patch", mock_patch)
    result = runner.invoke(
        cli,
        [
            "org",
            "install",
            "set-selection",
            "--installation-id",
            "ins-gitlab",
            "--project-selection",
            "selected",
        ],
    )

    assert result.exit_code == 0, result.output
    assert captured == {
        "path": "/orgs/org-default/gitlab-installations/ins-gitlab",
        "json": {"project_selection_mode": "SELECTED", "gitlab_project_ids": []},
        "timeout": 70.0,
    }
    assert "GitLab project IDs" in result.output
    assert "none" in result.output


def test_install_rotate_json_preserves_nested_one_time_webhook_setup(runner, monkeypatch):
    captured = {}

    def mock_post(self, path, json=None, timeout=None, **kwargs):
        captured.update(path=path, json=json, timeout=timeout)
        return GITLAB_WRITE_RESULT

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)
    result = runner.invoke(
        cli,
        [
            "org",
            "install",
            "rotate",
            "--installation-id",
            "ins-gitlab",
            "--access-token-stdin",
            "--json",
            "installation_id,webhook_setup",
        ],
        input="glpat-rotation-secret\n",
    )

    assert result.exit_code == 0, result.output
    assert captured == {
        "path": "/orgs/org-default/gitlab-installations/ins-gitlab/credential-rotations",
        "json": {"access_token": "glpat-rotation-secret"},
        "timeout": 70.0,
    }
    assert json.loads(result.output) == {
        "installation_id": "ins-gitlab",
        "webhook_setup": GITLAB_WRITE_RESULT["webhook_setup"],
    }
    assert "glpat-rotation-secret" not in result.output


def test_install_gitlab_timeout_is_reported_without_traceback_or_token(runner, monkeypatch):
    def mock_post(self, path, json=None, timeout=None, **kwargs):
        raise httpx.ReadTimeout("slow", request=httpx.Request("POST", f"https://api.avrea.com{path}"))

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", mock_post)
    result = runner.invoke(
        cli,
        [
            "org",
            "install",
            "rotate",
            "--installation-id",
            "ins-gitlab",
            "--access-token-stdin",
        ],
        input="glpat-timeout-secret\n",
    )

    assert result.exit_code == 1
    assert "Timed out while trying to rotate GitLab installation credentials" in result.output
    assert "glpat-timeout-secret" not in result.output
    assert "Traceback" not in result.output


@pytest.mark.parametrize(
    ("status", "detail", "expected"),
    [
        (404, "Not Found", "GitLab is not enabled for this organization"),
        (409, "The GitLab access token must be rotated", "access token must be rotated"),
        (422, "Selected project is outside the group", "Selected project is outside the group"),
        (429, "Too Many Requests", "rate-limiting requests"),
        (503, "GitLab is temporarily unavailable", "Avrea is having trouble"),
    ],
)
def test_install_set_selection_formats_api_errors(runner, monkeypatch, status, detail, expected):
    def mock_patch(self, path, json=None, timeout=None):
        request = httpx.Request("PATCH", f"https://api.avrea.com{path}")
        response = httpx.Response(status, request=request, json={"detail": detail})
        raise httpx.HTTPStatusError(str(status), request=request, response=response)

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_patch", mock_patch)
    result = runner.invoke(
        cli,
        [
            "org",
            "install",
            "set-selection",
            "--installation-id",
            "ins-gitlab",
            "--project-selection",
            "all",
        ],
    )

    assert result.exit_code == 1
    assert expected in result.output
    assert "Traceback" not in result.output


def test_install_list_displays_mixed_platforms(runner, monkeypatch):
    installations = [
        {
            "installation_id": "ins-github",
            "platform": "github",
            "platform_installation_id": "9001",
            "target_id": 77,
            "target_type": "Organization",
            "target_name": "octo",
            "organization_name": "Acme",
            "organization_slug": "acme",
            "platform_settings_url": "https://github.com/organizations/octo/settings/installations/9001",
            "state": "ACTIVE",
            "created_at": "2026-08-13T12:00:00Z",
        },
        {
            "installation_id": "ins-gitlab",
            "platform": "gitlab",
            "platform_installation_id": None,
            "target_id": 4242,
            "target_type": "Group",
            "target_name": "acme/platform",
            "organization_name": "Acme",
            "organization_slug": "acme",
            "platform_settings_url": "https://gitlab.com/groups/acme/platform/-/settings/general",
            "state": "ACTIVE",
            "created_at": "2026-08-13T12:00:00Z",
        },
    ]
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {"data": installations},
    )

    result = runner.invoke(cli, ["org", "install", "list"])

    assert result.exit_code == 0, result.output
    assert "Platform" in result.output
    assert "Provider ID" in result.output
    assert "GitHub" in result.output and "9001" in result.output
    assert "GitLab" in result.output and "4242" in result.output


def test_github_add_polling_ignores_gitlab_rows(runner, monkeypatch):
    responses = iter(
        [
            {"data": []},
            {
                "data": [
                    {
                        "platform": "gitlab",
                        "platform_installation_id": None,
                        "installation_id": "ins-gitlab",
                    }
                ]
            },
        ]
    )
    monotonic_times = iter((0, 0, 6))
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: next(responses))
    monkeypatch.setattr("avrea_cli.commands.org.webbrowser.open", lambda url: True)
    monkeypatch.setattr("avrea_cli.commands.org.time.sleep", lambda seconds: None)
    monkeypatch.setattr("avrea_cli.commands.org.time.time", lambda: next(monotonic_times))

    result = runner.invoke(cli, ["org", "install", "add", "--wait-seconds", "5"])

    assert result.exit_code == 0, result.output
    assert "Detected new installation" not in result.output
    assert "Installation not detected yet" in result.output


@pytest.mark.parametrize(
    ("platform", "expected_action", "expected_output"),
    [
        ("github", "suspended", "Installation ins-test suspended."),
        ("gitlab", "disconnected", "Installation ins-test permanently disconnected."),
    ],
)
def test_install_remove_uses_provider_lifecycle(runner, monkeypatch, platform, expected_action, expected_output):
    paths = []

    def mock_get(self, path, params=None):
        paths.append(("GET", path))
        return {
            "installation_id": "ins-test",
            "organization_id": "org-default",
            "platform": platform,
        }

    def mock_delete(self, path, params=None):
        paths.append(("DELETE", path))
        return None

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", mock_delete)
    result = runner.invoke(
        cli,
        [
            "org",
            "install",
            "remove",
            "--installation-id",
            "ins-test",
            "--yes",
            "--json",
            "installation_id,platform,action",
        ],
    )

    assert result.exit_code == 0, result.output
    assert paths == [
        ("GET", "/orgs/org-default/installations/ins-test"),
        ("DELETE", "/orgs/org-default/installations/ins-test"),
    ]
    assert json.loads(result.output) == {
        "installation_id": "ins-test",
        "platform": platform,
        "action": expected_action,
    }

    human_result = runner.invoke(
        cli,
        ["org", "install", "remove", "--installation-id", "ins-test", "--yes"],
    )
    assert human_result.exit_code == 0, human_result.output
    assert expected_output in human_result.output
    if platform == "gitlab":
        assert "Remove the Avrea webhook, the group access token and the Avrea group runners" in human_result.output


def test_install_remove_gitlab_confirmation_warns_that_disconnect_is_permanent(runner, monkeypatch):
    deleted = []
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {
            "installation_id": "ins-gitlab",
            "organization_id": "org-default",
            "platform": "gitlab",
        },
    )
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_delete",
        lambda self, path, params=None: deleted.append(path),
    )

    result = runner.invoke(
        cli,
        ["org", "install", "remove", "--installation-id", "ins-gitlab"],
        input="n\n",
    )

    assert result.exit_code == 1
    assert "permanently disconnects" in result.output
    assert "removes its Avrea history" in result.output
    assert "install rotate" in result.output
    assert deleted == []


def test_install_remove_unwraps_the_data_envelope(runner, monkeypatch):
    deleted = []
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None: {
            "data": {"installation_id": "ins-test", "organization_id": "org-default", "platform": "github"}
        },
    )
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_delete", lambda self, path, params=None: deleted.append(path)
    )

    result = runner.invoke(cli, ["org", "install", "remove", "--installation-id", "ins-test", "--yes"])

    assert result.exit_code == 0, result.output
    assert deleted == ["/orgs/org-default/installations/ins-test"]
    assert "Installation ins-test suspended." in result.output


@pytest.mark.parametrize(
    "command",
    [
        ["org", "install", "rotate", "--installation-id", "ins-gitlab"],
        [
            "org",
            "install",
            "add",
            "--platform",
            "gitlab",
            "--group-full-path",
            "acme/platform",
            "--project-selection",
            "all",
        ],
    ],
)
def test_gitlab_write_json_without_webhook_setup_sends_no_request(runner, monkeypatch, command):
    """The field list is checked before the token is read and the request sent:
    once the server has committed, a projection without the one-time secret
    would already have lost it."""
    posts = []
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_post",
        lambda self, path, json=None, timeout=None: posts.append(path) or GITLAB_WRITE_RESULT,
    )

    result = runner.invoke(
        cli, command + ["--access-token-stdin", "--json", "installation_id"], input="glpat-rotation-secret\n"
    )

    assert result.exit_code == 2, result.output
    assert "--json must include webhook_setup" in result.output
    assert posts == []


@pytest.mark.parametrize("failure", ["runtime", "drops_secret", "interrupt"])
def test_gitlab_write_json_filter_failure_keeps_the_secret_on_stdout_only(runner, monkeypatch, failure):
    """After the rotation has committed, a jq failure falls back to the
    unfiltered record on stdout; jq's own diagnostic, which quotes its input,
    never reaches stderr."""
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_post", lambda self, path, json=None, timeout=None: GITLAB_WRITE_RESULT
    )

    def filter_with_jq(data, expr):
        if failure == "runtime":
            raise click.ClickException("jq error: Invalid numeric literal (while parsing 'one-time-webhook-secret')")
        if failure == "interrupt":
            raise KeyboardInterrupt
        return "ins-gitlab\n"

    monkeypatch.setattr("avrea_cli.json_output.filter_with_jq", filter_with_jq)

    result = runner.invoke(
        cli,
        [
            "org",
            "install",
            "rotate",
            "--installation-id",
            "ins-gitlab",
            "--access-token-stdin",
            "--json",
            "installation_id,webhook_setup",
            "--jq",
            ".webhook_setup.secret | tonumber",
        ],
        input="glpat-rotation-secret\n",
    )

    assert result.exit_code == 1
    assert json.loads(result.stdout)["webhook_setup"]["secret"] == "one-time-webhook-secret"
    assert result.stdout.count("one-time-webhook-secret") == 1
    assert "one-time-webhook-secret" not in result.stderr
    assert "credentials were rotated, but output failed" in result.stderr
    assert "unfiltered" in result.stderr
    assert "Invalid numeric literal" not in result.stderr
    assert ("must retain the one-time credential" in result.stderr) == (failure == "drops_secret")


@pytest.mark.parametrize("failure", ["server", "transport"])
def test_install_list_warns_and_blanks_counts_when_a_runner_summary_fails(runner, monkeypatch, failure):
    """The runner counts are an optional add-on: a 5xx or an unreachable server
    on one organization's listing leaves that organization's counts empty and
    warns, instead of losing the installations list already in hand."""

    def mock_get(self, path, params=None):
        if path == "/users/me/installations":
            return {
                "data": [
                    {"installation_id": "ins-github", "platform": "github", "organization_id": "org-default"},
                    {"installation_id": "ins-gitlab", "platform": "gitlab", "organization_id": "org-default"},
                ]
            }
        request = httpx.Request("GET", f"https://api.avrea.com{path}")
        if failure == "transport":
            raise httpx.ConnectError("refused", request=request)
        response = httpx.Response(503, request=request, json={"detail": "upstream"})
        raise httpx.HTTPStatusError("503", request=request, response=response)

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)

    result = runner.invoke(cli, ["org", "install", "list"])

    assert result.exit_code == 0, result.output
    assert "ins-github" in result.stdout
    assert "ins-gitlab" in result.stdout
    assert "None" not in result.stdout
    assert "Warning: runner counts for org-default are unavailable" in result.stderr
    assert ("HTTP 503" in result.stderr) == (failure == "server")


def test_install_list_stays_fatal_on_an_unauthorized_runner_summary(runner, monkeypatch):
    def mock_get(self, path, params=None):
        if path == "/users/me/installations":
            return {"data": [{"installation_id": "ins-gitlab", "platform": "gitlab", "organization_id": "org-default"}]}
        request = httpx.Request("GET", f"https://api.avrea.com{path}")
        response = httpx.Response(401, request=request, json={"detail": "expired"})
        raise httpx.HTTPStatusError("401", request=request, response=response)

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)

    result = runner.invoke(cli, ["org", "install", "list"])

    assert result.exit_code == 4


def test_install_list_survives_a_forbidden_runner_summary_and_blanks_github_counts(runner, monkeypatch):
    def mock_get(self, path, params=None):
        if path == "/users/me/installations":
            return {
                "data": [
                    {"installation_id": "ins-github", "platform": "github", "organization_id": "org-default"},
                    {"installation_id": "ins-gitlab", "platform": "gitlab", "organization_id": "org-default"},
                ]
            }
        request = httpx.Request("GET", f"https://api.avrea.com{path}")
        response = httpx.Response(403, request=request, json={"detail": "admin only"})
        raise httpx.HTTPStatusError("403", request=request, response=response)

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", mock_get)

    result = runner.invoke(cli, ["org", "install", "list"])

    assert result.exit_code == 0, result.output
    assert "Configured runners" in result.output
    assert "None" not in result.output
