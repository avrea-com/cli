package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var runnerCPU = lazyRegexp(`(?i)-(\d+)-vcpu\b`)

func runnerSpecs(job map[string]any) (int, float64, string) {
	cpus, ram, label := 0, 0.0, ""
	labels, _ := job["job_labels"].([]any)
	for _, v := range labels {
		if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), "vcpu") {
			label = s
			break
		}
	}
	for _, v := range labels {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if parts := runnerCPU().FindStringSubmatch(s); parts != nil {
			cpus, _ = strconv.Atoi(parts[1])
			ram = float64(map[int]int{1: 2, 2: 8, 4: 16, 8: 32, 16: 64, 32: 128}[cpus]) * (1 << 30)
			break
		}
	}
	return cpus, ram, label
}

func newJobMetrics(d Dependencies) *cobra.Command {
	var org string
	var sources []string
	var start, end int64
	var live, jsonOutput bool
	c := &cobra.Command{Use: "metrics JOB_ID", Short: "Show CPU/memory/IO gauges for a job's VM.", Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return &ExitError{2, "Error: Missing argument 'JOB_ID'."}
		}
		return cobra.ExactArgs(1)(cmd, args)
	}}
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringArrayVar(&sources, "source", nil, "Metric source (repeatable): cpu, memory, filesystem, load, disk-io, disk-ops, network.")
	f.Int64Var(&start, "start", 0, "Start time (Unix seconds); defaults to execution start.")
	f.Int64Var(&end, "end", 0, "End time (Unix seconds); defaults to execution end or now.")
	f.BoolVarP(&live, "watch", "w", false, "Refresh metrics and recent logs until interrupted.")
	f.BoolVar(&jsonOutput, "json", false, "Output raw metrics responses as JSON.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		selected, err := selectMetricSources(cmd, sources, live)
		if err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		orgID, err := resolveOrg(cmd.Context(), d, cfg, api, org)
		if err != nil {
			return err
		}
		query := url.Values{}
		if f.Changed("start") {
			query.Set("start", strconv.FormatInt(start, 10))
		}
		if f.Changed("end") {
			query.Set("end", strconv.FormatInt(end, 10))
		}
		record, err := api.Job(cmd.Context(), orgID, args[0], nil)
		if err != nil {
			return apiError(cfg, err, "get job")
		}
		job := record.Fields
		if live {
			return watchJobMetrics(cmd.Context(), d, cfg, api, args[0], selected, job, jsonOutput)
		}
		fetched, err := fetchMetrics(cmd.Context(), api, orgID, args[0], selected, query)
		if err != nil {
			return apiError(cfg, err, "get metrics")
		}
		frame, err := metricFrame(d, args[0], job, selected, fetched, nil, false, jsonOutput)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintln(d.Out, frame); err != nil {
			return err
		}
		if !jsonOutput {
			_, err = fmt.Fprintln(d.Err, d.hint("View logs:  avr job logs "+output.CellText(args[0])))
		}
		return err
	})
	return c
}

func fetchMetrics(ctx context.Context, api *client.Client, org, id string, sources []string, query url.Values) (map[string]map[string]any, error) {
	result := map[string]map[string]any{}
	for _, source := range sources {
		record, err := api.JobMetrics(ctx, org, id, source, query)
		if err != nil {
			var he *client.HTTPError
			if errors.As(err, &he) && he.Status == 404 && he.Detail == "No execution found for job" {
				result[source] = map[string]any{"series": []any{}, "_error": "no execution"}
				continue
			}
			return nil, err
		}
		result[source] = record.Fields
	}
	return result, nil
}

