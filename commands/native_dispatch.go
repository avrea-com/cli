package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/google/uuid"
)

var nativeReceiptID = lazyRegexp(`^wap-[0-9a-f]{32}$`)
var nativeRunID = lazyRegexp(`^awr-[0-9a-f]{32}$`)
var nativeDecimalPattern = lazyRegexp(`^([+-]?)([0-9]*)(?:\.([0-9]*))?(?:[eE]([+-]?[0-9]+))?$`)

type nativeReceipt struct {
	id, state, reason, ref, commit, retry string
	runs                                  []string
}

func nativeDispatchError(message string) error { return &ExitError{1, "Error: " + message} }

func parseNativeReceipt(body map[string]any) (nativeReceipt, error) {
	invalid := nativeDispatchError("The API returned an invalid native dispatch receipt.")
	data, ok := body["data"].(map[string]any)
	if !ok {
		return nativeReceipt{}, invalid
	}
	id, iok := data["dispatch_id"].(string)
	state, sok := data["state"].(string)
	ref, rok := data["ref"].(string)
	commit, cok := data["commit_sha"].(string)
	runs, aok := data["run_ids"].([]any)
	if !iok || !nativeReceiptID().MatchString(id) || !sok || (state != "pending" && state != "admitted" && state != "rejected") || !rok || !cok || !aok {
		return nativeReceipt{}, invalid
	}
	receipt := nativeReceipt{id: id, state: state, ref: ref, commit: commit}
	if data["reason"] != nil {
		var ok bool
		receipt.reason, ok = data["reason"].(string)
		if !ok {
			return nativeReceipt{}, invalid
		}
	}
	for _, value := range runs {
		run, ok := value.(string)
		if !ok || !nativeRunID().MatchString(run) {
			return nativeReceipt{}, invalid
		}
		receipt.runs = append(receipt.runs, run)
	}
	return receipt, nil
}
func (r nativeReceipt) pendingMessage() string {
	message := "Dispatch " + r.id + " was accepted and is awaiting admission."
	if r.retry != "" {
		message += "\nResume following this dispatch:\n" + r.retry
	}
	return message
}

// Decimal spelling follows Python Decimal without a binary float round trip.
// Scientific values stay scientific, so large exponents cannot allocate a huge string.
func nativeNumber(value string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), "_", "")
	lower := strings.ToLower(strings.TrimLeft(value, "+-"))
	if strings.HasPrefix(lower, "nan") || strings.HasPrefix(lower, "snan") || lower == "inf" || lower == "infinity" {
		return "", nativeDispatchError("Native workflow numbers must be finite.")
	}
	parts := nativeDecimalPattern().FindStringSubmatch(value)
	if parts == nil || parts[2]+parts[3] == "" {
		return "", errors.New("invalid decimal")
	}
	exponent := int64(0)
	if parts[4] != "" {
		var err error
		exponent, err = strconv.ParseInt(parts[4], 10, 64)
		if err != nil {
			return "", err
		}
	}
	if exponent < math.MinInt64+int64(len(parts[3])) {
		return "", errors.New("invalid decimal exponent")
	}
	exponent -= int64(len(parts[3]))
	digits := strings.TrimLeft(parts[2]+parts[3], "0")
	if digits == "" {
		digits = "0"
	}
	if exponent > math.MaxInt64-int64(len(digits))+1 {
		return "", errors.New("invalid decimal exponent")
	}
	adjusted := exponent + int64(len(digits)) - 1
	sign := ""
	if parts[1] == "-" {
		sign = "-"
	}
	if exponent > 0 || adjusted < -6 {
		coefficient := digits[:1]
		if len(digits) > 1 {
			coefficient += "." + digits[1:]
		}
		suffix := strconv.FormatInt(adjusted, 10)
		if adjusted >= 0 {
			suffix = "+" + suffix
		}
		return sign + coefficient + "E" + suffix, nil
	}
	point := int64(len(digits)) + exponent
	if point <= 0 {
		return sign + "0." + strings.Repeat("0", int(-point)) + digits, nil
	}
	if point < int64(len(digits)) {
		return sign + digits[:int(point)] + "." + digits[int(point):], nil
	}
	return sign + digits, nil
}

