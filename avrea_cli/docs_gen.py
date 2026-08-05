"""Generate reference docs from the live Click command tree.

The Click tree in :mod:`avrea_cli.main` is the single source of truth.
Every option, argument, type, default, envvar, and help string is read
straight off the live command objects, so the generated docs cannot
drift from the implementation.

Supported formats:

- ``markdown`` — single-file man-page-style reference (humans + agents).
- ``json``     — structured tree with stable schema (tooling + agents).
- ``starlight``— one Astro Starlight page per top-level command.
- ``man``      — troff(1) per command (top-level and subcommand).
- ``all``      — write every format into a standard layout under ``--out``.

Run via ``avr internal docs ...`` (the Make targets call it for the
avr-cli source tree). The module also runs as ``python -m
avrea_cli.docs_gen`` for any context where the CLI itself isn't on PATH.
"""

from avrea_cli import __version__
from avrea_cli import main as _avr_main
from avrea_cli.click_ext import SECTION_ORDER
from click._utils import Sentinel as ClickSentinel
from pathlib import Path
from typing import Any
import click
import json
import re

PROG = "avr"


def _param_to_dict(param: click.Parameter) -> dict[str, Any]:
    type_obj = param.type
    type_name = getattr(type_obj, "name", None) or type(type_obj).__name__.lower()
    default = param.default
    if callable(default) or isinstance(default, ClickSentinel):
        default = None
    info: dict[str, Any] = {
        "name": param.name,
        "kind": "argument" if isinstance(param, click.Argument) else "option",
        "opts": list(param.opts),
        "secondary_opts": list(param.secondary_opts),
        "type": type_name,
        "required": bool(param.required),
        "multiple": bool(getattr(param, "multiple", False)),
        "nargs": param.nargs,
        "is_flag": bool(getattr(param, "is_flag", False)),
        "envvar": param.envvar,
        "help": getattr(param, "help", None),
        "hidden": bool(getattr(param, "hidden", False)),
        "show_default": bool(getattr(param, "show_default", False)),
        "default": default,
    }
    if isinstance(type_obj, click.Choice):
        info["choices"] = list(type_obj.choices)
    return info


def _walk(cmd: click.Command, path: list[str]) -> dict[str, Any]:
    ctx = click.Context(cmd, info_name=cmd.name)
    info: dict[str, Any] = {
        "name": cmd.name,
        "path": path,
        "full": " ".join(path),
        "is_group": isinstance(cmd, click.Group),
        "short_help": cmd.get_short_help_str(limit=200),
        "help": cmd.help,
        "epilog": cmd.epilog,
        "deprecated": getattr(cmd, "deprecated", False),
        "hidden": bool(getattr(cmd, "hidden", False)),
        "usage": " ".join([*path, *cmd.collect_usage_pieces(ctx)]),
        "params": [_param_to_dict(p) for p in cmd.params],
        "subcommands": [],
    }
    if isinstance(cmd, click.Group):
        for sub_name in cmd.list_commands(ctx):
            sub = cmd.get_command(ctx, sub_name)
            if sub is None or getattr(sub, "hidden", False):
                continue
            info["subcommands"].append(_walk(sub, [*path, sub_name]))
    return info


def build_tree() -> dict[str, Any]:
    # ``avr-cli/avrea_cli/main.py`` imports ``commands.internal`` which
    # imports this module — so binding ``main`` by module reference at the
    # top of this file (rather than ``from .main import cli, LAZY_COMMANDS, ...``)
    # is what prevents the eager-name circular import. We resolve the
    # attributes here, after main.py has finished loading.
    import importlib  # noqa: PLC0415

    root_cli = _avr_main.cli
    sections: dict[str, list[dict[str, Any]]] = {}
    for name, import_path, attr, _short_help, section in _avr_main.LAZY_COMMANDS:
        if section is None:
            continue
        # Doc generation needs the real click command — import the module
        # eagerly here. Slow-but-rare path; runtime ``--help`` stays lazy.
        module = importlib.import_module(import_path)
        cmd = getattr(module, attr)
        sections.setdefault(section, []).append(_walk(cmd, [PROG, name]))
    return {
        "program": PROG,
        "version": __version__,
        "summary": (root_cli.help or "").strip(),
        "global_options": [_param_to_dict(p) for p in root_cli.params if not isinstance(p, click.Argument)],
        "aliases": dict(_avr_main.ALIASES),
        "section_order": list(SECTION_ORDER),
        "sections": sections,
    }


