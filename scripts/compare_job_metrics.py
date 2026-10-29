"""Job resource gauges and live log-tail parity against the pinned Python CLI."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_job_watch as watch
from scripts import compare_jobs as jobs
import json
import sys

ORG, JOB = jobs.ORG, jobs.JOB
VIEW = ("GET", f"/orgs/{ORG}/jobs/{JOB}")
SOURCES = ("cpu", "memory", "filesystem", "load", "disk-io", "disk-ops", "network")
ACTIVE = jobs.JOB_DATA | {"state": "in_progress", "conclusion": None, "completed_at": None}
FINISHED = jobs.JOB_DATA | {"completed_at": "2026-01-01T00:00:12Z"}
FORBIDDEN = (403, {"detail": "fixture denied"})
FOOTER = "(logs every 1s, metrics every 5s — Ctrl-C to exit)"


def series(values, labels=None, rates=None):
    result = {"labels": labels or {}, "values": [[100 + i, v] for i, v in enumerate(values)]}
    if rates is not None:
        result["rates"] = [[100 + i, v] for i, v in enumerate(rates)]
    return result


METRICS = {
    "cpu": {
        "unit": "1",
        "series": [
            series([0.8, 0.2, 0], {"cpu": "0", "state": "idle"}),
            series([0.6, 0.4, 0.2], {"cpu": "1", "state": "idle"}),
            series([0.2, 0.8, 1], {"cpu": "0", "state": "user"}),
        ],
    },
    "memory": {
        "unit": "bytes",
        "series": [
            series([4 * 1024**3, 10 * 1024**3, 15 * 1024**3], {"state": "used"}),
            series([12 * 1024**3, 6 * 1024**3, 1024**3], {"state": "free"}),
        ],
    },
    "filesystem": {"unit": "1", "series": [series([0.2, 0.3, 0.5]), series([0.1, 0.2, 0.3])]},
    "load": {"unit": "1", "series": [series([0, 1, 4])]},
    "disk-io": {
        "unit": "bytes",
        "rate_unit": "bytes/sec",
        "series": [
            series([9007199254740993], rates=[128, 1024, 2048]),
            series([], rates=[128, 1024, 4096]),
        ],
    },
    "disk-ops": {
        "unit": "operations",
        "rate_unit": "operations/sec",
        "series": [series([99999], rates=[500, 1500, 2000])],
    },
    "network": {
        "unit": "bytes",
        "rate_unit": "bytes/sec",
        "series": [series([99999], rates=[1024**2, 1024**3, 2 * 1024**3])],
    },
}


def responses(job=FINISHED, metrics=None, query=""):
    return {VIEW: (200, {"data": job})} | {
        ("GET", VIEW[1] + "/metrics/" + source + query): (200, data)
        for source, data in (metrics if metrics is not None else METRICS).items()
    }


def scenarios():
    cases = []

    def case(name, extra=None, **kwargs):
        cases.append(
            {
                "name": "job metrics " + name,
                "args": ["job", "metrics", JOB, *(extra or [])],
                "responses": responses(),
                **kwargs,
            }
        )

    all_sources = [flag for s in SOURCES for flag in ("--source", s)]
    case("default gauges", presentation=True)
    case("all sources", all_sources, presentation=True)
    case("raw JSON", ["--json", *all_sources], json=True)
    case(
        "time bounds zero",
        ["--start", "0", "--end", "200", "--json"],
        responses=responses(query="?start=0&end=200"),
        json=True,
    )
    case(
        "negative bounds",
        ["--start", "-10", "--end", "-1"],
        responses=responses(query="?start=-10&end=-1"),
        presentation=True,
    )
    case("repeated source", ["--source", "cpu", "--source", "cpu"], presentation=True)
    case("org slug", ["--org", "acme"], responses=jobs.ORGS | responses(), presentation=True)
    for labels in [[], ["custom-3-vcpu"], ["custom-vcpu", "AVREA-2-VCPU"]]:
        case("runner " + repr(labels), responses=responses(FINISHED | {"job_labels": labels}), presentation=True)
    case("active", responses=responses(ACTIVE), presentation=True)
    case("queued", responses=responses(ACTIVE | {"state": "queued", "duration_seconds": None}), presentation=True)
    case(
        "empty sources",
        all_sources,
        responses=responses(metrics={s: {"unit": "1", "series": []} for s in SOURCES}),
        presentation=True,
    )
    missing = responses()
    missing[("GET", VIEW[1] + "/metrics/cpu")] = (404, {"detail": "no execution"})
    case("partial missing execution", responses=missing, presentation=True)
    case("missing execution JSON", ["--json"], responses=missing, json=True)
    for value in [0, 0.60, 0.85, 1, 1.2]:
        case(
            "saturation " + str(value),
            ["--source", "filesystem"],
            responses=responses(metrics={"filesystem": {"unit": "1", "series": [series([value])]}}),
            presentation=True,
        )
    case(
        "CPU missing idle",
        ["--source", "cpu"],
        responses=responses(metrics={"cpu": {"unit": "1", "series": [series([0.1], {"state": "user"})]}}),
        presentation=True,
    )
    case(
        "counter missing rates",
        ["--source", "network"],
        responses=responses(metrics={"network": {"unit": "bytes", "series": [series([999999])]}}),
        presentation=True,
    )
    case(
        "counter custom units",
        ["--source", "disk-ops"],
        responses=responses(metrics={"disk-ops": METRICS["disk-ops"] | {"rate_unit": "events/sec"}}),
        presentation=True,
    )
    case("finished watch fallback", ["-w"], presentation=True)
    case("finished JSON watch fallback", ["--watch", "--json"], json=True)
    case("missing auth", env={}, responses={}, exit=4)
    case("missing ID", args=["job", "metrics"], responses={}, exit=2, error_terms=["JOB_ID"])
    case("invalid source", ["--source", "CPU"], responses={}, exit=2, error_terms=["--source", "CPU"])
    for code in [401, 403, 503]:
        case(f"job HTTP {code}", responses={VIEW: (code, {"detail": "fixture denied"})}, exit=4 if code == 401 else 1)
        failed = responses()
        failed[("GET", VIEW[1] + "/metrics/cpu")] = (code, {"detail": "fixture denied"})
        case(f"source HTTP {code}", responses=failed, exit=4 if code == 401 else 1)
    live = responses(ACTIVE) | {jobs.LOGS: [jobs.LOG_RESPONSE, FORBIDDEN]}
    case("watch logs", ["-w", *all_sources], responses=live, exit=1, presentation=True)
    case(
        "watch empty logs",
        ["--watch"],
        responses=responses(ACTIVE) | {jobs.LOGS: [(200, {"results": [], "has_more": False}), FORBIDDEN]},
        exit=1,
        presentation=True,
    )
    case("watch JSON", ["-w", "--json"], responses=live, exit=1, json_frames=True)
    for code in [429, 503]:
        case(
            f"watch logs retry {code}",
            ["-w"],
            responses=responses(ACTIVE) | {jobs.LOGS: [(code, {"detail": "retry"}), jobs.LOG_RESPONSE, FORBIDDEN]},
            exit=1,
            presentation=True,
        )
        failed = deepcopy(live)
        failed[("GET", VIEW[1] + "/metrics/cpu")] = [(code, {"detail": "retry"}), (200, METRICS["cpu"])]
        case(f"watch metrics retry {code}", ["-w"], responses=failed, exit=1, presentation=True)
    case(
        "watch metrics auth loss",
        ["-w"],
        responses=responses(ACTIVE) | {("GET", VIEW[1] + "/metrics/cpu"): FORBIDDEN},
        exit=1,
    )
    case(
        "watch logs missing job",
        ["-w"],
        responses=responses(ACTIVE) | {jobs.LOGS: (404, {"detail": "fixture missing"})},
        exit=1,
    )
    many = [{"line_number": i, "content": f"line {i}", "level": "info"} for i in range(1, 18)]
    case(
        "watch paginated tail",
        ["-w"],
        responses=responses(ACTIVE)
        | {
            jobs.LOGS: [
                (200, {"results": many[:10], "has_more": True, "next_cursor": 10}),
                (
                    200,
                    {
                        "results": many[10:] + [{"line_number": 18, "content": "hidden", "level": "diagnostic"}],
                        "has_more": False,
                        "next_cursor": 18,
                    },
                ),
                FORBIDDEN,
            ]
        },
        exit=1,
        presentation=True,
    )
    if sys.platform != "win32":
        for name in [
            "all sources",
            "saturation 0",
            "saturation 1",
            "runner []",
            "finished watch fallback",
            "watch logs",
            "watch empty logs",
            "watch metrics retry 503",
            "watch logs retry 503",
        ]:
            original = next(c for c in cases if c["name"] == "job metrics " + name)
            item = deepcopy(original)
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
            if name == "all sources":
                cases.append(item | {"name": item["name"] + " no color", "args": ["--no-color", *item["args"]]})
        case(
            "watch interrupt",
            ["-w"],
            responses=responses(ACTIVE) | {jobs.LOGS: jobs.LOG_RESPONSE},
            interrupt_after_text=FOOTER,
            exit=130,
            presentation=True,
        )
    for item in cases:
        item.setdefault("env", {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG})
    return cases


def compare(scenario, python, go):
    python, go = deepcopy(python), deepcopy(go)
    differences = []
    if scenario.get("json_frames"):
        for result in (python, go):
            # Preserve footer/error text and the number/order of JSON frames.
            decoder = json.JSONDecoder()
            text, chunks = result["stdout"], []
            while text:
                if text.lstrip().startswith("{"):
                    text = text.lstrip()
                    try:
                        value, end = decoder.raw_decode(text)
                    except ValueError:
                        differences.append("invalid metrics JSON frame")
                        break
                    chunks.append(json.dumps(value, sort_keys=True))
                    text = text[end:]
                else:
                    end = text.find("\n{")
                    if end < 0:
                        chunks.append(text)
                        break
                    chunks.append(text[:end])
                    text = text[end + 1 :]
            result["stdout"] = "".join(chunks)
    return differences + watch.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/job-metrics-parity.json")
    )
