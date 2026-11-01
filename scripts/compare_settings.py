"""Settings parity: scope precedence, typed values, tables and write refusal."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import sys

ORG, REPO = inspection.ORG, inspection.REPO
BASE = f"/orgs/{ORG}/settings"
REPO_BASE = f"/orgs/{ORG}/repos/{REPO}/settings"
ORGS = {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})}
RESOLVE = f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb"
RESOLVED = (200, {"data": inspection.REPOSITORIES[0]})
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
ROWS = [
    {"key": "cache.gha.enabled", "value": True, "source": "repository", "redacted": False},
    {"key": "cache.packages.enabled", "value": False, "source": "organization"},
    {"key": "runner.memory", "value": 4096, "source": "default"},
    {"key": "runner.label", "value": "linux-arm64", "source": "organization"},
    {"key": "runner.tags", "value": ["linux", "arm64"], "source": "default"},
]
SCHEMA = {
    "key": "runner.memory",
    "value_type": "integer",
    "default": 4096,
    "scopes": ["organization", "repository"],
    "inherits": True,
    "description": "Memory available to the runner",
    "choices": [],
    "min_value": 512,
    "max_value": 65536,
    "repository_independent": False,
}
SCHEMAS = [
    SCHEMA,
    SCHEMA
    | {"key": "cache.gha.enabled", "value_type": "boolean", "default": True, "min_value": None, "max_value": None},
    SCHEMA
    | {
        "key": "runner.kind",
        "value_type": "string",
        "default": "small",
        "choices": ["small", "large"],
        "min_value": None,
        "max_value": None,
    },
    SCHEMA | {"key": "runner.minimum", "min_value": 0, "max_value": None},
    SCHEMA | {"key": "runner.maximum", "min_value": None, "max_value": 0, "inherits": False},
    SCHEMA
    | {"key": "runner.tags", "value_type": "string_list", "default": ["a", "b"], "min_value": None, "max_value": None},
]


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "settings " + name,
                "args": ["settings", *args],
                "env": ENV.copy(),
                "responses": {("GET", BASE): (200, ROWS)},
                **kwargs,
            }
        )

    case("list", ["list"], presentation=True)
    case("list empty", ["list"], responses={("GET", BASE): (200, [])}, presentation=True)
    case("list JSON", ["list", "--json", "*"], json=True)
    case("list jq", ["list", "--json", "key,value", "-q", ".[] | select(.value == true) | .key"])
    case(
        "list prefix",
        ["list", "--prefix", "cache."],
        responses={("GET", BASE + "?prefix=cache."): (200, ROWS[:2])},
        presentation=True,
    )
    case("schema", ["schema"], responses={("GET", "/settings/schema"): (200, SCHEMAS)}, presentation=True)
    case("schema empty", ["schema"], responses={("GET", "/settings/schema"): (200, [])}, presentation=True)
    case("schema JSON", ["schema", "--json", "*"], responses={("GET", "/settings/schema"): (200, SCHEMAS)}, json=True)
    case(
        "schema filters",
        ["schema", "--prefix", "runner.", "--scope", "repository", "--json", "key,default"],
        responses={("GET", "/settings/schema?prefix=runner.&scope=repository"): (200, SCHEMAS)},
        json=True,
    )
    case(
        "schema organization",
        ["schema", "--scope", "organization"],
        responses={("GET", "/settings/schema?scope=organization"): (200, SCHEMAS)},
        presentation=True,
    )
    case("schema invalid scope", ["schema", "--scope", "repo"], responses={}, exit=2, error_terms=["--scope"])
    case("schema empty scope", ["schema", "--scope", ""], responses={}, exit=2, error_terms=["--scope"])
    for verb, path in [("list", BASE), ("schema", "/settings/schema")]:
        case(verb + " discovery", [verb, "--json", "?"], env={}, responses={})
        case(
            verb + " projection",
            [verb, "--json", "key"],
            responses={("GET", path): (200, ROWS if verb == "list" else SCHEMAS)},
            json=True,
        )
        case(
            verb + " empty fields",
            [verb, "--json", ""],
            responses={("GET", path): (200, ROWS if verb == "list" else SCHEMAS)},
            json=True,
        )
        case(
            verb + " unknown field",
            [verb, "--json", "unknown"],
            responses={},
            exit=1,
            error_terms=["Unknown JSON field"],
        )
        case(verb + " jq without json", [verb, "-q", "."], responses={}, exit=2, error_terms=["--jq requires --json"])
        case(verb + " no auth", [verb], env={}, responses={}, exit=4)
        for status in [401, 403, 404, 422, 429, 503]:
            case(
                verb + " HTTP " + str(status),
                [verb],
                responses={("GET", path): (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )

    for verb in ["list", "set", "reset"]:
        args = [
            verb,
            *(["cache.gha.enabled", "false"] if verb == "set" else ["cache.gha.enabled"] if verb == "reset" else []),
        ]
        method = {"list": "GET", "set": "PUT", "reset": "DELETE"}[verb]
        suffix = "/cache.gha.enabled" if verb == "reset" else ""
        response = (
            (204, None)
            if verb == "reset"
            else (200, ROWS if verb == "list" else ROWS[1] | {"key": "cache.gha.enabled"})
        )

        def route(base, method=method, suffix=suffix, response=response):
            return {(method, base + suffix): response}

        case(
            verb + " explicit org suppresses ambient",
            args + ["--org", ORG],
            env=ENV | {"AVR_REPO": "acme/wrong"},
            git_remote="git@github.com:acme/wrong.git",
            responses=route(BASE),
            presentation=True,
        )
        case(
            verb + " explicit repo wins",
            args + ["--org", ORG, "--repo", REPO],
            env=ENV | {"AVR_REPO": "acme/wrong"},
            responses=route(REPO_BASE),
            presentation=True,
        )
        case(verb + " ambient repo", args, env=ENV | {"AVR_REPO": REPO}, responses=route(REPO_BASE), presentation=True)
        case(
            verb + " repo name",
            args + ["--repo", "acme/web"],
            responses=route(REPO_BASE) | {("GET", RESOLVE): RESOLVED},
            presentation=True,
        )
        case(
            verb + " git",
            args,
            git_remote="git@github.com:acme/web.git",
            responses=route(REPO_BASE) | {("GET", RESOLVE + "&platform=github"): RESOLVED},
            presentation=True,
        )
        case(verb + " org slug", args + ["--org", "AcMe"], responses=route(BASE) | ORGS, presentation=True)
        missing = {
            ("GET", RESOLVE + "&platform=github"): (404, {"detail": {"nearby_full_names": [], "other_orgs": []}})
        }
        case(
            verb + " disconnected git",
            args,
            git_remote="git@github.com:acme/web.git",
            responses=missing | (route(BASE) if verb == "list" else {}),
            exit=0 if verb == "list" else 1,
            presentation=True,
        )
        case(
            verb + " git forbidden",
            args,
            git_remote="git@github.com:acme/web.git",
            responses={("GET", RESOLVE + "&platform=github"): (403, {"detail": "denied"})},
            exit=1,
        )
        case(
            verb + " missing explicit repo",
            args + ["--repo", "acme/web"],
            responses={("GET", RESOLVE): (404, {"detail": "not connected"})},
            exit=1,
        )
        if verb != "list":
            case(verb + " no auth", args, env={}, responses={}, exit=4)
            for status in [401, 403, 404, 422, 429, 503]:
                case(
                    verb + " repo HTTP " + str(status),
                    args + ["--repo", REPO],
                    responses={(method, REPO_BASE + suffix): (status, {"detail": "fixture denied"})},
                    exit=4 if status == 401 else 1,
                )
            case(
                verb + " ambient org-only hint",
                args,
                env=ENV | {"AVR_REPO": REPO},
                responses={(method, REPO_BASE + suffix): (422, {"detail": "org only"})},
                exit=1,
            )
            case(
                verb + " org validation",
                args,
                responses={(method, BASE + suffix): (422, {"detail": "invalid"})},
                exit=1,
            )

    for raw, parsed in [
        ("true", True),
        ("YES", True),
        ("on", True),
        ("false", False),
        ("No", False),
        ("OFF", False),
        ("0", 0),
        ("-42", -42),
        (" +0042 ", 42),
        ("1_000", 1000),
        ("99999999999999999999999", 99999999999999999999999),
        ("1.5", "1.5"),
        (" true ", " true "),
        ("", ""),
        ("1__0", "1__0"),
        ("linux", "linux"),
    ]:
        case(
            "set value " + repr(raw),
            ["set", "--", "runner.value", raw],
            responses={("PUT", BASE): (200, {"key": "runner.value", "value": parsed, "source": "organization"})},
            presentation=True,
        )
    case(
        "reset encoded key",
        ["reset", "runner/a b?#%"],
        responses={("DELETE", BASE + "/runner%2Fa%20b%3F%23%25"): (204, None)},
    )
    case("list web", ["list", "--web", "--org", ORG], responses=ORGS)
    case("list repo web", ["list", "--web", "--repo", REPO], responses=ORGS)
    case(
        "list web JSON rejected",
        ["list", "--web", "--json", "?"],
        responses={},
        exit=2,
        error_terms=["--web", "--json", "mutually exclusive"],
    )
    for args in [["set"], ["set", "key"], ["reset"], ["reset", "key", "extra"]]:
        case(
            "arguments " + repr(args),
            args,
            responses={},
            exit=2,
            error_terms=[
                "unexpected extra argument" if len(args) > 2 else "Missing argument",
                "extra" if len(args) > 2 else "VALUE" if len(args) > 1 else "KEY",
            ],
        )
    if sys.platform != "win32":
        for name in [
            "list",
            "schema",
            "list git",
            "set git",
            "reset git",
            "set ambient org-only hint",
            "list disconnected git",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "settings " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
        for name in ["list web", "list repo web"]:
            item = deepcopy(next(c for c in cases if c["name"] == "settings " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = item["browser_capture"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/settings-parity.json"
        )
    )
