"""Paired local tunnel lifecycle checks; no real SSH or desktop clients run."""

from pathlib import Path
from scripts import compare_auth as harness
from scripts import compare_vm_inspection as vm
from scripts import compare_vm_ssh_config as ssh_config
from typing import Any
from typing import cast
import errno
import http.server
import io
import json
import os
import pytest
import select
import signal
import socket
import subprocess
import sys
import tarfile
import threading
import time

pty = pytest.importorskip("pty")


@pytest.fixture
def tunnel_environment(tmp_path):
    binary = os.environ.get("AVR_GO_BINARY")
    if not binary:
        pytest.skip("Set AVR_GO_BINARY to the public or admin executable")
    assert binary is not None
    root = Path(__file__).resolve().parents[1]
    oracle = tmp_path / "oracle"
    oracle.mkdir()
    archive = subprocess.check_output(["git", "archive", harness.BASELINE, "avrea_cli"], cwd=root)
    with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
        tar.extractall(oracle, filter="data")
    server = cast(Any, http.server.ThreadingHTTPServer(("127.0.0.1", 0), harness.Fixture))
    server.requests = []
    server.responses = {}
    server.encoding = None
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    try:
        yield str(Path(binary).resolve()), oracle, server, f"http://127.0.0.1:{server.server_port}"
    finally:
        server.shutdown()
        server.server_close()
        worker.join()


def unused_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def run_local_tunnel(executable, oracle, directory, host, command, ending, launch, missing_client=False):
    assert pty is not None
    stub = directory / "bin"
    stub.mkdir()
    ssh = stub / "ssh"
    ssh.write_text(
        "#!" + sys.executable + "\n"
        "import json,os,signal,socket,sys,threading\nfrom pathlib import Path\n"
        "signal.signal(signal.SIGTERM,lambda *_: sys.exit(0))\n"
        "Path('ssh-child.pid').write_text(str(os.getpid()))\n"
        "Path('ssh-args.json').write_text(json.dumps(sys.argv[1:]))\n"
        "for arg in sys.argv:\n"
        " if arg.startswith('UserKnownHostsFile='):\n"
        "  pin=Path(arg.split('=',1)[1]); Path('pin-path').write_text(str(pin))\n"
        "  Path('pin-copy').write_text(pin.read_text())\n"
        "  assert pin.stat().st_mode & 0o777 == 0o600\n"
        "def accept(sock):\n"
        " while True:\n"
        "  conn,_=sock.accept(); conn.close()\n"
        "sockets=[]\n"
        "for i,arg in enumerate(sys.argv):\n"
        " if arg=='-L':\n"
        "  bind,local,guest_host,guest=sys.argv[i+1].split(':')\n"
        "  assert bind=='127.0.0.1' and guest_host=='localhost'\n"
        "  sock=socket.socket();sock.bind((bind,int(local)));sock.listen();sockets.append(sock)\n"
        "  threading.Thread(target=accept,args=(sock,),daemon=True).start()\n"
        "print('LOCAL FORWARDS READY',flush=True)\n"
        "if os.environ.get('FIXTURE_LAUNCH')=='1':\n"
        " while True: signal.pause()\n"
        "command=sys.stdin.readline().strip()\n"
        "sys.exit(25 if command=='drop' else 0)\n"
    )
    ssh.chmod(0o700)
    for tool in [] if missing_client else ["open", "mstsc", "xfreerdp"]:
        path = stub / tool
        path.write_text(
            "#!" + sys.executable + "\nimport json,sys\nfrom pathlib import Path\n"
            "Path('client-args.json').write_text(json.dumps(sys.argv))\n"
            "print('CLIENT STARTED',flush=True)\n"
            + ("" if sys.platform == "darwin" else "assert sys.stdin.readline().strip()=='finish'\n")
        )
        path.chmod(0o700)
    ports = [unused_port()]
    if command == "port-forward":
        ports.append(unused_port())
        while ports[1] == ports[0]:
            ports[1] = unused_port()
        flags = ["--port", f"{ports[0]}:8080", "--port", f"{ports[1]}:5432"]
    else:
        flags = ["--local-port", str(ports[0])]
    if launch:
        flags.append("--launch")
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
        AVR_TOKEN=harness.TOKEN,
        AVR_ORG=vm.ORG,
        PYTHONPATH=str(oracle),
        FIXTURE_LAUNCH="1" if launch else "0",
        NO_COLOR="1",
        TERM="xterm-256color",
        PATH=str(stub) if missing_client else str(stub) + os.pathsep + env["PATH"],
    )
    master, slave = pty.openpty()
    process = subprocess.Popen(
        [*executable, "vm", command, vm.VM, *flags],
        stdin=slave,
        stdout=slave,
        stderr=slave,
        env=env,
        cwd=directory,
        start_new_session=True,
    )
    chunks = bytearray()

    def until(marker):
        deadline = time.monotonic() + 12
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
        until(b"LOCAL FORWARDS READY")
        assumed_client = command == "rdp" or sys.platform == "darwin"
        if launch:
            if missing_client:
                until(b"Could not launch")
                until(b"Connect manually.")
            else:
                until(b"CLIENT STARTED" if assumed_client else b"No auto-launch client is assumed")
            if assumed_client and not missing_client:
                argv = json.loads((directory / "client-args.json").read_text())
                if sys.platform == "darwin":
                    assert Path(argv[0]).name == "open"
                    assert argv[1].startswith(("rdp://", "vnc://"))
                else:
                    assert Path(argv[0]).name == "xfreerdp"
                    assert "/cert:ignore" in argv and "/gfx:rfx" in argv
        else:
            until(b"Tunnel is up")
        for port in ports:
            with socket.create_connection(("127.0.0.1", port), timeout=1):
                pass
        if ending == "interrupt":
            process.send_signal(signal.SIGINT)
            until(b"Closing tunnel.")
            expected = 130
        elif launch:
            os.write(master, b"finish\n")
            expected = 0
        else:
            os.write(master, (ending + "\n").encode())
            expected = 1 if ending == "drop" else 0
            if ending == "drop":
                until(b"SSH tunnel exited unexpectedly (status 25)")
        process.wait(timeout=10)
        assert process.returncode == expected, bytes(chunks)
        pin = Path((directory / "pin-path").read_text())
        assert not pin.exists()
        assert (directory / "pin-copy").read_text() == "[192.0.2.1]:30022 " + ssh_config.KEY + "\n"
        child_pid = int((directory / "ssh-child.pid").read_text())
        with pytest.raises(ProcessLookupError):
            os.kill(child_pid, 0)
        for port in ports:
            with socket.socket() as sock:
                sock.settimeout(0.2)
                assert sock.connect_ex(("127.0.0.1", port)) != 0, "forward survived parent exit"
        return process.returncode
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)
        os.close(master)
        os.close(slave)


