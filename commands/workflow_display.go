package commands

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/output"
)

func aggregatePct(row map[string]any, key string) string {
	completed := aggregateNumber(row, "completed_count")
	if completed == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*aggregateNumber(row, key)/completed)
}
func aggregateRate(d Dependencies, row map[string]any, value string) string {
	completed := aggregateNumber(row, "completed_count")
	if completed == 0 {
		return value
	}
	rate := 100 * aggregateNumber(row, "failure_count") / completed
	if rate >= 20 {
		return d.paint(value, "31")
	}
	if rate >= 5 {
		return d.paint(value, "33")
	}
	return value
}
func workflowHeaders(d Dependencies, labels []string, widths []int, rightAfter int, separator string) string {
	cells := make([]string, len(labels))
	for i, label := range labels {
		cells[i] = d.paint(padded(label, widths[i], i >= rightAfter), gray+";4")
	}
	return "  " + strings.Join(cells, separator)
}

func renderWorkflowList(d Dependencies, rows []map[string]any, total int, since string) error {
	if total == 0 {
		_, err := fmt.Fprintln(d.Out, "No workflow runs found in the selected window.")
		return err
	}
	var b strings.Builder
	labels := []string{"NAME", "REPOSITORY", "RUNS", "MEDIAN", "FAILURE", "FLAKE", "ID"}
	widths := []int{40, 30, 6, 12, 8, 8, 36}
	header := workflowHeaders(d, labels[:6], widths[:6], 2, " ") + " " + d.paint(padded("ID", 36, false), gray+";4")
	fmt.Fprintln(&b, header)
	for _, row := range rows {
		wf, _ := row["workflow"].(map[string]any)
		name := fieldText(wf, "name")
		if _, exists := wf["name"]; !exists {
			name = "(unknown)"
		}
		cells := []string{
			d.paint(padded(shortCell(name, 38), 40, false), "1"),
			d.paint(padded(shortCell(fieldText(wf, "repository_full_name"), 28), 30, false), "2"),
			padded(aggregateCount(row, "count"), 6, true),
			padded(formatDuration(row["median_duration_seconds"]), 12, true),
			aggregateRate(d, row, padded(aggregatePct(row, "failure_count"), 8, true)),
			padded(aggregatePct(row, "flaked_count"), 8, true),
			d.paint(output.CellText(fieldText(wf, "workflow_id")), "36"),
		}
		fmt.Fprintln(&b, "  "+strings.Join(cells, " "))
	}
	summary := fmt.Sprintf("Showing stats for the last %s (%d workflows).", output.CellText(since), len(rows))
	if total > len(rows) {
		summary = fmt.Sprintf("Showing top %d of %d workflows from the last %s.", len(rows), total, output.CellText(since))
	}
	fmt.Fprintln(&b, "\n"+d.paint(summary, "2")+"\n")
	if _, err := io.WriteString(d.Out, b.String()); err != nil {
		return err
	}
	hint := "To adjust the time window, try: avr workflow list --since 7d"
	if total > len(rows) {
		hint = "To see more, try: avr workflow list --limit 100"
	}
	_, err := fmt.Fprintln(d.Err, "To view a workflow, try: avr workflow view <wfl-id>\n"+hint)
	return err
}

