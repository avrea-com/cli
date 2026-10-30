"""Paired VM bootstrap checks with local API and credential/SSH stubs."""

from copy import deepcopy
from scripts import bootstrap_fixture as fixture
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from scripts import compare_vm_inspection as vm
from scripts import compare_vm_ssh_config as ssh_config
import sys

ALL_FLAGS = [
    "--env",
    "FIRST=fixture-env-secret",
    "--env",
    "EMPTY=",
    "--env",
    "FIRST=fixture-override-secret",
    "--setup-github",
    "--repo",
    "https://example.test/team/project.git",
    "--ref",
    "feature/go",
    "--dotfiles",
    "https://example.test/dotfiles.git",
    "--install",
    "claude,codex",
    "--install-avr",
    "--run",
    "printf '%s\\n' 'hello world'",
]


def scenarios():
    cases = []

    def case(name, flags=None, **kwargs):
        cases.append(
            {
                "name": "vm bootstrap " + name,
                "args": ["vm", "bootstrap", vm.VM, "--print", *(flags or [])],
                "env": fixture.CREDS.copy(),
                "responses": {},
                "capture_files": [],
                "bootstrap_stub": {},
                **kwargs,
            }
        )

    case("all print", ALL_FLAGS, presentation=True)
    for name, flags in [
        ("github print", ["--setup-github"]),
        ("repo print", ["--repo", "git@example.test:team/project.git"]),
        ("quoted repo", ["--repo", "https://example.test/a'b $(id).git", "--ref", "feature/go"]),
        ("dotfiles print", ["--dotfiles", "https://example.test/a'b.git"]),
        ("avr print", ["--install-avr"]),
        ("agents ordered", ["--install", " CODEX,claude ", "--install", "codex,,CLAUDE"]),
        ("env ordered", ["--env", " B =one", "--env", "A=two=three", "--env", "B=override"]),
        ("env quoting", ["--env", "SECRET=a'b $HOME;$(id)"]),
        ("env empty", ["--env", "EMPTY="]),
        ("run empty", ["--run", ""]),
        ("run inline", ["--run", "printf '%s\\n' 'a b'\necho second"]),
        ("negative toggle", ["--setup-github", "--no-setup-github", "--run", "true"]),
        ("positive toggle", ["--no-setup-github", "--setup-github"]),
    ]:
        case(name, flags, presentation=True)
    for name in ["é", "变量", "_PRIVATE", "a·b", "℘", "K", "á"]:
        case("unicode env " + name, ["--env", name + "=fixture-env-secret"], presentation=True)
    case(
        "bare env",
        ["--env", "BOOTSTRAP_FIXTURE"],
        env=fixture.CREDS | {"BOOTSTRAP_FIXTURE": "fixture-env-secret"},
        presentation=True,
    )
    case(
        "bare empty env",
        ["--env", "BOOTSTRAP_FIXTURE"],
        env=fixture.CREDS | {"BOOTSTRAP_FIXTURE": ""},
        presentation=True,
    )
    for path in ["run.sh", "~/run.sh", "FIXTURE_HOME/run.sh"]:
        case(
            "run file " + path,
            ["--run", "@" + path],
            fixture_files={"run.sh": b"echo one\r\necho two\recho three\n"},
            presentation=True,
        )
    for name, flags, terms in [
        ("nothing", [], ["Nothing to do"]),
        ("empty agents", ["--install", ", ,"], ["Nothing to do"]),
        ("no credentials", ["--forward-agent-creds"], ["Nothing to do"]),
        ("invalid agent", ["--install", "codex,other"], ["--install", "unknown agent", "other"]),
        ("ref without repo", ["--ref", "main"], ["--ref requires --repo"]),
        ("bad ref", ["--repo", "url", "--ref", "../bad"], ["--ref"]),
        ("missing env", ["--env", "BOOTSTRAP_FIXTURE_MISSING"], ["--env", "not set"]),
        ("invalid env", ["--env", "BAD-NAME=value"], ["--env", "not a valid variable name"]),
        ("digit env", ["--env", "1NAME=value"], ["--env", "not a valid variable name"]),
        ("nfkc invalid env", ["--env", "℀=value"], ["--env", "not a valid variable name"]),
        ("empty env name", ["--env", "=value"], ["--env", "not a valid variable name"]),
        ("newline env", ["--env", "NAME=one\ntwo"], ["--env", "newlines"]),
        ("missing file", ["--run", "@missing.sh"], ["--run", "cannot read run script"]),
        ("directory file", ["--run", "@."], ["--run", "cannot read run script"]),
    ]:
        case(name, flags, exit=2, error_terms=terms)
    case("missing ID", args=["vm", "bootstrap", "--run", "true"], exit=2, error_terms=["VM_ID"])
    case(
        "extra argument",
        args=["vm", "bootstrap", vm.VM, "extra", "--print", "--run", "true"],
        exit=2,
        error_terms=["unexpected extra argument"],
    )
    for flag in ["--org", "--identity", "-i", "--install", "--repo", "--ref", "--dotfiles", "--env", "--run"]:
        case("missing argument " + flag, [flag], exit=2, error_terms=[flag, "argument"])
    for name, flags, env in [
        (
            "forward both",
            ["--forward-agent-creds", "--install", "claude,codex"],
            {
                "ANTHROPIC_API_KEY": "fixture-api-secret",
                "CLAUDE_CODE_OAUTH_TOKEN": "fixture-oauth-secret",
                "OPENAI_API_KEY": "fixture-openai-secret",
            },
        ),
        (
            "forward only codex",
            ["--forward-agent-creds", "--install", "codex"],
            {"ANTHROPIC_API_KEY": "fixture-api-secret", "OPENAI_API_KEY": "fixture-openai-secret"},
        ),
        ("forward no install", ["--forward-agent-creds"], {"OPENAI_API_KEY": "fixture-openai-secret"}),
        (
            "explicit empty credential",
            ["--forward-agent-creds", "--install", "claude", "--env", "ANTHROPIC_API_KEY="],
            {"ANTHROPIC_API_KEY": "fixture-api-secret"},
        ),
        ("missing agent credentials", ["--forward-agent-creds", "--install", "claude,codex"], {}),
        (
            "no implicit forwarding",
            ["--install", "claude,codex"],
            {"ANTHROPIC_API_KEY": "fixture-api-secret", "OPENAI_API_KEY": "fixture-openai-secret"},
        ),
    ]:
        case(name, flags, env=fixture.CREDS | env, presentation=True)
    if sys.platform != "win32":
        live = vm.ENV | fixture.CREDS
        response = {vm.SHOW: (200, {"data": vm.ROW | {"endpoints": {"ssh": ssh_config.EP}}})}

        def real(name, flags, **kwargs):
            case(
                name,
                **(
                    {
                        "args": ["vm", "bootstrap", vm.VM, *flags],
                        "env": live.copy(),
                        "responses": deepcopy(response),
                    }
                    | kwargs
                ),
            )

        real(
            "all run",
            ALL_FLAGS,
            presentation=True,
            step_names=["env", "github", "repo", "dotfiles", "install-agents", "install-avr", "run"],
        )
        real("identity run", ["-i", "my key", "--run", "true"], presentation=True)
        real(
            "org slug",
            ["--org", "AcMe", "--run", "true"],
            responses=response | {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})},
        )
        real("no auth", ["--run", "true"], env=fixture.CREDS.copy(), responses={}, exit=4)
        real(
            "noninteractive missing credentials",
            ["--forward-agent-creds", "--install", "claude,codex"],
            presentation=True,
        )
        real(
            "forward oauth",
            ["--forward-agent-creds", "--install", "claude"],
            env=live | {"CLAUDE_CODE_OAUTH_TOKEN": "fixture-oauth-secret"},
            presentation=True,
        )
        real(
            "explicit wins",
            ["--forward-agent-creds", "--install", "codex", "--env", "OPENAI_API_KEY=fixture-override-secret"],
            env=live | {"OPENAI_API_KEY": "fixture-openai-secret"},
        )
        real(
            "no implicit live forwarding",
            ["--install", "claude"],
            env=live | {"ANTHROPIC_API_KEY": "fixture-api-secret"},
        )
        real("probe retry", ["--run", "true"], bootstrap_stub={"probe_failures": 1})
        real("probe exhausted", ["--run", "true"], bootstrap_stub={"probe_failures": 5}, exit=1)
        for index, name in enumerate(["env", "github", "repo", "dotfiles", "install-agents", "install-avr", "run"]):
            real("fail " + name, ALL_FLAGS, bootstrap_stub={"fail_step": index}, exit=1)
        for name, opts in [
            ("gh missing", {"missing": ["gh"]}),
            ("gh empty", {"token": ""}),
            ("gh failure", {"gh_exit": 7}),
        ]:
            real(name, ["--setup-github"], bootstrap_stub=opts, responses={}, exit=1)
        real("github before authentication", ["--setup-github"], env=fixture.CREDS.copy(), responses={}, exit=4)
        for status in [401, 403, 404, 422, 429, 503]:
            real(
                "HTTP " + str(status),
                ["--run", "true"],
                responses={vm.SHOW: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
        real(
            "pending endpoint",
            ["--run", "true"],
            responses={vm.SHOW: (200, {"data": vm.ROW | {"endpoints": {}}})},
            exit=1,
        )
        real(
            "no host key",
            ["--run", "true"],
            responses={vm.SHOW: (200, {"data": vm.ROW | {"endpoints": {"ssh": vm.ENDPOINTS["ssh"]}}})},
        )
        real(
            "bad host key",
            ["--run", "true"],
            responses={
                vm.SHOW: (200, {"data": vm.ROW | {"endpoints": {"ssh": ssh_config.EP | {"host_key": "key\nbad"}}}})
            },
            exit=1,
            error_terms=["malformed SSH host key"],
        )
        for name in ["all print", "forward both", "all run", "noninteractive missing credentials", "fail repo"]:
            item = deepcopy(next(c for c in cases if c["name"] == "vm bootstrap " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    differences = watch.compare(scenario, python, go)
    if python.get("bootstrap") != go.get("bootstrap"):
        differences.append("bootstrap helper calls/stdin/pins differ")
    if "--print" in scenario["args"] and python["stdout"] != go["stdout"]:
        differences.append("printed script content differs")
    for label, result in [("Python", python), ("Go", go)]:
        rows = result.get("bootstrap", [])
        if "--print" in scenario["args"] and rows:
            differences.append(label + " print called a helper")
        ssh = [r for r in rows if r["tool"] == "ssh"]
        for row in ssh:
            pin = row["pin"]
            if pin and (
                not pin["removed"]
                or pin["mode"] != 0o600
                or pin["text"] != "[192.0.2.1]:30022 " + ssh_config.KEY + "\n"
            ):
                differences.append(label + " pin content/mode/cleanup mismatch")
            if any(
                "fixture-" + kind + "-secret" in arg
                for arg in row["args"]
                for kind in ["env", "override", "github", "api", "oauth", "openai"]
            ):
                differences.append(label + " secret in argv")
        transcript = result["stdout"] + result["stderr"]
        if "fixture probe" in transcript or "fixture-helper-diagnostic" in transcript:
            differences.append(label + " leaked captured helper output")
        if any(
            "fixture-" + kind + "-secret" in transcript
            for kind in ["env", "override", "github", "api", "oauth", "openai"]
        ):
            differences.append(label + " leaked secret in terminal")
        options = scenario.get("bootstrap_stub", {})
        if (
            "fail_step" in options
            and len([r for r in ssh if r["args"][-3:-1] == ["bash", "-lc"]]) != options["fail_step"] + 1
        ):
            differences.append(label + " did not stop at first failed step")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/vm-bootstrap-parity.json"
        )
    )
