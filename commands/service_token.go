package commands

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var serviceTokenFields = []string{"id", "organization_id", "group_id", "created_by", "name", "created_at", "expires_at", "expiring_soon", "status", "rotated_at", "rotation_version", "grants_version", "previous_valid_until", "last_used_at", "revoked_at", "revoked_reason", "grants", "blocked_by"}
var serviceTokenSecretFields = append(slices.Clone(serviceTokenFields), "token")
var serviceTokenGroupPattern = lazyRegexp("^grp-[0-9a-f]{32}$")
var serviceTokenRulePattern = lazyRegexp("^efr-[0-9a-f]{32}$")

// Bounds the service-token API sets on lifetimes, overlaps, versions and
// grants.
const (
	serviceTokenMaxLifetime = 90 * 24 * 60 * 60
	serviceTokenMinOverlap  = 60 * 60
	serviceTokenMaxOverlap  = 7 * 24 * 60 * 60
	serviceTokenMaxVersion  = 1<<31 - 1
	serviceTokenMaxGrants   = 100
)

const serviceTokenGrantOrderHint = "grants[N] counts from 0 after repositories are resolved and duplicates removed: the --repo values first, then the --firewall-rule values in the order given, or --all-firewall-rules."
const serviceTokenStaleHint = "The token changed after this command read it. Run the command again to act on its current state."

// NewServiceToken exposes the management of an organization's group-owned
// service tokens to a signed-in organization admin.
func NewServiceToken(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "service-token", Aliases: []string{"service-tokens"}, Short: "Create and manage the organization's service tokens."}
	c.AddCommand(newServiceTokenList(d), newServiceTokenView(d), newServiceTokenCreate(d), newServiceTokenSetAccess(d), newServiceTokenRotate(d), newServiceTokenRevoke(d), newServiceTokenRevokeAll(d))
	return c
}

// serviceTokenContext loads a signed-in person's credentials and resolves the
// organization. The API closes these routes to scoped and service tokens, so
// they are refused before any request.
func serviceTokenContext(ctx context.Context, d Dependencies, org string) (config.Config, *client.Client, string, error) {
	cfg, _, api, err := d.load()
	if err != nil {
		return cfg, api, "", err
	}
	if err = authenticated(cfg); err != nil {
		return cfg, api, "", err
	}
	if err = requirePerson(cfg, "Managing service tokens", "manage service tokens"); err != nil {
		return cfg, api, "", err
	}
	id, err := resolveOrg(ctx, d, cfg, api, org)
	return cfg, api, id, err
}

func serviceTokenListHint(org string) string {
	return "Run \x60avr service-token list --org " + shellQuotePython(org) + "\x60 to see the organization's service tokens."
}

func serviceTokensPath(org string) string { return "/orgs/" + url.PathEscape(org) + "/service-tokens" }

// serviceTokenRefusal names the hints a refused request gets beyond apiError's.
type serviceTokenRefusal struct {
	action    string // completes "You don't have access to ..."
	notFound  string // 404
	stale     string // 409 without Retry-After
	unchanged string // 503 with the API's reason: nothing was written
	unknown   string // another 5xx: the write may have happened
}

func serviceTokenError(cfg config.Config, err error, r serviceTokenRefusal) error {
	var he *client.HTTPError
	if !errors.As(err, &he) {
		return err
	}
	if he.Status == 403 && he.Reauthentication != "" {
		return serviceTokenReauthentication(cfg, he, r.action)
	}
	result := apiError(cfg, he, r.action)
	hint := ""
	switch {
	case he.Status == 401:
	case (he.Status == 403 || he.Status == 422) && strings.Contains(pythonValue(he.Payload), "grants"):
		hint = serviceTokenGrantOrderHint
	case he.Status == 404:
		hint = r.notFound
	case he.Status == 409 && he.RetryAfter != "":
		hint = tokenRetryAfter(he)
	case he.Status == 409:
		hint = r.stale
	case he.Status == 503 && he.Detail != "" && r.unchanged != "":
		hint = r.unchanged + " " + tokenRetryAfter(he)
	case he.Status >= 500:
		hint = r.unknown
	}
	return insertTokenHint(result, hint)
}