func renderWorkflowSummary(d Dependencies, bucket map[string]any, host, slug string) error {
	var b strings.Builder
	wf, _ := bucket["workflow"].(map[string]any)
	name, repo, path := fieldText(wf, "name"), fieldText(wf, "repository_full_name"), fieldText(wf, "path")
	if name == "" {
		name = "(unknown)"
	}
	if repo == "" {
		repo = "-"
	}
	if path == "" {
		path = "-"
	}
	heading := d.paint(output.CellText(name), "97;1")
	if host != "" && fieldText(wf, "workflow_id") != "" {
		heading = linkCell(heading, host+"/org/"+slug+"/workflows/"+fieldText(wf, "workflow_id"))
	}
	fmt.Fprintln(&b, heading)
	fmt.Fprintln(&b, d.paint(output.CellText(repo+" -- "+path), "2")+"\n")
	values := []string{d.paint(aggregateCount(bucket, "count"), "1"), formatDuration(bucket["median_duration_seconds"]), formatDuration(bucket["p95_duration_seconds"]), aggregateRate(d, bucket, aggregatePct(bucket, "failure_count")), aggregatePct(bucket, "flaked_count")}
	for i, label := range []string{"Runs        ", "Median      ", "P95         ", "Failure rate", "Flake rate  "} {
		fmt.Fprintf(&b, "  %s  %s\n", d.paint(label, "2"), values[i])
	}
	jobs := objectList(bucket["jobs"])
	if len(jobs) > 0 {
		sort.SliceStable(jobs, func(i, j int) bool {
			return aggregateNumber(jobs[i], "median_duration_seconds") > aggregateNumber(jobs[j], "median_duration_seconds")
		})
		width := 10
		for _, row := range jobs {
			job, _ := row["job"].(map[string]any)
			width = max(width, utf8.RuneCountInString(fieldText(job, "name")))
		}
		width = min(width, 50)
		fmt.Fprintln(&b, "\n"+d.paint("JOBS", "97;1")+"\n")
		fmt.Fprintln(&b, workflowHeaders(d, []string{"NAME", "RUNS", "MEDIAN", "P95", "FAILURE", "FLAKE"}, []int{width, 5, 10, 10, 8, 7}, 1, "  "))
		for _, row := range jobs {
			job, _ := row["job"].(map[string]any)
			name := fieldText(job, "name")
			if _, exists := job["name"]; !exists {
				name = "(unknown)"
			}
			cells := []string{
				d.paint(padded(shortCell(name, width-2), width, false), "1"),
				padded(aggregateCount(row, "count"), 5, true),
				padded(formatDuration(row["median_duration_seconds"]), 10, true),
				padded(formatDuration(row["p95_duration_seconds"]), 10, true),
				aggregateRate(d, row, padded(aggregatePct(row, "failure_count"), 8, true)),
				padded(aggregatePct(row, "flaked_count"), 7, true),
			}
			fmt.Fprintln(&b, "  "+strings.Join(cells, "  "))
		}
	}
	_, err := io.WriteString(d.Out, b.String())
	return err
}

func renderWorkflowRecent(d Dependencies, rows []map[string]any, bucket map[string]any, host, slug string) error {
	if len(rows) == 0 {
		return nil
	}
	titleWidth, branchWidth := 10, 6
	for _, row := range rows {
		titleWidth = max(titleWidth, utf8.RuneCountInString(fieldText(row, "display_title")))
		branchWidth = max(branchWidth, utf8.RuneCountInString(fieldText(row, "head_branch")))
	}
	titleWidth, branchWidth = min(titleWidth, 50), min(branchWidth, 32)
	wf, _ := bucket["workflow"].(map[string]any)
	repo := fieldText(wf, "repository_full_name")
	platform := fieldText(wf, "platform")
	var b strings.Builder
	fmt.Fprintln(&b, "\n"+d.paint("RECENT RUNS", "97;1")+"\n")
	fmt.Fprintln(&b, workflowHeaders(d, []string{"TITLE", "BRANCH", "EVENT", "ID", "ELAPSED", "AGE"}, []int{titleWidth, branchWidth, 18, 36, 10, 12}, 4, "  "))
	for _, row := range rows {
		title := padded(shortCell(fieldText(row, "display_title"), titleWidth-2), titleWidth, false)
		branch := d.paint(padded(shortCell(fieldText(row, "head_branch"), branchWidth-2), branchWidth, false), "1")
		id := d.paint(padded(fieldText(row, "run_id"), 36, false), "36")
		if host != "" {
			if fieldText(row, "run_id") != "" {
				target := host + "/org/" + slug + "/runs/" + fieldText(row, "run_id")
				title, id = linkCell(title, target), linkCell(id, target)
			}
			if (platform == "" || platform == "github") && strings.Contains(repo, "/") && fieldText(row, "head_branch") != "" {
				branch = linkCell(branch, "https://github.com/"+repo+"/tree/"+fieldText(row, "head_branch"))
			}
		}
		cells := []string{title, branch, padded(fieldText(row, "event"), 18, false), id, padded(formatDuration(row["duration_seconds"]), 10, true), d.paint(padded(relativeAge(fieldText(row, "created_at"), d.Now()), 12, true), "2")}
		fmt.Fprintln(&b, d.indicator(row, "status")+" "+strings.Join(cells, "  "))
	}
	_, err := io.WriteString(d.Out, b.String())
	return err
}