# ------------------------------------------------------------------ shared --


def _slug(path: list[str]) -> str:
    return "-".join(path)


def _strip_short_help_prefix(help_text: str | None, short: str) -> str:
    """Drop the leading paragraph if it matches ``short`` — Click's
    ``get_short_help_str`` returns the first paragraph of ``help``, so
    re-rendering both would duplicate the line."""
    if not help_text:
        return ""
    if not short:
        return help_text
    paragraphs = help_text.lstrip().split("\n\n", 1)
    first = " ".join(paragraphs[0].split())
    if first == short.strip():
        return paragraphs[1] if len(paragraphs) == 2 else ""
    return help_text


def _iter_top_commands(tree: dict[str, Any]):
    for section in tree["section_order"]:
        for cmd in tree["sections"].get(section, []):
            yield section, cmd


# -------------------------------------------------------------- markdown --


_MD_CODE_SPAN_RE = re.compile(r"(`+).+?\1")


def _md_escape(text: str) -> str:
    """Escape angle brackets in prose so literal placeholders in help text
    (e.g. ``cache.<name>.enabled``, ``avr-<vm-id>``, ``<local> -> <VM>:<guest>``)
    render as text instead of being parsed as HTML/JSX tags and dropped by the
    markdown / Starlight renderer. Inline code spans are left verbatim: inside
    backticks ``<`` already renders literally, so escaping there would surface a
    raw ``&lt;`` instead. Only ``<``/``>`` are touched; ``&`` is left alone. Not
    for fenced or indented code blocks — the caller keeps those literal."""

    def _escape_prose(segment: str) -> str:
        return segment.replace("<", "&lt;").replace(">", "&gt;")

    out: list[str] = []
    last = 0
    for match in _MD_CODE_SPAN_RE.finditer(text):
        out.append(_escape_prose(text[last : match.start()]))
        out.append(match.group(0))  # code span, kept literal
        last = match.end()
    out.append(_escape_prose(text[last:]))
    return "".join(out)


def _md_render_help_block(text: str | None, lang: str = "text") -> str:
    """Convert a Click help/epilog string to markdown.

    Click uses a literal ``\\b`` (backspace) on its own line to mark the
    start of a paragraph that should not be re-flowed. We render those
    blocks as fenced code so indentation and line breaks survive.
    ``lang`` controls the fenced-block language hint — ``"sh"`` for help
    bodies (which contain ``Examples:``-style shell snippets in this CLI),
    ``"text"`` for epilogs (plain notes / field listings).
    """
    if not text:
        return ""
    lines = text.splitlines()
    out: list[str] = []
    i = 0
    while i < len(lines):
        line = lines[i]
        if line.strip() == "\b":
            block: list[str] = []
            i += 1
            while i < len(lines) and lines[i].strip() != "":
                block.append(lines[i])
                i += 1
            if block:
                indent = min((len(b) - len(b.lstrip(" ")) for b in block if b.strip()), default=0)
                stripped = [b[indent:] if len(b) >= indent else b for b in block]
                out.append(f"```{lang}")
                out.extend(stripped)
                out.append("```")
            continue
        # An indented line is a Markdown code block (shell examples like
        # ``avr ... >> ~/.ssh/config``); keep it literal. Escape only genuine
        # prose so bare <placeholders> survive but code stays intact.
        out.append(line if line.startswith("    ") else _md_escape(line))
        i += 1
    return "\n".join(out).strip()


