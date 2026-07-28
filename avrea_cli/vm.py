"""Long-running customer VM management CLI commands.

Customer VMs are durable, org-scoped machines reachable over SSH (all OSes)
plus RDP (Windows) or VNC (macOS Screen Sharing). Creation, start and stop
are asynchronous: the API records intent and the resource reaches RUNNING
(with connection endpoints) once the control plane places it on a node.

Disks are ephemeral today: stopping a VM (or losing its node) discards the
disk, and a restart boots fresh from the image. ``create`` therefore requires
an explicit ``--ephemeral`` acknowledgement so the no-persistence semantics
are a conscious opt-in.
"""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import CliConfig
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import ensure_prompts_allowed
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import validate_cursor
from avrea_cli.output import format_key_value
from avrea_cli.output import output_list
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Any
import click
import httpx
import json
import os
import shlex
import signal
import socket
import subprocess
import sys
import tempfile
import time

_OS_CHOICES = ["linux", "macos", "windows"]

# Hardware tiers and OS versions the API accepts (see the runner docs at
# https://docs.avrea.com/runners/). The customer picks an OS, an optional
# version and a size tier, and the control plane resolves those to a concrete
# image and cpu/memory/disk. The server validates the (os, size) and (os,
# version) pairs (sizes are OS-specific) and remains the source of truth.
_SIZE_CHOICES = ["1-vcpu", "2-vcpu", "4-vcpu", "8-vcpu", "16-vcpu", "32-vcpu"]
_OS_VERSION_CHOICES = ["ubuntu-22.04", "ubuntu-24.04", "ubuntu-26.04", "macos-26", "windows-2025"]

# Bounds / default mirror the API; kept here only to fail obviously-bad input
# locally with a clear message. The server remains the source of truth and
# re-validates everything.
_DEFAULT_TTL_SECONDS = 8 * 3600
_MIN_TTL_SECONDS = 300
_MAX_TTL_SECONDS = 7 * 24 * 3600

_TTL_UNITS = {"s": 1, "m": 60, "h": 3600, "d": 86400}

# --wait polls the freshly created VM until it is connectable, then reprints with
# a fully baked (real endpoints + password) connect command. 300s covers a cold
# provision plus first boot.
_WAIT_DEFAULT_TIMEOUT = 300
_WAIT_POLL_SECONDS = 3.0
# On a 429 we honor the server's Retry-After (seconds), capped so a large or
# bogus value can't stall the whole wait on a single response.
_WAIT_RETRY_AFTER_CAP = 30


def _parse_ttl(value: str) -> int:
    """Parse a ``--ttl`` value into seconds.

    Accepts a bare integer (seconds) or a single-unit duration: ``300s``,
    ``30m``, ``8h``, ``7d``. Raises ``click.BadParameter`` on malformed input
    or values outside the API's accepted range.
    """
    text = value.strip().lower()
    try:
        if text and text[-1] in _TTL_UNITS:
            seconds = int(text[:-1]) * _TTL_UNITS[text[-1]]
        else:
            seconds = int(text)
    except ValueError as exc:
        raise click.BadParameter(
            f"invalid duration {value!r}: use e.g. 8h, 7d, 1800s, or a number of seconds",
            param_hint="--ttl",
        ) from exc
    if not (_MIN_TTL_SECONDS <= seconds <= _MAX_TTL_SECONDS):
        raise click.BadParameter(
            f"TTL must be between {_MIN_TTL_SECONDS}s and {_MAX_TTL_SECONDS}s (got {seconds}s)",
            param_hint="--ttl",
        )
    return seconds


# OpenSSH public keys are a single line beginning with the algorithm name. We
# accept the common set plus FIDO/sk- variants; anything else (notably a PEM
# private key from a ``--ssh-key @~/.ssh/id_ed25519`` typo) is rejected before
# it can be uploaded.
_SSH_PUBKEY_PREFIXES = (
    "ssh-ed25519",
    "ssh-rsa",
    "ssh-dss",
    "ecdsa-sha2-",
    "sk-ssh-ed25519@",
    "sk-ecdsa-sha2-",
)


def _validate_ssh_public_key(content: str, source: str) -> None:
    """Reject anything that is not a single-line OpenSSH public key.

    The load-bearing check is refusing private-key material: ``--ssh-key
    @~/.ssh/id_ed25519`` (a ``.pub`` typo) would otherwise read a private key
    and upload it. ``source`` names the origin for the error message."""
    if "PRIVATE KEY" in content:
        raise click.BadParameter(
            f"{source} looks like a private key, not a public key. Point --ssh-key at the .pub file.",
            param_hint="--ssh-key",
        )
    tokens = content.split(maxsplit=1)
    first = tokens[0] if tokens else ""
    if not first.startswith(_SSH_PUBKEY_PREFIXES):
        raise click.BadParameter(
            f"{source} is not a recognized SSH public key (expected a line starting with e.g. ssh-ed25519 or ssh-rsa).",
            param_hint="--ssh-key",
        )


def _resolve_ssh_keys(keys: tuple[str, ...]) -> list[str]:
    """Resolve ``--ssh-key`` values. Each is either a literal public key or
    ``@path`` to read one from a file. Private-key material is rejected so a
    ``.pub`` typo cannot upload a private key."""
    resolved: list[str] = []
    for key in keys:
        if key.startswith("@"):
            path = Path(key[1:]).expanduser()
            try:
                content = path.read_text().strip()
            except OSError as exc:
                raise click.BadParameter(f"cannot read SSH key file {path}: {exc}", param_hint="--ssh-key") from exc
            if not content:
                raise click.BadParameter(f"SSH key file {path} is empty", param_hint="--ssh-key")
            source = str(path)
        else:
            content = key.strip()
            source = "the --ssh-key value"
        _validate_ssh_public_key(content, source)
        resolved.append(content)
    return resolved


def _load_egress_rules(value: str | None) -> list[dict[str, Any]] | None:
    """Parse ``--egress-rules``: an inline JSON array or ``@path`` to a JSON
    file. Returns ``None`` when not provided. Rule shape is validated by the
    server, which returns a 422 with detail on malformed rules."""
    if value is None:
        return None
    if value.startswith("@"):
        path = Path(value[1:]).expanduser()
        try:
            raw = path.read_text()
        except OSError as exc:
            raise click.BadParameter(
                f"cannot read egress rules file {path}: {exc}", param_hint="--egress-rules"
            ) from exc
    else:
        raw = value
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise click.BadParameter(f"--egress-rules must be valid JSON: {exc}", param_hint="--egress-rules") from exc
    if not isinstance(parsed, list):
        raise click.BadParameter("--egress-rules must be a JSON array of rule objects", param_hint="--egress-rules")
    return parsed


def _format_endpoints(endpoints: dict[str, Any] | None) -> str:
    """One-line summary of a VM's connection endpoints, or a pending note."""
    if not endpoints:
        return "(pending: VM not RUNNING yet)"
    parts: list[str] = []
    ssh = endpoints.get("ssh")
    if ssh:
        parts.append(f"ssh {ssh.get('username')}@{ssh.get('external_ip')} -p {ssh.get('external_port')}")
    remote_desktop = endpoints.get("remote_desktop")
    if remote_desktop:
        parts.append(
            f"{remote_desktop.get('protocol')} "
            f"{remote_desktop.get('external_ip')}:{remote_desktop.get('external_port')}"
        )
    return "; ".join(parts) if parts else "(none)"


def _vm_summary(vm: dict[str, Any]) -> dict[str, Any]:
    """Flatten a VM record into an ordered key-value view for human output."""
    return {
        "VM ID": vm.get("customer_vm_id"),
        "Name": vm.get("display_name"),
        "OS": vm.get("os_type"),
        "Resources": f"{vm.get('cpu_count')} vCPU / {vm.get('memory_mb')} MB / {vm.get('disk_gb')} GB disk",
        "State": vm.get("state"),
        "Desired state": vm.get("desired_state"),
        "Reason": vm.get("state_reason") or "-",
        "Remote desktop": "yes" if vm.get("enable_remote_desktop") else "no",
        "Endpoints": _format_endpoints(vm.get("endpoints")),
        "Auto-stop at": vm.get("stop_at"),
        "Created": vm.get("created_at"),
    }


# ``/gfx:rfx`` is a GNOME-Remote-Desktop-only workaround, not a general RDP flag.
# GRD on a GPU-less host emits only RemoteFX-Progressive and FreeRDP will not paint
# that stream unless this flag sets its RemoteFxCodec. Windows RDP negotiates
# AVC444/bitmap that FreeRDP paints by default, so this is emitted for Linux (GRD)
# guests only.
_RDP_RFX_FLAG = "/gfx:rfx"


def _quote_pw_posix(password: str) -> str:
    """Single-quote a password for a POSIX shell when it holds a non-alphanumeric
    character. Generated passwords are alphanumeric, so this is usually a no-op."""
    if password.isalnum():
        return password
    return "'" + password.replace("'", "'\\''") + "'"


def _freerdp_line(
    binary: str, ip_port: str, username: str, password: str | None, rfx: bool, cert: str | None = None
) -> str:
    """One FreeRDP invocation (``xfreerdp`` or ``sdl3-freerdp``).

    ``password`` is baked in only when known; ``rfx`` forces RemoteFX-Progressive
    for GNOME Remote Desktop (Linux) guests. ``cert`` sets an explicit
    ``/cert:<mode>``: the SSH-tunnel path passes ``"ignore"`` because the tunnel
    (host-key pinned) is the security boundary, and the guest's self-signed
    ``CN=avrea-rdp`` cert would otherwise trip a name-mismatch / host-changed
    warning on 127.0.0.1. Without ``cert``, ``/cert:tofu`` rides only the
    password-baked (create/rotate) line."""
    parts = [f"{binary} /v:{ip_port} /u:{username}"]
    if password:
        parts.append(f"/p:{_quote_pw_posix(password)}")
    if rfx:
        parts.append(_RDP_RFX_FLAG)
    parts.append("+clipboard")
    if cert:
        parts.append(f"/cert:{cert}")
    elif password:
        parts.append("/cert:tofu")
    return " ".join(parts)


