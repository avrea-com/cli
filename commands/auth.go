package commands

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var statusFields = []string{"email", "name", "created_at", "user_id", "host", "default_org", "token", "credential_type"}

// serviceStatusFields extends statusFields with a service token's identity;
// the other credential kinds keep their field set.
var serviceStatusFields = append(slices.Clone(statusFields), "organization_id", "api_key_id", "group_id", "expires_at", "previous_valid_until", "expiring_soon", "grants")

func newStatus(d Dependencies) *cobra.Command {
	var show bool
	var fields, expression string
	cmd := &cobra.Command{Use: "status", Short: "Display the authenticated user and connection state.", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&show, "show-token", false, "Display the auth token in plain text.")
	cmd.Flags().StringVar(&fields, "json", "", "Output comma-separated fields, * for all, or ? to list fields.")
	cmd.Flags().StringVarP(&expression, "jq", "q", "", "Filter JSON with an embedded jq expression.")
	cmd.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, _, err := d.configuration()
		if err != nil {
			return err
		}
		kind := config.Kind(cfg.Token)
		available := statusFields
		if kind == config.ServiceToken {
			available = serviceStatusFields
		}
		hasJSON := cmd.Flags().Changed("json")
		if expression != "" && !hasJSON {
			return &ExitError{2, "Error: --jq requires --json"}
		}
		var selected []string
		if hasJSON {
			if fields == "?" {
				return output.Discovery(d.Out, available)
			}
			if _, err := output.Fields(fields, available); err != nil {
				return err
			}
		}
		if err := authenticated(cfg); err != nil {
			return err
		}
		if hasJSON {
			// Checked before the identity request, which a service token pays
			// for out of its request allowance.
			schema := make([]string, 0, len(available))
			for _, f := range available {
				if f != "token" || show {
					schema = append(schema, f)
				}
			}
			if selected, err = output.Fields(fields, schema); err != nil {
				return err
			}
			if expression != "" {
				if _, err = output.CompileJQ(expression); err != nil {
					return err
				}
			}
		}
		result := map[string]any{}
		var credentialType string
		switch kind {
		case config.PersonalKey:
			credentialType = "api_key"
			result, err = d.apiClient(cfg).Do(cmd.Context(), http.MethodGet, "/users/me", nil)
			if err != nil {
				return apiError(cfg, err, "fetch user info")
			}
		case config.ScopedToken:
			credentialType = "scoped_token"
		case config.ServiceToken:
			// Unlike a scoped token, a service token can read its own identity,
			// so this verifies it against the API.
			credentialType = "service_token"
			result, err = serviceIdentity(cmd.Context(), cfg, d.apiClient(cfg))
			if err != nil {
				return apiError(cfg, err, "verify the service token")
			}
		default:
			return fmt.Errorf("unsupported credential: %s", kind)
		}
		if hasJSON {
			result["user_id"] = result["id"]
			result["host"] = cfg.Host
			result["default_org"] = nil
			if cfg.Org != "" {
				result["default_org"] = cfg.Org
			}
			result["credential_type"] = credentialType
			delete(result, "token")
			if show {
				result["token"] = cfg.Token
			}
			return output.JSON(cmd.Context(), d.Out, result, selected, expression, d.IsTerminal())
		}
		var b strings.Builder
		width := 12
		if config.IsOrgBound(cfg.Token) {
			width = 13
		}
		row := func(key string, value any) { fmt.Fprintf(&b, "%-*s %s\n", width, key, output.Text(fmt.Sprint(value))) }
		switch kind {
		case config.ScopedToken:
			row("Credential", "scoped token")
			row("Host", cfg.Host)
			org := cfg.Org
			if org == "" {
				org = "(not set: pass --org org-... or set AVR_ORG)"
			} else if !strings.HasPrefix(org, "org-") {
				org += " (a slug: a scoped token needs the ID, org-...)"
			}
			row("Organization", org)
			row("Token", maskToken(cfg.Token, show))
			b.WriteString("\n" + d.paint("  A scoped token reaches only the repositories and VMs it names.", "2") + "\n" + d.paint("  It is not checked here: an expired or revoked one fails on first use.", "2") + "\n")
		case config.ServiceToken:
			expires, present := result["expires_at"]
			switch {
			case !present:
				expires = "-"
			case expires == nil:
				expires = "never"
			default:
				expires = formatTimestamp(fieldText(result, "expires_at"))
			}
			row("Credential", "service token")
			row("Name", fallback(result["name"], "-"))
			row("Token ID", fallback(result["api_key_id"], "-"))
			row("Organization", fallback(result["organization_id"], "-"))
			row("Group", fallback(result["group_id"], "-"))
			row("Expires", expires)
			row("Grants", tokenGrantsSummary(result))
			row("Host", cfg.Host)
			row("Token", maskToken(cfg.Token, show))
			b.WriteString("\n")
			if result["expiring_soon"] == true {
				b.WriteString(d.paint("  ! This service token expires soon: an organization admin can rotate it with \x60avr service-token rotate\x60; then update AVR_TOKEN.", "33") + "\n")
			}
			// The field stays set after the overlap ends; by then the replaced
			// secret is already refused and the advice no longer applies.
			if until := fieldText(result, "previous_valid_until"); timestampAfter(until, d.Now()) {
				b.WriteString(d.paint("  ! This token was rotated: the replaced secret stops working at "+formatTimestamp(until)+". If AVR_TOKEN still holds it, switch to the new secret.", "33") + "\n")
			}
			b.WriteString(d.paint("  A service token reaches only what its access names, in its own organization.", "2") + "\n")
		case config.PersonalKey:
			row("User ID", fallback(result["id"], "-"))
			row("Email", fallback(result["email"], "-"))
			name, exists := result["name"]
			if !exists {
				name = "N/A"
			} else if name == nil {
				name = "None"
			}
			row("Name", name)
			row("Host", cfg.Host)
			row("Default org", fallback(cfg.Org, "(not set)"))
			row("Created", fallback(result["created_at"], "-"))
			row("Token", maskToken(cfg.Token, show))
			b.WriteString("\n" + d.paint("  Switch host:        avr auth switch <host>", "2") + "\n" + d.paint("  Switch default org: avr config set org <slug>", "2") + "\n")
		}
		_, err = fmt.Fprint(d.Out, b.String())
		return err
	})
	return cmd
}

