package commands

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/avrea-com/cli/docgen"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type completionItem struct{ kind, value, description string }

// completionWords accepts unfinished shell quoting without evaluating anything.
func completionWords(raw string) []string {
	words := []string{}
	var word strings.Builder
	quote := rune(0)
	started := false
	chars := []rune(raw)
	for i := 0; i < len(chars); i++ {
		ch := chars[i]
		if quote == '\'' {
			if ch == quote {
				quote = 0
			} else {
				word.WriteRune(ch)
			}
			continue
		}
		if ch == '\\' {
			started = true
			if i+1 < len(chars) {
				next := chars[i+1]
				if quote == '"' && next != '"' && next != '\\' {
					word.WriteRune(ch)
				}
				word.WriteRune(next)
				i++
			}
			continue
		}
		if quote == '"' {
			if ch == quote {
				quote = 0
			} else {
				word.WriteRune(ch)
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			started = true
			continue
		}
		if strings.ContainsRune(" \t\r\n", ch) {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
		} else {
			word.WriteRune(ch)
			started = true
		}
	}
	if started {
		words = append(words, word.String())
	}
	return words
}

func completionArguments(shell string, getenv func(string) string) ([]string, string, bool) {
	words := completionWords(getenv("COMP_WORDS"))
	current := getenv("COMP_CWORD")
	if shell == "fish" {
		incomplete := ""
		if partial := completionWords(current); len(partial) > 0 {
			incomplete = partial[0]
		}
		if len(words) > 0 {
			words = words[1:]
		}
		if incomplete != "" && len(words) > 0 && words[len(words)-1] == incomplete {
			words = words[:len(words)-1]
		}
		return words, incomplete, true
	}
	index, err := strconv.Atoi(current)
	if err != nil || index < 0 {
		return nil, "", false
	}
	incomplete := ""
	if index < len(words) {
		incomplete = words[index]
	}
	end := min(index, len(words))
	if end <= 1 {
		return nil, incomplete, true
	}
	return words[1:end], incomplete, true
}

func completionParams(c *cobra.Command, spec docgen.HelpSpec) []docgen.CompletionParam {
	c.InitDefaultHelpFlag()
	out := []docgen.CompletionParam{}
	used := map[string]bool{}
	for _, param := range spec.CompletionParams {
		if param.Kind == "argument" {
			out = append(out, param)
			continue
		}
		names := []string{}
		var live *pflag.Flag
		for _, name := range param.Names {
			if flag := helpFlag(c, name); flag != nil && !flag.Hidden {
				names = append(names, name)
				used[flag.Name] = true
				live = flag
			}
		}
		if len(names) > 0 && !param.Hidden {
			param.Names = names
			param.Flag = live.NoOptDefVal != ""
			param.Multiple = live.Value.Type() == "stringArray" || live.Value.Type() == "stringSlice" || live.Value.Type() == "intSlice"
			out = append(out, param)
		}
	}
	c.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if used[flag.Name] || flag.Hidden {
			return
		}
		names := []string{"--" + flag.Name}
		if flag.Shorthand != "" {
			names = append([]string{"-" + flag.Shorthand}, names...)
		}
		out = append(out, docgen.CompletionParam{Name: flag.Name, Kind: "option", Names: names, Help: flag.Usage, Flag: flag.NoOptDefVal != "", Nargs: 1, Multiple: flag.Value.Type() == "stringArray" || flag.Value.Type() == "stringSlice" || flag.Value.Type() == "intSlice"})
	})
	if len(spec.CompletionParams) == 0 {
		for _, arg := range strings.Fields(c.Use)[1:] {
			count := 1
			if strings.Contains(arg, "...") {
				count = -1
			}
			out = append(out, docgen.CompletionParam{Name: arg, Kind: "argument", Nargs: count})
		}
	}
	return out
}

func completionOption(params []docgen.CompletionParam, name string) *docgen.CompletionParam {
	for i := range params {
		if params[i].Kind == "option" && slices.Contains(params[i].Names, name) {
			return &params[i]
		}
	}
	return nil
}

type completionContext struct {
	command     *cobra.Command
	params      []docgen.CompletionParam
	used        map[string]bool
	positionals int
	pending     *docgen.CompletionParam
}

func resolveCompletion(root *cobra.Command, args []string, specs map[string]docgen.HelpSpec) completionContext {
	args = expandTestsShorthand(root, args)
	state := completionContext{}
	selectCommand := func(c *cobra.Command) {
		state = completionContext{command: c, params: completionParams(c, specs[canonicalHelpPath(c)]), used: map[string]bool{}}
	}
	selectCommand(root)
	stopped := false
	for _, arg := range args {
		if state.pending != nil {
			state.used[state.pending.Name] = true
			state.pending = nil
			continue
		}
		if !stopped && arg == "--" {
			stopped = true
			continue
		}
		if !stopped && strings.HasPrefix(arg, "-") && arg != "-" {
			name, _, attached := strings.Cut(arg, "=")
			if param := completionOption(state.params, name); param != nil {
				if param.Flag || attached {
					state.used[param.Name] = true
				} else {
					state.pending = param
				}
				continue
			}
			if !strings.HasPrefix(arg, "--") {
				for i, ch := range []rune(arg[1:]) {
					param := completionOption(state.params, "-"+string(ch))
					if param == nil {
						break
					}
					if param.Flag {
						state.used[param.Name] = true
						continue
					}
					if i+1 < len([]rune(arg[1:])) {
						state.used[param.Name] = true
					} else {
						state.pending = param
					}
					break
				}
			}
			continue
		}
		if state.command.HasSubCommands() {
			var target *cobra.Command
			for _, child := range state.command.Commands() {
				if child.Name() == arg || slices.Contains(child.Aliases, arg) {
					target = child
					break
				}
			}
			if target == nil {
				break
			}
			selectCommand(target)
			stopped = false
		} else {
			state.positionals++
		}
	}
	return state
}

