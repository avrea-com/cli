package docgen

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

func quotedJSON(value string) string {
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return strings.TrimSuffix(data.String(), "\n")
}

// frontmatter starts a page. Starlight renders title as the heading and
// sidebar label; a non-empty metaTitle sets the browser and og:title that
// search results and link previews show instead.
func frontmatter(title, description, metaTitle string) []string {
	out := []string{"---", "title: " + title, "description: " + quotedJSON(description)}
	if metaTitle != "" {
		out = append(out, "head:",
			"  - tag: title", "    content: "+quotedJSON(metaTitle+" | Avrea Docs"),
			"  - tag: meta", "    attrs:", "      property: og:title", "      content: "+quotedJSON(metaTitle))
	}
	return append(out, "---", "")
}
func commandPage(node *Node) string {
	short := strings.TrimSpace(node.ShortHelp)
	out := frontmatter(node.Full, short, "")
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
	out := frontmatter("CLI Reference", "Reference for the avr command-line client.", "avr command reference")
	out = append(out, "`"+tree.Program+"` is the Avrea command-line client. "+tree.Summary, "", "## Synopsis", "",
		"```sh\n"+tree.Program+" [GLOBAL OPTIONS] COMMAND [ARGS]...\n```", "")
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

// overviewDescription is the overview's search snippet. Keep it within 155
// characters so results do not truncate it.
const overviewDescription = "Install avr, sign in and manage runs, jobs, logs, tests, caches and VMs from the terminal, with JSON output for scripts and coding agents."

func overviewPage(readme string) string {
	lines := strings.Split(strings.TrimLeft(readme, " \t\r\n"), "\n")
	// The README opens with HTML paragraphs for GitHub (banner, badges, links
	// to the docs). The site has its own header, so they stop at the title.
	for len(lines) > 0 && strings.HasPrefix(lines[0], "<p") {
		end := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "</p>") })
		if end < 0 {
			break
		}
		lines = lines[end+1:]
		for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
			lines = lines[1:]
		}
	}
	if len(lines) > 0 && strings.HasPrefix(lines[0], "# ") {
		lines = lines[1:]
		for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
			lines = lines[1:]
		}
	}
	head := frontmatter("Avrea CLI", overviewDescription, "avr, the Avrea command-line client")
	return finish(append(head, lines...))
}
func sidebar(tree *Tree, overview bool) string {
	out := []string{
		"// Auto-generated from the avr command tree by `avr internal docs`.",
		"// Do not edit by hand — regenerate via `make docs`.", "",
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
		files["index.md"] = overviewPage(*readme)
	}
	for _, node := range tree.top() {
		files["reference/"+node.Name+".md"] = commandPage(node)
	}
	return files
}