// serviceTokenReauthentication answers the API's X-Reauthentication-Required
// refusal: the API key is too old for a change that outlives the session.
func serviceTokenReauthentication(cfg config.Config, he *client.HTTPError, action string) error {
	detail := ""
	if he.Detail != "" {
		detail = ": " + output.CellText(he.Detail)
	}
	message := "Error: Avrea needs a recent sign-in to " + action + " (HTTP 403)" + detail +
		"\n  Hint: creating or rotating a service token, and widening its access, need a recent sign-in. Run \x60avr auth login\x60 again, then retry. If AVR_TOKEN is set, unset it first: it takes precedence over the new sign-in."
	if cfg.Host != config.DefaultHost {
		message += "\n  API: " + cfg.Host + he.Path
	}
	return &ExitError{4, message}
}

// serviceTokenGrantOptions selects what a service token reads.
type serviceTokenGrantOptions struct {
	repos, rules []string
	allRules     bool
}

func (g *serviceTokenGrantOptions) flags(c *cobra.Command) {
	c.Flags().StringArrayVar(&g.repos, "repo", nil, "Repository read grant (owner/name or rep-...), repeatable.")
	c.Flags().StringArrayVar(&g.rules, "firewall-rule", nil, "Egress firewall rule read grant (efr-...), repeatable.")
	c.Flags().BoolVar(&g.allRules, "all-firewall-rules", false, "Read every egress firewall rule, including later ones.")
}

// validate checks the selection before any request and returns the repository
// names and rule IDs without their optional :read suffix.
func (g serviceTokenGrantOptions) validate() ([]string, []string, error) {
	if len(g.repos) == 0 && len(g.rules) == 0 && !g.allRules {
		return nil, nil, &ExitError{2, "Error: Pass at least one of --repo, --firewall-rule or --all-firewall-rules: a service token needs at least one grant."}
	}
	if g.allRules && len(g.rules) > 0 {
		return nil, nil, &ExitError{2, "Error: --all-firewall-rules already grants every firewall rule; drop --firewall-rule."}
	}
	repos := make([]string, len(g.repos))
	rules := make([]string, len(g.rules))
	var err error
	for i, spec := range g.repos {
		if repos[i], err = stripTokenLevel(spec, "repository", "--repo", "read"); err != nil {
			return nil, nil, err
		}
	}
	for i, spec := range g.rules {
		if rules[i], err = stripTokenLevel(spec, "firewall rule", "--firewall-rule", "read"); err != nil {
			return nil, nil, err
		}
		if !serviceTokenRulePattern().MatchString(rules[i]) {
			return nil, nil, &ExitError{2, fmt.Sprintf("Error: Invalid --firewall-rule value %s: expected a rule ID, efr- followed by 32 lowercase hexadecimal characters.", pythonQuote(output.CellText(spec)))}
		}
	}
	count := len(repos) + len(rules)
	if g.allRules {
		count++
	}
	if count > serviceTokenMaxGrants {
		return nil, nil, &ExitError{2, fmt.Sprintf("Error: A service token takes at most %d grants (got %d).", serviceTokenMaxGrants, count)}
	}
	return repos, rules, nil
}

// build resolves the repositories and returns the grants in the order the
// grant-order hint describes, without repeats.
func (g serviceTokenGrantOptions) build(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org string, repos, rules []string) ([]map[string]any, error) {
	grants, err := repositoryReadGrants(ctx, d, cfg, api, org, repos)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, rule := range rules {
		if !seen[rule] {
			grants = append(grants, map[string]any{"resource_type": "egress_firewall_rule", "resource_id": rule, "access_level": "read"})
			seen[rule] = true
		}
	}
	if g.allRules {
		// An explicit null resource_id grants every rule of the organization.
		grants = append(grants, map[string]any{"resource_type": "egress_firewall_rule", "resource_id": nil, "access_level": "read"})
	}
	return grants, nil
}

// serviceTokenLifetime chooses a new secret's lifetime.
type serviceTokenLifetime struct {
	ttl   string
	never bool
}

