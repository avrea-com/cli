package docgen

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

func escapeProse(value string) string {
	return strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(value)
}
func mdEscape(value string) string {
	var out strings.Builder
	for len(value) > 0 {
		start := strings.IndexByte(value, '`')
		if start < 0 {
			out.WriteString(escapeProse(value))
			break
		}
		out.WriteString(escapeProse(value[:start]))
		value = value[start:]
		count := 0
		for count < len(value) && value[count] == '`' {
			count++
		}
		ticks := value[:count]
		if len(value) <= count+1 {
			out.WriteString(escapeProse(value))
			break
		}
		end := strings.Index(value[count+1:], ticks)
		if end < 0 {
			out.WriteString(ticks)
			value = value[count:]
			continue
		}
		end += count + 1 + count
		out.WriteString(value[:end])
		value = value[end:]
	}
	return out.String()
}
func shortBody(help, short string) string {
	if help == "" || short == "" {
		return help
	}
	parts := strings.SplitN(strings.TrimLeft(help, " \t\r\n"), "\n\n", 2)
	if strings.Join(strings.Fields(parts[0]), " ") == strings.TrimSpace(short) {
		if len(parts) > 1 {
			return parts[1]
		}
		return ""
	}
	return help
}
func dedent(lines []string) []string {
	indent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		n := len(line) - len(strings.TrimLeft(line, " "))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	if indent < 0 {
		indent = 0
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if len(line) >= indent {
			out[i] = line[indent:]
		} else {
			out[i] = line
		}
	}
	return out
}

var (
	fieldListLine  = regexp.MustCompile(`^[a-z][a-z0-9_]*(, *[a-z][a-z0-9_]*)*,?$`)
	definitionLine = regexp.MustCompile(`^(\S+) {2,}(\S.*)$`)
)

func indented(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
}
func allMatch(lines []string, pattern *regexp.Regexp) bool {
	for _, line := range lines {
		if !pattern.MatchString(line) {
			return false
		}
	}
	return true
}

