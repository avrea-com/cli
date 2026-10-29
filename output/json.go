// Package output owns projection and embedded jq output.
package output

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/itchyny/gojq"
)

func Fields(raw string, available []string) ([]string, error) {
	if strings.TrimSpace(raw) == "*" {
		fields := append([]string(nil), available...)
		sort.Strings(fields)
		return fields, nil
	}
	var result, unknown []string
	for _, f := range strings.Split(raw, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		found := false
		for _, a := range available {
			if a == f {
				found = true
				break
			}
		}
		if !found {
			unknown = append(unknown, f)
		}
		result = append(result, f)
	}
	if len(unknown) > 0 {
		names := append([]string(nil), available...)
		sort.Strings(names)
		return nil, fmt.Errorf("Unknown JSON field(s): %s. Available: %s", strings.Join(unknown, ", "), strings.Join(names, ", ")) //nolint:staticcheck // Preserve the existing user-facing CLI diagnostic.
	}
	return result, nil
}

func Discovery(w io.Writer, fields []string) error {
	names := append([]string(nil), fields...)
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("Specify one or more comma-separated fields for `--json`:\n")
	for _, field := range names {
		fmt.Fprintf(&b, "  %s\n", field)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func JSON(ctx context.Context, w io.Writer, record map[string]any, fields []string, expression string) error {
	selected := make(map[string]any, len(fields))
	for _, field := range fields {
		selected[field] = record[field]
	}
	return jsonValue(ctx, w, selected, expression)
}

func JSONList(ctx context.Context, w io.Writer, records []map[string]any, fields []string, expression string) error {
	selected := make([]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(fields))
		for _, field := range fields {
			row[field] = record[field]
		}
		selected = append(selected, row)
	}
	return jsonValue(ctx, w, selected, expression)
}

func jsonValue(ctx context.Context, w io.Writer, selected any, expression string) error {
	if expression == "" {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		return encoder.Encode(selected)
	}
	query, err := gojq.Parse(expression)
	if err != nil {
		return errors.New("jq error: invalid expression")
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return errors.New("jq error: could not compile expression")
	}
	// gojq normalizes json.Number into arbitrary-precision integers itself.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	iter := code.RunWithContext(ctx, selected)
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	for {
		value, ok := iter.Next()
		if !ok {
			break
		}
		if halt, ok := value.(*gojq.HaltError); ok && halt.ExitCode() == 0 {
			break
		}
		if _, ok := value.(error); ok {
			return errors.New("jq error: expression failed")
		}
		if s, ok := value.(string); ok {
			b.WriteString(s)
			b.WriteByte('\n')
		} else if err := encoder.Encode(value); err != nil {
			return errors.New("could not encode jq result")
		}
		if b.Len() > 16<<20 {
			return errors.New("jq error: output exceeds 16 MiB")
		}
	}
	_, err = w.Write(b.Bytes())
	return err
}

// Text escapes external control characters while preserving readable content.
func Text(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 32 || r == 127 || (r >= 0x80 && r <= 0x9f) {
			fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