func (l *serviceTokenLifetime) flags(c *cobra.Command) {
	c.Flags().StringVar(&l.ttl, "ttl", "", "Lifetime: 30m, 8h, 7d or seconds (60 seconds-90 days; default 30 days).")
	c.Flags().BoolVar(&l.never, "never-expires", false, "The secret never expires; it works until revoked or rotated.")
}

// apply validates the choice and writes it into a create or rotate body.
func (l serviceTokenLifetime) apply(cmd *cobra.Command, body map[string]any) error {
	ttl := cmd.Flags().Changed("ttl")
	if ttl && l.never {
		return &ExitError{2, "Error: --ttl and --never-expires are mutually exclusive."}
	}
	if ttl {
		seconds, err := tokenDuration("--ttl", l.ttl, 60, serviceTokenMaxLifetime)
		if err != nil {
			return err
		}
		body["ttl_seconds"] = seconds
	}
	// Rotation requires the field; the API's default otherwise is 30 days.
	body["never_expires"] = l.never
	return nil
}

// serviceTokenSecretFieldsChosen checks --json before any request: the
// secret is returned once, so the selection must keep it.
func serviceTokenSecretFieldsChosen(cmd *cobra.Command, o jsonOptions) ([]string, error) {
	fields, err := output.Fields(o.fields, serviceTokenSecretFields)
	if err != nil {
		return nil, err
	}
	if cmd.Flags().Changed("json") && !slices.Contains(fields, "token") {
		return nil, &ExitError{2, "Error: --json must include the token field (or \"*\"): the secret is returned only once."}
	}
	return fields, nil
}

// readServiceTokenVersion reads the token and the version a write must echo.
func readServiceTokenVersion(ctx context.Context, cfg config.Config, api *client.Client, org, path, field string) (map[string]any, int64, error) {
	current, err := api.AccessToken(ctx, path)
	if err != nil {
		return nil, 0, serviceTokenError(cfg, err, serviceTokenRefusal{action: "read the service token", notFound: serviceTokenListHint(org)})
	}
	version, ok := devicePositiveInt(current[field], serviceTokenMaxVersion)
	if !ok {
		return nil, 0, errors.New("unexpected API response: " + field + " is missing or invalid")
	}
	return current, version, nil
}

// serviceTokenWriteStale is the 409 hint for a write that echoed a version:
// only when the read showed a live, unblocked token can a conflict mean that
// the token changed since; otherwise the API's reason already says why.
func serviceTokenWriteStale(current map[string]any, hint string) string {
	if fieldText(current, "status") == "live" && current["blocked_by"] == nil {
		return hint
	}
	return ""
}

func newServiceTokenList(d Dependencies) *cobra.Command {
	var org, group, status string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List the organization's service tokens, soonest expiry first.", Args: cobra.NoArgs}
	c.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	c.Flags().StringVar(&group, "group", "", "Only the tokens this group owns (grp-...).")
	c.Flags().StringVar(&status, "status", "", "Only tokens with this status: live, expired or revoked.")
	c.Flags().IntVarP(&limit, "limit", "L", 50, "Maximum tokens (1–1000).")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		base := url.Values{}
		if cmd.Flags().Changed("group") {
			if !serviceTokenGroupPattern().MatchString(group) {
				return &ExitError{2, "Error: Invalid value for '--group': expected a group ID, grp- followed by 32 lowercase hexadecimal characters."}
			}
			base.Set("group_id", "eq."+group)
		}
		if cmd.Flags().Changed("status") {
			if err := validateFirewallChoice("status", status, []string{"live", "expired", "revoked"}); err != nil {
				return err
			}
			base.Set("status", "eq."+status)
		}
		if done, err := o.prepare(cmd, d, serviceTokenFields); done || err != nil {
			return err
		}
		cfg, api, id, err := serviceTokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		rows := []map[string]any{}
		cursor := ""
		for {
			query := url.Values{"limit": {strconv.Itoa(min(limit-len(rows), 200))}}
			for key, values := range base {
				query[key] = values
			}
			if cursor != "" {
				query.Set("cursor", cursor)
			}
			page, e := api.AccessTokens(cmd.Context(), serviceTokensPath(id), query)
			if e != nil {
				return serviceTokenError(cfg, e, serviceTokenRefusal{action: "list service tokens"})
			}
			rows = append(rows, rawRecords(page)...)
			cursor = page.NextCursor
			if cursor == "" || len(page.Items) == 0 || len(rows) >= limit {
				break
			}
		}
		more := cursor != "" || len(rows) > limit
		if len(rows) > limit {
			rows = rows[:limit]
		}
		if cmd.Flags().Changed("json") {
			err = o.list(cmd, d, rows, serviceTokenFields)
		} else {
			err = renderServiceTokenList(d, rows)
		}
		if err != nil {
			return err
		}
		if more {
			advice := "Raise --limit to see the rest."
			if limit == 1000 {
				advice = "Narrow the list with --group or --status to see the rest."
			}
			_, err = fmt.Fprintf(d.Err, "\nShowing the first %d service tokens. %s\n", len(rows), advice)
		}
		return err
	})
	return c
}

