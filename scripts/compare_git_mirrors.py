"""Git mirror management Python/Go differential validation."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import sys

ORG, REPO = inspection.ORG, inspection.REPO
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
BASE = f"/orgs/{ORG}/repos/{REPO}/git-mirrors"
CLUSTERS = f"/orgs/{ORG}/git-clusters?limit=1000&order=cluster_id.asc"
RESOLVE = f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"
CLUSTER_ROWS = [
    {"cluster_id": "gsc-fi", "datacenter_id": "fi-1", "name": "Helsinki"},
    {"cluster_id": "gsc-de", "datacenter_id": "de-1", "name": "Frankfurt"},
]
PLACEMENT = {
    "cluster_id": "gsc-fi",
    "role": "primary",
    "last_sync_status": "success",
    "last_sync_at": "2026-10-01T12:30:45.789Z",
    "config_synced": True,
}
MIRROR = {
    "repository_id": REPO,
    "full_name": "acme/web",
    "enabled": True,
    "placements": [
        PLACEMENT,
        PLACEMENT
        | {
            "cluster_id": "gsc-de",
            "role": "replica",
            "last_sync_status": None,
            "last_sync_at": None,
            "config_synced": False,
        },
    ],
}
EMPTY = MIRROR | {"enabled": False, "placements": []}
DEFAULT = {
    ("GET", BASE): (200, MIRROR),
    ("PUT", BASE): (200, MIRROR),
    ("PUT", BASE + "/placements/gsc-fi"): (200, MIRROR),
    ("DELETE", BASE + "/placements/gsc-fi"): (200, EMPTY),
    ("GET", CLUSTERS): (200, {"data": CLUSTER_ROWS, "pagination": {"next_cursor": None}}),
    ("GET", RESOLVE): (200, {"data": inspection.REPOSITORIES[0]}),
    ("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS}),
}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "git mirror " + name,
                "args": ["repo", "mirror", *args],
                "env": ENV.copy(),
                "responses": DEFAULT.copy(),
                **kwargs,
            }
        )

    descriptors = [
        ("status", ["status", "--repo", REPO], ("GET", BASE), False),
        ("enable", ["enable", "--repo", REPO], ("PUT", BASE), False),
        ("disable", ["disable", "--repo", REPO, "--yes"], ("PUT", BASE), False),
        ("place", ["place", "gsc-fi", "--repo", REPO], ("PUT", BASE + "/placements/gsc-fi"), False),
        ("unplace", ["unplace", "gsc-fi", "--repo", REPO, "--yes"], ("DELETE", BASE + "/placements/gsc-fi"), False),
        ("locations", ["locations"], ("GET", CLUSTERS), True),
        ("create", ["create", "acme/web", "--location", "gsc-fi"], ("PUT", BASE + "/placements/gsc-fi"), False),
    ]
    for verb, args, route, is_list in descriptors:
        field = "cluster_id" if is_list else "enabled"
        case(verb + " human", args, presentation=True)
        case(verb + " JSON", args + ["--json", "*"], json=True)
        case(verb + " projection", args + ["--json", field], json=True)
        case(verb + " jq", args + ["--json", field, "-q", (".[]." if is_list else ".") + field])
        case(verb + " empty fields", args + ["--json", ""], json=True)
        case(verb + " discovery", args + ["--json", "?"], env={}, responses={})
        case(verb + " bad fields", args + ["--json", "wrong"], responses={}, exit=1, error_terms=["Unknown JSON field"])
        case(
            verb + " jq without JSON", args + ["--jq", "."], responses={}, exit=2, error_terms=["--jq requires --json"]
        )
        case(verb + " no auth", args, env={}, responses={}, exit=4)
        case(verb + " org slug", args + ["--org", "AcMe"], presentation=True)
        for status in [401, 403, 404, 409, 422, 429, 503]:
            case(
                verb + " HTTP " + str(status),
                args,
                responses=DEFAULT | {route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
        if verb != "locations":
            case(
                verb + " bad source",
                args + ["--source", "GitHub"],
                responses={},
                exit=2,
                error_terms=["--source", "github", "gitlab"],
            )
            case(
                verb + " GitLab disabled",
                args + ["--source", "gitlab"],
                responses=DEFAULT
                | {
                    ("GET", RESOLVE.replace("platform=github", "platform=gitlab")): (
                        200,
                        {"data": inspection.REPOSITORIES[0]},
                    ),
                    route: (404, {"detail": "disabled"}),
                },
                exit=1,
            )
    case(
        "status empty", ["status", "--repo", REPO], responses=DEFAULT | {("GET", BASE): (200, EMPTY)}, presentation=True
    )
    case(
        "status nullable",
        ["status", "--repo", REPO],
        responses=DEFAULT | {("GET", BASE): (200, MIRROR | {"full_name": None, "placements": None})},
        presentation=True,
    )
    case("status named", ["status", "--repo", "acme/web"], presentation=True)
    case("status env", ["status"], env=ENV | {"AVR_REPO": REPO}, presentation=True)
    case("status detected", ["status"], git_remote="git@github.com:acme/web.git", presentation=True)
    case("status missing context", ["status"], responses={}, exit=1)
    case(
        "status GitLab miss",
        ["status", "--repo", "acme/web", "--source", "gitlab"],
        responses={
            ("GET", RESOLVE.replace("github", "gitlab")): (404, {"detail": {"nearby_full_names": [], "other_orgs": []}})
        },
        exit=1,
    )
    case(
        "status enriched miss",
        ["status", "--repo", "acme/web"],
        responses={
            ("GET", RESOLVE): (
                404,
                {
                    "detail": {
                        "nearby_full_names": ["acme/website"],
                        "other_orgs": [{"slug": "team", "organization_id": "org-019a0000000070008000000000000005"}],
                    }
                },
            )
        },
        exit=1,
    )
    for verb, args in [("disable", ["disable", "--repo", REPO]), ("unplace", ["unplace", "gsc-fi", "--repo", REPO])]:
        case(verb + " confirm", args, stdin="yes\n", presentation=True)
        case(verb + " decline", args, stdin="no\n", responses={}, exit=1, presentation=True)
        case(verb + " EOF", args, responses={}, exit=1, presentation=True)
        case(verb + " disabled prompts", args, env=ENV | {"AVR_PROMPT_DISABLED": "1"}, responses={}, exit=1)
        case(verb + " short yes", args + ["-y"], presentation=True)
    for body in [None, b""]:
        case(
            "unplace empty " + repr(body),
            ["unplace", "gsc-fi", "--repo", REPO, "--yes"],
            responses=DEFAULT | {("DELETE", BASE + "/placements/gsc-fi"): (204, body)},
            presentation=True,
        )
        case(
            "unplace empty JSON " + repr(body),
            ["unplace", "gsc-fi", "--repo", REPO, "--yes", "--json", "*"],
            responses=DEFAULT | {("DELETE", BASE + "/placements/gsc-fi"): (204, body)},
            json=True,
        )
    case(
        "place escaped cluster",
        ["place", "gsc/a b?#", "--repo", REPO],
        responses=DEFAULT | {("PUT", BASE + "/placements/gsc%2Fa%20b%3F%23"): (200, MIRROR)},
        presentation=True,
    )
    case(
        "locations empty",
        ["locations"],
        responses={("GET", CLUSTERS): (200, {"data": [], "pagination": {"next_cursor": None}})},
        presentation=True,
    )
    case(
        "locations paginated",
        ["locations"],
        responses={
            ("GET", CLUSTERS): (200, {"data": CLUSTER_ROWS[:1], "pagination": {"next_cursor": "next /?"}}),
            ("GET", CLUSTERS + "&cursor=next+%2F%3F"): (
                200,
                {"data": CLUSTER_ROWS[1:], "pagination": {"next_cursor": None}},
            ),
        },
        presentation=True,
    )
    for payload in [
        {"data": []},
        {"data": [], "pagination": {}},
        {"data": [], "pagination": {"next_cursor": ""}},
        {"data": [], "pagination": {"next_cursor": 12}},
        {"data": [1], "pagination": {"next_cursor": None}},
        {"data": None, "pagination": {"next_cursor": None}},
    ]:
        case(
            "locations malformed " + repr(payload), ["locations"], responses={("GET", CLUSTERS): (200, payload)}, exit=1
        )
    case(
        "locations repeated cursor",
        ["locations"],
        responses={
            ("GET", CLUSTERS): (200, {"data": [], "pagination": {"next_cursor": "repeat"}}),
            ("GET", CLUSTERS + "&cursor=repeat"): (200, {"data": [], "pagination": {"next_cursor": "repeat"}}),
        },
        exit=1,
    )
    case(
        "create enables",
        ["create", "acme/web", "--location", "gsc-fi"],
        responses=DEFAULT | {("GET", BASE): (200, EMPTY)},
        presentation=True,
    )
    for location in ["HELSINKI", "FI-1"]:
        case("create location " + location, ["create", "acme/web", "--location", location], presentation=True)
    case("create unknown location", ["create", "acme/web", "--location", "missing"], exit=1)
    case(
        "create ambiguous location",
        ["create", "acme/web", "--location", "shared"],
        responses=DEFAULT
        | {
            ("GET", CLUSTERS): (
                200,
                {"data": [x | {"name": "shared"} for x in CLUSTER_ROWS], "pagination": {"next_cursor": None}},
            )
        },
        exit=1,
    )
    case(
        "create ID precedence",
        ["create", "acme/web", "--location", "gsc-fi"],
        responses=DEFAULT
        | {
            ("GET", CLUSTERS): (
                200,
                {"data": [x | {"name": "gsc-fi"} for x in CLUSTER_ROWS], "pagination": {"next_cursor": None}},
            )
        },
        presentation=True,
    )
    case(
        "create GitLab nested",
        ["create", "acme/team/web", "--source", "gitlab", "--location", "gsc-fi"],
        responses=DEFAULT
        | {
            ("GET", RESOLVE.replace("acme%2Fweb", "acme%2Fteam%2Fweb").replace("github", "gitlab")): (
                200,
                {"data": inspection.REPOSITORIES[0]},
            )
        },
        presentation=True,
    )
    for stage, route in [("read", ("GET", BASE)), ("enable", ("PUT", BASE)), ("locations", ("GET", CLUSTERS))]:
        case(
            "create failed " + stage,
            ["create", "acme/web", "--location", "gsc-fi"],
            responses=DEFAULT | {("GET", BASE): (200, EMPTY), route: (503, {"detail": "fixture denied"})},
            exit=1,
        )
    for name in ["repo", "/repo", "a/..", "a/./b", "https://github.com/a/b", "a/b/c"]:
        case(
            "create invalid " + name,
            ["create", name, "--location", "gsc-fi"],
            responses={},
            exit=2,
            error_terms=["REPOSITORY"],
        )
    for args, term in [
        (["create", "acme/web"], "location"),
        (["create", "--location", "gsc-fi"], "REPOSITORY"),
        (["place"], "CLUSTER_ID"),
        (["unplace"], "CLUSTER_ID"),
    ]:
        case("missing " + repr(args), args, responses={}, exit=2, error_terms=[term])
    if sys.platform != "win32":
        for name in [
            "status human",
            "status detected",
            "status empty",
            "create enables",
            "locations human",
            "disable confirm",
            "disable decline",
            "unplace confirm",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "git mirror " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/git-mirror-parity.json"
        )
    )
