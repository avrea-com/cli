package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

type configurationResult struct {
	Name         string               `json:"name"`
	Label        *string              `json:"label,omitempty"`
	Availability *client.Availability `json:"availability,omitempty"`
	cause        error
	ResourceID   string `json:"resource_id,omitempty"`
	VariantID    string `json:"variant_id,omitempty"`
	VersionID    string `json:"version_id,omitempty"`
	Status       string `json:"status"`
	Stage        string `json:"stage,omitempty"`
	Message      string `json:"message,omitempty"`
}

var configurationResultFields = []string{"name", "resource_id", "variant_id", "version_id", "status", "stage", "message", "label", "availability"}

func configurationVersionResult(cfg config.Config, id string, result client.ValueWrite, err error) (client.ValueWrite, error) {
	var he *client.HTTPError
	if err != nil {
		if errors.As(err, &he) && he.Status != 404 && he.Status < 500 {
			return client.ValueWrite{VersionID: id, Status: "unconfirmed"}, configurationAPIError(cfg, err)
		}
		return client.ValueWrite{VersionID: id, Status: "unconfirmed"}, errors.New("write is unconfirmed; do not resubmit it automatically")
	}
	if result.VersionID != id {
		return client.ValueWrite{VersionID: id, Status: "unconfirmed"}, errors.New("version response did not identify the submitted write")
	}
	if result.Status != "ENABLED" {
		return result, fmt.Errorf("version is not enabled (%s)", output.CellText(result.Status))
	}
	return result, nil
}
func waitConfigurationWrite(c *cobra.Command, d Dependencies, x configurationContext, variantID, versionID string, wait time.Duration) error {
	if wait == 0 {
		return errors.New("write accepted but not yet confirmed")
	}
	ctx, cancel := context.WithTimeout(c.Context(), wait)
	defer cancel()
	deadline := d.Now().Add(wait)
	for {
		var result client.ValueWrite
		err := x.api.ConfigurationMetadataRequest(ctx, http.MethodGet, x.variantPath(variantID)+"/versions/"+url.PathEscape(versionID), nil, &result)
		_, checked := configurationVersionResult(x.cfg, versionID, result, err)
		if checked == nil {
			return nil
		}
		var he *client.HTTPError
		if err == nil || errors.As(err, &he) && he.Status != 404 && he.Status < 500 {
			return checked
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return configurationWriteError(x.cfg, ctx.Err())
		}
		remaining := deadline.Sub(d.Now())
		if remaining <= 0 || ctx.Err() != nil {
			return errors.New("write accepted but confirmation timed out")
		}
		if err := d.Wait(ctx, min(time.Second, remaining)); err != nil {
			if errors.Is(err, context.Canceled) {
				return configurationWriteError(x.cfg, err)
			}
			return errors.New("write accepted but confirmation was interrupted")
		}
	}
}

const configurationFileImportHint = "File secrets cannot be imported from dotenv; use avr secret set NAME --type file < path."

