"""Python-versus-Go inspection checks using the pinned executable oracle."""

from datetime import datetime
from scripts import compare_auth as harness
from urllib.parse import parse_qsl
from urllib.parse import urlencode
from urllib.parse import urlsplit
from urllib.parse import urlunsplit
import decimal
import json
import re
import sys

ORG = "org-019a0000000070008000000000000001"
REPO = "rep-019a0000000070008000000000000002"
ORGANIZATIONS = [{"organization_id": ORG, "name": "Acme", "slug": "acme", "role": "admin"}]
REPOSITORIES = [
    {
        "repository_id": REPO,
        "full_name": "acme/web",
        "platform": "github",
        "platform_repository_id": 9007199254740993,
    }
]


def scenarios():
    env = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
    org_response = {("GET", "/users/me/organizations"): (200, {"data": ORGANIZATIONS})}
    repo_response = {("GET", f"/orgs/{ORG}/repos?limit=100"): (200, {"data": REPOSITORIES})}
    cases = [
        {"name": "org JSON", "args": ["org", "list", "--json", "*"], "responses": org_response, "json": True},
        {"name": "org human", "args": ["org", "list"], "responses": org_response},
        {"name": "org jq", "args": ["org", "list", "--json", "slug,role", "-q", ".[].slug"], "responses": org_response},
        {
            "name": "repo JSON preserves large integers",
            "args": ["repo", "list", "--json", "*"],
            "responses": repo_response,
            "json": True,
        },
        {"name": "repo TSV", "args": ["repo", "list"], "responses": repo_response},
        {
            "name": "repo jq",
            "args": ["repo", "list", "--json", "full_name", "-q", ".[].full_name"],
            "responses": repo_response,
        },
        {
            "name": "org slug",
            "args": ["repo", "list", "--org", "AcMe", "--json", "*"],
            "responses": org_response | repo_response,
            "json": True,
        },
        {
            "name": "auto organization",
            "args": ["repo", "list", "--json", "*"],
            "responses": org_response | repo_response,
            "env": {"AVR_TOKEN": harness.TOKEN},
            "json": True,
        },
        {
            "name": "explicit org overrides environment",
            "args": ["repo", "list", "--org", ORG, "--json", "*"],
            "responses": repo_response,
            "env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": "wrong"},
            "json": True,
        },
        {
            "name": "repo limit",
            "args": ["repo", "list", "-L", "2", "--json", "*"],
            "responses": {("GET", f"/orgs/{ORG}/repos?limit=2"): (200, {"data": REPOSITORIES})},
            "json": True,
        },
        {
            "name": "no organization",
            "args": ["repo", "list"],
            "responses": {("GET", "/users/me/organizations"): (200, {"data": []})},
            "env": {"AVR_TOKEN": harness.TOKEN},
            "exit": 1,
            "error_terms": ["No organizations found"],
        },
        {
            "name": "unknown organization",
            "args": ["repo", "list", "--org", "missing"],
            "responses": org_response,
            "exit": 1,
            "error_terms": ["No organization matching", "missing", "acme", ORG],
        },
        {
            "name": "scoped token requires ID",
            "args": ["repo", "list", "--org", "acme"],
            "env": {"AVR_TOKEN": "avs_fixture"},
            "exit": 1,
            "error_terms": ["scoped token", "organization", "AVR_ORG"],
        },
        {
            "name": "scoped token with explicit ID",
            "args": ["repo", "list", "--org", ORG, "--json", "*"],
            "env": {"AVR_TOKEN": "avs_fixture"},
            "responses": repo_response,
            "json": True,
        },
    ]
    for group, endpoint in [("org", "/users/me/organizations"), ("repo", f"/orgs/{ORG}/repos?limit=100")]:
        cases.extend(
            [
                {"name": f"{group} discovery", "args": [group, "list", "--json", "?"], "env": {}},
                {"name": f"{group} missing auth", "args": [group, "list"], "env": {}, "exit": 4},
                {
                    "name": f"{group} unknown field",
                    "args": [group, "list", "--json", "typo"],
                    "exit": 1,
                    "error_terms": ["Unknown JSON field", "typo"],
                },
                {
                    "name": f"{group} jq requires JSON",
                    "args": [group, "list", "--jq", "."],
                    "exit": 2,
                    "error_terms": ["--jq requires --json"],
                },
                {
                    "name": f"{group} empty JSON",
                    "args": [group, "list", "--json", "*"],
                    "responses": {("GET", endpoint): (200, {"data": []})},
                    "json": True,
                },
                {
                    "name": f"{group} empty human",
                    "args": [group, "list"],
                    "responses": {("GET", endpoint): (200, {"data": []})},
                },
                {
                    "name": f"{group} rejected credentials",
                    "args": [group, "list"],
                    "responses": {("GET", endpoint): (401, {"detail": "Unauthorized"})},
                    "exit": 4,
                },
                {
                    "name": f"{group} forbidden",
                    "args": [group, "list"],
                    "responses": {("GET", endpoint): (403, {"detail": "Forbidden"})},
                    "exit": 1,
                    "error_terms": ["403", "Forbidden"],
                },
            ]
        )
    for limit in ["0", "1001"]:
        cases.append(
            {
                "name": f"invalid repo limit {limit}",
                "args": ["repo", "list", "-L", limit],
                "exit": 2,
                "error_terms": ["--limit", "1", "1000"],
            }
        )
    # Width-library upgrades must retain Python's Unicode table geometry.
    unicode_orgs = [
        ORGANIZATIONS[0] | {"name": "研发团队 e\u0301 👩‍💻 🇫🇮"},
        ORGANIZATIONS[0] | {"organization_id": ORG[:-1] + "2", "name": "word\u2060join", "slug": "second"},
    ]
    for tty in [False, True] if sys.platform != "win32" else [False]:
        cases.append(
            {
                "name": "org Unicode widths " + ("TTY" if tty else "pipe"),
                "args": ["org", "list"],
                "responses": {("GET", "/users/me/organizations"): (200, {"data": unicode_orgs})},
                "tty": tty,
                "presentation": True,
            }
        )
    for case in cases:
        case.setdefault("env", env)
    return (
        cases + run_scenarios() + watch_scenarios() + view_scenarios() + terminal_scenarios() + presentation_scenarios()
    )


