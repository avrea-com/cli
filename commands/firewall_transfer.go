package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func newFirewallExport(d Dependencies) *cobra.Command {
	var org, repo, file string
	c := &cobra.Command{Use: "export", Short: "Export this scope's rules as portable JSON.", Args: cobra.NoArgs}
	firewallScopeFlags(c, &org, &repo, false)
	c.Flags().StringVar(&file, "file", "-", "Output file, or - for stdout. Existing files are not overwritten.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		repo, err := resolveFirewallRepository(cmd, d, cfg, api, id, repo)
		if err != nil {
			return err
		}
		policy, err := api.ExportFirewall(cmd.Context(), firewallBase(id, repo)+"/rules/export")
		if err != nil {
			return apiError(cfg, err, "export firewall rules")
		}
		if policy.Version != 1 || policy.Rules == nil {
			return errors.New("unexpected API response: invalid firewall export")
		}
		if file == "-" {
			return rawJSON(d, policy)
		}
		data, err := json.MarshalIndent(policy, "", "  ")
		if err != nil {
			return err
		}
		f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(append(data, '\n'))
		closeErr := f.Close()
		return errors.Join(writeErr, closeErr)
	})
	return c
}

func readFirewallPolicy(reader io.Reader) (client.FirewallPolicy, error) {
	// At most 1000 rules are accepted by the API. Bound file/stdin memory too.
	const maxBytes = 4 << 20
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return client.FirewallPolicy{}, err
	}
	if len(data) > maxBytes {
		return client.FirewallPolicy{}, errors.New("firewall policy exceeds 4 MiB")
	}
	var policy client.FirewallPolicy
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return policy, fmt.Errorf("invalid firewall policy JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return policy, errors.New("expected one firewall policy JSON document")
	}
	if policy.Version != 1 || policy.Rules == nil {
		return policy, errors.New("policy must have version 1 and a rules array")
	}
	return policy, nil
}

func firewallDefaultLabel(policy client.FirewallPolicy, repo string) string {
	for _, rule := range policy.Rules {
		if rule["is_default"] == true {
			return fieldText(rule, "action")
		}
	}
	if repo != "" {
		return "inherit organization default"
	}
	return "allow (platform default)"
}

func newFirewallImport(d Dependencies) *cobra.Command {
	var org, repo, file, mode string
	var dryRun, yes bool
	c := &cobra.Command{Use: "import", Short: "Append or replace this scope's rules from a portable JSON file.", Args: cobra.NoArgs}
	firewallScopeFlags(c, &org, &repo, false)
	c.Flags().StringVar(&file, "file", "", "Policy file, or - to read stdin.")
	_ = c.MarkFlagRequired("file")
	c.Flags().StringVar(&mode, "mode", "append", "Import mode: append or replace.")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "Validate and summarize changes without applying them.")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip replacement confirmation.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if err := validateFirewallChoice("mode", mode, []string{"append", "replace"}); err != nil {
			return err
		}
		reader := d.In
		if file != "-" {
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			reader = f
		}
		policy, err := readFirewallPolicy(reader)
		if err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		repo, err := resolveFirewallRepository(cmd, d, cfg, api, id, repo)
		if err != nil {
			return err
		}
		path := firewallBase(id, repo) + "/rule-imports"
		preview, err := api.PreviewFirewallImport(cmd.Context(), path+"/preview", mode, policy)
		if err != nil {
			return apiError(cfg, err, "preview firewall import")
		}
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(preview.Revision) || preview.Before.Version != 1 || preview.After.Version != 1 || preview.Before.Rules == nil || preview.After.Rules == nil || preview.Policy.Version != 1 || preview.Policy.Rules == nil {
			return errors.New("unexpected API response: incomplete firewall import preview")
		}
		if reflect.DeepEqual(preview.Before, preview.After) {
			_, err = fmt.Fprintln(d.Out, "No changes.")
			return err
		}
		scope := id
		if repo != "" {
			scope += " / " + repo
		}
		if _, err = fmt.Fprintf(d.Out, "%s: %s\n%d added, %d removed, %d skipped.\nDefault: %s -> %s\n", mode, output.CellText(scope), preview.Added, preview.Removed, preview.Skipped, output.CellText(firewallDefaultLabel(preview.Before, repo)), output.CellText(firewallDefaultLabel(preview.After, repo))); err != nil {
			return err
		}
		if preview.Added == 0 && preview.Removed == 0 {
			if _, err = fmt.Fprintln(d.Out, "Existing rule values or order will change."); err != nil {
				return err
			}
		}
		if dryRun {
			return nil
		}
		if mode == "replace" && !yes {
			if file == "-" || !d.IsInputTerminal() {
				return &ExitError{2, "Replacement requires --yes when input is non-interactive. Use --dry-run to review changes."}
			}
			if err := confirmMutation(cmd.Context(), d, "replacing firewall rules", "Replace rules in "+output.CellText(scope)+"?"); err != nil {
				return err
			}
		}
		// Use the normalized policy and revision from the same preview. A conflict
		// requires a new user-reviewed invocation, never an automatic re-preview.
		if err = api.ImportFirewall(cmd.Context(), path, mode, preview.Policy, preview.Revision); err != nil {
			return apiError(cfg, err, "import firewall rules; preview again if the policy changed")
		}
		_, err = fmt.Fprintln(d.Out, "Imported firewall rules.")
		return err
	})
	return c
}