@pytest.mark.parametrize("command", ["port-forward", "rdp", "vnc"])
@pytest.mark.parametrize("ending", ["interrupt", "drop", "done"])
def test_local_tunnel_lifetime_matches_python(tunnel_environment, tmp_path, command, ending):
    binary, oracle, server, host = tunnel_environment
    row = vm.ROW | {
        "state": "RUNNING",
        "enable_remote_desktop": True,
        "os_type": "macos" if command == "vnc" else "linux",
        "endpoints": {"ssh": ssh_config.EP},
    }
    server.responses = {vm.SHOW: (200, {"data": row})}
    codes = []
    for name, executable in [("python", [sys.executable, "-m", "avrea_cli.main"]), ("native", [binary])]:
        directory = tmp_path / name
        directory.mkdir()
        codes.append(run_local_tunnel(executable, oracle, directory, host, command, ending, False))
    assert codes[0] == codes[1]
    assert [r["path"] for r in server.requests] == [vm.SHOW[1], vm.SHOW[1]]


@pytest.mark.parametrize("command", ["rdp", "vnc"])
def test_desktop_launch_matches_python(tunnel_environment, tmp_path, command):
    binary, oracle, server, host = tunnel_environment
    row = vm.ROW | {
        "state": "RUNNING",
        "enable_remote_desktop": True,
        "os_type": "macos" if command == "vnc" else "linux",
        "endpoints": {"ssh": ssh_config.EP},
    }
    server.responses = {vm.SHOW: (200, {"data": row})}
    ending = "done" if sys.platform != "darwin" and command == "rdp" else "interrupt"
    codes = []
    for name, executable in [("python", [sys.executable, "-m", "avrea_cli.main"]), ("native", [binary])]:
        directory = tmp_path / name
        directory.mkdir()
        codes.append(run_local_tunnel(executable, oracle, directory, host, command, ending, True))
    assert codes[0] == codes[1]


def test_missing_desktop_client_keeps_tunnel_usable(tunnel_environment, tmp_path):
    binary, oracle, server, host = tunnel_environment
    row = vm.ROW | {"state": "RUNNING", "enable_remote_desktop": True, "endpoints": {"ssh": ssh_config.EP}}
    server.responses = {vm.SHOW: (200, {"data": row})}
    for name, executable in [("python", [sys.executable, "-m", "avrea_cli.main"]), ("native", [binary])]:
        directory = tmp_path / name
        directory.mkdir()
        assert (
            run_local_tunnel(executable, oracle, directory, host, "rdp", "interrupt", True, missing_client=True) == 130
        )