RUN = "run-019a0000000070008000000000000003"
RUN_DATA = {
    "run_id": RUN,
    "platform_run_id": 9007199254740993,
    "display_title": "Build web",
    "status": "completed",
    "conclusion": "success",
    "head_branch": "main",
    "head_sha": "abcdef01",
    "event": "push",
    "run_number": 42,
    "run_attempt": 1,
    "duration_seconds": 12.125,
    "created_at": "2026-10-29T12:00:00Z",
    "updated_at": "2026-10-29T12:01:00Z",
    "workflow_id": "wfl-019a0000000070008000000000000004",
    "workflow": {"name": "Build", "path": ".github/workflows/build.yml"},
    "repository": {"id": REPO, "full_name": "acme/web"},
    "triggering_actor": {"platform_id": 42, "platform_login": "fixture"},
}


def run_scenarios():
    env = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
    base = {"limit": 20, "order": "created_at.desc", "include": ["workflow"]}

    def response(params=None, data=None, pagination=None):
        query = base | (params or {})
        path = f"/orgs/{ORG}/workflow-runs?" + urlencode(query, doseq=True)
        return {("GET", path): (200, {"data": [RUN_DATA] if data is None else data, "pagination": pagination or {}})}

    result = [
        {"name": "run list JSON", "args": ["run", "list", "--json", "*"], "responses": response(), "json": True},
        {"name": "run list TSV", "args": ["run", "list"], "responses": response()},
        {
            "name": "run list jq",
            "args": ["run", "list", "--json", "run_id,conclusion", "-q", ".[].run_id"],
            "responses": response(),
        },
        {"name": "run list empty", "args": ["run", "list"], "responses": response(data=[])},
        {
            "name": "run list empty JSON",
            "args": ["run", "list", "--json", "*"],
            "responses": response(data=[]),
            "json": True,
        },
        {
            "name": "run cursor",
            "args": ["run", "list", "--cursor", "opaque-next", "--json", "*"],
            "responses": response({"cursor": "opaque-next"}),
            "json": True,
        },
        {
            "name": "run filters",
            "args": [
                "run",
                "list",
                "--status",
                "SUCCESS",
                "--status",
                "in_progress",
                "--branch",
                "main",
                "--branch",
                "feature/test",
                "--repo",
                REPO,
                "-w",
                "wfl-019a0000000070008000000000000004",
                "-L",
                "2",
                "--order",
                "created_at.asc",
                "--json",
                "*",
            ],
            "responses": response(
                {
                    "conclusions": ["success"],
                    "statuses": ["in_progress"],
                    "head_branches": ["main", "feature/test"],
                    "repository_ids": [REPO],
                    "workflow_ids": ["wfl-019a0000000070008000000000000004"],
                    "limit": 2,
                    "order": "created_at.asc",
                }
            ),
            "json": True,
        },
        {
            "name": "run date aliases",
            "args": ["run", "list", "--from", "2026-01-01", "--to", "2026-10-29T12:00:00Z", "--json", "*"],
            "responses": response(
                {"created_after": "2026-01-01T00:00:00", "created_before": "2026-10-29T12:00:00+00:00"}
            ),
            "json": True,
        },
        {
            "name": "run environment repo",
            "args": ["run", "list", "--json", "*"],
            "env": env | {"AVR_REPO": REPO},
            "responses": response({"repository_ids": [REPO]}),
            "json": True,
        },
        {
            "name": "run repo name",
            "args": ["run", "list", "--repo", "acme/web", "--json", "*"],
            "responses": response({"repository_ids": [REPO]})
            | {("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb"): (200, {"data": REPOSITORIES[0]})},
            "json": True,
        },
        {
            "name": "run git origin",
            "args": ["run", "list", "--json", "*"],
            "git_remote": "git@github.com:acme/web.git",
            "responses": response({"repository_ids": [REPO]})
            | {("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"): (200, {"data": REPOSITORIES[0]})},
            "json": True,
        },
        {
            "name": "run GitLab origin",
            "args": ["run", "list", "--json", "*"],
            "git_remote": "https://gitlab.com/acme/team/web.git",
            "responses": response({"repository_ids": [REPO]})
            | {
                ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fteam%2Fweb&platform=gitlab"): (
                    200,
                    {"data": REPOSITORIES[0] | {"full_name": "acme/team/web", "platform": "gitlab"}},
                )
            },
            "json": True,
        },
        {
            "name": "run scoped repo lookup",
            "args": ["run", "list", "--repo", "acme/web", "--json", "*"],
            "env": env | {"AVR_TOKEN": "avs_fixture"},
            "responses": response({"repository_ids": [REPO]})
            | {("GET", f"/orgs/{ORG}/repos?q=acme%2Fweb&limit=100"): (200, {"data": REPOSITORIES})},
            "json": True,
        },
        {
            "name": "run scoped paged repo lookup",
            "args": ["run", "list", "--repo", "acme/web", "--json", "*"],
            "env": env | {"AVR_TOKEN": "avs_fixture"},
            "responses": response({"repository_ids": [REPO]})
            | {
                ("GET", f"/orgs/{ORG}/repos?q=acme%2Fweb&limit=100"): (
                    200,
                    {
                        "data": [REPOSITORIES[0] | {"full_name": "acme/web-other"}],
                        "pagination": {"next_cursor": "page2"},
                    },
                ),
                ("GET", f"/orgs/{ORG}/repos?q=acme%2Fweb&limit=100&cursor=page2"): (200, {"data": REPOSITORIES}),
            },
            "json": True,
        },
    ]
    for flag, value in [
        ("--status", "bogus"),
        ("--order", "wrong"),
        ("--limit", "0"),
        ("--cursor", ""),
        ("--cursor", " whitespace "),
    ]:
        result.append(
            {
                "name": f"run invalid {flag} {value}",
                "args": ["run", "list", flag, value],
                "exit": 2,
                "error_terms": [flag],
            }
        )
    result.extend(
        [
            {"name": "run discovery", "args": ["run", "list", "--json", "?"], "env": {}},
            {"name": "run no auth", "args": ["run", "list"], "env": {}, "exit": 4},
            {
                "name": "run since conflict",
                "args": ["run", "list", "--since", "1d", "--from", "2026-01-01"],
                "exit": 2,
                "error_terms": ["--since", "cannot be combined"],
            },
            {
                "name": "run invalid date",
                "args": ["run", "list", "--from", "yesterday"],
                "exit": 1,
                "error_terms": ["--created-after", "ISO-8601"],
            },
        ]
    )
    result.append(
        {
            "name": "run jq explicit TSV columns",
            "args": [
                "run",
                "list",
                "--json",
                "status,conclusion,run_id",
                "--jq",
                ".[] | [.status,.conclusion,.run_id] | @tsv",
            ],
            "responses": response({}),
            "tsv": True,
        }
    )
    for timestamp in [
        "2026-10-01T10:00",
        "2026-10-01T10",
        "2026-10-01 10:00:00+02:00",
        "2026-10-01T10:00:00+0200",
        "20261001",
        "20261001T100000,1234567Z",
        "2026-10-01T10:00:00.1+0200",
    ]:
        result.append(
            {
                "name": "run ISO timestamp " + timestamp,
                "args": ["run", "list", "--from", timestamp, "--json", "*"],
                "responses": response({"created_after": datetime.fromisoformat(timestamp).isoformat()}),
                "json": True,
            }
        )
    for case in result:
        case.setdefault("env", env)
    return result


