package docgen_test

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/avrea-com/cli/commands"
	"github.com/avrea-com/cli/docgen"
	buildversion "github.com/avrea-com/cli/internal/version"
	"github.com/spf13/cobra"
)

func TestDocumentationOmitsReleaseVersion(t *testing.T) {
	tree, err := commands.Documentation(commands.NewRoot(commands.Dependencies{Version: "custom-preview+fixture"}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := docgen.JSON(tree)
	if err != nil {
		t.Fatal(err)
	}
	var reference map[string]any
	if err := json.Unmarshal([]byte(raw), &reference); err != nil {
		t.Fatal(err)
	}
	if _, exists := reference["version"]; exists {
		t.Error("reference JSON must not carry a release version")
	}
	outputs := map[string]string{"reference.json": raw, "REFERENCE.md": docgen.Markdown(tree)}
	for path, content := range docgen.Man(tree) {
		outputs["man/"+path] = content
	}
	for path, content := range docgen.Starlight(tree, nil) {
		outputs["starlight/"+path] = content
	}
	for path, content := range outputs {
		for _, version := range []string{buildversion.Current(), "custom-preview+fixture"} {
			if strings.Contains(content, version) {
				t.Errorf("%s contains release version %q", path, version)
			}
		}
	}
}

func findNode(tree *docgen.Tree, name string) *docgen.Node {
	for _, nodes := range tree.Sections {
		for _, node := range nodes {
			if node.Name == name {
				return node
			}
		}
	}
	return nil
}
func TestDocumentationTracksLiveCommandsFlagsDefaultsAndAliases(t *testing.T) {
	root := commands.NewRoot(commands.Dependencies{})
	repo, _, err := root.Find([]string{"repo"})
	if err != nil {
		t.Fatal(err)
	}
	root.RemoveCommand(repo)
	extra := &cobra.Command{Use: "example WIDGET", Short: "Document a new widget.", Aliases: []string{"widgets"}}
	extra.Flags().Int("limit", 7, "Maximum widgets.")
	root.AddCommand(extra, &cobra.Command{Use: "hidden-example", Short: "Hidden extension.", Hidden: true})
	vm, _, err := root.Find([]string{"vm", "list"})
	if err != nil {
		t.Fatal(err)
	}
	vm.Flags().String("new-filter", "all", "A live new filter.")
	vm.Flags().Lookup("limit").DefValue = "7"
	vm.Flags().Lookup("state").DefValue = "RUNNING"
	vm.Flags().StringArray("tag", []string{"a,b", "quoted word"}, "Default tags.")
	vm.Flags().IntSlice("project-id", []int{11, 22}, "Project IDs.")
	tree, err := commands.Documentation(root)
	if err != nil {
		t.Fatal(err)
	}
	if findNode(tree, "repo") != nil || findNode(tree, "hidden-example") != nil {
		t.Fatal("removed or hidden command documented")
	}
	if _, ok := tree.Aliases["repos"]; ok {
		t.Fatal("removed command alias documented")
	}
	if tree.Aliases["widgets"] != "example" {
		t.Fatal(tree.Aliases)
	}
	node := findNode(tree, "example")
	if node == nil || node.Usage != "avr example [OPTIONS] WIDGET" || len(node.Params) != 2 {
		t.Fatal(node)
	}
	var listing *docgen.Node
	for _, sub := range findNode(tree, "vm").Subcommands {
		if sub.Name == "list" {
			listing = sub
		}
	}
	if listing == nil {
		t.Fatal("missing VM list")
	}
	found := false
	for _, p := range listing.Params {
		if p.Name == "new_filter" {
			found = true
			if p.Default != "all" || p.Help == nil || *p.Help != "A live new filter." {
				t.Fatal(p)
			}
		}
		if p.Name == "state" && p.Default != "RUNNING" {
			t.Fatal("new default on optional flag omitted", p.Default)
		}
		if p.Name == "tag" && !reflect.DeepEqual(p.Default, []string{"a,b", "quoted word"}) {
			t.Fatal("array default omitted", p.Default)
		}
		if p.Name == "project_id" && (!p.Multiple || p.Type != "integer" || !reflect.DeepEqual(p.Default, []int64{11, 22})) {
			t.Fatalf("integer array documentation lost type/repeatability/default: %#v", p)
		}
		if p.Name == "limit" && p.Default != int64(7) {
			t.Fatal(p.Default)
		}
	}
	if !found {
		t.Fatal("new flag omitted")
	}
	if !strings.Contains(docgen.Markdown(tree), "--new-filter") {
		t.Fatal("live flag missing from Markdown")
	}
	files := docgen.Starlight(tree, nil)
	if _, ok := files["reference/example.md"]; !ok {
		t.Fatal("new Starlight page missing")
	}
	if _, ok := files["reference/repo.md"]; ok {
		t.Fatal("removed Starlight page remains")
	}
	if _, ok := docgen.Man(tree)["avr-example.1"]; !ok {
		t.Fatal("new man page missing")
	}
}
func TestDocumentationDropsRemovedFlags(t *testing.T) {
	root := commands.NewRoot(commands.Dependencies{})
	vm, _, err := root.Find([]string{"vm", "list"})
	if err != nil {
		t.Fatal(err)
	}
	vm.ResetFlags()
	tree, err := commands.Documentation(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range findNode(tree, "vm").Subcommands {
		if node.Name == "list" && len(node.Params) != 0 {
			t.Fatal("removed flags retained", node.Params)
		}
	}
}

func TestReferenceShowsLiveScopeAliases(t *testing.T) {
	tree, err := commands.Documentation(commands.NewRoot(commands.Dependencies{}))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	var visit func(*docgen.Node)
	visit = func(node *docgen.Node) {
		for _, p := range node.Params {
			if p.Name == "org" && node.Full != "avr vm ssh" && node.Full != "avr build" {
				checked++
				if !strings.Contains(strings.Join(p.Opts, ","), "-o,") && !strings.HasSuffix(strings.Join(p.Opts, ","), ",-o") {
					t.Fatal(node.Full, p.Opts)
				}
			}
		}
		for _, child := range node.Subcommands {
			visit(child)
		}
	}
	for _, nodes := range tree.Sections {
		for _, node := range nodes {
			visit(node)
		}
	}
	if checked == 0 {
		t.Fatal("no organization options inspected")
	}
}

// The README's GitHub header (banner, badges, links back to the docs) is not
// part of the docs site's overview page; the text after the title is.
func TestStarlightOverviewSkipsTheReadmeHeader(t *testing.T) {
	tree, err := commands.Documentation(commands.NewRoot(commands.Dependencies{}))
	if err != nil {
		t.Fatal(err)
	}
	readme := "<p align=\"center\">\n  <img src=\"banner.png\" />\n</p>\n" +
		"<p align=\"center\">\n  <a href=\"https://docs.example\">Documentation</a>\n</p>\n\n" +
		"# Avrea CLI\n\nOfficial command-line client.\n\n<p>Inline HTML later stays.</p>\n"
	page := docgen.Starlight(tree, &readme)["index.md"]
	if strings.Contains(page, "banner.png") || strings.Contains(page, "Documentation</a>") || strings.Contains(page, "# Avrea CLI") {
		t.Fatal(page)
	}
	if !strings.Contains(page, "Official command-line client.") || !strings.Contains(page, "<p>Inline HTML later stays.</p>") {
		t.Fatal(page)
	}
}

// The overview and reference index carry the docs site's search metadata:
// a browser title within 60 characters, an og:title and a description within
// 155, so results and link previews do not truncate them.
func TestStarlightLandingPagesSetSearchMetadata(t *testing.T) {
	tree, err := commands.Documentation(commands.NewRoot(commands.Dependencies{}))
	if err != nil {
		t.Fatal(err)
	}
	readme := "# Avrea CLI\n\nOfficial command-line client.\n"
	files := docgen.Starlight(tree, &readme)
	for _, path := range []string{"index.md", "reference/index.md"} {
		meta := map[string]string{}
		front, _, _ := strings.Cut(strings.TrimPrefix(files[path], "---\n"), "\n---\n")
		lines := strings.Split(front, "\n")
		for i, line := range lines {
			key, value, _ := strings.Cut(strings.TrimSpace(line), ": ")
			switch key {
			case "description":
				meta[key] = value
			case "- tag":
				if i+1 < len(lines) && value == "title" {
					meta["title"] = strings.TrimPrefix(strings.TrimSpace(lines[i+1]), "content: ")
				}
			case "property":
				if value == "og:title" && i+1 < len(lines) {
					meta["og:title"] = strings.TrimPrefix(strings.TrimSpace(lines[i+1]), "content: ")
				}
			}
		}
		for key, limit := range map[string]int{"title": 60, "og:title": 0, "description": 155} {
			var value string
			if err := json.Unmarshal([]byte(meta[key]), &value); err != nil || strings.TrimSpace(value) == "" {
				t.Errorf("%s: no %s in\n%s", path, key, front)
			} else if n := utf8.RuneCountInString(value); limit > 0 && n > limit {
				t.Errorf("%s: %s %q is %d characters, over %d", path, key, value, n, limit)
			}
		}
	}
}

// A reference page lists each positional argument under the name its usage
// line gives it, so `avr vm show [OPTIONS] VM_ID_OR_NAME` is followed by
// VM_ID_OR_NAME, not by the name of the parameter behind it.
var (
	commandHeading = regexp.MustCompile("(?m)^#+ ")
	plainArgument  = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// argumentName drops the optional brackets and repetition dots that usage
// lines and argument lists place differently.
func argumentName(token string) string {
	return strings.Trim(token, "[].")
}

// The JSON FIELDS list is read back from the help text, so a field name or
// wrapping the parser misses would fall back to a code block unnoticed.
func TestStarlightRendersEveryJSONFieldsListAsText(t *testing.T) {
	tree, err := commands.Documentation(commands.NewRoot(commands.Dependencies{}))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for path, page := range docgen.Starlight(tree, nil) {
		lines := strings.Split(page, "\n")
		for i, line := range lines {
			if !strings.HasPrefix(line, "**JSON FIELDS") {
				continue
			}
			checked++
			if i+2 >= len(lines) || !strings.HasPrefix(lines[i+2], "`") {
				t.Fatalf("%s: %s is not followed by an inline field list:\n%s", path, line, strings.Join(lines[i:min(i+6, len(lines))], "\n"))
			}
		}
	}
	if checked == 0 {
		t.Fatal("no JSON FIELDS list found")
	}
}
func TestStarlightArgumentsMatchTheUsageLine(t *testing.T) {
	tree, err := commands.Documentation(commands.NewRoot(commands.Dependencies{}))
	if err != nil {
		t.Fatal(err)
	}
	files := docgen.Starlight(tree, nil)
	checked := 0
	var visit func(*docgen.Node)
	visit = func(node *docgen.Node) {
		for _, child := range node.Subcommands {
			visit(child)
		}
		if node.IsGroup {
			return
		}
		usage := []string{}
		for _, token := range strings.Fields(strings.TrimPrefix(node.Usage, node.Full)) {
			if token != "[OPTIONS]" {
				usage = append(usage, argumentName(token))
			}
		}
		page := files["reference/"+node.Path[1]+".md"]
		_, section, _ := strings.Cut(page, "```sh\n"+node.Usage+"\n```")
		if next := commandHeading.FindStringIndex(section); next != nil {
			section = section[:next[0]]
		}
		listed := []string{}
		if _, arguments, ok := strings.Cut(section, "**Arguments**"); ok {
			arguments, _, _ = strings.Cut(arguments, "**Options**")
			for _, line := range strings.Split(arguments, "\n") {
				if _, rest, ok := strings.Cut(line, `<code class="cli-arg">`); ok {
					name, _, _ := strings.Cut(rest, "</code>")
					listed = append(listed, argumentName(name))
				}
			}
		}
		if len(listed) == 0 && len(usage) == 0 {
			return
		}
		checked++
		if len(listed) != len(usage) {
			t.Errorf("%s: usage arguments %v, listed %v", node.Full, usage, listed)
			return
		}
		for i := range usage {
			// A usage token that spells out choices, such as {on|off} or
			// <endpoint>, is listed under the parameter's name instead.
			if !plainArgument.MatchString(usage[i]) {
				listed[i], usage[i] = "", ""
			}
		}
		if !reflect.DeepEqual(listed, usage) {
			t.Errorf("%s: usage arguments %v, listed %v", node.Full, usage, listed)
		}
	}
	for _, nodes := range tree.Sections {
		for _, node := range nodes {
			visit(node)
		}
	}
	if checked == 0 {
		t.Fatal("no argument lists inspected")
	}
}

func TestArgumentNamesComeFromTheRenderedUsage(t *testing.T) {
	root := &cobra.Command{Use: "avr"}
	describe := map[string]docgen.Description{}
	add := func(use string, usage ...string) {
		c := &cobra.Command{Use: use, Run: func(*cobra.Command, []string) {}}
		root.AddCommand(c)
		describe[c.Name()] = docgen.Description{Usage: usage, Parameters: []docgen.Parameter{
			{Param: docgen.Param{Name: "value", Kind: "argument"}},
		}}
	}
	add("described ID", "[OPTIONS]", "JOB_ID")
	add("mismatched ID", "[OPTIONS] MESSAGE | status FEEDBACK_ID")
	add("literal {ON}")
	add("choices {on|off}")
	add("placeholder <endpoint>")
	add("repeated [ALIASES]...")
	add("variadic [TAGS...]")
	add("required NAMES...")
	tree, err := docgen.Build(root, func(c *cobra.Command) docgen.Description { return describe[c.Name()] }, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		command, metavar string
		required         bool
		nargs            int
	}{
		{"described", "JOB_ID", true, 1},
		{"mismatched", "ID", true, 1},
		{"literal", "", true, 1},
		{"choices", "", true, 1},
		{"placeholder", "", true, 1},
		{"repeated", "ALIASES", false, -1},
		{"variadic", "TAGS", false, -1},
		{"required", "NAMES", true, -1},
	} {
		params := findNode(tree, tt.command).Params
		if len(params) != 1 {
			t.Fatal(tt.command, params)
		}
		if p := params[0]; p.Name != "value" || p.Metavar != tt.metavar || p.Required != tt.required || p.Nargs != tt.nargs {
			t.Errorf("%s: %+v", tt.command, p)
		}
	}
}
