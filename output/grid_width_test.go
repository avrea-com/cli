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

func TestGridPreservesHeaderBordersAndEscapedCells(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []string
		rows    [][]string
		want    string
	}{
		{"empty rows", []string{"name", "id"}, nil, "+------+----+\n| name | id |\n+------+----+\n+------+----+\n"},
		{"escaped header and cell", []string{"a\n", "b"}, [][]string{{"\x1b", "雪"}}, "+------+----+\n| a\\n  | b  |\n+------+----+\n| \\x1b | 雪 |\n+------+----+\n"},
		{"no columns", nil, nil, "+\n|\n+\n+\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := Grid(&b, tc.headers, tc.rows); err != nil || b.String() != tc.want {
				t.Fatalf("got %q, want %q, error %v", b.String(), tc.want, err)
			}
		})
	}
}
