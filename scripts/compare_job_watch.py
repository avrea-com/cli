"""Active-job watch parity, including actual interruption and ordered toggles."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_jobs as jobs
import json
import re
import sys

ORG, REPO = inspection.ORG, inspection.REPO
WATCH = ("GET", f"/orgs/{ORG}/jobs?limit=50&order=created_at.desc&states=queued&states=in_progress")
ACTIVE = jobs.JOB_DATA | {"state": "in_progress", "conclusion": None, "completed_at": None, "duration_seconds": None}
QUEUED = ACTIVE | {"job_id": jobs.JOB2, "state": "queued", "job_name": "Test", "running_on_avrea": False}
ORGS = jobs.ORGS
FORBIDDEN = (403, {"detail": "fixture denied"})


def scenarios():
    cases = []

    def case(name, extra=None, **kwargs):
        cases.append(
            {
                "name": "job watch " + name,
                "args": ["job", "watch", "--interval", "1", *(extra or [])],
                "responses": ORGS | {WATCH: [(200, {"data": [ACTIVE, QUEUED]}), FORBIDDEN]},
                "exit": 1,
                **kwargs,
            }
        )

    case("snapshot stream", snapshots=True)
    case("empty stream", responses=ORGS | {WATCH: [(200, {"data": []}), FORBIDDEN]}, snapshots=True)
    case(
        "changing snapshots",
        responses=ORGS | {WATCH: [(200, {"data": [ACTIVE, QUEUED]}), (200, {"data": [QUEUED]}), FORBIDDEN]},
        snapshots=True,
    )
    case("explicit ndjson", ["--ndjson"], snapshots=True)
    case(
        "filters",
        ["--repo", REPO, "--repo", "rep-second", "--name", "Build", "--name", "Test"],
        responses=ORGS
        | {
            ("GET", WATCH[1] + f"&repository_ids={REPO}&repository_ids=rep-second&job_names=Build&job_names=Test"): [
                (200, {"data": [ACTIVE]}),
                FORBIDDEN,
            ]
        },
        snapshots=True,
    )
    case(
        "git context",
        git_remote="git@github.com:acme/web.git",
        responses=ORGS
        | {
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"): (
                200,
                {"data": inspection.REPOSITORIES[0]},
            ),
            ("GET", WATCH[1] + f"&repository_ids={REPO}"): [(200, {"data": [ACTIVE]}), FORBIDDEN],
        },
        snapshots=True,
    )
    case("org slug", ["--org", "acme"], snapshots=True)
    case("missing auth", env={}, responses={}, exit=4)
    for code in [401, 403, 404]:
        case(f"HTTP {code}", responses={WATCH: (code, {"detail": "fixture denied"})}, exit=4 if code == 401 else 1)
    for code in [429, 500, 503]:
        case(
            f"retry {code}",
            responses=ORGS | {WATCH: [(code, {"detail": "retry"}), (200, {"data": [ACTIVE]}), FORBIDDEN]},
            snapshots=True,
        )
    for interval in ["0", "-1"]:
        case("invalid interval " + interval, ["--interval", interval], responses={})
    for flags, on in [(["--shadowing", "--on-avrea"], "true"), (["--on-avrea", "--shadowing"], "false")]:
        cases.append(
            {
                "name": "job list last toggle " + on,
                "args": ["job", "list", *flags, "--json", "*"],
                "responses": {("GET", jobs.LIST[1] + "&running_on_avrea=" + on): (200, {"data": [ACTIVE]})},
                "json": True,
            }
        )
    if sys.platform != "win32":
        for name in ["snapshot stream", "empty stream", "retry 503"]:
            original = next(c for c in cases if c["name"] == "job watch " + name)
            for no_color in [False, True]:
                item = deepcopy(original)
                item["name"] += " terminal" + (" no color" if no_color else "")
                item.pop("snapshots", None)
                item["tty"] = item["merge_terminal"] = item["presentation"] = True
                item["args"] = ["--no-links", *(["--no-color"] if no_color else []), *item["args"]]
                item["env"] = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": "", "TERM": "xterm-256color"}
                cases.append(item)
        item = deepcopy(cases[0])
        item["name"] = "job watch terminal links"
        item.pop("snapshots", None)
        item["tty"] = item["merge_terminal"] = item["presentation"] = True
        item["env"] = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": "", "TERM": "xterm-256color"}
        item["links"] = [
            f"http://127.0.0.1/org/acme/jobs/{ACTIVE['job_id']}",
            f"http://127.0.0.1/org/acme/jobs/{QUEUED['job_id']}",
        ]
        cases.append(item)
        item = deepcopy(cases[3])
        item["name"] = "job watch explicit ndjson terminal"
        item["tty"] = True
        item["env"] = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": "", "TERM": "xterm-256color"}
        cases.append(item)
        case(
            "interrupt",
            responses={WATCH: (200, {"data": [ACTIVE]})},
            interrupt_after_event=True,
            exit=130,
            snapshots=True,
        )
        cases.append(
            {
                "name": "jobs alias interrupt",
                "args": ["jobs", "watch"],
                "responses": {WATCH: (200, {"data": []})},
                "interrupt_after_event": True,
                "exit": 130,
                "snapshots": True,
            }
        )
    for item in cases:
        item.setdefault("env", {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG})
    return cases


def compare(scenario, python, go):
    python, go = deepcopy(python), deepcopy(go)
    differences = []
    for result in (python, go):
        # API diagnostics expose the request URL. Query-key ordering is not a
        # contract; keep all keys, repeated values and their relative order.
        for stream in ("stdout", "stderr"):
            result[stream] = re.sub(
                r"(?m)(^\s*API: )(\S+)",
                lambda match: match[1] + harness.canonical_path(match[2]),
                result[stream],
            )
    if scenario.get("snapshots"):
        for result in (python, go):
            if "\x1b" in result["stdout"]:
                differences.append("terminal escapes in snapshot stream")
            lines = []
            for line in result["stdout"].splitlines():
                if line.startswith("{"):
                    lines.append(json.dumps(json.loads(line), sort_keys=True))
                else:
                    lines.append(line)
            result["stdout"] = "\n".join(lines)
    return differences + inspection.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/job-watch-parity.json")
    )