func fallback(value any, other string) any {
	if value == nil || value == "" {
		return other
	}
	return value
}
func maskToken(token string, show bool) string {
	if token == "" {
		return "(none)"
	}
	if show {
		return token
	}
	prefix := ""
	if len([]rune(token)) > 8 {
		prefix = string([]rune(token)[:4])
	}
	return prefix + strings.Repeat("*", 32)
}

func NewLogout(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	return &cobra.Command{Use: "logout", Short: "Revoke the current API key and remove stored credentials.", Args: cobra.NoArgs,
		RunE: run(func(cmd *cobra.Command, _ []string) error {
			cfg, store, api, err := d.load()
			if err != nil {
				return err
			}
			if config.IsService(cfg.Token) {
				return &ExitError{1, "Error: Logout does not revoke a service token and leaves stored credentials unchanged.\n  Hint: unset AVR_TOKEN to stop using the token. An organization admin can revoke it with `avr service-token revoke <token-id>`."}
			}
			if err := store.ValidateMutation(); err != nil {
				return err
			}
			if cfg.Token != "" {
				_, err = api.Do(cmd.Context(), http.MethodDelete, "/users/me/api-keys/current", nil)
				if err != nil {
					if cmd.Context().Err() != nil {
						return cmd.Context().Err()
					}
					var he *client.HTTPError
					if errors.As(err, &he) {
						_, _ = fmt.Fprintln(d.Err, "Warning: Could not revoke API key on server.")
					} else {
						_, _ = fmt.Fprintln(d.Err, "Warning: Could not reach server to revoke API key.")
					}
				}
			}
			cleared, err := store.Clear(cfg.Host)
			if err != nil {
				return err
			}
			message := "No stored credentials found."
			if cleared {
				message = d.paint("✓", "32") + " Logged out."
			}
			_, err = fmt.Fprintln(d.Out, message)
			return err
		})}
}

func newSwitch(d Dependencies) *cobra.Command {
	return &cobra.Command{Use: "switch [host]", Short: "Switch the default host used when AVR_HOST isn't set.", Args: cobra.MaximumNArgs(1),
		RunE: run(func(_ *cobra.Command, args []string) error {
			_, store, err := d.configuration()
			if err != nil {
				return err
			}
			h := store.Read()
			if len(h.Order) == 0 {
				return &ExitError{1, "No stored credentials. Run `avr auth device-login` or `avr auth login` first."}
			}
			if len(args) == 0 {
				if d.IsTerminal() {
					_, _ = fmt.Fprintln(d.Err, "Stored hosts:")
				}
				for _, host := range h.Order {
					if _, err = fmt.Fprintln(d.Out, output.Text(host)); err != nil {
						return err
					}
					if host == h.DefaultHost && d.IsTerminal() {
						_, _ = fmt.Fprintln(d.Err, d.errorPaint("  (default: "+output.Text(host)+")", "32"))
					}
				}
				_, err = fmt.Fprintf(d.Err, "\nPass one of these to switch, e.g. `avr auth switch %s`.\n", output.Text(h.Order[0]))
				return err
			}
			target := strings.TrimRight(args[0], "/")
			if _, ok := h.Entries[target]; !ok {
				var b strings.Builder
				fmt.Fprintf(&b, "Error: no stored credentials for host '%s'.\nAvailable hosts:", output.Text(target))
				for _, host := range h.Order {
					fmt.Fprintf(&b, "\n  %s", output.Text(host))
				}
				return &ExitError{1, b.String()}
			}
			if err := store.Switch(target); err != nil {
				return err
			}
			_, err = fmt.Fprintf(d.Out, "%s Default host set to %s\n", d.paint("✓", "32"), d.paint(output.Text(target), "1"))
			return err
		})}
}