JOB = "job-019a0000000070008000000000000005"
JOB_DATA = {
    "job_id": JOB,
    "platform_job_id": 9007199254740993,
    "job_name": "Unit tests",
    "state": "in_progress",
    "conclusion": None,
}
ACTIVE_RUN = RUN_DATA | {"status": "in_progress", "conclusion": None, "jobs": [JOB_DATA]}
FINISHED_RUN = RUN_DATA | {"jobs": [JOB_DATA | {"state": "completed", "conclusion": "success"}]}


def watch_scenarios():
    env = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
    endpoint = ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}?include=jobs")
    lookup = ("GET", f"/orgs/{ORG}/workflow-runs/by-platform-id/9007199254740993")
    args = ["run", "watch", RUN, "--interval", "1"]
    finished = (200, {"data": FINISHED_RUN})
    active = (200, {"data": ACTIVE_RUN})
    result = [
        {
            "name": "watch completed stream",
            "args": args,
            "responses": {endpoint: finished},
            "events": ["job_completed", "run_completed"],
        },
        {
            "name": "watch transitions suppress duplicate jobs",
            "args": args,
            "responses": {endpoint: [active, active, finished]},
            "events": ["job_started", "job_completed", "run_completed"],
        },
        {
            "name": "watch failure exit status",
            "args": args + ["--exit-status"],
            "responses": {endpoint: (200, {"data": FINISHED_RUN | {"conclusion": "failure"}})},
            "events": ["job_completed", "run_completed"],
            "exit": 1,
        },
        {
            "name": "watch failure informational",
            "args": args,
            "responses": {endpoint: (200, {"data": FINISHED_RUN | {"conclusion": "failure"}})},
            "events": ["job_completed", "run_completed"],
        },
        {
            "name": "watch explicit NDJSON",
            "args": args + ["--ndjson"],
            "responses": {endpoint: finished},
            "events": ["job_completed", "run_completed"],
        },
        {
            "name": "watch platform ID",
            "args": ["run", "watch", "9007199254740993", "--interval", "1"],
            "responses": {endpoint: finished, lookup: (200, {"data": RUN_DATA})},
            "events": ["job_completed", "run_completed"],
        },
        {
            "name": "watch GitHub URL",
            "args": ["run", "watch", "https://github.com/Acme/Web/actions/runs/9007199254740993", "--interval", "1"],
            "responses": {endpoint: finished, lookup: (200, {"data": RUN_DATA})},
            "events": ["job_completed", "run_completed"],
        },
        {"name": "watch missing auth", "args": args, "env": {}, "exit": 4},
        {
            "name": "watch invalid interval",
            "args": ["run", "watch", RUN, "--interval", "0"],
            "exit": 1,
            "error_terms": ["--interval", "at least 1 second"],
        },
        {
            "name": "watch repository mismatch",
            "args": ["run", "watch", "https://github.com/other/project/actions/runs/9007199254740993"],
            "responses": {lookup: (200, {"data": RUN_DATA})},
            "exit": 1,
            "error_terms": ["does not match", "repository"],
        },
    ]
    for status in [401, 403, 404]:
        result.append(
            {
                "name": f"watch terminal HTTP {status}",
                "args": args,
                "responses": {endpoint: (status, {"detail": "fixture denial"})},
                "exit": 4 if status == 401 else 1,
                "error_terms": [str(status)],
            }
        )
    for status in [429, 503]:
        result.append(
            {
                "name": f"watch recovers HTTP {status}",
                "args": args,
                "responses": {endpoint: [(status, {"detail": "temporary"}), finished]},
                "events": ["transient_error", "job_completed", "run_completed"],
            }
        )
    result.extend(
        [
            {
                "name": "watch aborts after five failures",
                "args": args,
                "responses": {endpoint: (503, {"detail": "temporary"})},
                "events": ["transient_error"] * 5 + ["watch_aborted"],
                "exit": 1,
            },
            {
                "name": "watch retry counter resets",
                "args": args,
                "responses": {endpoint: [(503, {}), active, (503, {}), finished]},
                "events": ["transient_error", "job_started", "transient_error", "job_completed", "run_completed"],
            },
        ]
    )
    for selected in [[], [RUN_DATA]]:
        auto = (
            "GET",
            f"/orgs/{ORG}/workflow-runs?limit=1&order=created_at.desc&statuses=in_progress&statuses=queued&repository_ids={REPO}",
        )
        case = {
            "name": f"watch auto selection {bool(selected)}",
            "args": ["run", "watch", "--repo", REPO, "--interval", "1"],
            "responses": {auto: (200, {"data": selected}), endpoint: finished},
        }
        if selected:
            case["events"] = ["job_completed", "run_completed"]
        result.append(case)
    if sys.platform != "win32":
        result.append(
            {
                "name": "watch Ctrl-C during polling",
                "args": args,
                "responses": {endpoint: active},
                "interrupt_after_event": True,
                "events": ["job_started"],
                "exit": 130,
                "error_terms": ["Aborted!"],
            }
        )
        result.append(
            {
                "name": "watch Ctrl-C terminal",
                "args": ["--no-links"] + args,
                "tty": True,
                "responses": {
                    ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}"): active,
                    ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}?include=jobs&include=workflow"): active,
                },
                "interrupt_after_text": "Unit tests",
                "exit": 130,
                "stdout_terms": ["Stopped watching."],
            }
        )
        result.append(
            {
                "name": "watch live terminal",
                "args": ["--no-links"] + args,
                "tty": True,
                "responses": {
                    ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}"): active,
                    ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}?include=jobs&include=workflow"): [active, finished],
                },
                "stdout_terms": [
                    "Watching run #42",
                    "Build web",
                    "JOBS",
                    "Unit tests",
                    JOB,
                    "Run #42 completed:",
                    "success",
                    "12s",
                ],
            }
        )
        result.append(
            {
                "name": "watch terminal already finished",
                "args": args,
                "tty": True,
                "responses": {("GET", f"/orgs/{ORG}/workflow-runs/{RUN}"): (200, {"data": RUN_DATA})},
            }
        )
    result.append(
        {
            "name": "watch consumer closes pipe",
            "python_windows_pipe_flush_bug": True,
            "args": args,
            "responses": {endpoint: [active, finished]},
            "close_pipe_after_event": True,
            "events": ["job_started"],
        }
    )
    for case in result:
        case.setdefault("env", env)
    return result