def _md_param_signature(p: dict[str, Any]) -> str:
    if p["kind"] == "argument":
        name = (p["name"] or "").upper()
        if p["nargs"] == -1:
            name += "..."
        if not p["required"]:
            name = f"[{name}]"
        return f"`{name}`"
    primary = ", ".join(p["opts"])
    if p["secondary_opts"]:
        primary = f"{primary} / {', '.join(p['secondary_opts'])}"
    if not p["is_flag"]:
        meta = (p["type"] or "value").upper()
        primary = f"{primary} <{meta}>"
    return f"`{primary}`"


def _md_param_signature_html(p: dict[str, Any]) -> str:
    """HTML signature with semantic CSS classes — used in Starlight pages so
    the inline tokens match the Shiki-highlighted synopsis above each command.
    On GitHub the classes are stripped and the spans render as plain inline
    code, identical in content to the backtick form.

    ``--`` is HTML-encoded as ``&#x2D;&#x2D;`` defensively. Markdown
    pipelines that enable smartypants (Astro's default) transform a
    literal ``--`` into an em-dash inside text nodes — and raw HTML
    ``<code>`` tags don't get the code-fence exemption that backtick
    code does. The avrea docs site disables smartypants globally, but
    encoding here keeps the output safe to drop into any pipeline.
    """
    if p["kind"] == "argument":
        name = (p["name"] or "").upper()
        if p["nargs"] == -1:
            name += "..."
        if not p["required"]:
            name = f"[{name}]"
        return f'<code class="cli-arg">{name}</code>'
    primary = ", ".join(p["opts"])
    if p["secondary_opts"]:
        primary = f"{primary} / {', '.join(p['secondary_opts'])}"
    primary = primary.replace("--", "&#x2D;&#x2D;")
    flag_html = f'<code class="cli-flag">{primary}</code>'
    if not p["is_flag"]:
        meta = (p["type"] or "value").upper()
        return f'{flag_html} <code class="cli-value">&lt;{meta}&gt;</code>'
    return flag_html


def _md_param_meta(p: dict[str, Any]) -> str:
    bits: list[str] = []
    if p.get("choices"):
        bits.append("choices: " + ", ".join(f"`{c}`" for c in p["choices"]))
    default = p.get("default")
    skip_default = (
        default in (None, "") or (p["is_flag"] and default is False) or (p["kind"] == "argument" and p["required"])
    )
    if not skip_default:
        bits.append(f"default: `{default}`")
    env = p.get("envvar")
    if env:
        env_str = ", ".join(env) if isinstance(env, list) else env
        bits.append(f"env: `{env_str}`")
    if p.get("multiple"):
        bits.append("repeatable")
    if p.get("required") and p["kind"] == "option":
        bits.append("required")
    return " · ".join(bits)


def _md_param_list(heading: str, params: list[dict[str, Any]], html: bool = False) -> list[str]:
    if not params:
        return []
    sig_fn = _md_param_signature_html if html else _md_param_signature
    out = [f"**{heading}**", ""]
    for p in params:
        sig = sig_fn(p)
        meta = _md_param_meta(p)
        help_ = _md_escape((p.get("help") or "").strip())
        tail = f" — {help_}" if help_ else ""
        if meta:
            tail += f" _({meta})_"
        out.append(f"- {sig}{tail}")
    out.append("")
    return out


def _md_command(node: dict[str, Any], depth: int, html: bool = False) -> list[str]:
    heading = "#" * depth
    lines: list[str] = [f"{heading} `{node['full']}`", ""]
    short = node["short_help"] or ""
    if short:
        lines.extend([short, ""])
    lines.extend([f"```sh\n{node['usage']}\n```", ""])
    body = _md_render_help_block(_strip_short_help_prefix(node["help"], short), lang="sh")
    if body:
        lines.extend([body, ""])
    visible = [p for p in node["params"] if not p["hidden"]]
    args = [p for p in visible if p["kind"] == "argument"]
    opts = [p for p in visible if p["kind"] == "option"]
    lines.extend(_md_param_list("Arguments", args, html=html))
    lines.extend(_md_param_list("Options", opts, html=html))
    epilog = _md_render_help_block(node["epilog"])
    if epilog:
        lines.extend([epilog, ""])
    for sub in node["subcommands"]:
        lines.extend(_md_command(sub, depth + 1, html=html))
    return lines