// reauthenticationRequired reports the API's X-Reauthentication-Required
// refusal: the credential is too old for a change that outlives the session.
func reauthenticationRequired(he *client.HTTPError) bool {
	return he.Status == 403 && he.Reauthentication != ""
}

func apiError(cfg config.Config, err error, action string) error {
	var he *client.HTTPError
	if !errors.As(err, &he) {
		return err
	}
	if reauthenticationRequired(he) {
		detail := ""
		if he.Detail != "" {
			detail = ": " + output.CellText(he.Detail)
		}
		message := "Error: Avrea needs a recent sign-in to " + action + " (HTTP 403)" + detail +
			"\n  Hint: Sign in again with \x60avr auth device-login\x60 or \x60avr auth login\x60, then retry. If AVR_TOKEN is set, unset it first: it takes precedence over the new sign-in."
		if cfg.Host != config.DefaultHost {
			message += "\n  API: " + cfg.Host + he.Path
		}
		return &ExitError{4, message}
	}
	kind := config.Kind(cfg.Token)
	bound := config.IsOrgBound(cfg.Token)
	if he.Status == 401 && bound {
		subject := "The " + kind.String() + " was rejected"
		if cfg.Host != config.DefaultHost {
			subject = cfg.Host + " rejected the " + kind.String()
		}
		detail := "."
		if he.Detail != "" {
			detail = ": " + output.CellText(he.Detail)
		}
		hint := "ask a member of the organization to check the API's reason above. If a replacement is needed, create one with `avr token create` and set it as AVR_TOKEN."
		if kind == config.ServiceToken {
			hint = "an organization admin can check the token with `avr service-token view <token-id>`. A revoked or expired token cannot be rotated: create a new one with `avr service-token create`. A live token was rotated: use its new secret, or rotate it again with `avr service-token rotate <token-id>`. Then set the secret as AVR_TOKEN."
		}
		return &ExitError{4, "Error: " + subject + " (HTTP 401)" + detail + "\n  Hint: " + hint}
	}
	if he.Status == 401 {
		origin := ""
		if cfg.Host != config.DefaultHost {
			origin = fmt.Sprintf("Error: %s rejected your credentials (HTTP 401).\n", cfg.Host)
		}
		return &ExitError{4, origin + authHint}
	}
	detail := ""
	if he.Detail != "" {
		detail = ": " + output.CellText(he.Detail)
	}
	var message string
	switch he.Status {
	case 403:
		message = "You don't have access to " + action + " (HTTP 403)" + detail
		if !bound {
			message += "\n  Hint: run `avr org list` to see available orgs, or pass --org."
		}
	case 404:
		message = "Not found while trying to " + action + " (HTTP 404)" + detail
	case 409:
		message = "Conflict while trying to " + action + " (HTTP 409)" + detail
	case 422:
		message = "Failed to " + action + " (HTTP 422)" + detail
	case 429:
		message = "Avrea is rate-limiting requests (HTTP 429)" + detail
		if n, e := strconv.Atoi(he.RetryAfter); e == nil && n >= 0 {
			message += fmt.Sprintf("\n  Retry in %ds.", n)
		} else {
			message += "\n  Try again in a few seconds."
		}
	default:
		if he.Status >= 500 && he.Status < 600 {
			message = fmt.Sprintf("Avrea is having trouble (HTTP %d). Try again shortly — `avr health` shows status.", he.Status)
			if detail != "" {
				message += "\n  Detail" + detail
			}
		} else {
			message = fmt.Sprintf("Failed to %s (HTTP %d)%s", action, he.Status, detail)
		}
	}
	if he.Status == 403 || he.Status == 404 {
		switch kind {
		case config.ScopedToken:
			message += "\n  Hint: a scoped token reaches only the repositories and VMs it names, and only the operations open to tokens."
		case config.ServiceToken:
			message += "\n  Hint: a service token reaches only what its access names, and only the operations open to service tokens."
		case config.PersonalKey:
		}
	}
	if cfg.Host != config.DefaultHost {
		message += "\n  API: " + cfg.Host + he.Path
	}
	return errors.New(message)
}

// requirePerson refuses an organization-bound token before a command that acts
// as a signed-in person sends any request.
func requirePerson(cfg config.Config, subject, action string) error {
	if !config.IsOrgBound(cfg.Token) {
		return nil
	}
	kind := config.Kind(cfg.Token).String()
	return &ExitError{4, "Error: " + subject + " needs a signed-in account; " + kind + "s cannot " + action + ".\n  Hint: unset AVR_TOKEN if it contains the " + kind + ", then run avr auth device-login or avr auth login."}
}
