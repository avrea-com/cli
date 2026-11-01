package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNotifierOnlyUsesReadyNoticesForSuccessfulInteractiveCommands(t *testing.T) {
	for _, mode := range []string{"ready", "not-ready", "closed", "disabled", "pipe", "stderr-pipe", "ci", "automation", "opt-out", "json", "help", "root-help", "version", "completion", "failure", "hidden"} {
		t.Run(mode, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			notices := make(chan string, 1)
			if mode == "closed" {
				close(notices)
			} else if mode != "not-ready" {
				notices <- "Update available"
			}
			d.UpdateNotices = notices
			if mode == "disabled" {
				d.UpdateNotices = nil
			}
			d.IsTerminal = func() bool { return mode != "pipe" }
			d.IsErrorTerminal = func() bool { return mode != "stderr-pipe" }
			d.Getenv = func(key string) string {
				if mode == "ci" && key == "CI" || mode == "automation" && key == "AVR_IN_AUTOMATION" || mode == "opt-out" && key == "AVR_NO_UPDATE_NOTIFIER" {
					return "1"
				}
				if mode == "completion" {
					return map[string]string{"_AVR_COMPLETE": "bash_complete", "COMP_WORDS": "avr ", "COMP_CWORD": "1"}[key]
				}
				return ""
			}
			root := NewRoot(d)
			cmd := &cobra.Command{Use: "probe", Hidden: mode == "hidden", RunE: func(*cobra.Command, []string) error {
				if mode == "failure" {
					return errors.New("failed")
				}
				return nil
			}}
			cmd.Flags().Bool("json", false, "")
			root.AddCommand(cmd)
			args := []string{"probe"}
			switch mode {
			case "json":
				args = append(args, "--json")
			case "help":
				args = append(args, "--help")
			case "root-help":
				args = []string{"--help"}
			case "version":
				args = []string{"--version"}
			}
			code := ExecuteRoot(t.Context(), root, args, d.Err, d)
			wantCode := 0
			if mode == "failure" {
				wantCode = 2
			}
			if code != wantCode {
				t.Fatal(code, stderr)
			}
			if strings.Contains(stderr.String(), "Update available") != (mode == "ready") || strings.Contains(out.String(), "Update available") {
				t.Fatal(out, stderr)
			}
		})
	}
}