func newServiceTokenView(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "view TOKEN_ID", Short: "Show a service token's details, versions and grants.", Args: tokenIDArgs}
	c.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, serviceTokenFields); done || err != nil {
			return err
		}
		cfg, api, id, err := serviceTokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.AccessToken(cmd.Context(), serviceTokensPath(id)+"/"+args[0])
		if err != nil {
			return serviceTokenError(cfg, err, serviceTokenRefusal{action: "fetch the service token", notFound: serviceTokenListHint(id)})
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record, serviceTokenFields)
		}
		return renderServiceToken(d, record)
	})
	return c
}

func newServiceTokenCreate(d Dependencies) *cobra.Command {
	var org, name, group string
	var g serviceTokenGrantOptions
	var l serviceTokenLifetime
	var o jsonOptions
	c := &cobra.Command{Use: "create", Short: "Create a service token and print its secret once.", Args: cobra.NoArgs}
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringVar(&name, "name", "", "Token name (1–100 characters).")
	f.StringVar(&group, "group", "", "The group that owns the token (grp-...).")
	g.flags(c)
	l.flags(c)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if !f.Changed("name") {
			return &ExitError{2, "Error: Missing option '--name'."}
		}
		if !f.Changed("group") {
			return &ExitError{2, "Error: Missing option '--group'."}
		}
		if !serviceTokenGroupPattern().MatchString(group) {
			return &ExitError{2, "Error: Invalid value for '--group': expected a group ID, grp- followed by 32 lowercase hexadecimal characters."}
		}
		body := map[string]any{"name": name, "group_id": group}
		if err := l.apply(cmd, body); err != nil {
			return err
		}
		repos, rules, err := g.validate()
		if err != nil {
			return err
		}
		if done, err := o.prepare(cmd, d, serviceTokenSecretFields); done || err != nil {
			return err
		}
		fields, err := serviceTokenSecretFieldsChosen(cmd, o)
		if err != nil {
			return err
		}
		cfg, api, id, err := serviceTokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		grants, err := g.build(cmd.Context(), d, cfg, api, id, repos, rules)
		if err != nil {
			return err
		}
		body["grants"] = grants
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		unknown := "A service token may have been created; check \x60avr service-token list --org " + shellQuotePython(id) + "\x60 before retrying."
		minted, err := api.MintAccessToken(cmd.Context(), serviceTokensPath(id), body)
		if err != nil {
			var he *client.HTTPError
			if errors.As(err, &he) {
				return serviceTokenError(cfg, he, serviceTokenRefusal{
					action:    "create a service token",
					notFound:  "Check the organization and API host. If service tokens are unavailable for this organization, contact support@avrea.com.",
					unchanged: "No service token was created.",
					unknown:   unknown,
				})
			}
			return tokenCreationFailure("The creation response was not received. "+unknown, err)
		}
		record, _ := minted.(map[string]any)
		credential, ok := record["token"].(string)
		if !ok || credential == "" {
			return &ExitError{1, "Error: The API reported success without a secret, so a service token may exist that nothing can use." + serviceTokenDiscardHint(record, id)}
		}
		return deliverOneTimeCredential(cmd.Context(), d, oneTimeCredential{
			record: record, credential: credential, hasJSON: f.Changed("json"), fields: fields, expression: o.expression,
			intro:    "The secret is shown only once. Store it now, for example as a CI secret, and set it as AVR_TOKEN where the token is used:",
			exports:  [][2]string{{"AVR_TOKEN", credential}},
			summary:  func() error { return renderServiceToken(d, record) },
			outcome:  "The service token was created",
			recovery: serviceTokenDiscardHint(record, id),
		})
	})
	return c
}

