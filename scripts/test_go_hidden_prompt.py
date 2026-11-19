"""Real-terminal checks for hidden-prompt echo suppression and interruption."""

from pathlib import Path
import errno
import os
import pytest
import select
import signal
import subprocess
import time

pty = pytest.importorskip("pty")
termios = pytest.importorskip("termios")

SECRET = "glrt-fixture-hidden-secret"


def terminal_run(executable, directory, interrupt=False):
    assert pty is not None and termios is not None
    master, slave = pty.openpty()
    before = termios.tcgetattr(slave)
    env = {key: value for key, value in os.environ.items() if not key.startswith("AVR_")}
    env.update(
        AVR_CONFIG_DIR=str(directory / "config"),
        AVR_HOST="http://127.0.0.1:1",
        AVR_TOKEN="avr_fixture_private",
        NO_COLOR="1",
        TERM="xterm-256color",
    )
    process = subprocess.Popen(
        [
            *executable,
            "org",
            "install",
            "runner",
            "set",
            "linux-x64",
            "--installation-id",
            "gli-fixture",
            "--org",
            "org-0196f0c0a1b27c3d8e9f0a1b2c3d4e5f",
        ],
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
        until(b"GitLab runner authentication token: ")
        hidden_ready()
        if interrupt:
            process.send_signal(signal.SIGINT)
        else:
            os.write(master, (SECRET + "\n").encode())
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
        assert SECRET not in result
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
        pytest.skip("Set AVR_GO_BINARY to the native avr executable")
    assert path is not None
    return Path(path).resolve()


def test_hidden_prompt(binary, tmp_path):
    code, _ = terminal_run([str(binary)], tmp_path)
    # The prompt read the secret; the API at 127.0.0.1:1 is unreachable.
    assert code == 1


def test_hidden_prompt_interrupt_restores_echo(binary, tmp_path):
    code, output = terminal_run([str(binary)], tmp_path, interrupt=True)
    assert code == 130
    assert "Interrupted." in output