def _rdp_connect_lines(
    ip_port: str, username: str, password: str | None, platform: str, rfx: bool, cert: str | None = None
) -> list[str]:
    """Platform-appropriate RDP client invocation(s) for the ``Connect`` row.

    ``platform`` is a ``sys.platform`` value for the machine running the CLI (not
    the guest). The first line is the primary command; later lines are further
    commands or parenthesised notes.

    The one-time password is deliberately baked into the command (``/p:`` /
    ``cmdkey``) so the line is true paste-and-go, accepting that it lands in shell
    history and ``ps`` for the session. This is a conscious convenience tradeoff:
    the password is single-use, rotates on every stop/start, and the exposure is
    local to the operator's own machine. Do not switch this to a client prompt
    without revisiting that decision.
    """
    if platform == "darwin":
        lines = [f'open "rdp://full%20address=s:{ip_port}&username=s:{username}"']
        if password:
            # The rdp:// URI has no password attribute, so Windows App prompts.
            lines.append("(no password in the URI; use the one-time password shown below)")
        # Always offer the scriptable FreeRDP client so `show` keeps the RFX guidance.
        lines.append(_freerdp_line("sdl3-freerdp", ip_port, username, password, rfx, cert))
        lines.append("(brew install freerdp; binary is sdl-freerdp on older installs)")
        return lines
    if platform == "win32":
        # cmdkey's TERMSRV target takes the host only, never the port.
        host = ip_port.split(":", 1)[0]
        if not password:
            return [f"mstsc /v:{ip_port}"]
        return [
            f"cmdkey /generic:TERMSRV/{host} /user:{username} /pass:{password}",
            f"mstsc /v:{ip_port}",
            f"cmdkey /delete:TERMSRV/{host}",
            "(run the delete afterwards; the credential persists until deleted)",
        ]
    # linux and any other POSIX host: the FreeRDP CLI client.
    return [_freerdp_line("xfreerdp", ip_port, username, password, rfx, cert)]


def _connect_block(vm: dict[str, Any], password: str | None) -> list[str]:
    """Ready-to-paste remote-desktop invocation lines for the ``Connect`` row.

    Empty when there is nothing to show: remote desktop disabled, or a non-RDP
    (macOS/VNC) guest. Bakes the password in only when it is known (create, and
    the password-rotation path of update/start).
    """
    if not vm.get("enable_remote_desktop"):
        return []
    # /gfx:rfx is a GNOME-Remote-Desktop (Linux guest) workaround; see _RDP_RFX_FLAG.
    rfx = vm.get("os_type") == "linux"
    rd = (vm.get("endpoints") or {}).get("remote_desktop")
    if rd:
        # macOS guests speak VNC; their connect story is separate, so skip them.
        if rd.get("protocol") != "rdp":
            return []
        ip_port = f"{rd.get('external_ip')}:{rd.get('external_port')}"
        username = rd.get("username") or "USER"
        pending = False
    else:
        # Endpoints are not known yet. Emit a placeholder line for RDP guests only;
        # the guest OS tells us whether it will speak RDP.
        if vm.get("os_type") == "macos":
            return []
        ip_port = "IP:PORT"
        username = "USER"
        # The IP:PORT placeholder must always carry its explanatory hint below.
        pending = True
    lines = _rdp_connect_lines(ip_port, username, password, sys.platform, rfx)
    if pending:
        lines.append(f"(IP:PORT appears in `avr vm show {vm.get('customer_vm_id')}` once the VM is RUNNING)")
    lines.append("(first connect shows a self-signed certificate warning; accept to continue)")
    return lines


def _print_vm(vm: dict[str, Any], password: str | None = None) -> None:
    summary = _vm_summary(vm)
    connect = _connect_block(vm, password)
    if connect:
        # Indent continuation lines to format_key_value's value column so the block
        # lines up under the first "Connect" line. "Connect" is narrower than the
        # widest key, so it does not widen the column.
        value_col = max(len(k) for k in summary) + 2
        connect_value = ("\n" + " " * value_col).join(connect)
        ordered: dict[str, Any] = {}
        for key, value in summary.items():
            ordered[key] = value
            if key == "Remote desktop":
                ordered["Connect"] = connect_value
        summary = ordered
    click.echo(format_key_value(summary))
    keys = vm.get("ssh_public_keys") or []
    if keys:
        click.echo(f"SSH keys     {len(keys)} configured")


def _print_password(password: str | None) -> None:
    """Surface the one-time password prominently. It is never stored, so this
    is the only chance to capture it."""
    if not password:
        return
    click.echo()
    click.secho(f"One-time password: {password}", fg="yellow", bold=True)
    click.echo("Save it now; it is not stored and cannot be retrieved later.")


def _endpoints_ready(vm: dict[str, Any]) -> bool:
    """True once the VM is connectable: RUNNING with an SSH endpoint, and a
    remote-desktop endpoint too when remote desktop was requested."""
    if (vm.get("state") or "").upper() != "RUNNING":
        return False
    eps = vm.get("endpoints") or {}
    if not eps.get("ssh"):
        return False
    if vm.get("enable_remote_desktop") and not eps.get("remote_desktop"):
        return False
    return True


def _wait_for_vm(
    client: ApiClient,
    org_id: str,
    vm_id: str,
    timeout: float,
    is_ready: Callable[[dict[str, Any]], bool],
    *,
    gone_is_ready: bool = False,
) -> tuple[dict[str, Any] | None, str]:
    """Poll a VM until ``is_ready`` holds, it enters an ERROR/FAILED state, it is
    gone (a 404, when ``gone_is_ready`` is set, e.g. delete), or the timeout
    elapses. Returns (most-recent VM record or None, disposition) where
    ``disposition`` is ``"ready"``, ``"failed"`` or ``"timeout"``. Progress goes
    to stderr so piped stdout stays clean.

    Transient poll errors (connection resets, timeouts, 5xx) are retried until
    the deadline rather than aborting the wait; only a 404 with ``gone_is_ready``
    is a definitive answer. Only the unambiguous ERROR/FAILED markers count as
    failure: STOPPED/DELETING are legitimate targets or progress for stop/delete."""
    deadline = time.monotonic() + timeout
    vm: dict[str, Any] | None = None
    last_state: str | None = None
    while True:
        try:
            resp = client.public_get(f"/orgs/{org_id}/vms/{vm_id}")
        except httpx.HTTPError as exc:
            if isinstance(exc, httpx.HTTPStatusError):
                status = exc.response.status_code
                if gone_is_ready and status == 404:
                    return None, "ready"
                if status < 500 and status not in (408, 429):
                    # A permanent 4xx (bad request, auth, not-found) must surface
                    # now, not hide behind a wait of up to the full timeout. 408
                    # (Request Timeout) and 429 (rate limit) are transient, so
                    # they fall through to the retry-until-deadline path below.
                    handle_http_error(exc, "check VM status")
            # Transport failure, a transient 408/429, or a retryable 5xx: keep
            # polling until the deadline. A 429 may carry Retry-After (seconds);
            # honor it (capped) so we back off instead of hammering. Never sleep
            # past the deadline.
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return vm, "timeout"
            sleep_for: float = _WAIT_POLL_SECONDS
            if isinstance(exc, httpx.HTTPStatusError) and exc.response.status_code == 429:
                retry_after = (exc.response.headers.get("Retry-After") or "").strip()
                if retry_after.isdigit():
                    sleep_for = min(int(retry_after), _WAIT_RETRY_AFTER_CAP)
            time.sleep(min(sleep_for, remaining))
            continue
        vm = resp["data"]
        state = (vm.get("state") or "").upper()
        if state != last_state:
            click.echo(f"  {state or 'UNKNOWN'}", err=True)
            last_state = state
        if is_ready(vm):
            return vm, "ready"
        if "ERROR" in state or "FAIL" in state:
            return vm, "failed"
        if time.monotonic() >= deadline:
            return vm, "timeout"
        time.sleep(_WAIT_POLL_SECONDS)


def _state_is(target: str) -> Callable[[dict[str, Any]], bool]:
    """Readiness predicate matching a specific VM state (e.g. STOPPED)."""

    def _pred(vm: dict[str, Any]) -> bool:
        return (vm.get("state") or "").upper() == target

    return _pred


def _emit_wait_json(ctx, vm_record: dict[str, Any] | None, password: str | None, disposition: str) -> None:
    """Emit the final VM record as one JSON document (with the one-time password
    merged in) after a --wait, then exit non-zero unless the wait succeeded."""
    final = dict(vm_record or {})
    if password is not None:
        final["password"] = password
    click.echo(json.dumps(final, indent=2, default=str))
    if disposition != "ready":
        ctx.exit(1)


def _wait_exit(
    ctx,
    vm_state: dict[str, Any] | None,
    disposition: str,
    target: str,
    wait_timeout: int,
    vm_id: str,
) -> None:
    """Print the failure/timeout tail for a human-readable --wait and exit
    non-zero. A ``ready`` disposition is a no-op."""
    if disposition == "ready":
        return
    click.echo()
    if disposition == "failed":
        state = (vm_state or {}).get("state") or "an error state"
        reason = (vm_state or {}).get("state_reason")
        click.echo(f"VM {vm_id} entered {state}" + (f": {reason}" if reason else "") + ".")
    else:
        click.echo(f"Not {target} yet after {wait_timeout}s. Re-run once ready: avr vm show {vm_id}")
    ctx.exit(1)


@click.group()
@click.pass_context
def vm(ctx):
    """Manage long-running VMs (SSH/RDP/VNC)."""
    ensure_ctx(ctx)


