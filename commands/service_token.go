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
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var serviceTokenFields = []string{"id", "organization_id", "group_id", "created_by", "name", "created_at", "expires_at", "expiring_soon", "status", "rotated_at", "rotation_version", "grants_version", "previous_valid_until", "last_used_at", "revoked_at", "revoked_reason", "grants", "vm_cap", "vm_count", "blocked_by"}
var serviceTokenSecretFields = append(slices.Clone(serviceTokenFields), "token")
var serviceTokenGroupPattern = lazyRegexp("^grp-[0-9a-f]{32}$")
var serviceTokenRulePattern = lazyRegexp("^efr-[0-9a-f]{32}$")
var serviceTokenVMPattern = lazyRegexp("^cvm-[0-9a-f]{32}$")

// serviceTokenLevels are the levels a grant takes, lowest first. Only a VM
// takes write; admin on a VM follows its creator and is never granted.
var serviceTokenLevels = []string{"read", "write"}

// Bounds the service-token API sets on lifetimes, overlaps, versions, grants
// and the VM cap.
const (
	serviceTokenMaxLifetime = 90 * 24 * 60 * 60
	serviceTokenMinOverlap  = 60 * 60
	serviceTokenMaxOverlap  = 7 * 24 * 60 * 60
	serviceTokenMaxVersion  = 1<<31 - 1
	serviceTokenMaxGrants   = 100
	serviceTokenMaxVMCap    = 1<<31 - 1
)

const serviceTokenGrantOrderHint = "grants[N] counts from 0 after repositories are resolved and duplicates removed: the --repo values first, then the --firewall-rule values in the order given or --all-firewall-rules, then the --vm values in the order given and --all-vms last."

// NewServiceToken exposes the management of an organization's group-owned
// service tokens to a signed-in organization admin.
func NewServiceToken(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "service-token", Aliases: []string{"service-tokens"}, Short: "Create and manage the organization's service tokens."}
	c.AddCommand(newServiceTokenList(d), newServiceTokenView(d), newServiceTokenCreate(d), newServiceTokenSetAccess(d), newServiceTokenRotate(d), newServiceTokenRevoke(d), newServiceTokenRevokeAll(d))
	return c
}

// personContext loads a signed-in person's credentials and resolves the
// organization. Routes the API closes to scoped and service tokens are refused
// before any request; subject and action complete requirePerson's message.
func personContext(ctx context.Context, d Dependencies, org, subject, action string) (config.Config, *client.Client, string, error) {
	return orgContext(ctx, d, org, func(cfg config.Config) error { return requirePerson(cfg, subject, action) })
}

func serviceTokenContext(ctx context.Context, d Dependencies, org string) (config.Config, *client.Client, string, error) {
	return personContext(ctx, d, org, "Managing service tokens", "manage service tokens")
}

func serviceTokenListHint(org string) string {
	return "Run \x60avr service-token list --org " + shellQuotePython(org) + "\x60 to see the organization's service tokens."
}

func serviceTokensPath(org string) string { return "/orgs/" + url.PathEscape(org) + "/service-tokens" }

// serviceTokenRefusal names the hints a refused request gets beyond apiError's.
type serviceTokenRefusal struct {
	action    string // completes "You don't have access to ..."
	grants    bool   // the request carried grants, so a refusal may name one
	notFound  string // 404
	stale     string // 409 without Retry-After
	rootOwned string // 422 refusing a VM cap to a token of the root group
	unchanged string // 503 the API answers before it writes
	unknown   string // another 5xx: the write may have happened
}

// serviceTokenUnwritten holds the 503 reasons the service-token routes give
// before any write (control/avrea/api/routers/service_tokens.py and the
// delegation check in control/avrea/api/authz.py). The API sends no error
// code, so any other 503, such as one from a proxy after the write
// committed, is treated as an unknown outcome.
var serviceTokenUnwritten = []string{
	"Service tokens are not yet available in every service revision",
	"Repository permissions are temporarily unavailable",
	"Delegation authorization is not ready",
}

// refusalAt returns the first validation error the API located in field of
// the request body, or nil.
func refusalAt(he *client.HTTPError, field string) map[string]any {
	details, _ := he.Payload["detail"].([]any)
	for _, detail := range details {
		entry, _ := detail.(map[string]any)
		loc, _ := entry["loc"].([]any)
		if len(loc) > 1 && loc[1] == field {
			return entry
		}
	}
	return nil
}

