// Command command_inventory exports the live Go CLI surface for migration audits.
package main

import (
	"encoding/json"
	"os"

	"github.com/avrea-com/cli/commands"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type flag struct {
	Name         string `json:"name"`
	Shorthand    string `json:"shorthand,omitempty"`
	Type         string `json:"type"`
	Default      string `json:"default"`
	NoOptDefault string `json:"no_opt_default"`
	Hidden       bool   `json:"hidden"`
	Required     bool   `json:"required"`
}
type command struct {
	Path     string    `json:"path"`
	Use      string    `json:"use"`
	Hidden   bool      `json:"hidden"`
	Aliases  []string  `json:"aliases"`
	Group    bool      `json:"group"`
	Flags    []flag    `json:"flags"`
	Commands []command `json:"commands,omitempty"`
}

func walk(c *cobra.Command) command {
	c.InitDefaultHelpFlag()
	node := command{Path: c.CommandPath(), Use: c.Use, Hidden: c.Hidden, Aliases: c.Aliases, Group: c.HasSubCommands(), Flags: []flag{}}
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		node.Flags = append(node.Flags, flag{Name: f.Name, Shorthand: f.Shorthand, Type: f.Value.Type(), Default: f.DefValue, NoOptDefault: f.NoOptDefVal, Hidden: f.Hidden, Required: len(f.Annotations[cobra.BashCompOneRequiredFlag]) > 0})
	})
	for _, child := range c.Commands() {
		node.Commands = append(node.Commands, walk(child))
	}
	return node
}
func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(walk(commands.NewRoot(commands.Dependencies{Getenv: func(string) string { return "" }}))); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
