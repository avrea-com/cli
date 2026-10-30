"""Paired bootstrap token prompts, local helper invocation, and echo restoration."""

from pathlib import Path
from scripts import bootstrap_fixture as fixture
from scripts import compare_auth as harness
import errno
import io
import os
import pytest
import select
import signal
import subprocess
import sys
import tarfile
import time

pty = pytest.importorskip("pty")
termios = pytest.importorskip("termios")


def terminal_run(executable, oracle, home, answer, token, options, interrupt=False):
    assert pty is not None and termios is not None
    stub_path = fixture.make_stubs(home, options)
    env = {key: value for key, value in os.environ.items() if not key.startswith("AVR_") and key not in fixture.CREDS}
    env.update(
        HOME=str(home),
        USERPROFILE=str(home),
        AVR_CONFIG_DIR=str(home / "config"),
        AVR_HOST="http://127.0.0.1:1",
        PYTHONPATH=str(oracle),
        NO_COLOR="1",
        TERM="xterm-256color",
        PATH=stub_path,
    )
    master, slave = pty.openpty()
    before = termios.tcgetattr(slave)
    process = subprocess.Popen(
        [
            *executable,
            "vm",
            "bootstrap",
            "cvm-019a0000000070008000000000000001",
            "--install",
            "claude",
            "--forward-agent-creds",
        ],
        stdin=slave,
        stdout=slave,
        stderr=slave,
        env=env,
        cwd=home,
        start_new_session=True,
    )
    chunks = bytearray()

    def until(marker):
        deadline = time.monotonic() + 10
        while marker not in chunks:
            assert time.monotonic() < deadline, bytes(chunks)
            if select.select([master], [], [], 0.1)[0]:
                try:
                    data = os.read(master, 65536)
                except OSError as exc:
                    if exc.errno != errno.EIO:
                        raise
                    data = b""
                assert data, bytes(chunks)
                chunks.extend(data)

    try:
        until(b"[y/N]: ")
        if answer == "invalid":
            os.write(master, b"maybe\n")
            until(b"Error: invalid input")
            os.write(master, b"n\n")
        else:
            os.write(master, (answer + "\n").encode())
        until(b"Paste the token (sk-ant-...), or leave blank to skip: ")
        deadline = time.monotonic() + 5
        while termios.tcgetattr(slave)[3] & termios.ECHO:
            assert time.monotonic() < deadline, "token echo remained enabled"
            time.sleep(0.01)
        if interrupt:
            process.send_signal(signal.SIGINT)
        else:
            os.write(master, (token + "\n").encode())
        process.wait(timeout=10)
        while select.select([master], [], [], 0.05)[0]:
            try:
                data = os.read(master, 65536)
            except OSError as exc:
                if exc.errno != errno.EIO:
                    raise
                break
            if not data:
                break
            chunks.extend(data)
        assert termios.tcgetattr(slave)[3] & termios.ECHO == before[3] & termios.ECHO
        transcript = chunks.decode().replace("\r\n", "\n")
        if token:
            assert token not in transcript
        records = fixture.read_records(home)
        assert all(row["tool"] == "claude" for row in records)
        return process.returncode, transcript, records
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)
        os.close(master)
        os.close(slave)


@pytest.fixture
def binary():
    value = os.environ.get("AVR_GO_BINARY")
    if not value:
        pytest.skip("Set AVR_GO_BINARY to the public or admin executable")
    assert value is not None
    return str(Path(value).resolve())


@pytest.mark.parametrize(
    ("answer", "token", "options"),
    [
        ("", "", {}),
        ("n", "sk-ant-fixture-hidden-secret", {}),
        ("y", "sk-ant-fixture-hidden-secret", {}),
        ("yes", "sk-ant-fixture-hidden-secret", {"claude_exit": 7}),
        ("y", "", {"missing": ["claude"]}),
        ("no", "unexpected-fixture-hidden-secret", {}),
        ("invalid", "sk-ant-fixture-hidden-secret", {}),
    ],
)
def test_bootstrap_prompt_matches_python(binary, tmp_path, answer, token, options):
    oracle = tmp_path / "oracle"
    oracle.mkdir()
    archive = subprocess.check_output(
        ["git", "archive", harness.BASELINE, "avrea_cli"], cwd=Path(__file__).resolve().parents[1]
    )
    with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
        tar.extractall(oracle, filter="data")
    results = []
    for name, executable in [("python", [sys.executable, "-m", "avrea_cli.main"]), ("native", [binary])]:
        home = tmp_path / name
        home.mkdir()
        results.append(terminal_run(executable, oracle, home, answer, token, options))
    assert results[0][0] == results[1][0] == 4
    assert results[0] == results[1]
    expected_calls = 1 if answer in {"y", "yes"} and not options.get("missing") else 0
    assert len(results[0][2]) == expected_calls


def test_bootstrap_prompt_interrupt_restores_echo(binary, tmp_path):
    code, transcript, records = terminal_run([binary], tmp_path, tmp_path, "n", "", {}, interrupt=True)
    assert code == 130 and "Interrupted." in transcript
    assert not records
