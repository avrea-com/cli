package commands

import (
	"fmt"
	"strings"

	"github.com/avrea-com/cli/output"
)

const gray = "38;5;245"

func (d Dependencies) paint(text, codes string) string {
	if !d.IsTerminal() || d.Getenv("NO_COLOR") != "" || d.ColorEnabled != nil && !d.ColorEnabled() || codes == "" {
		return text
	}
	return "\x1b[" + codes + "m" + text + "\x1b[0m"
}

func (d Dependencies) errorPaint(text, codes string) string {
	d.IsTerminal = d.IsErrorTerminal
	return d.paint(text, codes)
}
func (d Dependencies) hint(text string) string { return d.errorPaint(text, gray) }

func statusSymbol(state, conclusion string) (string, string) {
	if conclusion != "" {
		switch conclusion {
		case "success":
			return "✓", "32"
		case "failure", "timed_out", "startup_failure":
			return "✗", "31"
		case "action_required":
			return "●", "33"
		default:
			return "—", gray
		}
	}
	if state == "in_progress" {
		return "●", "33"
	}
	return "○", gray
}

func (d Dependencies) indicator(fields map[string]any, key string) string {
	symbol, color := statusSymbol(fieldText(fields, key), fieldText(fields, "conclusion"))
	return d.paint(symbol, color)
}

func linkCell(label, target string) string {
	if target == "" {
		return label
	}
	return "\x1b]8;;" + output.CellText(target) + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func padded(text string, width int, right bool) string {
	text = output.CellText(text)
	if right {
		return fmt.Sprintf("%*s", width, text)
	}
	return fmt.Sprintf("%-*s", width, text)
}

func (d Dependencies) repoTable(rows, targets [][]string) error {
	widths := []int{50, 36, 10, 12}
	headers := []string{"REPOSITORY", "ID", "PLATFORM", "PLATFORM ID"}
	cells := make([]string, len(headers))
	for i, h := range headers {
		cells[i] = d.paint(padded(h, widths[i], false), gray+";4")
	}
	if _, err := fmt.Fprintln(d.Out, "  "+strings.Join(cells, " ")); err != nil {
		return err
	}
	for i, row := range rows {
		codes := []string{"1", "36", "2", "2"}
		for j, value := range row {
			if j == 0 {
				value = shortCell(output.CellText(value), 48)
			}
			if j < 3 {
				value = padded(value, widths[j], false)
			} else {
				value = output.CellText(value)
			}
			cells[j] = d.paint(value, codes[j])
			if i < len(targets) && j < len(targets[i]) {
				cells[j] = linkCell(cells[j], targets[i][j])
			}
		}
		if _, err := fmt.Fprintln(d.Out, "  "+strings.Join(cells, " ")); err != nil {
			return err
		}
	}
	return nil
}

func (d Dependencies) runTable(rows, targets [][]string, records []map[string]any) error {
	widths := []int{50, 30, 30, 18, 36, 12, 12}
	headers := []string{"TITLE", "WORKFLOW", "BRANCH", "EVENT", "ID", "ELAPSED", "AGE"}
	cells := make([]string, len(headers))
	for j, h := range headers {
		cells[j] = d.paint(padded(h, widths[j], false), gray+";4")
	}
	if _, err := fmt.Fprintln(d.Out, "  "+strings.Join(cells, " ")); err != nil {
		return err
	}
	for i, row := range rows {
		codes := []string{"", "35", "1", "", "36", "", "2"}
		for j, value := range row[1:] {
			if j < 3 {
				value = shortCell(output.CellText(value), widths[j]-2)
			}
			cells[j] = d.paint(padded(value, widths[j], j >= 5), codes[j])
			if i < len(targets) && j+1 < len(targets[i]) {
				cells[j] = linkCell(cells[j], targets[i][j+1])
			}
		}
		if _, err := fmt.Fprintln(d.Out, d.indicator(records[i], "status")+" "+strings.Join(cells, " ")); err != nil {
			return err
		}
	}
	return nil
}

func conclusionColor(conclusion string) string {
	switch conclusion {
	case "success":
		return "32"
	case "failure", "timed_out", "startup_failure":
		return "31"
	case "cancelled", "skipped", "neutral", "stale":
		return "90"
	default:
		return ""
	}
}
