package commands

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

var metricSources = []string{"cpu", "memory", "filesystem", "load", "disk-io", "disk-ops", "network"}

type metricSample struct {
	timestamp int64
	value     float64
}

func metricNumber(value any) float64 { n, _ := strconv.ParseFloat(fmt.Sprint(value), 64); return n }
func aggregateMetric(source string, response map[string]any) []metricSample {
	bucket := map[int64]float64{}
	cpuIdle := map[int64]map[string]float64{}
	for _, series := range objectList(response["series"]) {
		labels, _ := series["labels"].(map[string]any)
		if source == "memory" && fieldText(labels, "state") != "used" {
			continue
		}
		key := "values"
		if source == "disk-io" || source == "disk-ops" || source == "network" {
			key = "rates"
		}
		samples, _ := series[key].([]any)
		for _, raw := range samples {
			pair, ok := raw.([]any)
			if !ok || len(pair) != 2 {
				continue
			}
			ts, value := int64(metricNumber(pair[0])), metricNumber(pair[1])
			if source == "cpu" {
				cpu := fieldText(labels, "cpu")
				if cpu == "" {
					cpu = "cpu0"
				}
				if cpuIdle[ts] == nil {
					cpuIdle[ts] = map[string]float64{}
				}
				if _, exists := cpuIdle[ts][cpu]; !exists {
					cpuIdle[ts][cpu] = 0
				}
				if fieldText(labels, "state") == "idle" {
					cpuIdle[ts][cpu] = value
				}
			} else {
				bucket[ts] += value
			}
		}
	}
	if source == "cpu" {
		for ts, cpus := range cpuIdle {
			for _, idle := range cpus {
				bucket[ts] += 1 - idle
			}
			bucket[ts] /= float64(len(cpus))
		}
	}
	result := make([]metricSample, 0, len(bucket))
	for ts, value := range bucket {
		result = append(result, metricSample{ts, value})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].timestamp < result[j].timestamp })
	return result
}

func metricBytes(value float64) string {
	switch {
	case value < 1024:
		return fmt.Sprintf("%.0f B", value)
	case value < 1024*1024:
		return fmt.Sprintf("%.1f KB", value/1024)
	case value < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", value/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GB", value/(1024*1024*1024))
	}
}
func metricRate(value float64, unit string) string {
	switch unit {
	case "bytes/sec":
		return metricBytes(value) + "/s"
	case "operations/sec":
		if value < 1000 {
			return fmt.Sprintf("%.0f ops/s", value)
		}
		return fmt.Sprintf("%.1fk ops/s", value/1000)
	default:
		return fmt.Sprintf("%.2f %s", value, unit)
	}
}
func metricColor(ratio float64) string {
	if math.IsNaN(ratio) {
		return ""
	}
	if ratio >= 0.85 {
		return "31"
	}
	if ratio >= 0.60 {
		return "33"
	}
	return "32"
}
func metricBar(d Dependencies, fill, peak float64, fillColor, peakColor string) string {
	if math.IsNaN(fill) {
		fill = 0
	}
	fillN := int(math.RoundToEven(max(0, min(1, fill)) * 20))
	peakN := -1
	if !math.IsNaN(peak) {
		peakN = max(0, min(19, int(math.RoundToEven(max(0, min(1, peak))*20))-1))
		if peakN < fillN {
			peakN = min(fillN, 19)
		}
	}
	filled := func(n int) string { return d.paint(strings.Repeat("█", max(0, n)), fillColor) }
	track := func(n int) string { return d.paint(strings.Repeat("░", max(0, n)), "90;100") }
	if peakN < 0 {
		return filled(fillN) + track(20-fillN)
	}
	visibleFill := min(fillN, peakN)
	gap := max(0, peakN-fillN)
	fg, bg := peakColor, "100"
	if peakN < fillN || gap == 0 {
		fg, bg = "30", ""
		switch fillColor {
		case "31":
			bg = "41"
		case "32":
			bg = "42"
		case "33":
			bg = "43"
		}
	}
	codes := fg
	if bg != "" {
		if codes != "" {
			codes += ";"
		}
		codes += bg
	}
	return filled(visibleFill) + track(gap) + d.paint("▲", codes) + track(19-peakN)
}

func renderMetricGauge(d Dependencies, source string, response map[string]any, ram float64, live bool) string {
	label := fmt.Sprintf("%-10s", source)
	samples := aggregateMetric(source, response)
	counter := source == "disk-io" || source == "disk-ops" || source == "network"
	if len(samples) == 0 {
		message := "(no samples yet)"
		if counter {
			message = "(no rate samples yet)"
		}
		return label + " " + message
	}
	total, peak := 0.0, samples[0].value
	for _, sample := range samples {
		total += sample.value
		peak = max(peak, sample.value)
	}
	average, last := total/float64(len(samples)), samples[len(samples)-1].value
	primary, secondary, primaryLabel, secondaryLabel := average, last, "avg", "last"
	if live {
		primary, secondary, primaryLabel, secondaryLabel = last, average, "now", "avg"
	}
	fillRatio, peakRatio := 0.0, 1.0
	if peak > 0 {
		fillRatio = primary / peak
	}
	color, peakColor := "", ""
	format := func(n float64) string { return fmt.Sprintf("%.2f", n) }
	if counter {
		format = func(n float64) string { return metricRate(n, fieldText(response, "rate_unit")) }
	} else if source == "cpu" || source == "filesystem" {
		fillRatio, peakRatio = primary, peak
		color, peakColor = metricColor(primary), metricColor(peak)
		format = func(n float64) string { return fmt.Sprintf("%.0f%%", n*100) }
	} else if source == "memory" {
		format = metricBytes
		if ram > 0 {
			fillRatio, peakRatio = primary/ram, peak/ram
			color, peakColor = metricColor(fillRatio), metricColor(peakRatio)
		}
	}
	primaryText := format(primary)
	if source == "memory" && ram > 0 {
		primaryText += " / " + metricBytes(ram)
	}
	bar := metricBar(d, fillRatio, peakRatio, color, peakColor)
	return fmt.Sprintf("%s %s  %s %s  (peak %s, %s %s)", label, bar, primaryLabel, d.paint(primaryText, color), format(peak), secondaryLabel, format(secondary))
}
