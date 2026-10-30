package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func prepareGlobalOptions(cmd *cobra.Command, d Dependencies) error {
	flags := cmd.Root().PersistentFlags()
	if !flags.Changed("links") && !flags.Changed("no-links") {
		value := d.Getenv("AVR_LINKS")
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "0", "1", "f", "false", "n", "no", "off", "on", "t", "true", "y", "yes":
		default:
			return &ExitError{2, "Error: Invalid value for '--links': " + pythonQuote(output.CellText(value)) +
				" is not a valid boolean. Recognized values: , 0, 1, f, false, n, no, off, on, t, true, y, yes"}
		}
	}
	raw := strings.ToLower(strings.TrimSpace(d.Getenv("AVR_DEBUG")))
	switch raw {
	case "", "1", "true", "yes", "on":
		return nil
	}
	unknown := map[string]bool{}
	for _, category := range strings.Split(raw, ",") {
		category = strings.TrimSpace(category)
		switch category {
		case "", "api", "timing": // Timing is the opt-in native instrumentation extension.
		default:
			unknown[category] = true
		}
	}
	if len(unknown) > 0 {
		categories := make([]string, 0, len(unknown))
		for category := range unknown {
			categories = append(categories, category)
		}
		sort.Strings(categories)
		noun := "category"
		if len(categories) > 1 {
			noun = "categories"
		}
		_, err := fmt.Fprintf(d.Err, "Warning: AVR_DEBUG ignored unknown %s: %s. Known: api.\n",
			noun, strings.Join(categories, ", "))
		return err
	}
	return nil
}
