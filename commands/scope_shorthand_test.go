package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestScopeShorthandsAcrossCommandTree(t *testing.T) {
	root := NewRoot(Dependencies{})
	var visit func(*cobra.Command)
	visit = func(c *cobra.Command) {
		for name, shorthand := range map[string]string{"org": "o", "repo": "R"} {
			f := c.LocalFlags().Lookup(name)
			if f == nil {
				continue
			}
			t.Run(c.CommandPath()+"/"+name, func(t *testing.T) {
				option := "-" + shorthand
				if c.CommandPath() == "avr vm ssh" && name == "org" {
					option = "--org"
					if f.Shorthand != "" {
						t.Fatal("SSH must preserve remote -o")
					}
				}
				args := []string{option, "acme/web"}
				if f.Value.Type() == "stringArray" {
					args = append(args, "-"+shorthand, "acme/worker")
				}
				var err error
				if c.DisableFlagParsing {
					_, err = vmSSHPassthrough(c, args)
				} else {
					err = c.ParseFlags(args)
				}
				if err != nil {
					t.Fatal(err)
				}
				if !f.Changed {
					t.Fatal("shorthand did not mark canonical flag changed")
				}
				if f.Value.Type() == "stringArray" {
					got, err := c.Flags().GetStringArray(name)
					if err != nil || len(got) != 2 || got[0] != "acme/web" || got[1] != "acme/worker" {
						t.Fatal(got, err)
					}
				} else if f.Value.String() != "acme/web" {
					t.Fatal(f.Value.String())
				}
			})
		}
		for _, child := range c.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func TestVMSSHScopeShorthandPreservesExplicitPassthrough(t *testing.T) {
	c := newVMSSH(Dependencies{}.defaults())
	remote, err := vmSSHPassthrough(c, []string{"--org", "acme", "vm-example", "--", "-o", "BatchMode=yes"})
	org, _ := c.Flags().GetString("org")
	if err != nil || org != "acme" || len(remote) != 3 || remote[1] != "-o" || remote[2] != "BatchMode=yes" {
		t.Fatal(org, remote, err)
	}
}

func TestVMSSHRemoteOptions(t *testing.T) {
	for _, args := range [][]string{{"vm-example", "ps", "-o", "pid,comm"}, {"vm-example", "ps", "-eo", "pid"}} {
		c := newVMSSH(Dependencies{}.defaults())
		remote, err := vmSSHPassthrough(c, args)
		if err != nil || strings.Join(remote, "|") != strings.Join(args, "|") || c.Flags().Changed("org") {
			t.Fatal(remote, err)
		}
	}
}
