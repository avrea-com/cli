"""Tests for the bundled agent-skill installer."""

from avrea_cli import __version__
from avrea_cli.commands import skill as skill_module
from avrea_cli.main import cli
from importlib import resources
from pathlib import Path
import json
import pytest
import tomllib


@pytest.fixture
def isolated_home(tmp_path, monkeypatch):
    monkeypatch.setattr(skill_module, "_user_home", lambda: tmp_path)
    return tmp_path


def test_install_defaults_to_both_targets(runner, isolated_home):
    result = runner.invoke(cli, ["skill", "install"])

    assert result.exit_code == 0, result.output
    codex_skill = isolated_home / ".agents" / "skills" / "avrea-cli"
    claude_skill = isolated_home / ".claude" / "skills" / "avrea-cli"
    assert (codex_skill / "SKILL.md").is_file()
    assert (claude_skill / "SKILL.md").is_file()
    assert json.loads((codex_skill / ".avrea-install.json").read_text())["version"] == __version__
    assert "Codex: installed" in result.output
    assert "Claude Code: installed" in result.output


@pytest.mark.parametrize(
    ("target", "present", "absent"),
    [
        ("codex", ".agents/skills/avrea-cli", ".claude/skills/avrea-cli"),
        ("claude", ".claude/skills/avrea-cli", ".agents/skills/avrea-cli"),
    ],
)
def test_install_one_target(runner, isolated_home, target, present, absent):
    result = runner.invoke(cli, ["skill", "install", "--target", target])

    assert result.exit_code == 0, result.output
    assert (isolated_home / present / "SKILL.md").is_file()
    assert not (isolated_home / absent).exists()


def test_install_is_idempotent(runner, isolated_home):
    assert runner.invoke(cli, ["skill", "install", "--target", "codex"]).exit_code == 0

    result = runner.invoke(cli, ["skill", "install", "--target", "codex"])

    assert result.exit_code == 0, result.output
    assert "already current" in result.output


def test_all_preflights_conflicts_before_writing(runner, isolated_home):
    codex_skill = isolated_home / ".agents" / "skills" / "avrea-cli"
    codex_skill.mkdir(parents=True)
    (codex_skill / "SKILL.md").write_text("customer customization\n")

    result = runner.invoke(cli, ["skill", "install", "--target", "all"])

    assert result.exit_code != 0
    assert "Refusing to overwrite" in result.output
    assert (codex_skill / "SKILL.md").read_text() == "customer customization\n"
    assert not (isolated_home / ".claude" / "skills" / "avrea-cli").exists()


def test_force_replaces_modified_skill(runner, isolated_home):
    destination = isolated_home / ".claude" / "skills" / "avrea-cli"
    destination.mkdir(parents=True)
    (destination / "SKILL.md").write_text("customer customization\n")

    result = runner.invoke(cli, ["skill", "install", "--target", "claude", "--force"])

    assert result.exit_code == 0, result.output
    assert "# Avrea CLI" in (destination / "SKILL.md").read_text()


def test_update_replaces_an_unmodified_older_install(runner, isolated_home):
    destination = isolated_home / ".agents" / "skills" / "avrea-cli"
    assert runner.invoke(cli, ["skill", "install", "--target", "codex"]).exit_code == 0
    (destination / "SKILL.md").write_text("old bundled skill\n")
    marker_path = destination / skill_module._MARKER_NAME
    marker = json.loads(marker_path.read_text())
    marker["content_sha256"] = skill_module._tree_digest(destination)
    marker["version"] = "0.1.0"
    marker_path.write_text(json.dumps(marker))

    result = runner.invoke(cli, ["skill", "update", "--target", "codex"])

    assert result.exit_code == 0, result.output
    assert "Codex: updated" in result.output
    assert "# Avrea CLI" in (destination / "SKILL.md").read_text()


def test_update_all_skips_uninstalled_target(runner, isolated_home):
    assert runner.invoke(cli, ["skill", "install", "--target", "codex"]).exit_code == 0

    result = runner.invoke(cli, ["skill", "update"])

    assert result.exit_code == 0, result.output
    assert "Codex: already current" in result.output
    assert "Claude Code: not installed; skipped" in result.output


def test_update_protects_local_modifications(runner, isolated_home):
    destination = isolated_home / ".agents" / "skills" / "avrea-cli"
    assert runner.invoke(cli, ["skill", "install", "--target", "codex"]).exit_code == 0
    (destination / "SKILL.md").write_text("customer customization\n")

    result = runner.invoke(cli, ["skill", "update", "--target", "codex"])

    assert result.exit_code != 0
    assert "Refusing to overwrite" in result.output
    assert (destination / "SKILL.md").read_text() == "customer customization\n"