// Match json.dumps strings in the printed replay command, including ASCII escaping.
func nativeJSONString(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			switch {
			case r < 32 || r >= 127 && r <= 0xffff:
				fmt.Fprintf(&b, `\u%04x`, r)
			case r > 0xffff:
				r -= 0x10000
				fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

type nativeInput struct{ name, value string }

func nativeInputs(raw []byte, fields []string, kinds map[string]string) (string, error) {
	var inputs []nativeInput
	add := func(name, value string) {
		for i := range inputs {
			if inputs[i].name == name {
				inputs[i].value = value
				return
			}
		}
		inputs = append(inputs, nativeInput{name, value})
	}
	if raw != nil {
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		if _, err := decoder.Token(); err != nil {
			return "", err
		}
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return "", err
			}
			name, ok := token.(string)
			if !ok {
				return "", nativeDispatchError("JSON inputs must be an object.")
			}
			var value json.RawMessage
			if err := decoder.Decode(&value); err != nil {
				return "", err
			}
			add(name, string(value))
		}
	} else {
		for _, field := range fields {
			name, value, _ := strings.Cut(field, "=")
			add(strings.TrimSpace(name), value)
		}
	}
	// Collapse duplicate keys before validating their final values, as Python dicts do.
	for i, input := range inputs {
		value := input.value
		if raw != nil {
			decoder := json.NewDecoder(strings.NewReader(value))
			decoder.UseNumber()
			var scalar any
			if err := decoder.Decode(&scalar); err != nil {
				return "", err
			}
			switch v := scalar.(type) {
			case string:
				value = nativeJSONString(v)
			case bool:
				value = strconv.FormatBool(v)
			case json.Number:
				value = v.String()
				if value == "-0" {
					value = "0"
				}
				if strings.ContainsAny(value, ".eE") {
					var err error
					value, err = nativeNumber(value)
					if err != nil {
						return "", err
					}
				}
			default:
				return "", nativeDispatchError("Native workflow inputs must be strings, booleans, or finite numbers.")
			}
		} else {
			switch kind := kinds[input.name]; kind {
			case "boolean":
				if !strings.EqualFold(value, "true") && !strings.EqualFold(value, "false") {
					return "", nativeDispatchError(fmt.Sprintf("Input %s must be true or false.", pythonQuote(output.CellText(input.name))))
				}
				value = strings.ToLower(value)
			case "number":
				number, err := nativeNumber(value)
				if err != nil {
					var exit *ExitError
					if errors.As(err, &exit) {
						return "", err
					}
					return "", nativeDispatchError(fmt.Sprintf("Input %s must be a number.", pythonQuote(output.CellText(input.name))))
				}
				value = number
			case "string", "choice", "environment", "":
				value = nativeJSONString(value)
			default:
				return "", nativeDispatchError(fmt.Sprintf("Unsupported workflow input type: %s", output.CellText(kind)))
			}
		}
		inputs[i].value = nativeJSONString(input.name) + ":" + value
	}
	values := make([]string, 0, len(inputs))
	for _, input := range inputs {
		values = append(values, input.value)
	}
	return "{" + strings.Join(values, ",") + "}", nil
}

