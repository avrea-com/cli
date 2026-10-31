package output

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// CellText keeps untrusted text in one terminal/TSV cell. JSON retains the
// original data; TSV follows the existing Python escaping contract.
func CellText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\t':
			b.WriteString("\\t")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		default:
			unsafe := r < 32 || r >= 127 && r <= 159 || r == 0x61c || r == 0x200e || r == 0x200f || r >= 0x2028 && r <= 0x202e || r >= 0x2066 && r <= 0x2069
			if unsafe {
				if r < 256 {
					fmt.Fprintf(&b, "\\x%02x", r)
				} else {
					fmt.Fprintf(&b, "\\u%04x", r)
				}
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// Table optionally links terminal cells. Links are added only after escaping
// and measuring labels; untrusted values can never supply terminal controls.
func Table(w io.Writer, headers []string, rows [][]string, piped bool, targets ...[][]string) error {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	escaped := make([][]string, len(rows))
	for i, row := range rows {
		escaped[i] = make([]string, len(headers))
		for j := range headers {
			if j < len(row) {
				escaped[i][j] = CellText(row[j])
			}
			if n := utf8.RuneCountInString(escaped[i][j]); n > widths[j] {
				widths[j] = n
			}
		}
	}
	emit := func(row []string, index int) error {
		var b strings.Builder
		for j, label := range row {
			if j > 0 {
				if piped {
					b.WriteByte('\t')
				} else {
					b.WriteString("  ")
				}
			}
			cell := label
			if !piped && index >= 0 && len(targets) > 0 && index < len(targets[0]) && j < len(targets[0][index]) && targets[0][index][j] != "" {
				cell = "\x1b]8;;" + CellText(targets[0][index][j]) + "\x1b\\" + label + "\x1b]8;;\x1b\\"
			}
			b.WriteString(cell)
			if !piped && j < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-utf8.RuneCountInString(label)))
			}
		}
		_, err := fmt.Fprintln(w, b.String())
		return err
	}
	if err := emit(headers, -1); err != nil {
		return err
	}
	for i, row := range escaped {
		if err := emit(row, i); err != nil {
			return err
		}
	}
	return nil
}

// Grid preserves the bordered organization table used by both TTY and pipes.
var gridWidth = &runewidth.Condition{EastAsianWidth: false, StrictEmojiNeutral: true}

func Grid(w io.Writer, headers []string, rows [][]string) error {
	widths := make([]int, len(headers))
	all := make([][]string, 0, len(rows)+1)
	all = append(all, headers)
	all = append(all, rows...)
	for _, row := range all {
		for i, text := range row {
			if n := gridWidth.StringWidth(CellText(text)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	var b strings.Builder
	border := func() {
		b.WriteByte('+')
		for _, width := range widths {
			b.WriteString(strings.Repeat("-", width+2))
			b.WriteByte('+')
		}
		b.WriteByte('\n')
	}
	border()
	for index, row := range all {
		b.WriteByte('|')
		for i, text := range row {
			text = CellText(text)
			b.WriteString(" " + text + strings.Repeat(" ", widths[i]-gridWidth.StringWidth(text)) + " |")
		}
		b.WriteByte('\n')
		if index == 0 {
			border()
		}
	}
	border()
	_, err := io.WriteString(w, b.String())
	return err
}
