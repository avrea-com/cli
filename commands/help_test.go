package commands

import (
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
)

func TestHelpReflectsComposedRegistryWithoutLoadingCredentials(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) { t.Fatal("help loaded credentials"); return config.Config{}, nil }
	root := NewRoot(d)
	repo, _, _ := root.Find([]string{"repo"})
	root.RemoveCommand(repo)
	root.AddCommand(&cobra.Command{Use: "extension", Short: "Inspect the extension."})
	root.AddCommand(&cobra.Command{Use: "secret-extension", Short: "Hidden extension.", Hidden: true})
	root.Flags().String("extra", "", "Extra composition setting.")
	if code := ExecuteRoot(t.Context(), root, []string{"--help"}, stderr); code != 0 {
		t.Fatal(code, stderr)
	}
	got := out.String()
	for _, want := range []string{"extension:", "Inspect the extension.", "--extra STRING", "Extra composition setting."} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in help:\n%s", want, got)
		}
	}
	for _, absent := range []string{"repo:", "secret-extension", "completion:"} {
		if strings.Contains(got, absent) {
			t.Fatalf("unexpected %q in help:\n%s", absent, got)
		}
	}
	out.Reset()
	second := NewRoot(d)
	if code := ExecuteRoot(t.Context(), second, []string{"--help"}, stderr); code != 0 {
		t.Fatal(code, stderr)
	}
	if !strings.Contains(out.String(), "repo:") || strings.Contains(out.String(), "extension:") {
		t.Fatal("composition leaked to another root", out.String())
	}
}

func TestBareRootAndGroupUseDifferentExitAndOutputContracts(t *testing.T) {
	for _, token := range []string{"", "fixture"} {
		t.Run(token, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) { return config.Config{Token: token}, nil }
			if code := Execute(t.Context(), nil, d); code != 0 || stderr.Len() != 0 {
				t.Fatal(code, out, stderr)
			}
			if token == "" && !strings.Contains(out.String(), "Welcome to Avrea CLI.") {
				t.Fatal("missing unauthenticated welcome", out)
			}
			if token != "" && !strings.Contains(out.String(), "CORE COMMANDS") {
				t.Fatal("missing authenticated help", out)
			}
		})
	}
	d, out, stderr := testDeps(t)
	if code := Execute(t.Context(), []string{"auth"}, d); code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "USAGE") {
		t.Fatal(code, out, stderr)
	}
}
