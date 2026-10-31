"""Pull-request listing parity, including availability and terminal tables."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from scripts import compare_workflow as workflow
from urllib.parse import urlencode
import sys

ORG, REPO = inspection.ORG, inspection.REPO
FEATURE = ("GET", f"/orgs/{ORG}/feature-flags/feature.org-pull-requests.enabled")
FLAG = {FEATURE: (200, {"enabled": True, "key": "feature.org-pull-requests.enabled"})}
PULL = {
    "number": 9007199254740993,
    "title": "A pull request with a sufficiently long title to exercise truncation and spacing",
    "state": "open",
    "draft": False,
    "merged": False,
    "author_login": "long-author-login-with-suffix",
    "base_ref": "main",
    "head_ref": "feature",
    "head_sha": "abcdef01",
    "base_sha": "abc00000",
    "created_at": "2026-01-01T00:00:00Z",
    "updated_at": "2026-01-02T00:00:00Z",
    "comment_count": 0,
    "unresolved_thread_count": 2,
    "check_status": "pending",
    "mergeability": {"status": "blocked", "reasons": ["pending_checks"]},
    "repository_id": REPO,
    "repository_full_name": "acme/web",
}
PULLS = [
    PULL,
    PULL | {"number": 2, "title": "Draft", "draft": True, "mergeability": None, "check_status": None},
    PULL | {"number": 3, "title": "Merged", "state": "closed", "merged": True, "mergeability": None},
    PULL | {"number": 4, "title": "Closed draft", "state": "closed", "draft": True, "mergeability": None},
    PULL | {"number": 5, "title": "未知", "author_login": None, "unresolved_thread_count": None, "mergeability": None},
]
ORGS = {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})}


def responses(params=None, data=None, cursor=None):
    path = f"/orgs/{ORG}/pull-requests?" + urlencode(
        params if params is not None else {"scope": "all", "limit": 20, "state": "open"}, doseq=True
    )
    return FLAG | {
        ("GET", path): (200, {"data": PULLS if data is None else data, "pagination": {"next_cursor": cursor}})
    }


def scenarios():
    cases = []

    def case(name, extra=None, **kwargs):
        cases.append(
            {"name": "pr list " + name, "args": ["pr", "list", *(extra or [])], "responses": responses(), **kwargs}
        )

    case("TSV", tsv=True)
    case("JSON", ["--json", "*"], json=True)
    case("projected jq", ["--json", "number,title", "-q", "map(.number)"], json=True)
    case(
        "missing fields",
        responses=responses(
            data=[
                PULL | {"updated_at": None, "number": None, "title": None, "author_login": None, "check_status": None}
            ]
        ),
        tsv=True,
    )
    case(
        "unicode cursor",
        ["--cursor", "é" * 512],
        responses=responses({"scope": "all", "state": "open", "limit": 20, "cursor": "é" * 512}),
        tsv=True,
    )
    case("empty", responses=responses(data=[]), tsv=True)
    case("empty JSON", ["--json", "*"], responses=responses(data=[]), json=True)
    case("fields without auth", ["--json", "?"], responses={}, env={})
    case("unknown field", ["--json", "bad"], responses={}, exit=1, error_terms=["bad"])
    case("jq without JSON", ["--jq", "."], responses={}, exit=2, error_terms=["--jq", "--json"])
    case("alias", args=["prs", "list", "--json", "*"], json=True)
    for scope in ["all", "authored", "involved"]:
        case(
            "scope " + scope,
            ["--scope", scope.upper(), "--json", "*"],
            responses=responses({"scope": scope, "limit": 20, "state": "open"}),
            json=True,
        )
    for state in ["open", "closed", "merged", "all"]:
        params = {"scope": "all", "limit": 20}
        if state != "all":
            params["state"] = state
        case("state " + state, ["--state", state.upper(), "--json", "*"], responses=responses(params), json=True)
    case(
        "limit",
        ["-L", "200", "--json", "*"],
        responses=responses({"scope": "all", "state": "open", "limit": 200}),
        json=True,
    )
    case(
        "cursor",
        ["--cursor", "next"],
        responses=responses({"scope": "all", "state": "open", "limit": 20, "cursor": "next"}, cursor="later"),
        tsv=True,
    )
    case("cursor JSON", ["--json", "*"], responses=responses(cursor="later"), json=True)
    case(
        "single repo",
        ["--repo", REPO],
        responses=responses({"scope": "all", "state": "open", "limit": 20, "repository_ids": [REPO]}),
        tsv=True,
    )
    repo2 = "rep-019a0000000070008000000000000007"
    case(
        "multiple repos",
        ["--repo", REPO, "--repo", repo2],
        responses=responses({"scope": "all", "state": "open", "limit": 20, "repository_ids": [REPO, repo2]}),
        tsv=True,
    )
    resolved = {("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb"): (200, {"data": inspection.REPOSITORIES[0]})}
    case(
        "repo name",
        ["--repo", "acme/web"],
        responses=resolved | responses({"scope": "all", "state": "open", "limit": 20, "repository_ids": [REPO]}),
        tsv=True,
    )
    case(
        "Git repo",
        git_remote="git@github.com:acme/web.git",
        responses={
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"): (
                200,
                {"data": inspection.REPOSITORIES[0]},
            )
        }
        | responses({"scope": "all", "state": "open", "limit": 20, "repository_ids": [REPO]}),
        tsv=True,
    )
    missing = {("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"): (404, {"detail": "missing"})}
    case("Git fallback", git_remote="git@github.com:acme/web.git", responses=missing | responses(), tsv=True)
    case("org slug", ["--org", "acme"], responses=ORGS | responses(), tsv=True)
    case("controls in TSV", responses=responses(data=[PULL | {"title": "tab\tline\nescape\x1b"}]), tsv=True)
    case("disabled", responses={FEATURE: (200, {"enabled": False})}, exit=1)
    case("availability missing", responses={FEATURE: (404, {"detail": "missing"})}, exit=1)
    case("flag absent", responses={FEATURE: (200, {})}, exit=1)
    case(
        "disabled before repo/cursor",
        ["--repo", "bad", "--cursor", ""],
        responses={FEATURE: (200, {"enabled": False})},
        exit=1,
    )
    case("missing auth", responses={}, env={}, exit=4)
    for code in [401, 403, 429, 503]:
        case(
            f"feature HTTP {code}",
            responses={FEATURE: (code, {"detail": "fixture denied"})},
            exit=4 if code == 401 else 1,
        )
        base = responses()
        endpoint = next(k for k in base if k != FEATURE)
        base[endpoint] = (code, {"detail": "fixture denied"})
        case(f"list HTTP {code}", responses=base, exit=4 if code == 401 else 1)
    for option, value in [("--limit", "0"), ("--limit", "201"), ("--scope", "invalid"), ("--state", "invalid")]:
        case("invalid " + option + value, [option, value], responses={}, exit=2, error_terms=[option])
    for value in ["", " leading", "trailing ", "x" * 513]:
        case(
            "invalid cursor " + repr(value[:12]), ["--cursor", value], responses=FLAG, exit=2, error_terms=["--cursor"]
        )
    if sys.platform != "win32":
        for name in ["TSV", "single repo", "multiple repos", "empty", "Git fallback", "Git repo", "missing fields"]:
            item = deepcopy(next(c for c in cases if c["name"] == "pr list " + name))
            item["name"] += " terminal"
            item.pop("tsv", None)
            item["args"] = ["--no-links", *item["args"]]
            item["tty"] = item["merge_terminal"] = item["presentation"] = True
            item["env"] = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
            if name in ["TSV", "single repo"]:
                cases.append(item | {"name": item["name"] + " no color", "args": ["--no-color", *item["args"]]})
        item = deepcopy(cases[0])
        item["name"] = "pr list linked terminal"
        item.pop("tsv", None)
        item["responses"] |= ORGS
        item["tty"] = item["merge_terminal"] = item["presentation"] = True
        item["env"] = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": "", "TERM": "xterm-256color"}
        item["links"] = [f"http://127.0.0.1/org/acme/repos/{REPO}/pulls/{pull['number']}" for pull in PULLS]
        cases.append(item)
    if sys.platform != "win32":
        for factory, name in [
            (inspection.scenarios, "run git origin"),
            (workflow.scenarios, "workflow list git context"),
        ]:
            item = deepcopy(next(c for c in factory() if c["name"] == name))
            item["name"] = "repository hints " + name
            item["args"] = ["--no-links", *item["args"][:-2]]
            item.pop("json", None)
            item["tty"] = item["merge_terminal"] = item["presentation"] = True
            item["env"] = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    for item in cases:
        item.setdefault("env", {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG})
    return cases


def compare(scenario, python, go):
    differences = watch.compare(scenario, python, go)
    if scenario.get("tsv") and python["stdout"] != go["stdout"]:
        differences.append("TSV bytes")
    return differences


if __name__ == "__main__":
    sys.exit(harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/pr-parity.json"))