func submitNativeWorkflow(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, repo, workflow, ref string, hasRef bool, fields []string, raw []byte, key string) (nativeReceipt, error) {
	base := "/orgs/" + url.PathEscape(org) + "/repos/" + url.PathEscape(repo)
	kinds := map[string]string{}
	if !hasRef || len(fields) > 0 {
		query := url.Values{}
		if ref != "" {
			query.Set("ref", ref)
		}
		body, err := api.Get(ctx, base+"/workflows/"+url.PathEscape(workflow)+"/dispatch-metadata", query)
		if err != nil {
			if ctx.Err() != nil {
				return nativeReceipt{}, ctx.Err()
			}
			var transport *client.TransportError
			if errors.As(err, &transport) {
				return nativeReceipt{}, nativeDispatchError("Could not read native workflow inputs; no dispatch was submitted.")
			}
			return nativeReceipt{}, apiError(cfg, err, "read native workflow inputs")
		}
		data, ok := body["data"].(map[string]any)
		metadataRef, rok := data["ref"].(string)
		dispatchable, dok := data["dispatchable"].(bool)
		allowed, aok := data["can_dispatch"].(bool)
		inputs, iok := data["inputs"].([]any)
		if !ok || !rok || !dok || !aok || !iok {
			return nativeReceipt{}, nativeDispatchError("The API returned invalid native workflow metadata.")
		}
		for _, item := range inputs {
			input, ok := item.(map[string]any)
			name, nok := input["name"].(string)
			kind, kok := input["type"].(string)
			if !ok || !nok || !kok {
				return nativeReceipt{}, nativeDispatchError("The API returned invalid native workflow metadata.")
			}
			kinds[name] = kind
		}
		if !dispatchable {
			return nativeReceipt{}, nativeDispatchError("Manual dispatch is not enabled for this native workflow at the selected ref.")
		}
		if !allowed {
			return nativeReceipt{}, nativeDispatchError("Native workflow dispatch is not permitted for this repository and user.")
		}
		if ref == "" {
			ref = metadataRef
		}
	}
	if ref == "" {
		return nativeReceipt{}, nativeDispatchError("Could not determine the workflow's default branch. Pass --ref explicitly.")
	}
	inputs, err := nativeInputs(raw, fields, kinds)
	if err != nil {
		return nativeReceipt{}, err
	}
	content := []byte(`{"workflow_id":` + nativeJSONString(workflow) + `,"ref":` + nativeJSONString(ref) + `,"inputs":` + inputs + `}`)
	if key == "" {
		id, err := uuid.NewRandom()
		if err != nil {
			return nativeReceipt{}, err
		}
		key = strings.ReplaceAll(id.String(), "-", "")
	}
	args := []string{"env", "AVR_HOST=" + cfg.Host, "avr", "workflow", "run", workflow, "--org", org, "--repo", repo, "--ref", ref, "--json", "--idempotency-key", key}
	for i := range args {
		args[i] = shellQuotePython(args[i])
	}
	retry := "printf '%s' " + shellQuotePython(inputs) + " | " + strings.Join(args, " ")
	_, _ = fmt.Fprintln(d.Err, "Invocation key: "+key)
	var body map[string]any
	for attempt := 0; attempt < 2; attempt++ {
		body, err = api.SubmitNativeDispatch(ctx, base+"/workflow-dispatches", content, key)
		var transport *client.TransportError
		if err == nil || !errors.As(err, &transport) {
			break
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return nativeReceipt{}, ctx.Err()
		}
		var transport *client.TransportError
		if errors.As(err, &transport) {
			return nativeReceipt{}, nativeDispatchError(fmt.Sprintf("Dispatch response was lost. Retry the exact request:\n%s", retry))
		}
		var httpErr *client.HTTPError
		if errors.As(err, &httpErr) && (httpErr.Status >= 500 || httpErr.Status == 429) {
			_, _ = fmt.Fprintln(d.Err, "Retry the exact request:\n"+retry)
		}
		return nativeReceipt{}, apiError(cfg, err, "dispatch native workflow")
	}
	receipt, err := parseNativeReceipt(body)
	if err != nil {
		_, _ = fmt.Fprintln(d.Err, "Retry the exact request:\n"+retry)
		return nativeReceipt{}, err
	}
	receipt.retry = retry
	return receipt, nil
}

func followNativeDispatch(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, base string, receipt nativeReceipt) (string, error) {
	deadline := d.Now().Add(90 * time.Second)
	id := receipt.id
	for {
		switch receipt.state {
		case "admitted":
			if len(receipt.runs) != 1 {
				return "", nativeDispatchError("The admitted dispatch did not identify exactly one native run.")
			}
			return receipt.runs[0], nil
		case "rejected":
			reason := receipt.reason
			if reason == "" {
				reason = "unknown reason"
			}
			return "", nativeDispatchError(fmt.Sprintf("Native dispatch %s was rejected: %s", id, output.CellText(reason)))
		}
		remaining := deadline.Sub(d.Now())
		if remaining <= 0 {
			return "", errors.New(receipt.pendingMessage())
		}
		requestCtx, cancel := context.WithTimeout(ctx, remaining)
		body, err := api.Get(requestCtx, base+"/workflow-dispatches/"+url.PathEscape(id), nil)
		cancel()
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err == nil {
			next, e := parseNativeReceipt(body)
			if e != nil {
				return "", e
			}
			if next.id != id {
				return "", nativeDispatchError("The API returned a different dispatch receipt.")
			}
			next.retry = receipt.retry
			receipt = next
			if receipt.state != "pending" {
				continue
			}
		} else {
			var he *client.HTTPError
			var transport *client.TransportError
			if errors.As(err, &he) {
				if he.Status < 500 && he.Status != 429 {
					return "", apiError(cfg, err, "follow native dispatch")
				}
			} else if !errors.As(err, &transport) && !errors.Is(err, context.DeadlineExceeded) {
				return "", err
			}
		}
		remaining = deadline.Sub(d.Now())
		if remaining > 0 {
			if err := d.Wait(ctx, min(time.Second, remaining)); err != nil {
				return "", err
			}
		}
	}
}

type nativeInvocationKey struct{ target *string }

func (v nativeInvocationKey) String() string { return *v.target }
func (v nativeInvocationKey) Type() string   { return "uuid" }
func (v nativeInvocationKey) Set(value string) error {
	id, err := uuid.Parse(value)
	if err != nil {
		return fmt.Errorf("%s is not a valid UUID", pythonQuote(output.CellText(value)))
	}
	*v.target = strings.ReplaceAll(id.String(), "-", "")
	return nil
}
