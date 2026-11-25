package commands

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

// validateCursor checks a --cursor value given on the command line.
func validateCursor(c *cobra.Command, cursor string) error {
	if c.Flags().Changed("cursor") && (cursor == "" || strings.TrimSpace(cursor) != cursor || utf8.RuneCountInString(cursor) > 512) {
		return &ExitError{2, "Error: --cursor must be 1..512 chars and not surrounded by whitespace"}
	}
	return nil
}

// printNextCursor tells how to fetch the next page, on standard error so that
// it stays out of the listing itself.
func printNextCursor(d Dependencies, next string, asJSON bool) error {
	if next == "" {
		return nil
	}
	prefix := "\nMore results available. Next page: --cursor "
	if asJSON {
		prefix = "next_cursor: "
	}
	_, err := fmt.Fprintln(d.Err, prefix+output.CellText(shellQuotePython(next)))
	return err
}

// withHint adds a hint after the first line of an error and keeps its exit
// code.
func withHint(err error, hint string) error {
	first, rest, found := strings.Cut(err.Error(), "\n")
	if found {
		rest = "\n" + rest
	}
	code := 1
	var exit *ExitError
	if errors.As(err, &exit) {
		code = exit.Code
	}
	if !strings.HasPrefix(first, "Error: ") {
		first = "Error: " + first
	}
	return &ExitError{code, first + "\n  Hint: " + hint + rest}
}
