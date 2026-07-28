"""Tests for egress firewall CLI commands."""

from avrea_cli.main import cli
import json
import re

FLOW_RESPONSE = {
    "results": [
        {
            "id": "summary-1",
            "repository_id": "rep-test",
            "vm_id": "vm-test",
            "vm_index": 0,
            "vm_ip": "10.0.0.2",
            "start_ts": "2026-07-16T10:00:00Z",
            "end_ts": "2026-07-16T10:01:00Z",
            "duration_s": 60,
            "bytes_egress": 1024,
            "bytes_ingress": 2048,
            "packets_egress": 10,
            "packets_ingress": 20,
            "flow_count": 3,
            "top_destinations": [{"dst_ip": "203.0.113.10", "dst_fqdn": "api.example.com", "bytes": 900, "flows": 2}],
            "top_proto_ports": [{"protocol": "tcp", "port": 443, "bytes": 900}],
            "drops": [{"label": "vm-rule-efr-test-deny", "packets": 2, "bytes": 128}],
            "blocked_dns_queries": [{"qname": "blocked.example.com", "ts": "2026-07-16T10:00:30Z", "count": 4}],
        }
    ],
    "total_estimated": 2,
    "limit": 1,
    "offset": 0,
    "has_more": True,
    "namespaces_searched": 1,
    "namespaces_with_results": ["rep-test"],
}


def test_flow_summaries_forwards_all_api_filters(runner, monkeypatch):
    captured = {}

    def fake_get(self, path, **kwargs):
        captured["path"] = path
        captured["params"] = kwargs["params"]
        return FLOW_RESPONSE

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

    result = runner.invoke(
        cli,
        [
            "firewall",
            "flow-summaries",
            "--repo",
            "rep-test",
            "--job-id",
            "job-test",
            "--with-drops",
            "--start-after",
            "2026-07-16T00:00:00Z",
            "--end-before",
            "2026-07-17T00:00:00Z",
            "--limit",
            "1",
            "--offset",
            "5",
        ],
    )

    assert result.exit_code == 0, result.output
    assert captured == {
        "path": "/orgs/org-default/repos/rep-test/firewall/vm-flow-summaries",
        "params": {
            "limit": 1,
            "offset": 5,
            "only_with_drops": True,
            "job_id": "job-test",
            "start_after": "2026-07-16T00:00:00Z",
            "end_before": "2026-07-17T00:00:00Z",
        },
    }
    # Anchored, not a bare `in`: a plain substring check also passes on
    # "evil-api.example.com.attacker.test", and CodeQL flags it as incomplete
    # URL sanitization. Match the destination as a whole host instead.
    assert re.search(r"(?<![\w.-])api\.example\.com(?![\w.-])", result.output)
    assert "vm-rule-efr-test-deny(2 pkt)" in result.output
    assert "DNS:blocked.example.com(4)" in result.output
    assert "More results available. Re-run with --offset 6" in result.stderr


def test_flow_summaries_supports_raw_json(runner, monkeypatch):
    captured = {}

    def fake_get(self, path, **kwargs):
        captured["params"] = kwargs["params"]
        return FLOW_RESPONSE

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

    result = runner.invoke(
        cli,
        ["firewall", "flow-summaries", "--repo", "rep-test", "--json"],
    )

    assert result.exit_code == 0, result.output
    assert "vm_id" not in captured["params"]
    assert json.loads(result.output) == FLOW_RESPONSE


def test_flow_summaries_help_omits_vm_filter(runner):
    result = runner.invoke(cli, ["firewall", "flow-summaries", "--help"])

    assert result.exit_code == 0
    assert "--vm" not in result.output
    assert "--job, --job-id" in result.output