// refusalType is the type of the validation error located in field, or "".
func refusalType(he *client.HTTPError, field string) string {
	return fieldText(refusalAt(he, field), "type")
}

// refusalNamesGrant reports whether the API's answer points at one grant: a
// validation error located in grants, or a refusal that starts with grants[N].
func refusalNamesGrant(he *client.HTTPError) bool {
	return refusalAt(he, "grants") != nil || strings.HasPrefix(he.Detail, "grants[")
}

// timestampAfter reports whether an API timestamp lies after now. An absent
// one does not; one that cannot be read counts as after, so the caller asks or
// warns rather than staying silent.
func timestampAfter(value string, now time.Time) bool {
	if value == "" {
		return false
	}
	t, err := parseAPITimestamp(value)
	return err != nil || t.After(now)
}

func serviceTokenError(cfg config.Config, err error, r serviceTokenRefusal) error {
	var he *client.HTTPError
	if !errors.As(err, &he) {
		return err
	}
	result := apiError(cfg, he, r.action)
	hint := ""
	switch {
	case he.Status == 401, reauthenticationRequired(he):
	case r.grants && (he.Status == 403 || he.Status == 422) && refusalNamesGrant(he):
		hint = serviceTokenGrantOrderHint
	case he.Status == 422 && r.rootOwned != "" && refusalType(he, "vm_cap") == "root_owned":
		hint = r.rootOwned
	case he.Status == 404:
		hint = r.notFound
	case he.Status == 409 && he.RetryAfter != "":
		hint = tokenRetryAfter(he)
	case he.Status == 409:
		hint = r.stale
	case he.Status == 503 && r.unchanged != "" && slices.Contains(serviceTokenUnwritten, he.Detail):
		hint = r.unchanged + " " + tokenRetryAfter(he)
	case he.Status >= 500:
		hint = r.unknown
	}
	return insertTokenHint(result, hint)
}

// serviceTokenWriteError explains a failed write. An HTTP refusal gets
// serviceTokenError's hints. Any other error, including a success response
// that could not be read, leaves the outcome unknown, so the cause is shown
// with r.unknown; change names what could not be confirmed.
func serviceTokenWriteError(cfg config.Config, err error, r serviceTokenRefusal, change string) error {
	var he *client.HTTPError
	if errors.As(err, &he) {
		return serviceTokenError(cfg, he, r)
	}
	return tokenCreationFailure("Could not confirm "+change+": "+err.Error()+". "+r.unknown, err)
}

// serviceTokenViewHint is the command that shows a token's current state.
func serviceTokenViewHint(token, org string) string {
	return "\x60avr service-token view " + token + " --org " + shellQuotePython(org) + "\x60"
}

// serviceTokenGrantOptions selects what a service token reaches: the grants
// and, through the VM cap, whether it creates VMs. Only set-access registers
// noVMCreate, since a new token starts without the VM permission anyway.
type serviceTokenGrantOptions struct {
	repos, rules, vms []string
	allRules          bool
	allVMs            string
	vmCap             int
	noVMCreate        bool
}

// serviceTokenSelection is what validate accepted: the repositories still to
// resolve, the other grants in the order the grant-order hint describes, and
// the VM cap change, if any. An explicit null resource_id grants every object
// of the type in the organization, including later ones.
type serviceTokenSelection struct {
	repos  []string
	grants []map[string]any
	capSet bool
	vmCap  any // a positive cap, or nil to withdraw it
}

func (g *serviceTokenGrantOptions) flags(c *cobra.Command) {
	f := c.Flags()
	f.StringArrayVarP(&g.repos, "repo", "R", nil, "Repository read grant (owner/name or rep-...), repeatable.")
	f.StringArrayVar(&g.rules, "firewall-rule", nil, "Egress firewall rule read grant (efr-...), repeatable.")
	f.BoolVar(&g.allRules, "all-firewall-rules", false, "Read every egress firewall rule, including later ones.")
	f.StringArrayVar(&g.vms, "vm", nil, "VM grant (cvm-...), read unless :write follows, repeatable.")
	f.StringVar(&g.allVMs, "all-vms", "", "Grant every VM, including later ones: read or write.")
	f.IntVar(&g.vmCap, "vm-create-limit", 0, "Let the token create VMs, holding at most this many at once.")
}

