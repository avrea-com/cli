package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var gitLabRunnerFields = []string{"machine_type", "tag", "architecture", "cpu_count", "memory_mb", "disk_gb", "description", "run_untagged", "paused", "configured", "status", "runner_id", "token_expires_at", "verified_at", "invalid_at"}
var gitLabCollectionFields = []string{"data", "setup"}

func newGitLabRunner(d Dependencies) *cobra.Command {
	c := &cobra.Command{Use: "runner", Short: "Manage GitLab runners, one per Linux machine type."}
	for _, verb := range []string{"list", "set", "delete", "create"} {
		c.AddCommand(newGitLabRunnerCommand(d, verb))
	}
	return c
}
func newGitLabRunnerCommand(d Dependencies, verb string) *cobra.Command {
	var org, id string
	var stdin, yes, protected bool
	var types []string
	var o jsonOptions
	c := &cobra.Command{Use: verb, Short: verb + " GitLab runners.", Args: cobra.NoArgs}
	fields := gitLabCollectionFields
	if verb == "set" || verb == "delete" {
		c.Use += " <machine-type>"
		c.Args = cobra.ExactArgs(1)
	}
	if verb == "set" {
		fields = gitLabRunnerFields
		c.Flags().BoolVar(&stdin, "runner-token-stdin", false, "Read the runner authentication token from stdin.")
	}
	if verb == "delete" {
		fields = []string{"installation_id", "machine_type", "deleted"}
		c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt.")
	}
	if verb == "create" {
		c.Flags().StringArrayVar(&types, "machine-type", nil, "Machine type to create (repeatable).")
		c.Flags().BoolVar(&protected, "protected", false, "Restrict runners to protected branches and tags.")
		c.Flags().BoolVar(&stdin, "personal-access-token-stdin", false, "Read your personal access token from stdin.")
	}
	orgFlag(c, &org)
	c.Flags().StringVar(&id, "installation-id", "", "GitLab installation ID.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		for _, flag := range []string{"installation-id", "machine-type"} {
			if (flag == "installation-id" || verb == "create") && !cmd.Flags().Changed(flag) {
				return &ExitError{2, "Error: Missing option '--" + flag + "'."}
			}
		}
		if done, e := o.prepare(cmd, d, fields); done || e != nil {
			return e
		}
		cfg, api, orgID, e := organizationContext(cmd.Context(), d, org)
		if e != nil {
			return e
		}
		path := gitLabPath(orgID, id) + "/runners"
		switch verb {
		case "list":
			row, e := api.Get(cmd.Context(), path, nil)
			if e != nil {
				return gitLabAPIError(cfg, e, "list GitLab runners", "")
			}
			if cmd.Flags().Changed("json") {
				return o.record(cmd, d, row, fields)
			}
			return renderGitLabRunners(d, row)
		case "set":
			token, e := gitLabSecret(cmd, d, stdin, "GitLab runner authentication token", "GitLab runner authentication token requires a hidden prompt or --runner-token-stdin")
			if e != nil {
				return e
			}
			row, e := gitLabWrite(cmd.Context(), api, http.MethodPut, path+"/"+url.PathEscape(args[0]), map[string]any{"runner_token": token})
			if e != nil {
				return gitLabAPIError(cfg, e, "configure GitLab runner", "")
			}
			if cmd.Flags().Changed("json") {
				return o.record(cmd, d, row, fields)
			}
			return gitLabKeyValues(d, "Runner configured", [][2]string{{"Machine type", pythonValue(row["machine_type"])}, {"Tag", pythonValue(row["tag"])}, {"Status", pythonValue(row["status"])}})
		case "delete":
			if !yes {
				if e = confirmMutation(cmd.Context(), d, "runner removal requires confirmation; use --yes for scripts", fmt.Sprintf("Remove %s runner configuration from %s?", output.CellText(args[0]), output.CellText(id))); e != nil {
					return e
				}
			}
			if _, e = api.Raw(cmd.Context(), http.MethodDelete, path+"/"+url.PathEscape(args[0]), nil); e != nil {
				return gitLabAPIError(cfg, e, "remove GitLab runner", "")
			}
			if cmd.Flags().Changed("json") {
				return o.record(cmd, d, map[string]any{"installation_id": id, "machine_type": args[0], "deleted": true}, fields)
			}
			_, e = fmt.Fprintf(d.Out, "Runner configuration removed for %s. Remove its GitLab runner separately if no longer needed.\n", output.CellText(args[0]))
			return e
		case "create":
			collection, e := api.Get(cmd.Context(), path, nil)
			if e != nil {
				return gitLabAPIError(cfg, e, "read GitLab runner setup", "")
			}
			available := map[string]map[string]any{}
			for _, row := range gitLabRows(collection["data"]) {
				available[fieldText(row, "machine_type")] = row
			}
			for _, machine := range types {
				row, ok := available[machine]
				if !ok {
					return &ExitError{2, "Error: Unknown runner machine type: " + output.CellText(machine)}
				}
				if row["configured"] == true {
					return &ExitError{2, "Error: Machine type " + output.CellText(machine) + " is already configured; use 'runner set' to replace it."}
				}
			}
			token, e := gitLabSecret(cmd, d, stdin, "GitLab personal access token (create_runner scope)", "GitLab personal access token (create_runner scope) requires a hidden prompt or --personal-access-token-stdin")
			if e != nil {
				return e
			}
			setup, _ := collection["setup"].(map[string]any)
			configured := []map[string]any{}
			done := map[string]bool{}
			for _, machine := range types {
				if done[machine] {
					continue
				}
				done[machine] = true
				row := available[machine]
				level := "not_protected"
				if protected {
					level = "ref_protected"
				}
				body := map[string]any{"runner_type": "group_type", "group_id": setup["group_id"], "tag_list": row["tag"], "description": row["description"], "run_untagged": row["run_untagged"], "paused": row["paused"], "access_level": level}
				rid, runnerToken, e := api.CreateGitLabRunner(cmd.Context(), token, body)
				var result map[string]any
				code := 1
				if e == nil {
					result, e = gitLabWrite(cmd.Context(), api, http.MethodPut, path+"/"+url.PathEscape(machine), map[string]any{"runner_token": runnerToken})
					if e != nil {
						if errors.Is(e, context.Canceled) {
							code = 130
							e = errors.New("Interrupted. " + gitLabRunnerUnconfirmed(rid, machine))
						} else {
							e = gitLabRunnerPutError(e, rid, machine)
						}
					}
				}
				if e != nil {
					if errors.Is(e, context.Canceled) {
						return e
					}
					message := e.Error()
					if len(configured) > 0 && cmd.Flags().Changed("json") {
						names := []string{}
						for _, row := range configured {
							names = append(names, fieldText(row, "machine_type"))
						}
						message += " Already configured before this failure: " + strings.Join(names, ", ") + "."
					}
					return &ExitError{code, "Error: " + output.CellText(message)}
				}
				configured = append(configured, result)
				if !cmd.Flags().Changed("json") {
					if _, e = fmt.Fprintf(d.Out, "Configured %s with GitLab runner %s.\n", output.CellText(machine), rid); e != nil {
						return &ExitError{1, "Error: " + gitLabRunnerUnconfirmed(rid, machine)}
					}
				}
			}
			if cmd.Flags().Changed("json") {
				return o.record(cmd, d, map[string]any{"data": configured, "setup": setup}, fields)
			}
		}
		return nil
	})
	return c
}
func gitLabRunnerUnconfirmed(id, machine string) string {
	return fmt.Sprintf("GitLab runner %s was created for %s. Avrea configuration could not be confirmed. Check Avrea before retrying; if unconfigured, reset its token in GitLab and use 'runner set'.", id, machine)
}
func gitLabRunnerPutError(err error, id, machine string) error {
	var he *client.HTTPError
	if !errors.As(err, &he) || he.Status >= 500 {
		return errors.New(gitLabRunnerUnconfirmed(id, machine))
	}
	suffix := "."
	if he.Detail != "" {
		suffix = ": " + he.Detail
	}
	lead := fmt.Sprintf("Avrea rejected the runner configuration for %s (HTTP %d)%s", machine, he.Status, suffix)
	if he.Status == 403 {
		lead = "You don't have permission to configure GitLab runners for this installation in Avrea (HTTP 403)" + suffix
	}
	return errors.New(lead + " GitLab runner " + id + " exists but is not configured in Avrea: remove it in GitLab, or reset its token there and use 'runner set'.")
}
func renderGitLabRunners(d Dependencies, collection map[string]any) error {
	rows := gitLabRows(collection["data"])
	cells := [][]string{}
	for _, row := range rows {
		values := []string{}
		for _, key := range []string{"machine_type", "tag", "status", "runner_id", "token_expires_at", "verified_at"} {
			value := "N/A"
			if v, ok := row[key]; ok {
				value = pythonValue(v)
			}
			values = append(values, value)
		}
		cells = append(cells, values)
	}
	if len(cells) == 0 {
		if _, e := fmt.Fprintln(d.Out, "No data found."); e != nil {
			return e
		}
	} else if e := output.Grid(d.Out, []string{"Machine type", "Tag", "Status", "Runner ID", "Token expiry", "Last verified"}, cells); e != nil {
		return e
	}
	setup, _ := collection["setup"].(map[string]any)
	if _, e := fmt.Fprintf(d.Out, "\nCreate group runners in %s itself, one per required machine type.\nDedicate these runners to Avrea. Do not register their tokens on your own runner managers.\nUse exactly the listed tag on each runner and exactly one tag on each CI job.\n", output.CellText(pythonValue(setup["group_full_path"]))); e != nil {
		return e
	}
	for _, row := range rows {
		if _, e := fmt.Fprintf(d.Out, "  %s: description=%s; run_untagged=%s; paused=%s\n", output.CellText(pythonValue(row["machine_type"])), output.CellText(pythonValue(row["description"])), pythonValue(row["run_untagged"]), pythonValue(row["paused"])); e != nil {
			return e
		}
	}
	_, e := fmt.Fprintf(d.Out, "%s\n%s\n\n.gitlab-ci.yml example:\n%s\nDocumentation: %s\n", pythonValue(setup["protected_branches"]), pythonValue(setup["fallback_image"]), pythonValue(setup["job_example"]), pythonValue(setup["documentation_url"]))
	return e
}
