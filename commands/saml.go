package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var samlFields = []string{"organization_saml_config_id", "organization_id", "idp_entity_id", "idp_sso_url", "idp_slo_url", "name_id_format", "attr_email", "attr_given_name", "attr_family_name", "attr_groups", "is_enforced", "jit_provisioning", "allow_idp_initiated", "default_role", "created_at", "updated_at"}

func samlBase(org string) string { return "/orgs/" + url.PathEscape(org) + "/saml" }
func newSAML(d Dependencies) *cobra.Command {
	c := &cobra.Command{Use: "saml", Short: "Configure SAML single sign-on for an organization."}
	c.AddCommand(newSAMLShow(d), newSAMLMetadata(d), newSAMLTest(d), newSAMLConfigure(d), newSAMLEnforcement(d), newSAMLRemove(d))
	return c
}
func renderSAML(d Dependencies, row map[string]any) error {
	optional := func(key string) string {
		if value := fieldText(row, key); value != "" {
			return value
		}
		return "-"
	}
	enabled := func(key string) string {
		if row[key] == true {
			return "enabled"
		}
		return "disabled"
	}
	return output.KeyValues(d.Out, [][2]string{
		{"IdP entity ID", pythonValue(row["idp_entity_id"])}, {"IdP SSO URL", pythonValue(row["idp_sso_url"])},
		{"IdP SLO URL", optional("idp_slo_url")}, {"NameID format", pythonValue(row["name_id_format"])},
		{"Email attribute", pythonValue(row["attr_email"])}, {"Given-name attribute", optional("attr_given_name")},
		{"Family-name attribute", optional("attr_family_name")}, {"Groups attribute", optional("attr_groups")},
		{"Default role", pythonValue(row["default_role"])}, {"JIT provisioning", enabled("jit_provisioning")},
		{"IdP-initiated login", enabled("allow_idp_initiated")}, {"SSO enforcement", enabled("is_enforced")},
		{"Updated", formatTimestamp(fieldText(row, "updated_at"))},
	})
}
func newSAMLShow(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "show", Short: "Show the current SAML configuration (admin only).", Args: cobra.NoArgs}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, samlFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.SAML(cmd.Context(), samlBase(id))
		if err != nil {
			return apiError(cfg, err, "fetch SAML configuration")
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record.Fields, samlFields)
		}
		return renderSAML(d, record.Fields)
	})
	return c
}
func newSAMLMetadata(d Dependencies) *cobra.Command {
	var org string
	c := &cobra.Command{Use: "sp-metadata", Short: "Print Avrea's SAML service-provider metadata XML.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		slug := orgSlug(cmd.Context(), cfg, api, id)
		if slug == id {
			return &ExitError{1, "Error: Could not resolve organization slug for " + output.CellText(id) + "."}
		}
		raw, err := api.Raw(cmd.Context(), http.MethodGet, "/saml/"+url.PathEscape(slug)+"/metadata", nil)
		if err != nil {
			return apiError(cfg, err, "fetch SAML SP metadata")
		}
		text := strings.ToValidUTF8(string(raw), "\ufffd")
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		_, err = io.WriteString(d.Out, text)
		return err
	})
	return c
}
func openFlowBrowser(ctx context.Context, d Dependencies, target, opened string) error {
	if err := d.OpenBrowser(ctx, target); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, err = fmt.Fprintln(d.Err, "Unable to open browser automatically. Please open the URL manually.")
		return err
	}
	_, err := fmt.Fprintln(d.Out, opened)
	return err
}
func newSAMLTest(d Dependencies) *cobra.Command {
	var org string
	var noBrowser bool
	c := &cobra.Command{Use: "test", Short: "Test the SAML connection in a browser (admin only).", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the test URL without opening a browser.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		slug := orgSlug(cmd.Context(), cfg, api, id)
		if slug == id {
			return &ExitError{1, "Error: Could not resolve organization slug for " + output.CellText(id) + "."}
		}
		target := cfg.Host + "/saml/" + url.PathEscape(slug) + "/test/login"
		if _, err = fmt.Fprintln(d.Out, target); err != nil {
			return err
		}
		if noBrowser {
			return nil
		}
		return openFlowBrowser(cmd.Context(), d, target, "Opened browser for SAML connection test.")
	})
	return c
}
func metadataFileError(path string, err error) error {
	reason := "Could not read file"
	if errors.Is(err, os.ErrNotExist) {
		reason = "No such file or directory"
	} else if errors.Is(err, os.ErrPermission) {
		reason = "Permission denied"
	}
	return &ExitError{2, "Error: Invalid value for 'METADATA': " + pythonQuote(output.CellText(path)) + ": " + reason}
}
func readSAMLMetadata(ctx context.Context, input io.Reader) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	ready := make(chan result, 1)
	go func() { data, err := io.ReadAll(io.LimitReader(input, (16<<20)+1)); ready <- result{data, err} }()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case got := <-ready:
		if got.err != nil {
			return nil, errors.New("could not read SAML metadata")
		}
		// The API accepts at most 1 MiB. This transport guard is above that limit
		// so normal server validation remains authoritative without unbounded input.
		if len(got.data) > 16<<20 {
			return nil, errors.New("SAML metadata exceeds the 16 MiB input limit")
		}
		return got.data, nil
	}
}
func newSAMLConfigure(d Dependencies) *cobra.Command {
	var org, email, given, family, groups, role string
	var jit, idp bool
	var o jsonOptions
	c := &cobra.Command{Use: "configure METADATA", Short: "Create or replace SAML configuration from IdP metadata."}
	c.Args = func(cmd *cobra.Command, args []string) error {
		if err := positionalArguments("METADATA")(cmd, args); err != nil {
			return err
		}
		if args[0] != "-" {
			file, err := os.Open(args[0])
			if err != nil {
				return metadataFileError(args[0], err)
			}
			if err = file.Close(); err != nil {
				return metadataFileError(args[0], err)
			}
		}
		return nil
	}
	orgFlag(c, &org)
	o.flags(c)
	f := c.Flags()
	for _, name := range []string{"email-attribute", "attr-email"} {
		f.StringVar(&email, name, "email", "IdP attribute carrying the member email.")
	}
	for _, name := range []string{"given-name-attribute", "attr-given-name"} {
		f.StringVar(&given, name, "", "IdP given-name attribute.")
	}
	for _, name := range []string{"family-name-attribute", "attr-family-name"} {
		f.StringVar(&family, name, "", "IdP family-name attribute.")
	}
	for _, name := range []string{"groups-attribute", "attr-groups"} {
		f.StringVar(&groups, name, "", "IdP groups attribute.")
	}
	f.StringVar(&role, "default-role", "user", "Role for new members: user, admin or billing_admin.")
	f.BoolVar(&jit, "jit-provisioning", true, "Allow SAML to provision new members.")
	f.Var(inverseBool{&jit}, "no-jit-provisioning", "Disable SAML provisioning.")
	f.Lookup("no-jit-provisioning").NoOptDefVal = "true"
	f.BoolVar(&idp, "allow-idp-initiated", false, "Allow sign-in initiated from the IdP.")
	f.Var(inverseBool{&idp}, "no-allow-idp-initiated", "Disable IdP-initiated sign-in.")
	f.Lookup("no-allow-idp-initiated").NoOptDefVal = "true"
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		role = strings.ToLower(role)
		if role != "user" && role != "admin" && role != "billing_admin" {
			return &ExitError{2, "Error: Invalid value for '--default-role': choose user, admin, or billing_admin."}
		}
		if done, err := o.prepare(cmd, d, samlFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		input := d.In
		if args[0] != "-" {
			file, e := os.Open(args[0])
			if e != nil {
				return metadataFileError(args[0], e)
			}
			defer func() { _ = file.Close() }()
			input = file
		}
		data, err := readSAMLMetadata(cmd.Context(), input)
		if err != nil {
			return err
		}
		query := url.Values{"attr_email": {email}, "default_role": {role}, "jit_provisioning": {strconv.FormatBool(jit)}, "allow_idp_initiated": {strconv.FormatBool(idp)}}
		for key, value := range map[string]string{"attr_given_name": given, "attr_family_name": family, "attr_groups": groups} {
			if value != "" {
				query.Set(key, value)
			}
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		record, err := api.ConfigureSAML(cmd.Context(), samlBase(id), data, query)
		if err != nil {
			return apiError(cfg, err, "configure SAML")
		}
		if f.Changed("json") {
			return o.record(cmd, d, record.Fields, samlFields)
		}
		return renderSAML(d, record.Fields)
	})
	return c
}
func newSAMLEnforcement(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "enforcement {on|off}", Short: "Enable or disable mandatory SAML sign-in (admin only).", Args: positionalArguments("{on|off}")}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		state := strings.ToLower(args[0])
		if state != "on" && state != "off" {
			return &ExitError{2, "Error: Invalid value for '{on|off}': " + pythonQuote(output.CellText(args[0])) + " is not one of 'on', 'off'."}
		}
		if done, err := o.prepare(cmd, d, samlFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		record, err := api.EnforceSAML(cmd.Context(), samlBase(id)+"/enforcement", state == "on")
		if err != nil {
			return apiError(cfg, err, "change SAML enforcement")
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record.Fields, samlFields)
		}
		label := "disabled"
		if record.Fields["is_enforced"] == true {
			label = "enabled"
		}
		_, err = fmt.Fprintf(d.Out, "SAML enforcement is now %s for %s.\n", label, output.CellText(id))
		return err
	})
	return c
}
func newSAMLRemove(d Dependencies) *cobra.Command {
	var org string
	var yes bool
	c := &cobra.Command{Use: "remove", Short: "Remove the organization's SAML configuration (admin only).", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if !yes {
			if err = confirmMutation(cmd.Context(), d, "removing SAML configuration requires confirmation", "Remove SAML configuration for "+output.CellText(id)+"?"); err != nil {
				return err
			}
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		if _, err = api.Raw(cmd.Context(), http.MethodDelete, samlBase(id), nil); err != nil {
			return apiError(cfg, err, "remove SAML configuration")
		}
		_, err = fmt.Fprintf(d.Out, "Removed SAML configuration for %s.\n", output.CellText(id))
		return err
	})
	return c
}