def typed_event(value):
    if isinstance(value, dict):
        return {key: typed_event(child) for key, child in value.items()}
    if isinstance(value, list):
        return [typed_event(child) for child in value]
    return [type(value).__name__, str(value)]


def normalized_events(text):
    events = []
    for line in text.splitlines():
        event = json.loads(line, parse_float=decimal.Decimal)
        timestamp = event.pop("timestamp")
        parsed = datetime.fromisoformat(timestamp.replace("Z", "+00:00"))
        if parsed.tzinfo is None:
            raise ValueError("event timestamp has no timezone")
        events.append(event)
    return events


def view_scenarios():
    env = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
    endpoint = ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}?include=jobs&include=workflow")
    platform = ("GET", f"/orgs/{ORG}/workflow-runs/by-platform-id/9007199254740993?include=jobs&include=workflow")
    siblings = ("GET", f"/orgs/{ORG}/workflow-runs?platform_run_id=9007199254740993&limit=10&order=created_at.desc")
    orgs = ("GET", "/users/me/organizations")
    data = FINISHED_RUN | {
        "jobs": [
            FINISHED_RUN["jobs"][0]
            | {
                "duration_seconds": 12.125,
                "steps": [
                    {
                        "name": "Run tests",
                        "status": "completed",
                        "conclusion": "success",
                        "started_at": "2026-10-29T12:00:00Z",
                        "completed_at": "2026-10-29T12:00:12Z",
                    }
                ],
            }
        ]
    }
    responses = {
        endpoint: (200, {"data": data}),
        siblings: (200, {"data": [RUN_DATA]}),
        orgs: (200, {"data": ORGANIZATIONS}),
    }
    args = ["run", "view", RUN]
    metadata = [
        "Build web",
        "main",
        "abcdef01",
        "push",
        "fixture",
        "12s",
        "#42",
        "attempt 1",
        "Build (build.yml)",
        "acme/web",
        "Unit tests",
        JOB,
    ]
    result = [
        {"name": "view JSON", "args": args + ["--json", "*"], "responses": responses, "json": True},
        {
            "name": "view JSON selection",
            "args": args + ["--json", "conclusion,jobs"],
            "responses": responses,
            "json": True,
        },
        {"name": "view jq", "args": args + ["--json", "jobs", "--jq", ".jobs[].job_name"], "responses": responses},
        {"name": "view metadata and jobs", "args": args, "responses": responses, "stdout_terms": metadata},
        {
            "name": "view steps",
            "args": args + ["--steps"],
            "responses": responses,
            "stdout_terms": metadata + ["Run tests"],
        },
        {"name": "view job filter", "args": args + ["--job", "UNIT"], "responses": responses, "stdout_terms": metadata},
        {
            "name": "view missing job",
            "args": args + ["--job", "missing"],
            "responses": responses,
            "stdout_terms": metadata[:10],
        },
        {
            "name": "view platform ID",
            "args": ["run", "view", "9007199254740993", "--json", "*"],
            "responses": {platform: (200, {"data": data})},
            "json": True,
        },
        {
            "name": "view GitHub URL",
            "args": ["run", "view", "https://github.com/acme/web/actions/runs/9007199254740993", "--json", "*"],
            "responses": {platform: (200, {"data": data})},
            "json": True,
        },
        {"name": "view discovery", "args": ["run", "view", "--json", "?"], "env": {}},
        {"name": "view unauthenticated", "args": args, "env": {}, "exit": 4},
        {
            "name": "view web JSON conflict",
            "args": args + ["--web", "--json", "*"],
            "exit": 2,
            "error_terms": ["--web", "--json"],
        },
        {
            "name": "view unknown JSON field",
            "args": args + ["--json", "unknown"],
            "exit": 1,
            "error_terms": ["Unknown JSON field", "unknown"],
        },
        {
            "name": "view empty recent",
            "args": ["run", "view"],
            "responses": {
                ("GET", f"/orgs/{ORG}/workflow-runs?limit=10&order=created_at.desc&include=jobs"): (200, {"data": []})
            },
        },
        {"name": "view web piped", "args": args + ["--web"], "responses": responses},
    ]
    for status in [401, 403, 404]:
        result.append(
            {
                "name": f"view HTTP {status}",
                "args": args,
                "responses": {endpoint: (status, {"detail": "fixture denial"})},
                "exit": 4 if status == 401 else 1,
                "error_terms": [str(status)],
            }
        )
    for reference in [
        "0",
        "https://github.com.evil.test/acme/web/actions/runs/42",
        "https://github.com:443/acme/web/actions/runs/42",
        "https://user@github.com/acme/web/actions/runs/42",
        "https://github.com/acme/web/actions/runs/42#fragment",
    ]:
        result.append(
            {
                "name": "view rejects " + reference,
                "args": ["run", "view", reference],
                "exit": 1,
                "error_terms": ["run reference"],
            }
        )
    attempt = (
        "GET",
        f"/orgs/{ORG}/workflow-runs?platform_run_id=9007199254740993&limit=1000&order=created_at.desc&include=jobs&include=workflow",
    )
    for found in [True, False]:
        result.append(
            {
                "name": f"view GitHub attempt {found}",
                "args": [
                    "run",
                    "view",
                    "https://github.com/acme/web/actions/runs/9007199254740993/attempts/2",
                    "--json",
                    "*",
                ],
                "responses": {attempt: (200, {"data": [data | {"run_attempt": 2}] if found else []})},
                **({"json": True} if found else {"exit": 1, "error_terms": ["no visible attempt 2"]}),
            }
        )
    logs = ("POST", "/logs/search")
    log_entries = [
        {
            "line_number": 1,
            "step_name": "Run tests",
            "level": "info",
            "timestamp": "2026-10-29T12:00:00.123Z",
            "content": "##[group]Test output",
        },
        {"line_number": 2, "step_name": "Run tests", "level": "diagnostic", "content": "hidden diagnostic"},
        {"line_number": 3, "step_name": "Run tests", "level": "info", "content": "\x1b[31massertions passed\x1b[0m"},
        {"line_number": 4, "step_name": "Run tests", "level": "info", "content": "##[endgroup]"},
    ]
    result.append(
        {
            "name": "view paged logs",
            "args": args + ["--log", "--no-pager"],
            "responses": responses
            | {
                logs: [
                    (200, {"results": log_entries[:2], "has_more": True, "next_cursor": 2}),
                    (200, {"results": log_entries[2:], "has_more": False}),
                ]
            },
            "stdout_terms": metadata + ["Run tests", "Test output", "assertions passed", "12:00:00.123"],
            "stdout_absent": ["hidden diagnostic", "##[endgroup]", "\\x1b", "[31m"],
        }
    )
    result.append(
        {
            "name": "view logs unavailable",
            "args": args + ["--log"],
            "responses": responses | {logs: (404, {"detail": "no log source"})},
            "stdout_terms": metadata,
        }
    )
    failed_data = data | {
        "conclusion": "failure",
        "jobs": [
            data["jobs"][0]
            | {
                "conclusion": "failure",
                "steps": [{"name": "Run tests", "status": "completed", "conclusion": "failure"}],
            }
        ],
    }
    result.append(
        {
            "name": "view failed step logs",
            "args": args + ["--log-failed"],
            "responses": responses
            | {endpoint: (200, {"data": failed_data}), logs: (200, {"results": log_entries, "has_more": False})},
            "stdout_terms": metadata + ["Run tests", "failure", "assertions passed"],
            "stdout_absent": ["hidden diagnostic"],
        }
    )
    for case in result:
        case.setdefault("env", env)
    return result


