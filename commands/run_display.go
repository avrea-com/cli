package commands

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
)

func formatDuration(value any) string {
	if value == nil {
		return "-"
	}
	seconds, err := strconv.ParseFloat(fmt.Sprint(value), 64)
	if err != nil || seconds < 0 {
		return "-"
	}
	n := int64(seconds)
	if n < 60 {
		return fmt.Sprintf("%ds", n)
	}
	if n < 3600 {
		return fmt.Sprintf("%dm %02ds", n/60, n%60)
	}
	return fmt.Sprintf("%dh %02dm %02ds", n/3600, n/60%60, n%60)
}

func runState(fields map[string]any, key string) string {
	if conclusion := fieldText(fields, "conclusion"); conclusion != "" {
		return conclusion
	}
	return fieldText(fields, key)
}

func renderJobs(d Dependencies, jobs []map[string]any, steps bool, host, slug string) error {
	if len(jobs) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(d.Out, "\n"+d.paint("JOBS", "97;1")+"\n"); err != nil {
		return err
	}
	for _, job := range jobs {
		duration := formatDuration(job["duration_seconds"])
		if fieldText(job, "state") == "in_progress" {
			duration = "running"
			if started, err := time.Parse(time.RFC3339Nano, fieldText(job, "started_at")); err == nil {
				duration += " " + formatDuration(d.Now().Sub(started).Seconds())
			}
		}
		id := fieldText(job, "job_id")
		target := ""
		if slug != "" && id != "" {
			target = consoleURL(host) + "/org/" + url.PathEscape(slug) + "/jobs/" + url.PathEscape(id)
		}
		name := linkCell(output.CellText(fieldText(job, "job_name")), target)
		suffix := ""
		if id != "" {
			suffix = "  " + linkCell(d.paint(output.CellText(id), "36"), target)
		}
		if _, err := fmt.Fprintf(d.Out, "  %s  %s (%s)%s\n", d.indicator(job, "state"), name, duration, suffix); err != nil {
			return err
		}
		if steps {
			for _, step := range objectList(job["steps"]) {
				duration := ""
				started, e1 := time.Parse(time.RFC3339Nano, fieldText(step, "started_at"))
				completed, e2 := time.Parse(time.RFC3339Nano, fieldText(step, "completed_at"))
				if e1 == nil && e2 == nil {
					duration = " (" + formatDuration(completed.Sub(started).Seconds()) + ")"
				}
				if _, err := fmt.Fprintf(d.Out, "       %s  %s%s\n", d.indicator(step, "status"), output.CellText(fieldText(step, "name")), duration); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(d.Out); err != nil {
				return err
			}
		}
	}
	return nil
}

func relativeAge(raw string, now time.Time) string {
	if raw == "" {
		return "unknown"
	}
	created, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	age := now.Sub(created)
	if age < 0 {
		return "just now"
	}
	if age < time.Minute {
		return fmt.Sprintf("%ds ago", int(age.Seconds()))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		if rem := int(age.Minutes()) % 60; rem != 0 {
			return fmt.Sprintf("%dh%dm ago", int(age.Hours()), rem)
		}
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(age.Hours()/24))
}

func shortCell(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimRight(string(runes[:limit-3]), " ") + "..."
}
