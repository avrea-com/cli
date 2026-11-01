package docgen

import (
	"bytes"
	"encoding/json"
	"strings"
)

func quotedJSON(value string) string {
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return strings.TrimSuffix(data.String(), "\n")
}
func commandPage(node *Node) string {
	short := strings.TrimSpace(node.ShortHelp)
	out := []string{"---", "title: " + node.Full, "description: \"" + strings.ReplaceAll(short, "\"", "\\\"") + "\"", "---", ""}
	if short != "" {
		out = append(out, short, "")
	}
	out = append(out, "```sh\n"+node.Usage+"\n```", "")
	if body := helpBlock(shortBody(text(node.Help), short), "sh"); body != "" {
		out = append(out, body, "")
	}
	args, opts := visibleParams(node.Params)
	out = append(out, paramList("Arguments", args, true)...)
	out = append(out, paramList("Options", opts, true)...)
	if epilog := helpBlock(text(node.Epilog), "text"); epilog != "" {
		out = append(out, epilog, "")
	}
	if len(node.Subcommands) > 0 {
		out = append(out, "## Subcommands", "")
		for _, sub := range node.Subcommands {
			out = append(out, mdCommand(sub, 3, true)...)
		}
	}
	return finish(out)
}
func indexPage(tree *Tree) string {
	out := []string{"---", "title: CLI Reference", "description: \"Reference for the avr command-line client (v" + tree.Version + ").\"", "---", "",
		"`" + tree.Program + "` is the Avrea command-line client. " + tree.Summary, "", "## Synopsis", "",
		"```sh\n" + tree.Program + " [GLOBAL OPTIONS] COMMAND [ARGS]...\n```", ""}
	out = append(out, paramList("Global options", tree.GlobalOptions, true)...)
	out = append(out, "## Commands", "")
	for _, section := range tree.SectionOrder {
		nodes, ok := tree.Sections[section]
		if !ok {
			continue
		}
		out = append(out, "### "+section, "")
		for _, node := range nodes {
			out = append(out, "- [`"+node.Full+"`](./"+node.Name+"/) — "+node.ShortHelp)
		}
		out = append(out, "")
	}
	if len(tree.Aliases) > 0 {
		out = append(out, "## Aliases", "")
		for _, alias := range sortedAliases(tree) {
			target := tree.Aliases[alias]
			out = append(out, "- `"+alias+"` → [`"+target+"`](./"+target+"/)")
		}
		out = append(out, "")
	}
	return finish(out)
}
func overviewPage(readme string, tree *Tree) string {
	lines := strings.Split(strings.TrimLeft(readme, " \t\r\n"), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "# ") {
		lines = lines[1:]
		for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
			lines = lines[1:]
		}
	}
	summary := tree.Summary
	if summary == "" {
		summary = "Avrea command-line client."
	}
	return "---\ntitle: Avrea CLI\ndescription: \"" + strings.ReplaceAll(summary, "\"", "\\\"") + "\"\n---\n\n" + finish(lines)
}
func sidebar(tree *Tree, overview bool) string {
	out := []string{
		"// Auto-generated from the avr command tree by `avr internal docs`.",
		"// Do not edit by hand — regenerate via `make -C avr-cli docs`.", "",
		"export default {", "  label: 'CLI',", "  items: [",
	}
	if overview {
		out = append(out, "    { label: 'Overview', slug: 'cli' },")
	}
	out = append(out, "    { label: 'All Commands', slug: 'cli/reference' },")
	for _, section := range tree.SectionOrder {
		nodes := tree.Sections[section]
		if len(nodes) == 0 {
			continue
		}
		out = append(out, "    {", "      label: "+quotedJSON(section)+",", "      collapsed: false,", "      items: [")
		for _, node := range nodes {
			out = append(out, "        { label: "+quotedJSON(node.Full)+", slug: "+quotedJSON("cli/reference/"+node.Name)+" },")
		}
		out = append(out, "      ],", "    },")
	}
	out = append(out, "  ],", "};")
	return finish(out)
}

// Starlight generates one page per visible top-level public command.
func Starlight(tree *Tree, readme *string) map[string]string {
	files := map[string]string{"reference/index.md": indexPage(tree), "_cli-sidebar.mjs": sidebar(tree, readme != nil)}
	if readme != nil && *readme != "" {
		files["index.md"] = overviewPage(*readme, tree)
	}
	for _, node := range tree.top() {
		files["reference/"+node.Name+".md"] = commandPage(node)
	}
	return files
}