def terminal_scenarios():
    env = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
    path = f"/orgs/{ORG}/workflow-runs?limit=20&order=created_at.desc&include=workflow"
    responses = {
        ("GET", path): (200, {"data": [RUN_DATA], "pagination": {"next_cursor": "next-page"}}),
        ("GET", "/users/me/organizations"): (200, {"data": ORGANIZATIONS}),
    }
    run_url = f"http://127.0.0.1/org/acme/runs/{RUN}"
    cases = []
    if sys.platform == "win32":
        return cases
    for name, flags, setting, links in [
        ("default", [], None, [run_url, "https://github.com/acme/web/tree/main"]),
        ("flag off", ["--no-links"], None, []),
        ("environment off", [], "0", []),
        ("flag overrides environment", ["--links"], "0", [run_url, "https://github.com/acme/web/tree/main"]),
    ]:
        cases.append(
            {
                "name": "run terminal links " + name,
                "args": flags + ["run", "list"],
                "env": env | ({"AVR_LINKS": setting} if setting is not None else {}),
                "responses": responses,
                "tty": True,
                "links": links,
                "stdout_terms": [
                    "TITLE",
                    "WORKFLOW",
                    "BRANCH",
                    "EVENT",
                    "ID",
                    "ELAPSED",
                    "AGE",
                    "Build web",
                    "Build",
                    "main",
                    "push",
                    RUN,
                    "12s",
                ],
            }
        )
    return cases


