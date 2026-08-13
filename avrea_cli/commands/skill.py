"""Manage the bundled Avrea agent skill for Codex and Claude Code."""

from avrea_cli import __version__
from avrea_cli.click_ext import GhGroup
from hashlib import sha256
from importlib import resources
from pathlib import Path
from typing import Literal
import click
import json
import os
import shutil
import tempfile

SkillTarget = Literal["codex", "claude"]
InstallState = Literal["missing", "current", "outdated", "modified"]

_SKILL_NAME = "avrea-cli"
_MARKER_NAME = ".avrea-install.json"
_INSTALLER_NAME = "avr"
_TARGETS: tuple[SkillTarget, ...] = ("codex", "claude")
_TARGET_LABELS: dict[SkillTarget, str] = {"codex": "Codex", "claude": "Claude Code"}
_TARGET_PATHS: dict[SkillTarget, Path] = {
    "codex": Path(".agents") / "skills" / _SKILL_NAME,
    "claude": Path(".claude") / "skills" / _SKILL_NAME,
}
_TARGET_CHOICE = click.Choice(["codex", "claude", "all"], case_sensitive=False)


def _user_home() -> Path:
    return Path.home()


def _bundled_skill_resource():
    return resources.files("avrea_cli").joinpath("bundled", "avrea", "skills", _SKILL_NAME)


def _selected_targets(target: str) -> tuple[SkillTarget, ...]:
    normalized = target.lower()
    if normalized == "all":
        return _TARGETS
    if normalized == "codex":
        return ("codex",)
    return ("claude",)


def _destination(target: SkillTarget) -> Path:
    return _user_home() / _TARGET_PATHS[target]


def _path_exists(path: Path) -> bool:
    return os.path.lexists(path)


def _tree_digest(root: Path) -> str:
    """Hash names, node types, link targets, and file contents under ``root``."""
    digest = sha256()
    for entry in sorted(root.rglob("*"), key=lambda path: path.relative_to(root).as_posix()):
        relative = entry.relative_to(root).as_posix()
        if relative == _MARKER_NAME:
            continue
        digest.update(relative.encode())
        digest.update(b"\0")
        if entry.is_symlink():
            digest.update(b"link\0")
            digest.update(os.readlink(entry).encode())
        elif entry.is_dir():
            digest.update(b"dir\0")
        elif entry.is_file():
            digest.update(b"file\0")
            with entry.open("rb") as handle:
                while chunk := handle.read(1024 * 1024):
                    digest.update(chunk)
        else:
            digest.update(b"other\0")
    return digest.hexdigest()


def _read_marker(destination: Path) -> dict[str, str] | None:
    marker_path = destination / _MARKER_NAME
    try:
        payload = json.loads(marker_path.read_text(encoding="utf-8"))
    except OSError, json.JSONDecodeError:
        return None
    if not isinstance(payload, dict) or payload.get("installer") != _INSTALLER_NAME:
        return None
    content_sha256 = payload.get("content_sha256")
    version = payload.get("version")
    if not isinstance(content_sha256, str) or not isinstance(version, str):
        return None
    return {"content_sha256": content_sha256, "version": version}


def _classify_install(destination: Path, source_digest: str) -> InstallState:
    if not _path_exists(destination):
        return "missing"
    if destination.is_symlink() or not destination.is_dir():
        return "modified"

    installed_digest = _tree_digest(destination)
    if installed_digest == source_digest:
        return "current"

    marker = _read_marker(destination)
    if marker is not None and marker["content_sha256"] == installed_digest:
        return "outdated"
    return "modified"


def _write_marker(staged_skill: Path, content_sha256: str) -> None:
    payload = {
        "installer": _INSTALLER_NAME,
        "version": __version__,
        "content_sha256": content_sha256,
    }
    (staged_skill / _MARKER_NAME).write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")


def _replace_skill(source: Path, destination: Path, source_digest: str) -> None:
    """Replace ``destination`` atomically while retaining rollback until success."""
    destination.parent.mkdir(parents=True, exist_ok=True)
    staging_root = Path(tempfile.mkdtemp(prefix=f".{_SKILL_NAME}-", dir=destination.parent))
    staged_skill = staging_root / "new"
    previous = staging_root / "previous"
    moved_previous = False
    try:
        shutil.copytree(source, staged_skill)
        _write_marker(staged_skill, source_digest)
        if _path_exists(destination):
            os.replace(destination, previous)
            moved_previous = True
        os.replace(staged_skill, destination)
    except OSError:
        if moved_previous and not _path_exists(destination) and _path_exists(previous):
            os.replace(previous, destination)
        raise
    finally:
        shutil.rmtree(staging_root, ignore_errors=True)


def _is_managed_install(destination: Path) -> bool:
    """Return whether ``destination`` is a real directory installed by avr."""
    return (
        _path_exists(destination)
        and not destination.is_symlink()
        and destination.is_dir()
        and _read_marker(destination) is not None
    )


