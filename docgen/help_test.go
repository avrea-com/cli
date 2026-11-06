package docgen_test

import (
	"github.com/avrea-com/cli/commands"
	"github.com/avrea-com/cli/docgen"
	"github.com/spf13/cobra"
	"reflect"
	"strings"
	"testing"
)

func TestDescriptionsDoNotShareMutableState(t *testing.T) {
	root := commands.NewRoot(commands.Dependencies{})
	want := docgen.Help(root, commands.Description)
	changed := docgen.Help(root, commands.Description)
	changed.Options[0].Names[0] = "changed"
	changed.CompletionParams[0].Names[0] = "changed"
	for _, c := range changed.Commands {
		if c.Section != nil {
			*c.Section = "changed"
		}
	}
	if got := docgen.Help(root, commands.Description); !reflect.DeepEqual(got, want) {
		t.Fatal("description state leaked")
	}
}

func TestOneDescriptionFeedsHelpCompletionAndReference(t *testing.T) {
	root := &cobra.Command{Use: "avr"}
	child := &cobra.Command{Use: "choose", Short: "Choose."}
	child.Flags().String("mode", "one", "fallback")
	root.AddCommand(child)
	describe := func(c *cobra.Command) docgen.Description {
		if c != child {
			return docgen.Description{Help: "Example CLI"}
		}
		return docgen.Description{Help: "Choose a mode.", Short: "Choose.", Parameters: []docgen.Parameter{{
			Param: docgen.Param{Name: "mode", Kind: "option", Opts: []string{"--mode"}, Type: "choice", Nargs: 1,
				Help: func() *string { s := "Shared description"; return &s }(), Choices: []string{"one", "two"}, ShowDefault: true, Default: "one"},
			CompletionKind: "Choice", CaseSensitive: true, Suffix: "  [default: one]",
		}}}
	}
	child.Flags().Lookup("mode").DefValue = "two"
	help := docgen.Help(child, describe)
	if help.Options[0].Label != "--mode [one|two]" || help.Options[0].Description != "Shared description  [default: two]" {
		t.Fatal(help.Options)
	}
	if !reflect.DeepEqual(help.CompletionParams[0].Type.Choices, []string{"one", "two"}) || help.CompletionParams[0].Help != "Shared description" {
		t.Fatal(help.CompletionParams)
	}
	tree, err := docgen.Build(root, describe, []docgen.Section{{Name: "Core", Commands: []string{"choose"}}})
	if err != nil {
		t.Fatal(err)
	}
	p := tree.Sections["Core"][0].Params[0]
	if p.Default != "two" || *p.Help != "Shared description" || !reflect.DeepEqual(p.Choices, []string{"one", "two"}) {
		t.Fatal(p)
	}
	if !strings.Contains(docgen.Markdown(tree), "Shared description") {
		t.Fatal("reference lost description")
	}
}
