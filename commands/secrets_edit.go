package commands

import (
	"fmt"
	"net/http"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func newConfigurationEdit(d Dependencies, kind string) *cobra.Command {
	o := configurationOptions{}
	var description string
	c := &cobra.Command{Use: "edit NAME", Short: "Edit description, label, or availability without replacing a value.", Args: positionalArguments("NAME")}
	c.Long = c.Short + "\n\n" + jsonFieldsHelp(configurationResultFields)
	o.contextFlags(c)
	o.selectionFlags(c)
	o.availabilityFlags(c, true)
	c.Flags().StringVar(&description, "description", "", "Organization-wide description shared by all scoped values; an empty string clears it.")
	c.RunE = run(func(c *cobra.Command, args []string) error {
		if done, err := o.json.prepare(c, d, configurationResultFields); done || err != nil {
			return err
		}
		if err := o.validate(c); err != nil {
			return err
		}
		if err := validateConfigurationDescription(description); err != nil {
			return err
		}
		scopeEdit := o.scopeChanged(c) || c.Flags().Changed("label") || o.clearLabel
		descEdit := c.Flags().Changed("description")
		if !scopeEdit && !descEdit {
			return configurationUsage("Specify at least one field to edit.")
		}
		x, err := o.context(c, d, kind)
		if err != nil {
			return err
		}
		values, err := x.all(c)
		if err != nil {
			return err
		}
		parent := configurationNamed(values, args[0])
		if parent == nil {
			return fmt.Errorf("%s was not found", output.CellText(args[0]))
		}
		if x.repo != "" && len(x.ownedVariants(*parent)) == 0 {
			return configurationUsage("This name has no repository-owned scoped values. Use --org to manage shared organization configuration.")
		}
		var variant client.ScopedValue
		var replacement map[string]any
		if scopeEdit {
			variant, err = o.selectVariant(c, d, x.ownedVariants(*parent))
			if err != nil {
				return err
			}
			a, err := o.availability(c, d, x, variant.Availability)
			if err != nil {
				return err
			}
			label := variant.Label
			if c.Flags().Changed("label") {
				label = &o.label
			}
			if o.clearLabel {
				label = nil
			}
			for _, sibling := range parent.Variants {
				if sibling.ID() == variant.ID() {
					continue
				}
				if equalAvailability(a, sibling.Availability) {
					return configurationUsage("Another scoped value already has this availability.")
				}
				if label != nil && sibling.Label != nil && *label == *sibling.Label {
					return configurationUsage("Another scoped value already uses this label.")
				}
			}
			replacement = map[string]any{"label": label, "availability": a}
			if kind == "secret" {
				replacement["rotation_reminder_days"] = variant.RotationReminderDays
			}
		} else if o.variant != "" || o.variantID != "" {
			return configurationUsage("Description-only edits apply to the whole name; omit the variant selector.")
		}
		result := configurationResult{Name: parent.Name, ResourceID: parent.ID(), Status: "saved", Stage: "complete"}
		if scopeEdit {
			result.VariantID = variant.ID()
			err = x.api.ConfigurationMetadataRequest(c.Context(), http.MethodPut, x.variantPath(variant.ID()), replacement, nil)
			if err != nil {
				result.Status = configurationWriteStatus(err)
				err = configurationWriteError(x.cfg, err)
				result.Stage = "edit-scope"
				result.Message = err.Error()
			}
		}
		if err == nil && descEdit {
			var value *string
			if description != "" {
				value = &description
			}
			err = x.api.ConfigurationMetadataRequest(c.Context(), http.MethodPatch, x.parent(parent.ID()), map[string]any{"description": value}, nil)
			if err != nil {
				result.Status = configurationWriteStatus(err)
				err = configurationWriteError(x.cfg, err)
				result.Stage = "edit-description"
				result.Message = err.Error()
				if scopeEdit {
					result.Message = "Scope changes were saved; description update " + result.Status + ". " + result.Message
				}
			}
		}
		if c.Flags().Changed("json") {
			if outErr := o.json.record(c, d, configurationRecord(result), configurationResultFields); outErr != nil {
				configurationReceiptFallback(d, []configurationResult{result})
				return outErr
			}
		} else {
			if err == nil {
				_, err = fmt.Fprintf(d.Out, "Updated %s.\n", output.CellText(parent.Name))
			} else if scopeEdit && result.Stage == "edit-description" {
				_, _ = fmt.Fprintln(d.Err, "Scope changes were saved; description update "+result.Status+".")
			}
		}
		return err
	})
	return c
}
func newConfigurationDelete(d Dependencies, kind string) *cobra.Command {
	o := configurationOptions{}
	var all, yes bool
	c := &cobra.Command{Use: "delete NAME", Short: "Delete one scoped value, or the whole name with --all.", Args: positionalArguments("NAME")}
	c.Long = c.Short + "\n\n" + jsonFieldsHelp(configurationResultFields)
	o.contextFlags(c)
	o.selectionFlags(c)
	c.Flags().BoolVar(&all, "all", false, "Delete the name and all of its scoped values.")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation.")
	c.MarkFlagsMutuallyExclusive("all", "variant")
	c.MarkFlagsMutuallyExclusive("all", "variant-id")
	c.RunE = run(func(c *cobra.Command, args []string) error {
		if done, err := o.json.prepare(c, d, configurationResultFields); done || err != nil {
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
		parent := configurationNamed(values, args[0])
		if parent == nil {
			return fmt.Errorf("%s was not found", output.CellText(args[0]))
		}
		path := x.parent(parent.ID())
		target := parent.Name + " and all its scoped values"
		result := configurationResult{Name: parent.Name, ResourceID: parent.ID(), Status: "deleted"}
		if all {
			// A repository context must not delete other repositories' shared data.
			if !c.Flags().Changed("org") || x.repo != "" {
				return configurationUsage("Whole-name deletion requires an explicit --org context without --repo.")
			}
		} else {
			v, err := o.selectVariant(c, d, x.ownedVariants(*parent))
			if err != nil {
				return err
			}
			path = x.variantPath(v.ID())
			result.VariantID = v.ID()
			target = parent.Name + " scoped value " + v.ID()
		}
		if !yes {
			if !d.IsInputTerminal() {
				return configurationUsage("Deletion requires --yes in non-interactive use.")
			}
			prompt := configurationPrompt(d)
			if err := confirmMutation(c.Context(), prompt, "delete configuration", "Delete "+output.CellText(target)+"?"); err != nil {
				return err
			}
		}
		err = x.api.ConfigurationMetadataRequest(c.Context(), http.MethodDelete, path, nil, nil)
		if err != nil {
			result.Status = configurationWriteStatus(err)
			err = configurationWriteError(x.cfg, err)
			result.Message = err.Error()
		}
		if c.Flags().Changed("json") {
			if outErr := o.json.record(c, d, configurationRecord(result), configurationResultFields); outErr != nil {
				configurationReceiptFallback(d, []configurationResult{result})
				return outErr
			}
			return err
		}
		if err != nil {
			configurationReceiptFallback(d, []configurationResult{result})
			return err
		}
		_, err = fmt.Fprintf(d.Out, "Deleted %s.\n", output.CellText(target))
		return err
	})
	return c
}
