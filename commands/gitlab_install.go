package commands

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var gitLabInstallationFields = []string{"installation_id", "organization_id", "gitlab_namespace_id", "gitlab_namespace_full_path", "target_type", "state", "credential_kind", "access_token_expires_on", "access_token_last_validated_at", "access_token_invalid_at", "webhook_secret_rotated_at", "configured_runner_count", "degraded_runner_count", "project_selection_mode", "selected_project_count", "gitlab_project_ids", "created_at", "updated_at"}
var gitLabWriteFields = append(slices.Clone(gitLabInstallationFields), "webhook_setup")

const gitLabNotEnabled = "GitLab is not enabled for this organization, the installation belongs to another organization, or the ID is not a GitLab installation. Check it with 'avr org install list'."

func gitLabRows(value any) []map[string]any {
	rows := []map[string]any{}
	items, _ := value.([]any)
	for _, item := range items {
		if row, ok := item.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows
}
func validateGitLabSelectionArgs(selection string, ids []int) error {
	if selection != "" && !strings.EqualFold(selection, "all") && !strings.EqualFold(selection, "selected") {
		return &ExitError{2, "Error: Invalid value for '--project-selection': choose all or selected."}
	}
	for _, id := range ids {
		if id < 1 {
			return &ExitError{2, "Error: Invalid value for '--project-id': must be at least 1."}
		}
	}
	return nil
}
func gitLabSelection(selection string, ids []int) (map[string]any, error) {
	if selection == "" {
		return nil, &ExitError{2, "Error: --project-selection is required"}
	}
	if len(ids) > 1000 {
		return nil, &ExitError{2, "Error: --project-id may be supplied at most 1,000 times"}
	}
	if strings.EqualFold(selection, "all") && len(ids) > 0 {
		return nil, &ExitError{2, "Error: --project-id cannot be used with --project-selection all"}
	}
	if ids == nil {
		ids = []int{}
	}
	return map[string]any{"project_selection_mode": strings.ToUpper(selection), "gitlab_project_ids": ids}, nil
}
func gitLabSecret(cmd *cobra.Command, d Dependencies, stdin bool, label, hint string) (string, error) {
	reader := bufio.NewReader(d.In)
	value := ""
	if stdin {
		type result struct {
			value string
			err   error
		}
		ch := make(chan result, 1)
		go func() { v, e := reader.ReadString('\n'); ch <- result{v, e} }()
		select {
		case <-cmd.Context().Done():
			return "", cmd.Context().Err()
		case got := <-ch:
			if got.err != nil && !errors.Is(got.err, io.EOF) {
				return "", errors.New("could not read credential from stdin")
			}
			value = got.value
		}
	} else {
		if d.Getenv("AVR_PROMPT_DISABLED") != "" {
			return "", &ExitError{1, "Error: " + hint + ", but AVR_PROMPT_DISABLED is set.\nAborted!"}
		}
		for value == "" {
			v, e := cardPrompt(cmd.Context(), d, reader, label, true)
			if e != nil {
				return "", e
			}
			value = v
		}
	}
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > 4096 {
		return "", &ExitError{2, "Error: " + label + " must contain 1 to 4096 characters"}
	}
	return value, nil
}
func gitLabAPIError(cfg config.Config, err error, action, hint string) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	var transport *client.TransportError
	if errors.As(err, &transport) {
		lead := "Could not reach Avrea"
		if strings.Contains(transport.Reason, "Timeout") {
			lead = "Timed out"
		}
		return &ExitError{1, "Error: " + lead + " while trying to " + action + ". Try again shortly."}
	}
	result := apiError(cfg, err, action)
	var he *client.HTTPError
	if hint != "" && errors.As(err, &he) && he.Status == 404 {
		first, rest, ok := strings.Cut(result.Error(), "\n")
		if ok {
			rest = "\n" + rest
		}
		return errors.New(first + "\n  Hint: " + hint + rest)
	}
	return result
}