func serviceTokenDiscardHint(record map[string]any, org string) string {
	if id, ok := record["id"].(string); ok && tokenIDPattern().MatchString(id) {
		return " To discard it, run \x60avr service-token revoke " + id + " --org " + shellQuotePython(org) + "\x60."
	}
	return " Check \x60avr service-token list --org " + shellQuotePython(org) + "\x60 before retrying."
}

func newServiceTokenSetAccess(d Dependencies) *cobra.Command {
	var org string
	var g serviceTokenGrantOptions
	c := &cobra.Command{Use: "set-access TOKEN_ID", Short: "Replace a service token's whole access selection.", Args: tokenIDArgs}
	c.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	g.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		repos, rules, err := g.validate()
		if err != nil {
			return err
		}
		cfg, api, id, err := serviceTokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		grants, err := g.build(cmd.Context(), d, cfg, api, id, repos, rules)
		if err != nil {
			return err
		}
		path := serviceTokensPath(id) + "/" + args[0]
		current, version, err := readServiceTokenVersion(cmd.Context(), cfg, api, id, path, "grants_version")
		if err != nil {
			return err
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		record, err := api.Do(cmd.Context(), http.MethodPut, path+"/grants", map[string]any{"grants": grants, "expected_grants_version": version})
		if err != nil {
			return serviceTokenError(cfg, err, serviceTokenRefusal{
				action:    "replace the service token's access",
				notFound:  serviceTokenListHint(id),
				stale:     serviceTokenWriteStale(current, serviceTokenStaleHint),
				unchanged: "The token's access was not changed.",
				unknown:   "The access may have been replaced; check \x60avr service-token view " + args[0] + " --org " + shellQuotePython(id) + "\x60 before retrying.",
			})
		}
		if _, err = fmt.Fprintf(d.Out, "Replaced the access of service token %s.\n\n", args[0]); err != nil {
			return err
		}
		return renderServiceToken(d, record)
	})
	return c
}

