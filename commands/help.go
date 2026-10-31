package commands

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/docgen"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

const helpLearnMore = "Use \"avr <command> <subcommand> --help\" for more information about a command.\nRead the docs at https://docs.avrea.com/cli"

func helpWidth(d Dependencies) int {
	columns := 80
	if value, err := strconv.Atoi(d.Getenv("COLUMNS")); err == nil && value > 0 {
		columns = value
	} else if file, ok := d.Out.(*os.File); ok {
		if value, _, err := term.GetSize(int(file.Fd())); err == nil && value > 0 {
			columns = value
		}
	}
	return max(50, min(columns, 80)-2)
}
func canonicalHelpPath(c *cobra.Command) string {
	return "avr" + strings.TrimPrefix(c.CommandPath(), c.Root().Name())
}
func helpFlag(c *cobra.Command, name string) *pflag.Flag {
	if strings.HasPrefix(name, "--") {
		return c.LocalFlags().Lookup(name[2:])
	}
	if strings.HasPrefix(name, "-") {
		return c.LocalFlags().ShorthandLookup(name[1:])
	}
	return nil
}
func helpOptions(c *cobra.Command, spec docgen.HelpSpec) []helpRow {
	c.InitDefaultHelpFlag()
	used := map[string]bool{}
	rows := []helpRow{}
	for _, option := range spec.Options {
		visible := false
		for _, name := range option.Names {
			if flag := helpFlag(c, name); flag != nil {
				used[flag.Name] = true
				visible = visible || !flag.Hidden
			}
		}
		if visible {
			rows = append(rows, helpRow{option.Label, option.Description})
		}
	}
	c.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if used[flag.Name] || flag.Hidden {
			return
		}
		label := "--" + flag.Name
		if flag.Shorthand != "" {
			label = "-" + flag.Shorthand + ", " + label
		}
		if flag.NoOptDefVal == "" {
			label += " " + strings.ToUpper(flag.Value.Type())
		}
		description := flag.Usage
		if flag.DefValue != "" && flag.DefValue != "false" && flag.DefValue != "0" && flag.DefValue != "[]" {
			description += "  [default: " + flag.DefValue + "]"
		}
		rows = append(rows, helpRow{label, description})
	})
	return rows
}
func helpCommands(c *cobra.Command, spec docgen.HelpSpec) ([]string, map[string][]helpRow) {
	descriptions := map[string]docgen.HelpCommand{}
	for _, child := range spec.Commands {
		descriptions[child.Name] = child
	}
	order := spec.SectionOrder
	if len(order) == 0 {
		order = []string{"AVAILABLE COMMANDS"}
	}
	rows := map[string][]helpRow{}
	for _, child := range c.Commands() {
		// Cobra's convenience help command is not part of the Python surface.
		if child.Hidden || child.Name() == "help" && child.Short == "Help about any command" {
			continue
		}
		entry, known := descriptions[child.Name()]
		description := child.Short
		section := order[0]
		if known {
			description = entry.Description
			if entry.Section != nil {
				section = *entry.Section
			}
		}
		if len(spec.SectionOrder) > 0 && !known {
			section = "Additional Commands"
		}
		label := child.Name()
		if spec.Sections {
			label += ":"
		}
		rows[section] = append(rows[section], helpRow{label, description})
	}
	return order, rows
}
func renderHelp(c *cobra.Command, d Dependencies, spec docgen.HelpSpec, stderr bool) string {
	width := helpWidth(d)
	if stderr {
		d.IsTerminal = d.IsErrorTerminal
	}
	pieces := spec.Usage
	if len(pieces) == 0 {
		pieces = []string{"[OPTIONS]"}
		pieces = append(pieces, strings.Fields(c.Use)[1:]...)
		if c.HasSubCommands() {
			pieces = append(pieces, "COMMAND", "[ARGS]...")
		}
	}
	usage := c.CommandPath() + " " + strings.Join(pieces, " ")
	body := spec.Help
	if body == "" {
		body = c.Long
		if body == "" {
			body = c.Short
		}
	}
	options := helpOptions(c, spec)
	order, children := helpCommands(c, spec)
	var out strings.Builder
	if spec.Sections {
		if body != "" {
			out.WriteString(body + "\n")
		}
		heading := func(title string) { out.WriteString("\n" + d.paint(strings.ToUpper(title), "1") + "\n") }
		heading("USAGE")
		out.WriteString("  " + usage + "\n")
		for _, section := range order {
			if len(children[section]) > 0 {
				heading(section)
				out.WriteString(helpTable(children[section], width))
			}
		}
		own, inherited := []helpRow{}, []helpRow{}
		for _, row := range options {
			if strings.HasPrefix(strings.TrimSpace(row.label), "--help") {
				inherited = append(inherited, row)
			} else {
				own = append(own, row)
			}
		}
		if len(own) > 0 {
			heading("FLAGS")
			out.WriteString(helpTable(own, width))
		}
		if len(inherited) > 0 {
			heading("INHERITED FLAGS")
			out.WriteString(helpTable(inherited, width))
		}
		heading("LEARN MORE")
		for _, line := range strings.Split(helpLearnMore, "\n") {
			out.WriteString("  " + line + "\n")
		}
	} else {
		out.WriteString("Usage: " + usage + "\n")
		if body != "" {
			out.WriteString("\n" + helpParagraphs(body, width, 2) + "\n")
		}
		if len(options) > 0 {
			out.WriteString("\nOptions:\n" + helpTable(options, width))
		}
		anyChildren := []helpRow{}
		for _, section := range order {
			anyChildren = append(anyChildren, children[section]...)
		}
		if len(anyChildren) > 0 {
			out.WriteString("\nCommands:\n" + helpTable(anyChildren, width))
		}
		if spec.Epilog != "" {
			out.WriteString("\n" + helpParagraphs(spec.Epilog, width, 2) + "\n")
		}
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}
func configureHelp(root *cobra.Command, d Dependencies) {
	root.CompletionOptions.DisableDefaultCmd = true
	get := func(c *cobra.Command) (docgen.HelpSpec, error) {
		spec, _, err := docgen.HelpTemplate(canonicalHelpPath(c))
		return spec, err
	}
	previous := root.HelpFunc()
	root.SetHelpFunc(func(c *cobra.Command, args []string) {
		spec, err := get(c)
		if err != nil {
			previous(c, args)
			return
		}
		color := d
		if c == root && c.Flags().Changed("help") {
			// Python's root help callback is eager: it renders before the
			// root callback applies --no-color / NO_COLOR. Preserve that
			// existing behavior only for explicit top-level help.
			getenv := color.Getenv
			color.Getenv = func(key string) string {
				if key == "NO_COLOR" {
					return ""
				}
				return getenv(key)
			}
			color.ColorEnabled = nil
		}
		_, _ = io.WriteString(c.OutOrStdout(), renderHelp(c, color, spec, false))
	})
	var groups func(*cobra.Command)
	groups = func(c *cobra.Command) {
		if c != root && c.HasSubCommands() && c.RunE == nil && c.Run == nil {
			c.RunE = run(func(cmd *cobra.Command, args []string) error {
				if len(args) > 0 {
					return &ExitError{2, "Error: No such command " + pythonQuote(args[0]) + "."}
				}
				spec, err := get(cmd)
				if err != nil {
					return err
				}
				if _, err = io.WriteString(cmd.ErrOrStderr(), renderHelp(cmd, d, spec, true)); err != nil {
					return err
				}
				return &ExitError{Code: 2}
			})
		}
		for _, child := range c.Commands() {
			groups(child)
		}
	}
	groups(root)
	root.RunE = run(func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return &ExitError{2, "Error: No such command " + pythonQuote(args[0]) + "."}
		}
		cfg, _, err := d.configuration()
		if err != nil {
			return err
		}
		if cfg.Token == "" {
			_, err = fmt.Fprintf(d.Out, "Welcome to Avrea CLI.\n\n  %s Run %s to sign in.\n  %s Run %s to see all commands.\n",
				d.paint("→", "36"), d.paint("avr auth login", "1"), d.paint("→", "36"), d.paint("avr --help", "1"))
			return err
		}
		spec, err := get(cmd)
		if err != nil {
			return err
		}
		_, err = io.WriteString(d.Out, renderHelp(cmd, d, spec, false))
		return err
	})
}
