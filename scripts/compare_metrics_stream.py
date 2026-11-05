"""0.6.0 VM metrics and shared job/VM observability streams against Python."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_job_metrics as gauges
from scripts import compare_jobs as jobs
from scripts import compare_vm_inspection as vm
import sys


def frame(kind, **fields):
    return {"frame": {"type": kind, "signal": "metrics", **fields}}


def scenarios():
    cases = []
    for kind in ("job", "vm"):
        identifier = jobs.JOB if kind == "job" else vm.VM
        view = gauges.VIEW if kind == "job" else vm.SHOW
        row = gauges.ACTIVE if kind == "job" else vm.ROW
        stream = (
            "GET",
            f"/vms/{identifier}/observability/stream?signals=metrics"
            if kind == "job"
            else view[1] + "/observability/stream?signals=metrics",
        )
        base = {view: (200, {"data": row})}
        if kind == "job":
            base[jobs.LOGS] = (200, {"results": [], "has_more": False})

        def case(name, events=None, extra=(), *, kind=kind, identifier=identifier, base=base, stream=stream, **kwargs):
            cases.append(
                {
                    "name": kind + " metrics " + name,
                    "args": [kind, "metrics", identifier, "--watch", *extra],
                    "env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": jobs.ORG},
                    "responses": base
                    | {
                        stream: (
                            101,
                            events
                            if events is not None
                            else [frame("bundle", metrics=gauges.METRICS), frame("complete")],
                        )
                    },
                    "presentation": True,
                    **kwargs,
                }
            )

        case("complete")
        case("all sources", extra=[flag for s in gauges.SOURCES for flag in ("--source", s)])
        case("NDJSON", extra=["--json"], json_frames=True)
        case(
            "repeated source",
            extra=["--source", "network", "--source", "cpu", "--source", "network", "--json"],
            json_frames=True,
        )
        case(
            "replace snapshots",
            [
                frame("bundle", metrics=gauges.METRICS),
                frame("bundle", metrics={"memory": gauges.METRICS["memory"]}),
                frame("complete"),
            ],
            extra=["--json"],
            json_frames=True,
        )
        for as_json in (False, True):
            extra = ["--json"] if as_json else []
            flags = {"json_frames": as_json}
            case("empty " + str(as_json), [frame("complete")], extra=extra, **flags)
            case("empty bundle " + str(as_json), [frame("bundle", metrics={}), frame("complete")], extra=extra, **flags)
            case(
                "window " + str(as_json),
                [frame("window", running=False), frame("bundle", metrics=gauges.METRICS), frame("complete")],
                extra=extra,
                **flags,
            )
            case(
                "signal error recovered " + str(as_json),
                [
                    frame("error", message="fixture unavailable"),
                    frame("window", running=True),
                    frame("bundle", metrics=gauges.METRICS),
                    frame("complete"),
                ],
                extra=extra,
                **flags,
            )
            case(
                "signal error completion " + str(as_json),
                [frame("error", message="fixture unavailable"), frame("complete")],
                extra=extra,
                exit=1,
                **flags,
            )
        case(
            "other signals",
            [
                frame("bundle", signal="logs", metrics={"cpu": {"unexpected": True}}),
                frame("complete", signal="status"),
                frame("bundle", metrics=gauges.METRICS),
                frame("complete"),
            ],
        )
        case("global error", [frame("error", signal=None, message="fixture authorization lost")], exit=1)
        case("invalid JSON", [{"raw": "{"}], exit=1)
        for value in ("null", "[]", "42"):
            case("invalid frame " + value, [{"raw": value}], exit=1)
        for status in (401, 403, 404, 422):
            case("handshake HTTP " + str(status), responses=base | {stream: (status, {})}, exit=1)
        case(
            "redirect",
            responses=base | {stream: (302, {}, {"Location": "/should-not-receive-credentials"})},
            expand_host=True,
            exit=1,
        )
        for status in (429, 503):
            case(
                "retry HTTP " + str(status),
                responses=base
                | {stream: [(status, {}), (101, [frame("bundle", metrics=gauges.METRICS), frame("complete")])]},
                extra=["--json"],
                json_frames=True,
            )
        case("exhaust retries", responses=base | {stream: (503, {})}, exit=1)
        for code in (1008, 4001, 4003, 4401, 4403):
            case("policy close " + str(code), [{"close": {"code": code, "reason": "fixture access revoked"}}], exit=1)
        case(
            "reconnect snapshot",
            responses=base
            | {
                stream: [
                    (101, [frame("bundle", metrics=gauges.METRICS), {"disconnect": True}]),
                    (101, [frame("bundle", metrics={"cpu": gauges.METRICS["cpu"]}), frame("complete")]),
                ]
            },
            extra=["--json"],
            json_frames=True,
        )
        for flag in ("--start", "--end"):
            case(
                "window rejected " + flag,
                extra=[flag, "0"],
                responses={},
                exit=2,
                error_terms=["--start and --end require single-shot mode; drop --watch."],
            )
        case("missing credentials", responses={}, env={}, exit=4)
        case("invalid source", extra=["--source", "CPU"], responses={}, exit=2, error_terms=["--source", "CPU"])
        if kind == "job":
            case(
                "finished job streams",
                responses=base
                | {
                    view: (200, {"data": gauges.FINISHED}),
                    stream: (101, [frame("bundle", metrics=gauges.METRICS), frame("complete")]),
                },
            )
            for status in (403, 429, 503):
                case(
                    "log HTTP " + str(status),
                    responses=base
                    | {
                        jobs.LOGS: [(status, {"detail": "fixture denied"}), jobs.LOG_RESPONSE],
                        stream: (101, [frame("window"), frame("bundle", metrics=gauges.METRICS), frame("complete")]),
                    },
                    exit=1 if status == 403 else 0,
                )
            case(
                "logs pending",
                responses=base
                | {
                    jobs.LOGS: [(404, {"detail": "No execution found for job"}), jobs.LOG_RESPONSE],
                    stream: (101, [frame("window"), frame("bundle", metrics=gauges.METRICS), frame("complete")]),
                },
            )
            case(
                "JSON never fetches logs",
                extra=["--json"],
                responses=base
                | {jobs.LOGS: (403, {}), stream: (101, [frame("bundle", metrics=gauges.METRICS), frame("complete")])},
                json_frames=True,
            )
        else:
            case(
                "watch by name resolves stream ID",
                args=["vm", "metrics", "Dev Box", "--watch"],
                responses={
                    ("GET", vm.BASE + "/Dev%20Box"): (200, {"data": vm.ROW}),
                    stream: (101, [frame("bundle", metrics=gauges.METRICS), frame("complete")]),
                },
            )
        if sys.platform != "win32":
            for name in ("complete", "all sources", "window False", "signal error recovered False"):
                original = next(c for c in cases if c["name"] == kind + " metrics " + name)
                cases.append(
                    deepcopy(original)
                    | {
                        "name": original["name"] + " terminal",
                        "tty": True,
                        "merge_terminal": True,
                        "env": {
                            "AVR_TOKEN": harness.TOKEN,
                            "AVR_ORG": jobs.ORG,
                            "NO_COLOR": "",
                            "TERM": "xterm-256color",
                        },
                    }
                )
            case(
                "interrupt",
                [frame("bundle", metrics=gauges.METRICS), {"delay": 2}],
                interrupt_after_text=gauges.FOOTER if kind == "job" else "memory",
                exit=130,
            )

    # Static VM requests keep the user-supplied name; watches resolve it to the ID.
    for name in (vm.VM, "Dev Box"):
        from urllib.parse import quote  # noqa: PLC0415 - fixture construction

        path = f"/orgs/{jobs.ORG}/vms/{quote(name, safe='')}"
        for selected in ([], ["network", "cpu"], ["cpu", "cpu"]):
            unique = list(dict.fromkeys(selected)) or ["cpu", "memory"]
            query = "&".join("source=" + s for s in unique)
            for as_json in (False, True):
                cases.append(
                    {
                        "name": f"vm metrics static {name} {selected} {as_json}",
                        "args": [
                            "vm",
                            "metrics",
                            name,
                            *[flag for s in selected for flag in ("--source", s)],
                            *(["--json"] if as_json else []),
                        ],
                        "env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": jobs.ORG},
                        "responses": {
                            ("GET", path): (200, {"data": vm.ROW}),
                            ("GET", path + "/metrics?" + query): (200, {"data": {"metrics": gauges.METRICS}}),
                        },
                        "json": as_json,
                        "presentation": not as_json,
                    }
                )
    for metrics in ({}, {"cpu": gauges.METRICS["cpu"]}, None):
        for as_json in (False, True):
            cases.append(
                {
                    "name": f"vm metrics static partial {metrics is None} {len(metrics or {})} {as_json}",
                    "args": ["vm", "metrics", vm.VM, "--start", "0", "--end", "200", *(["--json"] if as_json else [])],
                    "env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": jobs.ORG},
                    "responses": {
                        vm.SHOW: (200, {"data": vm.ROW | {"display_name": None, "memory_mb": None}}),
                        ("GET", vm.SHOW[1] + "/metrics?source=cpu&source=memory&start=0&end=200"): (
                            200,
                            {"data": {"metrics": metrics}},
                        ),
                    },
                    "json": as_json,
                    "presentation": not as_json,
                }
            )
    for code in (401, 403, 404, 422, 503):
        cases.append(
            {
                "name": f"vm metrics static HTTP {code}",
                "args": ["vm", "metrics", vm.VM],
                "env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": jobs.ORG},
                "responses": {
                    vm.SHOW: (200, {"data": vm.ROW}),
                    ("GET", vm.SHOW[1] + "/metrics?source=cpu&source=memory"): (code, {"detail": "fixture denied"}),
                },
                "exit": 4 if code == 401 else 1,
            }
        )
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=gauges.compare, default_report="build/go/metrics-stream-parity.json"
        )
    )
