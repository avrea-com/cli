"""Egress firewall rule management CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import CliConfig
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import handle_http_error
from avrea_cli.output import output_list
import click
import httpx
import ipaddress
import json

_PROTO_CHOICES = ["tcp", "udp", "icmp", "any"]


@click.group()
@click.pass_context
def firewall(ctx):
    """Manage the egress firewall rule list for orgs and repositories."""
    ensure_ctx(ctx)


def _rules_path(org_id: str, repo_id: str | None) -> str:
    if repo_id:
        return f"/orgs/{org_id}/repos/{repo_id}/firewall/rules"
    return f"/orgs/{org_id}/firewall/rules"


def _parse_ports(ports: str | None) -> tuple[int | None, int | None]:
    """Parse ``--ports`` (single port or ``start-end`` span) with validation.

    Raises :class:`click.UsageError` (not ``ValueError``) so bad input
    surfaces as a clean usage message instead of an unhandled traceback.
    """
    if ports is None:
        return None, None
    try:
        if "-" in ports:
            a, b = ports.split("-", 1)
            start, end = int(a), int(b)
        else:
            start = end = int(ports)
    except ValueError as exc:
        raise click.UsageError(f"Invalid --ports {ports!r}: must be 'N' or 'N-M'") from exc
    if not (1 <= start <= 65535) or not (1 <= end <= 65535):
        raise click.UsageError(f"Invalid --ports {ports!r}: ports must be in 1..65535")
    if start > end:
        raise click.UsageError(f"Invalid --ports {ports!r}: start must be <= end")
    return start, end


def _format_matcher(rule: dict) -> str:
    if rule.get("is_default"):
        return "*"
    if rule.get("cidr"):
        return str(rule["cidr"])
    if rule.get("fqdn"):
        return str(rule["fqdn"])
    return "?"


def _format_ports(rule: dict) -> str:
    ps = rule.get("port_start")
    pe = rule.get("port_end")
    if ps is None and pe is None:
        return ""
    if ps == pe or pe is None:
        return str(ps)
    if ps is None:
        return str(pe)
    return f"{ps}-{pe}"


def _print_rules(rules: list[dict], *, as_json: bool) -> None:
    if as_json:
        click.echo(json.dumps(rules, indent=2, default=str))
        return
    for r in rules:
        r["matcher"] = _format_matcher(r)
        r["ports_display"] = _format_ports(r) or "-"
        r["proto_display"] = r.get("protocol") or "any"
        r["default_display"] = "yes" if r.get("is_default") else "no"
    output_list(
        rules,
        columns=["position", "rule_id", "action", "matcher", "proto_display", "ports_display", "default_display"],
        column_labels=["#", "ID", "Action", "Match", "Proto", "Ports", "Default"],
    )


@firewall.command("list")
@click.option("--org", "org_id", help="Organization ID or slug. Uses default org if not specified.")
@click.option("--repo", "repo_id", help="Repository ID. If provided, shows the repo-level list.")
@click.option("--json", "as_json", is_flag=True, help="Output rules as JSON instead of a table.")
@click.pass_context
def firewall_list(ctx, org_id, repo_id, as_json):
    """List egress firewall rules for the given scope."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    try:
        rules = client.public_get(_rules_path(org_id, repo_id))
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list egress firewall rules")
    _print_rules(rules or [], as_json=as_json)