func newConfigurationSet(d Dependencies, kind string) *cobra.Command {
	o := configurationOptions{}
	var body, envFile, description, valueType string
	wait := 30 * time.Second
	c := &cobra.Command{Use: "set [NAME]", Short: "Create or replace a scoped value.", Args: cobra.MaximumNArgs(1)}
	c.Long = c.Short + "\n\n" + jsonFieldsHelp(configurationResultFields)
	o.contextFlags(c)
	o.selectionFlags(c)
	o.availabilityFlags(c, false)
	c.Flags().StringVarP(&envFile, "env-file", "f", "", "Read literal NAME=value entries from a dotenv file (- for stdin); no expansion.")
	c.Flags().StringVar(&description, "description", "", "Description for a newly created name; use edit to change an existing description.")
	if kind == "variable" {
		c.Flags().StringVarP(&body, "body", "b", "", "Variable value; otherwise prompt or read stdin.")
		c.MarkFlagsMutuallyExclusive("body", "env-file")
	} else {
		c.Flags().StringVar(&valueType, "type", "env_var", "Secret type: env_var or file; chosen at creation.")
		c.Flags().DurationVar(&wait, "wait", 30*time.Second, "Wait for write confirmation. With 0s, return the receipt immediately; unconfirmed writes exit nonzero.")
	}
	c.RunE = run(func(c *cobra.Command, args []string) error {
		if done, err := o.json.prepare(c, d, configurationResultFields); done || err != nil {
			return err
		}
		if err := o.validate(c); err != nil {
			return err
		}
		if wait < 0 || wait > time.Hour {
			return configurationUsage("--wait must be between 0s and 1h.")
		}
		if kind == "secret" && valueType != "env_var" && valueType != "file" {
			return configurationUsage("--type must be env_var or file.")
		}
		if err := validateConfigurationDescription(description); err != nil {
			return err
		}
		var inputs []configurationInput
		var err error
		if c.Flags().Changed("env-file") {
			if envFile == "" || len(args) != 0 {
				return configurationUsage("Use either NAME or --env-file PATH.")
			}
			if kind == "secret" && valueType == "file" {
				return configurationUsage(configurationFileImportHint)
			}
			inputs, err = configurationEnvInput(c.Context(), d.In, envFile)
			if err != nil {
				return err
			}
		} else {
			if len(args) != 1 {
				return configurationUsage("Specify NAME or --env-file PATH.")
			}
			if err = validateConfigurationName(args[0]); err != nil {
				return err
			}
			inputs = []configurationInput{{name: strings.ToUpper(args[0])}}
		}
		x, err := o.context(c, d, kind)
		if err != nil {
			return err
		}
		values, err := x.all(c)
		if err != nil {
			return err
		}
		base := client.Availability{}
		if x.repo != "" {
			base.Repositories = []string{x.repo}
		}
		availability, err := o.availability(c, d, x, base)
		if err != nil {
			return err
		}
		plans := make([]configurationSetPlan, 0, len(inputs))
		for _, input := range inputs {
			plan, err := planConfigurationSet(c, d, x, o, values, input, description, valueType, availability)
			if err != nil {
				return err
			}
			if c.Flags().Changed("env-file") && plan.asFile {
				return configurationUsage(configurationFileImportHint)
			}
			plans = append(plans, plan)
		}
		if !c.Flags().Changed("env-file") {
			plans[0].input.value, err = configurationInputValue(c, d, kind, body, plans[0].asFile)
			if err != nil {
				return err
			}
		}
		results := make([]configurationResult, 0, len(plans))
		failed := false
		var failure error
		for _, plan := range plans {
			if failed {
				results = append(results, configurationResult{Name: plan.input.name, Status: "unattempted"})
				continue
			}
			result := plan.execute(c, d, x, wait)
			results = append(results, result)
			failed = result.Status != "saved"
			if failed {
				failure = result.cause
			}
		}
		if c.Flags().Changed("json") {
			if len(args) > 0 {
				err = o.json.record(c, d, configurationRecord(results[0]), configurationResultFields)
			} else {
				records := []map[string]any{}
				for _, r := range results {
					records = append(records, configurationRecord(r))
				}
				err = o.json.list(c, d, records, configurationResultFields)
			}
		} else {
			for _, r := range results {
				if _, err = fmt.Fprintf(d.Out, "%s: %s\n", output.CellText(r.Name), r.Status); err != nil {
					break
				}
				if r.Availability != nil {
					label := configurationLabel(r.Label)
					scope, _ := json.Marshal(r.Availability)
					if _, err = fmt.Fprintf(d.Out, "  Scope: %s (%s); availability: %s\n", output.CellText(label), output.CellText(r.VariantID), output.CellText(string(scope))); err != nil {
						break
					}
				}
				if r.Message != "" && (failure == nil || r.Message != failure.Error()) {
					_, _ = fmt.Fprintln(d.Err, output.CellText(r.Message))
				}
				if r.Status != "saved" && r.Status != "unattempted" {
					if r.ResourceID != "" {
						_, _ = fmt.Fprintf(d.Err, "Resource: %s\n", output.CellText(r.ResourceID))
					}
					if r.VariantID != "" {
						_, _ = fmt.Fprintf(d.Err, "Variant: %s\n", output.CellText(r.VariantID))
					}
					_, _ = fmt.Fprintf(d.Err, "Stage: %s\n", r.Stage)
					if r.VersionID != "" && kind == "secret" {
						_, _ = fmt.Fprintf(d.Err, "Check: avr secret get %s --org %s --variant-id %s --version-id %s\n", r.Name, output.CellText(x.org), output.CellText(r.VariantID), output.CellText(r.VersionID))
					}
				}
			}
		}
		if err != nil {
			configurationReceiptFallback(d, results)
			return err
		}
		if failed {
			if failure != nil {
				return failure
			}
			return &ExitError{1, "Error: A write was not confirmed. Inspect the receipt before retrying; completed steps were retained."}
		}
		return nil
	})
	return c
}

type configurationSetPlan struct {
	input        configurationInput
	parent       *client.ConfigurationValue
	variant      *client.ScopedValue
	availability client.Availability
	label        *string
	description  string
	asFile       bool
}

