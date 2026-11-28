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

// dedent strips the leading whitespace every non-blank line shares byte for
// byte; a tab and spaces are no common prefix. Blank lines become empty.
func dedent(lines []string) []string {
	prefix, found := "", false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if !found {
			prefix, found = lead, true
			continue
		}
		n := 0
		for n < len(prefix) && n < len(lead) && prefix[n] == lead[n] {
			n++
		}
		prefix = prefix[:n]
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			out[i] = strings.TrimPrefix(line, prefix)
		}
	}
	return out
}

var (
	fieldListLine  = regexp.MustCompile(`^[a-z][a-z0-9_]*(, *[a-z][a-z0-9_]*)*,?$`)
	definitionLine = regexp.MustCompile(`^(\S+) {2,}([^\s#].*)$`)
	backtickRun    = regexp.MustCompile("`+")
	// keyTableLabels are the labels whose "name  description" runs are key
	// tables. Aligned runs anywhere else, such as sample output under
	// "Examples", stay code.
	keyTableLabels = map[string]bool{"Available keys": true, "Topics": true}
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

// indentedSpan returns the indented run starting at lines[i] and the index
// after it. Blank lines stay in the run while the next non-blank line is
// still indented, so an example with blank lines is one block.
func indentedSpan(lines []string, i int) ([]string, int) {
	end := i
	for j := i; j < len(lines); j++ {
		if indented(lines[j]) && strings.TrimSpace(lines[j]) != "" {
			end = j + 1
		} else if strings.TrimSpace(lines[j]) != "" {
			break
		}
	}
	return dedent(lines[i:end]), end
}

// preformattedBlock renders a click "\b" paragraph. "\b" only stops terminal
// rewrapping, so unindented lines are prose and stay Markdown text. Indented
// runs are field lists under a JSON FIELDS label (inline code),
// "name  description" tables (a list) or commands and file contents (fenced
// code). A one-line label introducing an indented run, such as "Examples:" or
// "JSON FIELDS (GitHub)", becomes a bold label.
func preformattedBlock(block []string, language string) []string {
	out := []string{}
	label := ""
	for start := 0; start < len(block); {
		if len(out) > 0 {
			out = append(out, "")
		}
		if indented(block[start]) {
			run, end := indentedSpan(block, start)
			out = append(out, indentedRun(run, label, language)...)
			start, label = end, ""
			continue
		}
		end := start
		for end < len(block) && !indented(block[end]) {
			end++
		}
		run := block[start:end]
		if len(run) == 1 && end < len(block) && len(strings.Fields(run[0])) <= 3 {
			label = strings.TrimSuffix(strings.TrimSpace(run[0]), ":")
			out = append(out, "**"+mdEscape(label)+"**")
		} else {
			for _, line := range run {
				out = append(out, mdEscape(line))
			}
		}
		start = end
	}
	return out
}
func indentedRun(lines []string, label, language string) []string {
	switch {
	case strings.HasPrefix(label, "JSON FIELDS") && allMatch(lines, fieldListLine):
		fields := []string{}
		for _, line := range lines {
			for _, field := range strings.Split(line, ",") {
				if field = strings.TrimSpace(field); field != "" {
					fields = append(fields, "`"+field+"`")
				}
			}
		}
		return []string{strings.Join(fields, ", ")}
	case keyTableLabels[label] && allMatch(lines, definitionLine):
		out := []string{}
		for _, line := range lines {
			match := definitionLine.FindStringSubmatch(line)
			out = append(out, "- `"+match[1]+"`: "+mdEscape(match[2]))
		}
		return out
	default:
		// A fence ends at the first run of as many backticks, so it must
		// be longer than any run inside the example.
		longest := 0
		for _, line := range lines {
			for _, run := range backtickRun.FindAllString(line, -1) {
				longest = max(longest, len(run))
			}
		}
		fence := strings.Repeat("`", max(3, longest+1))
		return append(append([]string{fence + language}, lines...), fence)
	}
}

// helpBlock renders help text as Markdown, one block per help segment.
func helpBlock(value, language string) string {
	blocks := []string{}
	for _, segment := range helpSegments(value) {
		var rendered []string
		switch segment.kind {
		case proseSegment:
			for _, line := range segment.lines {
				rendered = append(rendered, mdEscape(line))
			}
		case unwrappedSegment:
			rendered = preformattedBlock(segment.lines, language)
		case exampleSegment:
			rendered = indentedRun(segment.lines, "", language)
		default:
			panic("unknown help segment kind")
		}
		blocks = append(blocks, strings.Join(rendered, "\n"))
	}
	return strings.TrimSpace(strings.Join(blocks, "\n\n"))
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
