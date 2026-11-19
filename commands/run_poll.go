package commands

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
)

// pollRuns lists runs with each query in turn, once a second, until match
// picks a row, or returns no row once timeout passes. Server errors, 429 and
// network failures are retried; any other API error ends the poll and is
// reported for action.
// With progress, a dot every five seconds shows a terminal user the wait.
func pollRuns(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org string, queries []url.Values, timeout time.Duration, action string, progress bool, match func(map[string]any) bool) (map[string]any, error) {
	deadline := d.Now().Add(timeout)
	tick := d.Now().Add(5 * time.Second)
	dots := progress && d.IsErrorTerminal()
	finish := func() {
		if dots {
			_, _ = fmt.Fprintln(d.Err)
		}
	}
	for d.Now().Before(deadline) {
		for _, query := range queries {
			page, err := api.Runs(ctx, org, query)
			if err == nil {
				for _, row := range page.Items {
					if match(row) {
						finish()
						return row, nil
					}
				}
				continue
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var httpErr *client.HTTPError
			var transport *client.TransportError
			switch {
			case errors.As(err, &httpErr):
				if httpErr.Status < 500 && httpErr.Status != 429 {
					finish()
					return nil, apiError(cfg, err, action)
				}
			case !errors.As(err, &transport):
				finish()
				return nil, err
			}
			break // retry the whole round after the wait
		}
		if dots && !d.Now().Before(tick) {
			_, _ = fmt.Fprint(d.Err, ".")
			tick = d.Now().Add(5 * time.Second)
		}
		if err := d.Wait(ctx, time.Second); err != nil {
			return nil, err
		}
	}
	finish()
	return nil, nil
}
