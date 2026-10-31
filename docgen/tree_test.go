package docgen_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/avrea-com/cli/commands"
	"github.com/avrea-com/cli/docgen"
	"github.com/spf13/cobra"
)

func findNode(tree *docgen.Tree, name string) *docgen.Node {
	for _, nodes := range tree.Sections {
		for _, node := range nodes {
			if node.Name == name {
				return node
			}
		}
	}
	return nil
}
func TestDocumentationTracksLiveCommandsFlagsDefaultsAndAliases(t *testing.T) {
	root := commands.NewRoot(commands.Dependencies{})
	repo, _, err := root.Find([]string{"repo"})
	if err != nil {
		t.Fatal(err)
	}
	root.RemoveCommand(repo)
	extra := &cobra.Command{Use: "example WIDGET", Short: "Document a new widget.", Aliases: []string{"widgets"}}
	extra.Flags().Int("limit", 7, "Maximum widgets.")
	root.AddCommand(extra, &cobra.Command{Use: "secret", Short: "Hidden extension.", Hidden: true})
	vm, _, err := root.Find([]string{"vm", "list"})
	if err != nil {
		t.Fatal(err)
	}
	vm.Flags().String("new-filter", "all", "A live new filter.")
	vm.Flags().Lookup("limit").DefValue = "7"
	vm.Flags().Lookup("state").DefValue = "RUNNING"
	vm.Flags().StringArray("tag", []string{"a,b", "quoted word"}, "Default tags.")
	vm.Flags().IntSlice("project-id", []int{11, 22}, "Project IDs.")
	tree, err := docgen.Build(root)
	if err != nil {
		t.Fatal(err)
	}
	if findNode(tree, "repo") != nil || findNode(tree, "secret") != nil {
		t.Fatal("removed or hidden command documented")
	}
	if _, ok := tree.Aliases["repos"]; ok {
		t.Fatal("removed command alias documented")
	}
	if tree.Aliases["widgets"] != "example" {
		t.Fatal(tree.Aliases)
	}
	node := findNode(tree, "example")
	if node == nil || node.Usage != "avr example [OPTIONS] WIDGET" || len(node.Params) != 2 {
		t.Fatal(node)
	}
	var listing *docgen.Node
	for _, sub := range findNode(tree, "vm").Subcommands {
		if sub.Name == "list" {
			listing = sub
		}
	}
	if listing == nil {
		t.Fatal("missing VM list")
	}
	found := false
	for _, p := range listing.Params {
		if p.Name == "new_filter" {
			found = true
			if p.Default != "all" || p.Help == nil || *p.Help != "A live new filter." {
				t.Fatal(p)
			}
		}
		if p.Name == "state" && p.Default != "RUNNING" {
			t.Fatal("new default on optional flag omitted", p.Default)
		}
		if p.Name == "tag" && !reflect.DeepEqual(p.Default, []string{"a,b", "quoted word"}) {
			t.Fatal("array default omitted", p.Default)
		}
		if p.Name == "project_id" && (!p.Multiple || p.Type != "integer" || !reflect.DeepEqual(p.Default, []int64{11, 22})) {
			t.Fatalf("integer array documentation lost type/repeatability/default: %#v", p)
		}
		if p.Name == "limit" && p.Default != int64(7) {
			t.Fatal(p.Default)
		}
	}
	if !found {
		t.Fatal("new flag omitted")
	}
	if !strings.Contains(docgen.Markdown(tree), "--new-filter") {
		t.Fatal("live flag missing from Markdown")
	}
	files := docgen.Starlight(tree, nil)
	if _, ok := files["reference/example.md"]; !ok {
		t.Fatal("new Starlight page missing")
	}
	if _, ok := files["reference/repo.md"]; ok {
		t.Fatal("removed Starlight page remains")
	}
	if _, ok := docgen.Man(tree)["avr-example.1"]; !ok {
		t.Fatal("new man page missing")
	}
}
func TestDocumentationDropsRemovedFlags(t *testing.T) {
	root := commands.NewRoot(commands.Dependencies{})
	vm, _, err := root.Find([]string{"vm", "list"})
	if err != nil {
		t.Fatal(err)
	}
	vm.ResetFlags()
	tree, err := docgen.Build(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range findNode(tree, "vm").Subcommands {
		if node.Name == "list" && len(node.Params) != 0 {
			t.Fatal("removed flags retained", node.Params)
		}
	}
}
