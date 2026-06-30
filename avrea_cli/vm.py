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
from pathlib import Path
from typing import Any
import click
import httpx
import json
import os
import shlex

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


def _print_vm(vm: dict[str, Any]) -> None:
    click.echo(format_key_value(_vm_summary(vm)))
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
    help="Enable RDP (Windows) / VNC (macOS Screen Sharing). Not available for linux.",
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
    as_json,
):
    """Create a long-running VM.

    \b
    Provisioning is asynchronous: poll `avr vm show <id>` until the state is
    RUNNING and endpoints are populated. The response carries a one-time
    password for the VM's local account; save it now, it is never stored.
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
    if as_json:
        click.echo(json.dumps(data, indent=2, default=str))
        return

    _print_vm(data["vm"])
    _print_password(data.get("password"))
    click.echo()
    click.echo(f"Provisioning started. Poll status with: avr vm show {data['vm'].get('customer_vm_id')}")


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

    Power state is controlled separately with `avr vm start` / `avr vm stop`.
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
        _print_vm(data["vm"])
    _print_password(data.get("password"))


@vm.command("start")
@click.argument("customer_vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--json", "as_json", is_flag=True, help="Emit the raw API response as JSON.")
@click.pass_context
def vm_start(ctx, customer_vm_id, org_id, as_json):
    """Start a stopped VM. Boots a fresh disk and returns a one-time password."""
    _set_desired_state(ctx, customer_vm_id, org_id, "RUNNING", as_json, action="start VM")


@vm.command("stop")
@click.argument("customer_vm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--json", "as_json", is_flag=True, help="Emit the raw API response as JSON.")
@click.pass_context
def vm_stop(ctx, customer_vm_id, org_id, as_json):
    """Stop a running VM. The ephemeral disk is discarded."""
    _set_desired_state(ctx, customer_vm_id, org_id, "STOPPED", as_json, action="stop VM")


def _set_desired_state(ctx, customer_vm_id: str, org_id: str | None, desired_state: str, as_json: bool, *, action: str):
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
    if as_json:
        click.echo(json.dumps(response, indent=2, default=str))
        return

    if data.get("vm"):
        _print_vm(data["vm"])
    _print_password(data.get("password"))


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
@click.option("--json", "as_json", is_flag=True, help="Emit the raw API response as JSON.")
@click.pass_context
def vm_delete(ctx, customer_vm_id, org_id, yes, as_json):
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

    if as_json:
        click.echo(json.dumps(response, indent=2, default=str))
        return

    state = (response or {}).get("data", {}).get("state", "DELETING")
    click.echo(f"VM {customer_vm_id} is now {state}.")
