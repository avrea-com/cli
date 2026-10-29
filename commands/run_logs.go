package commands

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
)

func fetchRunLogs(ctx context.Context, api *client.Client, job string) ([]map[string]any, error) {
	return fetchJobLogs(ctx, api, job, logOptions{})
}

func runLogs(ctx context.Context, d Dependencies, api *client.Client, jobs []map[string]any, failed bool, host, slug string) (string, error) {
	var b strings.Builder
	for _, job := range jobs {
		id := fieldText(job, "job_id")
		if id == "" || failed && fieldText(job, "conclusion") == "success" {
			continue
		}
		target := ""
		if slug != "" {
			target = consoleURL(host) + "/org/" + url.PathEscape(slug) + "/jobs/" + url.PathEscape(id)
		}
		header := "=== " + output.CellText(fieldText(job, "job_name")) + " (" + output.CellText(id) + ") ==="
		fmt.Fprintf(&b, "\n%s\n%s\n", strings.Repeat("=", 60), linkCell(d.paint(header, "1"), target))
		failedSteps := []map[string]any{}
		if failed {
			for _, step := range objectList(job["steps"]) {
				conclusion := fieldText(step, "conclusion")
				if conclusion != "" && conclusion != "success" && conclusion != "skipped" {
					failedSteps = append(failedSteps, step)
				}
			}
			if len(failedSteps) == 0 {
				b.WriteString("No failed steps.\n")
				continue
			}
		}
		entries, err := fetchRunLogs(ctx, api, id)
		if err != nil {
			return "", err
		}
		if failed {
			for _, step := range failedSteps {
				name := fieldText(step, "name")
				printed := false
				for _, entry := range entries {
					if fieldText(entry, "step_name") != name {
						continue
					}
					if !printed {
						fmt.Fprintf(&b, "\n%s\n", linkCell(d.paint("--- "+d.indicator(step, "status")+" "+output.CellText(name)+" ("+output.CellText(fieldText(step, "conclusion"))+") ---", "1"), target))
						printed = true
					}
					writeStyledLogLine(&b, entry, d, target)
				}
			}
		} else {
			current := ""
			for _, entry := range entries {
				name := fieldText(entry, "step_name")
				if name == "" {
					name = "(unknown step)"
				}
				if name != current {
					fmt.Fprintf(&b, "\n%s\n", linkCell(d.paint("--- "+output.CellText(name)+" ---", "1"), target))
					current = name
				}
				writeStyledLogLine(&b, entry, d, target)
			}
		}
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

var logSGR = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func writeStyledLogLine(b *strings.Builder, entry map[string]any, d Dependencies, target string) {
	content := fieldText(entry, "content")
	if strings.HasPrefix(content, "##[endgroup]") {
		return
	}
	codes := map[string]string{"error": "31", "warning": "33", "notice": "36", "debug": "90"}[fieldText(entry, "level")]
	if strings.HasPrefix(content, "##[group]") {
		codes = "1"
		content = strings.TrimPrefix(content, "##[group]")
	} else if strings.HasPrefix(content, "##[command]") {
		codes = "36"
		content = strings.TrimPrefix(content, "##[command]")
	}
	timestamp := fieldText(entry, "timestamp")
	if _, after, ok := strings.Cut(timestamp, "T"); ok {
		timestamp = after
	}
	if len(timestamp) > 12 {
		timestamp = timestamp[:12]
	}
	timestamp = strings.TrimRight(timestamp, "Z")
	if timestamp != "" {
		b.WriteString(linkCell(d.paint(output.CellText(timestamp), gray), target) + " ")
	}
	// Keep log newlines useful while escaping terminal control sequences.
	for i, line := range strings.Split(content, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		var safe strings.Builder
		offset := 0
		for _, span := range logSGR.FindAllStringIndex(line, -1) {
			safe.WriteString(logText(line[offset:span[0]]))
			if d.paint("x", "1") != "x" {
				safe.WriteString(line[span[0]:span[1]])
			}
			offset = span[1]
		}
		safe.WriteString(logText(line[offset:]))
		b.WriteString(d.paint(safe.String(), codes))
	}
	b.WriteByte('\n')
}

// Log bodies preserve horizontal indentation; table cells still escape tabs.
func logText(value string) string {
	parts := strings.Split(value, "\t")
	for i, part := range parts {
		parts[i] = output.CellText(part)
	}
	return strings.Join(parts, "\t")
}
