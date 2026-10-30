package commands

import (
	"encoding/json"
	"os"
	"testing"
)

// The oracle inventory supplies aliases independently of the Go constructors.
func TestPythonCommandAliasesResolve(t *testing.T) {
	data, err := os.ReadFile("../docs/go-python-command-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Aliases map[string]string `json:"aliases"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	for alias, target := range contract.Aliases {
		t.Run(alias, func(t *testing.T) {
			root := NewRoot(Dependencies{})
			command, rest, err := root.Find([]string{alias})
			if err != nil || len(rest) != 0 || command.Name() != target {
				t.Fatalf("%s resolved to %s (%v), remaining %v; want %s", alias, command.Name(), err, rest, target)
			}
		})
	}
}
