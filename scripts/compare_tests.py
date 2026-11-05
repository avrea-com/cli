"""Job test-result parity: terminal output, bounded pagination and atomic failures."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_help as help_parity
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from urllib.parse import urlencode
import sys

ORG, REPO = inspection.ORG, inspection.REPO
JOB = "job-019a0000000070008000000000000003"
NATIVE = "awj-019a0000000070008000000000000007"
ROW = {
    "test_id": "a" * 64,
    "suite": "checkout",
    "class_name": "TestCheckout",
    "name": "test_payment",
    "outcome": "failed",
    "duration_ms": 21,
    "failure_summary": "expected 200, got 500",
}
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}


def page(rows=None, cursor=None, **extra):
    return {
        "job_id": JOB,
        "execution_id": "exe-019a0000000070008000000000000004",
        "status": "available",
        "reporting_state": "complete",
        "reason": None,
        "commit_sha": "b" * 40,
        "observed_at": "2026-10-28T12:00:00Z",
        "total_count": 3,
        "data": [ROW] if rows is None else rows,
        "pagination": {"next_cursor": cursor},
        **extra,
    }


def metadata(job=JOB):
    return ("GET", f"/orgs/{ORG}/jobs/{job}")


def endpoint(limit=100, cursor=None, job=JOB):
    query = {"limit": limit}
    if cursor is not None:
        query["cursor"] = cursor
    return ("GET", f"/orgs/{ORG}/repos/{REPO}/jobs/{job}/tests?" + urlencode(query))


def responses(result=None, job=JOB):
    return {
        metadata(job): (200, {"data": {"job_id": job, "repository_id": REPO}}),
        endpoint(job=job): (200, page(job_id=job) if result is None else result),
    }


def scenarios():
    cases = []

    def case(name, extra=None, **kwargs):
        cases.append(
            {
                "name": "tests " + name,
                "args": ["tests", "list", JOB, *(extra or [])],
                "env": ENV.copy(),
                "responses": responses(),
                **kwargs,
            }
        )

    case("human", presentation=True)
    case("JSON", ["--json", "*"], json=True)
    case("jq", ["--json", "data", "--jq", ".data[].name"])
    case("short jq", ["--json", "data", "-q", ".data | length"], json=True)
    case("fields", ["--json", "job_id,organization_id,repository_id,pagination"], json=True)
    case("empty fields", ["--json", ""], json=True)
    case("discovery", ["--json", "?"], env={}, responses={}, presentation=True)
    case("unknown field", ["--json", "wrong"], env={}, responses={}, exit=1)
    case("jq requires JSON", ["--jq", "."], responses={}, exit=2, error_terms=["--jq requires --json"])
    case("missing auth", env={}, responses={}, exit=4)
    case("explicit scope", ["--org", ORG, "--repo", REPO], responses={endpoint(): (200, page())}, presentation=True)
    case("job scope overrides env repo", env=ENV | {"AVR_REPO": "unrelated/project"}, presentation=True)
    case(
        "org slug",
        ["--org", "acme"],
        env=ENV | {"AVR_ORG": "acme"},
        responses=responses() | {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})},
        presentation=True,
    )
    for job in (JOB, NATIVE):
        for prefix in (["tests", "list"], ["tests"]):
            case(
                "route " + " ".join(prefix + [job]),
                args=[*prefix, job],
                responses=responses(job=job),
                presentation=True,
            )
    case("global before shorthand", args=["--no-color", "tests", JOB], presentation=True)
    case("shorthand help", args=["tests", JOB, "--help"], env={}, responses={}, presentation=True)
    case("shorthand invalid ID", args=["tests", "job-bad"], responses={}, exit=2, error_terms=["job-", "awj-"])
    case("unknown verb", args=["tests", "history"], responses={}, exit=2, error_terms=["history"])

    for status, reporting in [
        ("available", "complete"),
        ("available", "partial"),
        ("pending", "pending"),
        ("missing", "unknown"),
        ("expired", "expired"),
        ("unavailable", "disabled"),
    ]:
        case(
            f"empty {status} {reporting}",
            responses=responses(page([], status=status, reporting_state=reporting, total_count=0)),
            presentation=True,
        )
    case("empty last page", responses=responses(page([])), presentation=True)
    controls = ROW | {
        "suite": "支付\tline\n\x1b[31m",
        "class_name": "Class\u202e",
        "name": "テスト",
        "failure_summary": "bad\x1b]0;title\x07\rline\nnext",
    }
    case("control characters", responses=responses(page([controls], reason="reason\x1b[31m\nnext")), presentation=True)
    case(
        "raw JSON controls and integer",
        ["--json", "*"],
        responses=responses(page([controls | {"duration_ms": 9007199254740993}])),
        json=True,
    )
    case("duplicate names", responses=responses(page([ROW, ROW, ROW | {"class_name": "Other"}])), presentation=True)
    case("missing cells", responses=responses(page([{"failure_summary": "failure"}, {}])), presentation=True)

    case(
        "short empty pages",
        responses=responses(page([ROW], "next"))
        | {
            endpoint(99, "next"): (200, page([], "last")),
            endpoint(99, "last"): (200, page([ROW, ROW | {"class_name": "Other"}])),
        },
        presentation=True,
    )
    case(
        "row budget",
        ["-L", "2"],
        responses={
            metadata(): responses()[metadata()],
            endpoint(2): (200, page([ROW], "next")),
            endpoint(1, "next"): (200, page([ROW], "resume +/'page")),
        },
        presentation=True,
    )
    cursor = "x" * 900
    case(
        "long resume cursor",
        ["--cursor", cursor, "--limit", "1", "--json", "*"],
        responses={metadata(): responses()[metadata()], endpoint(1, cursor): (200, page())},
        json=True,
    )
    budget = {metadata(): responses()[metadata()]}
    for i in range(20):
        budget[endpoint(100, None if i == 0 else f"page-{i}")] = (200, page([], f"page-{i + 1}"))
    case("request budget", responses=budget, presentation=True)
    case(
        "maximum limit",
        ["--limit", "1000", "--json", "data"],
        responses={metadata(): responses()[metadata()], endpoint(1000): (200, page([ROW] * 1000))},
        json=True,
    )

    for field in ("execution_id", "status", "reporting_state", "reason", "commit_sha", "observed_at", "total_count"):
        case(
            "changed " + field,
            responses=responses(page([ROW], "next")) | {endpoint(99, "next"): (200, page(**{field: "changed"}))},
            exit=1,
        )
    for cursor in ("", " padded", "x" * 1025, 7, {}, []):
        case("invalid API cursor " + repr(cursor)[:30], responses=responses(page(cursor=cursor)), exit=1)
    for cursor in ("", " padded", "x" * 1025):
        case(
            "invalid input cursor " + repr(cursor)[:30],
            ["--cursor", cursor],
            responses={},
            exit=2,
            error_terms=["--cursor", "1..1024"],
        )
    case(
        "cursor cycle",
        responses=responses(page([ROW], "next"))
        | {
            endpoint(99, "next"): (200, page(cursor="next")),
        },
        exit=1,
    )
    case(
        "initial cursor repeated",
        ["--cursor", "same"],
        responses={metadata(): responses()[metadata()], endpoint(100, "same"): (200, page(cursor="same"))},
        exit=1,
    )
    for label, malformed in (
        ("wrong job", page(job_id=NATIVE)),
        ("no pagination", page(pagination=None)),
        ("non-list rows", page(data={})),
        ("non-object row", page(data=[None])),
        ("overfull", page(data=[ROW] * 101)),
    ):
        case(label, responses=responses(malformed), exit=1)
    for label, data in (
        ("wrong metadata job", {"job_id": NATIVE, "repository_id": REPO}),
        ("missing repository", {"job_id": JOB}),
        ("invalid repository", {"job_id": JOB, "repository_id": "bad"}),
        ("invalid metadata", []),
    ):
        case(label, responses={metadata(): (200, {"data": data})}, exit=1)
    for code in (401, 403, 404, 409, 429, 503):
        case(
            f"metadata HTTP {code}",
            responses={metadata(): (code, {"detail": "fixture denied"})},
            exit=4 if code == 401 else 1,
        )
        case(
            f"late HTTP {code}",
            responses=responses(page([ROW], "next"))
            | {
                endpoint(99, "next"): (code, {"detail": "fixture denied"}),
            },
            exit=4 if code == 401 else 1,
        )
    for limit in ("0", "1001"):
        case("invalid limit " + limit, ["--limit", limit], responses={}, exit=2, error_terms=["--limit"])
    for job in ("not-a-reference", "job-" + "A" * 32, "awj-short"):
        case("invalid ID " + job, args=["tests", "list", job], responses={}, exit=2, error_terms=["job-", "awj-"])
    if sys.platform != "win32":
        for name in ("human", "control characters", "duplicate names", "empty available complete", "row budget"):
            item = deepcopy(next(c for c in cases if c["name"] == "tests " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases + release_scenarios()


def release_scenarios():
    run = inspection.RUN
    jobs = [{"job_id": JOB, "job_name": "Build tests", "platform_job_id": 99}]
    record = inspection.RUN_DATA | {"jobs": jobs}
    run_route = ("GET", f"/orgs/{ORG}/workflow-runs/{run}?include=jobs")
    base = responses() | {run_route: (200, {"data": record})}
    cases = []

    def add(name, args, routes=base, **kw):
        cases.append({"name": "tests release " + name, "args": ["tests", *args], "env": ENV, "responses": routes, **kw})

    for prefix in ([], ["list"]):
        add("single run " + str(prefix), [*prefix, run], presentation=True)
        add(
            "platform run " + str(prefix),
            [*prefix, "42", "--json", "*"],
            responses()
            | {("GET", f"/orgs/{ORG}/workflow-runs/by-platform-id/42?include=jobs"): (200, {"data": record})},
            json=True,
        )
    add("job name substring", [run, "--job", "TEST", "--json", "*"], json=True)
    for name, selected in [
        ("ambiguous", jobs + [{"job_id": NATIVE, "job_name": "Lint tests"}]),
        ("empty", []),
        ("invalid", [None]),
        ("not array", {}),
        ("missing ID", [{"job_name": "Build"}]),
    ]:
        add(
            name,
            [run],
            responses() | {run_route: (200, {"data": record | {"jobs": selected}})},
            exit=1,
            **({"error_terms": ["invalid"]} if name == "not array" else {}),
        )
    add("no job match", [run, "--job", "missing"], exit=1)
    add("blank job name", [run, "--job", " "], {}, exit=2)
    add("job flag with ID", [JOB, "--job", "Build"], {}, exit=2)
    for option in ("--test", "--suite", "--class"):
        add("empty " + option, [JOB, option, ""], {}, exit=2)
    add("invalid status", [JOB, "--status", "wrong"], {}, exit=2)
    for filters, flags in [
        ({"name": "test_payment"}, ["--test", "test_payment"]),
        (
            {"name": "literal.*%[x]", "suite": "支付", "class_name": "Class", "outcome": "failed"},
            ["--test", "literal.*%[x]", "--suite", "支付", "--class", "Class", "--status", "failed"],
        ),
    ]:
        query = urlencode({"limit": 100, **{key: "eq." + value for key, value in filters.items()}})
        route = ("GET", f"/orgs/{ORG}/repos/{REPO}/jobs/{JOB}/tests?{query}")
        routes = {metadata(): responses()[metadata()], route: (200, page(filters=filters))}
        add("filtered JSON " + str(filters), [JOB, *flags, "--json", "*"], routes, json=True)
        add("filtered human " + str(filters), [JOB, *flags], routes, presentation=True)
        for label, echo in [("missing", {}), ("null", None), ("array", []), ("different", filters | {"name": "other"})]:
            add(
                "filter echo " + label + str(filters),
                [JOB, *flags],
                routes | {route: (200, page(filters=echo))},
                exit=1,
            )
        add(
            "filter cursor mismatch " + str(filters),
            [JOB, *flags],
            routes | {route: (400, {"detail": "cursor belongs to different filters"})},
            exit=1,
        )
        add(
            "filtered empty " + str(filters),
            [JOB, *flags],
            routes | {route: (200, page([], filters=filters))},
            presentation=True,
        )
        add(
            "filtered continuation " + str(filters),
            [JOB, *flags, "--limit", "1"],
            {
                metadata(): responses()[metadata()],
                ("GET", route[1].replace("limit=100", "limit=1")): (200, page(cursor="next cursor", filters=filters)),
            },
            presentation=True,
        )
    add(
        "nullable filter echo",
        [JOB, "--json", "*"],
        responses(page(filters={"name": None, "outcome": None})),
        json=True,
    )
    if sys.platform != "win32":
        for original in [cases[0], next(c for c in cases if "filtered human" in c["name"])]:
            cases.append(
                original
                | {
                    "name": original["name"] + " terminal",
                    "tty": True,
                    "merge_terminal": True,
                    "presentation": True,
                    "env": ENV | {"NO_COLOR": "", "TERM": "xterm-256color"},
                }
            )
    return cases


def compare(scenario, python, go):
    # Help uses the invoking executable's name in an admin-composed tree.
    if "--help" in scenario["args"] or "-h" in scenario["args"]:
        return help_parity.compare(scenario, python, go)
    return watch.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/tests-parity.json"))
