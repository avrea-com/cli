package commands

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
)

func vmConnectable(row map[string]any) bool {
	if strings.ToUpper(fieldText(row, "state")) != "RUNNING" {
		return false
	}
	eps, _ := row["endpoints"].(map[string]any)
	ssh, _ := eps["ssh"].(map[string]any)
	if len(ssh) == 0 {
		return false
	}
	rd, _ := eps["remote_desktop"].(map[string]any)
	return row["enable_remote_desktop"] != true || len(rd) > 0
}
func vmRetryDelay(header string) time.Duration {
	raw := strings.TrimSpace(header)
	if !sbomRetrySeconds().MatchString(raw) {
		return 3 * time.Second
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value > 30 {
		return 30 * time.Second
	}
	return time.Duration(value) * time.Second
}
func waitForVM(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, path string, timeout int, ready func(map[string]any) bool, goneReady bool, failed func(map[string]any) bool) (map[string]any, string, error) {
	deadline := d.Now().Add(time.Duration(timeout) * time.Second)
	var row map[string]any
	last := ""
	seen := false
	for {
		record, err := api.VM(ctx, path)
		if err != nil {
			if ctx.Err() != nil {
				return row, "", ctx.Err()
			}
			var he *client.HTTPError
			var transport *client.TransportError
			delay := 3 * time.Second
			switch {
			case errors.As(err, &he):
				if goneReady && he.Status == 404 {
					return nil, "ready", nil
				}
				if he.Status < 500 && he.Status != 408 && he.Status != 429 {
					return row, "", apiError(cfg, err, "check VM status")
				}
				if he.Status == 429 {
					delay = vmRetryDelay(he.RetryAfter)
				}
			case errors.As(err, &transport):
			default:
				return row, "", err
			}
			remaining := deadline.Sub(d.Now())
			if remaining <= 0 {
				return row, "timeout", nil
			}
			if err = d.Wait(ctx, min(delay, remaining)); err != nil {
				return row, "", err
			}
			continue
		}
		row = record.Fields
		state := strings.ToUpper(fieldText(row, "state"))
		if !seen || state != last {
			shown := state
			if shown == "" {
				shown = "UNKNOWN"
			}
			if _, err = fmt.Fprintln(d.Err, "  "+output.CellText(shown)); err != nil {
				return row, "", err
			}
			last = state
			seen = true
		}
		if ready(row) {
			return row, "ready", nil
		}
		if failed != nil && failed(row) || strings.Contains(state, "ERROR") || strings.Contains(state, "FAIL") {
			return row, "failed", nil
		}
		remaining := deadline.Sub(d.Now())
		if remaining <= 0 {
			return row, "timeout", nil
		}
		if err = d.Wait(ctx, min(3*time.Second, remaining)); err != nil {
			return row, "", err
		}
	}
}
func vmWaitJSON(d Dependencies, row map[string]any, password any, disposition string) error {
	value := maps.Clone(row)
	if value == nil {
		value = map[string]any{}
	}
	if password != nil {
		value["password"] = password
	}
	if err := rawJSON(d, value); err != nil {
		return err
	}
	if disposition != "ready" {
		return &ExitError{1, ""}
	}
	return nil
}
func vmFailureText(row map[string]any, id string) string {
	state := pythonValue(fallback(row["state"], "an error state"))
	message := "VM " + id + " entered " + state
	if reason := fieldText(row, "state_reason"); reason != "" {
		message += ": " + reason
	}
	return message + "."
}
func vmWaitExit(d Dependencies, row map[string]any, disposition, target string, timeout int, id string) error {
	if disposition == "ready" {
		return nil
	}
	if _, err := fmt.Fprintln(d.Out); err != nil {
		return err
	}
	message := fmt.Sprintf("Not %s yet after %ds. Re-run once ready: avr vm show %s", target, timeout, id)
	if disposition == "failed" {
		message = vmFailureText(row, id)
	}
	if _, err := fmt.Fprintln(d.Out, output.CellText(message)); err != nil {
		return err
	}
	return &ExitError{1, ""}
}
func vmPrintPassword(d Dependencies, password string) error {
	if password == "" {
		return nil
	}
	if _, err := fmt.Fprintln(d.Out); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(d.Out, d.paint("One-time password: "+output.CellText(password), "33;1")); err != nil {
		return err
	}
	_, err := fmt.Fprintln(d.Out, "Save it now; it is not stored and cannot be retrieved later.")
	return err
}
