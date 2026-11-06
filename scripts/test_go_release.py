"""Publication requires both an explicit gate and an exact version tag."""

from scripts.go_release import publication_allowed
import pytest


@pytest.mark.parametrize("enabled", ["", "false", "TRUE", "1", "true"])
def test_manual_run_never_publishes(enabled):
    assert not publication_allowed("workflow_dispatch", "refs/tags/v0.7.0", enabled, "0.7.0")


@pytest.mark.parametrize("enabled", ["", "false", "TRUE", "1"])
def test_tag_does_not_publish_without_explicit_gate(enabled):
    assert not publication_allowed("push", "refs/tags/v0.7.0", enabled, "0.7.0")


@pytest.mark.parametrize("version", ["0.7.0", "0.7.0-rc.1"])
def test_exact_release_tag_with_gate_can_publish(version):
    assert publication_allowed("push", f"refs/tags/v{version}", "true", version)


@pytest.mark.parametrize(
    "event,ref",
    [
        ("push", "refs/heads/main"),
        ("push", "refs/tags/v0.6.0"),
        ("pull_request", "refs/tags/v0.7.0"),
    ],
)
def test_other_refs_and_events_cannot_publish(event, ref):
    with pytest.raises(ValueError, match="tag matching"):
        publication_allowed(event, ref, "true", "0.7.0")
