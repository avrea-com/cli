package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestGlobalLinkOrderAndEnvironmentValidation(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		env     string
		enabled bool
		code    int
	}{
		{[]string{"--links", "--no-links"}, "", false, 0},
		{[]string{"--no-links", "--links"}, "", true, 0},
		{[]string{"--no-links", "--links", "--no-links"}, "", false, 0},
		{nil, "false", false, 0},
		{nil, " ", false, 0},
		{nil, "invalid", false, 2},
		{[]string{"--links"}, "invalid", true, 0},
		{[]string{"--no-links"}, "invalid", false, 0},
	} {
		d, _, stderr := testDeps(t)
		d.Getenv = func(key string) string {
			if key == "AVR_LINKS" {
				return tc.env
			}
			return ""
		}
		d.IsTerminal = func() bool { return true }
		root := NewRoot(d)
		calls := 0
		root.AddCommand(&cobra.Command{Use: "probe", RunE: func(c *cobra.Command, _ []string) error {
			calls++
			if linksEnabled(c, d) != tc.enabled {
				t.Fatal(tc, "wrong final link setting")
			}
			return nil
		}})
		if code := ExecuteRoot(t.Context(), root, append([]string{"probe"}, tc.args...), stderr); code != tc.code {
			t.Fatal(tc, code, stderr)
		}
		if tc.code != 0 && calls != 0 {
			t.Fatal("handler ran with invalid global options")
		}
	}
}

func TestDebugWarningIsSortedAndEagerVersionIsOffline(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.Getenv = func(key string) string {
		if key == "AVR_DEBUG" {
			return "zeta,api,alpha,zeta"
		}
		return ""
	}
	if code := Execute(t.Context(), []string{"auth", "status", "--json", "?"}, d); code != 0 {
		t.Fatal(code, stderr)
	}
	if stderr.String() != "Warning: AVR_DEBUG ignored unknown categories: alpha, zeta. Known: api.\n" {
		t.Fatal(stderr)
	}
	out.Reset()
	stderr.Reset()
	if code := Execute(t.Context(), []string{"--version"}, d); code != 0 || stderr.Len() != 0 || !strings.Contains(out.String(), "avr version") {
		t.Fatal(code, out, stderr)
	}
}

func TestVMSSHGlobalFlagsAreParsedBeforeEnvironmentValidation(t *testing.T) {
	for _, flags := range [][]string{{"--links"}, {"--no-links"}, {}} {
		d, out, stderr := testDeps(t)
		d.Getenv = func(key string) string {
			if key == "AVR_LINKS" {
				return "invalid"
			}
			return ""
		}
		args := append([]string{"vm", "ssh"}, flags...)
		args = append(args, "--help")
		code := Execute(t.Context(), args, d)
		if len(flags) == 0 {
			if code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "--links") {
				t.Fatal(code, out, stderr)
			}
		} else if code != 0 || stderr.Len() != 0 || !strings.Contains(out.String(), "Usage:") {
			t.Fatal(code, out, stderr)
		}
	}
}