def _run_install(action: Literal["install", "update"], target: str, force: bool) -> None:
    targets = _selected_targets(target)
    with resources.as_file(_bundled_skill_resource()) as source:
        source_digest = _tree_digest(source)
        states: dict[SkillTarget, InstallState] = {
            selected: _classify_install(_destination(selected), source_digest) for selected in targets
        }

        conflicts: list[SkillTarget] = [selected for selected, state in states.items() if state == "modified"]
        missing: list[SkillTarget] = [selected for selected, state in states.items() if state == "missing"]
        if conflicts and not force:
            rendered = ", ".join(str(_destination(selected)) for selected in conflicts)
            raise click.ClickException(
                f"Refusing to overwrite a modified or unmanaged skill at {rendered}. Re-run with --force to replace it."
            )
        if action == "update" and target.lower() != "all" and missing:
            rendered = ", ".join(str(_destination(selected)) for selected in missing)
            raise click.ClickException(f"Cannot update a skill that is not installed at {rendered}.")
        if action == "update" and len(missing) == len(targets):
            raise click.ClickException("Cannot update the Avrea skill because it is not installed for any target.")

        for selected in targets:
            destination = _destination(selected)
            state = states[selected]
            if action == "update" and state == "missing":
                click.echo(f"{_TARGET_LABELS[selected]}: not installed; skipped")
                continue
            marker = _read_marker(destination) if state == "current" else None
            if state == "current" and marker is not None and marker["version"] == __version__:
                click.echo(f"{_TARGET_LABELS[selected]}: already current at {destination}")
                continue
            try:
                _replace_skill(source, destination, source_digest)
            except OSError as exc:
                raise click.ClickException(f"Could not install {_TARGET_LABELS[selected]} skill: {exc}") from exc
            verb = "Updated" if state in {"current", "outdated", "modified"} else "Installed"
            click.echo(f"{_TARGET_LABELS[selected]}: {verb.lower()} at {destination}")


def _run_uninstall(target: str, force: bool) -> None:
    targets = _selected_targets(target)
    with resources.as_file(_bundled_skill_resource()) as source:
        source_digest = _tree_digest(source)
        states: dict[SkillTarget, InstallState] = {
            selected: _classify_install(_destination(selected), source_digest) for selected in targets
        }

    unmanaged: list[SkillTarget] = [
        selected
        for selected, state in states.items()
        if state != "missing" and not _is_managed_install(_destination(selected))
    ]
    if unmanaged:
        rendered = ", ".join(str(_destination(selected)) for selected in unmanaged)
        raise click.ClickException(
            f"Refusing to remove an unmanaged skill at {rendered}. Remove it manually if it is no longer needed."
        )

    modified: list[SkillTarget] = [selected for selected, state in states.items() if state == "modified"]
    if modified and not force:
        rendered = ", ".join(str(_destination(selected)) for selected in modified)
        raise click.ClickException(
            f"Refusing to remove a locally modified skill at {rendered}. Re-run with --force to remove it."
        )

    for selected in targets:
        destination = _destination(selected)
        if states[selected] == "missing":
            click.echo(f"{_TARGET_LABELS[selected]}: not installed; skipped")
            continue
        try:
            shutil.rmtree(destination)
        except OSError as exc:
            raise click.ClickException(f"Could not uninstall {_TARGET_LABELS[selected]} skill: {exc}") from exc
        click.echo(f"{_TARGET_LABELS[selected]}: uninstalled from {destination}")


@click.group(cls=GhGroup)
def skill():
    """Manage Avrea's agent skill for Codex and Claude."""


@skill.command("install")
@click.option(
    "--target",
    type=_TARGET_CHOICE,
    default="all",
    show_default=True,
    help="Agent host to install for.",
)
@click.option("--force", is_flag=True, help="Replace an existing modified or unmanaged skill.")
def skill_install(target: str, force: bool):
    """Install the bundled Avrea skill.

    \b
    Examples:
        avr skill install
        avr skill install --target codex
        avr skill install --target claude
        avr skill install --target all --force
    """
    _run_install("install", target, force)


@skill.command("update")
@click.option(
    "--target",
    type=_TARGET_CHOICE,
    default="all",
    show_default=True,
    help="Installed agent host to update.",
)
@click.option("--force", is_flag=True, help="Replace a locally modified skill.")
def skill_update(target: str, force: bool):
    """Update an installed Avrea skill from this avr release.

    \b
    Examples:
        avr skill update
        avr skill update --target claude
        avr skill update --target all --force
    """
    _run_install("update", target, force)


@skill.command("uninstall")
@click.option(
    "--target",
    type=_TARGET_CHOICE,
    default="all",
    show_default=True,
    help="Installed agent host to uninstall from.",
)
@click.option("--force", is_flag=True, help="Remove a locally modified avr-managed skill.")
def skill_uninstall(target: str, force: bool):
    """Uninstall the Avrea skill. Alias: remove.

    Unmanaged skill directories are never removed automatically.

    \b
    Examples:
        avr skill uninstall
        avr skill uninstall --target claude
        avr skill remove --target codex
        avr skill uninstall --target all --force
    """
    _run_uninstall(target, force)


@skill.command("remove", hidden=True)
@click.option(
    "--target",
    type=_TARGET_CHOICE,
    default="all",
    show_default=True,
    help="Installed agent host to uninstall from.",
)
@click.option("--force", is_flag=True, help="Remove a locally modified avr-managed skill.")
def skill_remove(target: str, force: bool):
    """Alias for ``avr skill uninstall``."""
    _run_uninstall(target, force)


@skill.command("status")
@click.option(
    "--target",
    type=_TARGET_CHOICE,
    default="all",
    show_default=True,
    help="Agent host to inspect.",
)
def skill_status(target: str):
    """Show whether the bundled Avrea skill is installed and current."""
    targets = _selected_targets(target)
    labels: dict[InstallState, str] = {
        "missing": "not installed",
        "current": "current",
        "outdated": "update available",
        "modified": "locally modified or unmanaged",
    }
    all_current = True
    with resources.as_file(_bundled_skill_resource()) as source:
        source_digest = _tree_digest(source)
        for selected in targets:
            destination = _destination(selected)
            state = _classify_install(destination, source_digest)
            click.echo(f"{_TARGET_LABELS[selected]}: {labels[state]} at {destination}")
            all_current = all_current and state == "current"
    if not all_current:
        raise click.exceptions.Exit(1)
