package docgen_test

import (
	"strings"
	"testing"

	"github.com/avrea-com/cli/commands"
)

func TestFeedbackDocumentationUsesLiveCommand(t *testing.T) {
	tree, err := commands.Documentation(commands.NewRoot(commands.Dependencies{}))
	if err != nil {
		t.Fatal(err)
	}
	node := findNode(tree, "feedback")
	if node == nil || !strings.Contains(node.Usage, "[OPTIONS] MESSAGE | status FEEDBACK_ID") || len(node.Subcommands) != 1 || node.Subcommands[0].Name != "status" {
		t.Fatal(node)
	}
	wanted := map[string]bool{"bug": false, "org": false, "idempotency_key": false, "json": false, "jq": false}
	for _, param := range node.Params {
		if _, exists := wanted[param.Name]; exists {
			wanted[param.Name] = true
		}
	}
	for name, present := range wanted {
		if !present {
			t.Error("missing feedback option", name)
		}
	}
}
