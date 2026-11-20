package commands

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
)

var admissionIDPattern = lazyRegexp(`^wap-[0-9a-f]{32}$`)

// avreaCIDispatchCommand repeats a dispatch with its invocation key. The API
// answers a repeat with the first request's receipt, so one command both
// retries a lost response and resumes following the run. flags are the watch
// options to repeat with it.
func avreaCIDispatchCommand(cfg config.Config, org, repo, workflow, ref, key string, inputs map[string]string, flags ...string) string {
	args := []string{"env", "AVR_HOST=" + cfg.Host, "avr", "workflow", "run", workflow, "--org", org, "--repo", repo, "--ref", ref, "--idempotency-key", key}
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		args = append(args, "-f", name+"="+inputs[name])
	}
	args = append(args, flags...)
	for i := range args {
		args[i] = shellQuoteControls(args[i])
	}
	return strings.Join(args, " ")
}

// shellQuoteControls quotes an argument so that pasting it reproduces every
// byte. Control characters go inside $'...', which bash and zsh expand, since
// a display escape would repeat the dispatch with different inputs. Those
// outside ASCII are written as their UTF-8 bytes: bash 3.2, the macOS
// /bin/bash, has no \u escape.
func shellQuoteControls(value string) string {
	if !strings.ContainsFunc(value, output.UnsafeControl) {
		return shellQuotePython(value)
	}
	var b strings.Builder
	b.WriteString("$'")
	for _, r := range value {
		switch {
		case r == '\\' || r == '\'':
			b.WriteRune('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case output.UnsafeControl(r):
			for _, octet := range []byte(string(r)) {
				fmt.Fprintf(&b, `\x%02x`, octet)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("'")
	return b.String()
}

// submitAvreaCIDispatch returns the dispatch receipt. HTTP errors come back
// unwrapped for the caller to explain.
func submitAvreaCIDispatch(ctx context.Context, d Dependencies, api *client.Client, org, repo, workflow, ref, key, retry string, inputs map[string]string) (map[string]any, error) {
	for attempt := range 2 {
		response, err := api.DispatchAvreaCIWorkflow(ctx, org, repo, workflow, ref, key, inputs)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			if run := fieldText(response, "run_id"); !admissionIDPattern().MatchString(fieldText(response, "admission_id")) || run != "" && !runIDPattern().MatchString(run) {
				return nil, &ExitError{1, "Error: The API returned an invalid Avrea CI dispatch receipt. Recover the same dispatch:\n" + retry}
			}
			return response, nil
		}
		var httpErr *client.HTTPError
		if errors.As(err, &httpErr) {
			if httpErr.Status >= 500 || httpErr.Status == 429 {
				_, _ = fmt.Fprintln(d.Err, "Retry the same dispatch:\n"+retry)
			}
			return nil, err
		}
		var transport *client.TransportError
		if !errors.As(err, &transport) {
			return nil, err
		}
		if attempt == 1 {
			break
		}
	}
	return nil, &ExitError{1, "Error: Could not confirm the dispatch. Recover the same dispatch:\n" + retry}
}

// awaitAvreaCIRun reads the dispatch receipt until it names the run. It
// returns no run ID when the receipt names none after 90 seconds, and reports
// whether the receipt could be read at all.
func awaitAvreaCIRun(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, repo, admission string) (string, bool, error) {
	read := false
	for deadline := d.Now().Add(90 * time.Second); d.Now().Before(deadline); {
		receipt, err := api.WorkflowAdmission(ctx, org, repo, admission)
		if err == nil {
			read = true
			if runs, _ := receipt["run_ids"].([]any); len(runs) > 0 {
				if id, _ := runs[0].(string); runIDPattern().MatchString(id) {
					return id, true, nil
				}
				return "", true, &ExitError{1, "Error: The API returned an invalid run ID for the dispatch."}
			}
			if fieldText(receipt, "state") == "rejected" {
				if reason := fieldText(receipt, "reason"); reason != "" {
					return "", true, &ExitError{1, "Error: Avrea CI rejected the dispatch: " + output.CellText(reason)}
				}
				return "", true, &ExitError{1, "Error: Avrea CI rejected the dispatch without giving a reason."}
			}
		} else {
			if ctx.Err() != nil {
				return "", read, ctx.Err()
			}
			var httpErr *client.HTTPError
			var transport *client.TransportError
			switch {
			case errors.As(err, &httpErr):
				if httpErr.Status < 500 && httpErr.Status != 429 {
					return "", read, apiError(cfg, err, "read dispatch receipt")
				}
			case !errors.As(err, &transport):
				return "", read, err
			}
		}
		if err := d.Wait(ctx, time.Second); err != nil {
			return "", read, err
		}
	}
	return "", read, nil
}
