"""Real-terminal checks for card-input echo suppression and interruption."""

from pathlib import Path
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

NUMBER = "4242424242424242"
CVC = "987"


def terminal_run(executable, root, directory, interrupt=False):
    assert pty is not None and termios is not None
    master, slave = pty.openpty()
    before = termios.tcgetattr(slave)
    env = {key: value for key, value in os.environ.items() if not key.startswith("AVR_")}
    env.update(
        AVR_CONFIG_DIR=str(directory / "config"),
        AVR_HOST="http://127.0.0.1:1",
        PYTHONPATH=str(root),
        NO_COLOR="1",
        TERM="xterm-256color",
    )
    process = subprocess.Popen(
        [*executable, "billing", "payment-methods", "add"],
        stdin=slave,
        stdout=slave,
        stderr=slave,
        env=env,
        cwd=directory,
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

    def hidden_ready():
        deadline = time.monotonic() + 5
        while termios.tcgetattr(slave)[3] & termios.ECHO:
            assert time.monotonic() < deadline, "echo remained enabled"
            time.sleep(0.01)

    try:
        until(b"Card number: ")
        hidden_ready()
        if interrupt:
            process.send_signal(signal.SIGINT)
        else:
            os.write(master, (NUMBER + "\n").encode())
            until(b"Expiration month (1-12): ")
            os.write(master, b"3\n")
            until(b"Expiration year (e.g. 2027): ")
            os.write(master, b"2028\n")
            until(b"CVC: ")
            hidden_ready()
            os.write(master, (CVC + "\n").encode())
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
        result = chunks.decode().replace("\r\n", "\n")
        assert NUMBER not in result and CVC not in result
        return process.returncode, result
    finally:
        if process.poll() is None:
            process.kill()
            process.wait(timeout=5)
        os.close(master)
        os.close(slave)


@pytest.fixture
def binary():
    path = os.environ.get("AVR_GO_BINARY")
    if not path:
        pytest.skip("Set AVR_GO_BINARY to the public or admin executable")
    assert path is not None
    return Path(path).resolve()


def test_hidden_card_prompts_match_python(binary, tmp_path):
    root = Path(__file__).resolve().parents[1]
    oracle = tmp_path / "oracle"
    oracle.mkdir()
    archive = subprocess.check_output(["git", "archive", harness.BASELINE, "avrea_cli"], cwd=root)
    with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
        tar.extractall(oracle, filter="data")
    python_dir = tmp_path / "python"
    native_dir = tmp_path / "native"
    python_dir.mkdir()
    native_dir.mkdir()
    python = terminal_run([str(Path(sys.executable)), "-m", "avrea_cli.main"], oracle, python_dir)
    native = terminal_run([str(binary)], oracle, native_dir)
    assert python[0] == native[0] == 4
    assert python[1] == native[1]


def test_card_prompt_interrupt_restores_echo(binary, tmp_path):
    code, output = terminal_run([str(binary)], tmp_path, tmp_path, interrupt=True)
    assert code == 130
    assert "Interrupted." in output
