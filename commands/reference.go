package commands

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/docgen"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var referenceFields = []string{"command", "short_help", "usage", "is_group", "help", "params", "json_fields", "deprecated"}

// NewReference serves the command tree the reference docs are generated
// from, so an agent can learn the CLI offline without scraping help text.
func NewReference(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	var o jsonOptions
	c := &cobra.Command{Use: "reference [COMMAND]...", Short: "List avr's commands, options and JSON fields.", Args: cobra.ArbitraryArgs}
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, referenceFields); done || err != nil {
			return err
		}
		// Defaults describe the public CLI, not this invocation's environment.
		reference := d
		reference.Getenv = func(string) string { return "" }
		reference.LookupEnv = func(string) (string, bool) { return "", false }
		root := NewRoot(reference)
		tree, err := Documentation(root)
		if err != nil {
			return err
		}
		// Arguments name a command as the CLI reads them, aliases included.
		prefix := []string{tree.Program}
		var nodes []*docgen.Node
		if len(args) == 0 {
			nodes = append(nodes, referenceRoot(tree, Description(root).Usage))
		} else if found, rest, err := root.Find(args); err == nil && len(rest) == 0 && found != root {
			prefix = strings.Fields(found.CommandPath())
		} else {
			prefix = append(prefix, args...)
		}
		for _, node := range tree.Commands() {
			if !node.Hidden && len(node.Path) >= len(prefix) && slices.Equal(node.Path[:len(prefix)], prefix) {
				nodes = append(nodes, node)
			}
		}
		if len(nodes) == 0 {
			return &ExitError{2, "Error: No such command: " + output.CellText(strings.Join(prefix, " ")) + ".\n  Hint: Run `avr reference` to list every command."}
		}
		if cmd.Flags().Changed("json") {
			rows := make([]map[string]any, len(nodes))
			for i, node := range nodes {
				if rows[i], err = referenceRow(node); err != nil {
					return err
				}
			}
			if len(args) == 0 {
				markInheritedOptions(rows[0], root)
			}
			return o.list(cmd, d, rows, referenceFields)
		}
		cells := make([][]string, len(nodes))
		for i, node := range nodes {
			cells[i] = []string{node.Full, node.ShortHelp}
		}
		if !d.IsTerminal() {
			return output.Table(d.Out, []string{"command", "short_help"}, cells, true)
		}
		return output.Grid(d.Out, []string{"Command", "Description"}, cells)
	})
	return c
}

// referenceRoot is avr itself, whose row lists avr's own options.
func referenceRoot(tree *docgen.Tree, usage []string) *docgen.Node {
	return &docgen.Node{
		Name: tree.Program, Path: []string{tree.Program}, Full: tree.Program, IsGroup: true, ShortHelp: tree.Summary, Help: &tree.Summary,
		Deprecated: false, Usage: strings.Join(append([]string{tree.Program}, usage...), " "), Params: tree.GlobalOptions,
	}
}

// markInheritedOptions says of each option in avr's own row whether every
// command takes it too; --version, for one, works only on avr itself.
func markInheritedOptions(row map[string]any, root *cobra.Command) {
	params, _ := row["params"].([]any)
	for _, value := range params {
		param, _ := value.(map[string]any)
		opts, _ := param["opts"].([]any)
		if len(opts) == 0 {
			continue
		}
		name, _ := opts[0].(string)
		param["inherited"] = root.PersistentFlags().Lookup(strings.TrimPrefix(name, "--")) != nil
	}
}

// referenceRow is one command as a record, without its subcommands, which are
// rows of their own.
func referenceRow(node *docgen.Node) (map[string]any, error) {
	own := *node
	own.Subcommands = nil
	data, err := json.Marshal(own)
	if err != nil {
		return nil, err
	}
	value, err := client.DecodeJSON(data)
	if err != nil {
		return nil, err
	}
	row, _ := value.(map[string]any)
	delete(row, "subcommands")
	row["command"] = node.Full
	if row["json_fields"] == nil {
		row["json_fields"] = []any{}
	}
	return row, nil
}