def presentation_scenarios():
    if sys.platform == "win32":
        return []
    env = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": None, "TERM": "xterm-256color", "AVR_PAGER": ""}
    orgs = {("GET", "/users/me/organizations"): (200, {"data": ORGANIZATIONS})}
    repos = REPOSITORIES + [REPOSITORIES[0] | {"full_name": "acme/" + "long-repository-name-" * 4}]
    cases = [
        {
            "name": "styled repo list",
            "args": ["repo", "list"],
            "responses": orgs | {("GET", f"/orgs/{ORG}/repos?limit=100"): (200, {"data": repos})},
        },
        {"name": "styled org list", "args": ["org", "list"], "responses": orgs},
    ]
    # Reuse request/behavior fixtures; only terminal presentation changes here.
    for case in run_scenarios() + view_scenarios() + watch_scenarios():
        if case["name"] in {
            "run list TSV",
            "view steps",
            "view recent",
            "view paged logs",
            "view failed step logs",
        } or (case.get("tty") and "watch" in case["name"]):
            cases.append(case | {"name": "styled " + case["name"]})
    for case in harness.scenarios():
        if case["name"] in {
            "TTY status",
            "TTY host selection",
            "logout",
            "github login",
            "switch hosts",
            "scoped human status",
        }:
            cases.append(case | {"name": "styled auth " + case["name"]})
    states = [(state, None) for state in ("queued", "pending", "in_progress")] + [
        ("completed", conclusion)
        for conclusion in (
            "success",
            "failure",
            "cancelled",
            "skipped",
            "timed_out",
            "action_required",
            "neutral",
            "stale",
            "startup_failure",
        )
    ]
    runs = [
        RUN_DATA
        | {
            "status": state,
            "conclusion": conclusion,
            "display_title": "Long title " * 9,
            "head_branch": "long-branch-" * 5,
            "workflow": RUN_DATA["workflow"] | {"name": "Long workflow " * 5},
        }
        for state, conclusion in states
    ]
    cases.append(
        {
            "name": "styled run status matrix",
            "args": ["run", "list"],
            "responses": orgs
            | {
                ("GET", f"/orgs/{ORG}/workflow-runs?limit=20&order=created_at.desc&include=workflow"): (
                    200,
                    {"data": runs},
                )
            },
        }
    )
    cases.append(
        {
            "name": "styled recent runs",
            "args": ["run", "view"],
            "responses": {
                ("GET", f"/orgs/{ORG}/workflow-runs?limit=10&order=created_at.desc&include=jobs"): (
                    200,
                    {"data": [RUN_DATA]},
                )
            },
        }
    )
    result = []
    for case in cases:
        original_env = case.get("env", {})
        case = case | {
            "args": ["--no-links"] + case["args"],
            "tty": True,
            "presentation": True,
            "env": env | case.get("env", {}) | {"NO_COLOR": None, "TERM": "xterm-256color", "AVR_PAGER": ""},
        }
        # Auth host fixtures use the stored token rather than the inspection token.
        if "auth " in case["name"]:
            case["env"] = original_env | {"NO_COLOR": None, "TERM": "xterm-256color"}
        case["merge_terminal"] = True
        result.append(case)
        result.append(case | {"name": case["name"] + " no color", "args": ["--no-color"] + case["args"]})
    result.append(result[0] | {"name": "styled repo NO_COLOR environment", "env": result[0]["env"] | {"NO_COLOR": "1"}})
    return result