func completionValues(param docgen.CompletionParam, prefix string) []completionItem {
	switch param.Type.Kind {
	case "Path":
		kind := "file"
		if !param.Type.FileOkay {
			kind = "dir"
		}
		return []completionItem{{kind: kind, value: prefix}}
	case "File":
		return []completionItem{{kind: "file", value: prefix}}
	case "Choice":
		out := []completionItem{}
		for _, choice := range param.Type.Choices {
			value, start := choice, prefix
			if !param.Type.CaseSensitive {
				value, start = strings.ToLower(value), strings.ToLower(start)
			}
			if strings.HasPrefix(value, start) {
				out = append(out, completionItem{kind: "plain", value: choice})
			}
		}
		return out
	}
	return nil
}

func completeCommand(root *cobra.Command, args []string, incomplete string) ([]completionItem, error) {
	specs, err := docgen.HelpTemplates()
	if err != nil {
		return nil, err
	}
	if incomplete == "=" {
		incomplete = ""
	} else if strings.HasPrefix(incomplete, "-") {
		if name, value, found := strings.Cut(incomplete, "="); found {
			args = append(slices.Clone(args), name)
			incomplete = value
		}
	}
	state := resolveCompletion(root, args, specs)
	optionPrefix := strings.HasPrefix(incomplete, "-") && !slices.Contains(args, "--")
	if !optionPrefix {
		if state.pending != nil {
			return completionValues(*state.pending, incomplete), nil
		}
		position := state.positionals
		for _, param := range state.params {
			if param.Kind != "argument" {
				continue
			}
			if param.Nargs == -1 || position < param.Nargs {
				return completionValues(param, incomplete), nil
			}
			position -= param.Nargs
		}
	}
	out := []completionItem{}
	for _, child := range state.command.Commands() {
		if child.Hidden || child.Name() == "help" && child.Short == "Help about any command" || !strings.HasPrefix(child.Name(), incomplete) {
			continue
		}
		description := child.Short
		if spec, known := specs[canonicalHelpPath(child)]; known {
			description = spec.CompletionHelp
		}
		out = append(out, completionItem{"plain", child.Name(), description})
	}
	if len(incomplete) > 0 && !unicode.IsLetter([]rune(incomplete)[0]) && !unicode.IsDigit([]rune(incomplete)[0]) {
		for _, param := range state.params {
			if param.Kind != "option" || param.Hidden || !param.Multiple && state.used[param.Name] {
				continue
			}
			for _, name := range param.Names {
				if strings.HasPrefix(name, incomplete) {
					out = append(out, completionItem{"plain", name, param.Help})
				}
			}
		}
	}
	return out, nil
}

func formatCompletion(shell string, item completionItem) string {
	switch shell {
	case "zsh":
		description := item.description
		if description == "" {
			description = "_"
		} else {
			item.value = strings.ReplaceAll(item.value, ":", "\\:")
		}
		return item.kind + "\n" + item.value + "\n" + description
	case "fish":
		if item.description != "" {
			return item.kind + "," + item.value + "\t" + strings.NewReplacer("\n", "\\n", "\t", " ").Replace(item.description)
		}
	}
	return item.kind + "," + item.value
}

// executeCompletion runs before argument parsing, config loading, or callbacks.
// Existing shell installations continue to use Click's environment protocol.
func executeCompletion(root *cobra.Command, dependencies []Dependencies) (int, bool) {
	getenv := os.Getenv
	if len(dependencies) > 0 && dependencies[0].Getenv != nil {
		getenv = dependencies[0].Getenv
	}
	variable := "_" + strings.ToUpper(strings.ReplaceAll(root.Name(), "-", "_")) + "_COMPLETE"
	mode := getenv(variable)
	if mode == "" && variable != "_AVR_COMPLETE" {
		variable, mode = "_AVR_COMPLETE", getenv("_AVR_COMPLETE")
	}
	if mode == "" {
		return 0, false
	}
	shell, operation, ok := strings.Cut(mode, "_")
	if !ok || !slices.Contains([]string{"bash", "zsh", "fish"}, shell) {
		return 1, true
	}
	var err error
	switch operation {
	case "source":
		_, err = io.WriteString(root.OutOrStdout(), completionSource(shell, root.Name(), variable))
	case "complete":
		args, incomplete, valid := completionArguments(shell, getenv)
		if !valid {
			return 1, true
		}
		var items []completionItem
		items, err = completeCommand(root, args, incomplete)
		if err == nil {
			lines := make([]string, len(items))
			for i, item := range items {
				lines[i] = formatCompletion(shell, item)
			}
			_, err = fmt.Fprintln(root.OutOrStdout(), strings.Join(lines, "\n"))
		}
	default:
		return 1, true
	}
	if err != nil && !isBrokenPipe(err) {
		return 1, true
	}
	return 0, true
}
