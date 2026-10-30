// Package docgen renders the public CLI reference in the existing documentation formats.
package docgen

import (
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// metadata contains migrated prose and Click-specific documentation fields.
// Runtime command/flag membership always comes from the live Cobra registry.
//
//go:embed metadata.json
var metadata []byte

type Param struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Opts        []string `json:"opts"`
	Secondary   []string `json:"secondary_opts"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Multiple    bool     `json:"multiple"`
	Nargs       int      `json:"nargs"`
	IsFlag      bool     `json:"is_flag"`
	Envvar      any      `json:"envvar"`
	Help        *string  `json:"help"`
	Hidden      bool     `json:"hidden"`
	ShowDefault bool     `json:"show_default"`
	Default     any      `json:"default"`
	Choices     []string `json:"choices,omitempty"`
}
type Node struct {
	Name        string   `json:"name"`
	Path        []string `json:"path"`
	Full        string   `json:"full"`
	IsGroup     bool     `json:"is_group"`
	ShortHelp   string   `json:"short_help"`
	Help        *string  `json:"help"`
	Epilog      *string  `json:"epilog"`
	Deprecated  any      `json:"deprecated"`
	Hidden      bool     `json:"hidden"`
	Usage       string   `json:"usage"`
	Params      []Param  `json:"params"`
	Subcommands []*Node  `json:"subcommands"`
}
type Tree struct {
	Program       string             `json:"program"`
	Version       string             `json:"version"`
	Summary       string             `json:"summary"`
	GlobalOptions []Param            `json:"global_options"`
	Aliases       map[string]string  `json:"aliases"`
	SectionOrder  []string           `json:"section_order"`
	Sections      map[string][]*Node `json:"sections"`
}

func text(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func str(value string) *string { return &value }
func pythonValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return "False"
	case string:
		return v
	case []any:
		if len(v) == 0 {
			return "()"
		}
	case []string:
		items := make([]string, len(v))
		for i, item := range v {
			items[i] = strconv.Quote(item)
		}
		suffix := ""
		if len(items) == 1 {
			suffix = ","
		}
		return "(" + strings.Join(items, ", ") + suffix + ")"
	}
	return fmt.Sprint(value)
}
func flagDefault(flag *pflag.Flag) any {
	switch flag.Value.Type() {
	case "bool":
		value, _ := strconv.ParseBool(flag.DefValue)
		return value
	case "int", "int64":
		value, _ := strconv.ParseInt(flag.DefValue, 10, 64)
		return value
	case "stringArray", "stringSlice", "intSlice":
		raw := strings.TrimSuffix(strings.TrimPrefix(flag.DefValue, "["), "]")
		if raw == "" {
			return []any{}
		}
		values, err := csv.NewReader(strings.NewReader(raw)).Read()
		if err != nil {
			return flag.DefValue
		}
		if flag.Value.Type() == "intSlice" {
			numbers := make([]int64, len(values))
			for i, value := range values {
				number, e := strconv.ParseInt(value, 10, 64)
				if e != nil {
					return flag.DefValue
				}
				numbers[i] = number
			}
			return numbers
		}
		return values
	default:
		return flag.DefValue
	}
}
func zeroFlagDefault(flag *pflag.Flag) bool {
	switch flag.Value.Type() {
	case "bool":
		return flag.DefValue == "false"
	case "int", "int64":
		return flag.DefValue == "0"
	case "stringArray", "stringSlice", "intSlice":
		return flag.DefValue == "[]"
	default:
		return flag.DefValue == ""
	}
}
func flagParam(flag *pflag.Flag) Param {
	typ := "text"
	if flag.Value.Type() == "bool" {
		typ = "boolean"
	}
	if flag.Value.Type() == "int" || flag.Value.Type() == "int64" || flag.Value.Type() == "intSlice" {
		typ = "integer"
	}
	opts := []string{"--" + flag.Name}
	if flag.Shorthand != "" {
		opts = append(opts, "-"+flag.Shorthand)
	}
	return Param{Name: strings.ReplaceAll(flag.Name, "-", "_"), Kind: "option", Opts: opts, Secondary: []string{}, Type: typ, Required: len(flag.Annotations[cobra.BashCompOneRequiredFlag]) > 0, Multiple: flag.Value.Type() == "stringArray" || flag.Value.Type() == "stringSlice" || flag.Value.Type() == "intSlice", Nargs: 1, IsFlag: flag.NoOptDefVal != "", Help: str(flag.Usage), Hidden: flag.Hidden, Default: flagDefault(flag)}
}
func lookupFlag(c *cobra.Command, option string) *pflag.Flag {
	if strings.HasPrefix(option, "--") {
		return c.LocalFlags().Lookup(option[2:])
	}
	if strings.HasPrefix(option, "-") {
		return c.LocalFlags().ShorthandLookup(option[1:])
	}
	return nil
}
func params(c *cobra.Command, original []Param) []Param {
	out := []Param{}
	arguments := []Param{}
	used := map[string]bool{}
	args := strings.Fields(c.Use)[1:]
	originals := []Param{}
	for _, param := range original {
		if param.Kind == "argument" {
			originals = append(originals, param)
		}
	}
	for i, arg := range args {
		name := strings.Trim(arg, "[]{}<>")
		name = strings.TrimSuffix(name, "...")
		p := Param{Name: strings.ToLower(name), Kind: "argument", Opts: []string{strings.ToLower(name)}, Secondary: []string{}, Type: "text", Required: !strings.HasPrefix(arg, "["), Nargs: 1}
		if strings.Contains(arg, "...") {
			p.Nargs = -1
		}
		if len(args) == len(originals) {
			p = originals[i]
			p.Required = !strings.HasPrefix(arg, "[")
			p.Nargs = 1
			if strings.Contains(arg, "...") {
				p.Nargs = -1
			}
		}
		arguments = append(arguments, p)
	}
	for _, param := range original {
		if param.Kind == "argument" {
			if len(arguments) > 0 {
				out = append(out, arguments[0])
				arguments = arguments[1:]
			}
			continue
		}
		if len(param.Opts) == 0 {
			continue
		}
		var flag *pflag.Flag
		for _, option := range param.Opts {
			if f := lookupFlag(c, option); f != nil {
				flag = f
				break
			}
		}
		if flag == nil {
			continue
		}
		keep := func(options []string) []string {
			kept := []string{}
			for _, option := range options {
				if f := lookupFlag(c, option); f != nil {
					kept = append(kept, option)
					used[f.Name] = true
				}
			}
			return kept
		}
		param.Opts = keep(param.Opts)
		param.Secondary = keep(param.Secondary)
		param.IsFlag = flag.NoOptDefVal != ""
		param.Multiple = flag.Value.Type() == "stringArray" || flag.Value.Type() == "stringSlice" || flag.Value.Type() == "intSlice"
		param.Hidden = flag.Hidden
		if param.Default != nil || !zeroFlagDefault(flag) {
			param.Default = flagDefault(flag)
		}
		out = append(out, param)
	}
	out = append(out, arguments...)
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if used[f.Name] || f.Name == "help" {
			return
		}
		out = append(out, flagParam(f))
	})
	return out
}
func buildNode(c *cobra.Command, path []string, old *Node) *Node {
	node := &Node{Name: c.Name(), Path: path, Full: strings.Join(path, " "), IsGroup: c.HasSubCommands(), ShortHelp: c.Short, Help: str(c.Long), Deprecated: false, Hidden: c.Hidden, Params: []Param{}, Subcommands: []*Node{}}
	previous := map[string]*Node{}
	if old != nil {
		node.ShortHelp = old.ShortHelp
		node.Help = old.Help
		node.Epilog = old.Epilog
		node.Usage = old.Usage
		for _, sub := range old.Subcommands {
			previous[sub.Name] = sub
		}
		node.Params = params(c, old.Params)
	} else {
		node.Params = params(c, nil)
	}
	if node.Help == nil || text(node.Help) == "" {
		node.Help = str(c.Short)
	}
	if c.Deprecated != "" {
		node.Deprecated = c.Deprecated
	}
	if node.Usage == "" {
		node.Usage = node.Full + " [OPTIONS]"
		if tail := strings.Fields(c.Use)[1:]; len(tail) > 0 {
			node.Usage += " " + strings.Join(tail, " ")
		}
		if node.IsGroup {
			node.Usage += " COMMAND [ARGS]..."
		}
	}
	for _, sub := range c.Commands() {
		if sub.Hidden {
			continue
		}
		node.Subcommands = append(node.Subcommands, buildNode(sub, append(append([]string{}, path...), sub.Name()), previous[sub.Name()]))
	}
	return node
}

// Build merges migrated documentation with the live public command registry.
// The reference version identifies the public package, independently of an
// embedding admin executable's own build/version string.
func Build(root *cobra.Command) (*Tree, error) {
	var tree Tree
	if err := json.Unmarshal(metadata, &tree); err != nil {
		return nil, err
	}
	templates := map[string]*Node{}
	sections := map[string]string{}
	for section, nodes := range tree.Sections {
		for _, node := range nodes {
			templates[node.Name] = node
			sections[node.Name] = section
		}
	}
	tree.GlobalOptions = params(root, tree.GlobalOptions)
	tree.Aliases = map[string]string{}
	tree.Sections = map[string][]*Node{}
	live := map[string]*cobra.Command{}
	for _, command := range root.Commands() {
		if command.Hidden {
			continue
		}
		live[command.Name()] = command
		for _, alias := range command.Aliases {
			tree.Aliases[alias] = command.Name()
		}
	}
	// Keep the public help section and command order, then append new commands
	// in Cobra's stable order instead of silently omitting undocumented nodes.
	var original Tree
	if err := json.Unmarshal(metadata, &original); err != nil {
		return nil, err
	}
	for _, section := range tree.SectionOrder {
		for _, node := range original.Sections[section] {
			if command := live[node.Name]; command != nil {
				tree.Sections[section] = append(tree.Sections[section], buildNode(command, []string{tree.Program, node.Name}, node))
				delete(live, node.Name)
			}
		}
	}
	for _, command := range root.Commands() {
		if live[command.Name()] == nil {
			continue
		}
		section := sections[command.Name()]
		if section == "" {
			section = "Additional Commands"
		}
		tree.Sections[section] = append(tree.Sections[section], buildNode(command, []string{tree.Program, command.Name()}, templates[command.Name()]))
	}
	return &tree, nil
}
func (tree *Tree) top() []*Node {
	nodes := []*Node{}
	for _, section := range tree.SectionOrder {
		nodes = append(nodes, tree.Sections[section]...)
	}
	return nodes
}
func JSON(tree *Tree) (string, error) {
	data, err := json.MarshalIndent(tree, "", "  ")
	return string(data) + "\n", err
}