@vm.command("create")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--name", "display_name", required=True, help="Human-readable VM name.")
@click.option("--os", "os_type", type=click.Choice(_OS_CHOICES), required=True, help="Guest operating system.")
@click.option(
    "--os-version",
    "os_version",
    type=click.Choice(_OS_VERSION_CHOICES),
    default=None,
    help="Guest OS version (e.g. ubuntu-26.04). Defaults to the latest version for the chosen --os.",
)
@click.option(
    "--size",
    type=click.Choice(_SIZE_CHOICES),
    required=True,
    help="Hardware tier. Availability is OS-specific: linux 1-32 vCPU, macos 8/16, windows 2-16.",
)
@click.option(
    "--ssh-key",
    "ssh_keys",
    multiple=True,
    help="SSH public key, or @path to read one from a file. Repeatable.",
)
@click.option(
    "--remote-desktop/--no-remote-desktop",
    default=False,
    show_default=True,
    help=(
        "Enable a remote desktop: RDP (Windows, Linux) or VNC (macOS Screen Sharing). "
        "Availability depends on OS version; the server validates."
    ),
)
@click.option("--ttl", default=None, help="Auto-stop the VM after this long (e.g. 8h, 7d, 1800s). Default 8h, max 7d.")
@click.option(
    "--egress-rules",
    "egress_rules_raw",
    default=None,
    help="Per-VM egress firewall rules as a JSON array, or @path to a JSON file.",
)
@click.option(
    "--ephemeral",
    is_flag=True,
    default=False,
    help="Required: acknowledge that the VM's disk is ephemeral (discarded on stop).",
)
@click.option(
    "--wait",
    is_flag=True,
    default=False,
    help="Wait until the VM is RUNNING, then print a ready-to-paste connect command with the password baked in.",
)
@click.option(
    "--wait-timeout",
    default=_WAIT_DEFAULT_TIMEOUT,
    show_default=True,
    type=int,
    help="Seconds to wait when --wait is set.",
)
@click.option("--json", "as_json", is_flag=True, help="Emit the raw API response (VM plus one-time password) as JSON.")
@click.pass_context
def vm_create(
    ctx,
    org_id,
    display_name,
    os_type,
    os_version,
    size,
    ssh_keys,
    remote_desktop,
    ttl,
    egress_rules_raw,
    ephemeral,
    wait,
    wait_timeout,
    as_json,
):
    """Create a long-running VM.

    \b
    Provisioning is asynchronous: poll `avr vm show <id>` until the state is
    RUNNING and endpoints are populated, or pass --wait to block until then and
    print a ready-to-paste connect command with the password baked in. The
    response carries a one-time password for the VM's local account; save it
    now, it is never stored.
    """
    if not ephemeral:
        raise click.UsageError(
            "These VMs have no persistent storage yet: stopping a VM (or losing its node) discards "
            "the disk, and a restart boots fresh from the image. Pass --ephemeral to acknowledge."
        )
    # Windows remote desktop is not available yet (coming soon). Gate it at the
    # CLI so `--remote-desktop --os windows` reports "coming soon" rather than
    # provisioning. Delete this block (and its test) to enable it; Linux and
    # macOS remote desktop are unaffected.
    if remote_desktop and os_type == "windows":
        raise click.ClickException(
            "Windows remote desktop is coming soon. Create the VM without --remote-desktop, "
            "or use --os linux / --os macos for a remote desktop now."
        )
    ttl_seconds = _parse_ttl(ttl) if ttl is not None else _DEFAULT_TTL_SECONDS
    keys = _resolve_ssh_keys(ssh_keys)
    egress_rules = _load_egress_rules(egress_rules_raw)

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    body: dict[str, Any] = {
        "display_name": display_name,
        "ephemeral": True,
        "os_type": os_type,
        "size": size,
        "ssh_public_keys": keys,
        "enable_remote_desktop": remote_desktop,
        "ttl_seconds": ttl_seconds,
    }
    # os_version is optional; omit it so the server resolves the OS default.
    if os_version is not None:
        body["os_version"] = os_version
    if egress_rules is not None:
        body["egress_rules"] = egress_rules

    try:
        response = client.public_post(f"/orgs/{org_id}/vms", json=body)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "create VM")

    data = response["data"]
    password = data.get("password")
    vm_id = data["vm"].get("customer_vm_id")

    if wait:
        # Human output surfaces the one-time password before waiting so a timeout
        # or Ctrl-C cannot lose it; JSON carries it in the single final document.
        if not as_json:
            _print_password(password)
            click.echo()
        click.echo(f"Waiting up to {wait_timeout}s for {vm_id} to become RUNNING...", err=True)
        vm_state, disposition = _wait_for_vm(client, org_id, vm_id, wait_timeout, _endpoints_ready)
        if as_json:
            _emit_wait_json(ctx, vm_state or data["vm"], password, disposition)
            return
        click.echo()
        _print_vm(vm_state or data["vm"], password=password)
        _wait_exit(ctx, vm_state, disposition, "connectable", wait_timeout, vm_id)
        return

    if as_json:
        click.echo(json.dumps(data, indent=2, default=str))
        return

    _print_vm(data["vm"], password=password)
    _print_password(password)
    click.echo()
    click.echo(f"Provisioning started. Poll status with: avr vm show {vm_id}")


@vm.command("list")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--state", default=None, help="Filter by lifecycle state (e.g. RUNNING, STOPPED, PENDING).")
@click.option("-L", "--limit", type=click.IntRange(1, 100), default=50, show_default=True, help="Max VMs to return.")
@click.option("--cursor", default=None, help="Pagination cursor from a previous response.")
@click.option("--json", "as_json", is_flag=True, help="Emit the VM list as JSON.")
@click.pass_context
def vm_list(ctx, org_id, state, limit, cursor, as_json):
    """List the organization's VMs, newest first (deleted ones excluded)."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    cursor = validate_cursor(cursor)
    params: dict[str, Any] = {"limit": limit}
    if state:
        params["state"] = state
    if cursor:
        params["cursor"] = cursor

    try:
        response = client.public_get(f"/orgs/{org_id}/vms", params=params)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list VMs")

    data = response["data"]
    next_cursor = (response.get("pagination") or {}).get("next_cursor")

    if as_json:
        click.echo(json.dumps(data, indent=2, default=str))
        if next_cursor:
            # Cursor on stderr so stdout stays a clean JSON document for pipes.
            click.echo(f"next_cursor: {shlex.quote(next_cursor)}", err=True)
        return

    output_list(
        data,
        columns=[
            "customer_vm_id",
            "display_name",
            "os_type",
            "state",
            "desired_state",
            "cpu_count",
            "memory_mb",
            "created_at",
        ],
        column_labels=["VM ID", "Name", "OS", "State", "Desired", "vCPU", "Mem (MB)", "Created"],
    )

    if next_cursor:
        click.echo(f"\nMore results available. Next page: --cursor {shlex.quote(next_cursor)}", err=True)


@vm.command("show")
@click.argument("vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--json", "as_json", is_flag=True, help="Emit the full VM record (with egress rules) as JSON.")
@click.pass_context
def vm_show(ctx, vm_id, org_id, as_json):
    """Show a VM's details, including connection endpoints and egress rules."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(f"/orgs/{org_id}/vms/{vm_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch VM", hint="Run `avr vm list` to see your VMs.")

    data = response["data"]
    if as_json:
        click.echo(json.dumps(data, indent=2, default=str))
        return

    _print_vm(data)
    rules = data.get("egress_rules") or []
    if rules:
        click.echo()
        click.echo("Egress rules:")
        for rule in rules:
            rule["matcher"] = "*" if rule.get("is_default") else (rule.get("cidr") or rule.get("fqdn") or "?")
        output_list(
            rules,
            columns=["position", "action", "matcher", "protocol", "port_start", "port_end"],
            column_labels=["#", "Action", "Match", "Proto", "Port start", "Port end"],
        )


