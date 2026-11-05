package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var nativeJobID = lazyRegexp(`^awj-[0-9a-f]{32}$`)
var nativeAttemptString = lazyRegexp(`^[+-]?[0-9](?:_?[0-9])*(?:\.0+)?$`)

type nativeRerunReceipt struct{ runID, attempt string }

func resolveNativeRerunJobs(cmd *cobra.Command, cfg config.Config, api *client.Client, org, runID string, run map[string]any, selectors []string, key string) ([]string, error) {
	response, err := api.Get(cmd.Context(), "/orgs/"+org+"/workflow-runs/"+runID+"/rerun", nil)
	if err != nil {
		return nil, apiError(cfg, err, "check Avrea rerun availability")
	}
	options, _ := response["data"].(map[string]any)
	subset, exists := options["can_rerun_subset"]
	if !exists || (subset != true && options["can_rerun"] != false && key == "") {
		return nil, &ExitError{1, "Error: This server does not currently allow Avrea subset reruns."}
	}
	jobs := objectList(run["jobs"])
	selected := []string{}
	seen := map[string]bool{}
	for _, selector := range selectors {
		id := selector
		if !nativeJobID().MatchString(id) {
			matched := []map[string]any{}
			for _, job := range jobs {
				if fieldText(job, "job_name") == selector {
					matched = append(matched, job)
				}
			}
			if len(matched) != 1 {
				candidates := matched
				if len(candidates) == 0 {
					candidates = jobs
				}
				lines := []string{}
				for _, job := range candidates {
					lines = append(lines, "  "+output.CellText(pythonValue(job["job_name"]))+" ("+output.CellText(pythonValue(job["job_id"]))+")")
				}
				if len(lines) == 0 {
					lines = append(lines, "  none")
				}
				return nil, usageError(cmd, "Job "+pythonQuote(selector)+" must match exactly one job.\nAvailable matches:\n"+strings.Join(lines, "\n"))
			}
			id = fieldText(matched[0], "job_id")
		}
		if !seen[id] {
			selected = append(selected, id)
			seen[id] = true
		}
	}
	return selected, nil
}

func parseNativeRerunReceipt(response map[string]any) (*nativeRerunReceipt, bool) {
	data, ok := response["data"].(map[string]any)
	if !ok {
		return nil, false
	}
	id, ok := data["run_id"].(string)
	if !ok || !nativeRunID().MatchString(id) || data["status"] != "rerun_requested" {
		return nil, false
	}
	var value string
	switch v := data["run_attempt"].(type) {
	case json.Number:
		value = string(v)
	case string:
		value = strings.TrimSpace(v)
		// Pydantic accepts integer strings and an all-zero fractional part,
		// but an exponent in a string isn't an integer receipt field.
		if !nativeAttemptString().MatchString(value) {
			return nil, false
		}
		value = strings.ReplaceAll(value, "_", "")
	default:
		return nil, false
	}
	if !nativeDecimalPattern().MatchString(value) || len(value) > 128 {
		return nil, false
	}
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(value[index+1:])
		// Bound rational expansion before parsing an untrusted numeric exponent.
		if err != nil || exponent < -128 || exponent > 128 {
			return nil, false
		}
	}
	attempt, ok := new(big.Rat).SetString(value)
	if !ok || !attempt.IsInt() || attempt.Cmp(big.NewRat(2, 1)) < 0 {
		return nil, false
	}
	return &nativeRerunReceipt{id, attempt.Num().String()}, true
}

func submitNativeRerun(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, runID, key string, failed bool, jobs []string) (*nativeRerunReceipt, error) {
	if key == "" {
		id, err := uuid.NewRandom()
		if err != nil {
			return nil, err
		}
		key = strings.ReplaceAll(id.String(), "-", "")
	}
	args := []string{"env", "AVR_HOST=" + cfg.Host, "avr", "run", "rerun", runID, "--org", org, "--idempotency-key", key, "--yes"}
	if failed {
		args = append(args, "--failed")
	}
	for _, job := range jobs {
		args = append(args, "--job", job)
	}
	for i := range args {
		args[i] = shellQuotePython(args[i])
	}
	retry := strings.Join(args, " ")
	_, _ = fmt.Fprintln(d.Err, "Invocation key: "+key)
	body := map[string]any{"failed_only": failed, "invocation_key": key}
	if len(jobs) > 0 {
		body["job_ids"] = jobs
	}
	for attempt := range 2 {
		response, err := api.SubmitNativeRerun(ctx, "/orgs/"+org+"/workflow-runs/"+runID+"/rerun", body)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			if receipt, valid := parseNativeRerunReceipt(response); valid {
				return receipt, nil
			}
			return nil, nativeDispatchError("The API returned an invalid Avrea CI rerun receipt. Recover the same attempt:\n" + retry)
		}
		var httpErr *client.HTTPError
		if errors.As(err, &httpErr) {
			if httpErr.Status >= 500 || httpErr.Status == 429 {
				_, _ = fmt.Fprintln(d.Err, "Retry the same request:\n"+retry)
			}
			return nil, apiError(cfg, err, "rerun Avrea workflow")
		}
		var transport *client.TransportError
		if errors.As(err, &transport) {
			if attempt == 0 {
				continue
			}
			return nil, nativeDispatchError("Could not confirm the rerun. Recover the same attempt:\n" + retry)
		}
		return nil, err
	}
	return nil, nativeDispatchError("Could not confirm the rerun. Recover the same attempt:\n" + retry)
}