// serviceTokenLevelCovers reports whether a grant at have also allows need.
func serviceTokenLevelCovers(have, need string) bool {
	return slices.Index(serviceTokenLevels, have) >= slices.Index(serviceTokenLevels, need)
}

// serviceTokenIDError refuses a grant value that is not an object ID.
func serviceTokenIDError(flag, spec, noun, prefix string) error {
	return &ExitError{2, fmt.Sprintf("Error: Invalid %s value %s: expected a %s ID, %s followed by 32 lowercase hexadecimal characters.", flag, pythonQuote(output.CellText(spec)), noun, prefix)}
}

// validate checks the selection before any request.
func (g serviceTokenGrantOptions) validate(cmd *cobra.Command) (serviceTokenSelection, error) {
	var s serviceTokenSelection
	limited := cmd.Flags().Changed("vm-create-limit")
	allVMs := cmd.Flags().Changed("all-vms")
	if limited && g.noVMCreate {
		return s, &ExitError{2, "Error: --vm-create-limit and --no-vm-create are mutually exclusive."}
	}
	if limited && (g.vmCap < 1 || g.vmCap > serviceTokenMaxVMCap) {
		return s, &ExitError{2, fmt.Sprintf("Error: --vm-create-limit must be between 1 and %d.", serviceTokenMaxVMCap)}
	}
	// --no-vm-create alone leaves a token only the VMs it created, which the
	// API accepts; with nothing selected at all, set-access would remove
	// every grant by accident.
	if len(g.repos) == 0 && len(g.rules) == 0 && !g.allRules && len(g.vms) == 0 && !allVMs && !limited && !g.noVMCreate {
		if cmd.Flags().Lookup("no-vm-create") != nil {
			return s, &ExitError{2, "Error: Pass the access the token keeps (--repo, --firewall-rule, --all-firewall-rules, --vm, --all-vms or --vm-create-limit), or --no-vm-create to withdraw VM creation: set-access replaces the whole selection."}
		}
		return s, &ExitError{2, "Error: Pass at least one of --repo, --firewall-rule, --all-firewall-rules, --vm, --all-vms or --vm-create-limit: a service token needs a grant or permission to create VMs."}
	}
	if g.allRules && len(g.rules) > 0 {
		return s, &ExitError{2, "Error: --all-firewall-rules already grants every firewall rule; drop --firewall-rule."}
	}
	if allVMs {
		if err := validateChoice("all-vms", g.allVMs, serviceTokenLevels); err != nil {
			return s, err
		}
	}
	s.capSet, s.vmCap = limited || g.noVMCreate, nil
	if limited {
		s.vmCap = g.vmCap
	}
	for _, spec := range g.repos {
		repo, err := stripTokenLevel(spec, "repository", "--repo", "read")
		if err != nil {
			return s, err
		}
		s.repos = append(s.repos, repo)
	}
	// Repeats are one grant, so they neither count toward the limit nor
	// resolve twice.
	s.repos = distinct(s.repos)
	add := func(kind string, id any, level string) {
		s.grants = append(s.grants, map[string]any{"resource_type": kind, "resource_id": id, "access_level": level})
	}
	for _, spec := range g.rules {
		rule, err := stripTokenLevel(spec, "firewall rule", "--firewall-rule", "read")
		if err != nil {
			return s, err
		}
		if !serviceTokenRulePattern().MatchString(rule) {
			return s, serviceTokenIDError("--firewall-rule", spec, "rule", "efr-")
		}
		if !slices.ContainsFunc(s.grants, func(grant map[string]any) bool { return grant["resource_id"] == rule }) {
			add("egress_firewall_rule", rule, "read")
		}
	}
	if g.allRules {
		add("egress_firewall_rule", nil, "read")
	}
	for _, spec := range g.vms {
		id, level, err := splitTokenLevel(spec, "VM", "--vm", serviceTokenLevels...)
		if err != nil {
			return s, err
		}
		if !serviceTokenVMPattern().MatchString(id) {
			return s, serviceTokenIDError("--vm", spec, "VM", "cvm-")
		}
		if allVMs && serviceTokenLevelCovers(g.allVMs, level) {
			return s, &ExitError{2, fmt.Sprintf("Error: --all-vms %s already grants every VM at %s; drop --vm %s.", g.allVMs, g.allVMs, output.CellText(spec))}
		}
		i := slices.IndexFunc(s.grants, func(grant map[string]any) bool { return grant["resource_id"] == id })
		switch {
		case i < 0:
			add("customer_vm", id, level)
		case s.grants[i]["access_level"] != level:
			return s, &ExitError{2, fmt.Sprintf("Error: --vm names %s at both %s and %s; pass it once at the level it needs.", id, s.grants[i]["access_level"], level)}
		}
	}
	if allVMs {
		add("customer_vm", nil, g.allVMs)
	}
	if count := len(s.repos) + len(s.grants); count > serviceTokenMaxGrants {
		return s, &ExitError{2, fmt.Sprintf("Error: A service token takes at most %d grants (got %d).", serviceTokenMaxGrants, count)}
	}
	return s, nil
}

