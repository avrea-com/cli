package commands

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var builderSessionFields = []string{"builder_session_id", "platform", "state", "state_reason", "cpu_count", "memory_mb", "disk_gb", "repository_full_name", "stop_at", "created_at", "updated_at"}

var builderSessionStates = []string{"PENDING", "STARTING", "RUNNING", "STOPPING", "STOPPED", "FAILED"}

// NewBuilder manages the sessions behind `avr build`. Nothing stops an idle
// session on the server, so a client that dies without deleting its session
// holds it, and its quota, until the TTL.
func NewBuilder(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "builder", Aliases: []string{"builders"}, Short: "Manage remote builder sessions."}
	c.AddCommand(newBuilderList(d), newBuilderStop(d))
	return c
}

func builderBase(org string) string { return "/orgs/" + url.PathEscape(org) + "/builder-sessions" }

func builderStatus(err error) int {
	var he *client.HTTPError
	if errors.As(err, &he) {
		return he.Status
	}
	return 0
}

func newBuilderList(d Dependencies) *cobra.Command {
	var org, state, cursor string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List builder sessions, newest first.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().StringVar(&state, "state", "", "Filter by state.")
	c.Flags().IntVarP(&limit, "limit", "L", 50, "Max sessions (1–100).")
	c.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous response.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if cmd.Flags().Changed("state") {
			state = strings.ToUpper(state)
			if err := validateChoice("state", state, builderSessionStates); err != nil {
				return err
			}
		}
		if limit < 1 || limit > 100 {
			return &ExitError{2, "Error: --limit must be between 1 and 100."}
		}
		if err := validateCursor(cmd, cursor); err != nil {
			return err
		}
		if done, err := o.prepare(cmd, d, builderSessionFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		if state != "" {
			query.Set("state", state)
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		page, err := api.BuilderSessions(cmd.Context(), builderBase(id), query)
		if err != nil {
			return apiError(cfg, err, "list builder sessions")
		}
		asJSON := cmd.Flags().Changed("json")
		if asJSON {
			err = o.list(cmd, d, page.Items, builderSessionFields)
		} else {
			err = renderBuilderList(d, page.Items)
		}
		if err != nil {
			return err
		}
		return printNextCursor(d, page.NextCursor, asJSON)
	})
	return c
}

func renderBuilderList(d Dependencies, rows []map[string]any) error {
	piped := !d.IsTerminal()
	if !piped && len(rows) == 0 {
		_, err := fmt.Fprintln(d.Out, "No builder sessions found.")
		return err
	}
	keys := []string{"builder_session_id", "platform", "state", "repository_full_name", "stop_at", "created_at"}
	if piped {
		keys = []string{"builder_session_id", "platform", "state", "state_reason", "repository_full_name", "stop_at", "created_at"}
	}
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		values := make([]string, 0, len(keys))
		for _, key := range keys {
			values = append(values, fieldText(row, key))
		}
		cells = append(cells, values)
	}
	if piped {
		return output.Table(d.Out, keys, cells, true)
	}
	return output.Grid(d.Out, []string{"ID", "Platform", "State", "Repo", "Stop at", "Created"}, cells)
}

func newBuilderStop(d Dependencies) *cobra.Command {
	var org string
	var yes, asJSON bool
	c := &cobra.Command{Use: "stop SESSION_ID", Short: "Stop a builder session, reclaiming its VM and quota slot.", Args: positionalArguments("SESSION_ID")}
	orgFlag(c, &org)
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	c.Flags().BoolVar(&asJSON, "json", false, "Emit the raw API response as JSON.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		id := args[0]
		if !yes {
			if err = confirmMutation(cmd.Context(), d, "stopping a builder session", "Stop builder session "+output.CellText(id)+"?"); err != nil {
				return err
			}
		}
		response, err := api.DeleteBuilderSession(cmd.Context(), builderBase(orgID)+"/"+url.PathEscape(id))
		if err != nil {
			result := apiError(cfg, err, "stop builder session")
			switch status := builderStatus(err); status {
			case 409:
				// The hosting node is in maintenance: the stop was not
				// accepted, but nothing is stuck.
				return withHint(result, "safe to retry; the session's TTL will also reclaim it.")
			case 404, 422:
				return withHint(result, "Run `avr builder list` to see your sessions.")
			}
			return result
		}
		if asJSON {
			return rawJSON(d, response)
		}
		data, _ := response["data"].(map[string]any)
		_, err = fmt.Fprintf(d.Out, "Builder session %s is now %s.\n", output.CellText(id), output.CellText(billingText(data, "state", "STOPPING")))
		return err
	})
	return c
}