@vm.command("ssh", context_settings={"ignore_unknown_options": True})
@click.argument("vm_id")
@click.argument("ssh_args", nargs=-1, type=click.UNPROCESSED)
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "-i",
    "--identity",
    "identity_file",
    type=click.Path(),
    default=None,
    help="Private key file to pass to ssh as -i.",
)
@click.option("--print", "print_only", is_flag=True, help="Print the ssh command instead of running it.")
@click.pass_context
def vm_ssh(ctx, vm_id, ssh_args, org_id, identity_file, print_only):
    """Open an SSH session to a RUNNING VM, or run a command on it.

    With no extra arguments this opens an interactive session. Anything after
    `--` is run as a remote command instead, e.g.:

        avr vm ssh cvm-abc123 -- uname -a

    When the VM's endpoint publishes a host key it is pinned, so the first
    connect neither prompts nor is spoofable. If the endpoint has no host key,
    `avr` prints a warning and falls back to trust-on-first-use, so this
    spoofing protection is conditional rather than guaranteed. For
    port-forwarding use `avr vm port-forward`.
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    ssh_ep = _resolve_ssh_endpoint(client, org_id, vm_id)

    if print_only:
        # The real run pins the host key via an ephemeral known_hosts file that
        # a pasted command can't reference, so the printed form uses accept-new.
        argv = _ssh_connect_argv(ssh_ep, identity_file=identity_file, known_hosts=None)
        argv += list(ssh_args)
        click.echo(" ".join(shlex.quote(arg) for arg in argv))
        return

    known_hosts = _write_known_hosts(ssh_ep.get("host_key"), ssh_ep["external_ip"], ssh_ep["external_port"])
    if known_hosts is None:
        # Same fallback the tunnel path surfaces: without a host key we can't pin,
        # so ssh accepts the key on first use. Say so rather than doing it silently.
        click.echo(
            "Warning: this VM's endpoint has no SSH host key; the connection falls back to "
            "trust-on-first-use instead of pinning.",
            err=True,
        )
    argv = _ssh_connect_argv(ssh_ep, identity_file=identity_file, known_hosts=known_hosts)
    argv += list(ssh_args)  # remote command after the destination; empty is interactive

    # Ignore SIGINT in the parent so Ctrl-C reaches the ssh child (and the
    # remote session), not this process.
    previous_sigint = signal.signal(signal.SIGINT, signal.SIG_IGN)
    try:
        completed = subprocess.run(argv)
    except OSError as exc:  # ssh not on PATH, etc.
        raise click.ClickException(f"failed to launch ssh: {exc}") from exc
    finally:
        signal.signal(signal.SIGINT, previous_sigint)
        if known_hosts is not None:
            known_hosts.unlink(missing_ok=True)
    sys.exit(completed.returncode)


@vm.command("update")
@click.argument("vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--name", "display_name", default=None, help="New display name.")
@click.option("--ttl", default=None, help="Extend the auto-stop window from now (e.g. 8h, 7d). Max 7d.")
@click.option(
    "--ssh-key",
    "ssh_keys",
    multiple=True,
    help="Replace stored SSH public keys (literal or @path). Repeatable. Applies live on a RUNNING VM, "
    "otherwise at next start.",
)
@click.option(
    "--rotate-password",
    is_flag=True,
    default=False,
    help="Provision a fresh one-time password (returned in the response).",
)
@click.option(
    "--egress-rules",
    "egress_rules_raw",
    default=None,
    help="Replace the per-VM egress rules with this JSON array (or @path to a file).",
)
@click.option("--json", "as_json", is_flag=True, help="Emit the raw API response as JSON.")
@click.pass_context
def vm_update(ctx, vm_id, org_id, display_name, ttl, ssh_keys, rotate_password, egress_rules_raw, as_json):
    """Update a VM's name, TTL, or SSH keys, or rotate its password.

    Power state is controlled separately with avr vm start / avr vm stop.
    """
    ttl_seconds = _parse_ttl(ttl) if ttl is not None else None
    keys = _resolve_ssh_keys(ssh_keys) if ssh_keys else None
    egress_rules = _load_egress_rules(egress_rules_raw)

    body: dict[str, Any] = {"rotate_password": rotate_password}
    if display_name is not None:
        body["display_name"] = display_name
    if ttl_seconds is not None:
        body["ttl_seconds"] = ttl_seconds
    if keys is not None:
        body["ssh_public_keys"] = keys
    if egress_rules is not None:
        body["egress_rules"] = egress_rules

    if not rotate_password and len(body) == 1:
        raise click.UsageError(
            "Nothing to update. Pass --name, --ttl, --ssh-key, --rotate-password, or --egress-rules."
        )

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_patch(f"/orgs/{org_id}/vms/{vm_id}", json=body)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "update VM", hint="Run `avr vm list` to see your VMs.")

    data = (response or {}).get("data") or {}
    if as_json:
        click.echo(json.dumps(response, indent=2, default=str))
        return

    if data.get("vm"):
        _print_vm(data["vm"], password=data.get("password"))
    _print_password(data.get("password"))


@vm.command("start")
@click.argument("vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "--wait",
    is_flag=True,
    default=False,
    help="Wait until RUNNING, then print a ready-to-paste connect command with the fresh password.",
)
@click.option(
    "--wait-timeout",
    default=_WAIT_DEFAULT_TIMEOUT,
    show_default=True,
    type=int,
    help="Seconds to wait when --wait is set.",
)
@click.option("--json", "as_json", is_flag=True, help="Emit the raw API response as JSON.")
@click.pass_context
def vm_start(ctx, vm_id, org_id, wait, wait_timeout, as_json):
    """Start a stopped VM. Boots a fresh disk and returns a one-time password."""
    _set_desired_state(ctx, vm_id, org_id, "RUNNING", as_json, action="start VM", wait=wait, wait_timeout=wait_timeout)


@vm.command("stop")
@click.argument("vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--wait", is_flag=True, default=False, help="Wait until the VM reaches STOPPED before returning.")
@click.option(
    "--wait-timeout",
    default=_WAIT_DEFAULT_TIMEOUT,
    show_default=True,
    type=int,
    help="Seconds to wait when --wait is set.",
)
@click.option("--json", "as_json", is_flag=True, help="Emit the raw API response as JSON.")
@click.pass_context
def vm_stop(ctx, vm_id, org_id, wait, wait_timeout, as_json):
    """Stop a running VM. The ephemeral disk is discarded."""
    _set_desired_state(ctx, vm_id, org_id, "STOPPED", as_json, action="stop VM", wait=wait, wait_timeout=wait_timeout)


def _set_desired_state(
    ctx,
    vm_id: str,
    org_id: str | None,
    desired_state: str,
    as_json: bool,
    *,
    action: str,
    wait: bool = False,
    wait_timeout: int = _WAIT_DEFAULT_TIMEOUT,
):
    """Shared body for ``start`` / ``stop`` (both PATCH ``desired_state``)."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_patch(f"/orgs/{org_id}/vms/{vm_id}", json={"desired_state": desired_state})
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, action, hint="Run `avr vm list` to see your VMs.")

    data = (response or {}).get("data") or {}
    password = data.get("password")

    if wait:
        running = desired_state == "RUNNING"
        is_ready = _endpoints_ready if running else _state_is(desired_state)
        fail_target = "connectable" if running else desired_state
        # start returns a fresh one-time password; surface it (human mode) before
        # waiting so a timeout or Ctrl-C cannot lose it.
        if not as_json:
            _print_password(password)
            click.echo()
        click.echo(f"Waiting up to {wait_timeout}s for {vm_id} to become {desired_state}...", err=True)
        vm_state, disposition = _wait_for_vm(client, org_id, vm_id, wait_timeout, is_ready)
        if as_json:
            _emit_wait_json(ctx, vm_state or data.get("vm"), password, disposition)
            return
        click.echo()
        if running:
            # Reprint with real endpoints + the fresh password baked into the connect line.
            shown = vm_state or data.get("vm")
            if shown:
                _print_vm(shown, password=password)
        elif disposition == "ready":
            # Stopping has no connect payload; a concise confirmation is cleaner,
            # but only once actually stopped (never on a timed-out wait).
            final = (vm_state or {}).get("state") or desired_state
            click.echo(f"VM {vm_id} is now {final}.")
        _wait_exit(ctx, vm_state, disposition, fail_target, wait_timeout, vm_id)
        return

    if as_json:
        click.echo(json.dumps(response, indent=2, default=str))
        return

    if data.get("vm"):
        _print_vm(data["vm"], password=password)
    _print_password(password)


