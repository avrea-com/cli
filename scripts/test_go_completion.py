"""Execute generated shell setup in isolated shells; never modify user profiles."""

from pathlib import Path
import errno
import os
import pytest
import select
import shlex
import shutil
import signal
import subprocess
import time

pty = pytest.importorskip("pty")


@pytest.fixture
def package(tmp_path):
    value = os.environ.get("AVR_GO_BINARY")
    if not value:
        pytest.skip("Set AVR_GO_BINARY to the public or admin executable")
    assert value is not None
    binary = Path(value).resolve()
    program = "avr-admin" if binary.name.startswith("avr-admin") else "avr"
    bindir = tmp_path / "bin"
    bindir.mkdir()
    (bindir / program).symlink_to(binary)
    env = {
        key: value
        for key, value in os.environ.items()
        if not key.startswith(("AVR_", "_AVR_")) and key.lower() not in {"http_proxy", "https_proxy", "all_proxy"}
    }
    env.update(
        HOME=str(tmp_path),
        ZDOTDIR=str(tmp_path),
        XDG_CONFIG_HOME=str(tmp_path / "config"),
        AVR_CONFIG_DIR=str(tmp_path / "config"),
        AVR_HOST="http://127.0.0.1:1",
        TERM="xterm-256color",
        NO_COLOR="1",
        PATH=str(bindir) + os.pathsep + env["PATH"],
    )
    (tmp_path / "target directory").mkdir()
    (tmp_path / "target-file").write_text("fixture")
    return program, env


def source(tmp_path, package, shell):
    program, env = package
    variable = "_" + program.upper().replace("-", "_") + "_COMPLETE"
    result = subprocess.run(
        [program],
        env=env | {variable: shell + "_source"},
        cwd=tmp_path,
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 0 and not result.stderr, result
    path = tmp_path / ("setup." + shell)
    path.write_text(result.stdout)
    return path


CASES = [
    (["repo", "li"], ["repo", "list"]),
    (["run", "list", "--stat"], ["run", "list", "--status"]),
    (["run", "list", "--status", "suc"], ["run", "list", "--status", "success"]),
    (["repo", "clone", "org/repo", "target"], ["repo", "clone", "org/repo", "target directory/"]),
]


def test_bash_generated_setup_completes_words_and_paths(tmp_path, package):
    shell = shutil.which("bash")
    if shell is None:
        pytest.skip("Bash is not installed")
    assert shell is not None
    program, env = package
    setup = source(tmp_path, package, "bash")
    for args, expected in CASES:
        script = (
            "source " + shlex.quote(str(setup)) + "\n"
            "registration=$(complete -p " + shlex.quote(program) + ")\n"
            'set -- $registration\nwhile [[ "$1" != -F ]]; do shift; done\nshift\nfn="$1"\n'
            "COMP_WORDS=(" + shlex.join([program, *args]) + ")\n"
            "COMP_CWORD=" + str(len(args)) + "\n"
            '"$fn" ' + shlex.quote(program) + "\n"
            'printf "%s\\0" "${COMPREPLY[@]}"\n'
        )
        result = subprocess.run(
            [shell, "--noprofile", "--norc", "-c", script], env=env, cwd=tmp_path, capture_output=True, timeout=10
        )
        assert result.returncode == 0 and not result.stderr, result
        assert result.stdout.decode().split("\0")[:-1] == [expected[-1]]


@pytest.mark.parametrize("autoload", [False, True])
def test_zsh_generated_setup_handles_first_tab(tmp_path, package, autoload):
    shell = shutil.which("zsh")
    if shell is None:
        pytest.skip("Zsh is not installed")
    assert shell is not None
    assert pty is not None
    program, env = package
    setup = source(tmp_path, package, "zsh")
    capture = tmp_path / "completed-line"
    completion_dir = tmp_path / "completions"
    completion_dir.mkdir()
    (completion_dir / ("_" + program)).write_text(setup.read_text())
    boot = tmp_path / "boot.zsh"
    boot.write_text(
        ("fpath=(" + shlex.quote(str(completion_dir)) + " $fpath)\n" if autoload else "")
        + "autoload -Uz compinit\ncompinit -D -i\n"
        + ("" if autoload else "source " + shlex.quote(str(setup)) + "\n")
        + 'capture_completion() { print -r -- "$BUFFER" > '
        + shlex.quote(str(capture))
        + "; BUFFER=''; zle reset-prompt; }\n"
        + "zle -N capture_completion\nbindkey '^X' capture_completion\n"
        + "PROMPT='COMPLETE> '\nprint -r -- SHELL_READY\n"
    )
    master, slave = pty.openpty()
    process = subprocess.Popen(
        [shell, "-f", "-i"], env=env, cwd=tmp_path, stdin=slave, stdout=slave, stderr=slave, start_new_session=True
    )
    chunks = bytearray()

    def until(predicate):
        deadline = time.monotonic() + 10
        while not predicate():
            assert time.monotonic() < deadline and process.poll() is None, bytes(chunks)
            if select.select([master], [], [], 0.05)[0]:
                try:
                    data = os.read(master, 65536)
                except OSError as exc:
                    if exc.errno != errno.EIO:
                        raise
                    data = b""
                assert data, bytes(chunks)
                chunks.extend(data)

    try:
        os.write(master, ("source " + shlex.quote(str(boot)) + "\n").encode())
        until(lambda: b"SHELL_READY" in chunks)
        for args, expected in CASES:
            capture.unlink(missing_ok=True)
            os.write(master, (shlex.join([program, *args]) + "\t\x18").encode())
            until(capture.exists)
            assert shlex.split(capture.read_text()) == [program, *expected], bytes(chunks)
        os.write(master, b"exit\n")
        until(lambda: process.poll() is not None)
        assert process.returncode == 0
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)
        os.close(master)
        os.close(slave)


def test_fish_generated_setup_completes_words_and_paths(tmp_path, package):
    shell = shutil.which("fish")
    if shell is None:
        pytest.skip("Fish is not installed; required by the Linux CI shell gate")
    assert shell is not None
    program, env = package
    setup = source(tmp_path, package, "fish")
    for args, expected in CASES:
        command = "source " + shlex.quote(str(setup)) + "\ncomplete -C " + shlex.quote(shlex.join([program, *args]))
        result = subprocess.run(
            [shell, "--no-config", "-c", command], env=env, cwd=tmp_path, capture_output=True, text=True, timeout=10
        )
        assert result.returncode == 0 and not result.stderr, result
        values = [line.split("\t", 1)[0] for line in result.stdout.splitlines()]
        assert values == [expected[-1]]
