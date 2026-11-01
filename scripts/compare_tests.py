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
    case("shorthand invalid ID", args=["tests", "job-bad"], responses={}, exit=2, error_terms=["Avrea job ID"])
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
    for job in ("123", "job-" + "A" * 32, "awj-short"):
        case("invalid ID " + job, args=["tests", "list", job], responses={}, exit=2, error_terms=["Avrea job ID"])
    if sys.platform != "win32":
        for name in ("human", "control characters", "duplicate names", "empty available complete", "row budget"):
            item = deepcopy(next(c for c in cases if c["name"] == "tests " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    # Help uses the invoking executable's name in an admin-composed tree.
    if "--help" in scenario["args"] or "-h" in scenario["args"]:
        return help_parity.compare(scenario, python, go)
    return watch.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/tests-parity.json"))
