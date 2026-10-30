package commands

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var publicMirrorFields = []string{"available", "repository_id", "platform_repository_id", "repository_full_name", "https_clone_url", "default_branch", "platform_owner_id", "platform_owner_type", "platform_owner_login", "is_archived", "is_disabled", "is_fork", "platform_size_kb", "platform_pushed_at", "public_metadata_verified_at", "approval_state", "public_access_expires_at", "mirror_enabled", "installation_kind"}
var publicMirrorRequestFields = []string{"request_id", "repository_id", "repository_full_name", "status", "requester_organization_id", "requester_user_id", "reason", "github_snapshot", "created_at", "updated_at", "reviewed_at", "reviewed_by_user_id", "review_note", "approval_state", "public_access_expires_at"}

func newPublicMirrors(d Dependencies) *cobra.Command {
	c := &cobra.Command{Use: "public-mirror", Short: "Request and browse mirrors of public GitHub repositories."}
	c.AddCommand(newPublicMirrorCheck(d), newPublicMirrorRequest(d), newPublicMirrorRequests(d), newPublicMirrorView(d, false), newPublicMirrorView(d, true))
	return c
}
func publicMirrorLookupPath(fullName string) (string, error) {
	parts := strings.Split(fullName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return "", &ExitError{2, "Error: Invalid value for FULL_NAME: must be an owner/repository name"}
	}
	return "/public-mirrors/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]), nil
}
func publicMirrorRequestsPath(org string) string {
	return "/orgs/" + url.PathEscape(org) + "/public-mirrors/requests"
}
func newPublicMirrorCheck(d Dependencies) *cobra.Command {
	var o jsonOptions
	c := &cobra.Command{Use: "check FULL_NAME", Short: "Check whether a public GitHub repository mirror is available.", Args: positionalArguments("FULL_NAME")}
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, publicMirrorFields); done || err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		path, err := publicMirrorLookupPath(args[0])
		if err != nil {
			return err
		}
		mirror, err := api.PublicMirror(cmd.Context(), path)
		missing := false
		if err != nil {
			var he *client.HTTPError
			if !errors.As(err, &he) || he.Status != 404 {
				return apiError(cfg, err, "check public-mirror availability")
			}
			missing = true
		}
		record := map[string]any{"available": !missing}
		if missing {
			record["repository_full_name"] = args[0]
		} else {
			maps.Copy(record, mirror.Fields)
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record, publicMirrorFields)
		}
		if missing {
			if err = output.KeyValues(d.Out, [][2]string{{"Available", "no"}, {"Repository", args[0]}}); err != nil {
				return err
			}
			_, err = fmt.Fprintf(d.Err, "\nHint: request one with \x60avr repo public-mirror request %s\x60.\n", output.CellText(args[0]))
			return err
		}
		return output.KeyValues(d.Out, [][2]string{
			{"Available", "yes"}, {"Repository", pythonValue(record["repository_full_name"])}, {"Repository ID", pythonValue(record["repository_id"])},
			{"Default branch", pythonValue(fallback(record["default_branch"], "-"))}, {"Public until", formatTimestamp(fieldText(record, "public_access_expires_at"))},
		})
	})
	return c
}
func newPublicMirrorRequest(d Dependencies) *cobra.Command {
	var org, reason string
	var o jsonOptions
	c := &cobra.Command{Use: "request FULL_NAME", Short: "Request a mirror of a public GitHub repository.", Args: positionalArguments("FULL_NAME")}
	orgFlag(c, &org)
	o.flags(c)
	c.Flags().StringVar(&reason, "reason", "", "Why your organization needs this repository mirrored.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, publicMirrorRequestFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		body := map[string]any{"full_name": args[0]}
		if cmd.Flags().Changed("reason") {
			body["reason"] = reason
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		record, err := api.PublicMirrorRequest(cmd.Context(), http.MethodPost, publicMirrorRequestsPath(id), body, "")
		if err != nil {
			return apiError(cfg, err, "request a public mirror")
		}
		row := record.Fields
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, row, publicMirrorRequestFields)
		}
		return output.KeyValues(d.Out, [][2]string{
			{"Request ID", pythonValue(row["request_id"])}, {"Repository", pythonValue(row["repository_full_name"])}, {"Status", pythonValue(row["status"])},
			{"Organization", pythonValue(row["requester_organization_id"])}, {"Created", formatTimestamp(fieldText(row, "created_at"))},
		})
	})
	return c
}
func newPublicMirrorRequests(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "requests", Short: "List your organization's public-mirror requests.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, publicMirrorRequestFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		page, err := api.PublicMirrorRequests(cmd.Context(), publicMirrorRequestsPath(id))
		if err != nil {
			return apiError(cfg, err, "list public-mirror requests")
		}
		rows := rawRecords(page)
		if cmd.Flags().Changed("json") {
			return o.list(cmd, d, rows, publicMirrorRequestFields)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No data found.")
			return err
		}
		var cells [][]string
		for _, row := range rows {
			reviewed := "-"
			if value := fieldText(row, "reviewed_at"); value != "" {
				reviewed = formatTimestamp(value)
			}
			cells = append(cells, []string{organizationCell(row, "request_id"), organizationCell(row, "repository_full_name"), organizationCell(row, "status"), formatTimestamp(fieldText(row, "created_at")), reviewed})
		}
		return output.Grid(d.Out, []string{"Request ID", "Repository", "Status", "Created", "Reviewed"}, cells)
	})
	return c
}
func newPublicMirrorView(d Dependencies, cancel bool) *cobra.Command {
	var org string
	var yes bool
	var o jsonOptions
	verb, short, method, action := "view", "View one public-mirror request.", http.MethodGet, "get public-mirror request"
	if cancel {
		verb, short, method, action = "cancel", "Cancel one pending public-mirror request.", http.MethodDelete, "cancel public-mirror request"
	}
	c := &cobra.Command{Use: verb + " REQUEST_ID", Short: short, Args: positionalArguments("REQUEST_ID")}
	orgFlag(c, &org)
	o.flags(c)
	if cancel {
		c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	}
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, publicMirrorRequestFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if cancel && !yes {
			if err = confirmMutation(cmd.Context(), d, "public-mirror cancellation needs confirmation", "Cancel public-mirror request "+output.CellText(args[0])+"?"); err != nil {
				return err
			}
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		fallbackID := ""
		if cancel {
			fallbackID = args[0]
		}
		record, err := api.PublicMirrorRequest(cmd.Context(), method, publicMirrorRequestsPath(id)+"/"+url.PathEscape(args[0]), nil, fallbackID)
		if err != nil {
			return apiError(cfg, err, action)
		}
		row := record.Fields
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, row, publicMirrorRequestFields)
		}
		if cancel {
			suffix := ""
			if name := fieldText(row, "repository_full_name"); name != "" {
				suffix = " (" + name + ")"
			}
			_, err = fmt.Fprintf(d.Out, "Cancelled public-mirror request %s%s.\n", output.CellText(fieldText(row, "request_id")), output.CellText(suffix))
			return err
		}
		optional := func(key string) string { return pythonValue(fallback(row[key], "-")) }
		optionalTime := func(key string) string {
			if value := fieldText(row, key); value != "" {
				return formatTimestamp(value)
			}
			return "-"
		}
		return output.KeyValues(d.Out, [][2]string{
			{"Request ID", pythonValue(row["request_id"])}, {"Repository", pythonValue(row["repository_full_name"])}, {"Repository ID", pythonValue(row["repository_id"])},
			{"Status", pythonValue(row["status"])}, {"Reason", optional("reason")}, {"Created", formatTimestamp(fieldText(row, "created_at"))}, {"Updated", formatTimestamp(fieldText(row, "updated_at"))},
			{"Reviewed", optionalTime("reviewed_at")}, {"Review note", optional("review_note")}, {"Approval", optional("approval_state")}, {"Public until", optionalTime("public_access_expires_at")},
		})
	})
	return c
}
