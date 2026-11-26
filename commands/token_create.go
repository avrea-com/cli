package commands

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"slices"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var tokenCreateFields = append(slices.Clone(tokenFields), "token")
var tokenDurationDigits = lazyRegexp("^[0-9]+$")

const tokenUnknownOutcome = "A token may have been created; check \x60avr token list\x60 before retrying."
const tokenGrantOrderHint = "grants[N] counts from 0 after resources are resolved and duplicates removed: the --repo values first, then the --vm values, in the order given."
const tokenFilterRetain = "The output filter must retain the one-time credential."

func stripTokenLevel(spec, kind, flag, level string) (string, error) {
	target, _, err := splitTokenLevel(spec, kind, flag, level)
	return target, err
}

// splitTokenLevel separates a grant value from its optional :level suffix and
// checks the level against the ones the grant takes. Without a suffix the
// grant gets the first level.
func splitTokenLevel(spec, kind, flag string, levels ...string) (string, string, error) {
	target, level := spec, levels[0]
	// Python rpartition treats only the last colon as the optional level.
	if index := strings.LastIndex(spec, ":"); index >= 0 {
		target, level = spec[:index], strings.ToLower(spec[index+1:])
	}
	if target != "" && slices.Contains(levels, level) {
		return target, level, nil
	}
	example := output.CellText(cmp.Or(target, "ID"))
	takes := kind + " grants are created at the " + levels[0] + " level"
	if len(levels) > 1 {
		takes = kind + " grants take " + joinOr(levels)
	}
	uses := []string{flag + " " + example}
	for _, l := range levels {
		uses = append(uses, flag+" "+example+":"+l)
	}
	return "", "", &ExitError{2, fmt.Sprintf("Error: Invalid %s value %s: %s. Use %s.", flag, pythonQuote(output.CellText(spec)), takes, joinOr(uses))}
}

// joinOr joins values as "a, b or c".
func joinOr(values []string) string {
	last := len(values) - 1
	if last == 0 {
		return values[0]
	}
	return strings.Join(values[:last], ", ") + " or " + values[last]
}
func tokenTTL(value string) (int, error) { return tokenDuration("--ttl", value, 60, 604800) }

