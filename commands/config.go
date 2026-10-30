package commands

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

// NewConfig exposes the shared host-scoped CLI configuration.
func NewConfig(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	status := run(func(cmd *cobra.Command, _ []string) error { return printConfigStatus(cmd.Context(), d) })
	group := &cobra.Command{Use: "config", Short: "View and manage CLI configuration.", Args: cobra.NoArgs, RunE: status}
	group.AddCommand(&cobra.Command{Use: "list", Short: "Show the active CLI configuration.", Args: cobra.NoArgs, RunE: status})
	keyArgs := func(count int) cobra.PositionalArgs {
		return func(cmd *cobra.Command, args []string) error {
			if len(args) < count {
				key := "{org}"
				if len(args) > 0 {
					key = "VALUE"
				}
				return &ExitError{2, "Error: Missing argument '" + key + "'."}
			}
			if len(args) > count {
				return &ExitError{2, "Error: Got unexpected extra argument (" + output.CellText(strings.Join(args[count:], " ")) + ")"}
			}
			if args[0] != "org" {
				return &ExitError{2, "Error: Invalid value for '{org}': " + pythonQuote(output.CellText(args[0])) + " is not 'org'."}
			}
			return nil
		}
	}
	get := &cobra.Command{Use: "get {org}", Short: "Print the value of a configuration key.", Args: keyArgs(1)}
	get.RunE = run(func(_ *cobra.Command, _ []string) error {
		cfg, _, _, err := d.load()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(d.Out, output.CellText(cfg.Org))
		return err
	})
	set := &cobra.Command{Use: "set {org} VALUE", Short: "Set a CLI configuration value.", Args: keyArgs(2)}
	set.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, store, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		if err = refuseScopedStoredOrg(cfg, "set"); err != nil {
			return err
		}
		page, err := api.Organizations(cmd.Context())
		if err != nil {
			return apiError(cfg, err, "fetch organizations")
		}
		rows := rawRecords(page)
		var selected map[string]any
		// IDs take precedence over slugs, including when another org's slug
		// happens to spell the selected org ID.
		for _, field := range []string{"organization_id", "slug"} {
			for _, row := range rows {
				if fieldText(row, field) == args[1] {
					selected = row
					break
				}
			}
			if selected != nil {
				break
			}
		}
		if selected == nil {
			for _, row := range rows {
				slug := fieldText(row, "slug")
				if slug != "" && strings.EqualFold(slug, args[1]) {
					selected = row
					break
				}
			}
		}
		if selected == nil {
			var message strings.Builder
			fmt.Fprintf(&message, "Error: Organization '%s' not found or you don't have access.\nAvailable organizations:\n", output.CellText(args[1]))
			for _, row := range rows {
				label := fieldText(row, "slug")
				if label == "" {
					label = fieldText(row, "name")
				}
				fmt.Fprintf(&message, "  %s (%s)\n", output.CellText(fieldText(row, "organization_id")), output.CellText(label))
			}
			message.WriteString("Aborted!")
			return &ExitError{1, message.String()}
		}
		id := fieldText(selected, "organization_id")
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		if err = store.Set(cfg.Host, "default_org", id); err != nil {
			return err
		}
		_, err = fmt.Fprintf(d.Out, "Default organization set to: %s (%s)\n", output.CellText(fieldText(selected, "name")), output.CellText(id))
		return err
	})
	unset := &cobra.Command{Use: "unset {org}", Short: "Remove a configuration override.", Args: keyArgs(1)}
	unset.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, store, _, err := d.load()
		if err != nil {
			return err
		}
		if err = refuseScopedStoredOrg(cfg, "unset"); err != nil {
			return err
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		changed, err := store.ClearDefaultOrg(cfg.Host)
		if err != nil {
			return err
		}
		message := "No default organization was set."
		if changed {
			message = "Cleared default organization."
		}
		_, err = fmt.Fprintln(d.Out, message)
		return err
	})
	group.AddCommand(get, set, unset)
	return group
}
func refuseScopedStoredOrg(cfg config.Config, verb string) error {
	if !config.IsScoped(cfg.Token) {
		return nil
	}
	return &ExitError{1, "Error: A scoped token is bound to one organization and has no stored default to " + verb + ".\nPass that organization's ID with --org org-... or set AVR_ORG to it.\nAborted!"}
}
func printConfigStatus(ctx context.Context, d Dependencies) error {
	cfg, store, api, err := d.load()
	if err != nil {
		return err
	}
	source := func(label string) string { return d.paint("(from "+label+")", "2") }
	u, err := url.Parse(cfg.Host)
	if err != nil {
		return err
	}
	host := u.Hostname()
	if host == "" {
		host = cfg.Host
	}
	hostSource := "default"
	if d.Getenv("AVR_HOST") != "" {
		hostSource = "AVR_HOST"
	} else if store.Read().DefaultHost != "" {
		hostSource = "hosts.json"
	}
	if _, err = fmt.Fprintln(d.Out, d.paint(output.CellText(host), "1")+" "+source(hostSource)); err != nil {
		return err
	}
	var authLine string
	if cfg.Token == "" {
		authLine = "  " + d.paint("✗", "31") + " not authenticated (run: avr auth login)"
	} else {
		tokenSource := "hosts.json"
		if d.Getenv("AVR_TOKEN") != "" {
			tokenSource = "AVR_TOKEN"
		}
		if config.IsScoped(cfg.Token) {
			authLine = "  - scoped token " + source(tokenSource) + ", not checked against the API"
		} else {
			me, e := api.Get(ctx, "/users/me", nil)
			if e != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				var he *client.HTTPError
				var te *client.TransportError
				unavailable := errors.As(e, &te) && (te.Reason == "ConnectError" || te.Reason == "ConnectTimeout" || te.Reason == "ReadTimeout")
				if !errors.As(e, &he) && !unavailable {
					return e
				}
				me = nil
			}
			if inner, ok := me["data"].(map[string]any); ok && len(inner) > 0 {
				me = inner
			}
			switch {
			case fieldText(me, "email") != "":
				authLine = "  " + d.paint("✓", "32") + " authenticated as " + output.CellText(fieldText(me, "email")) + " " + source(tokenSource)
			case len(me) > 0:
				authLine = "  " + d.paint("✓", "32") + " authenticated " + source(tokenSource)
			default:
				authLine = "  " + d.paint("!", "33") + " token set but /users/me failed " + source(tokenSource)
			}
		}
	}
	if _, err = fmt.Fprintln(d.Out, authLine); err != nil {
		return err
	}
	org := d.paint("(not set)", "2")
	if cfg.Org != "" {
		slug := cfg.Org
		if cfg.Token != "" {
			slug = orgSlug(ctx, cfg, api, cfg.Org)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		org = d.paint(output.CellText(slug), "1")
		if slug != cfg.Org {
			org += " (" + output.CellText(cfg.Org) + ")"
		}
		orgSource := "hosts.json"
		if d.Getenv("AVR_ORG") != "" {
			orgSource = "AVR_ORG"
		}
		org += " " + source(orgSource)
	}
	repo := d.paint("(no git remote; pass --repo explicitly)", "2")
	if cfg.Repo != "" {
		repo = d.paint(output.CellText(cfg.Repo), "1") + " " + source("AVR_REPO")
	} else {
		remote, _ := d.GitRemote(ctx)
		if err = ctx.Err(); err != nil {
			return err
		}
		name, _ := parseGitRemote(remote)
		if name != "" {
			repo = d.paint(output.CellText(name), "1") + " " + source("git remote")
		}
	}
	if _, err = fmt.Fprintln(d.Out, "  - Active org:   "+org); err != nil {
		return err
	}
	_, err = fmt.Fprintln(d.Out, "  - Default repo: "+repo)
	return err
}
