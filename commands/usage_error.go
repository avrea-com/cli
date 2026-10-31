package commands

import (
	"strings"

	"github.com/avrea-com/cli/docgen"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// usageError mirrors Click's concise parser diagnostic without rendering full help.
// Shared execution also uses it for required flags, including composed admin trees.
func usageError(c *cobra.Command, message string) error {
	spec, _, _ := docgen.HelpTemplate(canonicalHelpPath(c))
	pieces := spec.Usage
	if len(pieces) == 0 {
		pieces = append([]string{"[OPTIONS]"}, strings.Fields(c.Use)[1:]...)
	}
	for _, param := range spec.CompletionParams {
		for _, name := range param.Names {
			if message == "Missing option '"+name+"'." && len(param.Type.Choices) > 0 {
				message += " Choose from:\n\t" + strings.Join(param.Type.Choices, ",\n\t")
			}
		}
	}
	path := c.CommandPath()
	return &ExitError{2, "Usage: " + path + " " + strings.Join(pieces, " ") + "\nTry '" + path + " --help' for help.\n\nError: " + message}
}

func requiredOptionError(c *cobra.Command, err error) error {
	if c == nil {
		return err
	}
	if strings.HasPrefix(err.Error(), "Error: Missing option '") {
		return usageError(c, strings.TrimPrefix(err.Error(), "Error: "))
	}
	if !strings.HasPrefix(err.Error(), "required flag(s) ") {
		return err
	}
	missing := func(f *pflag.Flag) bool {
		return f != nil && !f.Changed && len(f.Annotations[cobra.BashCompOneRequiredFlag]) > 0
	}
	// Click reports the first missing parameter in declaration order, not alphabetically.
	spec, _, _ := docgen.HelpTemplate(canonicalHelpPath(c))
	for _, param := range spec.CompletionParams {
		for _, name := range param.Names {
			if flag := helpFlag(c, name); missing(flag) {
				return usageError(c, "Missing option '--"+flag.Name+"'.")
			}
		}
	}
	var first string
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if first == "" && missing(f) {
			first = f.Name
		}
	})
	if first != "" {
		return usageError(c, "Missing option '--"+first+"'.")
	}
	return err
}