func newServiceTokenRotate(d Dependencies) *cobra.Command {
	var org, overlap string
	var l serviceTokenLifetime
	var o jsonOptions
	c := &cobra.Command{Use: "rotate TOKEN_ID", Short: "Issue a service token a new secret and print it once.", Args: tokenIDArgs}
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	l.flags(c)
	f.StringVar(&overlap, "overlap", "", "How long the old secret keeps working: 1h to 7d (default 24h).")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		if err := l.apply(cmd, body); err != nil {
			return err
		}
		if f.Changed("overlap") {
			seconds, err := tokenDuration("--overlap", overlap, serviceTokenMinOverlap, serviceTokenMaxOverlap)
			if err != nil {
				return err
			}
			body["overlap_seconds"] = seconds
		}
		if done, err := o.prepare(cmd, d, serviceTokenSecretFields); done || err != nil {
			return err
		}
		fields, err := serviceTokenSecretFieldsChosen(cmd, o)
		if err != nil {
			return err
		}
		cfg, api, id, err := serviceTokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		path := serviceTokensPath(id) + "/" + args[0]
		current, version, err := readServiceTokenVersion(cmd.Context(), cfg, api, id, path, "rotation_version")
		if err != nil {
			return err
		}
		body["expected_rotation_version"] = version
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		retire := "Rotating again retires the previous secret at once."
		view := "\x60avr service-token view " + args[0] + " --org " + shellQuotePython(id) + "\x60"
		unknown := "The token may have been rotated; check its rotation version with " + view + " before retrying. " + retire
		stale := "The token changed after this command read it. Check " + view + " before retrying: if it was rotated, rotating again retires the previous secret at once."
		minted, err := api.MintAccessToken(cmd.Context(), path+"/rotate", body)
		if err != nil {
			var he *client.HTTPError
			if errors.As(err, &he) {
				return serviceTokenError(cfg, he, serviceTokenRefusal{
					action:    "rotate the service token",
					notFound:  serviceTokenListHint(id),
					stale:     serviceTokenWriteStale(current, stale),
					unchanged: "The token was not rotated.",
					unknown:   unknown,
				})
			}
			return tokenCreationFailure("The rotation response was not received. "+unknown, err)
		}
		record, _ := minted.(map[string]any)
		credential, ok := record["token"].(string)
		if !ok || credential == "" {
			return &ExitError{1, "Error: The API reported success without a secret. " + unknown}
		}
		return deliverOneTimeCredential(cmd.Context(), d, oneTimeCredential{
			record: record, credential: credential, hasJSON: f.Changed("json"), fields: fields, expression: o.expression,
			intro:   "The new secret is shown only once. Store it now and set it as AVR_TOKEN wherever the token is used:",
			exports: [][2]string{{"AVR_TOKEN", credential}},
			summary: func() error {
				previous := "The previous secret no longer works."
				if until := fieldText(record, "previous_valid_until"); until != "" {
					previous = "The previous secret keeps working until " + formatTimestamp(until) + "; switch everything that uses it before then."
				}
				if _, err := fmt.Fprintln(d.Out, previous+"\n"); err != nil {
					return err
				}
				return renderServiceToken(d, record)
			},
			outcome:  "The service token was rotated",
			recovery: " If the new secret was not saved, check " + view + " and rotate again. " + retire,
		})
	})
	return c
}

func newServiceTokenRevoke(d Dependencies) *cobra.Command {
	var org string
	var yes bool
	c := &cobra.Command{Use: "revoke TOKEN_ID", Short: "Revoke a service token.", Args: tokenIDArgs}
	c.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, id, err := serviceTokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if !yes {
			if err = confirmMutation(cmd.Context(), d, "revoking a service token", "Revoke service token "+args[0]+"? Everything using it stops working, including a secret still in its rotation overlap."); err != nil {
				return err
			}
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		if _, err = api.Do(cmd.Context(), http.MethodDelete, serviceTokensPath(id)+"/"+args[0], nil); err != nil {
			return serviceTokenError(cfg, err, serviceTokenRefusal{action: "revoke the service token", notFound: serviceTokenListHint(id)})
		}
		_, err = fmt.Fprintf(d.Out, "Service token %s revoked.\n", args[0])
		return err
	})
	return c
}

func newServiceTokenRevokeAll(d Dependencies) *cobra.Command {
	var org string
	var yes bool
	c := &cobra.Command{Use: "revoke-all", Short: "Revoke every service token of the organization.", Args: cobra.NoArgs}
	c.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, api, id, err := serviceTokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		// Only the prompt looks the slug up; --yes uses what is already known.
		slug := knownOrganizationSlug(cmd.Context(), cfg, id)
		if !yes {
			slug = orgSlug(cmd.Context(), cfg, api, id)
		}
		label := id
		if slug != "" && slug != id {
			label = slug + " (" + id + ")"
		}
		label = output.CellText(label)
		if !yes {
			if err = confirmMutation(cmd.Context(), d, "revoking every service token", "Revoke every service token of organization "+label+"? Everything using them stops working."); err != nil {
				return err
			}
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		result, err := api.Do(cmd.Context(), http.MethodPost, serviceTokensPath(id)+"/revoke-all", nil)
		if err != nil {
			return serviceTokenError(cfg, err, serviceTokenRefusal{action: "revoke the organization's service tokens"})
		}
		revoked, _ := result["revoked"].([]any)
		noun := "service tokens"
		if len(revoked) == 1 {
			noun = "service token"
		}
		if _, err = fmt.Fprintf(d.Out, "Revoked %d %s of organization %s.\n", len(revoked), noun, label); err != nil {
			return err
		}
		for _, token := range revoked {
			if _, err = fmt.Fprintln(d.Out, "  "+output.CellText(fmt.Sprint(token))); err != nil {
				return err
			}
		}
		return nil
	})
	return c
}

