"""Differential job inspection, logs and SSH checks against the pinned Python CLI."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
import sys

ORG, JOB, RUN = inspection.ORG, inspection.JOB, inspection.RUN
JOB2 = "job-019a0000000070008000000000000006"
STEP = {
    "name": "Build",
    "status": "completed",
    "conclusion": "failure",
    "started_at": "2026-10-29T12:00:00Z",
    "completed_at": "2026-10-29T12:00:12Z",
}
JOB_DATA = {
    "job_id": JOB,
    "platform_job_id": 9007199254740993,
    "platform_run_id": 9007199254740995,
    "job_name": "Build web",
    "state": "completed",
    "conclusion": "failure",
    "repository_id": inspection.REPO,
    "repository_full_name": "acme/web",
    "job_labels": ["avrea-ubuntu-latest-4-vcpu", "linux"],
    "running_on_avrea": True,
    "duration_seconds": 12.125,
    "created_at": "2026-01-01T00:00:00Z",
    "started_at": "2026-10-29T12:00:00Z",
    "completed_at": "2026-10-29T12:00:12.125Z",
    "steps": [STEP],
    "workflow_run": {"run_id": RUN, "run_number": 42},
}
ORGS = {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})}
LIST = ("GET", f"/orgs/{ORG}/jobs?limit=20&order=created_at.desc")
VIEW = ("GET", f"/orgs/{ORG}/jobs/{JOB}?include=steps&include=workflow_run")
LOG_JOB = ("GET", f"/orgs/{ORG}/jobs/{JOB}?include=steps")
LOGS = ("POST", "/logs/search")
RUN_VIEW = ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}?include=jobs")
SSH = ("POST", "/vms/ssh-forwarding")
SSH_DATA = {
    "host": "runner.example.test",
    "port": 2222,
    "username": "runner",
    "password": "synthetic password",
    "ssh_keys_installed": False,
    "host_key": "fixture",
}
LINES = [
    {"line_number": 1, "step_name": "Setup", "content": "hidden diagnostic", "level": "diagnostic"},
    {
        "line_number": 2,
        "step_name": "Setup",
        "content": "##[command]echo ready",
        "level": "info",
        "timestamp": "2026-10-29T12:00:01Z",
    },
    {
        "line_number": 3,
        "step_name": "Build",
        "content": "\x1b[31mcompile failed\x1b[0m",
        "level": "error",
        "timestamp": "2026-10-29T12:00:02Z",
    },
]
LOG_RESPONSE = (200, {"results": LINES, "has_more": False, "next_cursor": 3})


def scenarios():
    listing = {LIST: (200, {"data": [JOB_DATA], "pagination": {"next_cursor": "next-page"}})}
    view = ORGS | {VIEW: (200, {"data": JOB_DATA}), LOG_JOB: (200, {"data": JOB_DATA}), LOGS: LOG_RESPONSE}
    run = ORGS | {RUN_VIEW: (200, {"data": inspection.RUN_DATA | {"jobs": [JOB_DATA]}}), LOGS: LOG_RESPONSE}
    cases = [
        {"name": "job list JSON", "args": ["job", "list", "--json", "*"], "responses": listing, "json": True},
        {
            "name": "job list jq aliases",
            "args": ["job", "list", "--json", "labels,repository", "--jq", ".[]"],
            "responses": listing,
            "json": True,
        },
        {"name": "job list TSV", "args": ["job", "list"], "responses": listing},
        {"name": "job list empty", "args": ["job", "list"], "responses": {LIST: (200, {"data": []})}},
        {
            "name": "job list cursor filters",
            "args": [
                "job",
                "list",
                "--repo",
                inspection.REPO,
                "--name",
                "Build",
                "--name",
                "Test",
                "--status",
                "in_progress",
                "--status",
                "FAILURE",
                "--shadowing",
                "-w",
                "workflow-fixture",
                "--cursor",
                "page",
                "-L",
                "2",
                "--order",
                "created_at.asc",
                "--json",
                "*",
            ],
            "responses": {
                (
                    "GET",
                    f"/orgs/{ORG}/jobs?limit=2&order=created_at.asc&repository_ids={inspection.REPO}&job_names=Build&job_names=Test&states=in_progress&conclusions=failure&running_on_avrea=false&workflow_ids=workflow-fixture&cursor=page",
                ): (200, {"data": [JOB_DATA]})
            },
            "json": True,
        },
        {"name": "job view JSON", "args": ["job", "view", JOB, "--json", "*"], "responses": view, "json": True},
        {"name": "job view human", "args": ["job", "view", JOB], "responses": view},
        {"name": "job view full logs", "args": ["job", "view", JOB, "--log"], "responses": view},
        {"name": "job view failed logs", "args": ["job", "view", JOB, "--log-failed"], "responses": view},
        {"name": "job view web piped", "args": ["job", "view", JOB, "--web"], "responses": view},
        {"name": "job logs", "args": ["job", "logs", JOB], "responses": view},
        {"name": "job logs all levels", "args": ["job", "logs", JOB, "--all-levels"], "responses": view},
        {"name": "job logs failed", "args": ["job", "logs", JOB, "--failed"], "responses": view},
        {
            "name": "job logs step level",
            "args": ["job", "logs", JOB, "--step", "Build", "--level", "error"],
            "responses": view,
        },
        {"name": "job logs missing", "args": ["job", "logs", JOB], "responses": view | {LOGS: (404, {})}},
        {
            "name": "job logs queued empty",
            "args": ["job", "logs", JOB],
            "responses": view
            | {LOG_JOB: (200, {"data": JOB_DATA | {"state": "queued"}}), LOGS: (200, {"results": []})},
        },
        {
            "name": "job logs pagination",
            "args": ["job", "logs", JOB],
            "responses": view
            | {
                LOGS: [
                    (200, {"results": LINES[:1], "has_more": True}),
                    (200, {"results": LINES[1:], "has_more": False}),
                ]
            },
        },
        {"name": "run logs", "args": ["run", "logs", RUN], "responses": run},
        {"name": "run logs failed", "args": ["run", "logs", RUN, "--failed"], "responses": run},
        {"name": "run logs no matching job", "args": ["run", "logs", RUN, "--job", "absent"], "responses": run},
        {
            "name": "run logs multiple jobs",
            "args": ["run", "logs", RUN],
            "responses": run
            | {
                RUN_VIEW: (
                    200,
                    {
                        "data": inspection.RUN_DATA
                        | {"jobs": [JOB_DATA, JOB_DATA | {"job_id": JOB2, "job_name": "Test suite"}]}
                    },
                )
            },
        },
        {
            "name": "run logs no failures",
            "args": ["run", "logs", RUN, "--failed"],
            "responses": run
            | {RUN_VIEW: (200, {"data": inspection.RUN_DATA | {"jobs": [JOB_DATA | {"conclusion": "success"}]}})},
        },
    ]
    for group, args in [("job logs", ["job", "logs", JOB]), ("run logs", ["run", "logs", RUN])]:
        cases.append(
            {
                "name": group + " incompatible flags",
                "args": args + ["--follow", "--failed"],
                "exit": 2,
                "error_terms": ["--follow", "--failed"],
            }
        )
    for name, args in [
        ("job list fields", ["job", "list", "--json", "?"]),
        ("job view fields", ["job", "view", JOB, "--json", "?"]),
    ]:
        cases.append({"name": name, "args": args, "env": {}})
    for args, name, term in [
        (["job", "list", "-L", "0"], "job invalid limit", "--limit"),
        (["job", "list", "--status", "bad"], "job invalid state", "--status"),
        (["job", "logs", JOB, "--level", "bad"], "job invalid level", "--level"),
        (["job", "view", JOB, "--web", "--json", "*"], "job web json conflict", "--web"),
    ]:
        cases.append({"name": name, "args": args, "exit": 2, "error_terms": [term]})
    for status in (401, 403, 404, 500):
        for name, args, endpoint in [
            ("job view", ["job", "view", JOB], VIEW),
            ("job list", ["job", "list"], LIST),
            ("job ssh", ["job", "ssh", JOB, "--print-command"], SSH),
        ]:
            cases.append(
                {
                    "name": f"{name} HTTP {status}",
                    "args": args,
                    "responses": {endpoint: (status, {"detail": "fixture denial"})},
                    "exit": 4 if status == 401 else 1,
                }
            )
    for flags, data, name in [
        (["--print-command"], SSH_DATA, "masked"),
        (["--print-command", "--show-password"], SSH_DATA, "explicit password"),
        (["--print-command"], SSH_DATA | {"ssh_keys_installed": True}, "keys"),
        (["--print-command"], SSH_DATA | {"password": None}, "no password"),
    ]:
        cases.append(
            {
                "name": "ssh print " + name,
                "args": ["job", "ssh", JOB] + flags,
                "responses": {SSH: (200, data)},
                "ssh_stub": 0,
            }
        )
    if sys.platform != "win32":
        for code in (0, 7):
            cases.append(
                {
                    "name": f"ssh subprocess exit {code}",
                    "args": ["job", "ssh", JOB],
                    "responses": {SSH: (200, SSH_DATA)},
                    "ssh_stub": code,
                    "ssh_stub_pins": True,
                    "exit": code,
                }
            )
    # The cleanup sentinel exercises real follow execution without waiting for
    # periodic completion polling. The Go suite separately tests the late drain.
    end = {"line_number": 4, "step_name": "Cleanup", "content": "Cleaning up orphan processes", "level": "info"}
    follow = view | {
        LOGS: (200, {"results": LINES + [end], "next_cursor": 4}),
        ("GET", f"/orgs/{ORG}/jobs/{JOB}"): (200, {"data": JOB_DATA}),
    }
    cases.append({"name": "job follow completion", "args": ["job", "logs", JOB, "--follow"], "responses": follow})
    cases.append(
        {
            "name": "job follow transient retry",
            "args": ["job", "logs", JOB, "--follow"],
            "responses": follow | {LOGS: [(503, {}), follow[LOGS]]},
        }
    )
    cases.append(
        {
            "name": "job follow diagnostic cursor",
            "args": ["job", "logs", JOB, "--follow"],
            "responses": follow
            | {LOGS: [(200, {"results": LINES[:1], "has_more": True}), (200, {"results": LINES[1:] + [end]})]},
        }
    )
    cases.append(
        {
            "name": "run follow selects active",
            "args": ["run", "logs", RUN, "--follow"],
            "responses": follow
            | run
            | {
                RUN_VIEW: (
                    200,
                    {
                        "data": inspection.RUN_DATA
                        | {"jobs": [JOB_DATA | {"job_id": JOB2}, JOB_DATA | {"state": "in_progress"}]}
                    },
                ),
                LOGS: follow[LOGS],
            },
        }
    )

    cases.append(
        {"name": "job logs uppercase level", "args": ["job", "logs", JOB, "--level", "ERROR"], "responses": view}
    )
    cases.append(
        {
            "name": "job failed ignores other filters",
            "args": ["job", "logs", JOB, "--failed", "--step", "absent", "--level", "warning"],
            "responses": view,
        }
    )
    # Completion after idle polling must include the delayed tail, not just
    # the initial batch. This intentionally exercises the real Python delay.
    cases.append(
        {
            "name": "job follow final drain",
            "args": ["job", "logs", JOB, "--follow"],
            "responses": view | {LOGS: [(200, {"results": []}), (200, {"results": []}), (200, {"results": LINES[1:]})]},
        }
    )
    if sys.platform != "win32":
        for label, args in [("job", ["job", "logs", JOB, "--follow"]), ("run", ["run", "logs", RUN, "--follow"])]:
            cases.append(
                {
                    "name": label + " follow Ctrl-C",
                    "args": args,
                    "responses": view | run | {LOGS: LOG_RESPONSE},
                    "interrupt_after_text": "compile failed",
                    "exit": 130,
                    "error_terms": [],
                    "stdout_terms": ["Stopped following."],
                }
            )

    cases.append(
        {
            "name": "job logs tab spacing",
            "args": ["job", "logs", JOB],
            "responses": view
            | {
                LOGS: (
                    200,
                    {
                        "results": [
                            {
                                "line_number": 1,
                                "step_name": "Build",
                                "content": "actual" + chr(9) + "tab; literal" + chr(92) + "ttext",
                                "level": "info",
                            }
                        ]
                    },
                )
            },
        }
    )
    env = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
    for case in cases:
        case.setdefault("env", env)
    if sys.platform != "win32":
        for case in list(cases):
            if case["name"] in {
                "job list TSV",
                "job view human",
                "job view full logs",
                "job logs",
                "job logs failed",
                "run logs",
                "run logs failed",
                "job follow completion",
                "run follow selects active",
            }:
                for color in (True, False):
                    cases.append(
                        deepcopy(case)
                        | {
                            "name": "styled " + case["name"] + ("" if color else " no color"),
                            "args": (["--no-links"] if color else ["--no-links", "--no-color"]) + case["args"],
                            "tty": True,
                            "merge_terminal": True,
                            "presentation": True,
                            "env": env | {"NO_COLOR": None, "TERM": "xterm-256color", "AVR_PAGER": ""},
                        }
                    )

    if sys.platform != "win32":
        link_env = env | {"NO_COLOR": None, "TERM": "xterm-256color", "AVR_PAGER": ""}
        target = f"http://127.0.0.1/org/acme/jobs/{JOB}"
        for name, args, responses, targets in [
            ("job list linked", ["job", "list"], listing | ORGS, [target]),
            (
                "job view linked logs",
                ["job", "view", JOB, "--log"],
                view,
                [
                    target,
                    f"http://127.0.0.1/org/acme/runs/{RUN}",
                    f"http://127.0.0.1/org/acme/activity?repositories={inspection.REPO}",
                ],
            ),
        ]:
            cases.append(
                {
                    "name": name,
                    "args": args,
                    "responses": responses,
                    "tty": True,
                    "merge_terminal": True,
                    "presentation": True,
                    "links": targets,
                    "env": link_env,
                }
            )
        varied = [
            JOB_DATA
            | {
                "job_name": "Long job name " * 8,
                "repository_full_name": "acme/" + "long-name-" * 6,
                "conclusion": conclusion,
                "running_on_avrea": index % 2 == 0,
            }
            for index, conclusion in enumerate(
                [
                    "success",
                    "failure",
                    "cancelled",
                    "skipped",
                    "timed_out",
                    "action_required",
                    "neutral",
                    "stale",
                    "startup_failure",
                ]
            )
        ]
        cases.append(
            {
                "name": "job list terminal status matrix",
                "args": ["--no-links", "job", "list"],
                "responses": {LIST: (200, {"data": varied})},
                "env": link_env,
                "tty": True,
                "presentation": True,
            }
        )
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=inspection.compare, default_report="build/go/job-parity.json"
        )
    )
