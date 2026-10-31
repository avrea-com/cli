package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var settingsFields = []string{"key", "value", "source"}
var settingsSchemaFields = []string{"key", "value_type", "default", "scopes", "inherits", "description", "choices", "min_value", "max_value"}
var settingInteger = regexp.MustCompile("^[+-]?[0-9]+(_[0-9]+)*$")

// NewSettings shares public settings scope selection and output with avr-admin.
func NewSettings(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "settings", Short: "View and toggle cache and runner settings."}
	c.AddCommand(newSettingsList(d), newSettingsSchema(d), newSettingsWrite(d, false), newSettingsWrite(d, true))
	return c
}
func settingsBase(org, repo string) string {
	path := "/orgs/" + url.PathEscape(org)
	if repo != "" {
		path += "/repos/" + url.PathEscape(repo)
	}
	return path + "/settings"
}
func settingsContext(cmd *cobra.Command, d Dependencies, org, repo string, write bool) (config.Config, *client.Client, string, string, error) {
	cfg, _, api, err := d.load()
	if err != nil {
		return cfg, api, "", "", err
	}
	if err = authenticated(cfg); err != nil {
		return cfg, api, "", "", err
	}
	id, err := resolveOrg(cmd.Context(), d, cfg, api, org)
	if err != nil {
		return cfg, api, "", "", err
	}
	explicitOrg, explicitRepo := cmd.Flags().Changed("org"), cmd.Flags().Changed("repo")
	// An explicit org suppresses ambient repository context unless --repo is
	// also supplied. Never fall back from a detected write to an org-wide write.
	if explicitOrg {
		if !explicitRepo {
			return cfg, api, id, "", nil
		}
		if repo == "" {
			repo = cfg.Repo
		}
		if repo == "" {
			return cfg, api, id, "", nil
		}
	}
	var repos []string
	if repo != "" {
		repos = []string{repo}
	}
	ids, err := resolveRepositories(cmd.Context(), d, cfg, api, id, repos, !write)
	rid := ""
	if len(ids) > 0 {
		rid = ids[0]
	}
	return cfg, api, id, rid, err
}
func newSettingsList(d Dependencies) *cobra.Command {
	var org, repo, prefix string
	var web bool
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List settings with their current values and source.", Args: cobra.NoArgs}
	cacheScopeFlags(c, &org, &repo)
	c.Flags().StringVar(&prefix, "prefix", "", "Filter by key prefix.")
	c.Flags().BoolVar(&web, "web", false, "Open in browser.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if web && cmd.Flags().Changed("json") {
			return &ExitError{2, "Error: --web and --json are mutually exclusive."}
		}
		if done, err := o.prepare(cmd, d, settingsFields); done || err != nil {
			return err
		}
		cfg, api, id, rid, err := settingsContext(cmd, d, org, repo, false)
		if err != nil {
			return err
		}
		if web {
			target := consoleURL(cfg.Host) + "/org/" + url.PathEscape(orgSlug(cmd.Context(), cfg, api, id))
			if rid != "" {
				target += "/repos/" + url.PathEscape(rid)
			}
			target += "/settings"
			label := target
			if d.IsTerminal() {
				label = "Opening: " + target
			}
			if _, err = fmt.Fprintln(d.Out, label); err != nil {
				return err
			}
			if d.IsTerminal() {
				return d.OpenBrowser(cmd.Context(), target)
			}
			return nil
		}
		query := url.Values{}
		if prefix != "" {
			query.Set("prefix", prefix)
		}
		page, err := api.Settings(cmd.Context(), settingsBase(id, rid), query)
		if err != nil {
			return apiError(cfg, err, "list settings")
		}
		rows := rawRecords(page)
		if cmd.Flags().Changed("json") {
			return o.list(cmd, d, rows, settingsFields)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No data found.")
			return err
		}
		cells := make([][]string, 0, len(rows))
		for _, row := range rows {
			value := pythonValue(row["value"])
			switch row["value"] {
			case true:
				value = "yes"
			case false:
				value = "no"
			}
			cells = append(cells, []string{fieldText(row, "key"), value, fieldText(row, "source")})
		}
		return output.Grid(d.Out, []string{"Key", "Value", "Source"}, cells)
	})
	return c
}
func newSettingsSchema(d Dependencies) *cobra.Command {
	var prefix, scope string
	var o jsonOptions
	c := &cobra.Command{Use: "schema", Short: "List available setting definitions.", Args: cobra.NoArgs}
	c.Flags().StringVar(&prefix, "prefix", "", "Filter by key prefix.")
	c.Flags().StringVar(&scope, "scope", "", "Filter by scope: repository or organization.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if cmd.Flags().Changed("scope") && scope != "repository" && scope != "organization" {
			return &ExitError{2, "Error: Invalid value for '--scope': " + pythonQuote(output.CellText(scope)) + " is not one of 'repository', 'organization'."}
		}
		if done, err := o.prepare(cmd, d, settingsSchemaFields); done || err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		query := url.Values{}
		if prefix != "" {
			query.Set("prefix", prefix)
		}
		if scope != "" {
			query.Set("scope", scope)
		}
		page, err := api.SettingsSchema(cmd.Context(), query)
		if err != nil {
			return apiError(cfg, err, "list settings schema")
		}
		rows := rawRecords(page)
		if cmd.Flags().Changed("json") {
			return o.list(cmd, d, rows, settingsSchemaFields)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No data found.")
			return err
		}
		cells := make([][]string, 0, len(rows))
		for _, row := range rows {
			inherits := "no"
			if row["inherits"] == true {
				inherits = "yes"
			}
			cells = append(cells, []string{fieldText(row, "key"), fieldText(row, "value_type"), pythonValue(row["default"]), settingsConstraints(row), strings.Join(settingsStrings(row["scopes"]), ", "), inherits})
		}
		return output.Grid(d.Out, []string{"Key", "Type", "Default", "Constraints", "Scopes", "Inherits"}, cells)
	})
	return c
}
func settingsConstraints(row map[string]any) string {
	parts := []string{}
	if choices := settingsStrings(row["choices"]); len(choices) > 0 {
		parts = append(parts, strings.Join(choices, "|"))
	}
	min, max := row["min_value"], row["max_value"]
	switch {
	case min != nil && max != nil:
		parts = append(parts, pythonValue(min)+".."+pythonValue(max))
	case min != nil:
		parts = append(parts, ">="+pythonValue(min))
	case max != nil:
		parts = append(parts, "<="+pythonValue(max))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}
func parseSettingValue(raw string) any {
	switch strings.ToLower(raw) {
	case "true", "yes", "on":
		return true
	case "false", "no", "off":
		return false
	}
	text := strings.TrimSpace(raw)
	if settingInteger.MatchString(text) {
		number, ok := new(big.Int).SetString(strings.ReplaceAll(text, "_", ""), 10)
		if ok {
			return json.Number(number.String())
		}
	}
	return raw
}
func settingsWriteError(cfg config.Config, err error, action, key string, repo, repoFlag bool) error {
	result := apiError(cfg, err, action)
	var he *client.HTTPError
	if !repo || !errors.As(err, &he) || he.Status != 422 {
		return result
	}
	hint := "'" + output.CellText(key) + "' may be settable only at org scope — "
	if repoFlag {
		hint += "drop --repo and use --org."
	} else {
		hint += "use --org to target the organization."
	}
	first, rest, found := strings.Cut(result.Error(), "\n")
	if found {
		rest = "\n" + rest
	}
	return errors.New(first + "\n  Hint: " + hint + rest)
}
func newSettingsWrite(d Dependencies, reset bool) *cobra.Command {
	var org, repo string
	use, short := "set KEY VALUE", "Set a setting value."
	names := []string{"KEY", "VALUE"}
	if reset {
		use, short = "reset KEY", "Remove a setting override."
		names = []string{"KEY"}
	}
	c := &cobra.Command{Use: use, Short: short, Args: positionalArguments(names...)}
	cacheScopeFlags(c, &org, &repo)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, id, rid, err := settingsContext(cmd, d, org, repo, true)
		if err != nil {
			return err
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		scope := "org"
		if rid != "" {
			scope = "repo"
		}
		path := settingsBase(id, rid)
		if reset {
			_, err = api.Raw(cmd.Context(), http.MethodDelete, path+"/"+url.PathEscape(args[0]), nil)
			if err != nil {
				return settingsWriteError(cfg, err, "reset setting", args[0], rid != "", cmd.Flags().Changed("repo"))
			}
			_, err = fmt.Fprintf(d.Out, "Reset %s (%s) to inherited default\n", output.CellText(args[0]), scope)
		} else {
			record, e := api.SetSetting(cmd.Context(), path, args[0], parseSettingValue(args[1]))
			if e != nil {
				return settingsWriteError(cfg, e, "update setting", args[0], rid != "", cmd.Flags().Changed("repo"))
			}
			_, err = fmt.Fprintf(d.Out, "Set %s = %s (%s)\n", output.CellText(fieldText(record.Fields, "key")), output.CellText(pythonValue(record.Fields["value"])), scope)
		}
		return err
	})
	return c
}

func settingsStrings(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