def test_uninstall_defaults_to_both_targets_and_preserves_parents(runner, isolated_home):
    assert runner.invoke(cli, ["skill", "install"]).exit_code == 0
    other_skill = isolated_home / ".agents" / "skills" / "customer-skill" / "SKILL.md"
    other_skill.parent.mkdir(parents=True)
    other_skill.write_text("customer skill\n")

    result = runner.invoke(cli, ["skill", "uninstall"])

    assert result.exit_code == 0, result.output
    assert "Codex: uninstalled" in result.output
    assert "Claude Code: uninstalled" in result.output
    assert not (isolated_home / ".agents" / "skills" / "avrea-cli").exists()
    assert not (isolated_home / ".claude" / "skills" / "avrea-cli").exists()
    assert other_skill.read_text() == "customer skill\n"
    assert (isolated_home / ".claude" / "skills").is_dir()


def test_remove_alias_supports_one_target(runner, isolated_home):
    assert runner.invoke(cli, ["skill", "install"]).exit_code == 0

    result = runner.invoke(cli, ["skill", "remove", "--target", "codex"])

    assert result.exit_code == 0, result.output
    assert not (isolated_home / ".agents" / "skills" / "avrea-cli").exists()
    assert (isolated_home / ".claude" / "skills" / "avrea-cli").is_dir()


def test_uninstall_is_idempotent_when_targets_are_missing(runner, isolated_home):
    result = runner.invoke(cli, ["skill", "uninstall"])

    assert result.exit_code == 0, result.output
    assert "Codex: not installed; skipped" in result.output
    assert "Claude Code: not installed; skipped" in result.output


def test_uninstall_all_preflights_modified_install(runner, isolated_home):
    codex_skill = isolated_home / ".agents" / "skills" / "avrea-cli"
    claude_skill = isolated_home / ".claude" / "skills" / "avrea-cli"
    assert runner.invoke(cli, ["skill", "install"]).exit_code == 0
    (claude_skill / "SKILL.md").write_text("customer customization\n")

    result = runner.invoke(cli, ["skill", "uninstall"])

    assert result.exit_code != 0
    assert "Refusing to remove a locally modified skill" in result.output
    assert codex_skill.is_dir()
    assert claude_skill.is_dir()


def test_uninstall_force_removes_modified_managed_install(runner, isolated_home):
    destination = isolated_home / ".agents" / "skills" / "avrea-cli"
    assert runner.invoke(cli, ["skill", "install", "--target", "codex"]).exit_code == 0
    (destination / "SKILL.md").write_text("customer customization\n")

    result = runner.invoke(cli, ["skill", "uninstall", "--target", "codex", "--force"])

    assert result.exit_code == 0, result.output
    assert not destination.exists()


def test_uninstall_never_removes_unmanaged_install(runner, isolated_home):
    destination = isolated_home / ".claude" / "skills" / "avrea-cli"
    destination.mkdir(parents=True)
    (destination / "SKILL.md").write_text("customer skill\n")

    result = runner.invoke(cli, ["skill", "uninstall", "--target", "claude", "--force"])

    assert result.exit_code != 0
    assert "Refusing to remove an unmanaged skill" in result.output
    assert (destination / "SKILL.md").read_text() == "customer skill\n"


def test_status_reports_current_and_modified(runner, isolated_home):
    destination = isolated_home / ".agents" / "skills" / "avrea-cli"
    assert runner.invoke(cli, ["skill", "install", "--target", "codex"]).exit_code == 0

    current = runner.invoke(cli, ["skill", "status", "--target", "codex"])
    assert current.exit_code == 0
    assert "Codex: current" in current.output

    (destination / "SKILL.md").write_text("customer customization\n")
    modified = runner.invoke(cli, ["skill", "status", "--target", "codex"])
    assert modified.exit_code == 1
    assert "locally modified or unmanaged" in modified.output


def test_bundled_plugin_has_both_manifests():
    plugin = resources.files("avrea_cli").joinpath("bundled", "avrea")
    codex = json.loads(plugin.joinpath(".codex-plugin", "plugin.json").read_text())
    claude = json.loads(plugin.joinpath(".claude-plugin", "plugin.json").read_text())
    pyproject = tomllib.loads((Path(__file__).parents[1] / "pyproject.toml").read_text())

    assert codex["name"] == claude["name"] == "avrea"
    assert codex["version"] == claude["version"] == pyproject["project"]["version"]
    assert plugin.joinpath("skills", "avrea-cli", "SKILL.md").is_file()
