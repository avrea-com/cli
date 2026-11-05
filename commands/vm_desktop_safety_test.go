package commands

import (
	"strings"
	"testing"
)

func TestDesktopInstructionsRejectUntrustedShellSyntax(t *testing.T) {
	for _, platform := range []string{"linux", "darwin", "windows"} {
		for _, pair := range [][2]string{{"127.0.0.1:3389", "runner;touch marker"}, {"$(touch marker):3389", "runner"}, {"127.0.0.1:3389", "runner&password=s:injected"}} {
			lines := vmRDPConnect(pair[0], pair[1], "", platform, false, "")
			if len(lines) != 1 || !strings.Contains(lines[0], "Invalid remote desktop") || strings.Contains(lines[0], "marker") {
				t.Fatal(platform, lines)
			}
		}
	}
	// Even direct URI construction encodes parameters independently.
	if uri := vmRDPURI("127.0.0.1:3389", "runner&password=s:injected"); strings.Count(uri, "&") != 1 || !strings.Contains(uri, "%26") {
		t.Fatal(uri)
	}
}
