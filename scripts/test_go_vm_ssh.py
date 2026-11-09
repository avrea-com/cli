"""Native terminal checks for VM SSH interrupt ownership and temporary pin cleanup."""

from pathlib import Path
from scripts import native_vm_fixture as vm
from typing import Any
from typing import cast
import errno
import http.server
import os
import pytest
import select
import signal
import subprocess
import sys
import threading
import time

pty = pytest.importorskip("pty")


def terminal_ssh(executable, directory, host):
    assert pty is not None
    stub = directory / "bin"
    stub.mkdir()
    script = stub / "ssh"
    script.write_text(
        "#!" + sys.executable + "\n"
        "import signal,sys\n"
        "signal.signal(signal.SIGINT, lambda *_: print('CHILD INTERRUPT',flush=True))\n"
        "for arg in sys.argv:\n"
        " if arg.startswith('UserKnownHostsFile='): print('PIN '+arg.split('=',1)[1],flush=True)\n"
        "print('CHILD READY',flush=True)\n"
        "assert sys.stdin.readline().strip() == 'finish'\n"
        "print('CHILD FINISHED',flush=True)\n"
        "sys.exit(42)\n"
    )
    script.chmod(0o700)
    env = {
        key: value
        for key, value in os.environ.items()
        if not key.startswith("AVR_") and key.lower() not in {"http_proxy", "https_proxy", "all_proxy"}
    }
    env.update(
        HOME=str(directory),
        USERPROFILE=str(directory),
        AVR_CONFIG_DIR=str(directory / "config"),
        AVR_HOST=host,
        AVR_TOKEN=vm.TOKEN,
        AVR_ORG=vm.ORG,
        NO_COLOR="1",
        TERM="xterm-256color",
        PATH=str(stub) + os.pathsep + env["PATH"],
    )
    master, slave = pty.openpty()
    process = subprocess.Popen(
        [*executable, "vm", "ssh", vm.VM],
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

    try:
        until(b"CHILD READY")
        os.killpg(process.pid, signal.SIGINT)
        until(b"CHILD INTERRUPT")
        assert process.poll() is None
        os.write(master, b"finish\n")
        until(b"CHILD FINISHED")
        process.wait(timeout=10)
        assert process.returncode == 42, bytes(chunks)
        assert b"Interrupted." not in chunks and b"Aborted!" not in chunks
        pin = next(line[4:] for line in chunks.decode().splitlines() if line.startswith("PIN "))
        assert not Path(pin).exists(), "temporary host-key pin survived SSH exit"
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)
        os.close(master)
        os.close(slave)


def test_vm_ssh_terminal_interrupt_and_pin_cleanup(tmp_path):
    binary = os.environ.get("AVR_GO_BINARY")
    if not binary:
        pytest.skip("Set AVR_GO_BINARY to the native avr executable")
    assert binary is not None
    binary = str(Path(binary).resolve())
    server = cast(Any, http.server.ThreadingHTTPServer(("127.0.0.1", 0), vm.Fixture))
    server.requests = []
    server.responses = {vm.SHOW: (200, {"data": vm.ROW | {"state": "RUNNING", "endpoints": {"ssh": vm.EP}}})}
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    try:
        host = f"http://127.0.0.1:{server.server_port}"
        directory = tmp_path / "native"
        directory.mkdir()
        terminal_ssh([binary], directory, host)
        assert [r["path"] for r in server.requests] == [vm.SHOW[1]]
    finally:
        server.shutdown()
        server.server_close()
        worker.join()
