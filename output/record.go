package output

import (
	"fmt"
	"io"
	"unicode/utf8"
)

// KeyValues preserves the insertion order and alignment of Python records.
func KeyValues(w io.Writer, pairs [][2]string) error {
	width := 0
	for _, pair := range pairs {
		width = max(width, utf8.RuneCountInString(pair[0]))
	}
	for _, pair := range pairs {
		if _, err := fmt.Fprintf(w, "%-*s %s\n", width+1, CellText(pair[0]), CellText(pair[1])); err != nil {
			return err
		}
	}
	return nil
}