def presentation(value):
    """Compare visible glyph styles, not escape encoding or whitespace padding."""
    value = re.sub(r"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)", "", value)
    state = {"fg": None, "bg": None, "bold": False, "dim": False, "underline": False}
    glyphs = []
    offset = 0
    for match in re.finditer(r"\x1b\[([0-?]*)([ -/]*)([@-~])", value):
        for char in value[offset : match.start()]:
            if not char.isspace():
                glyphs.append((char, tuple(state.values())))
        if match[3] == "m":
            codes = [int(c or 0) for c in match[1].split(";")]
            i = 0
            while i < len(codes):
                code = codes[i]
                if code == 0:
                    state = {"fg": None, "bg": None, "bold": False, "dim": False, "underline": False}
                elif code in (1, 2, 4):
                    state[{1: "bold", 2: "dim", 4: "underline"}[code]] = True
                elif code == 22:
                    state["bold"] = state["dim"] = False
                elif code == 24:
                    state["underline"] = False
                elif code == 39:
                    state["fg"] = None
                elif code == 49:
                    state["bg"] = None
                elif 40 <= code <= 47:
                    state["bg"] = code - 40
                elif 100 <= code <= 107:
                    state["bg"] = code - 100 + 8
                elif 30 <= code <= 37:
                    state["fg"] = code - 30
                elif 90 <= code <= 97:
                    state["fg"] = code - 90 + 8
                elif code in (38, 48) and i + 2 < len(codes) and codes[i + 1] == 5:
                    state["fg" if code == 38 else "bg"] = codes[i + 2]
                    i += 2
                i += 1
        offset = match.end()
    glyphs.extend((c, tuple(state.values())) for c in value[offset:] if not c.isspace())
    return glyphs