def render_markdown(tree: dict[str, Any]) -> str:
    """Render the full reference as a single markdown file."""
    out: list[str] = [
        f"# {tree['program']}(1) — Avrea command-line client",
        "",
        (
            f"Reference for `{tree['program']}` v{tree['version']}. "
            "Generated from the source tree — do not edit by hand. "
            "Run `make -C avr-cli docs` to regenerate."
        ),
        "",
        "## Name",
        "",
        f"`{tree['program']}` — {tree['summary']}",
        "",
        "## Synopsis",
        "",
        f"```sh\n{tree['program']} [GLOBAL OPTIONS] COMMAND [ARGS]...\n```",
        "",
    ]
    out.extend(_md_param_list("Global options", tree["global_options"]))
    if tree["aliases"]:
        out.extend(["## Aliases", ""])
        for alias, target in sorted(tree["aliases"].items()):
            out.append(f"- `{alias}` → `{target}`")
        out.append("")
    out.extend(["## Commands", ""])
    for section in tree["section_order"]:
        if section not in tree["sections"]:
            continue
        out.extend([f"### {section}", ""])
        for cmd in tree["sections"][section]:
            out.append(f"- [`{cmd['full']}`](#{_slug(cmd['path'])}) — {cmd['short_help']}")
        out.append("")
    out.extend(["## Reference", ""])
    for _section, cmd in _iter_top_commands(tree):
        out.extend(_md_command(cmd, depth=3))
    return "\n".join(out).rstrip() + "\n"


def render_json(tree: dict[str, Any]) -> str:
    return json.dumps(tree, indent=2, default=str) + "\n"


# ------------------------------------------------------------- starlight --


def _starlight_filename(cmd: dict[str, Any]) -> str:
    # The Click name is what the user types (e.g. ``audit-events``); use it
    # verbatim so the URL matches the command literal.
    return f"{cmd['name']}.md"


def _starlight_command_page(cmd: dict[str, Any]) -> str:
    """Render one top-level command as a Starlight page.

    The frontmatter ``title`` becomes the page H1, so the body starts at
    H2. For groups, each subcommand is an H2 section anchored at its full
    command path (``avr-run-list``) — Starlight auto-generates anchors
    from heading text, and ``avr run list`` slugifies to that anchor.
    """
    short = (cmd["short_help"] or "").strip()
    description = short.replace('"', '\\"')
    out: list[str] = [
        "---",
        f"title: {cmd['full']}",
        f'description: "{description}"',
        "---",
        "",
    ]
    if short:
        out.extend([short, ""])
    out.extend([f"```sh\n{cmd['usage']}\n```", ""])
    body = _md_render_help_block(_strip_short_help_prefix(cmd["help"], short), lang="sh")
    if body:
        out.extend([body, ""])
    visible = [p for p in cmd["params"] if not p["hidden"]]
    args = [p for p in visible if p["kind"] == "argument"]
    opts = [p for p in visible if p["kind"] == "option"]
    out.extend(_md_param_list("Arguments", args, html=True))
    out.extend(_md_param_list("Options", opts, html=True))
    epilog = _md_render_help_block(cmd["epilog"])
    if epilog:
        out.extend([epilog, ""])
    if cmd["subcommands"]:
        out.extend(["## Subcommands", ""])
        for sub in cmd["subcommands"]:
            out.extend(_md_command(sub, depth=3, html=True))
    return "\n".join(out).rstrip() + "\n"


def _starlight_index_page(tree: dict[str, Any]) -> str:
    out: list[str] = [
        "---",
        "title: CLI Reference",
        f'description: "Reference for the avr command-line client (v{tree["version"]})."',
        "---",
        "",
        f"`{tree['program']}` is the Avrea command-line client. {tree['summary']}",
        "",
        "## Synopsis",
        "",
        f"```sh\n{tree['program']} [GLOBAL OPTIONS] COMMAND [ARGS]...\n```",
        "",
    ]
    out.extend(_md_param_list("Global options", tree["global_options"], html=True))
    out.extend(["## Commands", ""])
    for section in tree["section_order"]:
        if section not in tree["sections"]:
            continue
        out.extend([f"### {section}", ""])
        for cmd in tree["sections"][section]:
            out.append(f"- [`{cmd['full']}`](./{cmd['name']}/) — {cmd['short_help']}")
        out.append("")
    if tree["aliases"]:
        out.extend(["## Aliases", ""])
        for alias, target in sorted(tree["aliases"].items()):
            out.append(f"- `{alias}` → [`{target}`](./{target}/)")
        out.append("")
    return "\n".join(out).rstrip() + "\n"


