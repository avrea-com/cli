package output

import (
	"bytes"
	"testing"
)

func TestGridUsesTerminalCellsForUnicode(t *testing.T) {
	var b bytes.Buffer
	if err := Grid(&b, []string{"name"}, [][]string{{"雪 café"}, {"e\u0301"}}); err != nil {
		t.Fatal(err)
	}
	want := "+---------+\n| name    |\n+---------+\n| 雪 café |\n| e\u0301       |\n+---------+\n"
	if b.String() != want {
		t.Fatalf("got %q, want %q", b.String(), want)
	}
}
