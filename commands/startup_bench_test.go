package commands

import (
	"context"
	"io"
	"testing"
)

func startupDependencies() Dependencies {
	return Dependencies{
		In: nil, Out: io.Discard, Err: io.Discard,
		Getenv:          func(string) string { return "" },
		IsTerminal:      func() bool { return false },
		IsErrorTerminal: func() bool { return false },
	}
}

func BenchmarkStartup(b *testing.B) {
	b.Run("CommandTree", func(b *testing.B) {
		d := startupDependencies()
		b.ReportAllocs()
		for b.Loop() {
			_ = NewRoot(d)
		}
	})
	b.Run("HelpDescription", func(b *testing.B) {
		root := NewRoot(startupDependencies())
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := commandHelp(root); err != nil {
				b.Fatal(err)
			}
		}
	})
	for name, args := range map[string][]string{
		"RootHelp":       {"--help"},
		"HelpCommand":    {"help"},
		"NestedHelp":     {"repo", "list", "--help"},
		"Version":        {"--version"},
		"FieldDiscovery": {"auth", "status", "--json", "?"},
	} {
		b.Run(name, func(b *testing.B) {
			d := startupDependencies()
			b.ReportAllocs()
			for b.Loop() {
				if code := Execute(context.Background(), args, d); code != 0 {
					b.Fatal(code)
				}
			}
		})
	}
}
