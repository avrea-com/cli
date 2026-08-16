"""Unit tests for the reference-docs generator's Markdown escaping."""

from avrea_cli.docs_gen import _md_escape
from avrea_cli.docs_gen import _md_render_help_block


class TestMdEscape:
    def test_bare_placeholder_is_escaped(self):
        # Outside code, a <placeholder> would be parsed as an HTML/JSX tag and
        # dropped, so it must be entity-escaped to render literally.
        assert _md_escape("a raw cache.<name>.enabled key") == "a raw cache.&lt;name&gt;.enabled key"
        assert _md_escape("default: avr-<vm-id>") == "default: avr-&lt;vm-id&gt;"

    def test_inline_code_span_keeps_literal_angle_brackets(self):
        # Inside backticks, <vm-id> already renders literally; escaping it would
        # surface a raw "&lt;". The code span must be left untouched.
        assert _md_escape("Use `avr-<vm-id>`") == "Use `avr-<vm-id>`"
        assert _md_escape("Run the `-- <cmd>` in a login shell") == "Run the `-- <cmd>` in a login shell"

    def test_mixed_prose_and_code_span(self):
        # Prose escaped, code span preserved, in the same string.
        assert _md_escape("A `-- <cmd>` maps <local> -> <VM>") == "A `-- <cmd>` maps &lt;local&gt; -&gt; &lt;VM&gt;"

    def test_multiple_code_spans_preserved(self):
        text = "one `a <x>` two `b <y>` three <z>"
        assert _md_escape(text) == "one `a <x>` two `b <y>` three &lt;z&gt;"

    def test_no_angle_brackets_is_identity(self):
        assert _md_escape("plain prose with `code`") == "plain prose with `code`"


class TestMdRenderHelpBlock:
    def test_indented_shell_example_stays_literal(self):
        # A 4-space-indented example is a Markdown code block; its >> and <...>
        # placeholders must not be entity-escaped or the code would render a
        # literal "&gt;&gt;" / "&lt;local&gt;".
        text = "Redirect it yourself:\n\n    ssh -L <local>:x cvm-abc123 >> ~/.ssh/config\n"
        out = _md_render_help_block(text, lang="sh")
        assert "    ssh -L <local>:x cvm-abc123 >> ~/.ssh/config" in out
        assert "&gt;" not in out
        assert "&lt;" not in out

    def test_prose_placeholder_still_escaped(self):
        text = "opens 127.0.0.1:<local> -> <VM>:<guest> through the endpoint"
        out = _md_render_help_block(text)
        assert "127.0.0.1:&lt;local&gt; -&gt; &lt;VM&gt;:&lt;guest&gt;" in out