func planConfigurationSet(c *cobra.Command, d Dependencies, x configurationContext, o configurationOptions, values []client.ConfigurationValue, input configurationInput, description, valueType string, a client.Availability) (configurationSetPlan, error) {
	p := configurationSetPlan{input: input, parent: configurationNamed(values, input.name), description: description, asFile: valueType == "file"}
	if c.Flags().Changed("label") {
		p.label = &o.label
	}
	p.availability = a
	explicitScope := o.scopeChanged(c)
	if p.parent != nil {
		if c.Flags().Changed("description") && (p.parent.Description == nil && description != "" || p.parent.Description != nil && *p.parent.Description != description) {
			return p, configurationUsage("Use edit --description to change an existing description.")
		}
		if !c.Flags().Changed("type") {
			p.asFile = p.parent.AsFile
		}
		if c.Flags().Changed("type") && p.parent.AsFile != p.asFile {
			return p, configurationUsage("An existing secret's type cannot be changed.")
		}
		variants := x.ownedVariants(*p.parent)
		if o.variant != "" || o.variantID != "" {
			v, err := o.selectVariant(c, d, variants)
			if err != nil {
				return p, err
			}
			if explicitScope && !equalAvailability(v.Availability, a) {
				return p, configurationUsage("The selected variant has different availability; use edit to change its scope.")
			}
			p.variant = &v
		} else if explicitScope {
			for _, v := range variants {
				if equalAvailability(v.Availability, a) {
					copy := v
					p.variant = &copy
					break
				}
			}
		} else if len(variants) > 0 {
			v, err := o.selectVariant(c, d, variants)
			if err != nil {
				return p, err
			}
			p.variant = &v
		}
		if p.variant != nil && c.Flags().Changed("label") && (p.variant.Label == nil || *p.variant.Label != o.label) {
			return p, configurationUsage("Use edit --label to change an existing label.")
		}
		if p.variant == nil {
			if len(p.parent.Variants) >= 20 {
				return p, configurationUsage("A name can have at most 20 scoped values.")
			}
			for _, v := range p.parent.Variants {
				if p.label != nil && v.Label != nil && *p.label == *v.Label {
					return p, configurationUsage("Another scoped value already uses this label.")
				}
			}
		}
	} else if o.variant != "" || o.variantID != "" {
		return p, configurationUsage("The selected variant does not exist. Use --label to name a new scoped value.")
	}
	if p.variant == nil && x.repo == "" && !c.Flags().Changed("repos") && !c.Flags().Changed("visibility") {
		return p, configurationUsage("New organization values require explicit --repos or --visibility all|private. Alternatively use --repo.")
	}
	return p, nil
}
func (p configurationSetPlan) execute(c *cobra.Command, d Dependencies, x configurationContext, wait time.Duration) configurationResult {
	r := configurationResult{Name: p.input.name, Status: "unconfirmed", Stage: "create-name"}
	fail := func(err error) configurationResult {
		r.Status = configurationWriteStatus(err)
		r.cause = configurationWriteError(x.cfg, err)
		r.Message = r.cause.Error()
		return r
	}
	parent := p.parent
	if parent == nil {
		parent = &client.ConfigurationValue{}
		var description *string
		if p.description != "" {
			description = &p.description
		}
		body := map[string]any{"name": p.input.name, "description": description}
		if x.kind == "secret" {
			body["as_file"] = p.asFile
		}
		if err := x.api.ConfigurationMetadataRequest(c.Context(), http.MethodPost, x.base(), body, parent); err != nil {
			return fail(err)
		}
		if parent.ID() == "" {
			return fail(errors.New("name creation response was incomplete; inspect get before retrying"))
		}
	}
	r.ResourceID = parent.ID()
	r.Stage = "create-scope"
	v := p.variant
	if v == nil {
		v = &client.ScopedValue{}
		body := map[string]any{"label": p.label, "availability": p.availability}
		if err := x.api.ConfigurationMetadataRequest(c.Context(), http.MethodPost, x.parent(parent.ID())+"/variants", body, v); err != nil {
			return fail(err)
		}
		if v.ID() == "" {
			return fail(errors.New("scope creation response was incomplete; inspect get before retrying"))
		}
	}
	r.VariantID = v.ID()
	if p.variant != nil {
		r.Label = v.Label
		r.Availability = &v.Availability
	} else {
		r.Label = p.label
		r.Availability = &p.availability
	}
	r.Stage = "write-value"
	var write client.ValueWrite
	if err := x.api.ConfigurationRequest(c.Context(), http.MethodPut, x.variantPath(v.ID())+"/value", map[string]string{"value": p.input.value}, &write); err != nil {
		r = fail(err)
		if r.Status == "unconfirmed" && !errors.Is(err, context.Canceled) {
			r.Message = "The write outcome is unconfirmed and no version ID was received; do not retry automatically."
			r.cause = errors.New(r.Message)
		}
		return r
	}
	r.VersionID = write.VersionID
	if write.VersionID == "" {
		return fail(errors.New("write response did not contain a version ID; do not retry automatically"))
	}
	if x.kind == "secret" && write.Status != "stored" {
		r.Stage = "confirm-value"
		if err := waitConfigurationWrite(c, d, x, v.ID(), write.VersionID, wait); err != nil {
			r.Message = err.Error()
			r.cause = err
			return r
		}
	}
	r.Status = "saved"
	r.Stage = "complete"
	return r
}

// A failed output filter must not discard the identifiers of accepted writes.
func configurationReceiptFallback(d Dependencies, results []configurationResult) {
	_, _ = fmt.Fprintln(d.Err, "Retained write receipts:")
	_ = output.JSONValue(context.Background(), d.Err, results, "", d.IsErrorTerminal())
}