def _starlight_overview_from_readme(readme_text: str, tree: dict[str, Any]) -> str:
    """Convert avr-cli's README into a Starlight overview page.

    Strips the leading H1 (Starlight's frontmatter ``title`` becomes the
    page H1) and prepends Starlight frontmatter. Everything else passes
    through verbatim — the README is the curated, human-edited landing.
    """
    lines = readme_text.lstrip().splitlines()
    if lines and lines[0].startswith("# "):
        lines = lines[1:]
        while lines and lines[0].strip() == "":
            lines.pop(0)
    body = "\n".join(lines).rstrip() + "\n"
    summary = (tree["summary"] or "Avrea command-line client.").replace('"', '\\"')
    frontmatter = f'---\ntitle: Avrea CLI\ndescription: "{summary}"\n---\n\n'
    return frontmatter + body


def _starlight_sidebar_module(tree: dict[str, Any], has_overview: bool) -> str:
    """Emit an ESM module exporting the CLI sidebar block for ``astro.config.mjs``.

    The sidebar mirrors the section grouping used by ``avr --help`` (Core
    Commands / Setup & Config / Additional Commands) so the in-CLI and
    docs-site mental models stay aligned. Filename is prefixed with ``_``
    so Astro's content collection ignores it.
    """

    def js(value: str) -> str:
        return json.dumps(value)

    inner: list[str] = ["    { label: 'All Commands', slug: 'cli/reference' },"]
    for section in tree["section_order"]:
        section_cmds = tree["sections"].get(section, [])
        if not section_cmds:
            continue
        inner.append("    {")
        inner.append(f"      label: {js(section)},")
        inner.append("      collapsed: false,")
        inner.append("      items: [")
        for cmd in section_cmds:
            label = js(cmd["full"])
            slug = js(f"cli/reference/{cmd['name']}")
            inner.append(f"        {{ label: {label}, slug: {slug} }},")
        inner.append("      ],")
        inner.append("    },")

    overview_line = "    { label: 'Overview', slug: 'cli' },\n" if has_overview else ""
    body = "\n".join(inner)
    return (
        "// Auto-generated from the avr Click tree by `avr internal docs`.\n"
        "// Do not edit by hand — regenerate via `make -C avr-cli docs`.\n"
        "\n"
        "export default {\n"
        "  label: 'CLI',\n"
        "  items: [\n"
        f"{overview_line}"
        f"{body}\n"
        "  ],\n"
        "};\n"
    )


def render_starlight(tree: dict[str, Any], readme_text: str | None = None) -> dict[str, str]:
    """Return a mapping of filename → content for a Starlight content dir.

    Layout:
      ``index.md``            — curated overview (sourced from README.md).
      ``reference/index.md``  — auto-generated reference landing.
      ``reference/<cmd>.md``  — one file per top-level command.
      ``_cli-sidebar.mjs``    — sidebar config for ``astro.config.mjs``.
    """
    files: dict[str, str] = {}
    has_overview = readme_text is not None
    if readme_text:
        files["index.md"] = _starlight_overview_from_readme(readme_text, tree)
    files["reference/index.md"] = _starlight_index_page(tree)
    for _section, cmd in _iter_top_commands(tree):
        files[f"reference/{_starlight_filename(cmd)}"] = _starlight_command_page(cmd)
    files["_cli-sidebar.mjs"] = _starlight_sidebar_module(tree, has_overview)
    return files


# ------------------------------------------------------------------- man --