@firewall.command("show")
@click.option("--org", "org_id", help="Organization ID or slug. Uses default org if not specified.")
@click.option("--repo", "repo_id", required=True, help="Repository ID.")
@click.option("--json", "as_json", is_flag=True, help="Output resolved rules as JSON instead of a table.")
@click.pass_context
def firewall_show(ctx, org_id, repo_id, as_json):
    """Show the resolved (merged) firewall rule list for a repository."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    try:
        resolved = client.public_get(f"/orgs/{org_id}/repos/{repo_id}/firewall/resolved")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "show resolved firewall rules")
    rules = (resolved or {}).get("rules", [])
    _print_rules(rules, as_json=as_json)


@firewall.command("add")
@click.option("--org", "org_id", help="Organization ID or slug. Uses default org if not specified.")
@click.option("--repo", "repo_id", help="Repository ID. If provided, adds at repo scope.")
@click.option("--action", "action", type=click.Choice(["allow", "deny"]), required=True)
@click.option("--cidr", "cidr", help="Destination CIDR (e.g. 10.0.0.0/8 or 1.2.3.4/32).")
@click.option("--fqdn", "fqdn", help="Destination hostname (e.g. api.example.com).")
@click.option("--any", "any_matcher", is_flag=True, help="Catch-all (default) rule.")
@click.option("--proto", "proto", type=click.Choice(_PROTO_CHOICES), default="any")
@click.option("--ports", "ports", help="Port or port range (e.g. 443 or 30000-39999).")
@click.option("--position", "position", type=int, help="Insert at a specific 0-indexed position.")
@click.pass_context
def firewall_add(ctx, org_id, repo_id, action, cidr, fqdn, any_matcher, proto, ports, position):
    """Add a rule. Exactly one of --cidr, --fqdn, --any must be specified."""
    targets = sum(1 for v in (cidr, fqdn, any_matcher) if v)
    if targets != 1:
        raise click.UsageError("Exactly one of --cidr, --fqdn, or --any must be provided")
    if cidr:
        try:
            ipaddress.ip_network(cidr, strict=False)
        except ValueError as exc:
            raise click.UsageError(f"Invalid --cidr: {exc}") from exc
    port_start, port_end = _parse_ports(ports)
    if (port_start is not None or port_end is not None) and proto not in ("tcp", "udp"):
        raise click.UsageError("--ports requires --proto tcp or --proto udp")

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    body = {
        "action": action,
        "cidr": cidr,
        "fqdn": fqdn,
        "protocol": proto,
        "port_start": port_start,
        "port_end": port_end,
        "is_default": bool(any_matcher),
    }
    if position is not None:
        body["position"] = position

    try:
        rule = client.public_post(_rules_path(org_id, repo_id), json=body)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "add egress firewall rule")
    click.echo(f"Added rule {rule['rule_id']} at position {rule['position']}")


@firewall.command("delete")
@click.argument("rule_id")
@click.option("--org", "org_id", help="Organization ID or slug. Uses default org if not specified.")
@click.option("--repo", "repo_id", help="Repository ID. If provided, deletes a repo-level rule.")
@click.pass_context
def firewall_delete(ctx, rule_id, org_id, repo_id):
    """Delete a rule by ID."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    try:
        # Inline f-strings (rather than going through `_rules_path`) so the
        # control-side CLI/API contract test can statically extract the path
        # template by AST scan.
        if repo_id:
            client.public_delete(f"/orgs/{org_id}/repos/{repo_id}/firewall/rules/{rule_id}")
        else:
            client.public_delete(f"/orgs/{org_id}/firewall/rules/{rule_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "delete egress firewall rule")
    click.echo(f"Deleted rule {rule_id}")


@firewall.command("move")
@click.argument("rule_id")
@click.option("--to", "to_position", type=int, required=True, help="Target 0-indexed position.")
@click.option("--org", "org_id", help="Organization ID or slug. Uses default org if not specified.")
@click.option("--repo", "repo_id", help="Repository ID. If provided, moves a repo-level rule.")
@click.pass_context
def firewall_move(ctx, rule_id, to_position, org_id, repo_id):
    """Move a rule to a new position (rewrites the full ordering atomically)."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    try:
        rules = client.public_get(_rules_path(org_id, repo_id)) or []
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch rules for reorder")
    ids = [r["rule_id"] for r in rules]
    if rule_id not in ids:
        raise click.UsageError(f"Rule {rule_id} not found in this scope")
    if to_position < 0 or to_position >= len(ids):
        raise click.UsageError(f"Target position must be in 0..{len(ids) - 1}")
    ids.remove(rule_id)
    ids.insert(to_position, rule_id)
    try:
        if repo_id:
            client.public_post(f"/orgs/{org_id}/repos/{repo_id}/firewall/rules/reorder", json={"rule_ids": ids})
        else:
            client.public_post(f"/orgs/{org_id}/firewall/rules/reorder", json={"rule_ids": ids})
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "reorder egress firewall rules")
    click.echo(f"Moved {rule_id} to position {to_position}")


@firewall.command("set-default")
@click.option("--org", "org_id", help="Organization ID or slug. Uses default org if not specified.")
@click.option("--repo", "repo_id", help="Repository ID. If provided, sets the repo-level default.")
@click.option("--action", "action", type=click.Choice(["allow", "deny"]), required=True)
@click.pass_context
def firewall_set_default(ctx, org_id, repo_id, action):
    """Set (or replace) the catch-all (default) rule for the scope."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    try:
        rules = client.public_get(_rules_path(org_id, repo_id)) or []
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch rules before setting default")
    # Update an existing default in-place rather than delete-then-create:
    # any failure between those two calls would leave the scope with no
    # default rule at all, which is a live policy regression.
    existing_default = next((r for r in rules if r.get("is_default")), None)
    if existing_default is not None:
        rid = existing_default["rule_id"]
        if repo_id:
            patch_path = f"/orgs/{org_id}/repos/{repo_id}/firewall/rules/{rid}"
        else:
            patch_path = f"/orgs/{org_id}/firewall/rules/{rid}"
        try:
            rule = client.public_patch(patch_path, json={"action": action})
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "update default rule")
    else:
        body = {"action": action, "is_default": True, "protocol": "any"}
        try:
            rule = client.public_post(_rules_path(org_id, repo_id), json=body)
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "create default rule")
    click.echo(f"Set default rule: {action} (rule_id={rule['rule_id']})")


