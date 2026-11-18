package commands

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func (x configurationContext) applicable(c *cobra.Command, d Dependencies, values []client.ConfigurationValue) ([]client.ConfigurationValue, error) {
	if x.repo == "" {
		return values, nil
	}
	// Only fetch privacy when needed. Unknown privacy must not include private-only values.
	private := false
	needPrivacy := false
	for _, r := range values {
		for _, v := range r.Variants {
			needPrivacy = needPrivacy || v.Availability.PrivateOnly
		}
	}
	if needPrivacy {
		repo, err := x.api.Repository(c.Context(), x.org, x.repo)
		if err != nil {
			return nil, configurationAPIError(x.cfg, err)
		}
		visibility, _ := repo["visibility"].(string)
		switch visibility {
		case "private":
			private = true
		case "public", "internal":
		default:
			_, _ = fmt.Fprintln(d.Err, "Warning: Repository visibility is unknown; private-only values were omitted.")
		}
	}
	result := []client.ConfigurationValue{}
	for _, r := range values {
		variants := []client.ScopedValue{}
		for _, v := range r.Variants {
			a := v.Availability
			if (len(a.Repositories) == 0 || slices.Contains(a.Repositories, x.repo)) && (!a.PrivateOnly || private) {
				variants = append(variants, v)
			}
		}
		if len(variants) > 0 {
			r.Variants = variants
			result = append(result, r)
		}
	}
	return result, nil
}
func (x configurationContext) ownedVariants(r client.ConfigurationValue) []client.ScopedValue {
	if x.repo == "" {
		return r.Variants
	}
	variants := []client.ScopedValue{}
	for _, v := range r.Variants {
		if len(v.Availability.Repositories) == 1 && v.Availability.Repositories[0] == x.repo && !v.Availability.PrivateOnly {
			variants = append(variants, v)
		}
	}
	return variants
}
func newConfigurationRead(d Dependencies, kind string, get bool) *cobra.Command {
	o := configurationOptions{}
	version := ""
	c := &cobra.Command{Use: "list", Short: "List names, scoped-value counts, and update times.", Args: cobra.NoArgs}
	fields := configurationFields(kind)
	if get {
		c.Use = "get NAME"
		c.Short = "Inspect a " + kind + "."
		c.Args = positionalArguments("NAME")
		o.selectionFlags(c)
		if kind == "secret" {
			c.Flags().StringVar(&version, "version-id", "", "Inspect a specific submitted version without resubmitting it.")
		} else {
			fields = variableValueFields
		}
	}
	o.contextFlags(c)
	c.RunE = run(func(c *cobra.Command, args []string) error {
		available := fields
		if c.Flags().Changed("version-id") {
			available = []string{"version_id", "status"}
			if version == "" {
				return configurationUsage("--version-id cannot be empty.")
			}
		}
		if done, err := o.json.prepare(c, d, available); done || err != nil {
			return err
		}
		if err := o.validate(c); err != nil {
			return err
		}
		x, err := o.context(c, d, kind)
		if err != nil {
			return err
		}
		values, err := x.all(c)
		if err != nil {
			return err
		}
		if get {
			named := configurationNamed(values, args[0])
			if named == nil {
				return fmt.Errorf("%s %s was not found in this context", kind, output.CellText(args[0]))
			}
			values = []client.ConfigurationValue{*named}
		}
		values, err = x.applicable(c, d, values)
		if err != nil {
			return err
		}
		if !get {
			if c.Flags().Changed("json") {
				records := []map[string]any{}
				for _, v := range values {
					records = append(records, namedConfigurationRecord(kind, v))
				}
				return o.json.list(c, d, records, fields)
			}
			rows := [][]string{}
			for _, v := range values {
				updated := configurationUpdatedAt(v)
				rows = append(rows, []string{v.Name, fmt.Sprint(len(v.Variants)), updated})
			}
			return output.Table(d.Out, []string{"NAME", "SCOPED VALUES", "UPDATED"}, rows, !d.IsTerminal())
		}
		r := configurationNamed(values, args[0])
		if r == nil {
			return fmt.Errorf("%s %s was not found in this context", kind, output.CellText(args[0]))
		}
		if kind == "variable" || o.variant != "" || o.variantID != "" || version != "" {
			v, err := o.selectVariant(c, d, r.Variants)
			if err != nil {
				return err
			}
			r.Variants = []client.ScopedValue{v}
			if version != "" {
				var result client.ValueWrite
				err := x.api.ConfigurationMetadataRequest(c.Context(), http.MethodGet, x.variantPath(v.ID())+"/versions/"+url.PathEscape(version), nil, &result)
				if err != nil {
					return apiError(x.cfg, err, "read secret version")
				}
				if result.VersionID != version {
					return fmt.Errorf("version response did not identify the requested version")
				}
				switch result.Status {
				case "ENABLED", "DISABLED", "DESTROYED":
				default:
					return fmt.Errorf("invalid secret version status: %s", output.CellText(result.Status))
				}
				record := configurationRecord(result)
				if c.Flags().Changed("json") {
					err = o.json.record(c, d, record, available)
				} else {
					_, err = fmt.Fprintf(d.Out, "Version: %s\nState: %s\n", output.CellText(result.VersionID), output.CellText(result.Status))
				}
				if err != nil {
					return err
				}
				return nil
			}
			if kind == "variable" {
				if c.Flags().Changed("json") {
					return o.json.record(c, d, scopedConfigurationRecord(kind, v), fields)
				}
				if v.Value == nil {
					return fmt.Errorf("this scoped value has no stored value")
				}
				value := *v.Value
				if d.IsTerminal() {
					value = output.MultilineText(value)
				}
				_, err = fmt.Fprint(d.Out, value)
				return err
			}
		}
		if c.Flags().Changed("json") {
			return o.json.record(c, d, namedConfigurationRecord(kind, *r), fields)
		}
		_, err = fmt.Fprintln(d.Out, output.CellText(r.Name))
		if err != nil {
			return err
		}
		if r.Description != nil {
			_, _ = fmt.Fprintln(d.Out, output.CellText(*r.Description))
		}
		rows := [][]string{}
		for _, v := range r.Variants {
			label := configurationLabel(v.Label)
			number, id, created := "not set", "", ""
			if v.CurrentVersion != nil {
				number = fmt.Sprint(*v.CurrentVersion)
			}
			if v.CurrentVersionID != nil {
				id = *v.CurrentVersionID
			}
			if v.CurrentVersionCreatedAt != nil {
				created = *v.CurrentVersionCreatedAt
			}
			scope := configurationAvailabilityText(v.Availability)
			rows = append(rows, []string{label, v.ID(), number, id, created, scope})
		}
		return output.Table(d.Out, []string{"LABEL", "VARIANT ID", "VERSION", "VERSION ID", "CREATED", "AVAILABILITY"}, rows, !d.IsTerminal())
	})
	return c
}

func configurationAvailabilityText(a client.Availability) string {
	repositories := "all repositories"
	if a.PrivateOnly {
		repositories = "private repositories"
	} else if len(a.Repositories) > 0 {
		repositories = "repositories: " + strings.Join(a.Repositories, ", ")
	}
	parts := []string{repositories}
	for _, category := range []struct {
		name     string
		patterns []string
	}{{"environments", a.Environments}, {"branches", a.Branches}, {"workflows", a.Workflows}} {
		if len(category.patterns) > 0 {
			parts = append(parts, category.name+": "+strings.Join(category.patterns, ", "))
		}
	}
	return strings.Join(parts, "; ")
}
func configurationUpdatedAt(r client.ConfigurationValue) string {
	timestamps := []*string{r.UpdatedAt}
	for _, v := range r.Variants {
		timestamps = append(timestamps, v.UpdatedAt, v.CurrentVersionCreatedAt)
	}
	result := ""
	var latest time.Time
	for _, timestamp := range timestamps {
		if timestamp == nil {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339Nano, *timestamp); err == nil && parsed.After(latest) {
			latest = parsed
			result = *timestamp
		}
	}
	return result
}