def _troff_escape(text: str) -> str:
    """Minimal troff escaping safe for body text."""
    if not text:
        return ""
    text = text.replace("\\", "\\e")
    # Lines that begin with ``.`` or ``'`` are interpreted as troff
    # requests; prefix with ``\&`` to neutralize. Apply line-by-line.
    out_lines: list[str] = []
    for line in text.splitlines():
        if line[:1] in (".", "'"):
            out_lines.append("\\&" + line)
        else:
            out_lines.append(line)
    return "\n".join(out_lines)


def _troff_flag(opt: str) -> str:
    """Bold a flag in troff and freeze hyphens so they aren't broken."""
    return "\\fB" + opt.replace("-", "\\-") + "\\fR"


def _man_param_signature(p: dict[str, Any]) -> str:
    if p["kind"] == "argument":
        name = (p["name"] or "").upper()
        if p["nargs"] == -1:
            name += "..."
        return f"\\fI{name}\\fR" if p["required"] else f"[\\fI{name}\\fR]"
    parts = [_troff_flag(o) for o in p["opts"]]
    primary = ", ".join(parts)
    if p["secondary_opts"]:
        primary = primary + " / " + ", ".join(_troff_flag(o) for o in p["secondary_opts"])
    if not p["is_flag"]:
        meta = (p["type"] or "value").upper()
        primary = f"{primary} \\fI{meta}\\fR"
    return primary


def _man_help_block(text: str | None) -> list[str]:
    """Render a help/epilog body as troff lines."""
    if not text:
        return []
    out: list[str] = []
    lines = text.splitlines()
    i = 0
    paragraph: list[str] = []

    def _flush_paragraph():
        if paragraph:
            joined = " ".join(paragraph).strip()
            if joined:
                out.append(_troff_escape(joined))
                out.append(".PP")
            paragraph.clear()

    while i < len(lines):
        line = lines[i]
        stripped = line.strip()
        if stripped == "\b":
            _flush_paragraph()
            i += 1
            block: list[str] = []
            while i < len(lines) and lines[i].strip() != "":
                block.append(lines[i])
                i += 1
            if block:
                indent = min((len(b) - len(b.lstrip(" ")) for b in block if b.strip()), default=0)
                cleaned = [b[indent:] if len(b) >= indent else b for b in block]
                out.append(".RS 4")
                out.append(".nf")
                out.extend(_troff_escape("\n".join(cleaned)).splitlines())
                out.append(".fi")
                out.append(".RE")
                out.append(".PP")
            continue
        if stripped == "":
            _flush_paragraph()
        else:
            paragraph.append(stripped)
        i += 1
    _flush_paragraph()
    if out and out[-1] == ".PP":
        out.pop()
    return out


def _man_param_section(heading: str, params: list[dict[str, Any]]) -> list[str]:
    if not params:
        return []
    out = [f".SH {heading}"]
    for p in params:
        out.append(".TP")
        sig = _man_param_signature(p)
        out.append(sig)
        body_bits: list[str] = []
        help_ = (p.get("help") or "").strip()
        if help_:
            body_bits.append(_troff_escape(help_))
        meta_bits: list[str] = []
        if p.get("choices"):
            meta_bits.append("choices: " + ", ".join(p["choices"]))
        default = p.get("default")
        skip_default = (
            default in (None, "") or (p["is_flag"] and default is False) or (p["kind"] == "argument" and p["required"])
        )
        if not skip_default:
            meta_bits.append(f"default: {default}")
        env = p.get("envvar")
        if env:
            env_str = ", ".join(env) if isinstance(env, list) else env
            meta_bits.append(f"env: {env_str}")
        if p.get("multiple"):
            meta_bits.append("repeatable")
        if p.get("required") and p["kind"] == "option":
            meta_bits.append("required")
        if meta_bits:
            body_bits.append("(" + "; ".join(meta_bits) + ")")
        out.append(" ".join(body_bits) if body_bits else "\\&")
    return out


def _man_filename(path: list[str]) -> str:
    return "-".join(path) + ".1"


def _man_title(path: list[str]) -> str:
    return "-".join(path).upper().replace("-", "\\-")


