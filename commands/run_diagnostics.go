package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
)

const diagnosticMaxJobs = 100
const diagnosticMaxSteps = 1000
const diagnosticMaxLogJobs = 10
const diagnosticMaxLogLines = 100
const diagnosticMaxLogBytes = 25 * 1024
const diagnosticBaselineDays = 30

var diagnosticSources = []string{"cpu", "memory", "disk-io", "network"}

type diagnosticResult struct {
	data map[string]any
	err  error
}

// Each diagnostic request is bounded by the shared client's response limit and
// timeout. Buffered results allow cancellation without a blocked result sender.
func diagnosticAsync(fn func() (map[string]any, error)) <-chan diagnosticResult {
	result := make(chan diagnosticResult, 1)
	go func() { data, err := fn(); result <- diagnosticResult{data, err} }()
	return result
}
func diagnosticObject(value any) map[string]any { result, _ := value.(map[string]any); return result }

func diagnosticWarning(warnings *[]any, code, component, message, job string) {
	warning := map[string]any{"code": code, "component": component, "message": message}
	if job != "" {
		warning["job_id"] = job
	}
	*warnings = append(*warnings, warning)
}

// Retain the Python report's floating-point numeric shape, including whole
// seconds, without converting opaque identifiers or raw API numbers to float.
func diagnosticNumber(value float64) json.Number {
	s := strconv.FormatFloat(value, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return json.Number(s)
}
func diagnosticTime(value any) *time.Time {
	s, ok := value.(string)
	if !ok {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}
func diagnosticISO(t time.Time) string {
	t = t.UTC()
	if t.Nanosecond() == 0 {
		return t.Format(time.RFC3339)
	}
	return t.Format("2006-01-02T15:04:05.000000Z")
}
func diagnosticTiming(created, started, completed, parent any, generated time.Time, done bool, source string) (map[string]any, bool) {
	c, s, e, p := diagnosticTime(created), diagnosticTime(started), diagnosticTime(completed), diagnosticTime(parent)
	if !done {
		e = &generated
	}
	skew := false
	delta := func(a, b *time.Time) any {
		if a == nil || b == nil {
			return nil
		}
		if b.Before(*a) {
			skew = true
			return nil
		}
		return diagnosticNumber(b.Sub(*a).Seconds())
	}
	queue, execution, offset := delta(c, s), delta(s, e), delta(p, s)
	var endSource any
	if s != nil && e != nil {
		endSource = source
		if !done {
			endSource = "generated_at"
		}
	}
	return map[string]any{"queue_seconds": queue, "execution_seconds": execution, "start_offset_seconds": offset, "execution_end_source": endSource}, skew
}

func diagnosticFinite(value any) (float64, error) {
	number, err := strconv.ParseFloat(fmt.Sprint(value), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("invalid metric sample")
	}
	return number, nil
}

func diagnosticMetric(source string, response map[string]any) (map[string]any, error) {
	if response["series"] != nil {
		if _, ok := response["series"].([]any); !ok {
			return nil, fmt.Errorf("metrics series must be a list")
		}
	}
	for _, series := range objectList(response["series"]) {
		labels := diagnosticObject(series["labels"])
		if source == "memory" && fieldText(labels, "state") != "used" {
			continue
		}
		key := "values"
		if source == "disk-io" || source == "network" {
			key = "rates"
		}
		raw, _ := series[key].([]any)
		for _, item := range raw {
			pair, ok := item.([]any)
			if !ok || len(pair) != 2 {
				return nil, fmt.Errorf("invalid metric sample")
			}
			if _, err := diagnosticFinite(pair[0]); err != nil {
				return nil, err
			}
			if source != "cpu" || fieldText(labels, "state") == "idle" {
				if _, err := diagnosticFinite(pair[1]); err != nil {
					return nil, err
				}
			}
		}
	}
	samples := aggregateMetric(source, response)
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		if math.IsNaN(sample.value) || math.IsInf(sample.value, 0) {
			return nil, fmt.Errorf("invalid metric sample")
		}
		values = append(values, sample.value)
	}
	slices.Sort(values)
	counter := source == "disk-io" || source == "network"
	unit := response["unit"]
	if counter && fieldText(response, "rate_unit") != "" {
		unit = response["rate_unit"]
	}
	summary := map[string]any{"sample_count": len(values), "unit": unit, "p95": nil, "peak": nil, "total": nil, "total_unit": nil}
	if len(values) > 0 {
		rank := float64(len(values)-1) * 0.95
		lower, upper := int(math.Floor(rank)), int(math.Ceil(rank))
		summary["p95"] = diagnosticNumber(values[lower] + (values[upper]-values[lower])*(rank-float64(lower)))
		summary["peak"] = diagnosticNumber(values[len(values)-1])
	}
	if counter {
		total := 0.0
		for _, series := range objectList(response["series"]) {
			raw, _ := series["values"].([]any)
			for i := 1; i < len(raw); i++ {
				previous, pok := raw[i-1].([]any)
				current, cok := raw[i].([]any)
				if !pok || !cok || len(previous) < 2 || len(current) < 2 {
					return nil, fmt.Errorf("invalid metric sample")
				}
				a, err := diagnosticFinite(previous[1])
				if err != nil {
					return nil, err
				}
				b, err := diagnosticFinite(current[1])
				if err != nil {
					return nil, err
				}
				if b >= a {
					total += b - a
				} else {
					total += b
				}
			}
		}
		if math.IsNaN(total) || math.IsInf(total, 0) {
			return nil, fmt.Errorf("invalid metric total")
		}
		summary["total"], summary["total_unit"] = diagnosticNumber(total), response["unit"]
	}
	return summary, nil
}

func diagnosticMetrics(response map[string]any, jobs []map[string]any, warnings *[]any) (map[string]map[string]any, error) {
	entries := map[string]map[string]any{}
	if response == nil {
		diagnosticWarning(warnings, "metrics_unavailable", "metrics", "Runner metrics are temporarily unavailable.", "")
	} else {
		raw, ok := response["data"].([]any)
		if !ok {
			return nil, fmt.Errorf("metrics response data must be a list")
		}
		for _, item := range raw {
			entry := diagnosticObject(item)
			if id, ok := entry["job_id"].(string); ok {
				entries[id] = entry
			}
		}
	}
	result := map[string]map[string]any{}
	for _, job := range jobs {
		id := fieldText(job, "job_id")
		data := map[string]any{"status": "unavailable", "reason": "metrics_backend_unavailable", "sources": map[string]any{}}
		result[id] = data
		if job["running_on_avrea"] != true {
			data["status"], data["reason"] = "not_applicable", "not_running_on_avrea"
			continue
		}
		if response == nil {
			continue
		}
		entry := entries[id]
		if entry == nil || fieldText(entry, "error") != "" {
			reason := "metrics_missing"
			if entry != nil {
				reason = fieldText(entry, "error")
			}
			data["reason"] = reason
			code := "metrics_missing"
			if reason == "no_execution" {
				code = "metrics_no_execution"
			}
			diagnosticWarning(warnings, code, "metrics", "Runner metrics are unavailable for this job.", id)
			continue
		}
		raw, ok := entry["metrics"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("metrics entry must contain a metrics object")
		}
		sources := map[string]any{}
		missing := []string{}
		for _, source := range diagnosticSources {
			metric, ok := raw[source].(map[string]any)
			if !ok {
				missing = append(missing, source)
				continue
			}
			summary, err := diagnosticMetric(source, metric)
			if err != nil {
				return nil, err
			}
			sources[source] = summary
		}
		data["status"], data["reason"], data["sources"] = "complete", nil, sources
		if len(missing) > 0 {
			data["status"], data["reason"] = "partial", "metrics_sources_missing"
			diagnosticWarning(warnings, "metrics_partial", "metrics", "Runner metrics are missing sources: "+strings.Join(missing, ", ")+".", id)
		}
	}
	return result, nil
}

func diagnosticLogs(response diagnosticResult, id string, warnings *[]any) map[string]any {
	unavailable := func() map[string]any {
		diagnosticWarning(warnings, "logs_unavailable", "logs", "Failed-job logs are temporarily unavailable.", id)
		return map[string]any{"status": "unavailable", "reason": "logs_backend_unavailable", "truncated": false, "lines": []any{}}
	}
	if response.err != nil {
		return unavailable()
	}
	raw, ok := response.data["results"].([]any)
	if !ok {
		return unavailable()
	}
	for _, item := range raw {
		if _, ok := item.(map[string]any); !ok {
			return unavailable()
		}
	}
	truncated := len(raw) > diagnosticMaxLogLines || response.data["has_more"] == true
	lines := []any{}
	remaining := diagnosticMaxLogBytes
	for _, item := range raw[:min(len(raw), diagnosticMaxLogLines)] {
		entry := diagnosticObject(item)
		content, ok := entry["content"].(string)
		if !ok {
			return unavailable()
		}
		clipped := len(content) > remaining
		if clipped {
			truncated = true
			if remaining == 0 {
				break
			}
			content = content[:remaining]
			for !utf8.ValidString(content) {
				content = content[:len(content)-1]
			}
		}
		line := map[string]any{}
		for _, key := range []string{"line_number", "content", "stream", "level", "timestamp", "step_name"} {
			line[key] = entry[key]
		}
		line["content"] = content
		lines = append(lines, line)
		remaining -= len(content)
		if clipped || remaining == 0 {
			break
		}
	}
	slices.Reverse(lines)
	status := "complete"
	if truncated {
		status = "partial"
		diagnosticWarning(warnings, "logs_truncated", "logs", "Failed-job logs were truncated to the diagnostic response bounds.", id)
	}
	return map[string]any{"status": status, "reason": nil, "truncated": truncated, "lines": lines}
}

func diagnosticBaseline(future <-chan diagnosticResult, start, end time.Time, warnings *[]any) map[string]any {
	data := map[string]any{"status": "unavailable", "reason": "workflow_metadata_missing", "window_start": diagnosticISO(start), "window_end": diagnosticISO(end), "sample_count": 0, "median_duration_seconds": nil, "p95_duration_seconds": nil}
	if future == nil {
		return data
	}
	result := <-future
	raw, valid := result.data["data"].([]any)
	if valid && len(raw) > 0 {
		_, valid = raw[0].(map[string]any)
	}
	if !valid && result.err == nil {
		result.err = fmt.Errorf("invalid baseline response")
	}
	if result.err != nil {
		data["reason"] = "baseline_backend_unavailable"
		diagnosticWarning(warnings, "baseline_unavailable", "baseline", "The workflow baseline is temporarily unavailable.", "")
		return data
	}
	rows := objectList(result.data["data"])
	row := map[string]any{}
	if len(rows) > 0 {
		row = rows[0]
	}
	count := row["success_count"]
	if count == nil {
		count = 0
	}
	data["status"], data["reason"], data["sample_count"] = "complete", nil, count
	data["median_duration_seconds"], data["p95_duration_seconds"] = row["success_median_duration_seconds"], row["success_p95_duration_seconds"]
	return data
}

func buildRunDiagnostics(ctx context.Context, d Dependencies, api *client.Client, org string, run map[string]any) (map[string]any, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	generated := d.Now().UTC().Truncate(time.Microsecond)
	id := fieldText(run, "run_id")
	warnings := []any{}
	target := diagnosticTime(run["created_at"])
	if target == nil {
		target = &generated
		diagnosticWarning(&warnings, "baseline_target_time_missing", "baseline", "The baseline window uses report generation time because run creation time is missing.", "")
	}
	start := target.AddDate(0, 0, -diagnosticBaselineDays)
	repo, ok := run["repository_id"].(string)
	if !ok {
		repo, _ = diagnosticObject(run["repository"])["id"].(string)
	}
	platform, platformOK := run["platform_workflow_id"].(json.Number)
	_, platformErr := strconv.ParseInt(string(platform), 10, 64)
	jobsFuture := diagnosticAsync(func() (map[string]any, error) {
		r, e := api.RunJobs(ctx, org, id, url.Values{"limit": {"100"}, "order": {"created_at.asc"}, "include": {"steps"}})
		return r.Fields, e
	})
	metricsFuture := diagnosticAsync(func() (map[string]any, error) {
		r, e := api.RunMetrics(ctx, org, id, url.Values{"source": diagnosticSources})
		return r.Fields, e
	})
	var baselineFuture <-chan diagnosticResult
	if repo != "" && platformOK && platformErr == nil {
		baselineFuture = diagnosticAsync(func() (map[string]any, error) {
			page, e := api.WorkflowAggregates(ctx, org, url.Values{
				"repository_ids": {repo}, "conclusions": {"success"}, "workflow_platform_ids": {string(platform)},
				"created_after": {diagnosticISO(start)}, "created_before": {diagnosticISO(target.Add(-time.Microsecond))}, "time_bucket": {"total"},
			})
			return page.Fields, e
		})
	} else {
		diagnosticWarning(&warnings, "baseline_metadata_missing", "baseline", "The workflow baseline is unavailable because workflow metadata is missing.", "")
	}
	jobResult := <-jobsFuture
	rawJobs, valid := jobResult.data["data"].([]any)
	jobs := objectList(rawJobs)
	if jobResult.err == nil && (!valid || len(jobs) != len(rawJobs)) {
		jobResult.err = fmt.Errorf("avrea returned an invalid workflow-run jobs response")
	}
	if jobResult.err != nil {
		// Python joins the already-started auxiliary fetches before returning
		// a jobs failure. All requests still share cancellation and deadlines.
		<-metricsFuture
		if baselineFuture != nil {
			<-baselineFuture
		}
		return nil, jobResult.err
	}
	truncated := len(jobs) > diagnosticMaxJobs || diagnosticObject(jobResult.data["pagination"])["next_cursor"] != nil
	jobs = jobs[:min(len(jobs), diagnosticMaxJobs)]
	if truncated {
		diagnosticWarning(&warnings, "jobs_truncated", "jobs", "Jobs were truncated to the diagnostic response bounds.", "")
	}
	failed, eligible := []map[string]any{}, []map[string]any{}
	for _, job := range jobs {
		if slices.Contains([]string{"failure", "timed_out", "action_required"}, fieldText(job, "conclusion")) {
			failed = append(failed, job)
			if job["running_on_avrea"] != false {
				eligible = append(eligible, job)
			}
		}
	}
	selected := eligible[:min(len(eligible), diagnosticMaxLogJobs)]
	logFutures := map[string]<-chan diagnosticResult{}
	for _, job := range selected {
		jobID := fieldText(job, "job_id")
		logFutures[jobID] = diagnosticAsync(func() (map[string]any, error) {
			r, e := api.SearchLogs(ctx, map[string]any{"job_id": jobID, "limit": 101, "order": "line_number.desc"})
			return r.Fields, e
		})
	}
	metricsResult := <-metricsFuture
	metricsWarnings := []any{}
	metricsData := metricsResult.data
	if metricsResult.err != nil {
		metricsData = nil
	}
	metrics, err := diagnosticMetrics(metricsData, jobs, &metricsWarnings)
	if err != nil {
		metricsWarnings = []any{}
		metrics, _ = diagnosticMetrics(nil, jobs, &metricsWarnings)
	}
	warnings = append(warnings, metricsWarnings...)
	baseline := diagnosticBaseline(baselineFuture, start, *target, &warnings)
	logs := map[string]map[string]any{}
	for _, job := range jobs {
		logs[fieldText(job, "job_id")] = map[string]any{"status": "not_applicable", "reason": "job_did_not_fail", "truncated": false, "lines": []any{}}
	}
	for _, job := range failed {
		if job["running_on_avrea"] == false {
			logs[fieldText(job, "job_id")]["reason"] = "not_running_on_avrea"
		}
	}
	for _, job := range selected {
		jobID := fieldText(job, "job_id")
		logs[jobID] = diagnosticLogs(<-logFutures[jobID], jobID, &warnings)
	}
	for _, job := range eligible[len(selected):] {
		logs[fieldText(job, "job_id")] = map[string]any{"status": "partial", "reason": "log_job_limit", "truncated": true, "lines": []any{}}
	}
	if len(eligible) > diagnosticMaxLogJobs {
		diagnosticWarning(&warnings, "log_jobs_truncated", "logs", "Failed-job log excerpts were limited to the first 10 failed jobs.", "")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runTiming, skew := diagnosticTiming(run["created_at"], run["started_at"], run["updated_at"], nil, generated, run["status"] == "completed", "updated_at")
	if skew {
		diagnosticWarning(&warnings, "run_clock_skew", "run", "A run timestamp regression prevented one or more timing calculations.", "")
	}
	remaining := diagnosticMaxSteps
	entries := []any{}
	for _, job := range jobs {
		jobID := fieldText(job, "job_id")
		timing, skew := diagnosticTiming(job["created_at"], job["started_at"], job["completed_at"], run["started_at"], generated, job["state"] == "completed", "completed_at")
		if skew {
			diagnosticWarning(&warnings, "job_clock_skew", "jobs", "A job timestamp regression prevented one or more timing calculations.", jobID)
		}
		rawSteps, _ := job["steps"].([]any)
		selected := rawSteps[:min(remaining, len(rawSteps))]
		stepsTruncated := len(selected) < len(rawSteps)
		remaining -= len(selected)
		steps := []any{}
		for _, raw := range selected {
			step, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			timing, skew := diagnosticTiming(nil, step["started_at"], step["completed_at"], run["started_at"], generated, step["status"] == "completed", "completed_at")
			if skew {
				diagnosticWarning(&warnings, "step_clock_skew", "steps", "A step timestamp regression prevented one or more timing calculations.", jobID)
			}
			steps = append(steps, map[string]any{"step": step, "timing": timing})
		}
		if stepsTruncated {
			diagnosticWarning(&warnings, "steps_truncated", "steps", "Steps were truncated to the diagnostic response bounds.", jobID)
		}
		withoutSteps := map[string]any{}
		for key, value := range job {
			if key != "steps" {
				withoutSteps[key] = value
			}
		}
		entries = append(entries, map[string]any{"job": withoutSteps, "timing": timing, "steps": steps, "steps_truncated": stepsTruncated, "metrics": metrics[jobID], "failed_logs": logs[jobID]})
	}
	return map[string]any{
		"generated_at": diagnosticISO(generated), "complete": len(warnings) == 0,
		"bounds": map[string]any{"max_jobs": diagnosticMaxJobs, "max_steps": diagnosticMaxSteps, "max_log_jobs": diagnosticMaxLogJobs, "max_log_lines_per_job": diagnosticMaxLogLines, "max_log_bytes_per_job": diagnosticMaxLogBytes, "baseline_days": diagnosticBaselineDays},
		"run":    run, "timing": runTiming, "jobs": entries, "jobs_truncated": truncated, "baseline": baseline, "warnings": warnings,
	}, nil
}