// GitLab validation can use the server's full 60-second deadline. Copy the
// client so this allowance cannot change other requests or admin embedding.
func gitLabWrite(ctx context.Context, api *client.Client, method, path string, body any) (map[string]any, error) {
	copyClient := *api
	transport := *api.HTTP
	transport.Timeout = 70 * time.Second
	copyClient.HTTP = &transport
	return copyClient.Do(ctx, method, path, body)
}
func gitLabPath(org, id string) string {
	return "/orgs/" + url.PathEscape(org) + "/gitlab-installations/" + url.PathEscape(id)
}
func gitLabWriteProjection(cmd *cobra.Command, o jsonOptions) ([]string, error) {
	if !cmd.Flags().Changed("json") {
		return nil, nil
	}
	fields, e := output.Fields(o.fields, gitLabWriteFields)
	if e != nil {
		return nil, e
	}
	if !slices.Contains(fields, "webhook_setup") {
		return nil, &ExitError{2, "Error: --json must include webhook_setup: it carries the webhook secret, which is shown only once."}
	}
	return fields, nil
}
func gitLabCommitted(cmd *cobra.Command, d Dependencies, row map[string]any, fields []string, expression, committed string) error {
	setup, _ := row["webhook_setup"].(map[string]any)
	secret := fieldText(setup, "secret")
	var buffer bytes.Buffer
	err := output.JSON(cmd.Context(), &buffer, row, fields, expression)
	retained := err != nil || secret == "" || strings.Contains(buffer.String(), secret)
	if err == nil && !retained {
		err = errors.New("filter discarded credential")
	}
	if err == nil {
		_, err = io.Copy(d.Out, &buffer)
	}
	if err == nil {
		err = flushTokenOutput(d.Out)
	}
	if err == nil {
		err = cmd.Context().Err()
	}
	if err == nil {
		return nil
	}
	message := committed + ", but output failed."
	if !retained {
		message = committed + ", but output failed: " + tokenFilterRetain
	}
	recovery := output.JSON(context.Background(), d.Out, row, fields, "")
	if recovery == nil {
		recovery = flushTokenOutput(d.Out)
	}
	if recovery == nil {
		message += " stdout contains its unfiltered JSON; save the webhook secret rather than retrying."
	} else {
		message += " Nothing could be written to stdout; the webhook secret is lost."
	}
	return &ExitError{1, "Error: " + message}
}
func gitLabKeyValues(d Dependencies, title string, pairs [][2]string) error {
	if title != "" {
		if _, e := fmt.Fprintf(d.Out, "%s\n%s\n", title, strings.Repeat("-", utf8.RuneCountInString(title))); e != nil {
			return e
		}
	}
	return output.KeyValues(d.Out, pairs)
}
func gitLabWriteResult(cmd *cobra.Command, d Dependencies, row map[string]any, fields []string, o jsonOptions, title, committed string) error {
	if fields != nil {
		return gitLabCommitted(cmd, d, row, fields, o.expression, committed)
	}
	pairs := [][2]string{}
	for _, p := range [][2]string{{"Installation ID", "installation_id"}, {"GitLab group", "gitlab_namespace_full_path"}, {"Token expires", "access_token_expires_on"}, {"Project selection", "project_selection_mode"}, {"Selected project count", "selected_project_count"}} {
		pairs = append(pairs, [2]string{p[0], pythonValue(row[p[1]])})
	}
	if e := gitLabKeyValues(d, title, pairs); e != nil {
		return e
	}
	if _, e := fmt.Fprintln(d.Out, "\nConfigure the GitLab group webhook with these exact settings:"); e != nil {
		return e
	}
	setup, _ := row["webhook_setup"].(map[string]any)
	triggers := []string{}
	if items, ok := setup["enabled_triggers"].([]any); ok {
		for _, item := range items {
			triggers = append(triggers, pythonValue(item))
		}
	}
	enabled := func(value any) string {
		if value == true {
			return "enabled"
		}
		return "disabled"
	}
	if e := gitLabKeyValues(d, "", [][2]string{{"URL", pythonValue(setup["url"])}, {"Secret header", pythonValue(setup["secret_header"])}, {"Secret (shown once)", pythonValue(setup["secret"])}, {"Enabled triggers", strings.Join(triggers, ", ")}, {"SSL verification", enabled(setup["ssl_verification"])}, {"Unlisted triggers", enabled(setup["unlisted_triggers_enabled"])}}); e != nil {
		return e
	}
	_, e := fmt.Fprintf(d.Err, "Save the webhook secret now: Avrea cannot show it again.\n%s\n", output.CellText(pythonValue(setup["warning"])))
	return e
}
func addGitLabInstallation(cmd *cobra.Command, d Dependencies, cfg config.Config, api *client.Client, org, group, selection string, ids []int, stdin bool, o jsonOptions) error {
	if strings.TrimSpace(group) == "" {
		return &ExitError{2, "Error: --group-full-path is required with --platform gitlab"}
	}
	if selection == "" {
		return &ExitError{2, "Error: --project-selection is required with --platform gitlab"}
	}
	incompatible := []string{}
	if value, _ := cmd.Flags().GetBool("no-browser"); value {
		incompatible = append(incompatible, "--no-browser")
	}
	if cmd.Flags().Changed("wait-seconds") {
		incompatible = append(incompatible, "--wait-seconds")
	}
	if len(incompatible) > 0 {
		return &ExitError{2, "Error: " + strings.Join(incompatible, ", ") + " cannot be used with --platform gitlab"}
	}
	body, e := gitLabSelection(selection, ids)
	if e != nil {
		return e
	}
	orgID, e := resolveOrg(cmd.Context(), d, cfg, api, org)
	if e != nil {
		return e
	}
	fields, e := gitLabWriteProjection(cmd, o)
	if e != nil {
		return e
	}
	token, e := gitLabSecret(cmd, d, stdin, "GitLab group access token", "GitLab access token input requires a hidden prompt or --access-token-stdin")
	if e != nil {
		return e
	}
	body["group_full_path"] = strings.TrimSpace(group)
	body["access_token"] = token
	row, e := gitLabWrite(cmd.Context(), api, http.MethodPost, "/orgs/"+url.PathEscape(orgID)+"/gitlab-installations", body)
	if e != nil {
		return gitLabAPIError(cfg, e, "register GitLab installation", "GitLab is not enabled for this organization.")
	}
	return gitLabWriteResult(cmd, d, row, fields, o, "GitLab installation configured", "The GitLab installation was registered")
}
func newGitLabSelection(d Dependencies) *cobra.Command {
	var org, id, selection string
	var ids []int
	var o jsonOptions
	c := &cobra.Command{Use: "set-selection", Short: "Replace a GitLab installation's project selection.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().StringVar(&id, "installation-id", "", "GitLab installation ID.")
	c.Flags().StringVar(&selection, "project-selection", "", "Project selection mode.")
	c.Flags().IntSliceVar(&ids, "project-id", nil, "Project ID (repeatable).")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		for _, flag := range []string{"installation-id", "project-selection"} {
			if !cmd.Flags().Changed(flag) {
				return &ExitError{2, "Error: Missing option '--" + flag + "'."}
			}
		}
		if e := validateGitLabSelectionArgs(selection, ids); e != nil {
			return e
		}
		if done, e := o.prepare(cmd, d, gitLabInstallationFields); done || e != nil {
			return e
		}
		cfg, _, api, e := d.load()
		if e != nil {
			return e
		}
		if e = authenticated(cfg); e != nil {
			return e
		}
		body, e := gitLabSelection(selection, ids)
		if e != nil {
			return e
		}
		orgID, e := resolveOrg(cmd.Context(), d, cfg, api, org)
		if e != nil {
			return e
		}
		row, e := gitLabWrite(cmd.Context(), api, http.MethodPatch, gitLabPath(orgID, id), body)
		if e != nil {
			return gitLabAPIError(cfg, e, "update GitLab project selection", gitLabNotEnabled)
		}
		if len(row) == 0 {
			return &ExitError{1, "Error: GitLab project selection update returned no installation details"}
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, row, gitLabInstallationFields)
		}
		projects := []string{}
		items, _ := row["gitlab_project_ids"].([]any)
		for _, v := range items {
			projects = append(projects, pythonValue(v))
		}
		text := strings.Join(projects, ", ")
		if text == "" {
			text = "none"
		}
		return gitLabKeyValues(d, "GitLab project selection updated", [][2]string{{"Installation ID", pythonValue(row["installation_id"])}, {"GitLab group", pythonValue(row["gitlab_namespace_full_path"])}, {"Project selection", pythonValue(row["project_selection_mode"])}, {"Selected project count", pythonValue(row["selected_project_count"])}, {"GitLab project IDs", text}})
	})
	return c
}
func newGitLabRotate(d Dependencies) *cobra.Command {
	var org, id string
	var stdin bool
	var o jsonOptions
	c := &cobra.Command{Use: "rotate", Short: "Rotate a GitLab installation's access token and webhook secret.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().StringVar(&id, "installation-id", "", "GitLab installation ID.")
	c.Flags().BoolVar(&stdin, "access-token-stdin", false, "Read the group access token from stdin.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if !cmd.Flags().Changed("installation-id") {
			return &ExitError{2, "Error: Missing option '--installation-id'."}
		}
		if done, e := o.prepare(cmd, d, gitLabWriteFields); done || e != nil {
			return e
		}
		cfg, api, orgID, e := organizationContext(cmd.Context(), d, org)
		if e != nil {
			return e
		}
		fields, e := gitLabWriteProjection(cmd, o)
		if e != nil {
			return e
		}
		token, e := gitLabSecret(cmd, d, stdin, "GitLab group access token", "GitLab access token input requires a hidden prompt or --access-token-stdin")
		if e != nil {
			return e
		}
		row, e := gitLabWrite(cmd.Context(), api, http.MethodPost, gitLabPath(orgID, id)+"/credential-rotations", map[string]any{"access_token": token})
		if e != nil {
			return gitLabAPIError(cfg, e, "rotate GitLab installation credentials", gitLabNotEnabled)
		}
		return gitLabWriteResult(cmd, d, row, fields, o, "GitLab credentials rotated", "The GitLab credentials were rotated")
	})
	return c
}
