package docgen

import "strings"

func troffEscape(value string) string {
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\\", "\\e")
	lines := strings.Split(strings.TrimSuffix(value, "\n"), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "'") {
			lines[i] = "\\&" + line
		}
	}
	return strings.Join(lines, "\n")
}
func troffFlag(value string) string { return "\\fB" + strings.ReplaceAll(value, "-", "\\-") + "\\fR" }
func manSignature(p Param) string {
	if p.Kind == "argument" {
		name := argumentLabel(p)
		if p.Nargs == -1 {
			name += "..."
		}
		value := "\\fI" + name + "\\fR"
		if !p.Required {
			value = "[" + value + "]"
		}
		return value
	}
	parts := []string{}
	for _, opt := range p.Opts {
		parts = append(parts, troffFlag(opt))
	}
	primary := strings.Join(parts, ", ")
	if len(p.Secondary) > 0 {
		parts = []string{}
		for _, opt := range p.Secondary {
			parts = append(parts, troffFlag(opt))
		}
		primary += " / " + strings.Join(parts, ", ")
	}
	if !p.IsFlag {
		meta := strings.ToUpper(p.Type)
		if meta == "" {
			meta = "VALUE"
		}
		primary += " \\fI" + meta + "\\fR"
	}
	return primary
}
func manLiteral(lines []string) []string {
	out := []string{".RS 4", ".nf"}
	out = append(out, strings.Split(troffEscape(strings.Join(lines, "\n")), "\n")...)
	return append(out, ".fi", ".RE", ".PP")
}
func manHelp(value string) []string {
	if value == "" {
		return nil
	}
	out := []string{}
	for _, segment := range helpSegments(value) {
		switch segment.kind {
		case proseSegment:
			words := make([]string, len(segment.lines))
			for i, line := range segment.lines {
				words[i] = strings.TrimSpace(line)
			}
			out = append(out, troffEscape(strings.Join(words, " ")), ".PP")
		case unwrappedSegment:
			out = append(out, manLiteral(dedent(segment.lines))...)
		case exampleSegment:
			out = append(out, manLiteral(segment.lines)...)
		default:
			panic("unknown help segment kind")
		}
	}
	if len(out) > 0 && out[len(out)-1] == ".PP" {
		out = out[:len(out)-1]
	}
	return out
}
func manParams(heading string, params []Param) []string {
	if len(params) == 0 {
		return nil
	}
	out := []string{".SH " + heading}
	for _, p := range params {
		out = append(out, ".TP", manSignature(p))
		parts := []string{}
		if help := strings.TrimSpace(text(p.Help)); help != "" {
			parts = append(parts, troffEscape(help))
		}
		if meta := paramMeta(p, false); len(meta) > 0 {
			parts = append(parts, "("+strings.Join(meta, "; ")+")")
		}
		if len(parts) == 0 {
			out = append(out, "\\&")
		} else {
			out = append(out, strings.Join(parts, " "))
		}
	}
	return out
}
func flatten(node *Node) []*Node {
	out := []*Node{node}
	for _, sub := range node.Subcommands {
		out = append(out, flatten(sub)...)
	}
	return out
}
func seeAlso(node *Node, nodes []*Node) [][]string {
	refs := [][]string{}
	path := node.Path
	root := []string{"avr"}
	if len(path) != 1 {
		refs = append(refs, root)
	}
	if len(path) > 2 {
		refs = append(refs, path[:len(path)-1])
	}
	parent := strings.Join(path[:len(path)-1], " ")
	for _, other := range nodes {
		p := other.Path
		if len(p) == len(path) && strings.Join(p[:len(p)-1], " ") == parent && other.Full != node.Full {
			refs = append(refs, p)
		}
	}
	for _, other := range nodes {
		p := other.Path
		if len(p) == len(path)+1 && strings.Join(p[:len(p)-1], " ") == node.Full {
			refs = append(refs, p)
		}
	}
	seen := map[string]bool{}
	out := [][]string{}
	for _, ref := range refs {
		key := strings.Join(ref, " ")
		if !seen[key] {
			out = append(out, ref)
			seen[key] = true
		}
	}
	return out
}
func manPage(node *Node, nodes []*Node) string {
	title := strings.ReplaceAll(strings.ToUpper(strings.Join(node.Path, "-")), "-", "\\-")
	short := strings.TrimSpace(node.ShortHelp)
	name := short
	if name == "" {
		name = node.Name
	}
	out := []string{".TH " + title + " 1 \"\" \"avr\" \"Avrea CLI\"", ".SH NAME",
		troffEscape(strings.Join(node.Path, "-")) + " \\- " + troffEscape(name), ".SH SYNOPSIS", ".B " + node.Full}
	if tail := strings.TrimSpace(strings.TrimPrefix(node.Usage, node.Full)); tail != "" {
		out = append(out, troffEscape(tail))
	}
	out = append(out, ".SH DESCRIPTION")
	if short != "" {
		out = append(out, troffEscape(short), ".PP")
	}
	out = append(out, manHelp(shortBody(text(node.Help), short))...)
	args, opts := visibleParams(node.Params)
	out = append(out, manParams("ARGUMENTS", args)...)
	out = append(out, manParams("OPTIONS", opts)...)
	if epilog := manHelp(text(node.Epilog)); len(epilog) > 0 {
		out = append(out, ".SH NOTES")
		out = append(out, epilog...)
	}
	if len(node.Subcommands) > 0 {
		out = append(out, ".SH COMMANDS")
		for _, sub := range node.Subcommands {
			out = append(out, ".TP", troffEscape(sub.Name), troffEscape(sub.ShortHelp))
		}
	}
	if refs := seeAlso(node, nodes); len(refs) > 0 {
		lines := []string{}
		for _, ref := range refs {
			lines = append(lines, ".BR "+strings.Join(ref, "-")+" (1)")
		}
		out = append(out, ".SH SEE ALSO", strings.Join(lines, ",\n"))
	}
	return finish(out)
}

// Man generates the root and every visible command's troff page.
func Man(tree *Tree) map[string]string {
	root := &Node{Name: "avr", Path: []string{"avr"}, Full: "avr", IsGroup: true, ShortHelp: tree.Summary, Help: str(tree.Summary), Deprecated: false, Usage: "avr [OPTIONS] COMMAND [ARGS]...", Params: tree.GlobalOptions, Subcommands: tree.top()}
	if root.ShortHelp == "" {
		root.ShortHelp = "Avrea command-line client."
	}
	nodes := flatten(root)
	files := map[string]string{}
	for _, node := range nodes {
		files[strings.Join(node.Path, "-")+".1"] = manPage(node, nodes)
	}
	return files
}
