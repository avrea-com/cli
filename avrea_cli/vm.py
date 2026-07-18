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
from pathlib import Path
from typing import Any
import click
import httpx
import json
import os
import shlex
import sys
import time

_OS_CHOICES = ["linux", "macos", "windows"]

# Hardware tiers and OS versions the API accepts. Mirrors the server
# (avrea/executor/runner_specs.py: VmSize / OsVersion); the customer picks an
# OS, an optional version and a size tier, and the control plane resolves those
# to a concrete image and cpu/memory/disk. The server validates the (os, size)
# and (os, version) pairs (sizes are OS-specific) and remains the source of truth.
_SIZE_CHOICES = ["1-vcpu", "2-vcpu", "4-vcpu", "8-vcpu", "16-vcpu", "32-vcpu"]
_OS_VERSION_CHOICES = ["ubuntu-22.04", "ubuntu-24.04", "ubuntu-26.04", "macos-26", "windows-2025"]

# Bounds / default mirror the API (avrea/api/routers/customer_vms.py); kept
# here only to fail obviously-bad input locally with a clear message. The
# server remains the source of truth and re-validates everything.
_DEFAULT_TTL_SECONDS = 8 * 3600
_MIN_TTL_SECONDS = 300
_MAX_TTL_SECONDS = 7 * 24 * 3600

_TTL_UNITS = {"s": 1, "m": 60, "h": 3600, "d": 86400}

# --wait polls the freshly created VM until it is connectable, then reprints with
# a fully baked (real endpoints + password) connect command. 300s covers a cold
# provision plus first boot.
_WAIT_DEFAULT_TIMEOUT = 300
_WAIT_POLL_SECONDS = 3.0


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


def _resolve_ssh_keys(keys: tuple[str, ...]) -> list[str]:
    """Resolve ``--ssh-key`` values. Each is either a literal public key or
    ``@path`` to read one from a file."""
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
            resolved.append(content)
        else:
            resolved.append(key.strip())
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


def _freerdp_line(binary: str, ip_port: str, username: str, password: str | None, rfx: bool) -> str:
    """One FreeRDP invocation (``xfreerdp`` or ``sdl3-freerdp``).

    ``password`` is baked in only when known; ``rfx`` forces RemoteFX-Progressive
    for GNOME Remote Desktop (Linux) guests. ``/cert:tofu`` rides only the
    password-baked line (create/rotate); the passwordless line relies on the
    client's own first-connect trust prompt."""
    parts = [f"{binary} /v:{ip_port} /u:{username}"]
    if password:
        parts.append(f"/p:{_quote_pw_posix(password)}")
    if rfx:
        parts.append(_RDP_RFX_FLAG)
    parts.append("+clipboard")
    if password:
        parts.append("/cert:tofu")
    return " ".join(parts)


def _rdp_connect_lines(ip_port: str, username: str, password: str | None, platform: str, rfx: bool) -> list[str]:
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
        lines.append(_freerdp_line("sdl3-freerdp", ip_port, username, password, rfx))
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
    return [_freerdp_line("xfreerdp", ip_port, username, password, rfx)]


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
    customer_vm_id: str,
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
            resp = client.public_get(f"/orgs/{org_id}/vms/{customer_vm_id}")
        except httpx.HTTPError as exc:
            if isinstance(exc, httpx.HTTPStatusError):
                status = exc.response.status_code
                if gone_is_ready and status == 404:
                    return None, "ready"
                if status < 500:
                    # A permanent 4xx (bad request, auth, not-found) must surface
                    # now, not hide behind a wait of up to the full timeout.
                    handle_http_error(exc, "check VM status")
            # Transport failure or a retryable 5xx: keep polling until the deadline.
            if time.monotonic() >= deadline:
                return vm, "timeout"
            time.sleep(_WAIT_POLL_SECONDS)
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
    customer_vm_id: str,
) -> None:
    """Print the failure/timeout tail for a human-readable --wait and exit
    non-zero. A ``ready`` disposition is a no-op."""
    if disposition == "ready":
        return
    click.echo()
    if disposition == "failed":
        state = (vm_state or {}).get("state") or "an error state"
        reason = (vm_state or {}).get("state_reason")
        click.echo(f"VM {customer_vm_id} entered {state}" + (f": {reason}" if reason else "") + ".")
    else:
        click.echo(f"Not {target} yet after {wait_timeout}s. Re-run once ready: avr vm show {customer_vm_id}")
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
    help="Guest OS version (e.g. ubuntu-22.04). Defaults to the latest version for the chosen --os.",
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
    customer_vm_id = data["vm"].get("customer_vm_id")

    if wait:
        # Human output surfaces the one-time password before waiting so a timeout
        # or Ctrl-C cannot lose it; JSON carries it in the single final document.
        if not as_json:
            _print_password(password)
            click.echo()
        click.echo(f"Waiting up to {wait_timeout}s for {customer_vm_id} to become RUNNING...", err=True)
        vm_state, disposition = _wait_for_vm(client, org_id, customer_vm_id, wait_timeout, _endpoints_ready)
        if as_json:
            _emit_wait_json(ctx, vm_state or data["vm"], password, disposition)
            return
        click.echo()
        _print_vm(vm_state or data["vm"], password=password)
        _wait_exit(ctx, vm_state, disposition, "connectable", wait_timeout, customer_vm_id)
        return

    if as_json:
        click.echo(json.dumps(data, indent=2, default=str))
        return

    _print_vm(data["vm"], password=password)
    _print_password(password)
    click.echo()
    click.echo(f"Provisioning started. Poll status with: avr vm show {customer_vm_id}")


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
@click.argument("customer_vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--json", "as_json", is_flag=True, help="Emit the full VM record (with egress rules) as JSON.")
@click.pass_context
def vm_show(ctx, customer_vm_id, org_id, as_json):
    """Show a VM's details, including connection endpoints and egress rules."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(f"/orgs/{org_id}/vms/{customer_vm_id}")
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
@click.argument("customer_vm_id")
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
def vm_ssh(ctx, customer_vm_id, ssh_args, org_id, identity_file, print_only):
    """Open an SSH session to a RUNNING VM (or print the command with --print).

    Resolves the VM's SSH endpoint and replaces this process with `ssh`.
    Extra options are passed through to ssh and placed before the destination,
    so port-forwarding and similar flags work. Use `--` to stop avr from
    interpreting them, e.g.:

        avr vm ssh cvm-abc123 -- -L 8080:localhost:80
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(f"/orgs/{org_id}/vms/{customer_vm_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch VM", hint="Run `avr vm list` to see your VMs.")

    data = response["data"]
    ssh = (data.get("endpoints") or {}).get("ssh")
    if not ssh:
        raise click.ClickException(
            f"VM {customer_vm_id} has no SSH endpoint yet (state: {data.get('state')}). "
            "Endpoints appear once the VM is RUNNING; check `avr vm show`."
        )

    argv = ["ssh"]
    if identity_file:
        argv += ["-i", identity_file]
    port = ssh.get("external_port")
    if port:
        argv += ["-p", str(port)]
    argv += list(ssh_args)
    username = ssh.get("username")
    host = ssh.get("external_ip")
    argv.append(f"{username}@{host}" if username else host)

    if print_only:
        click.echo(" ".join(shlex.quote(arg) for arg in argv))
        return

    try:
        os.execvp("ssh", argv)
    except OSError as exc:  # ssh not on PATH, etc.
        raise click.ClickException(f"failed to launch ssh: {exc}") from exc


