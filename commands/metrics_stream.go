package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func selectMetricSources(cmd *cobra.Command, sources []string, watch bool) ([]string, error) {
	for _, source := range sources {
		if !slices.Contains(metricSources, source) {
			return nil, &ExitError{2, "Error: invalid --source: " + output.CellText(source)}
		}
	}
	if watch && (cmd.Flags().Changed("start") || cmd.Flags().Changed("end")) {
		return nil, &ExitError{2, "Error: --start and --end require single-shot mode; drop --watch."}
	}
	selected := []string{}
	for _, source := range sources {
		if !slices.Contains(selected, source) {
			selected = append(selected, source)
		}
	}
	if len(selected) == 0 {
		selected = []string{"cpu", "memory"}
	}
	return selected, nil
}

func filterMetricSnapshot(value any, selected []string) (map[string]map[string]any, error) {
	result := map[string]map[string]any{}
	if value == nil {
		return result, nil
	}
	metrics, ok := value.(map[string]any)
	if !ok {
		return nil, &ExitError{1, "Error: Invalid metrics stream snapshot."}
	}
	for _, source := range selected {
		if raw, exists := metrics[source]; exists {
			metric, ok := raw.(map[string]any)
			if !ok {
				return nil, &ExitError{1, "Error: Invalid metrics stream snapshot."}
			}
			result[source] = metric
		}
	}
	return result, nil
}

type metricUpdate struct {
	metrics                               map[string]map[string]any
	sawBundle, running, changed, complete bool
	message                               string
}

// watchMetrics reduces replaceable stream snapshots for both VM and job
// commands. JSON emits one complete selected-source map per bundle, with no
// human frames or incidental log requests.
func watchMetrics(ctx context.Context, d Dependencies, api *client.Client, path string, selected []string, asJSON bool, render func(metricUpdate) error) (err error) {
	defer func() {
		if errors.Is(err, context.Canceled) {
			err = &ExitError{Code: 130}
		}
	}()
	u := metricUpdate{metrics: map[string]map[string]any{}, running: true}
	return metricsFrames(ctx, d, api, path, func(frame map[string]any) error {
		u.changed, u.complete = false, false
		switch frame["type"] {
		case "window":
			if running, exists := frame["running"]; exists {
				var ok bool
				u.running, ok = running.(bool)
				if !ok {
					return &ExitError{1, "Error: Invalid metrics stream window."}
				}
			}
		case "error":
			u.message = "Metrics: " + output.CellText(pythonValue(fallback(frame["message"], "Metrics stream error")))
			u.changed = true
			if asJSON {
				_, err := fmt.Fprintln(d.Err, u.message)
				return err
			}
		case "bundle":
			var err error
			u.metrics, err = filterMetricSnapshot(frame["metrics"], selected)
			if err != nil {
				return err
			}
			u.sawBundle, u.changed, u.message = true, true, ""
			if asJSON {
				return json.NewEncoder(d.Out).Encode(u.metrics)
			}
		case "complete":
			u.complete = true
			if asJSON && !u.sawBundle {
				return json.NewEncoder(d.Out).Encode(u.metrics)
			}
		}
		if !asJSON {
			return render(u)
		}
		return nil
	})
}

func metricsFrames(ctx context.Context, d Dependencies, api *client.Client, path string, yield func(map[string]any) error) error {
	failures := 0
	lastError := ""
	for {
		stream, err := api.OpenObservability(ctx, path, "metrics", 10*time.Second)
		if err == nil {
			complete := false
			err = func() error {
				defer stream.Close()
				for {
					raw, err := stream.Next(ctx, time.Second)
					if err != nil {
						return err
					}
					var frame map[string]any
					if raw != nil {
						if !utf8.Valid(raw) || !json.Valid(raw) {
							return &ExitError{1, "Error: Invalid JSON in metrics stream."}
						}
						decoder := json.NewDecoder(bytes.NewReader(raw))
						decoder.UseNumber()
						if decoder.Decode(&frame) != nil || frame == nil {
							return &ExitError{1, "Error: Invalid metrics stream frame."}
						}
						if frame["signal"] != nil && frame["signal"] != "metrics" {
							continue
						}
						switch frame["type"] {
						case "error":
							message := output.CellText(pythonValue(fallback(frame["message"], "Metrics stream error")))
							if frame["signal"] == nil {
								return errors.New(message)
							}
							lastError = message
						case "bundle":
							lastError, failures = "", 0
						case "complete":
							if lastError != "" {
								return errors.New(lastError)
							}
							complete = true
						}
					}
					if err := yield(frame); err != nil {
						return err
					}
					if complete {
						return nil
					}
				}
			}()
			if complete || err == nil {
				return err
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var failure *client.StreamError
		if !errors.As(err, &failure) {
			return err
		}
		if failure.HTTPStatus >= 300 && failure.HTTPStatus < 400 {
			return &ExitError{1, "Error: Refusing metrics stream redirect."}
		}
		if failure.HTTPStatus != 0 && failure.HTTPStatus != 429 && failure.HTTPStatus < 500 {
			return &ExitError{1, fmt.Sprintf("Error: Metrics stream refused: HTTP %d.", failure.HTTPStatus)}
		}
		if slices.Contains([]int{1008, 4001, 4003, 4401, 4403}, failure.CloseCode) {
			reason := failure.Reason
			if reason == "connection closed" {
				reason = "access denied"
			}
			return &ExitError{1, fmt.Sprintf("Error: Metrics stream refused: %s.", output.CellText(reason))}
		}
		failures++
		if failures == 4 {
			return &ExitError{1, fmt.Sprintf("Error: Metrics stream disconnected; reconnect attempts exhausted (last error: %s).", output.CellText(failure.Error()))}
		}
		delay := time.Duration(1<<(failures-1)) * time.Second
		if _, err := fmt.Fprintf(d.Err, "Metrics stream disconnected; reconnecting in %ds...\n", int(delay/time.Second)); err != nil {
			return err
		}
		if err := d.Wait(ctx, delay); err != nil {
			return err
		}
	}
}

func clearMetricFrame(d Dependencies) error {
	if d.IsTerminal() {
		_, err := io.WriteString(d.Out, "\x1b[2J\x1b[H")
		return err
	}
	return nil
}
