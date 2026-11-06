package docgen

import (
	"strings"
	"testing"
)

func TestMarkdownEscapesProseButPreservesCode(t *testing.T) {
	for _, row := range [][2]string{
		{"a raw cache.<name>.enabled key", "a raw cache.&lt;name&gt;.enabled key"},
		{"A `-- <cmd>` maps <local> -> <VM>", "A `-- <cmd>` maps &lt;local&gt; -&gt; &lt;VM&gt;"},
		{"Use ``a ` <cmd>`` and <more>", "Use ``a ` <cmd>`` and &lt;more&gt;"},
	} {
		if got := mdEscape(row[0]); got != row[1] {
			t.Fatalf("%q: got %q, want %q", row[0], got, row[1])
		}
	}
	block := helpBlock("Redirect it yourself:\n\n    ssh -L <local>:x VM >> ~/.ssh/config\n", "sh")
	if !strings.Contains(block, "    ssh -L <local>:x VM >> ~/.ssh/config") {
		t.Fatal(block)
	}
	fenced := helpBlock("Example:\n\n\b\n    avr vm ssh VM -- echo '<hello>'\n", "sh")
	if !strings.Contains(fenced, "```sh\navr vm ssh VM -- echo '<hello>'\n```") {
		t.Fatal(fenced)
	}
}
func TestTroffNeutralizesRequestsAndPreservesLiteralBlocks(t *testing.T) {
	raw := ".danger\n'command\npath\\name"
	if got := troffEscape(raw); got != "\\&.danger\n\\&'command\npath\\ename" {
		t.Fatal(got)
	}
	body := strings.Join(manHelp("Example:\n\n\b\n    .literal\n    next line\n"), "\n")
	if !strings.Contains(body, ".nf\n\\&.literal\nnext line\n.fi") {
		t.Fatal(body)
	}
}
