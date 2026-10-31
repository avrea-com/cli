package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var tokenFields = []string{"id", "organization_id", "user_id", "name", "created_at", "expires_at", "last_used_at", "revoked_at", "revoked_reason", "allow_vm_create", "vm_create_limit", "vm_create_count", "grants"}
var tokenIDPattern = regexp.MustCompile("^key-[0-9a-f]{32}$")

const noTokenHint = "Run \x60avr token list\x60 to see the tokens visible to you."

// NewToken exposes scoped token creation, inspection and revocation.
func NewToken(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "token", Aliases: []string{"tokens"}, Short: "Create and manage scoped access tokens."}
	c.AddCommand(newTokenList(d), newTokenView(d), newTokenRevoke(d), newTokenCreate(d))
	return c
}
func tokenIDArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}
	if !tokenIDPattern.MatchString(args[0]) {
		return &ExitError{2, "Error: Expected a token ID: key- followed by 32 lowercase hexadecimal characters."}
	}
	return nil
}
func tokenContext(ctx context.Context, d Dependencies, org string) (config.Config, *client.Client, string, error) {
	cfg, _, api, err := d.load()
	if err != nil {
		return cfg, api, "", err
	}
	if err = authenticated(cfg); err != nil {
		return cfg, api, "", err
	}
	id, err := resolveOrg(ctx, d, cfg, api, org)
	return cfg, api, "/orgs/" + url.PathEscape(id) + "/access-tokens", err
}
func tokenError(cfg config.Config, err error, action string) error {
	result := apiError(cfg, err, action)
	var he *client.HTTPError
	if !errors.As(err, &he) || he.Status != 404 && he.Status != 422 {
		return result
	}
	first, rest, found := strings.Cut(result.Error(), "\n")
	if found {
		rest = "\n" + rest
	}
	return errors.New(first + "\n  Hint: " + noTokenHint + rest)
}
func newTokenList(d Dependencies) *cobra.Command {
	var org string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List live tokens, newest first.", Args: cobra.NoArgs}
	c.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	c.Flags().IntVarP(&limit, "limit", "L", 50, "Maximum tokens (1–1000).")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if done, err := o.prepare(cmd, d, tokenFields); done || err != nil {
			return err
		}
		cfg, api, path, err := tokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		rows := []map[string]any{}
		cursor := ""
		for {
			query := url.Values{"limit": {strconv.Itoa(min(limit-len(rows), 200))}}
			if cursor != "" {
				query.Set("cursor", cursor)
			}
			page, e := api.AccessTokens(cmd.Context(), path, query)
			if e != nil {
				return apiError(cfg, e, "list tokens")
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
			err = o.list(cmd, d, rows, tokenFields)
		} else {
			err = renderTokenList(d, rows)
		}
		if err != nil {
			return err
		}
		if more {
			_, err = fmt.Fprintf(d.Err, "\nShowing the newest %d tokens. Raise --limit to see the rest.\n", len(rows))
		}
		return err
	})
	return c
}
func newTokenView(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "view TOKEN_ID", Short: "Show a token's details and grants.", Args: tokenIDArgs}
	c.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, tokenFields); done || err != nil {
			return err
		}
		cfg, api, path, err := tokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.AccessToken(cmd.Context(), path+"/"+args[0])
		if err != nil {
			return tokenError(cfg, err, "fetch the token")
		}
		if cmd.Flags().Changed("json") {
			fields, e := output.Fields(o.fields, tokenFields)
			if e != nil {
				return e
			}
			return output.JSON(cmd.Context(), d.Out, record.Fields, fields, o.expression)
		}
		return renderToken(d, record.Fields)
	})
	return c
}
func newTokenRevoke(d Dependencies) *cobra.Command {
	var org string
	var yes bool
	c := &cobra.Command{Use: "revoke TOKEN_ID", Short: "Revoke a token.", Args: tokenIDArgs}
	c.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, path, err := tokenContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if !yes {
			if err = confirmMutation(cmd.Context(), d, "revoking a token", "Revoke token "+args[0]+"? Anything using it stops working."); err != nil {
				return err
			}
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		if _, err = api.Do(cmd.Context(), http.MethodDelete, path+"/"+args[0], nil); err != nil {
			return tokenError(cfg, err, "revoke the token")
		}
		_, err = fmt.Fprintf(d.Out, "Token %s revoked.\n", args[0])
		return err
	})
	return c
}
func tokenVMUsage(row map[string]any) string {
	if row["vm_create_count"] == nil {
		return "0"
	}
	return pythonValue(row["vm_create_count"])
}
func tokenGrantsSummary(row map[string]any) string {
	repos, vms, allVMs := 0, 0, false
	for _, grant := range objectList(row["grants"]) {
		switch fieldText(grant, "resource_type") {
		case "repository":
			repos++
		case "customer_vm":
			vms++
			if grant["resource_id"] == nil {
				allVMs = true
			}
		}
	}
	parts := []string{}
	if repos > 0 {
		label := "repo"
		if repos > 1 {
			label += "s"
		}
		parts = append(parts, fmt.Sprintf("%d %s", repos, label))
	}
	if allVMs {
		parts = append(parts, "all VMs")
	} else if vms > 0 {
		label := "VM"
		if vms > 1 {
			label += "s"
		}
		parts = append(parts, fmt.Sprintf("%d %s", vms, label))
	}
	if row["allow_vm_create"] == true {
		label := "VM create"
		if row["vm_create_limit"] != nil {
			label += " " + tokenVMUsage(row) + "/" + pythonValue(row["vm_create_limit"])
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}
func renderTokenList(d Dependencies, rows []map[string]any) error {
	piped := !d.IsTerminal()
	if !piped && len(rows) == 0 {
		_, err := fmt.Fprintln(d.Out, "No data found.")
		return err
	}
	owners := map[string]bool{}
	for _, row := range rows {
		owners[fieldText(row, "user_id")] = true
	}
	showOwner := piped || len(owners) > 1
	headers := []string{"id", "name", "expires_at", "last_used_at"}
	if !piped {
		headers = []string{"Token ID", "Name", "Expires", "Last used"}
	}
	if showOwner {
		if piped {
			headers = append(headers, "user_id")
		} else {
			headers = append(headers, "Owner")
		}
	}
	if piped {
		headers = append(headers, "grants")
	} else {
		headers = append(headers, "Grants")
	}
	var cells [][]string
	for _, row := range rows {
		expires, used := fieldText(row, "expires_at"), fieldText(row, "last_used_at")
		if !piped {
			expires = formatTimestamp(expires)
			if used == "" {
				used = "never"
			} else {
				used = relativeAge(used, d.Now())
			}
		}
		values := []string{fieldText(row, "id"), fieldText(row, "name"), expires, used}
		if showOwner {
			values = append(values, fieldText(row, "user_id"))
		}
		values = append(values, tokenGrantsSummary(row))
		cells = append(cells, values)
	}
	if piped {
		return output.Table(d.Out, headers, cells, true)
	}
	return output.Grid(d.Out, headers, cells)
}
func tokenStatus(row map[string]any, now time.Time) string {
	if value := fieldText(row, "revoked_at"); value != "" {
		status := "revoked " + formatTimestamp(value)
		if reason := fieldText(row, "revoked_reason"); reason != "" {
			status += " (" + output.CellText(reason) + ")"
		}
		return status
	}
	value := fieldText(row, "expires_at")
	expiry, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		expiry, err = time.Parse("2006-01-02T15:04:05", value)
	}
	if err == nil && !expiry.After(now) {
		return "expired"
	}
	return "active"
}
func renderToken(d Dependencies, row map[string]any) error {
	used := "never"
	if value := fieldText(row, "last_used_at"); value != "" {
		used = formatTimestamp(value)
	}
	vm := "not allowed"
	if row["allow_vm_create"] == true {
		vm = "allowed, " + tokenVMUsage(row)
		if row["vm_create_limit"] != nil {
			vm += " of " + pythonValue(row["vm_create_limit"])
		}
		vm += " used"
	}
	values := [][2]string{
		{"Token ID", pythonValue(row["id"])}, {"Name", fieldText(row, "name")}, {"Owner", pythonValue(row["user_id"])},
		{"Status", tokenStatus(row, d.Now())}, {"Created", formatTimestamp(fieldText(row, "created_at"))},
		{"Expires", formatTimestamp(fieldText(row, "expires_at"))}, {"Last used", used}, {"VM create", vm},
	}
	for _, pair := range values {
		if _, err := fmt.Fprintf(d.Out, "%-11s%s\n", pair[0], output.CellText(pair[1])); err != nil {
			return err
		}
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
		if kind == "customer_vm" {
			kind = "VM"
		}
		resource := fieldText(grant, "resource_id")
		if resource == "" {
			resource = "every VM the owner can reach"
		}
		cells = append(cells, []string{kind, resource, pythonValue(grant["access_level"])})
	}
	return output.Grid(d.Out, []string{"Type", "Resource", "Level"}, cells)
}
