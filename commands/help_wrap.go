package commands

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

var helpWordPattern = lazyRegexp(`[^\t\n\r\v\f ]+`)

func helpLength(value string) int { return utf8.RuneCountInString(value) }
func helpWords(value string, width int) []string {
	if width < 1 {
		width = 1
	}
	out := []string{}
	line := ""
	previous := 0
	for _, location := range helpWordPattern().FindAllStringIndex(value, -1) {
		word := value[location[0]:location[1]]
		gap := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return ' '
			}
			return r
		}, value[previous:location[0]])
		previous = location[1]
		for word != "" {
			space := 0
			if line != "" {
				space = helpLength(gap)
				if space == 0 {
					space = 1
					gap = " "
				}
			}
			available := width - helpLength(line) - space
			if helpLength(word) <= available {
				if space > 0 {
					line += gap
				}
				line += word
				break
			}
			runes := []rune(word)
			split := 0
			for i := 1; i+1 < len(runes) && i+1 <= available; i++ {
				if runes[i] == '-' && unicode.IsLetter(runes[i-1]) && unicode.IsLetter(runes[i+1]) {
					split = i + 1
				}
			}
			if split == 0 && line == "" {
				split = min(width, len(runes))
			}
			if split > 0 {
				if space > 0 {
					line += gap
				}
				line += string(runes[:split])
				word = string(runes[split:])
			}
			if line != "" {
				out = append(out, line)
				line = ""
			}
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
func helpParagraphs(value string, width, indent int) string {
	value = strings.SplitN(value, "\f", 2)[0]
	out := []string{}
	block := []string{}
	flush := func() {
		if len(block) == 0 {
			return
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		prefix := strings.Repeat(" ", indent)
		if strings.TrimSpace(block[0]) == "\b" {
			for _, line := range block[1:] {
				out = append(out, prefix+line)
			}
		} else {
			extra := len(block[0]) - len(strings.TrimLeft(block[0], " "))
			prefix += strings.Repeat(" ", extra)
			for _, line := range helpWords(strings.Join(block, " "), width-indent-extra) {
				out = append(out, prefix+line)
			}
		}
		block = nil
	}
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
		} else {
			block = append(block, line)
		}
	}
	flush()
	return strings.Join(out, "\n")
}

type helpRow struct{ label, description string }

func helpTable(rows []helpRow, width int) string {
	if len(rows) == 0 {
		return ""
	}
	column := 0
	for _, row := range rows {
		column = max(column, helpLength(row.label))
	}
	column = min(column, 30) + 2
	var out strings.Builder
	for _, row := range rows {
		out.WriteString("  " + row.label)
		if row.description == "" {
			out.WriteByte('\n')
			continue
		}
		if helpLength(row.label) <= column-2 {
			out.WriteString(strings.Repeat(" ", column-helpLength(row.label)))
		} else {
			out.WriteByte('\n')
			out.WriteString(strings.Repeat(" ", column+2))
		}
		lines := helpWords(row.description, max(width-column-2, 10))
		for i, line := range lines {
			if i > 0 {
				out.WriteString(strings.Repeat(" ", column+2))
			}
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}