// tokenDuration parses 30m, 8h, 7d or plain seconds for a duration option and
// checks it against the API's bounds in seconds.
func tokenDuration(flag, value string, minimum, maximum int64) (int, error) {
	text := strings.ToLower(strings.TrimSpace(value))
	number, multiplier := text, int64(1)
	if len(text) > 0 {
		switch text[len(text)-1] {
		case 's':
			number = text[:len(text)-1]
		case 'm':
			number = text[:len(text)-1]
			multiplier = 60
		case 'h':
			number = text[:len(text)-1]
			multiplier = 3600
		case 'd':
			number = text[:len(text)-1]
			multiplier = 86400
		}
	}
	if !tokenDurationDigits().MatchString(number) {
		return 0, &ExitError{2, fmt.Sprintf("Error: Invalid value for %s: invalid duration %s: use e.g. 30m, 8h, 7d, or a number of seconds", flag, pythonQuote(output.CellText(value)))}
	}
	seconds, _ := new(big.Int).SetString(number, 10)
	seconds.Mul(seconds, big.NewInt(multiplier))
	if seconds.Cmp(big.NewInt(minimum)) < 0 || seconds.Cmp(big.NewInt(maximum)) > 0 {
		return 0, &ExitError{2, fmt.Sprintf("Error: Invalid value for %s: must be between %ds and %ds (got %ss)", flag, minimum, maximum, seconds.String())}
	}
	return int(seconds.Int64()), nil
}
func newTokenCreate(d Dependencies) *cobra.Command {
	var org, name, ttl string
	var repos, vms []string
	var allow bool
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "create", Short: "Create a personal access token and print its credential.", Args: cobra.NoArgs}
	f := c.Flags()
	f.StringVarP(&org, "org", "o", "", "Organization ID or slug.")
	f.StringVar(&name, "name", "", "Token name (1–100 characters).")
	f.StringArrayVarP(&repos, "repo", "R", nil, "Repository read grant, repeatable.")
	f.StringArrayVar(&vms, "vm", nil, "VM admin grant, repeatable.")
	f.BoolVar(&allow, "allow-vm-create", false, "Let the token create VMs.")
	f.IntVar(&limit, "vm-create-limit", 0, "Maximum VMs the token may create (1–10).")
	f.StringVar(&ttl, "ttl", "", "Lifetime: 30m, 8h, 7d or seconds (60 seconds–7 days).")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if !f.Changed("name") {
			return &ExitError{2, "Error: Missing option '--name'."}
		}
		if f.Changed("vm-create-limit") && (limit < 1 || limit > 10) {
			return &ExitError{2, "Error: --vm-create-limit must be between 1 and 10."}
		}
		if done, err := o.prepare(cmd, d, tokenCreateFields); done || err != nil {
			return err
		}
		fields, err := output.Fields(o.fields, tokenCreateFields)
		if err != nil {
			return err
		}
		hasJSON := f.Changed("json")
		if hasJSON && !slices.Contains(fields, "token") {
			return &ExitError{2, "Error: --json must include the token field (or \"*\"): the credential is returned only once."}
		}
		if len(repos) == 0 && len(vms) == 0 && !allow {
			return &ExitError{2, "Error: Pass at least one of --repo, --vm or --allow-vm-create: a token with no grant can do nothing."}
		}
		if f.Changed("vm-create-limit") && !allow {
			return &ExitError{2, "Error: --vm-create-limit requires --allow-vm-create."}
		}
		repoNames := make([]string, len(repos))
		vmIDs := make([]string, len(vms))
		for i, spec := range repos {
			repoNames[i], err = stripTokenLevel(spec, "repository", "--repo", "read")
			if err != nil {
				return err
			}
		}
		for i, spec := range vms {
			vmIDs[i], err = stripTokenLevel(spec, "VM", "--vm", "admin")
			if err != nil {
				return err
			}
		}
		if len(repos)+len(vms) > 100 {
			return &ExitError{2, fmt.Sprintf("Error: A token takes at most 100 grants (got %d).", len(repos)+len(vms))}
		}
		body := map[string]any{"name": name, "allow_vm_create": allow}
		if f.Changed("vm-create-limit") {
			body["vm_create_limit"] = limit
		}
		if f.Changed("ttl") {
			seconds, e := tokenTTL(ttl)
			if e != nil {
				return e
			}
			body["ttl_seconds"] = seconds
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		id, err := resolveOrg(cmd.Context(), d, cfg, api, org)
		if err != nil {
			return err
		}
		grants, err := repositoryReadGrants(cmd.Context(), d, cfg, api, id, repoNames)
		if err != nil {
			return err
		}
		vmSeen := map[string]bool{}
		for _, vm := range vmIDs {
			if !vmSeen[vm] {
				grants = append(grants, map[string]any{"resource_type": "customer_vm", "resource_id": vm, "access_level": "admin"})
				vmSeen[vm] = true
			}
		}
		body["grants"] = grants
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		minted, err := api.MintAccessToken(cmd.Context(), "/orgs/"+url.PathEscape(id)+"/access-tokens", body)
		if err != nil {
			var he *client.HTTPError
			if errors.As(err, &he) {
				return tokenCreateError(cfg, he, len(vmIDs) > 0 || allow)
			}
			return tokenCreationFailure("The creation response was not received. "+tokenUnknownOutcome, err)
		}
		record, _ := minted.(map[string]any)
		credential, ok := record["token"].(string)
		if !ok || credential == "" {
			return &ExitError{1, "Error: The API reported success without a credential, so a token may exist that nothing can use." + discardHint("token", record, id)}
		}
		return deliverCreatedToken(cmd.Context(), d, record, credential, id, hasJSON, fields, o.expression)
	})
	return c
}

// repositoryReadGrants resolves repositories in the order given and drops
// repeats, so grants[N] in an API refusal follows the --repo values.
func repositoryReadGrants(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org string, names []string) ([]map[string]any, error) {
	grants := []map[string]any{}
	seen := map[string]bool{}
	for _, repo := range names {
		rid, err := resolveRepository(ctx, d, cfg, api, org, repo, "")
		if err != nil {
			return nil, err
		}
		if !seen[rid] {
			grants = append(grants, map[string]any{"resource_type": "repository", "resource_id": rid, "access_level": "read"})
			seen[rid] = true
		}
	}
	return grants, nil
}

// discardHint tells how to revoke a token whose secret was not delivered;
// command is the token's command group, token or service-token.
func discardHint(command string, record map[string]any, org string) string {
	if id, ok := record["id"].(string); ok && tokenIDPattern().MatchString(id) {
		return " To discard it, run \x60avr " + command + " revoke " + id + " --org " + shellQuotePython(org) + "\x60."
	}
	return " Check \x60avr " + command + " list --org " + shellQuotePython(org) + "\x60 before retrying."
}
func tokenCreationFailure(message string, cause error) error {
	code := 1
	if errors.Is(cause, context.Canceled) {
		code = 130
	}
	return &ExitError{code, "Error: " + message}
}
func tokenCreateError(cfg config.Config, he *client.HTTPError, namesVMs bool) error {
	result := apiError(cfg, he, "create the token")
	if he.Status == 401 {
		return result
	}
	hint := ""
	switch he.Status {
	case 403, 422:
		raw := pythonValue(he.Payload)
		if strings.Contains(raw, "grants") {
			hint = tokenGrantOrderHint
		}
	case 404:
		hint = "Check the organization and API host. If personal access tokens are unavailable, contact support@avrea.com."
		if namesVMs {
			hint += " Check whether customer VMs are enabled for this organization."
		}
	case 409:
		hint = "A member can hold at most 20 live tokens per organization. Free one with \x60avr token list\x60 and \x60avr token revoke <token-id>\x60."
	default:
		if he.Status >= 500 {
			hint = tokenUnknownOutcome
			if he.Status == 503 && he.Detail != "" {
				hint = "No token was created. " + tokenRetryAfter(he)
			}
		}
	}
	return insertTokenHint(result, hint)
}

