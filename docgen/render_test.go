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
	if !strings.Contains(block, "Redirect it yourself:\n\n```sh\nssh -L <local>:x VM >> ~/.ssh/config\n```") {
		t.Fatal(block)
	}
	fenced := helpBlock("Example:\n\n\b\n    avr vm ssh VM -- echo '<hello>'\n", "sh")
	if !strings.Contains(fenced, "```sh\navr vm ssh VM -- echo '<hello>'\n```") {
		t.Fatal(fenced)
	}
}
func TestMarkdownRendersUnwrappedProseAsText(t *testing.T) {
	got := helpBlock("Create a VM.\n\n\b\nFor example, as a <hook>:\n    exec avr vm create --org <org-id>\n\n\b\nExamples:\n    avr vm list\n      --json id\n", "sh")
	want := "Create a VM.\n\n" +
		"For example, as a &lt;hook&gt;:\n\n```sh\nexec avr vm create --org <org-id>\n```\n\n" +
		"**Examples**\n\n```sh\navr vm list\n  --json id\n```"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
func TestMarkdownRendersFieldListsAndDefinitionsAsText(t *testing.T) {
	got := helpBlock("\b\nJSON FIELDS (GitHub)\n    created_at, id,\n    name\n\n\b\nTopics:\n  test      Push a <change>\n  vms       On-demand VMs\n", "sh")
	want := "**JSON FIELDS (GitHub)**\n\n`created_at`, `id`, `name`\n\n" +
		"**Topics**\n\n- `test`: Push a &lt;change&gt;\n- `vms`: On-demand VMs"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
func TestMarkdownSeparatesIndentedRunsFromProse(t *testing.T) {
	for _, row := range [][2]string{
		// A list after prose needs a blank line or it joins the paragraph.
		{"\b\nTopics:\n  test  Run tests\nAfter the list.\n", "**Topics**\n\n- `test`: Run tests\n\nAfter the list."},
		// A blank line inside an example keeps one block and its indentation.
		{"Config:\n\n    jobs:\n      a: 1\n\n      b: 2\n\nDone.\n", "Config:\n\n```sh\njobs:\n  a: 1\n\n  b: 2\n```\n\nDone."},
		// A wrapped continuation of a bullet stays prose.
		{"- Pass --wait to block until the VM\n    is RUNNING.\n", "- Pass --wait to block until the VM\n    is RUNNING."},
		// Tabs indent the same way as spaces.
		{"Run:\n\n\tavr vm list\n", "Run:\n\n```sh\navr vm list\n```"},
	} {
		if got := helpBlock(row[0], "sh"); got != row[1] {
			t.Fatalf("%q:\ngot:\n%s\nwant:\n%s", row[0], got, row[1])
		}
	}
}
func TestMarkdownKeepsCommandsThatResembleListsAsCode(t *testing.T) {
	for _, row := range [][2]string{
		{"\b\nExamples:\n    make\n", "**Examples**\n\n```sh\nmake\n```"},
		{"\b\nExamples:\n    ls    # list files\n", "**Examples**\n\n```sh\nls    # list files\n```"},
		{"Fields:\n\n    id, name\n", "Fields:\n\n```sh\nid, name\n```"},
	} {
		if got := helpBlock(row[0], "sh"); got != row[1] {
			t.Fatalf("%q:\ngot:\n%s\nwant:\n%s", row[0], got, row[1])
		}
	}
}
func TestManKeepsIndentedExamplesOutsideUnwrappedParagraphs(t *testing.T) {
	body := strings.Join(manHelp("Examples:\n\n    avr vm list\n    avr vm show VM\n\nDone.\n"), "\n")
	if !strings.Contains(body, ".nf\navr vm list\navr vm show VM\n.fi") {
		t.Fatal(body)
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