@vm.command("update")
@click.argument("customer_vm_id")
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
def vm_update(ctx, customer_vm_id, org_id, display_name, ttl, ssh_keys, rotate_password, egress_rules_raw, as_json):
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
        response = client.public_patch(f"/orgs/{org_id}/vms/{customer_vm_id}", json=body)
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
@click.argument("customer_vm_id")
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
def vm_start(ctx, customer_vm_id, org_id, wait, wait_timeout, as_json):
    """Start a stopped VM. Boots a fresh disk and returns a one-time password."""
    _set_desired_state(
        ctx, customer_vm_id, org_id, "RUNNING", as_json, action="start VM", wait=wait, wait_timeout=wait_timeout
    )


@vm.command("stop")
@click.argument("customer_vm_id")
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
def vm_stop(ctx, customer_vm_id, org_id, wait, wait_timeout, as_json):
    """Stop a running VM. The ephemeral disk is discarded."""
    _set_desired_state(
        ctx, customer_vm_id, org_id, "STOPPED", as_json, action="stop VM", wait=wait, wait_timeout=wait_timeout
    )


def _set_desired_state(
    ctx,
    customer_vm_id: str,
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
        response = client.public_patch(f"/orgs/{org_id}/vms/{customer_vm_id}", json={"desired_state": desired_state})
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
        click.echo(f"Waiting up to {wait_timeout}s for {customer_vm_id} to become {desired_state}...", err=True)
        vm_state, disposition = _wait_for_vm(client, org_id, customer_vm_id, wait_timeout, is_ready)
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
            click.echo(f"VM {customer_vm_id} is now {final}.")
        _wait_exit(ctx, vm_state, disposition, fail_target, wait_timeout, customer_vm_id)
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
@click.argument("customer_vm_id")
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
def vm_delete(ctx, customer_vm_id, org_id, yes, wait, wait_timeout, as_json):
    """Delete a VM. Asynchronous while live: shows DELETING until the node confirms the stop."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    if not yes:
        ensure_prompts_allowed("deleting a VM")
        click.confirm(f"Delete VM {customer_vm_id}? This is permanent.", abort=True)

    try:
        response = client.public_delete(f"/orgs/{org_id}/vms/{customer_vm_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "delete VM", hint="Run `avr vm list` to see your VMs.")

    state = (response or {}).get("data", {}).get("state", "DELETING")

    if wait:
        click.echo(f"Waiting up to {wait_timeout}s for {customer_vm_id} to be deleted...", err=True)
        _, disposition = _wait_for_vm(
            client, org_id, customer_vm_id, wait_timeout, _state_is("DELETED"), gone_is_ready=True
        )
        gone = disposition == "ready"
        if as_json:
            click.echo(json.dumps({"customer_vm_id": customer_vm_id, "deleted": gone}, indent=2))
        elif gone:
            click.echo(f"VM {customer_vm_id} deleted.")
        else:
            click.echo(f"Still deleting after {wait_timeout}s. Check with: avr vm show {customer_vm_id}")
        if not gone:
            ctx.exit(1)
        return

    if as_json:
        click.echo(json.dumps(response, indent=2, default=str))
        return

    click.echo(f"VM {customer_vm_id} is now {state}.")
