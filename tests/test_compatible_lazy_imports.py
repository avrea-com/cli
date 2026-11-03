"""Check import timing in fresh interpreters, independent of test collection."""

from pathlib import Path
import json
import subprocess
import sys


def test_stream_transport_imports_on_first_use() -> None:
    script = """
import json
import sys
import avrea_cli.metrics_stream as stream
before = 'websockets.sync.client' in sys.modules
stream._connection_without_redirects()
after = 'websockets.sync.client' in sys.modules
print(json.dumps([before, after]))
"""
    result = subprocess.run(
        [sys.executable, "-c", script],
        cwd=Path(__file__).resolve().parents[1],
        capture_output=True,
        text=True,
        check=True,
    )
    assert json.loads(result.stdout) == [sys.version_info < (3, 15), True]


def test_command_help_keeps_transport_deferred_on_python315() -> None:
    script = """
import json
import sys
from avrea_cli.main import cli
from click.testing import CliRunner
for args in (['--help'], ['vm', '--help'], ['job', 'metrics', '--help']):
    result = CliRunner().invoke(cli, args)
    assert result.exit_code == 0, result.output
print(json.dumps('websockets.sync.client' in sys.modules))
"""
    result = subprocess.run(
        [sys.executable, "-c", script],
        cwd=Path(__file__).resolve().parents[1],
        capture_output=True,
        text=True,
        check=True,
    )
    assert json.loads(result.stdout) is (sys.version_info < (3, 15))
