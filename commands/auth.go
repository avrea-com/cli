package commands

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var statusFields = []string{"email", "name", "created_at", "user_id", "host", "default_org", "token", "credential_type"}

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
		hasJSON := cmd.Flags().Changed("json")
		if expression != "" && !hasJSON {
			return &ExitError{2, "Error: --jq requires --json"}
		}
		if hasJSON {
			if fields == "?" {
				return output.Discovery(d.Out, statusFields)
			}
			if _, err := output.Fields(fields, statusFields); err != nil {
				return err
			}
		}
		if err := authenticated(cfg); err != nil {
			return err
		}
		scoped := config.IsScoped(cfg.Token)
		result := map[string]any{}
		if !scoped {
			result, err = d.apiClient(cfg).Do(cmd.Context(), http.MethodGet, "/users/me", nil)
			if err != nil {
				return apiError(cfg, err, "fetch user info")
			}
		}
		if hasJSON {
			schema := make([]string, 0, len(statusFields))
			for _, f := range statusFields {
				if f != "token" || show {
					schema = append(schema, f)
				}
			}
			selected, err := output.Fields(fields, schema)
			if err != nil {
				return err
			}
			kind := "api_key"
			if scoped {
				kind = "scoped_token"
			}
			result["user_id"] = result["id"]
			result["host"] = cfg.Host
			result["default_org"] = nil
			if cfg.Org != "" {
				result["default_org"] = cfg.Org
			}
			result["credential_type"] = kind
			delete(result, "token")
			if show {
				result["token"] = cfg.Token
			}
			return output.JSON(cmd.Context(), d.Out, result, selected, expression)
		}
		var b strings.Builder
		width := 12
		if scoped {
			width = 13
		}
		row := func(key string, value any) { fmt.Fprintf(&b, "%-*s %s\n", width, key, output.Text(fmt.Sprint(value))) }
		if scoped {
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
		} else {
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
				return &ExitError{1, "No stored credentials. Run `avr auth login` first."}
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

func apiError(cfg config.Config, err error, action string) error {
	var he *client.HTTPError
	if !errors.As(err, &he) {
		return err
	}
	scoped := config.IsScoped(cfg.Token)
	if he.Status == 401 && scoped {
		subject := "The scoped token was rejected"
		if cfg.Host != config.DefaultHost {
			subject = cfg.Host + " rejected the scoped token"
		}
		detail := "."
		if he.Detail != "" {
			detail = ": " + output.CellText(he.Detail)
		}
		return &ExitError{4, "Error: " + subject + " (HTTP 401)" + detail + "\n  Hint: ask a member of the organization to check the API's reason above. If a replacement is needed, create one with `avr token create` and set it as AVR_TOKEN."}
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
		if !scoped {
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
	if scoped && (he.Status == 403 || he.Status == 404) {
		message += "\n  Hint: a scoped token reaches only the repositories and VMs it names, and only the operations open to tokens."
	}
	if cfg.Host != config.DefaultHost {
		message += "\n  API: " + cfg.Host + he.Path
	}
	return errors.New(message)
}
