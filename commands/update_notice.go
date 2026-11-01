package commands

import (
	"fmt"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

// Notifier scaffold only: there is no release lookup, cache, network client,
// goroutine, or update installation. Future producers may offer a ready notice.
// Never wait on them, including during shutdown, and never alter stdout.
func printUpdateNotice(cmd *cobra.Command, d Dependencies) {
	if d.UpdateNotices == nil || cmd == nil || cmd.Context().Err() != nil ||
		d.Getenv("AVR_NO_UPDATE_NOTIFIER") != "" || d.Getenv("CI") != "" ||
		d.Getenv("AVR_IN_AUTOMATION") != "" || !d.IsTerminal() || !d.IsErrorTerminal() {
		return
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" || c.Name() == "version" {
			return
		}
	}
	if cmd.Parent() == nil {
		return
	}
	for _, name := range []string{"help", "version", "json", "jq", "format"} {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			return
		}
	}
	select {
	case notice, ok := <-d.UpdateNotices:
		if ok && strings.TrimSpace(notice) != "" {
			_, _ = fmt.Fprintln(d.Err, output.CellText(notice))
		}
	default:
	}
}
