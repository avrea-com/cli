package commands

import (
	"fmt"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func updateNoticeAllowed(cmd *cobra.Command, d Dependencies) bool {
	if cmd == nil || cmd.Context().Err() != nil ||
		d.Getenv("AVR_NO_UPDATE_NOTIFIER") != "" || d.Getenv("CI") != "" ||
		d.Getenv("AVR_IN_AUTOMATION") != "" || !d.IsTerminal() || !d.IsErrorTerminal() {
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" || c.Name() == "version" {
			return false
		}
	}
	if cmd.Parent() == nil {
		return false
	}
	for _, name := range []string{"help", "version", "json", "jq", "format", "ndjson"} {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			return false
		}
	}
	return true
}

// Only consume an already-ready message, including during shutdown.
func printUpdateNotice(cmd *cobra.Command, d Dependencies) {
	if d.UpdateNotices == nil || !updateNoticeAllowed(cmd, d) {
		return
	}
	select {
	case notice, ok := <-d.UpdateNotices:
		if ok && strings.TrimSpace(notice) != "" {
			_, _ = fmt.Fprintln(d.Err, output.CellText(notice))
		}
	default:
	}
}
