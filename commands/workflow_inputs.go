package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Python converts workflow JSON inputs with str(), including booleans/null and
// nested containers. Retain object order while reading so nested input strings
// preserve that public behavior instead of depending on Go map iteration.
type workflowInput struct {
	text, repr string
	fields     map[string]string
	order      []string
}

func pythonQuote(value string) string {
	quoted := strconv.Quote(value)
	if strings.Contains(value, "'") && !strings.Contains(value, "\"") {
		return quoted
	}
	content := quoted[1 : len(quoted)-1]
	content = strings.ReplaceAll(content, "\\\"", "\"")
	content = strings.ReplaceAll(content, "'", "\\'")
	return "'" + content + "'"
}

func readWorkflowInput(decoder *json.Decoder, depth int) (workflowInput, error) {
	if depth > 100 {
		return workflowInput{}, fmt.Errorf("JSON inputs exceed nesting limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return workflowInput{}, err
	}
	value := workflowInput{}
	switch token := token.(type) {
	case nil:
		value.text = "None"
	case bool:
		if token {
			value.text = "True"
		} else {
			value.text = "False"
		}
	case string:
		return workflowInput{text: token, repr: pythonQuote(token)}, nil
	case json.Number:
		value.text = token.String()
		if strings.ContainsAny(value.text, ".eE") {
			number, err := strconv.ParseFloat(value.text, 64)
			if err != nil {
				return value, err
			}
			format := byte('g')
			if number == 0 || (math.Abs(number) >= 1e-4 && math.Abs(number) < 1e16) {
				format = 'f'
			}
			value.text = strconv.FormatFloat(number, format, -1, 64)
			if !strings.ContainsAny(value.text, ".eE") {
				value.text += ".0"
			}
		}
	case json.Delim:
		var entries []string
		switch token {
		case '[':
			for decoder.More() {
				child, err := readWorkflowInput(decoder, depth+1)
				if err != nil {
					return value, err
				}
				entries = append(entries, child.repr)
			}
			if _, err := decoder.Token(); err != nil {
				return value, err
			}
			value.text = "[" + strings.Join(entries, ", ") + "]"
		case '{':
			value.fields = map[string]string{}
			representations := map[string]string{}
			var order []string
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return value, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return value, fmt.Errorf("invalid JSON object key")
				}
				child, err := readWorkflowInput(decoder, depth+1)
				if err != nil {
					return value, err
				}
				if _, ok := value.fields[key]; !ok {
					order = append(order, key)
				}
				value.fields[key], representations[key] = child.text, child.repr
			}
			if _, err := decoder.Token(); err != nil {
				return value, err
			}
			value.order = order
			for _, key := range order {
				entries = append(entries, pythonQuote(key)+": "+representations[key])
			}
			value.text = "{" + strings.Join(entries, ", ") + "}"
		default:
			return value, fmt.Errorf("unexpected JSON delimiter")
		}
	default:
		return value, fmt.Errorf("unexpected JSON input")
	}
	value.repr = value.text
	return value, nil
}

func parseWorkflowInputs(ctx context.Context, input io.Reader) (map[string]string, error) {
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() { data, err := io.ReadAll(io.LimitReader(input, (16<<20)+1)); done <- result{data, err} }()
	var raw []byte
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case got := <-done:
		if got.err != nil {
			return nil, got.err
		}
		raw = got.data
	}
	if len(raw) > 16<<20 {
		return nil, fmt.Errorf("workflow inputs exceed 16 MiB")
	}
	if strings.TrimSpace(string(raw)) == "" {
		return map[string]string{}, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	value, err := readWorkflowInput(decoder, 0)
	if err != nil {
		return nil, &ExitError{1, "Error: Invalid JSON on stdin: " + err.Error()}
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, &ExitError{1, "Error: Invalid JSON on stdin: trailing input"}
	}
	if value.fields == nil {
		return nil, &ExitError{1, "Error: JSON inputs must be an object."}
	}
	return value.fields, nil
}
