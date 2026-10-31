package commands

import (
	"strings"
	"testing"
)

func TestSearchLogContentPreservesTextAndColorsWithoutTerminalCommands(t *testing.T) {
	d, _, _ := testDeps(t)
	d.IsTerminal = func() bool { return true }
	d = d.defaults()
	raw := "first\n\t\x1b[31mred\x1b[0m\x1b]52;c;secret\x07\r"
	got := searchLogContent(d, raw)
	if !strings.Contains(got, "first\n\t\x1b[31mred\x1b[0m") || strings.Contains(got, "\x1b]52") || !strings.Contains(got, "\\x1b]52;c;secret\\x07\\r") {
		t.Fatal(got)
	}
	d.IsTerminal = func() bool { return false }
	got = searchLogContent(d, raw)
	if strings.Contains(got, "\x1b") || !strings.Contains(got, "first\n\tred") {
		t.Fatal(got)
	}
}
