package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

// confirmMutation shares Python's default-no prompt and automation escape hatch.
func confirmMutation(ctx context.Context, d Dependencies, action, prompt string) error {
	if d.Getenv("AVR_PROMPT_DISABLED") != "" {
		return &ExitError{1, "Error: " + action + ", but AVR_PROMPT_DISABLED is set. Pass the bypass flag (e.g. --yes) instead.\nAborted!"}
	}
	reader := bufio.NewReader(d.In)
	for {
		if _, err := fmt.Fprint(d.Out, prompt+" [y/N]: "); err != nil {
			return err
		}
		type result struct {
			answer string
			err    error
		}
		read := make(chan result, 1)
		// Input belongs to the embedding caller. Cancellation releases the
		// command without closing its stdin; the pending read finishes when
		// the owner supplies input or closes the reader.
		go func() {
			answer, err := reader.ReadString('\n')
			read <- result{answer, err}
		}()
		var answer string
		var err error
		select {
		case <-ctx.Done():
			return ctx.Err()
		case got := <-read:
			answer, err = got.answer, got.err
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if errors.Is(err, io.EOF) && answer == "" {
			_, _ = fmt.Fprintln(d.Out)
			return &ExitError{1, "Aborted!"}
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return nil
		case "", "n", "no":
			return &ExitError{1, "Aborted!"}
		default:
			if _, err := fmt.Fprintln(d.Out, "Error: invalid input"); err != nil {
				return err
			}
		}
	}
}

func newRunCancel(d Dependencies) *cobra.Command { return newRunMutation(d, false) }
func newRunRerun(d Dependencies) *cobra.Command  { return newRunMutation(d, true) }

func newRunMutation(d Dependencies, rerun bool) *cobra.Command {
	name, summary := "cancel", "Cancel an in-progress or queued workflow run."
	if rerun {
		name, summary = "rerun", "Re-run a completed workflow run."
	}
	var org string
	var yes, failed bool
	cmd := &cobra.Command{Use: name + " RUN", Short: summary, Args: cobra.ExactArgs(1)}
	cmd.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	if rerun {
		cmd.Flags().BoolVar(&failed, "failed", false, "Re-run only the failed jobs.")
	}
	cmd.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err := authenticated(cfg); err != nil {
			return err
		}
		ref, orgID, err := referenceAndOrg(cmd.Context(), d, cfg, api, args[0], org)
		if err != nil {
			return err
		}
		id := ref.id
		var existing map[string]any
		if rerun || id == "" {
			record, err := resolveRun(cmd.Context(), cfg, api, orgID, ref, nil, "look up workflow run before "+name)
			if err != nil {
				return err
			}
			existing, id = record.Fields, fieldText(record.Fields, "run_id")
		}
		if rerun {
			if err := requireExternalCI(existing, "Reruns"); err != nil {
				return err
			}
		}
		scope := "all jobs"
		if failed {
			scope = "failed jobs only"
		}
		if !yes {
			prompt := "Cancel " + id + "?"
			if rerun {
				prompt = "Re-run " + id + " (" + scope + ")?"
			}
			if err := confirmMutation(cmd.Context(), d, "run "+name+" needs confirmation", prompt); err != nil {
				return err
			}
		}
		var cancelFields map[string]any
		if rerun {
			err = api.Rerun(cmd.Context(), orgID, id, failed)
		} else {
			record, requestErr := api.CancelRun(cmd.Context(), orgID, id)
			err, cancelFields = requestErr, record.Fields
		}
		if err != nil {
			return apiError(cfg, err, name+" workflow run")
		}
		terminal := false
		if rerun {
			if failed {
				scope = "failed jobs"
			}
			if _, err := fmt.Fprintf(d.Out, "%s Re-run requested for %s (%s).\n", d.paint("✓", "32"), d.paint(output.CellText(id), "36"), scope); err != nil {
				return err
			}
			if existing["platform_run_id"] != nil {
				_, _ = fmt.Fprintln(d.Err, d.errorPaint("Waiting for the new attempt to appear...", "2"))
				attempt := fieldText(existing, "run_attempt")
				if attempt == "" {
					attempt = "1"
				}
				next, err := pollRerun(cmd.Context(), d, cfg, api, orgID, fieldText(existing, "platform_run_id"), attempt)
				if err != nil {
					return err
				}
				if next != nil {
					id = fieldText(next, "run_id")
					attempt = fieldText(next, "run_attempt")
					if attempt == "" {
						attempt = "?"
					}
					if _, err := fmt.Fprintf(d.Out, "  Run: %s  (attempt %s)\n", d.paint(output.CellText(id), "36"), output.CellText(attempt)); err != nil {
						return err
					}
				} else {
					_, _ = fmt.Fprintln(d.Err, d.errorPaint("New attempt not yet visible after 30s. Use `avr run list` to find it.", "2"))
				}
			}
		} else {
			terminal = fieldText(cancelFields, "status") == "already_terminal"
			symbol, color, message := "✓", "32", "Cancel requested for %s."
			if terminal {
				symbol, color, message = "•", "33", "Run %s had already finished; nothing to cancel."
			}
			if _, err := fmt.Fprintln(d.Out, d.paint(symbol, color)+" "+fmt.Sprintf(message, d.paint(output.CellText(id), "36"))); err != nil {
				return err
			}
		}
		slug := orgSlug(cmd.Context(), cfg, api, orgID)
		if _, err := fmt.Fprintf(d.Out, "  %s/org/%s/runs/%s\n", consoleURL(cfg.Host), output.CellText(slug), output.CellText(id)); err != nil {
			return err
		}
		if !terminal {
			hint := "  → avr run watch " + output.CellText(id)
			if !rerun {
				hint += "  (verify cancellation)"
			}
			_, err = fmt.Fprintln(d.Err, d.errorPaint(hint, "2"))
		}
		return err
	})
	return cmd
}

func pollRerun(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, platform, attempt string) (map[string]any, error) {
	current, err := strconv.ParseInt(attempt, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid run attempt in API response")
	}
	deadline := d.Now().Add(30 * time.Second)
	for d.Now().Before(deadline) {
		page, err := api.Runs(ctx, org, url.Values{"platform_run_id": {platform}, "limit": {"10"}, "order": {"created_at.desc"}})
		if err == nil {
			for _, row := range page.Items {
				next, _ := strconv.ParseInt(fieldText(row.Fields, "run_attempt"), 10, 64)
				if next > current {
					return row.Fields, nil
				}
			}
		} else {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var apiErr *client.HTTPError
			if errors.As(err, &apiErr) && apiErr.Status < 500 && apiErr.Status != 429 {
				return nil, apiError(cfg, err, "poll for rerun attempt")
			}
			var transport *client.TransportError
			if !errors.As(err, &apiErr) && !errors.As(err, &transport) {
				return nil, err
			}
		}
		if err := d.Wait(ctx, time.Second); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