def _flatten_commands(node: dict[str, Any]) -> list[dict[str, Any]]:
    out = [node]
    for sub in node["subcommands"]:
        out.extend(_flatten_commands(sub))
    return out


def _man_see_also(node: dict[str, Any], all_paths: list[list[str]]) -> list[list[str]]:
    """Return paths for the SEE ALSO section.

    Convention: link the root, the parent, and siblings (for non-root pages),
    plus immediate children (for groups). Avoids dumping the entire command
    tree on every page.
    """
    refs: list[list[str]] = []
    path = node["path"]
    root = [PROG]
    parent = path[:-1] if len(path) > 1 else []
    if path != root:
        refs.append(root)
    if parent and parent != root:
        refs.append(parent)
    for p in all_paths:
        if len(p) == len(path) and p[:-1] == path[:-1] and p != path:
            refs.append(p)
    for p in all_paths:
        if len(p) == len(path) + 1 and p[:-1] == path:
            refs.append(p)
    seen: set[tuple[str, ...]] = set()
    out: list[list[str]] = []
    for r in refs:
        key = tuple(r)
        if key in seen:
            continue
        seen.add(key)
        out.append(r)
    return out


def _man_page(node: dict[str, Any], tree: dict[str, Any], all_paths: list[list[str]]) -> str:
    title = _man_title(node["path"])
    short = (node["short_help"] or "").strip()
    body = _strip_short_help_prefix(node["help"], short)
    lines: list[str] = [
        f'.TH {title} 1 "" "avr {tree["version"]}" "Avrea CLI"',
        ".SH NAME",
        f"{_troff_escape('-'.join(node['path']))} \\- {_troff_escape(short or node['name'])}",
        ".SH SYNOPSIS",
        f".B {' '.join(node['path'])}",
    ]
    # Trailing usage pieces (e.g., ``[OPTIONS]``, ``COMMAND``, ``[ARGS]...``).
    usage_tail = node["usage"][len(" ".join(node["path"])) :].strip()
    if usage_tail:
        lines.append(_troff_escape(usage_tail))
    lines.append(".SH DESCRIPTION")
    if short:
        lines.append(_troff_escape(short))
        lines.append(".PP")
    lines.extend(_man_help_block(body))
    visible = [p for p in node["params"] if not p["hidden"]]
    args = [p for p in visible if p["kind"] == "argument"]
    opts = [p for p in visible if p["kind"] == "option"]
    lines.extend(_man_param_section("ARGUMENTS", args))
    lines.extend(_man_param_section("OPTIONS", opts))
    epilog_lines = _man_help_block(node["epilog"])
    if epilog_lines:
        lines.append(".SH NOTES")
        lines.extend(epilog_lines)
    if node["subcommands"]:
        lines.append(".SH COMMANDS")
        for sub in node["subcommands"]:
            lines.append(".TP")
            lines.append(_troff_escape(sub["name"]))
            lines.append(_troff_escape(sub["short_help"] or ""))
    refs = _man_see_also(node, all_paths)
    if refs:
        lines.append(".SH SEE ALSO")
        lines.append(",\n".join(f".BR {'-'.join(p)} (1)" for p in refs))
    return "\n".join(lines).rstrip() + "\n"


def render_man(tree: dict[str, Any]) -> dict[str, str]:
    """Return a mapping of filename → troff content for every command page."""
    # Top-level avr.1 page: synthesize a node from the tree summary.
    root_node: dict[str, Any] = {
        "name": "avr",
        "path": [PROG],
        "full": PROG,
        "is_group": True,
        "short_help": tree["summary"] or "Avrea command-line client.",
        "help": tree["summary"],
        "epilog": None,
        "deprecated": False,
        "hidden": False,
        "usage": f"{PROG} [OPTIONS] COMMAND [ARGS]...",
        "params": tree["global_options"],
        "subcommands": [cmd for _section, cmd in _iter_top_commands(tree)],
    }
    all_nodes = _flatten_commands(root_node)
    all_paths = [n["path"] for n in all_nodes]
    files: dict[str, str] = {}
    for node in all_nodes:
        files[_man_filename(node["path"])] = _man_page(node, tree, all_paths)
    return files


