package commands

import (
	"fmt"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

// positionalArguments retains Python's named argument diagnostics. A final
// name ending in ... accepts one or more values for that positional.
func positionalArguments(names ...string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) < len(names) {
			return &ExitError{2, "Error: Missing argument '" + names[len(args)] + "'."}
		}
		variadic := len(names) > 0 && strings.HasSuffix(names[len(names)-1], "...")
		if !variadic && len(args) > len(names) {
			noun := "argument"
			if len(args)-len(names) > 1 {
				noun = "arguments"
			}
			return &ExitError{2, fmt.Sprintf("Error: Got unexpected extra %s (%s)", noun, output.CellText(strings.Join(args[len(names):], " ")))}
		}
		return nil
	}
}