// build resolves the repositories and puts their grants first. Two names of
// one repository still become one grant.
func (s serviceTokenSelection) build(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org string) ([]map[string]any, error) {
	grants, err := repositoryReadGrants(ctx, d, cfg, api, org, s.repos)
	if err != nil {
		return nil, err
	}
	return append(grants, s.grants...), nil
}

// applyVMCap writes the VM cap change into a create or set-access body. An
// absent key keeps a token's current cap, and null withdraws it.
func (s serviceTokenSelection) applyVMCap(body map[string]any) {
	if s.capSet {
		body["vm_cap"] = s.vmCap
	}
}

// distinct keeps the first of each repeated value, in order.
func distinct(values []string) []string {
	seen := map[string]bool{}
	return slices.DeleteFunc(slices.Clone(values), func(value string) bool {
		repeated := seen[value]
		seen[value] = true
		return repeated
	})
}

// serviceTokenRemovals lists what a set-access body takes away from the
// token as read: each grant the new grants neither repeat nor cover at its
// level or above, and a withdrawn or lowered VM cap. A grant of every object
// of a type covers a grant of one at the same level.
func serviceTokenRemovals(current, body map[string]any) []string {
	grants, _ := body["grants"].([]map[string]any)
	var removed []string
	for _, held := range objectList(current["grants"]) {
		kind, id, level := fieldText(held, "resource_type"), fieldText(held, "resource_id"), fieldText(held, "access_level")
		// The strongest new grant on the same object, named or every object.
		best := ""
		for _, grant := range grants {
			wanted, at := fieldText(grant, "resource_id"), fieldText(grant, "access_level")
			if fieldText(grant, "resource_type") == kind && (wanted == id || wanted == "") && (best == "" || serviceTokenLevelCovers(at, best)) {
				best = at
			}
		}
		label, resource := serviceTokenGrantLabel(held)
		if id != "" {
			resource = label + " " + resource
		}
		switch {
		case best != "" && serviceTokenLevelCovers(best, level):
		case best != "":
			removed = append(removed, fmt.Sprintf("%s lowered from %s to %s", resource, level, best))
		default:
			removed = append(removed, fmt.Sprintf("%s at %s", resource, level))
		}
	}
	held, capped := devicePositiveInt(current["vm_cap"], serviceTokenMaxVMCap)
	if value, set := body["vm_cap"]; set && capped {
		switch wanted, _ := value.(int); {
		case value == nil:
			removed = append(removed, fmt.Sprintf("permission to create VMs (up to %d)", held))
		case int64(wanted) < held:
			removed = append(removed, fmt.Sprintf("VM cap lowered from %d to %d", held, wanted))
		}
	}
	return removed
}

// serviceTokenOwnedVMs counts the VMs a service token still owns after its
// permission to create them was withdrawn, or "" when there are none. It
// administers them until they are deleted.
func serviceTokenOwnedVMs(row map[string]any) string {
	allowed, owned, _ := tokenVMCreate(row)
	if allowed || row["vm_count"] == nil || owned == "0" {
		return ""
	}
	return owned
}

// serviceTokenVMs adds serviceTokenOwnedVMs to tokenVMCreateText.
func serviceTokenVMs(row map[string]any) string {
	if owned := serviceTokenOwnedVMs(row); owned != "" {
		return tokenVMCreateText(row) + ", " + owned + " still owned"
	}
	return tokenVMCreateText(row)
}

