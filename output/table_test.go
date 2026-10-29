package output

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestTSVControlsAndTerminalLinks(t *testing.T) {
	rows := [][]string{{"a\tb\n\x1b[31m\u202e", "42"}}
	links := [][]string{{"https://console.example.test/org/test", ""}}
	var out bytes.Buffer
	if err := Table(&out, []string{"name", "id"}, rows, true, links); err != nil {
		t.Fatal(err)
	}
	if out.String() != "name\tid\na\\tb\\n\\x1b[31m\\u202e\t42\n" {
		t.Fatalf("TSV %q", out.String())
	}
	out.Reset()
	if err := Table(&out, []string{"NAME", "ID"}, rows, false, links); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b]8;;https://console.example.test/org/test\x1b\\") || strings.Contains(out.String(), "\x1b[31m") {
		t.Fatalf("terminal %q", out.String())
	}
}

func TestJSONListProjectionAndJQ(t *testing.T) {
	var out bytes.Buffer
	records := []map[string]any{{"slug": "one", "secret": "hidden"}, {"slug": "two"}}
	if err := JSONList(context.Background(), &out, records, []string{"slug"}, ".[].slug"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "one\ntwo\n" {
		t.Fatalf("jq %q", out.String())
	}
	out.Reset()
	if err := JSONList(context.Background(), &out, nil, []string{"slug"}, ""); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[]\n" {
		t.Fatalf("empty list %q", out.String())
	}
}
