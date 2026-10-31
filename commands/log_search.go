package commands

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var logSearchFields = []string{"id", "vm_id", "line_number", "content", "stream", "level", "timestamp", "score", "repository_id", "group_name", "step_name", "step_record_id"}

// NewLog returns repository-scoped full-text log search.
func NewLog(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	group := &cobra.Command{Use: "log", Aliases: []string{"logs"}, Short: "Search across runner execution logs."}
	var org, repo, query, stream, level, vm string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "search", Short: "Search logs for a repository.", Args: cobra.NoArgs}
	f := c.Flags()
	o.flags(c)
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringVar(&repo, "repo", "", "Repository name or ID; otherwise detect from Git.")
	f.StringVar(&query, "query", "", "Full-text search query.")
	f.StringVar(&stream, "stream", "", "Filter by stdout or stderr.")
	f.StringVar(&level, "level", "", "Filter by debug, info, warning, or error.")
	f.StringVar(&vm, "vm-id", "", "Filter by execution/VM ID.")
	f.IntVarP(&limit, "limit", "L", 100, "Maximum results to return.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		stream, level = strings.ToLower(stream), strings.ToLower(level)
		if f.Changed("stream") && !slices.Contains([]string{"stdout", "stderr"}, stream) {
			return &ExitError{2, "Error: invalid --stream: " + output.CellText(stream)}
		}
		if f.Changed("level") && !slices.Contains([]string{"debug", "info", "warning", "error"}, level) {
			return &ExitError{2, "Error: invalid --level: " + output.CellText(level)}
		}
		if done, err := o.prepare(cmd, d, logSearchFields); done || err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		id, err := resolveOrg(cmd.Context(), d, cfg, api, org)
		if err != nil {
			return err
		}
		rid, err := requiredWorkflowRepo(cmd.Context(), d, cfg, api, id, repo)
		if err != nil {
			return err
		}
		payload := map[string]any{"repository_ids": []string{rid}, "limit": limit}
		for k, v := range map[string]string{"query": query, "stream": stream, "level": level, "vm_id": vm} {
			if v != "" {
				payload[k] = v
			}
		}
		result, err := api.SearchLogs(cmd.Context(), payload)
		if err != nil {
			return apiError(cfg, err, "search logs")
		}
		rows := objectList(result.Fields["results"])
		if f.Changed("json") {
			return o.list(cmd, d, rows, logSearchFields)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No logs found.")
			return err
		}
		total := strconv.Itoa(len(rows))
		if value, ok := result.Fields["total_estimated"]; ok {
			total = pythonValue(value)
		}
		suffix := ""
		if result.Fields["has_more"] == true {
			suffix = " (more available)"
		}
		if _, err = fmt.Fprintf(d.Out, "Found %s log entries%s\n\n", total, suffix); err != nil {
			return err
		}
		for _, row := range rows {
			stamp := []rune(fieldText(row, "timestamp"))
			if len(stamp) > 19 {
				stamp = stamp[:19]
			}
			level := diagnosticDefaultText(row, "level", "info")
			level = truncateSearchField(strings.ToUpper(level), 5)
			source := truncateSearchField(diagnosticDefaultText(row, "stream", "stdout"), 6)
			codes := map[string]string{"ERROR": "31", "WARNI": "33", "DEBUG": "36"}[level]
			number := "0"
			if value, ok := row["line_number"]; ok {
				number = pythonValue(value)
			}
			prefix := fmt.Sprintf("[%s] %s %s L%s", output.CellText(string(stamp)), d.paint(output.CellText(level), codes), output.CellText(source), padded(number, 6, true))
			content := searchLogContent(d, fieldText(row, "content"))
			if _, err = fmt.Fprintf(d.Out, "%s: %s\n", prefix, content); err != nil {
				return err
			}
		}
		return nil
	})
	group.AddCommand(c)
	return group
}
func truncateSearchField(text string, width int) string {
	runes := []rune(text)
	if len(runes) > width {
		runes = runes[:width]
	}
	return string(runes) + strings.Repeat(" ", width-len(runes))
}

// Keep readable log indentation/newlines and SGR colors, but escape other
// terminal controls. JSON always retains the original content.
func searchLogContent(d Dependencies, content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		var b strings.Builder
		offset := 0
		for _, span := range logSGR.FindAllStringIndex(line, -1) {
			b.WriteString(logText(line[offset:span[0]]))
			if d.paint("x", "1") != "x" {
				b.WriteString(line[span[0]:span[1]])
			}
			offset = span[1]
		}
		b.WriteString(logText(line[offset:]))
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}
