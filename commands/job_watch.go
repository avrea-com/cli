package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
)

func newJobWatch(d Dependencies) *cobra.Command {
	var org string
	var repos, names []string
	var interval int
	var ndjson bool
	c := &cobra.Command{Use: "watch", Short: "Watch active jobs with auto-refresh.", Args: cobra.NoArgs}
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringArrayVar(&repos, "repo", nil, "Repository name or ID (repeatable); otherwise detect from Git.")
	f.StringArrayVar(&names, "name", nil, "Job name (repeatable).")
	f.IntVar(&interval, "interval", 5, "Refresh interval in seconds.")
	f.BoolVar(&ndjson, "ndjson", false, "Emit one JSON object per refresh; automatic when piped.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		if interval < 1 || int64(interval) > int64((1<<63-1)/time.Second) {
			return &ExitError{1, "Error: --interval must be at least 1 second.\nAborted!"}
		}
		id, err := resolveOrg(cmd.Context(), d, cfg, api, org)
		if err != nil {
			return err
		}
		ids, err := resolveRepositories(cmd.Context(), d, cfg, api, id, repos, false)
		if err != nil {
			return err
		}
		slug := ""
		if linksEnabled(cmd, d) {
			slug = orgSlug(cmd.Context(), cfg, api, id)
		}
		query := url.Values{"limit": {"50"}, "order": {"created_at.desc"}, "states": {"queued", "in_progress"}}
		if len(ids) > 0 {
			query["repository_ids"] = ids
		}
		if len(names) > 0 {
			query["job_names"] = names
		}
		return watchJobs(cmd.Context(), d, cfg, api, id, slug, query, time.Duration(interval)*time.Second, ndjson || !d.IsTerminal())
	})
	return c
}

func watchJobs(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, slug string, query url.Values, interval time.Duration, stream bool) (err error) {
	encoder := json.NewEncoder(d.Out)
	encoder.SetEscapeHTML(false)
	if !stream {
		_, _ = fmt.Fprint(d.Out, "\x1b[22;0t\x1b]2;avr ▸ jobs: watching\x07")
		defer func() { _, _ = fmt.Fprint(d.Out, "\x1b[23;0t") }()
	}
	defer func() {
		if errors.Is(err, context.Canceled) {
			_, _ = fmt.Fprintln(d.Out, "\nStopped watching.")
			err = &ExitError{Code: 130}
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := api.Jobs(ctx, org, query)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var httpErr *client.HTTPError
			var transport *client.TransportError
			switch {
			case errors.As(err, &httpErr):
				if httpErr.Status < 500 && httpErr.Status != 429 {
					return apiError(cfg, err, "list jobs")
				}
				_, _ = fmt.Fprintf(d.Err, "Error fetching jobs (HTTP %d); retrying.\n", httpErr.Status)
			case errors.As(err, &transport):
				_, _ = fmt.Fprintf(d.Err, "Error fetching jobs: %s\nRetrying in %ds...\n", transport.Message, int64(interval/time.Second))
			default:
				return err
			}
		} else if stream {
			if err := encoder.Encode(map[string]any{"data": rawRecords(page)}); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(d.Out, "\x1b[2J\x1b[H\x1b]2;avr ▸ jobs: %d active\x07Active jobs (refreshing every %ds) -- %s\n\n", len(page.Items), int64(interval/time.Second), d.Now().UTC().Format("15:04:05 UTC")); err != nil {
				return err
			}
			if len(page.Items) == 0 {
				if _, err := fmt.Fprintln(d.Out, "No active jobs."); err != nil {
					return err
				}
			} else if err := renderJobList(d, rawRecords(page), cfg.Host, slug); err != nil {
				return err
			}
		}
		if err := d.Wait(ctx, interval); err != nil {
			return err
		}
	}
}