@vm.command("usage")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "--start",
    "period_start",
    type=click.DateTime(formats=["%Y-%m-%d", "%Y-%m-%dT%H:%M:%S"]),
    default=None,
    help="Inclusive period start (default: 30 days ago).",
)
@click.option(
    "--end",
    "period_end",
    type=click.DateTime(formats=["%Y-%m-%d", "%Y-%m-%dT%H:%M:%S"]),
    default=None,
    help="Exclusive period end (default: now).",
)
@click.option("--json", "as_json", is_flag=True, help="Emit the usage report as JSON.")
@click.pass_context
def vm_usage(ctx, org_id, period_start, period_end, as_json):
    """Show usage metering (runtime / vCPU / memory seconds) per VM.

    Each power-on cycle's window is clipped to the requested period and summed.
    Deleted VMs are included: usage survives deletion.
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    params: dict[str, Any] = {}
    if period_start is not None:
        params["period_start"] = period_start.isoformat()
    if period_end is not None:
        params["period_end"] = period_end.isoformat()

    try:
        response = client.public_get(f"/orgs/{org_id}/vms/usage", params=params)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch VM usage")

    data = response["data"]
    if as_json:
        click.echo(json.dumps(data, indent=2, default=str))
        return

    output_list(
        data.get("vms") or [],
        columns=[
            "customer_vm_id",
            "display_name",
            "os_type",
            "state",
            "run_count",
            "runtime_seconds",
            "vcpu_seconds",
            "memory_mb_seconds",
        ],
        column_labels=["VM ID", "Name", "OS", "State", "Runs", "Runtime (s)", "vCPU (s)", "Mem (MB-s)"],
    )
    click.echo()
    click.echo(
        format_key_value(
            {
                "Period start": data.get("period_start"),
                "Period end": data.get("period_end"),
                "Total runtime (s)": data.get("total_runtime_seconds"),
                "Total vCPU (s)": data.get("total_vcpu_seconds"),
                "Total memory (MB-s)": data.get("total_memory_mb_seconds"),
            }
        )
    )


@vm.command("delete")
@click.argument("vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--yes", "-y", is_flag=True, help="Skip the confirmation prompt.")
@click.option("--wait", is_flag=True, default=False, help="Wait until the VM is fully deleted before returning.")
@click.option(
    "--wait-timeout",
    default=_WAIT_DEFAULT_TIMEOUT,
    show_default=True,
    type=int,
    help="Seconds to wait when --wait is set.",
)
@click.option("--json", "as_json", is_flag=True, help="Emit the raw API response as JSON.")
@click.pass_context
def vm_delete(ctx, vm_id, org_id, yes, wait, wait_timeout, as_json):
    """Delete a VM. Asynchronous while live: shows DELETING until the node confirms the stop."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    if not yes:
        ensure_prompts_allowed("deleting a VM")
        click.confirm(f"Delete VM {vm_id}? This is permanent.", abort=True)

    try:
        response = client.public_delete(f"/orgs/{org_id}/vms/{vm_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "delete VM", hint="Run `avr vm list` to see your VMs.")

    state = ((response or {}).get("data") or {}).get("state", "DELETING")

    if wait:
        click.echo(f"Waiting up to {wait_timeout}s for {vm_id} to be deleted...", err=True)
        vm_state, disposition = _wait_for_vm(
            client, org_id, vm_id, wait_timeout, _state_is("DELETED"), gone_is_ready=True
        )
        gone = disposition == "ready"
        if as_json:
            # Carry the disposition (and the failure reason) so a script can tell
            # deleted from "entered FAILED" from "still deleting", not just gone/not.
            out: dict[str, Any] = {"customer_vm_id": vm_id, "deleted": gone, "disposition": disposition}
            if disposition == "failed":
                out["state"] = (vm_state or {}).get("state")
                out["state_reason"] = (vm_state or {}).get("state_reason")
            click.echo(json.dumps(out, indent=2, default=str))
        elif gone:
            click.echo(f"VM {vm_id} deleted.")
        elif disposition == "failed":
            # The VM hit ERROR/FAILED mid-teardown; surface the reason instead of
            # mislabeling it as a timeout and discarding state_reason.
            state = (vm_state or {}).get("state") or "an error state"
            reason = (vm_state or {}).get("state_reason")
            click.echo(f"VM {vm_id} entered {state}" + (f": {reason}" if reason else "") + ".")
        else:
            click.echo(f"Still deleting after {wait_timeout}s. Check with: avr vm show {vm_id}")
        if not gone:
            ctx.exit(1)
        return

    if as_json:
        click.echo(json.dumps(response, indent=2, default=str))
        return

    click.echo(f"VM {vm_id} is now {state}.")


# --- Remote desktop / port-forward over SSH -------------------------------
#
# `avr vm ssh` reaches the VM directly. `avr vm rdp` / `avr vm vnc` reach the
# guest's *desktop* by forwarding a local port to the desktop service over that
# same SSH endpoint, so the desktop never needs a public forward of its own.
# `avr vm port-forward` is the generic primitive the two build on. All three
# hold the ssh child open for the life of the session: closing it (Ctrl-C)
# tears the forward down and drops the desktop, the same contract as
# `gcloud ... start-tcp-tunnel` and `kubectl port-forward`.
#
# The endpoint record carries no guest-internal port, so we derive it from the
# protocol: RDP 3389 (Windows and Linux GNOME Remote Desktop), VNC 5900 (macOS
# Screen Sharing).
_RDP_GUEST_PORT = 3389
_VNC_GUEST_PORT = 5900

# How long to wait for the local forward to start listening after we spawn ssh.
# Generous: key auth is instant, but password auth (the one-time VM password)
# means the user is typing at ssh's own prompt while we poll.
_TUNNEL_READY_TIMEOUT = 60.0
_TUNNEL_POLL_SECONDS = 0.25


def _pick_local_port() -> int:
    """Reserve an unused loopback TCP port by binding to :0 and reading it back.

    A racy window exists between our close and ssh's bind; ssh's
    ``ExitOnForwardFailure=yes`` turns a lost race into a clean, reported
    failure rather than a silent forward-less tunnel."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def _port_is_listening(port: int) -> bool:
    """True if something accepts a TCP connect on 127.0.0.1:port right now."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.settimeout(0.5)
        return sock.connect_ex(("127.0.0.1", port)) == 0


def _known_hosts_line(host_key: str, external_ip: str, external_port: int) -> str:
    """One known_hosts entry pinning ``host_key`` to the SSH endpoint. OpenSSH
    keys a non-default port as ``[host]:port``; port 22 is bare."""
    host = external_ip if external_port == 22 else f"[{external_ip}]:{external_port}"
    return f"{host} {host_key.strip()}\n"


def _write_known_hosts(host_key: str | None, external_ip: str, external_port: int) -> Path | None:
    """Materialize a temp known_hosts pinning the endpoint's host key, or None
    when the endpoint carries no key (older VMs). The caller unlinks it."""
    if not host_key:
        return None
    fd, name = tempfile.mkstemp(prefix="avr-vm-known-hosts-")
    with os.fdopen(fd, "w") as handle:
        handle.write(_known_hosts_line(host_key, external_ip, external_port))
    return Path(name)


def _resolve_ssh_endpoint(client: ApiClient, org_id: str, vm_id: str) -> dict[str, Any]:
    """Fetch the VM and return its SSH endpoint, raising if it has none yet
    (endpoints appear once the VM is RUNNING)."""
    try:
        response = client.public_get(f"/orgs/{org_id}/vms/{vm_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch VM", hint="Run `avr vm list` to see your VMs.")

    data = response["data"]
    ssh_ep = (data.get("endpoints") or {}).get("ssh")
    if not ssh_ep:
        raise click.ClickException(
            f"VM {vm_id} has no SSH endpoint yet (state: {data.get('state')}). "
            "Endpoints appear once the VM is RUNNING; check `avr vm show`."
        )
    return ssh_ep


def _ssh_connect_argv(
    ssh_ep: dict[str, Any],
    *,
    identity_file: str | None,
    known_hosts: Path | None,
    extra_opts: tuple[str, ...] = (),
) -> list[str]:
    """The ssh argv connecting to the endpoint, up to and including the
    destination. Pins the endpoint's host key when present (first connect
    neither prompts nor is spoofable); otherwise accept-new so ssh never blocks
    on a prompt. A remote command, if any, goes after the returned argv."""
    argv = ["ssh"]
    if identity_file:
        argv += ["-i", identity_file]
    argv += ["-p", str(ssh_ep["external_port"])]
    if known_hosts is not None:
        argv += ["-o", f"UserKnownHostsFile={known_hosts}", "-o", "StrictHostKeyChecking=yes"]
    else:
        argv += ["-o", "StrictHostKeyChecking=accept-new"]
    argv += list(extra_opts)
    argv.append(f"{ssh_ep.get('username') or 'runner'}@{ssh_ep['external_ip']}")
    return argv


def run_ssh(
    ssh_ep: dict[str, Any],
    command: list[str],
    *,
    identity_file: str | None = None,
    stdin_data: str | None = None,
    timeout: float | None = None,
    capture: bool = True,
) -> subprocess.CompletedProcess[str]:
    """Run ``command`` on the VM over SSH, non-interactively, host key pinned.

    Feed secrets via ``stdin_data``, never through ``command``: argv is visible
    to ``ps`` on the VM. ``BatchMode=yes`` makes auth or host-key failures fail
    fast rather than hang on a prompt. With ``capture`` (the default) stdout and
    stderr are captured on the returned CompletedProcess; with ``capture=False``
    they stream straight to this process's stdout/stderr (for long-running steps
    whose progress the user should see live) and are ``None`` on the result."""
    known_hosts = _write_known_hosts(ssh_ep.get("host_key"), ssh_ep["external_ip"], ssh_ep["external_port"])
    try:
        argv = _ssh_connect_argv(
            ssh_ep,
            identity_file=identity_file,
            known_hosts=known_hosts,
            extra_opts=("-o", "BatchMode=yes", "-o", "ConnectTimeout=15"),
        )
        argv += command
        return subprocess.run(argv, input=stdin_data, capture_output=capture, text=True, timeout=timeout)
    finally:
        if known_hosts is not None:
            known_hosts.unlink(missing_ok=True)


def _ssh_tunnel_argv(
    ssh_ep: dict[str, Any],
    *,
    local_port: int,
    guest_port: int,
    identity_file: str | None,
    known_hosts: Path | None,
) -> list[str]:
    """The ``ssh -N -L`` argv forwarding 127.0.0.1:local_port to the guest's own
    loopback:guest_port over the VM's SSH endpoint."""
    argv = [
        "ssh",
        "-N",  # forward only, no remote command
        "-o",
        "ExitOnForwardFailure=yes",  # fail loudly if the local bind fails
        "-o",
        "ConnectTimeout=15",
        "-o",
        "ServerAliveInterval=30",  # keep an idle desktop session alive
        "-L",
        f"127.0.0.1:{local_port}:localhost:{guest_port}",
        "-p",
        str(ssh_ep["external_port"]),
    ]
    if known_hosts is not None:
        # Pin the endpoint's host key: first connect neither prompts nor is
        # spoofable. Without a key, accept-new so ssh still doesn't block on an
        # interactive yes/no.
        argv += ["-o", f"UserKnownHostsFile={known_hosts}", "-o", "StrictHostKeyChecking=yes"]
    else:
        argv += ["-o", "StrictHostKeyChecking=accept-new"]
    if identity_file:
        argv += ["-i", identity_file]
    argv.append(f"{ssh_ep.get('username') or 'runner'}@{ssh_ep['external_ip']}")
    return argv


def _terminate_tunnel(proc: subprocess.Popen[bytes]) -> None:
    """Best-effort teardown of the ssh child: SIGTERM, then SIGKILL."""
    if proc.poll() is not None:
        return
    proc.terminate()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()


def _wait_until_listening(proc: subprocess.Popen[bytes], local_port: int, timeout: float) -> bool:
    """Poll until the local forward accepts connections. False if ssh exits
    first (auth/host-key/bind failure, already on its own stderr) or we time
    out."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            return False
        if _port_is_listening(local_port):
            return True
        time.sleep(_TUNNEL_POLL_SECONDS)
    return False


def _run_tunnel(
    ssh_ep: dict[str, Any],
    *,
    local_port: int,
    guest_port: int,
    identity_file: str | None,
    on_ready: Callable[[subprocess.Popen[bytes]], None],
) -> None:
    """Open the SSH forward, wait for it to listen, hand the live ssh process to
    ``on_ready`` (which holds or launches a client), then tear it down. Ctrl-C
    closes cleanly."""
    known_hosts = _write_known_hosts(ssh_ep.get("host_key"), ssh_ep["external_ip"], ssh_ep["external_port"])
    if known_hosts is None:
        click.echo(
            "Warning: this VM's endpoint has no SSH host key; the tunnel falls back to "
            "trust-on-first-use instead of pinning.",
            err=True,
        )
    argv = _ssh_tunnel_argv(
        ssh_ep, local_port=local_port, guest_port=guest_port, identity_file=identity_file, known_hosts=known_hosts
    )
    try:
        try:
            proc = subprocess.Popen(argv)
        except OSError as exc:  # ssh not on PATH, etc.
            raise click.ClickException(f"failed to launch ssh: {exc}") from exc
        try:
            if not _wait_until_listening(proc, local_port, _TUNNEL_READY_TIMEOUT):
                raise click.ClickException(
                    "SSH tunnel did not come up. The local port may be busy (try --local-port) "
                    "or the VM may be unreachable (check `avr vm show`)."
                )
            on_ready(proc)
            # If ssh exited on its own with a failure (e.g. 255 on a dropped or
            # refused connection) rather than via Ctrl-C or a client-driven
            # teardown, surface it as a nonzero exit instead of a clean success.
            # A negative code (killed by our own SIGTERM/SIGKILL) is intentional.
            rc = proc.poll()
            if rc is not None and rc > 0:
                raise click.ClickException(f"SSH tunnel exited unexpectedly (status {rc}); the connection was lost.")
        except KeyboardInterrupt:
            click.echo("\nClosing tunnel.", err=True)
        finally:
            _terminate_tunnel(proc)
    finally:
        if known_hosts is not None:
            known_hosts.unlink(missing_ok=True)


def _rdp_launch_argv(ip_port: str, username: str, platform: str, rfx: bool) -> tuple[list[str] | None, bool]:
    """A native RDP client argv for --launch, plus whether it detaches.

    ``detaches`` is True when the launcher hands off and exits immediately
    (macOS ``open``), so the tunnel can't be tied to the client's lifetime and
    is held until Ctrl-C instead."""
    if platform == "win32":
        return ["mstsc", f"/v:{ip_port}"], False
    if platform == "darwin":
        return ["open", f"rdp://full%20address=s:{ip_port}&username=s:{username}"], True
    # linux and other POSIX: xfreerdp. /cert:ignore because the SSH tunnel is the
    # security boundary; the guest's self-signed CN=avrea-rdp cert on 127.0.0.1
    # would otherwise trip a name-mismatch / host-changed prompt and block launch.
    argv = ["xfreerdp", f"/v:{ip_port}", f"/u:{username}", "+clipboard", "/cert:ignore"]
    if rfx:
        argv.append(_RDP_RFX_FLAG)
    return argv, False


def _vnc_launch_argv(ip_port: str, platform: str) -> tuple[list[str] | None, bool]:
    """A native VNC client argv for --launch. Only macOS has an assumed client
    (Screen Sharing via ``open vnc://``); elsewhere returns (None, False)."""
    if platform == "darwin":
        return ["open", f"vnc://{ip_port}"], True
    return None, False


def _vnc_connect_lines(ip_port: str, platform: str) -> list[str]:
    """Paste-ready VNC connect line(s) for the CLI-host platform."""
    if platform == "darwin":
        return [f"open vnc://{ip_port}", "(opens macOS Screen Sharing)"]
    return [f"point a VNC client at {ip_port}"]


def _desktop_connect_lines(protocol: str, ip_port: str, username: str, os_type: str | None) -> list[str]:
    """Paste-ready client invocation line(s) for a tunnelled desktop endpoint."""
    if protocol == "vnc":
        return _vnc_connect_lines(ip_port, sys.platform)
    # /gfx:rfx is required for Linux GNOME Remote Desktop; see _RDP_RFX_FLAG.
    # /cert:ignore because the tunnel (SSH host-key pinned) is the security
    # boundary, so the guest's self-signed cert on 127.0.0.1 needn't be checked.
    return _rdp_connect_lines(ip_port, username, None, sys.platform, os_type == "linux", cert="ignore")


def _launch_client(launch_argv: list[str] | None, detaches: bool, tunnel_proc: subprocess.Popen[bytes]) -> None:
    """Spawn the native desktop client, then hold the tunnel until the client
    exits (waitable clients) or until Ctrl-C (detaching launchers / platforms
    with no assumed client)."""
    if launch_argv is None:
        click.echo("No auto-launch client is assumed on this platform; connect manually.", err=True)
        tunnel_proc.wait()
        return
    try:
        client = subprocess.Popen(launch_argv)
    except OSError as exc:
        click.echo(f"Could not launch {launch_argv[0]}: {exc}. Connect manually.", err=True)
        tunnel_proc.wait()
        return
    if detaches:
        click.echo("Launched the desktop client. Press Ctrl-C to close the tunnel when done.", err=True)
        tunnel_proc.wait()
    else:
        client.wait()


def _vm_remote_desktop(
    ctx,
    vm_id: str,
    org_id: str | None,
    *,
    want_protocol: str,
    guest_port: int,
    local_port: int | None,
    identity_file: str | None,
    launch: bool,
    print_only: bool,
) -> None:
    """Shared body for ``avr vm rdp`` / ``avr vm vnc``."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(f"/orgs/{org_id}/vms/{vm_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch VM", hint="Run `avr vm list` to see your VMs.")

    data = response["data"]
    if not data.get("enable_remote_desktop"):
        raise click.ClickException(f"VM {vm_id} has no remote desktop. Recreate it with `--remote-desktop`.")
    ssh_ep = (data.get("endpoints") or {}).get("ssh")
    if not ssh_ep:
        raise click.ClickException(
            f"VM {vm_id} has no SSH endpoint yet (state: {data.get('state')}). "
            "Endpoints appear once the VM is RUNNING; check `avr vm show`."
        )

    rd = (data.get("endpoints") or {}).get("remote_desktop")
    os_type = data.get("os_type")
    # Prefer the live endpoint's protocol; fall back to the OS default before
    # the remote-desktop forward is published.
    actual_protocol = rd.get("protocol") if rd else ("vnc" if os_type == "macos" else "rdp")
    if actual_protocol != want_protocol:
        alt = "vnc" if actual_protocol == "vnc" else "rdp"
        raise click.ClickException(
            f"VM {vm_id} speaks {actual_protocol}, not {want_protocol}. Use `avr vm {alt} {vm_id}` instead."
        )

    username = (rd.get("username") if rd else None) or ssh_ep.get("username") or "runner"
    local_port = local_port or _pick_local_port()
    ip_port = f"127.0.0.1:{local_port}"
    connect_lines = _desktop_connect_lines(want_protocol, ip_port, username, os_type)

    if print_only:
        display_argv = _ssh_tunnel_argv(
            ssh_ep, local_port=local_port, guest_port=guest_port, identity_file=identity_file, known_hosts=None
        )
        click.echo("Open the tunnel:")
        click.echo(f"  {shlex.join(display_argv)}")
        click.echo("Then connect with:")
        for line in connect_lines:
            click.echo(f"  {line}")
        click.echo(
            "(the live command pins the VM's SSH host key; this printed form uses trust-on-first-use)",
            err=True,
        )
        return

    click.echo(f"Opening SSH tunnel {ip_port} -> {vm_id}:{guest_port} ...", err=True)

    def on_ready(proc: subprocess.Popen[bytes]) -> None:
        click.echo("Connect with:", err=True)
        for line in connect_lines:
            click.echo(f"  {line}", err=True)
        click.echo("(use the one-time password saved at create / last start)", err=True)
        if launch:
            if want_protocol == "vnc":
                argv, detaches = _vnc_launch_argv(ip_port, sys.platform)
            else:
                argv, detaches = _rdp_launch_argv(ip_port, username, sys.platform, os_type == "linux")
            _launch_client(argv, detaches, proc)
        else:
            click.echo("Tunnel is up. Press Ctrl-C to close it.", err=True)
            proc.wait()

    _run_tunnel(ssh_ep, local_port=local_port, guest_port=guest_port, identity_file=identity_file, on_ready=on_ready)


@vm.command("rdp")
@click.argument("vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "--local-port",
    type=click.IntRange(1, 65535),
    default=None,
    help="Local port to bind (default: an unused port).",
)
@click.option(
    "-i", "--identity", "identity_file", type=click.Path(), default=None, help="Private key file to pass to ssh as -i."
)
@click.option(
    "--launch/--no-launch",
    default=False,
    show_default=True,
    help="Also start a local RDP client, instead of just printing the connect command.",
)
@click.option(
    "--print",
    "print_only",
    is_flag=True,
    help="Print the tunnel and client commands and exit, without opening the tunnel.",
)
@click.pass_context
def vm_rdp(ctx, vm_id, org_id, local_port, identity_file, launch, print_only):
    """Open an RDP desktop on a Windows or Linux VM over an SSH tunnel.

    Forwards a local port to the guest's RDP service (:3389) through the VM's
    SSH endpoint, so the desktop is never exposed publicly. Holds the tunnel
    open until Ctrl-C; pass --launch to also start a local RDP client.
    """
    _vm_remote_desktop(
        ctx,
        vm_id,
        org_id,
        want_protocol="rdp",
        guest_port=_RDP_GUEST_PORT,
        local_port=local_port,
        identity_file=identity_file,
        launch=launch,
        print_only=print_only,
    )


@vm.command("vnc")
@click.argument("vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "--local-port",
    type=click.IntRange(1, 65535),
    default=None,
    help="Local port to bind (default: an unused port).",
)
@click.option(
    "-i", "--identity", "identity_file", type=click.Path(), default=None, help="Private key file to pass to ssh as -i."
)
@click.option(
    "--launch/--no-launch",
    default=False,
    show_default=True,
    help="Also start a local VNC client (macOS Screen Sharing), instead of just printing the connect command.",
)
@click.option(
    "--print",
    "print_only",
    is_flag=True,
    help="Print the tunnel and client commands and exit, without opening the tunnel.",
)
@click.pass_context
def vm_vnc(ctx, vm_id, org_id, local_port, identity_file, launch, print_only):
    """Open a VNC desktop on a macOS VM (Screen Sharing) over an SSH tunnel.

    Forwards a local port to the guest's Screen Sharing service (:5900) through
    the VM's SSH endpoint, so the desktop is never exposed publicly. Holds the
    tunnel open until Ctrl-C; pass --launch to also open Screen Sharing.
    """
    _vm_remote_desktop(
        ctx,
        vm_id,
        org_id,
        want_protocol="vnc",
        guest_port=_VNC_GUEST_PORT,
        local_port=local_port,
        identity_file=identity_file,
        launch=launch,
        print_only=print_only,
    )


@vm.command("port-forward")
@click.argument("vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "--port",
    "guest_port",
    type=click.IntRange(1, 65535),
    required=True,
    help="Guest-side TCP port to forward (e.g. 8080).",
)
@click.option(
    "--local-port",
    type=click.IntRange(1, 65535),
    default=None,
    help="Local port to bind (default: an unused port).",
)
@click.option(
    "-i", "--identity", "identity_file", type=click.Path(), default=None, help="Private key file to pass to ssh as -i."
)
@click.option("--print", "print_only", is_flag=True, help="Print the ssh command and exit, without opening the tunnel.")
@click.pass_context
def vm_port_forward(ctx, vm_id, org_id, guest_port, local_port, identity_file, print_only):
    """Forward a local port to a TCP port on the VM over SSH.

    The generic primitive behind `avr vm rdp` / `avr vm vnc`: opens
    127.0.0.1:<local-port> -> <VM>:<port> through the VM's SSH endpoint, where
    <port> is set by --port, and holds it open until Ctrl-C. Bring your own
    client.
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(f"/orgs/{org_id}/vms/{vm_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch VM", hint="Run `avr vm list` to see your VMs.")

    data = response["data"]
    ssh_ep = (data.get("endpoints") or {}).get("ssh")
    if not ssh_ep:
        raise click.ClickException(
            f"VM {vm_id} has no SSH endpoint yet (state: {data.get('state')}). "
            "Endpoints appear once the VM is RUNNING; check `avr vm show`."
        )

    local_port = local_port or _pick_local_port()
    if print_only:
        display_argv = _ssh_tunnel_argv(
            ssh_ep, local_port=local_port, guest_port=guest_port, identity_file=identity_file, known_hosts=None
        )
        click.echo(shlex.join(display_argv))
        click.echo(
            "(the live command pins the VM's SSH host key; this printed form uses trust-on-first-use)",
            err=True,
        )
        return

    click.echo(f"Forwarding 127.0.0.1:{local_port} -> {vm_id}:{guest_port} ...", err=True)

    def on_ready(proc: subprocess.Popen[bytes]) -> None:
        click.echo(f"Tunnel is up on 127.0.0.1:{local_port}. Press Ctrl-C to close it.", err=True)
        proc.wait()

    _run_tunnel(ssh_ep, local_port=local_port, guest_port=guest_port, identity_file=identity_file, on_ready=on_ready)


# --- Bootstrap ------------------------------------------------------------
#
# `avr vm bootstrap` sets a freshly-started VM up with the things a developer
# reaches for first: their GitHub auth, agent CLIs (claude / codex), a repo,
# dotfiles, environment variables, the `avr` CLI, and an arbitrary run script.
# Each is one shell step run over the pinned-host-key SSH endpoint via `run_ssh`;
# steps stream their output live and stop on the first failure.
#
# Secrets (the GitHub token, forwarded env values, agent API keys) ride SSH
# stdin, never argv, so they never appear in `ps` on the VM. The step script is
# non-secret and is passed as a single shell-quoted argument so OpenSSH's
# space-join to the remote login shell survives one extra parse intact.
#
# Disks are ephemeral (a fresh boot each start), so bootstrap is per-boot; run
# it again after every `avr vm start`.

# claude / codex: the npm package to install and the local env var whose value
# --forward-agent-creds carries into the VM so the agent is usable non-interactively.
_BOOTSTRAP_AGENTS: dict[str, dict[str, str]] = {
    "claude": {"npm": "@anthropic-ai/claude-code", "env": "ANTHROPIC_API_KEY"},
    "codex": {"npm": "@openai/codex", "env": "OPENAI_API_KEY"},
}

# Per-step SSH timeout. npm -g and pipx installs over a cold cache are slow, so
# this is generous; a hung step should still not wedge the whole bootstrap.
_BOOTSTRAP_STEP_TIMEOUT = 600.0

# Connectivity probe before the first step: a just-RUNNING VM may still be
# starting sshd even though its endpoint is published.
_BOOTSTRAP_SSH_ATTEMPTS = 5
_BOOTSTRAP_SSH_DELAY = 3.0
# A dropped connection or a timeout both mean sshd is not up yet: retry. Named
# because the inline `except (A, B)` tuple gets reformatted to the Python-2-shaped
# `except A, B` by the formatter on this Python version.
_SSH_PROBE_ERRORS = (OSError, subprocess.TimeoutExpired)


@dataclass
class _BootstrapStep:
    """One bootstrap action: a bash ``script`` run on the VM, with an optional
    ``stdin`` secret. ``secret`` marks steps whose stdin must never be printed."""

    name: str
    description: str
    script: str
    stdin: str = ""
    secret: bool = False
    timeout: float | None = _BOOTSTRAP_STEP_TIMEOUT


def _local_gh_token() -> str:
    """The local user's GitHub token from ``gh auth token``, to forward into the
    VM. Fails with a clear message if the GitHub CLI is missing or logged out."""
    try:
        completed = subprocess.run(["gh", "auth", "token"], capture_output=True, text=True, timeout=15)
    except FileNotFoundError as exc:
        raise click.ClickException(
            "`gh` CLI not found locally; install the GitHub CLI or drop --setup-github."
        ) from exc
    except OSError as exc:
        raise click.ClickException(f"failed to run `gh auth token`: {exc}") from exc
    token = completed.stdout.strip()
    if completed.returncode != 0 or not token:
        raise click.ClickException("no local GitHub token; run `gh auth login` first, or drop --setup-github.")
    return token


def _parse_bootstrap_agents(flags: tuple[str, ...]) -> list[str]:
    """Resolve ``--install`` values (repeatable and/or comma-separated) to an
    ordered, de-duplicated list of known agent keys."""
    agents: list[str] = []
    for flag in flags:
        for raw in flag.split(","):
            agent = raw.strip().lower()
            if not agent:
                continue
            if agent not in _BOOTSTRAP_AGENTS:
                raise click.BadParameter(
                    f"unknown agent {agent!r} (choose from: {', '.join(_BOOTSTRAP_AGENTS)})",
                    param_hint="--install",
                )
            if agent not in agents:
                agents.append(agent)
    return agents


def _parse_bootstrap_env(flags: tuple[str, ...]) -> dict[str, str]:
    """Resolve ``--env`` values into an ordered name->value map. ``KEY=VALUE``
    sets a literal; a bare ``KEY`` forwards the local environment's value."""
    env: dict[str, str] = {}
    for flag in flags:
        if "=" in flag:
            key, value = flag.split("=", 1)
        else:
            key = flag
            local = os.environ.get(key)
            if local is None:
                raise click.BadParameter(f"{key!r} is not set in the local environment", param_hint="--env")
            value = local
        key = key.strip()
        if not key.isidentifier():
            raise click.BadParameter(f"{key!r} is not a valid variable name", param_hint="--env")
        if "\n" in value:
            raise click.BadParameter(f"{key}: values containing newlines are not supported", param_hint="--env")
        env[key] = value
    return env


def _forward_agent_creds(env: dict[str, str], agents: list[str]) -> None:
    """Add each requested agent's API key from the local environment to ``env``,
    without overriding an explicit --env. With no agents named, forward every
    known key that is present locally. Warns (does not fail) if none are found."""
    added: list[str] = []
    for agent in agents or list(_BOOTSTRAP_AGENTS):
        key = _BOOTSTRAP_AGENTS[agent]["env"]
        value = os.environ.get(key)
        if value and key not in env:
            env[key] = value
            added.append(key)
    if not added:
        click.echo(
            "Warning: --forward-agent-creds set but no agent API keys "
            f"({', '.join(a['env'] for a in _BOOTSTRAP_AGENTS.values())}) are set locally.",
            err=True,
        )


def _load_run_script(value: str) -> str:
    """Resolve ``--run``: a literal script, or ``@path`` to read one from a file."""
    if value.startswith("@"):
        path = Path(value[1:]).expanduser()
        try:
            return path.read_text()
        except OSError as exc:
            raise click.BadParameter(f"cannot read run script {path}: {exc}", param_hint="--run") from exc
    return value


def _env_step(env: dict[str, str], *, redact: bool) -> _BootstrapStep:
    """Write ``env`` to ``~/.avrea/bootstrap.env`` and source it from the shell
    startup files so the vars reach future shells and later `bash -lc` steps.

    Values ride stdin (the file is written from stdin verbatim), so they never
    touch argv. The file is hooked into both ~/.bashrc (interactive non-login
    shells) and the bash *login* file that `ssh` actually reads (the first of
    ~/.bash_profile, ~/.bash_login, ~/.profile that exists). The login file is
    load-bearing: an interactive `ssh` session runs a login shell, and images
    like the GitHub runner ship a ~/.bash_profile that never sources ~/.bashrc,
    so a ~/.bashrc-only hook would never load."""
    blob = "".join(f"export {name}={shlex.quote(value)}\n" for name, value in env.items())
    script = rf"""set -euo pipefail
dest="$HOME/.avrea/bootstrap.env"
mkdir -p "$(dirname "$dest")"
umask 077
cat > "$dest"
marker="# avrea bootstrap env"
_avr_hook() {{
  if ! grep -qF "$marker" "$1" 2>/dev/null; then
    {{ printf '\n%s\n' "$marker"; printf '[ -f "%s" ] && . "%s"\n' "$dest" "$dest"; }} >> "$1"
  fi
}}
_avr_hook "$HOME/.bashrc"
login_rc="$HOME/.profile"
for f in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
  if [ -f "$f" ]; then login_rc="$f"; break; fi
done
_avr_hook "$login_rc"
echo "wrote {len(env)} environment variable(s) to $dest (sourced from ~/.bashrc and $(basename "$login_rc"))"
"""
    return _BootstrapStep(
        "env",
        f"Set {len(env)} environment variable(s): {', '.join(env)}",
        script,
        stdin="" if redact else blob,
        secret=True,
    )


def _github_step(gh_token: str | None, *, redact: bool) -> _BootstrapStep:
    """Authenticate the VM's GitHub CLI with the forwarded local token and wire
    it into git, so later private clones work. Token rides stdin."""
    script = (
        "set -euo pipefail\n"
        "if ! command -v gh >/dev/null 2>&1; then\n"
        '  echo "gh CLI not found on the VM; cannot set up GitHub auth" >&2\n'
        "  exit 1\n"
        "fi\n"
        "gh auth login --with-token\n"
        "gh auth setup-git\n"
        'echo "GitHub: authenticated as $(gh api user -q .login 2>/dev/null || echo unknown)"'
    )
    return _BootstrapStep(
        "github",
        "Authenticate the VM's GitHub CLI and git with your local token",
        script,
        stdin="" if redact else (gh_token or ""),
        secret=True,
    )


def _repo_step(url: str, ref: str | None) -> _BootstrapStep:
    """Clone ``url`` into the home directory (idempotent: fetch if already
    present) and optionally check out ``ref``. Runs after --setup-github so
    private repos authenticate."""
    script = (
        "set -euo pipefail\n"
        f"url={shlex.quote(url)}\n"
        f"ref={shlex.quote(ref or '')}\n"
        'name="$(basename "$url" .git)"\n'
        'dest="$HOME/$name"\n'
        'if [ -d "$dest/.git" ]; then\n'
        '  echo "repo $name already present; fetching"\n'
        '  git -C "$dest" fetch --all --tags --prune\n'
        "else\n"
        '  git clone "$url" "$dest"\n'
        "fi\n"
        'if [ -n "$ref" ]; then git -C "$dest" checkout "$ref"; fi\n'
        'echo "repo ready at $dest"'
    )
    label = f"Clone {url}" + (f" @ {ref}" if ref else "")
    return _BootstrapStep("repo", label, script)


def _dotfiles_step(url: str) -> _BootstrapStep:
    """Clone a dotfiles repo to ~/.dotfiles and run its installer if it ships one
    (install.sh / install / bootstrap.sh / setup.sh).

    The main clone is plain (reliable over https); submodules are then a
    best-effort pass that rewrites the dead ``git://`` protocol (GitHub dropped
    it in 2022) to https and does not fail the step if a stale submodule is
    unreachable. Reports honestly: repos with no recognized installer (e.g.
    stow-style layouts) are cloned only, not applied, so the message says so
    rather than claiming an install that did not happen."""
    script = (
        "set -euo pipefail\n"
        f"url={shlex.quote(url)}\n"
        'dir="$HOME/.dotfiles"\n'
        'if [ -d "$dir/.git" ]; then git -C "$dir" pull --ff-only; else git clone "$url" "$dir"; fi\n'
        "git -C \"$dir\" -c 'url.https://github.com/.insteadOf=git://github.com/' "
        "submodule update --init --recursive "
        '|| echo "warning: some dotfiles submodules failed to init (continuing)"\n'
        'ran=""\n'
        "for f in install.sh install bootstrap.sh setup.sh; do\n"
        '  if [ -f "$dir/$f" ]; then echo "running dotfiles installer $f"; '
        '( cd "$dir" && sh "./$f" ); ran="$f"; break; fi\n'
        "done\n"
        'if [ -n "$ran" ]; then\n'
        '  echo "dotfiles: ran $ran from $url"\n'
        "else\n"
        '  echo "dotfiles: cloned to $dir (no install.sh/install/bootstrap.sh/setup.sh found; '
        'apply manually, e.g. with stow)"\n'
        "fi"
    )
    return _BootstrapStep("dotfiles", f"Install dotfiles from {url}", script)


def _install_agents_step(agents: list[str]) -> _BootstrapStep:
    """Install the requested agent CLIs globally with npm, falling back to sudo
    for a root-owned global prefix."""
    pkgs = " ".join(shlex.quote(_BOOTSTRAP_AGENTS[a]["npm"]) for a in agents)
    script = (
        "set -euo pipefail\n"
        "if ! command -v npm >/dev/null 2>&1; then\n"
        '  echo "npm not found on the VM; cannot install agents" >&2\n'
        "  exit 1\n"
        "fi\n"
        f"for pkg in {pkgs}; do\n"
        '  echo "installing $pkg"\n'
        '  npm install -g "$pkg" || sudo -n npm install -g "$pkg"\n'
        "done\n"
        f'echo "installed: {", ".join(agents)}"'
    )
    return _BootstrapStep("install-agents", f"Install {', '.join(agents)} (npm -g)", script)


def _install_avr_step() -> _BootstrapStep:
    """Install the avr CLI itself, preferring pipx and falling back to pip."""
    script = (
        "set -euo pipefail\n"
        "if command -v pipx >/dev/null 2>&1; then\n"
        "  pipx install --force avr-cli\n"
        "elif command -v python3 >/dev/null 2>&1; then\n"
        "  python3 -m pip install --user --upgrade avr-cli\n"
        "else\n"
        '  echo "no pipx or python3 on the VM; cannot install avr" >&2\n'
        "  exit 1\n"
        "fi\n"
        "echo \"avr installed ($(avr --version 2>/dev/null || echo 'open a new shell, then: avr --version'))\""
    )
    return _BootstrapStep("install-avr", "Install the avr CLI", script)


def _build_bootstrap_steps(
    *,
    env: dict[str, str],
    setup_github: bool,
    gh_token: str | None,
    repo_url: str | None,
    repo_ref: str | None,
    dotfiles_url: str | None,
    agents: list[str],
    install_avr: bool,
    run_script: str | None,
    redact: bool,
) -> list[_BootstrapStep]:
    """Assemble the ordered step list from resolved inputs. ``redact`` blanks the
    secret stdin of the env / github steps for --print. Order matters: env and
    GitHub auth come first so later clones and scripts see them; the user's --run
    runs last."""
    steps: list[_BootstrapStep] = []
    if env:
        steps.append(_env_step(env, redact=redact))
    if setup_github:
        steps.append(_github_step(gh_token, redact=redact))
    if repo_url:
        steps.append(_repo_step(repo_url, repo_ref))
    if dotfiles_url:
        steps.append(_dotfiles_step(dotfiles_url))
    if agents:
        steps.append(_install_agents_step(agents))
    if install_avr:
        steps.append(_install_avr_step())
    if run_script is not None:
        steps.append(_BootstrapStep("run", "Run custom script", run_script))
    return steps


def _print_bootstrap_plan(steps: list[_BootstrapStep]) -> None:
    """Print the ordered plan (scripts shown, secret stdin redacted) for --print."""
    for index, step in enumerate(steps, 1):
        click.echo(f"# [{index}/{len(steps)}] {step.description}")
        if step.secret:
            click.echo("#   (a secret is fed to this step over stdin and is not shown)")
        click.echo(step.script)
        click.echo()


def _wait_for_ssh(ssh_ep: dict[str, Any], identity_file: str | None) -> None:
    """Probe the endpoint until sshd answers, since a just-RUNNING VM may still
    be starting it. Raises if it never becomes reachable."""
    for attempt in range(_BOOTSTRAP_SSH_ATTEMPTS):
        try:
            completed = run_ssh(ssh_ep, ["true"], identity_file=identity_file, stdin_data="", timeout=20)
        except _SSH_PROBE_ERRORS:
            completed = None
        if completed is not None and completed.returncode == 0:
            return
        if attempt < _BOOTSTRAP_SSH_ATTEMPTS - 1:
            click.echo("  waiting for SSH to come up ...", err=True)
            time.sleep(_BOOTSTRAP_SSH_DELAY)
    raise click.ClickException(
        "could not reach the VM over SSH. Confirm it is RUNNING (`avr vm show`) and that your key is authorized."
    )


def _run_bootstrap_steps(ssh_ep: dict[str, Any], steps: list[_BootstrapStep], identity_file: str | None) -> None:
    """Run each step in order over SSH, streaming output. Stops on the first
    failure, naming the step so the live output above pinpoints the cause."""
    _wait_for_ssh(ssh_ep, identity_file)
    for index, step in enumerate(steps, 1):
        click.echo(f"==> [{index}/{len(steps)}] {step.description}", err=True)
        try:
            completed = run_ssh(
                ssh_ep,
                ["bash", "-lc", shlex.quote(step.script)],
                identity_file=identity_file,
                stdin_data=step.stdin,
                timeout=step.timeout,
                capture=False,
            )
        except subprocess.TimeoutExpired as exc:
            raise click.ClickException(f"bootstrap step '{step.name}' timed out after {step.timeout:.0f}s") from exc
        except OSError as exc:  # ssh not on PATH, etc.
            raise click.ClickException(f"failed to launch ssh: {exc}") from exc
        if completed.returncode != 0:
            raise click.ClickException(f"bootstrap step '{step.name}' failed (exit {completed.returncode}).")
    click.echo("Bootstrap complete.", err=True)


@vm.command("bootstrap")
@click.argument("vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "-i", "--identity", "identity_file", type=click.Path(), default=None, help="Private key file to pass to ssh as -i."
)
@click.option(
    "--setup-github/--no-setup-github",
    default=False,
    show_default=True,
    help="Forward your local `gh auth token` into the VM (gh auth login --with-token + gh auth setup-git).",
)
@click.option(
    "--install",
    "install_agents",
    multiple=True,
    help=f"Install an agent CLI (repeatable or comma-separated): {', '.join(_BOOTSTRAP_AGENTS)}.",
)
@click.option(
    "--forward-agent-creds",
    is_flag=True,
    default=False,
    help="Also forward the installed agents' API keys (ANTHROPIC_API_KEY / OPENAI_API_KEY) from your environment.",
)
@click.option("--install-avr", is_flag=True, default=False, help="Install the avr CLI in the VM (pipx, else pip).")
@click.option("--repo", "repo_url", default=None, help="Clone this git repo into the VM's home directory.")
@click.option("--ref", "repo_ref", default=None, help="Check out this ref after cloning (requires --repo).")
@click.option("--dotfiles", "dotfiles_url", default=None, help="Clone this dotfiles repo and run its installer.")
@click.option(
    "--env",
    "env_flags",
    multiple=True,
    help="Set an env var in the VM: KEY=VALUE, or a bare KEY to forward it from your environment. Repeatable.",
)
@click.option("--run", "run_raw", default=None, help="Run a custom script last: an inline script, or @path to a file.")
@click.option("--print", "print_only", is_flag=True, help="Print the ordered plan (secrets redacted) without running.")
@click.pass_context
def vm_bootstrap(
    ctx,
    vm_id,
    org_id,
    identity_file,
    setup_github,
    install_agents,
    forward_agent_creds,
    install_avr,
    repo_url,
    repo_ref,
    dotfiles_url,
    env_flags,
    run_raw,
    print_only,
):
    """Set up a RUNNING VM with your dev essentials over SSH.

    \b
    Each selected step runs on the VM and streams its output; bootstrap stops at
    the first failure. Secrets (GitHub token, forwarded env values, agent API
    keys) ride SSH stdin, never argv. Disks are ephemeral, so re-run bootstrap
    after every `avr vm start`. Example:

    \b
        avr vm bootstrap cvm-abc123 --setup-github --install claude,codex \\
          --repo https://github.com/me/project --env AWS_REGION=eu-north-1
    """
    if repo_ref and not repo_url:
        raise click.UsageError("--ref requires --repo.")

    agents = _parse_bootstrap_agents(install_agents)
    env = _parse_bootstrap_env(env_flags)
    if forward_agent_creds:
        _forward_agent_creds(env, agents)
    run_script = _load_run_script(run_raw) if run_raw is not None else None

    # A token is only fetched for a real run; --print marks the step secret
    # without touching the local gh state.
    gh_token = _local_gh_token() if setup_github and not print_only else None

    steps = _build_bootstrap_steps(
        env=env,
        setup_github=setup_github,
        gh_token=gh_token,
        repo_url=repo_url,
        repo_ref=repo_ref,
        dotfiles_url=dotfiles_url,
        agents=agents,
        install_avr=install_avr,
        run_script=run_script,
        redact=print_only,
    )
    if not steps:
        raise click.UsageError(
            "Nothing to do. Pass at least one of --setup-github, --install, --install-avr, "
            "--repo, --dotfiles, --env, or --run."
        )

    if print_only:
        _print_bootstrap_plan(steps)
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    ssh_ep = _resolve_ssh_endpoint(client, org_id, vm_id)
    _run_bootstrap_steps(ssh_ep, steps, identity_file)