// serviceTokenGrantLabel names a grant's type and object for people.
func serviceTokenGrantLabel(grant map[string]any) (string, string) {
	kind, resource := fieldText(grant, "resource_type"), fieldText(grant, "resource_id")
	switch kind {
	case "repository":
	case "egress_firewall_rule":
		kind = "firewall rule"
		if resource == "" {
			resource = "every firewall rule"
		}
	case "customer_vm":
		kind = "VM"
		if resource == "" {
			resource = "every VM"
		}
	}
	return kind, output.CellText(resource)
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

// serviceTokenWriteNotFound is the 404 hint for a write after a successful
// read. While service tokens are switched off for the organization, the read
// still succeeds and shows the block, but rotating and widening access answer
// 404; narrowing and revoking still work.
func serviceTokenWriteNotFound(current map[string]any, org string) string {
	if fieldText(current, "blocked_by") == "feature_disabled" {
		return "Service tokens are not available for this change in this organization; narrowing a token's access and revoking it still work. Contact support@avrea.com to enable them."
	}
	return serviceTokenListHint(org)
}

func newServiceTokenList(d Dependencies) *cobra.Command {
	var org, group, status string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List the organization's service tokens, soonest expiry first.", Args: cobra.NoArgs}
	c.Flags().StringVarP(&org, "org", "o", "", "Organization ID or slug.")
	c.Flags().StringVar(&group, "group", "", "Only the tokens this group owns: a group name, path or ID (grp-...).")
	c.Flags().StringVar(&status, "status", "", "Only tokens with this status: live, expired or revoked.")
	c.Flags().IntVarP(&limit, "limit", "L", 50, "Maximum tokens (1–1000).")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		base := url.Values{}
		if cmd.Flags().Changed("group") {
			var err error
			if group, err = groupOption(group); err != nil {
				return err
			}
		}
		if cmd.Flags().Changed("status") {
			if err := validateChoice("status", status, []string{"live", "expired", "revoked"}); err != nil {
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
		if cmd.Flags().Changed("group") {
			if group, err = resolveGroup(cmd.Context(), cfg, api, id, group); err != nil {
				return err
			}
			base.Set("group_id", "eq."+group)
		}
		rows, more, err := walkPages("service token", limit, func(cursor string, wanted int) (client.Page, error) {
			query := url.Values{"limit": {strconv.Itoa(min(wanted, 200))}}
			for key, values := range base {
				query[key] = values
			}
			if cursor != "" {
				query.Set("cursor", cursor)
			}
			page, e := api.AccessTokens(cmd.Context(), serviceTokensPath(id), query)
			if e != nil {
				return page, serviceTokenError(cfg, e, serviceTokenRefusal{action: "list service tokens"})
			}
			return page, nil
		})
		if err != nil {
			return err
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
				// The CLI has no cursor option, so only an unused filter helps.
				var unset []string
				for _, name := range []string{"group", "status"} {
					if !cmd.Flags().Changed(name) {
						unset = append(unset, "--"+name)
					}
				}
				advice = "The CLI shows at most 1000 service tokens for these filters."
				if len(unset) > 0 {
					advice = "Narrow the list with " + strings.Join(unset, " or ") + " to see the rest."
				}
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
	c.Flags().StringVarP(&org, "org", "o", "", "Organization ID or slug.")
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
	f.StringVarP(&org, "org", "o", "", "Organization ID or slug.")
	f.StringVar(&name, "name", "", "Token name (1–100 characters).")
	f.StringVar(&group, "group", "", "The group that owns the token: a group name, path or ID (grp-...).")
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
		var err error
		if group, err = groupOption(group); err != nil {
			return err
		}
		body := map[string]any{"name": name}
		if err := l.apply(cmd, body); err != nil {
			return err
		}
		selection, err := g.validate(cmd)
		if err != nil {
			return err
		}
		selection.applyVMCap(body)
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
		if body["group_id"], err = resolveGroup(cmd.Context(), cfg, api, id, group); err != nil {
			return err
		}
		if body["grants"], err = selection.build(cmd.Context(), d, cfg, api, id); err != nil {
			return err
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		minted, err := api.MintAccessToken(cmd.Context(), serviceTokensPath(id), body)
		if err != nil {
			return serviceTokenWriteError(cfg, err, serviceTokenRefusal{
				action:    "create a service token",
				grants:    true,
				notFound:  "Check the organization and API host. If service tokens are unavailable for this organization, contact support@avrea.com.",
				rootOwned: "A token of the root group cannot create VMs. Pass a group below it to --group: \x60avr group list\x60 shows them.",
				unchanged: "No service token was created.",
				unknown:   "A service token may have been created; check \x60avr service-token list --org " + shellQuotePython(id) + "\x60 before retrying.",
			}, "the creation")
		}
		record, _ := minted.(map[string]any)
		credential, ok := record["token"].(string)
		if !ok || credential == "" {
			return &ExitError{1, "Error: The API reported success without a secret, so a service token may exist that nothing can use." + discardHint("service-token", record, id)}
		}
		return deliverOneTimeCredential(cmd.Context(), d, oneTimeCredential{
			record: record, credential: credential, hasJSON: f.Changed("json"), fields: fields, expression: o.expression,
			intro:    "The secret is shown only once. Store it now, for example as a CI secret, and set it as AVR_TOKEN where the token is used:",
			exports:  [][2]string{{"AVR_TOKEN", credential}, {"AVR_ORG", id}},
			summary:  func() error { return renderServiceToken(d, record) },
			outcome:  "The service token was created",
			recovery: discardHint("service-token", record, id),
		})
	})
	return c
}

func newServiceTokenSetAccess(d Dependencies) *cobra.Command {
	var org string
	var yes bool
	var g serviceTokenGrantOptions
	c := &cobra.Command{Use: "set-access TOKEN_ID", Short: "Replace a service token's whole access selection.", Args: tokenIDArgs}
	c.Flags().StringVarP(&org, "org", "o", "", "Organization ID or slug.")
	g.flags(c)
	c.Flags().BoolVar(&g.noVMCreate, "no-vm-create", false, "Withdraw the token's permission to create VMs.")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Apply the change without asking, even when it removes or lowers access.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		selection, err := g.validate(cmd)
		if err != nil {
			return err
		}
		cfg, api, id, err := serviceTokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		// Repositories resolve before the token is read, so the version the
		// write echoes is as fresh as the requests allow; a question about
		// removed access then stands between the read and the write.
		grants, err := selection.build(cmd.Context(), d, cfg, api, id)
		if err != nil {
			return err
		}
		path := serviceTokensPath(id) + "/" + args[0]
		current, version, err := readServiceTokenVersion(cmd.Context(), cfg, api, id, path, "grants_version")
		if err != nil {
			return err
		}
		body := map[string]any{"grants": grants, "expected_grants_version": version}
		selection.applyVMCap(body)
		// Leaving a grant out removes it, which is easy to do by accident, for
		// example when only the VM cap was meant to change. A terminal run is
		// asked on stderr; any other run must confirm with --yes rather than
		// block on a question nobody sees, and is told what it would remove.
		if removed := serviceTokenRemovals(current, body); len(removed) > 0 && !yes {
			summary := "This replacement removes access service token " + args[0] + " holds:\n    " + strings.Join(removed, "\n    ")
			if !d.IsInputTerminal() || !d.IsErrorTerminal() || d.Getenv("AVR_PROMPT_DISABLED") != "" {
				return &ExitError{1, "Error: " + summary + "\n  Hint: Pass each grant to keep at its level, or pass --yes to apply the change."}
			}
			if err = confirmMutation(cmd.Context(), configurationPrompt(d), "replacing the access removes access the token holds", summary+"\nReplace its access?"); err != nil {
				return err
			}
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		view := serviceTokenViewHint(args[0], id)
		// A rerun sends the same whole selection, which would undo a change
		// another admin made in between.
		stale := "The token changed after this command read it. Check " + view + ", then run set-access again with the full selection you want."
		record, err := api.Do(cmd.Context(), http.MethodPut, path+"/grants", body)
		if err != nil {
			return serviceTokenWriteError(cfg, err, serviceTokenRefusal{
				action:    "replace the service token's access",
				grants:    true,
				notFound:  serviceTokenWriteNotFound(current, id),
				stale:     serviceTokenWriteStale(current, stale),
				rootOwned: "A token of the root group cannot create VMs. Leave out --vm-create-limit, or create a token in a group below the root group with \x60avr service-token create\x60.",
				unchanged: "The token's access was not changed.",
				unknown:   "The access may have been replaced; check " + view + " before retrying.",
			}, "the access change")
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
	var yes bool
	var l serviceTokenLifetime
	var o jsonOptions
	c := &cobra.Command{Use: "rotate TOKEN_ID", Short: "Issue a service token a new secret and print it once.", Args: tokenIDArgs}
	f := c.Flags()
	f.StringVarP(&org, "org", "o", "", "Organization ID or slug.")
	l.flags(c)
	f.StringVar(&overlap, "overlap", "", "How long the old secret keeps working: 1h to 7d (default 24h).")
	f.BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
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
		// Rotating a live token inside an earlier overlap retires the secret
		// that rotation replaced, which consumers may still use. Only a run
		// with stdin and stderr on a terminal is asked, so scripts keep
		// working and a prompt never hides in a redirected log; it goes to
		// stderr to keep stdout for the secret. A revoked or expired token
		// keeps previous_valid_until, but the API refuses to rotate it.
		if until := fieldText(current, "previous_valid_until"); !yes && d.IsInputTerminal() && d.IsErrorTerminal() && fieldText(current, "status") == "live" && timestampAfter(until, d.Now()) {
			if err = confirmMutation(cmd.Context(), configurationPrompt(d), "rotating would retire the secret an earlier rotation replaced", "An earlier rotation of service token "+args[0]+" is still in its overlap until "+formatTimestamp(until)+". Rotating now retires the secret it replaced at once. Rotate?"); err != nil {
				return err
			}
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		retire := "Rotating again retires the previous secret at once."
		view := serviceTokenViewHint(args[0], id)
		unknown := fmt.Sprintf("The token may have been rotated; it was at rotation version %d before this attempt. Check its rotation version with %s before retrying: a higher version means a rotation happened. %s", version, view, retire)
		stale := "The token changed after this command read it. Check " + view + " before retrying: if it was rotated, rotating again retires the previous secret at once."
		minted, err := api.MintAccessToken(cmd.Context(), path+"/rotate", body)
		if err != nil {
			return serviceTokenWriteError(cfg, err, serviceTokenRefusal{
				action:    "rotate the service token",
				notFound:  serviceTokenWriteNotFound(current, id),
				stale:     serviceTokenWriteStale(current, stale),
				unchanged: "The token was not rotated.",
				unknown:   unknown,
			}, "the rotation")
		}
		record, _ := minted.(map[string]any)
		credential, ok := record["token"].(string)
		if !ok || credential == "" {
			return &ExitError{1, "Error: The API reported success without a secret. " + unknown}
		}
		// What the rotation changed beyond the secret, from the token as it
		// was read. Written to stderr so --json output stays the record, and
		// best-effort: a failed notice must not keep the secret from stdout.
		if current["expires_at"] == nil && !l.never && !f.Changed("ttl") {
			_, _ = fmt.Fprintln(d.Err, d.errorPaint("! This token did not expire before; its new secret expires at "+formatTimestamp(fieldText(record, "expires_at"))+". To keep it from expiring, switch consumers to the new secret first, then rotate with --never-expires: rotating before the overlap ends retires the previous secret at once.", "33"))
		}
		if timestampAfter(fieldText(current, "previous_valid_until"), d.Now()) {
			_, _ = fmt.Fprintln(d.Err, d.errorPaint("! An earlier rotation was still in its overlap: the secret it replaced stopped working now.", "33"))
		}
		return deliverOneTimeCredential(cmd.Context(), d, oneTimeCredential{
			record: record, credential: credential, hasJSON: f.Changed("json"), fields: fields, expression: o.expression,
			intro:   "The new secret is shown only once. Store it now and set it as AVR_TOKEN wherever the token is used:",
			exports: [][2]string{{"AVR_TOKEN", credential}, {"AVR_ORG", id}},
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
	c.Flags().StringVarP(&org, "org", "o", "", "Organization ID or slug.")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, id, err := serviceTokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if !yes {
			if err = confirmMutation(cmd.Context(), d, "revoking a service token", "Revoke service token "+args[0]+"? Everything using it stops working, including a secret still in its rotation overlap, and the VMs it created are deleted."); err != nil {
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
	c.Flags().StringVarP(&org, "org", "o", "", "Organization ID or slug.")
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
			if err = confirmMutation(cmd.Context(), d, "revoking every service token", "Revoke every service token of organization "+label+"? Everything using them stops working, and the VMs they created are deleted."); err != nil {
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
		{"Secret issued", optional("rotated_at", "unknown")}, {"Old secret until", optional("previous_valid_until", "-")},
		{"Last used", optional("last_used_at", "never")},
		{"VM create", serviceTokenVMs(row)},
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
		kind, resource := serviceTokenGrantLabel(grant)
		cells = append(cells, []string{kind, resource, fieldText(grant, "access_level")})
	}
	return output.Grid(d.Out, []string{"Type", "Resource", "Level"}, cells)
}