// tokenRetryAfter reads a refusal's Retry-After seconds as advice.
func tokenRetryAfter(he *client.HTTPError) string {
	value := strings.TrimSpace(he.RetryAfter)
	if !tokenDurationDigits().MatchString(value) {
		return "Retry shortly."
	}
	seconds, _ := new(big.Int).SetString(value, 10)
	return "Retry in " + seconds.String() + "s."
}

// insertTokenHint places a hint after the API's reason and before the
// credential-kind hint and API line that apiError appends.
func insertTokenHint(result error, hint string) error {
	if hint == "" {
		return result
	}
	lines := strings.Split(result.Error(), "\n")
	index := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(line, "  Hint: a personal access token") || strings.HasPrefix(line, "  API: ") {
			index = i
			break
		}
	}
	lines = slices.Insert(lines, index, "  Hint: "+hint)
	return errors.New(strings.Join(lines, "\n"))
}
func flushTokenOutput(out io.Writer) error {
	if writer, ok := out.(interface{ Flush() error }); ok {
		return writer.Flush()
	}
	return nil
}
func deliverCreatedToken(ctx context.Context, d Dependencies, record map[string]any, credential, org string, hasJSON bool, fields []string, expression string) error {
	target := fieldText(record, "organization_id")
	if target == "" {
		target = org
	}
	return deliverOneTimeCredential(ctx, d, oneTimeCredential{
		record: record, credential: credential, hasJSON: hasJSON, fields: fields, expression: expression,
		intro:   "The credential is shown only once. To use the token:",
		exports: [][2]string{{"AVR_TOKEN", credential}, {"AVR_ORG", target}},
		summary: func() error {
			// Python's human summary rejects non-string timestamps after writing
			// exports. Preserve the delivered credential and report the same recovery.
			for _, key := range []string{"created_at", "expires_at", "last_used_at", "revoked_at"} {
				if value := record[key]; value != nil {
					if _, ok := value.(string); !ok {
						return errors.New("invalid token summary")
					}
				}
			}
			return renderToken(d, record)
		},
		outcome:  "The token was created",
		recovery: discardHint("token", record, org),
	})
}

// oneTimeCredential is a credential the API returns once, with what to print
// around it and how to recover when its output fails.
type oneTimeCredential struct {
	record            map[string]any
	credential        string
	hasJSON           bool
	fields            []string
	expression        string
	intro             string
	exports           [][2]string
	summary           func() error
	outcome, recovery string
}

func deliverOneTimeCredential(ctx context.Context, d Dependencies, c oneTimeCredential) error {
	record, credential, hasJSON, fields := c.record, c.credential, c.hasJSON, c.fields
	exportsShown := false
	retained := true
	err := func() error {
		if hasJSON {
			var buffer bytes.Buffer
			if err := output.JSON(ctx, &buffer, record, fields, c.expression, d.IsTerminal()); err != nil {
				return err
			}
			if !strings.Contains(buffer.String(), credential) {
				retained = false
				return errors.New("output filter omitted credential")
			}
			if _, err := io.Copy(d.Out, &buffer); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintln(d.Out, c.intro); err != nil {
				return err
			}
			for _, export := range c.exports {
				if _, err := fmt.Fprintln(d.Out, "  export "+export[0]+"="+shellQuotePython(export[1])); err != nil {
					return err
				}
			}
			exportsShown = true
			if _, err := fmt.Fprintln(d.Out); err != nil {
				return err
			}
			if err := c.summary(); err != nil {
				return err
			}
		}
		return flushTokenOutput(d.Out)
	}()
	if err == nil {
		// Everything reached stdout. A cancellation that arrives now must not
		// start recovery, which would print the secret a second time, but the
		// caller still learns that the change happened.
		if ctx.Err() != nil {
			return &ExitError{130, "Interrupted. " + c.outcome + " and its output on stdout is complete; if that output was saved, do not run the command again." + c.recovery}
		}
		return nil
	}
	message := c.outcome + ", but output failed."
	if !retained {
		message = c.outcome + ", but output failed: " + tokenFilterRetain
	}
	delivered := ""
	recovery := func() error {
		if hasJSON {
			// Recovery must work even when Ctrl-C interrupted jq. It is a local,
			// unfiltered write with no query execution or further network request.
			if e := output.JSON(ctx, d.Out, record, fields, "", d.IsTerminal()); e != nil {
				return e
			}
			delivered = " stdout contains its unfiltered JSON; save the credential rather than retrying."
		} else if exportsShown {
			delivered = " The export lines on stdout are complete and valid; only the token summary is missing."
		}
		return flushTokenOutput(d.Out)
	}()
	if recovery != nil {
		delivered = ""
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return tokenCreationFailure(message+delivered+c.recovery, err)
}
