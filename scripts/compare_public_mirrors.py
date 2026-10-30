"""Public mirror availability and request management differential validation."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import sys

ORG, REPO = inspection.ORG, inspection.REPO
REQUEST = "pmr-019a0000000070008000000000000004"
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
ORGS = {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})}
BASE = f"/orgs/{ORG}/public-mirrors/requests"
LOOKUP = ("GET", "/public-mirrors/rust-lang/rust")
VIEW = ("GET", BASE + "/" + REQUEST)
DELETE = ("DELETE", BASE + "/" + REQUEST)
CATALOG = {
    "repository_id": REPO,
    "platform_repository_id": 9007199254740993,
    "repository_full_name": "rust-lang/rust",
    "https_clone_url": "https://github.com/rust-lang/rust.git",
    "default_branch": "main",
    "platform_owner_id": 9007199254740995,
    "platform_owner_type": "Organization",
    "platform_owner_login": "rust-lang",
    "is_archived": False,
    "is_disabled": False,
    "is_fork": False,
    "platform_size_kb": 123456,
    "platform_pushed_at": "2026-10-01T12:30:45Z",
    "public_metadata_verified_at": "2026-10-01T12:31:00Z",
    "approval_state": "approved",
    "public_access_expires_at": "2026-11-01T12:00:00Z",
    "mirror_enabled": True,
    "installation_kind": "public_mirror",
}
ROW = {
    "request_id": REQUEST,
    "repository_id": REPO,
    "repository_full_name": "rust-lang/rust",
    "status": "pending",
    "requester_organization_id": ORG,
    "requester_user_id": "usr-019a0000000070008000000000000003",
    "reason": None,
    "github_snapshot": {"stars": 9007199254740993, "private": False, "nested": {"unknown": None}},
    "created_at": "2026-10-01T12:30:45.789123Z",
    "updated_at": "2026-10-02T15:00:00+03:00",
    "reviewed_at": None,
    "reviewed_by_user_id": None,
    "review_note": None,
    "approval_state": None,
    "public_access_expires_at": None,
}
REVIEWED = ROW | {
    "status": "approved",
    "reason": "Build dependency",
    "reviewed_at": "2026-10-03T10:00:00Z",
    "review_note": "Approved",
    "reviewed_by_user_id": "usr-019a0000000070008000000000000005",
    "approval_state": "approved",
    "public_access_expires_at": "2026-11-01T12:00:00Z",
}
CANCELLED = ROW | {"status": "cancelled"}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {"name": "public mirror " + name, "args": ["repo", "public-mirror", *args], "env": ENV.copy(), **kwargs}
        )

    descriptors = [
        ("check", ["check", "rust-lang/rust"], LOOKUP, (200, CATALOG), "repository_full_name", False),
        ("request", ["request", "rust-lang/rust"], ("POST", BASE), (201, ROW), "request_id", False),
        ("requests", ["requests"], ("GET", BASE), (200, [ROW, REVIEWED]), "request_id", True),
        ("view", ["view", REQUEST], VIEW, (200, ROW), "request_id", False),
        ("cancel", ["cancel", REQUEST, "--yes"], DELETE, (200, CANCELLED), "request_id", False),
    ]
    for label, args, route, response, field, is_list in descriptors:
        responses = {route: response}
        case(label + " human", args, responses=responses, presentation=True)
        case(label + " JSON", args + ["--json", "*"], responses=responses, json=True)
        case(label + " projection", args + ["--json", field], responses=responses, json=True)
        case(label + " jq", args + ["--json", field, "-q", (".[]." if is_list else ".") + field], responses=responses)
        case(label + " empty fields", args + ["--json", ""], responses=responses, json=True)
        case(label + " discovery", args + ["--json", "?"], env={}, responses={})
        case(
            label + " unknown field",
            args + ["--json", "unknown"],
            responses={},
            exit=1,
            error_terms=["Unknown JSON field"],
        )
        case(
            label + " jq without JSON", args + ["--jq", "."], responses={}, exit=2, error_terms=["--jq requires --json"]
        )
        case(label + " no auth", args, env={}, responses={}, exit=4)
        if label != "check":
            case(label + " org slug", args + ["--org", "AcMe"], responses=responses | ORGS, presentation=True)
            case(
                label + " explicit org",
                args + ["--org", ORG],
                env=ENV | {"AVR_ORG": "org-other"},
                responses=responses,
                presentation=True,
            )
        for status in [401, 403, 404, 409, 422, 429, 503]:
            case(
                label + " HTTP " + str(status),
                args,
                responses={route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 0 if label == "check" and status == 404 else 1,
            )
    case(
        "check no org",
        ["check", "rust-lang/rust"],
        env={"AVR_TOKEN": harness.TOKEN},
        responses={LOOKUP: (200, CATALOG)},
        presentation=True,
    )
    case(
        "check missing JSON",
        ["check", "rust-lang/rust", "--json", "*"],
        responses={LOOKUP: (404, {"detail": "not mirrored"})},
        json=True,
    )
    case(
        "check missing jq",
        ["check", "rust-lang/rust", "--json", "available", "-q", ".available"],
        responses={LOOKUP: (404, {"detail": "not mirrored"})},
    )
    case(
        "check nullable metadata",
        ["check", "rust-lang/rust"],
        responses={LOOKUP: (200, CATALOG | {"default_branch": None, "public_access_expires_at": None})},
        presentation=True,
    )
    case(
        "check escaped segments",
        ["check", "owner/a b?#%"],
        responses={("GET", "/public-mirrors/owner/a%20b%3F%23%25"): (200, CATALOG)},
        presentation=True,
    )
    for name in ["rust", "/rust", "rust/", "../rust", "rust/..", "./rust", "rust/.", "a/b/c"]:
        case(
            "check invalid " + repr(name), ["check", name], responses={}, exit=2, error_terms=["owner/repository name"]
        )
    case("check discovery before invalid", ["check", "../rust", "--json", "?"], env={}, responses={})
    for reason in ["Build dependency", ""]:
        case(
            "request reason " + repr(reason),
            ["request", "rust-lang/rust", "--reason", reason],
            responses={("POST", BASE): (201, ROW | {"reason": reason})},
            presentation=True,
        )
    case("requests empty", ["requests"], responses={("GET", BASE): (200, [])}, presentation=True)
    case("view reviewed", ["view", REQUEST], responses={VIEW: (200, REVIEWED)}, presentation=True)
    for status, body in [(204, None), (200, b""), (200, None), (200, {})]:
        for json_output in [False, True]:
            case(
                "cancel empty " + repr((status, body, json_output)),
                ["cancel", REQUEST, "--yes", *(["--json", "*"] if json_output else [])],
                responses={DELETE: (status, body)},
                json=json_output,
            )
    case("cancel confirm", ["cancel", REQUEST], stdin="yes\n", responses={DELETE: (200, CANCELLED)}, presentation=True)
    case("cancel short yes", ["cancel", REQUEST, "-y"], responses={DELETE: (200, CANCELLED)})
    case("cancel decline", ["cancel", REQUEST], stdin="n\n", responses={}, exit=1, presentation=True)
    case("cancel default no", ["cancel", REQUEST], stdin="\n", responses={}, exit=1, presentation=True)
    case("cancel EOF", ["cancel", REQUEST], responses={}, exit=1, presentation=True)
    case("cancel disabled", ["cancel", REQUEST], env=ENV | {"AVR_PROMPT_DISABLED": "1"}, responses={}, exit=1)
    for verb, arg in [
        ("check", "FULL_NAME"),
        ("request", "FULL_NAME"),
        ("view", "REQUEST_ID"),
        ("cancel", "REQUEST_ID"),
    ]:
        case(verb + " missing argument", [verb], responses={}, exit=2, error_terms=[arg])
    case("plural alias", ["check", "rust-lang/rust"], responses={LOOKUP: (200, CATALOG)})
    cases[-1]["args"][0] = "repos"
    if sys.platform != "win32":
        for name in [
            "check human",
            "check HTTP 404",
            "request human",
            "requests human",
            "view reviewed",
            "cancel confirm",
            "cancel decline",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "public mirror " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/public-mirror-parity.json"
        )
    )