// preformattedBlock renders a click "\b" paragraph. "\b" only stops terminal
// rewrapping, so unindented lines are prose and stay Markdown text. Indented
// runs are field lists (inline code), "name  description" tables (a list) or
// commands and file contents (fenced code). A one-line label introducing an
// indented run, such as "Examples:" or "JSON FIELDS (GitHub)", becomes a bold
// label.
func preformattedBlock(block []string, language string) []string {
	out := []string{}
	for start := 0; start < len(block); {
		code := indented(block[start])
		end := start
		for end < len(block) && indented(block[end]) == code {
			end++
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		run := block[start:end]
		switch {
		case !code && len(run) == 1 && end < len(block) && len(strings.Fields(run[0])) <= 3:
			out = append(out, "**"+mdEscape(strings.TrimSuffix(strings.TrimSpace(run[0]), ":"))+"**")
		case !code:
			for _, line := range run {
				out = append(out, mdEscape(line))
			}
		default:
			out = append(out, indentedRun(dedent(run), language)...)
		}
		start = end
	}
	return out
}
func indentedRun(lines []string, language string) []string {
	switch {
	case allMatch(lines, fieldListLine):
		fields := []string{}
		for _, line := range lines {
			for _, field := range strings.Split(line, ",") {
				if field = strings.TrimSpace(field); field != "" {
					fields = append(fields, "`"+field+"`")
				}
			}
		}
		return []string{strings.Join(fields, ", ")}
	case allMatch(lines, definitionLine):
		out := []string{}
		for _, line := range lines {
			match := definitionLine.FindStringSubmatch(line)
			out = append(out, "- `"+match[1]+"`: "+mdEscape(match[2]))
		}
		return out
	default:
		return append(append([]string{"```" + language}, lines...), "```")
	}
}
func helpBlock(value, language string) string {
	if value == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(value, "\n"), "\n")
	out := []string{}
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "\b" {
			block := []string{}
			i++
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
				block = append(block, lines[i])
				i++
			}
			out = append(out, preformattedBlock(block, language)...)
			i--
			continue
		}
		if strings.HasPrefix(lines[i], "    ") {
			out = append(out, lines[i])
		} else {
			out = append(out, mdEscape(lines[i]))
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
func paramSignature(p Param, html bool) string {
	if p.Kind == "argument" {
		name := argumentLabel(p)
		if p.Nargs == -1 {
			name += "..."
		}
		if !p.Required {
			name = "[" + name + "]"
		}
		if html {
			return "<code class=\"cli-arg\">" + name + "</code>"
		}
		return "`" + name + "`"
	}
	primary := strings.Join(p.Opts, ", ")
	if len(p.Secondary) > 0 {
		primary += " / " + strings.Join(p.Secondary, ", ")
	}
	meta := strings.ToUpper(p.Type)
	if meta == "" {
		meta = "VALUE"
	}
	if html {
		result := "<code class=\"cli-flag\">" + strings.ReplaceAll(primary, "--", "&#x2D;&#x2D;") + "</code>"
		if !p.IsFlag {
			result += " <code class=\"cli-value\">&lt;" + meta + "&gt;</code>"
		}
		return result
	}
	if !p.IsFlag {
		primary += " <" + meta + ">"
	}
	return "`" + primary + "`"
}
func defaultVisible(p Param) bool {
	if p.Default == nil || p.Default == "" {
		return false
	}
	if p.IsFlag && p.Default == false {
		return false
	}
	return p.Kind != "argument" || !p.Required
}
func envText(value any) string {
	if items, ok := value.([]any); ok {
		parts := []string{}
		for _, item := range items {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, ", ")
	}
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
func paramMeta(p Param, markdown bool) []string {
	quote := func(v string) string {
		if markdown {
			return "`" + v + "`"
		}
		return v
	}
	out := []string{}
	if len(p.Choices) > 0 {
		values := []string{}
		for _, value := range p.Choices {
			values = append(values, quote(value))
		}
		out = append(out, "choices: "+strings.Join(values, ", "))
	}
	if defaultVisible(p) {
		out = append(out, "default: "+quote(pythonValue(p.Default)))
	}
	if env := envText(p.Envvar); env != "" {
		out = append(out, "env: "+quote(env))
	}
	if p.Multiple {
		out = append(out, "repeatable")
	}
	if p.Required && p.Kind == "option" {
		out = append(out, "required")
	}
	return out
}
func visibleParams(params []Param) ([]Param, []Param) {
	args, opts := []Param{}, []Param{}
	for _, p := range params {
		if p.Hidden {
			continue
		}
		if p.Kind == "argument" {
			args = append(args, p)
		} else {
			opts = append(opts, p)
		}
	}
	return args, opts
}
func paramList(heading string, params []Param, html bool) []string {
	if len(params) == 0 {
		return nil
	}
	out := []string{"**" + heading + "**", ""}
	for _, p := range params {
		tail := ""
		if help := mdEscape(strings.TrimSpace(text(p.Help))); help != "" {
			tail = " — " + help
		}
		if meta := paramMeta(p, true); len(meta) > 0 {
			tail += " _(" + strings.Join(meta, " · ") + ")_"
		}
		out = append(out, "- "+paramSignature(p, html)+tail)
	}
	return append(out, "")
}
func mdCommand(node *Node, depth int, html bool) []string {
	out := []string{strings.Repeat("#", depth) + " `" + node.Full + "`", ""}
	if node.ShortHelp != "" {
		out = append(out, node.ShortHelp, "")
	}
	out = append(out, "```sh\n"+node.Usage+"\n```", "")
	if body := helpBlock(shortBody(text(node.Help), node.ShortHelp), "sh"); body != "" {
		out = append(out, body, "")
	}
	args, opts := visibleParams(node.Params)
	out = append(out, paramList("Arguments", args, html)...)
	out = append(out, paramList("Options", opts, html)...)
	if epilog := helpBlock(text(node.Epilog), "text"); epilog != "" {
		out = append(out, epilog, "")
	}
	for _, sub := range node.Subcommands {
		out = append(out, mdCommand(sub, depth+1, html)...)
	}
	return out
}
func sortedAliases(tree *Tree) []string {
	keys := make([]string, 0, len(tree.Aliases))
	for key := range tree.Aliases {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
func finish(lines []string) string {
	return strings.TrimRight(strings.Join(lines, "\n"), " \t\r\n") + "\n"
}
func Markdown(tree *Tree) string {
	out := []string{
		"# " + tree.Program + "(1) — Avrea command-line client", "",
		"Reference for `" + tree.Program + "`. Generated from the source tree — do not edit by hand. Run `make docs` to regenerate.", "",
		"## Name", "", "`" + tree.Program + "` — " + tree.Summary, "", "## Synopsis", "",
		"```sh\n" + tree.Program + " [GLOBAL OPTIONS] COMMAND [ARGS]...\n```", "",
	}
	out = append(out, paramList("Global options", tree.GlobalOptions, false)...)
	if len(tree.Aliases) > 0 {
		out = append(out, "## Aliases", "")
		for _, alias := range sortedAliases(tree) {
			out = append(out, "- `"+alias+"` → `"+tree.Aliases[alias]+"`")
		}
		out = append(out, "")
	}
	out = append(out, "## Commands", "")
	for _, section := range tree.SectionOrder {
		nodes, ok := tree.Sections[section]
		if !ok {
			continue
		}
		out = append(out, "### "+section, "")
		for _, node := range nodes {
			out = append(out, "- [`"+node.Full+"`](#"+strings.Join(node.Path, "-")+") — "+node.ShortHelp)
		}
		out = append(out, "")
	}
	out = append(out, "## Reference", "")
	for _, node := range tree.top() {
		out = append(out, mdCommand(node, 3, false)...)
	}
	return finish(out)
}
