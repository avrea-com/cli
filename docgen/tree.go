// Package docgen renders the public CLI reference in the existing documentation formats.
package docgen

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

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
	// Metavar is the argument's name in the usage line when that differs
	// from Name, so the argument list matches the usage line.
	Metavar string `json:"metavar,omitempty"`
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
	JSONFields  []string `json:"json_fields,omitempty"`
	Subcommands []*Node  `json:"subcommands"`
}
type Tree struct {
	Program       string             `json:"program"`
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

// LiveOptionNames preserves documented option order while adding live aliases.
func LiveOptionNames(c *cobra.Command, options []string) []string {
	names := []string{}
	for _, option := range options {
		if lookupFlag(c, option) != nil && !slices.Contains(names, option) {
			names = append(names, option)
		}
	}
	for _, option := range options {
		if f := lookupFlag(c, option); f != nil && f.Shorthand != "" && !slices.Contains(names, "-"+f.Shorthand) {
			names = append(names, "-"+f.Shorthand)
		}
	}
	return names
}

// metavarPattern matches a plain usage name such as VM_ID_OR_NAME. Usage
// tokens that spell out choices, like {on|off} or <endpoint>, keep the
// parameter's own name in the argument list.
var metavarPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// argumentLabel is how argument lists name a positional argument.
func argumentLabel(p Param) string {
	if p.Metavar != "" {
		return p.Metavar
	}
	return strings.ToUpper(p.Name)
}

// usageName drops the optional brackets and repetition dots around a usage
// token, written as [NAME], NAME..., [NAME...] or [NAME]...
func usageName(token string) string {
	token = strings.TrimSuffix(token, "...")
	if strings.HasPrefix(token, "[") && strings.HasSuffix(token, "]") {
		token = token[1 : len(token)-1]
	}
	return strings.TrimSuffix(token, "...")
}

// usageArguments returns the argument tokens of the usage line a command is
// documented with. Cobra's own usage decides how many arguments there are,
// so a description that words them differently is not used.
func usageArguments(args, described []string) []string {
	tokens := []string{}
	for _, token := range strings.Fields(strings.Join(described, " ")) {
		if token != "[OPTIONS]" {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) != len(args) {
		return args
	}
	return tokens
}

func params(c *cobra.Command, original []Param, usage []string) []Param {
	out := []Param{}
	arguments := []Param{}
	used := map[string]bool{}
	args := strings.Fields(c.Use)[1:]
	labels := usageArguments(args, usage)
	originals := []Param{}
	for _, param := range original {
		if param.Kind == "argument" {
			originals = append(originals, param)
		}
	}
	for i, arg := range args {
		name := strings.Trim(usageName(arg), "{}<>")
		p := Param{Name: strings.ToLower(name), Kind: "argument", Opts: []string{strings.ToLower(name)}, Secondary: []string{}, Type: "text", Required: !strings.HasPrefix(arg, "["), Nargs: 1}
		if strings.Contains(arg, "...") {
			p.Nargs = -1
		}
		if len(args) == len(originals) {
			p = originals[i]
			if label := usageName(labels[i]); metavarPattern.MatchString(label) && strings.ToLower(label) != p.Name {
				p.Metavar = label
			}
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
			for _, option := range LiveOptionNames(c, options) {
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
func buildNode(c *cobra.Command, path []string, describe Describer) *Node {
	d := describe(c)
	node := &Node{Name: c.Name(), Path: path, Full: strings.Join(path, " "), IsGroup: c.HasSubCommands(), ShortHelp: c.Short, Help: str(c.Long), Deprecated: false, Hidden: c.Hidden, Params: []Param{}, Subcommands: []*Node{}}
	if d.Short != "" {
		node.ShortHelp = d.Short
	}
	if d.Help != "" {
		node.Help = str(d.Help)
	}
	if d.Epilog != "" {
		node.Epilog = str(d.Epilog)
	}
	node.Params = params(c, d.params(), d.Usage)
	if len(d.Usage) > 0 {
		node.Usage = node.Full + " " + strings.Join(d.Usage, " ")
	}
	if node.Help == nil || text(node.Help) == "" {
		node.Help = str(c.Short)
	}
	node.JSONFields = jsonFields(text(node.Help))
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
		node.Subcommands = append(node.Subcommands, buildNode(sub, append(append([]string{}, path...), sub.Name()), describe))
	}
	return node
}

// Build derives reference output from the live Go command tree and descriptions.
func Build(root *cobra.Command, describe Describer, sections []Section) (*Tree, error) {
	d := describe(root)
	tree := Tree{Program: "avr", Summary: strings.TrimSpace(d.Help),
		GlobalOptions: params(root, d.params(), d.Usage), Aliases: map[string]string{}, Sections: map[string][]*Node{}}
	live := map[string]*cobra.Command{}
	for _, c := range root.Commands() {
		if c.Hidden {
			continue
		}
		live[c.Name()] = c
		for _, alias := range c.Aliases {
			tree.Aliases[alias] = c.Name()
		}
	}
	for _, section := range sections {
		tree.SectionOrder = append(tree.SectionOrder, section.Name)
		for _, name := range section.Commands {
			if c := live[name]; c != nil {
				tree.Sections[section.Name] = append(tree.Sections[section.Name], buildNode(c, []string{tree.Program, c.Name()}, describe))
				delete(live, name)
			}
		}
	}
	for _, c := range root.Commands() {
		if live[c.Name()] == nil {
			continue
		}
		section := "Additional Commands"
		tree.Sections[section] = append(tree.Sections[section], buildNode(c, []string{tree.Program, c.Name()}, describe))
	}
	return &tree, nil
}

// jsonFields reads the field names a command's help lists under JSON FIELDS:
// comma-separated, on the indented lines that follow the heading. A command
// whose fields depend on its target lists them under headings such as
// "JSON FIELDS (GitHub)" instead, and gets none here.
func jsonFields(help string) []string {
	_, after, found := strings.Cut(help, "\nJSON FIELDS\n")
	if !found {
		return nil
	}
	var fields []string
	for _, line := range strings.Split(after, "\n") {
		if strings.TrimSpace(line) == "" || strings.TrimLeft(line, " \t") == line {
			break
		}
		for _, field := range strings.Split(line, ",") {
			if field = strings.TrimSpace(field); field != "" {
				fields = append(fields, field)
			}
		}
	}
	return fields
}

// Commands returns every command of the tree, each group before its
// subcommands, in section order.
func (tree *Tree) Commands() []*Node {
	var nodes []*Node
	for _, node := range tree.top() {
		nodes = append(nodes, flatten(node)...)
	}
	return nodes
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