# ----------------------------------------------------------- cli command --


_FORMATS = ("markdown", "json", "starlight", "man", "all")


def _write_file(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def _write_dir(directory: Path, files: dict[str, str]) -> None:
    """Write ``files`` into ``directory``, removing stale ``.md``/``.1`` files.

    ``files`` keys may include a relative subpath (``reference/index.md``);
    nested directories are created on demand. Stale-page cleanup walks the
    full tree so a renamed command doesn't leave an orphan behind.
    """
    directory.mkdir(parents=True, exist_ok=True)
    existing: set[Path] = {
        p.relative_to(directory) for p in directory.rglob("*") if p.is_file() and p.suffix in (".md", ".1")
    }
    target = {Path(name) for name in files}
    for stale in existing - target:
        (directory / stale).unlink()
    for name, content in files.items():
        path = directory / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")


@click.command("docs")
@click.option(
    "--format",
    "fmt",
    type=click.Choice(_FORMATS),
    default="all",
    show_default=True,
    help="Output format.",
)
@click.option(
    "--out",
    "out",
    type=click.Path(file_okay=True, dir_okay=True, path_type=Path),
    default=None,
    help=(
        "Output target. For 'markdown'/'json' it's a file path (or directory; "
        "the standard filename is used). For 'starlight'/'man' it's a directory. "
        "For 'all' it's a base directory (writes docs/ and man/ subtrees)."
    ),
)
@click.option(
    "--stdout",
    "to_stdout",
    is_flag=True,
    help="Write to stdout (only valid for single-file formats: markdown, json).",
)
@click.option(
    "--readme",
    "readme",
    type=click.Path(dir_okay=False, path_type=Path),
    default=None,
    help=(
        "README to embed as the Starlight overview page (default: avr-cli/README.md "
        "alongside this package; omitted if not found)."
    ),
)
def docs_command(fmt: str, out: Path | None, to_stdout: bool, readme: Path | None) -> None:
    """Generate avr reference docs from the live Click tree."""
    if to_stdout and fmt not in ("markdown", "json"):
        raise click.UsageError("--stdout is only valid with --format markdown or --format json")
    if not to_stdout and out is None:
        raise click.UsageError("--out is required (or pass --stdout for markdown/json)")

    tree = build_tree()
    readme_text: str | None = None
    if fmt in ("starlight", "all"):
        readme_path = readme or (Path(__file__).resolve().parent.parent / "README.md")
        if readme_path.exists():
            readme_text = readme_path.read_text(encoding="utf-8")

    if to_stdout:
        if fmt == "markdown":
            click.echo(render_markdown(tree), nl=False)
        else:
            click.echo(render_json(tree), nl=False)
        return

    assert out is not None  # narrowed by the UsageError guard above

    if fmt == "markdown":
        target = out / "REFERENCE.md" if out.is_dir() else out
        _write_file(target, render_markdown(tree))
        click.echo(f"wrote {target}")
        return

    if fmt == "json":
        target = out / "reference.json" if out.is_dir() else out
        _write_file(target, render_json(tree))
        click.echo(f"wrote {target}")
        return

    if fmt == "starlight":
        files = render_starlight(tree, readme_text)
        _write_dir(out, files)
        click.echo(f"wrote {len(files)} files to {out}")
        return

    if fmt == "man":
        files = render_man(tree)
        # ``man -M`` and Homebrew both expect ``man1/`` under the man root.
        _write_dir(out / "man1", files)
        click.echo(f"wrote {len(files)} files to {out / 'man1'}")
        return

    if fmt == "all":
        _write_file(out / "docs" / "REFERENCE.md", render_markdown(tree))
        _write_file(out / "docs" / "reference.json", render_json(tree))
        _write_dir(out / "docs" / "starlight", render_starlight(tree, readme_text))
        _write_dir(out / "man" / "man1", render_man(tree))
        click.echo(f"wrote markdown, json, starlight, man under {out}")
        return


def main() -> None:
    docs_command()


if __name__ == "__main__":
    main()