@firewall.command("flow-summaries")
@click.option("--org", "org_id", help="Organization ID or slug. Uses default org if not specified.")
@click.option("--repo", "repo_id", required=True, help="Repository ID.")
@click.option(
    "--job",
    "--job-id",
    "job_id",
    default=None,
    help="Filter to every VM execution attempt for a job ID.",
)
@click.option(
    "--with-drops",
    "only_with_drops",
    is_flag=True,
    help="Show only summaries where the firewall blocked at least one flow.",
)
@click.option(
    "-L",
    "--limit",
    type=click.IntRange(1, 1000),
    default=20,
    show_default=True,
    help="Max summaries to return.",
)
@click.option("--offset", type=click.IntRange(min=0), default=0, show_default=True, help="Number of summaries to skip.")
@click.option(
    "--from",
    "--start-after",
    "start_after",
    default=None,
    help="Only include summaries that started at or after this ISO-8601 timestamp.",
)
@click.option(
    "--to",
    "--end-before",
    "end_before",
    default=None,
    help="Only include summaries that ended at or before this ISO-8601 timestamp.",
)
@click.option("--json", "as_json", is_flag=True, help="Emit raw JSON instead of a table.")
@click.pass_context
def firewall_flow_summaries(
    ctx,
    org_id,
    repo_id,
    job_id,
    only_with_drops,
    limit,
    offset,
    start_after,
    end_before,
    as_json,
):
    """Show per-VM network activity summaries captured at VM stop.

    Each row is the totals + top-N destinations + per-rule drop counters
    for one VM run. Use ``--with-drops`` to triage what the firewall
    blocked after editing a rule or ``--job`` to include every execution
    attempt for a job.
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    params: dict = {
        "limit": limit,
        "offset": offset,
        "only_with_drops": only_with_drops,
    }
    if job_id:
        params["job_id"] = job_id
    if start_after:
        params["start_after"] = start_after
    if end_before:
        params["end_before"] = end_before
    try:
        body = client.public_get(
            f"/orgs/{org_id}/repos/{repo_id}/firewall/vm-flow-summaries",
            params=params,
        )
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list flow summaries")
    rows = (body or {}).get("results") or []
    if as_json:
        click.echo(json.dumps(body, indent=2, default=str))
        return
    if not rows:
        click.echo("No flow summaries yet. They are captured at VM stop, so wait until a run finishes.")
        return
    display_rows = []
    for r in rows:
        drops = r.get("drops") or []
        blocked_dns = r.get("blocked_dns_queries") or []
        top = r.get("top_destinations") or []
        blocked = [f"{d.get('label')}({d.get('packets', 0)} pkt)" for d in drops]
        blocked.extend(f"DNS:{d.get('qname')}({d.get('count', 1)})" for d in blocked_dns)
        display_rows.append(
            {
                "vm_id": r.get("vm_id") or "-",
                "end_ts": r.get("end_ts") or "-",
                "duration": f"{r.get('duration_s', 0)}s",
                "egress": f"{r.get('bytes_egress', 0)} B / {r.get('packets_egress', 0)} pkt",
                "ingress": f"{r.get('bytes_ingress', 0)} B / {r.get('packets_ingress', 0)} pkt",
                "flows": r.get("flow_count", 0),
                "top_dst": ", ".join(d.get("dst_fqdn") or d.get("dst_ip") or "-" for d in top[:3]),
                "blocked": ", ".join(blocked) or "-",
            }
        )
    output_list(
        display_rows,
        columns=["vm_id", "end_ts", "duration", "egress", "ingress", "flows", "top_dst", "blocked"],
        column_labels=["VM", "Ended", "Run", "Egress", "Ingress", "Flows", "Top destinations", "Blocked"],
    )

    if body.get("has_more"):
        click.echo(f"\nMore results available. Re-run with --offset {offset + len(rows)}", err=True)
