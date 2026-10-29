package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func newRunDiagnose(d Dependencies) *cobra.Command {
	var org string
	var raw bool
	c := &cobra.Command{Use: "diagnose RUN", Short: "Explain a failed or unexpectedly slow workflow run.", Args: cobra.ExactArgs(1)}
	c.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	c.Flags().BoolVar(&raw, "json", false, "Output the diagnostic report as JSON.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		ref, id, err := referenceAndOrg(cmd.Context(), d, cfg, api, args[0], org)
		if err != nil {
			return err
		}
		record, err := resolveRun(cmd.Context(), cfg, api, id, ref, []string{"workflow"}, "resolve workflow run")
		if err != nil {
			return err
		}
		report, err := buildRunDiagnostics(cmd.Context(), d, api, id, record.Fields)
		if err != nil {
			return apiError(cfg, err, "diagnose workflow run")
		}
		if raw {
			b, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(d.Out, string(b))
			return err
		}
		_, err = fmt.Fprintln(d.Out, renderRunDiagnostics(d, report))
		return err
	})
	return c
}

func diagnosticTimingParts(timing map[string]any) []string {
	parts := []string{}
	for _, item := range [][2]string{{"queue_seconds", "queued "}, {"start_offset_seconds", "started +"}, {"execution_seconds", "ran "}} {
		if timing[item[0]] != nil {
			parts = append(parts, item[1]+formatDuration(timing[item[0]]))
		}
	}
	return parts
}
func diagnosticMetricValue(value any, unit string) string {
	if value == nil {
		return "-"
	}
	n := metricNumber(value)
	switch unit {
	case "ratio":
		return fmt.Sprintf("%.1f%%", n*100)
	case "bytes":
		return metricBytes(n)
	case "bytes/sec", "operations/sec":
		return metricRate(n, unit)
	default:
		return strings.TrimSpace(fmt.Sprintf("%.2f %s", n, output.CellText(unit)))
	}
}
func diagnosticDefaultText(data map[string]any, key, fallback string) string {
	if _, ok := data[key]; !ok {
		return fallback
	}
	return output.CellText(fieldText(data, key))
}
func renderRunDiagnostics(d Dependencies, report map[string]any) string {
	run := diagnosticObject(report["run"])
	title := fieldText(run, "display_title")
	if title == "" {
		title = fieldText(run, "run_id")
	}
	if title == "" {
		title = "Workflow run"
	}
	if run["run_number"] != nil {
		title += " #" + fieldText(run, "run_number")
	}
	title += " (attempt " + diagnosticDefaultText(run, "run_attempt", "1") + ")"
	lines := []string{d.indicator(run, "status") + " " + d.paint(output.CellText(title), "1"), "  " + diagnosticDefaultText(run, "run_id", "-")}
	if parts := diagnosticTimingParts(diagnosticObject(report["timing"])); len(parts) > 0 {
		lines = append(lines, "  Run timing: "+strings.Join(parts, " · "))
	}
	baseline := diagnosticObject(report["baseline"])
	if baseline["status"] == "complete" {
		count := metricNumber(baseline["sample_count"])
		label := "successful runs"
		if count == 1 {
			label = "successful run"
		}
		parts := []string{
			formatTimestamp(fieldText(baseline, "window_start")) + " to " + formatTimestamp(fieldText(baseline, "window_end")),
			fieldText(baseline, "sample_count") + " " + label,
		}
		if baseline["median_duration_seconds"] != nil {
			parts = append(parts, "median "+formatDuration(baseline["median_duration_seconds"]))
		}
		if baseline["p95_duration_seconds"] != nil {
			parts = append(parts, "p95 "+formatDuration(baseline["p95_duration_seconds"]))
		}
		duration := diagnosticObject(report["timing"])["execution_seconds"]
		median := metricNumber(baseline["median_duration_seconds"])
		if duration != nil && baseline["median_duration_seconds"] != nil && median != 0 {
			parts = append(parts, fmt.Sprintf("this run %+.1f%% vs median", (metricNumber(duration)-median)/median*100))
		}
		lines = append(lines, "  Baseline: "+strings.Join(parts, " · "))
	}
	jobs := objectList(report["jobs"])
	if len(jobs) > 0 {
		lines = append(lines, "", d.paint("JOBS", "97;1"))
	}
	suffix := func(timing any) string {
		parts := diagnosticTimingParts(diagnosticObject(timing))
		if len(parts) == 0 {
			return ""
		}
		return " · " + strings.Join(parts, " · ")
	}
	labels := map[string]string{"cpu": "CPU", "memory": "Memory", "disk-io": "Disk I/O", "network": "Network"}
	for _, entry := range jobs {
		job := diagnosticObject(entry["job"])
		lines = append(lines, "  "+d.indicator(job, "state")+" "+d.paint(diagnosticDefaultText(job, "job_name", "?"), "1")+suffix(entry["timing"]))
		for _, stepEntry := range objectList(entry["steps"]) {
			step := diagnosticObject(stepEntry["step"])
			if step["conclusion"] == nil || step["conclusion"] == "success" || step["conclusion"] == "skipped" {
				continue
			}
			lines = append(lines, "    Failed step: "+diagnosticDefaultText(step, "name", "?")+suffix(stepEntry["timing"]))
		}
		sources := diagnosticObject(diagnosticObject(entry["metrics"])["sources"])
		for _, source := range diagnosticSources {
			summary := diagnosticObject(sources[source])
			if metricNumber(summary["sample_count"]) == 0 {
				continue
			}
			unit := fieldText(summary, "unit")
			parts := []string{"p95 " + diagnosticMetricValue(summary["p95"], unit), "peak " + diagnosticMetricValue(summary["peak"], unit)}
			if summary["total"] != nil {
				parts = append(parts, "total "+diagnosticMetricValue(summary["total"], fieldText(summary, "total_unit")))
			}
			parts = append(parts, fieldText(summary, "sample_count")+" samples")
			lines = append(lines, "    "+labels[source]+": "+strings.Join(parts, ", "))
		}
		logs := diagnosticObject(entry["failed_logs"])
		logLines := objectList(logs["lines"])
		if len(logLines) > 0 {
			label := d.paint("LOG TAIL", "1")
			if logs["truncated"] == true {
				label += " (truncated)"
			}
			lines = append(lines, "    "+label)
			for _, line := range logLines {
				var b strings.Builder
				writeStyledLogLine(&b, line, d, "")
				if b.Len() > 0 {
					lines = append(lines, "      "+strings.TrimSuffix(b.String(), "\n"))
				}
			}
		}
	}
	warnings := objectList(report["warnings"])
	if len(warnings) > 0 {
		lines = append(lines, "", d.paint("INCOMPLETE DATA", "33;1"))
		for _, warning := range warnings {
			job := ""
			if fieldText(warning, "job_id") != "" {
				job = " (" + output.CellText(fieldText(warning, "job_id")) + ")"
			}
			lines = append(lines, d.paint("  "+diagnosticDefaultText(warning, "code", "warning")+job+": "+output.CellText(fieldText(warning, "message")), "33"))
		}
	} else if report["complete"] != true {
		lines = append(lines, "", d.paint("Diagnostic data is incomplete.", "33"))
	}
	bounds := diagnosticObject(report["bounds"])
	if len(bounds) > 0 {
		lines = append(lines, "", d.paint("Response bounds: "+fieldText(bounds, "max_jobs")+" jobs, "+fieldText(bounds, "max_steps")+" steps, "+fieldText(bounds, "max_log_lines_per_job")+" log lines per failed job.", gray))
	}
	return strings.Join(lines, "\n")
}
