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
