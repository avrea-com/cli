package commands

import (
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestRequiredOptionDiagnosticsInPublicAndComposedTrees(t *testing.T) {
	for _, name := range []string{"avr", "avr-admin"} {
		for _, tc := range []struct {
			args   []string
			option string
		}{
			{[]string{"firewall", "flow-summaries"}, "repo"},
			{[]string{"firewall", "add"}, "action"},
			{[]string{"firewall", "move", "efr-fixture"}, "to"},
			{[]string{"repo", "mirror", "create", "acme/repo"}, "location"},
		} {
			t.Run(name+" "+strings.Join(tc.args, " "), func(t *testing.T) {
				d, out, stderr := testDeps(t)
				d.LoadConfig = func() (config.Config, error) {
					t.Fatal("required flag validation must not load credentials or call the API")
					return config.Config{}, nil
				}
				root := NewRoot(d)
				root.Use = name
				if code := ExecuteRoot(t.Context(), root, tc.args, stderr, d); code != 2 {
					t.Fatalf("exit %d: %s", code, stderr)
				}
				if out.Len() != 0 {
					t.Fatal("unexpected stdout", out)
				}
				message := stderr.String()
				if !strings.HasPrefix(message, "Usage: "+name+" ") || !strings.Contains(message, "Try '"+name+" ") || !strings.Contains(message, "\n\nError: Missing option '--"+tc.option+"'.") {
					t.Fatal(message)
				}
			})
		}
	}
}
