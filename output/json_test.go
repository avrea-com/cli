package output

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestJQ(t *testing.T) {
	data := map[string]any{"n": json.Number("9007199254740993"), "email": "fixture@example.test", "missing": nil}
	tests := []struct{ query, want string }{
		{".email", "fixture@example.test\n"}, {".n + 1", "9007199254740994\n"},
		{".missing", "null\n"}, {"halt", ""}, {".email, halt", "fixture@example.test\n"}, {"empty", ""}, {".email, .missing", "fixture@example.test\nnull\n"},
	}
	for _, tt := range tests {
		var b bytes.Buffer
		if err := JSON(context.Background(), &b, data, []string{"n", "email", "missing"}, tt.query); err != nil {
			t.Fatal(err)
		}
		if b.String() != tt.want {
			t.Fatalf("%s: %q != %q", tt.query, b.String(), tt.want)
		}
	}
	for _, expr := range []string{"[", "unknown_function", "error(.email)", ".email, error(\"secret\")"} {
		var b bytes.Buffer
		err := JSON(context.Background(), &b, data, []string{"email"}, expr)
		if err == nil || strings.Contains(err.Error(), "fixture") || b.Len() != 0 {
			t.Fatalf("error leaked input or partial output: %q %v", b.String(), err)
		}
	}
}

func TestJQCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var b bytes.Buffer
	if err := JSON(ctx, &b, map[string]any{}, nil, "repeat(1)"); err == nil {
		t.Fatal("uncancelled expression")
	}
}

func TestProjection(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(context.Background(), &b, map[string]any{"secret": "hidden"}, []string{"unknown"}, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "secret") || !strings.Contains(b.String(), "null") {
		t.Fatal(b.String())
	}
	if _, err := Fields("nope", []string{"email"}); err == nil {
		t.Fatal("accepted unknown field")
	}
}

// gh-style map ordering is intentional; explicit arrays define column order.
func TestJQOrderingContract(t *testing.T) {
	row := map[string]any{"status": "completed", "conclusion": "failure", "run_id": "run-123"}
	for _, tc := range []struct{ query, want string }{
		{".[] | [.[]] | @tsv", "failure\trun-123\tcompleted\n"},
		{".[] | [.status,.conclusion,.run_id] | @tsv", "completed\tfailure\trun-123\n"},
		{".[] | to_entries | map(.key) | join(\",\")", "conclusion,run_id,status\n"},
	} {
		var b bytes.Buffer
		if err := JSONList(t.Context(), &b, []map[string]any{row}, []string{"status", "conclusion", "run_id"}, tc.query); err != nil || b.String() != tc.want {
			t.Fatalf("%s: %q %v", tc.query, b.String(), err)
		}
	}
	var b bytes.Buffer
	if err := JSON(t.Context(), &b, row, []string{"status", "conclusion", "run_id"}, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Index(b.String(), "conclusion") >= strings.Index(b.String(), "run_id") || strings.Index(b.String(), "run_id") >= strings.Index(b.String(), "status") {
		t.Fatal(b.String())
	}
	if err := JSON(t.Context(), &b, row, []string{"status"}, "keys_unsorted"); err == nil {
		t.Fatal("unsupported function unexpectedly accepted")
	}
}