// serviceTokenState is a token's status, or why a live token's requests are
// refused all the same.
func serviceTokenState(row map[string]any) string {
	if blocked := fieldText(row, "blocked_by"); blocked != "" {
		return "blocked: " + strings.ReplaceAll(blocked, "_", " ")
	}
	return fieldText(row, "status")
}

func serviceTokenExpiry(row map[string]any) string {
	value := fieldText(row, "expires_at")
	if value == "" {
		return "never"
	}
	value = formatTimestamp(value)
	if row["expiring_soon"] == true {
		value += " (expiring soon)"
	}
	return value
}

func renderServiceTokenList(d Dependencies, rows []map[string]any) error {
	piped := !d.IsTerminal()
	if !piped && len(rows) == 0 {
		_, err := fmt.Fprintln(d.Out, "No data found.")
		return err
	}
	var cells [][]string
	if piped {
		headers := []string{"id", "name", "group_id", "status", "blocked_by", "expires_at", "expiring_soon", "last_used_at", "grants"}
		for _, row := range rows {
			values := make([]string, len(headers))
			for i, key := range headers[:len(headers)-1] {
				values[i] = fieldText(row, key)
			}
			values[len(headers)-1] = tokenGrantsSummary(row)
			cells = append(cells, values)
		}
		return output.Table(d.Out, headers, cells, true)
	}
	for _, row := range rows {
		used := "never"
		if value := fieldText(row, "last_used_at"); value != "" {
			used = relativeAge(value, d.Now())
		}
		cells = append(cells, []string{fieldText(row, "id"), fieldText(row, "name"), fieldText(row, "group_id"), serviceTokenState(row), serviceTokenExpiry(row), used, tokenGrantsSummary(row)})
	}
	return output.Grid(d.Out, []string{"Token ID", "Name", "Group", "Status", "Expires", "Last used", "Grants"}, cells)
}

func renderServiceToken(d Dependencies, row map[string]any) error {
	status := serviceTokenState(row)
	if value := fieldText(row, "revoked_at"); value != "" {
		status = "revoked " + formatTimestamp(value)
		if reason := fieldText(row, "revoked_reason"); reason != "" {
			status += " (" + reason + ")"
		}
	}
	optional := func(key, absent string) string {
		if value := fieldText(row, key); value != "" {
			return formatTimestamp(value)
		}
		return absent
	}
	pairs := [][2]string{
		{"Token ID", fieldText(row, "id")}, {"Name", fieldText(row, "name")},
		{"Organization", fieldText(row, "organization_id")}, {"Group", fieldText(row, "group_id")},
		{"Status", status}, {"Created", optional("created_at", "unknown")},
		{"Created by", cmp.Or(fieldText(row, "created_by"), "-")}, {"Expires", serviceTokenExpiry(row)},
		{"Rotated", optional("rotated_at", "unknown")}, {"Old secret until", optional("previous_valid_until", "-")},
		{"Last used", optional("last_used_at", "never")},
		{"Versions", "rotation " + cmp.Or(pythonValue(row["rotation_version"]), "-") + ", grants " + cmp.Or(pythonValue(row["grants_version"]), "-")},
	}
	if err := output.KeyValues(d.Out, pairs); err != nil {
		return err
	}
	grants := objectList(row["grants"])
	if len(grants) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(d.Out, "\nGrants:"); err != nil {
		return err
	}
	var cells [][]string
	for _, grant := range grants {
		kind := fieldText(grant, "resource_type")
		resource := fieldText(grant, "resource_id")
		if kind == "egress_firewall_rule" {
			kind = "firewall rule"
			if resource == "" {
				resource = "every firewall rule"
			}
		}
		cells = append(cells, []string{kind, resource, fieldText(grant, "access_level")})
	}
	return output.Grid(d.Out, []string{"Type", "Resource", "Level"}, cells)
}