func metricFrame(d Dependencies, id string, job map[string]any, sources []string, fetched map[string]map[string]any, logs []map[string]any, live, jsonOutput bool) (string, error) {
	if jsonOutput {
		data, err := json.MarshalIndent(fetched, "", "  ")
		return string(data), err
	}
	cpus, ram, label := runnerSpecs(job)
	state := fieldText(job, "conclusion")
	if state == "" {
		state = fieldText(job, "state")
	}
	if state == "" {
		state = "unknown"
	}
	status := d.indicator(job, "state") + " " + output.CellText(state) + " (" + formatDuration(job["duration_seconds"])
	if fieldText(job, "state") == "completed" && fieldText(job, "completed_at") != "" {
		status += ", finished " + relativeAge(fieldText(job, "completed_at"), d.Now())
	}
	status += ")"
	name := fieldText(job, "job_name")
	if _, exists := job["job_name"]; !exists {
		name = "-"
	}
	rows := [][2]string{{"Job", output.CellText(id)}, {"Name", output.CellText(name)}, {"Status", status}}
	if cpus > 0 && ram > 0 {
		rows = append(rows, [2]string{"VM", fmt.Sprintf("%d vCPU, %.0f GB RAM", cpus, ram/(1<<30))})
	}
	if label != "" {
		rows = append(rows, [2]string{"Runner", output.CellText(label)})
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	var b strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&b, "%-*s %s\n", width+1, row[0], row[1])
	}
	b.WriteByte('\n')
	for _, source := range sources {
		b.WriteString(renderMetricGauge(d, source, fetched[source], ram, live))
		b.WriteByte('\n')
	}
	if logs != nil {
		b.WriteString("\n" + d.paint("LOGS", "97;1") + "\n")
		if len(logs) == 0 {
			b.WriteString(d.paint("(no log lines yet)", gray) + "\n")
		} else {
			for _, entry := range logs {
				writeStyledLogLine(&b, entry, d, "")
			}
		}
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

func fetchMetricLogs(ctx context.Context, api *client.Client, id string, cursor any) ([]map[string]any, any, error) {
	entries := []map[string]any{}
	seen := map[string]bool{}
	for {
		raw, next, more, err := fetchLogBatch(ctx, api, id, cursor, "")
		if err != nil {
			return nil, cursor, err
		}
		if len(raw) == 0 {
			return entries, cursor, nil
		}
		entries = append(entries, filterLogEntries(raw, logOptions{})...)
		if len(entries) > 12 {
			entries = entries[len(entries)-12:]
		}
		key := fmt.Sprint(next)
		if !more || next == nil || key == fmt.Sprint(cursor) || seen[key] {
			return entries, next, nil
		}
		seen[key] = true
		cursor = next
	}
}

func metricFetchError(err error, kind string) (message string, retry bool) {
	var he *client.HTTPError
	if errors.As(err, &he) {
		if kind == "log" && he.Status == 404 && he.Detail == "No execution found for job" {
			return "", true
		}
		return fmt.Sprintf("%s fetch failed: HTTP %d", kind, he.Status), he.Status < 400 || he.Status >= 500 || he.Status == 429
	}
	var transport *client.TransportError
	if errors.As(err, &transport) {
		return kind + " fetch failed: " + transport.Reason, true
	}
	return "", false
}

func watchJobMetrics(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, id string, sources []string, job map[string]any, jsonOutput bool) error {
	logs := []map[string]any{}
	var cursor any
	var lastLogs time.Time
	logError := ""
	refresh := func() error {
		entries, next, err := fetchMetricLogs(ctx, api, id, cursor)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var retry bool
			logError, retry = metricFetchError(err, "log")
			if !retry {
				return apiError(cfg, err, "fetch logs")
			}
		} else {
			logError = ""
			if len(entries) > 0 {
				logs = append(logs, entries...)
				cursor = next
				if len(logs) > 12 {
					logs = logs[len(logs)-12:]
				}
			}
		}
		return nil
	}
	return watchMetrics(ctx, d, api, "/vms/"+url.PathEscape(id)+"/observability/stream", sources, jsonOutput, func(u metricUpdate) error {
		refreshLogs := lastLogs.IsZero() || d.Now().Sub(lastLogs) >= time.Second
		if u.complete || refreshLogs {
			lastLogs = d.Now()
			if err := refresh(); err != nil {
				return err
			}
		} else if !u.changed {
			return nil
		}
		frame, err := metricFrame(d, id, job, sources, u.metrics, logs, true, false)
		if err != nil {
			return err
		}
		if logError != "" {
			frame += "\n\n" + logError
		}
		if !u.complete {
			if u.message != "" {
				frame += "\n\n" + u.message
			}
			frame += "\n\n(metrics streamed; logs every second; Ctrl-C to exit)"
		}
		if err := clearMetricFrame(d); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(d.Out, frame); err != nil {
			return err
		}
		if u.complete && !u.sawBundle {
			_, err = fmt.Fprintln(d.Out, "No metrics available for this job.")
		}
		return err
	})
}
