package commands

import (
	"fmt"
	"github.com/avrea-com/cli/output"
)

// Notifications are terminal behavior, independent of color and stdout piping.
func notifyCompletion(d Dependencies, message string) {
	if d.IsErrorTerminal() {
		_, _ = fmt.Fprintf(d.Err, "\x1b]9;%s\x07", output.CellText(message))
	}
}
