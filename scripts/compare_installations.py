"""GitHub installation management parity with isolated browser and polling fixtures."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import sys

ORG = inspection.ORG
INSTALL = "ins-019a0000000070008000000000000004"
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
ORGS = {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})}
LIST = ("GET", "/users/me/installations")
DELETE = ("DELETE", f"/orgs/{ORG}/installations/{INSTALL}")
ROW = {
    "installation_id": INSTALL,
    "platform_installation_id": "9007199254740993",
    "target_name": "acme",
    "organization_id": ORG,
    "organization_name": "Acme",
    "organization_slug": "acme",
    "state": "active",
    "created_at": "2026-10-01T15:30:45.123456+03:00",
    "platform": "github",
    "target_id": 123,
    "target_type": "Organization",
    "platform_settings_url": "https://github.com/settings/installations/123",
    "github_url": None,
}
NEW = ROW | {"installation_id": "ins-019a0000000070008000000000000005", "platform_installation_id": "20"}
NEW2 = ROW | {"installation_id": "ins-019a0000000070008000000000000006", "platform_installation_id": "100"}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "installation " + name,
                "args": ["org", "install", *args],
                "env": ENV.copy(),
                "responses": {LIST: (200, {"data": [ROW]}), DELETE: (204, None)},
                **kwargs,
            }
        )

    case("list", ["list"], presentation=True)
    case("list JSON", ["list", "--json", "*"], json=True)
    case("list projection", ["list", "--json", "installation_id,platform_installation_id"], json=True)
    case("list jq", ["list", "--json", "target_name", "-q", ".[].target_name"])
    case("list empty fields", ["list", "--json", ""], json=True)
    case("list empty", ["list"], responses={LIST: (200, {"data": []})}, presentation=True)
    case(
        "list fallbacks",
        ["list"],
        responses={
            LIST: (
                200,
                {
                    "data": [
                        ROW | {"organization_name": None},
                        ROW | {"organization_name": None, "organization_slug": None, "platform_installation_id": None},
                    ]
                },
            )
        },
        presentation=True,
    )
    case("list no org", ["list"], env={"AVR_TOKEN": harness.TOKEN}, presentation=True)
    case("list discovery", ["list", "--json", "?"], env={}, responses={})
    case("list unknown fields", ["list", "--json", "wrong"], responses={}, exit=1, error_terms=["Unknown JSON field"])
    case("list jq without JSON", ["list", "--jq", "."], responses={}, exit=2, error_terms=["--jq requires --json"])
    for verb, args, route in [
        ("list", ["list"], LIST),
        ("add", ["add", "--no-browser", "--wait-seconds", "0"], LIST),
        ("remove", ["remove", "--installation-id", INSTALL, "--yes"], DELETE),
    ]:
        case(verb + " no auth", args, env={}, responses={}, exit=4)
        for status in [401, 403, 404, 409, 422, 429, 503]:
            case(
                verb + " HTTP " + str(status),
                args,
                responses={route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
    case("add skip", ["add", "--no-browser", "--wait-seconds", "0"])
    case("add negative wait", ["add", "--no-browser", "--wait-seconds", "-1"])
    case("add browser", ["add", "--wait-seconds", "0"], browser_capture=True)
    case(
        "add org slug",
        ["add", "--no-browser", "--wait-seconds", "0", "--org", "AcMe"],
        responses=ORGS | {LIST: (200, {"data": [ROW]})},
    )
    case(
        "add detected sorted distinct",
        ["add", "--no-browser", "--wait-seconds", "1"],
        responses={LIST: [(200, {"data": [ROW]}), (200, {"data": [ROW, NEW, NEW2, NEW]})]},
    )
    case("add timeout", ["add", "--no-browser", "--wait-seconds", "1"])
    case(
        "add poll failure",
        ["add", "--no-browser", "--wait-seconds", "1"],
        responses={LIST: [(200, {"data": [ROW]}), (403, {"detail": "poll denied"})]},
        exit=1,
    )
    case("add invalid wait", ["add", "--wait-seconds", "wrong"], responses={}, exit=2, error_terms=["--wait-seconds"])
    args = ["remove", "--installation-id", INSTALL]
    case("remove yes", args + ["--yes"])
    case("remove short yes", args + ["-y"])
    case("remove confirm", args, stdin="yes\n", presentation=True)
    case("remove decline", args, stdin="n\n", responses={}, exit=1, presentation=True)
    case("remove default no", args, stdin="\n", responses={}, exit=1, presentation=True)
    case("remove EOF", args, responses={}, exit=1, presentation=True)
    case("remove disabled", args, env=ENV | {"AVR_PROMPT_DISABLED": "1"}, responses={}, exit=1)
    case("remove org slug", args + ["--yes", "--org", "acme"], responses=ORGS | {DELETE: (204, None)})
    case("remove missing option", ["remove", "--yes"], responses={}, exit=2, error_terms=["--installation-id"])
    case("plural alias", ["list"], presentation=True)
    cases[-1]["args"][0] = "orgs"
    if sys.platform != "win32":
        case(
            "add interrupted",
            ["add", "--no-browser"],
            interrupt_after_text="Waiting for installation to appear...",
            exit=130,
        )
        for name in ["list", "list fallbacks", "add skip", "remove confirm", "remove decline"]:
            item = deepcopy(next(c for c in cases if c["name"] == "installation " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/installation-parity.json"
        )
    )
