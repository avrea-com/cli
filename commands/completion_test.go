package commands

import (
	"slices"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
)

func TestCompletionDoesNotRunHandlersOrReadCredentials(t *testing.T) {
	for _, program := range []string{"avr", "avr-admin"} {
		t.Run(program, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			variable := "_" + strings.ToUpper(strings.ReplaceAll(program, "-", "_")) + "_COMPLETE"
			env := map[string]string{variable: "bash_complete", "COMP_WORDS": program + " extension --", "COMP_CWORD": "2"}
			d.Getenv = func(key string) string { return env[key] }
			d.LoadConfig = func() (config.Config, error) { t.Fatal("completion read credentials"); return config.Config{}, nil }
			root := NewRoot(d)
			root.Use = program
			extension := &cobra.Command{Use: "extension", RunE: func(*cobra.Command, []string) error { t.Fatal("completion invoked command"); return nil }}
			extension.Flags().String("example", "", "Example setting.")
			root.AddCommand(extension)
			code := ExecuteRoot(t.Context(), root, []string{"--not-an-argument"}, stderr, d)
			if code != 0 || stderr.Len() != 0 || !strings.Contains(out.String(), "plain,--example\n") {
				t.Fatal(code, out, stderr)
			}
			out.Reset()
			env[variable] = "unknown_complete"
			if code := ExecuteRoot(t.Context(), root, nil, stderr, d); code != 1 || out.Len() != 0 || stderr.Len() != 0 {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestCompletionFollowsLiveFlagsAndCommandVisibility(t *testing.T) {
	root := NewRoot(Dependencies{})
	repo, _, _ := root.Find([]string{"repo"})
	root.RemoveCommand(repo)
	root.AddCommand(&cobra.Command{Use: "extension", Short: "Inspect extension."})
	root.AddCommand(&cobra.Command{Use: "hidden-extension", Hidden: true})
	got, err := completeCommand(root, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, item := range got {
		names = append(names, item.value)
	}
	if slices.Contains(names, "repo") || slices.Contains(names, "hidden-extension") || !slices.Contains(names, "extension") {
		t.Fatal(names)
	}
	runList, _, _ := root.Find([]string{"run", "list"})
	runList.Flags().Lookup("status").Hidden = true
	got, err = completeCommand(root, []string{"run", "list"}, "--stat")
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}

func TestCompletionWordsRetainsPartialInputWithoutExpansion(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"avr 'two words", []string{"avr", "two words"}},
		{"avr \"two words", []string{"avr", "two words"}},
		{"avr two\\ words", []string{"avr", "two words"}},
		{"avr tail\\", []string{"avr", "tail"}},
		{"avr '' \"\"", []string{"avr", "", ""}},
		{"avr '$HOME' '$(touch never)'", []string{"avr", "$HOME", "$(touch never)"}},
	}
	for _, test := range cases {
		if got := completionWords(test.input); !slices.Equal(got, test.want) {
			t.Fatalf("%q: got %q want %q", test.input, got, test.want)
		}
	}
}

func TestCompletionSuppressesCommandTimingDiagnostics(t *testing.T) {
	d, out, stderr := testDeps(t)
	env := map[string]string{"_AVR_COMPLETE": "bash_complete", "COMP_WORDS": "avr rep", "COMP_CWORD": "1", "AVR_DEBUG": "timing"}
	d.Getenv = func(key string) string { return env[key] }
	if code := Execute(t.Context(), nil, d); code != 0 || stderr.Len() != 0 || out.String() != "plain,repo\n" {
		t.Fatal(code, out, stderr)
	}
}