def terminal_text(value):
    # Repeated CR before LF returns to column zero repeatedly, not extra rows.
    value = re.sub(r"\r+\n", "\n", value)
    # Link targets are metadata, not visible information. A hidden URL must
    # not satisfy a check for a missing visible ID or label.
    value = re.sub(r"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)", "", value)
    return re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", value)


def comparable(result):
    result = dict(result)
    requests = []
    for request in result["requests"]:
        request = dict(request)
        parts = urlsplit(request["path"])
        # Query-key ordering isn't a contract; repeated values retain order.
        request["path"] = urlunsplit(
            parts._replace(
                query=urlencode(sorted(parse_qsl(parts.query, keep_blank_values=True), key=lambda item: item[0]))
            )
        )
        requests.append(request)
    result["requests"] = requests
    return result


def compare(scenario, python, go):
    python, go = comparable(python), comparable(go)
    if scenario["name"] == "org human":
        # Compare all table cells, ignoring PrettyTable's border decoration.
        for result in (python, go):
            lines = [line for line in result["stdout"].splitlines() if not re.fullmatch(r"[+\-\s]+", line)]
            result["stdout"] = "\n".join(line.replace("|", " ") for line in lines)
    event_differences = []
    # Keep the raw capture in the report. Only the known pinned Python flush
    # failure is accepted; Go must still exit cleanly and emit valid events.
    if (
        sys.platform == "win32"
        and scenario.get("python_windows_pipe_flush_bug")
        and python["exit"] == 120
        and "OSError: [Errno 22] Invalid argument" in python["stderr"]
        and "flush" in python["stderr"]
    ):
        python["exit"] = 0
        python["stderr"] = ""
    notifications = [
        re.findall(r"\x1b\]9;([^\x07\x1b]*)(?:\x07|\x1b\\)", r["stdout"] + r["stderr"]) for r in (python, go)
    ]
    if notifications[0] != notifications[1]:
        event_differences.append("terminal completion notifications")
    if scenario.get("presentation"):
        # Normalize moving clocks/ages before comparing, but retain semantic styles.
        def stable(value):
            value = re.sub(r"\d{2}:\d{2}:\d{2} UTC", "<clock>", value)
            return re.sub(r"\d+d ago", "<age>", value)

        if presentation(stable(python["stdout"])) != presentation(stable(go["stdout"])):
            event_differences.append("terminal text/styles")
        if not scenario["name"].startswith("styled auth") and "watch" not in scenario["name"]:
            a = [line.rstrip() for line in terminal_text(stable(python["stdout"])).splitlines()]
            b = [line.rstrip() for line in terminal_text(stable(go["stdout"])).splitlines()]
            if a != b:
                event_differences.append("terminal layout")
    if "events" in scenario:
        for result in (python, go):
            try:
                events = normalized_events(result["stdout"])
                if [event["event"] for event in events] != scenario["events"]:
                    event_differences.append("event sequence")
                result["stdout"] = json.dumps(typed_event(events), sort_keys=True)
            except ValueError, KeyError, TypeError:
                event_differences.append("invalid NDJSON event")
    if "links" in scenario:
        for result in (python, go):
            links = set(re.findall(r"\x1b\]8;;([^\x07\x1b]+)(?:\x07|\x1b\\)", result["stdout"]))
            if links != set(scenario["links"]):
                event_differences.append("terminal link targets")
    if "stdout_terms" in scenario:
        for result in (python, go):
            result["stdout"] = terminal_text(result["stdout"])
            for term in scenario["stdout_terms"]:
                if term not in result["stdout"]:
                    event_differences.append("stdout missing: " + term)
            for term in scenario.get("stdout_absent", []):
                if term in result["stdout"]:
                    event_differences.append("stdout contains: " + term)
            result["stdout"] = "<semantic terminal comparison>"
    if scenario.get("presentation"):
        for result in (python, go):
            result["stdout"] = terminal_text(stable(result["stdout"]))
    differences = event_differences + harness.compare(scenario, python, go)
    if scenario["name"] in {"repo TSV", "run list TSV", "job list TSV"} and python["stdout"] != go["stdout"]:
        differences.append("TSV bytes")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/inspection-parity.json")
    )
