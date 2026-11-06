package commands

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/avrea-com/cli/output"
)

func renderStatus(d Dependencies, report, repoInfo map[string]any, repoID, host string, links bool) error {
	slug := fieldText(report, "org")
	base := consoleURL(host) + "/org/" + url.PathEscape(slug)
	var b strings.Builder
	fmt.Fprintf(&b, "Organization: %s  (%s)\n\n", d.paint(output.CellText(slug), "1"), output.CellText(fieldText(report, "since")))
	runs, _ := report["runs"].([]map[string]any)
	if len(runs) > 0 {
		b.WriteString(d.paint("RECENT RUNS", "97;1") + "\n\n")
		widths := []int{50, 30, 30, 18, 36, 12, 12}
		headers := []string{"TITLE", "WORKFLOW", "BRANCH", "EVENT", "ID", "ELAPSED", "AGE"}
		header := make([]string, len(headers))
		for i, h := range headers {
			header[i] = d.paint(padded(h, widths[i], false), gray+";4")
		}
		b.WriteString("  " + strings.Join(header, " ") + "\n")
		for _, row := range runs {
			title := padded(shortCell(fieldText(row, "display_title"), 48), 50, false)
			workflow := d.paint(padded(shortCell(fieldText(diagnosticObject(row["workflow"]), "name"), 28), 30, false), "35")
			branchName := fieldText(row, "head_branch")
			branch := d.paint(padded(shortCell(branchName, 28), 30, false), "1")
			id := fieldText(row, "run_id")
			idCell := d.paint(padded(id, 36, false), "36")
			if links && id != "" {
				target := base + "/runs/" + url.PathEscape(id)
				title, workflow, idCell = linkCell(title, target), linkCell(workflow, target), linkCell(idCell, target)
			}
			repo := fieldText(diagnosticObject(row["repository"]), "full_name")
			if links && branchName != "" && strings.Contains(repo, "/") {
				branch = linkCell(branch, "https://github.com/"+repo+"/tree/"+branchName)
			}
			cells := []string{title, workflow, branch, padded(fieldText(row, "event"), 18, false), idCell, padded(formatDuration(row["duration_seconds"]), 12, true), d.paint(padded(relativeAge(fieldText(row, "created_at"), d.Now()), 12, true), "2")}
			b.WriteString(d.indicator(row, "status") + " " + strings.Join(cells, " ") + "\n")
		}
	} else {
		b.WriteString("No recent workflow runs.\n")
	}
	for _, panel := range []struct {
		key             string
		workflow, trend bool
	}{
		{"slowest_workflows", true, false}, {"workflows_slowing_down", true, true}, {"slowest_jobs", false, false}, {"jobs_slowing_down", false, true},
	} {
		b.WriteByte('\n')
		items, _ := report[panel.key].([]map[string]any)
		renderStatusPanel(&b, d, items, panel.workflow, panel.trend, base, links)
	}
	cache := diagnosticObject(report["cache"])
	if len(cache) > 0 {
		total, quota := metricNumber(cache["total_size_bytes"]), metricNumber(cache["quota_bytes"])
		pct := 0.0
		if quota > 0 {
			pct = total / quota * 100
		}
		fmt.Fprintf(&b, "\n%s\n\n  %s / %s (%.0f%%)\n", d.paint("CACHE", "97;1"), formatSize(total), formatSize(quota), pct)
		for _, entry := range objectList(cache["by_type"]) {
			name := diagnosticDefaultText(entry, "cache_type", "?")
			count := diagnosticDefaultText(entry, "entry_count", "0")
			fmt.Fprintf(&b, "  %-12s %10s  (%s entries)\n", name, formatSize(entry["size_bytes"]), count)
		}
	}
	if len(repoInfo) > 0 {
		fmt.Fprintf(&b, "\n%s\n\n  Avrea:   %s/activity?repositories=%s\n", d.paint("LINKS", "97;1"), output.CellText(base), output.CellText(repoID))
		if strings.EqualFold(fieldText(repoInfo, "platform"), "github") && fieldText(repoInfo, "full_name") != "" {
			fmt.Fprintf(&b, "  GitHub:  https://github.com/%s/actions\n", output.CellText(fieldText(repoInfo, "full_name")))
		}
	}
	b.WriteString("\n" + d.paint("  ✓ success  ✗ failure  ● running  ○ queued  — skipped/cancelled", gray) + "\n")
	_, err := fmt.Fprint(d.Out, b.String())
	return err
}
func renderStatusPanel(b *strings.Builder, d Dependencies, items []map[string]any, workflows, trend bool, base string, links bool) {
	if !trend {
		items = statusSlowest(items)
	}
	if len(items) == 0 {
		return
	}
	noun := "JOBS"
	if workflows {
		noun = "WORKFLOWS"
	}
	title := "SLOWEST " + noun
	headers := []string{"#", "NAME", "MEDIAN", "RUNS"}
	widths := []int{3, 40, 12, 5}
	right := []bool{true, false, true, true}
	if trend {
		title = noun + " SLOWING DOWN"
		headers = []string{"#", "NAME", "CURRENT", "CHANGE"}
		widths = []int{3, 40, 12, 10}
	} else if workflows {
		headers = []string{"#", "NAME", "REPOSITORY", "MEDIAN", "RUNS"}
		widths = []int{3, 40, 28, 12, 5}
		right = []bool{true, false, false, true, true}
	}
	b.WriteString(d.paint(title, "97;1") + "\n\n")
	styled := make([]string, len(headers))
	for i, h := range headers {
		styled[i] = d.paint(padded(h, widths[i], right[i]), gray+";4")
	}
	b.WriteString("  " + strings.Join(styled, "  ") + "\n")
	for i, item := range items {
		metadata := diagnosticObject(item["job"])
		if workflows {
			metadata = diagnosticObject(item["workflow"])
		}
		name := d.paint(padded(shortCell(diagnosticDefaultText(metadata, "name", "(unknown)"), 38), 40, false), "1")
		if workflows && links && fieldText(metadata, "workflow_id") != "" {
			name = linkCell(name, base+"/workflows/"+url.PathEscape(fieldText(metadata, "workflow_id")))
		}
		cells := []string{fmt.Sprintf("%3d", i+1), name}
		if !trend && workflows {
			repo := d.paint(padded(shortCell(fieldText(metadata, "repository_full_name"), 26), 28, false), "2")
			if links && fieldText(metadata, "repository_id") != "" {
				repo = linkCell(repo, base+"/activity?repositories="+url.QueryEscape(fieldText(metadata, "repository_id")))
			}
			cells = append(cells, repo)
		}
		median := item["success_median_duration_seconds"]
		if trend {
			median = item["_cur_median"]
		}
		cells = append(cells, padded(formatDuration(median), 12, true))
		if trend {
			change := fmt.Sprintf("+%.0f%%", metricNumber(item["_delta_pct"]))
			cells = append(cells, d.paint(padded(change, 10, true), "31;1"))
		} else {
			cells = append(cells, padded(diagnosticDefaultText(item, "success_count", "0"), 5, true))
		}
		b.WriteString("  " + strings.Join(cells, "  ") + "\n")
	}
}
