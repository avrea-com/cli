package commands

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/output"
)

func sbomCount(value any) string {
	if value == nil {
		return "?"
	}
	return pythonValue(value)
}
func sbomSummaryCount(summary map[string]any, key string) any {
	number, ok := summary[key].(json.Number)
	if !ok {
		return nil
	}
	n, err := number.Int64()
	if err != nil || n < 0 {
		return nil
	}
	return number
}
func sbomOmitted(summary map[string]any, key string) int64 {
	value := sbomSummaryCount(summary, key)
	if value == nil {
		return 0
	}
	n, _ := value.(json.Number).Int64()
	return n
}

type sbomBreakdown struct {
	name  string
	count int64
	known bool
}

func sbomCounts(value any) []sbomBreakdown {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	var result []sbomBreakdown
	for name, value := range fields {
		number, ok := value.(json.Number)
		if !ok {
			continue
		}
		count, err := number.Int64()
		if err != nil {
			continue
		}
		result = append(result, sbomBreakdown{name, count, count >= 0})
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.known != b.known {
			return a.known
		}
		if a.known && a.count != b.count {
			return a.count > b.count
		}
		return a.name < b.name
	})
	return result
}
func sbomHead(size int, full bool) int {
	if full || size <= 11 {
		return size
	}
	return 10
}
func sbomMore(hidden int, omitted int64, nouns []string, hint string) string {
	more := int64(hidden) + omitted
	if more == 0 {
		return ""
	}
	where := "use --full to list them"
	if omitted > 0 {
		where = "see \x60" + hint + "\x60"
		if hidden > 0 {
			where = fmt.Sprintf("--full lists %d; see \x60%s\x60 for all", hidden, hint)
		}
	}
	label := ""
	if len(nouns) > 0 {
		label = nouns[1]
		if more == 1 {
			label = nouns[0]
		}
		label = " " + label
	}
	return fmt.Sprintf("… and %d more%s (%s)", more, label, where)
}
func sbomLeftOut(omitted int64, nouns []string, hint string) string {
	if omitted == 0 {
		return ""
	}
	noun := nouns[1]
	if omitted == 1 {
		noun = nouns[0]
	}
	return fmt.Sprintf("%d %s left out of the summary (see \x60%s\x60)", omitted, noun, hint)
}
func renderSBOMView(d Dependencies, snapshot map[string]any, org, repo string, full bool) error {
	summary := diagnosticObject(snapshot["summary"])
	copyleft, _ := summary["copyleft"].([]any)
	var copyleftCount any
	if copyleft != nil {
		copyleftCount = int64(len(copyleft)) + sbomOmitted(summary, "copyleft_omitted")
	}
	args := []string{"avr", "sbom", "download", fieldText(snapshot, "commit_sha"), "--org", org, "--repo", repo, "--format", "inventory"}
	for i, arg := range args {
		args[i] = shellQuotePython(arg)
	}
	hint := strings.Join(args, " ")
	var b strings.Builder
	release := "no"
	if snapshot["is_release"] == true {
		release = "yes"
	}
	branch := ""
	if value, ok := snapshot["branch"]; ok {
		branch = pythonValue(value)
	}
	rows := [][2]string{
		{"Commit", fieldText(snapshot, "commit_sha")},
		{"Branch", branch}, {"Release", release},
		{"Generated", formatTimestamp(fieldText(snapshot, "generated_at"))},
		{"Dependencies", sbomCount(sbomSummaryCount(summary, "total_dependencies"))},
		{"Unresolved", sbomCount(sbomSummaryCount(summary, "unresolved"))},
		{"Copyleft", sbomCount(copyleftCount)},
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "%-14s%s\n", row[0], output.CellText(row[1]))
	}
	if reason, ok := summary["omitted"].(string); ok && reason != "" {
		fmt.Fprintf(&b, "\nSummary unavailable (%s); run \x60%s\x60 for the full dependency list.\n", output.CellText(reason), hint)
	}
	for _, kind := range []string{"licenses", "ecosystems"} {
		counts := sbomCounts(summary[kind])
		omitted := sbomOmitted(summary, kind+"_omitted")
		head := len(counts)
		label := "Ecosystem"
		nouns := []string{"ecosystem", "ecosystems"}
		if kind == "licenses" {
			head = sbomHead(len(counts), full)
			label = "License"
			nouns = []string{"license", "licenses"}
		}
		if head > 0 {
			b.WriteByte('\n')
			var cells [][]string
			for _, entry := range counts[:head] {
				name := output.CellText(entry.name)
				if kind == "licenses" {
					name = output.CellText(strings.Join(strings.Fields(entry.name), " "))
					if runes := []rune(name); len(runes) > 60 {
						name = string(runes[:59]) + "…"
					}
				}
				count := "?"
				if entry.known {
					count = strconv.FormatInt(entry.count, 10)
				}
				cells = append(cells, []string{name, count})
			}
			_ = output.Grid(&b, []string{label, "Count"}, cells)
			if rest := sbomMore(len(counts)-head, omitted, nouns, hint); rest != "" {
				b.WriteString(rest + "\n")
			}
		} else if rest := sbomLeftOut(omitted, nouns, hint); rest != "" {
			b.WriteString("\n" + rest + "\n")
		}
	}
	omitted := sbomOmitted(summary, "copyleft_omitted")
	if len(copyleft) > 0 || omitted > 0 {
		head := sbomHead(len(copyleft), full)
		b.WriteString("\nCopyleft dependencies:\n")
		for _, name := range copyleft[:head] {
			b.WriteString("  " + output.CellText(pythonValue(name)) + "\n")
		}
		var rest string
		if head > 0 {
			rest = sbomMore(len(copyleft)-head, omitted, nil, hint)
		} else {
			rest = sbomLeftOut(omitted, []string{"copyleft dependency", "copyleft dependencies"}, hint)
		}
		if rest != "" {
			b.WriteString("  " + rest + "\n")
		}
	}
	if artifacts := objectList(snapshot["artifacts"]); len(artifacts) > 0 {
		b.WriteByte('\n')
		var cells [][]string
		for _, item := range artifacts {
			name, media := "N/A", "N/A"
			if value, ok := item["filename"]; ok {
				name = pythonValue(value)
			}
			if value, ok := item["media_type"]; ok {
				media = pythonValue(value)
			}
			cells = append(cells, []string{name, media, formatSize(item["raw_bytes"])})
		}
		_ = output.Grid(&b, []string{"Artifact", "Media Type", "Size"}, cells)
	}
	_, err := fmt.Fprint(d.Out, b.String())
	return err
}
