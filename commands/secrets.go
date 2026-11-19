package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

// NewSecret constructs secret management commands. Secret values are write-only.
func NewSecret(deps Dependencies) *cobra.Command { return newConfiguration(deps.defaults(), "secret") }

// NewVariable constructs readable configuration-variable management commands.
func NewVariable(deps Dependencies) *cobra.Command {
	return newConfiguration(deps.defaults(), "variable")
}
func newConfiguration(d Dependencies, kind string) *cobra.Command {
	c := &cobra.Command{Use: kind, Aliases: []string{kind + "s"}, Short: "Manage organization and repository " + kind + "s."}
	c.AddCommand(newConfigurationRead(d, kind, false), newConfigurationRead(d, kind, true), newConfigurationSet(d, kind), newConfigurationEdit(d, kind), newConfigurationDelete(d, kind))
	return c
}

type configurationOptions struct {
	org, repo, variant, variantID                    string
	repos, envs, branches, workflows                 []string
	visibility, label                                string
	clearEnv, clearBranch, clearWorkflow, clearLabel bool
	json                                             jsonOptions
}

func (o *configurationOptions) contextFlags(c *cobra.Command) {
	c.Flags().StringVarP(&o.org, "org", "o", "", "Organization ID or slug; does not broaden availability.")
	c.Flags().StringVarP(&o.repo, "repo", "R", "", "Repository full name or ID; defaults to the current checkout.")
	o.json.flags(c)
}
func (o *configurationOptions) selectionFlags(c *cobra.Command) {
	c.Flags().StringVar(&o.variant, "variant", "", "Select an existing scoped value by label.")
	c.Flags().StringVar(&o.variantID, "variant-id", "", "Select an existing scoped value by ID.")
	c.MarkFlagsMutuallyExclusive("variant", "variant-id")
}
func (o *configurationOptions) availabilityFlags(c *cobra.Command, edit bool) {
	c.Flags().StringSliceVar(&o.repos, "repos", nil, "Organization availability: repository names or IDs, comma-separated.")
	c.Flags().StringVar(&o.visibility, "visibility", "", "Organization availability: all, private, or selected (requires --repos).")
	c.Flags().StringArrayVarP(&o.envs, "env", "e", nil, "Environment pattern; repeat for alternatives.")
	c.Flags().StringArrayVar(&o.branches, "branch", nil, "Branch pattern; repeat for alternatives.")
	c.Flags().StringArrayVar(&o.workflows, "workflow", nil, "Workflow path pattern; repeat for alternatives.")
	c.Flags().StringVar(&o.label, "label", "", "Label for the scoped value.")
	c.MarkFlagsMutuallyExclusive("repo", "repos")
	c.MarkFlagsMutuallyExclusive("repo", "visibility")
	if edit {
		c.Flags().BoolVar(&o.clearEnv, "clear-env", false, "Remove environment restrictions.")
		c.Flags().BoolVar(&o.clearBranch, "clear-branch", false, "Remove branch restrictions.")
		c.Flags().BoolVar(&o.clearWorkflow, "clear-workflow", false, "Remove workflow restrictions.")
		c.Flags().BoolVar(&o.clearLabel, "clear-label", false, "Clear the scoped value label.")
		for _, name := range []string{"env", "branch", "workflow", "label"} {
			c.MarkFlagsMutuallyExclusive(name, "clear-"+name)
		}
	}
}
func (o configurationOptions) scopeChanged(c *cobra.Command) bool {
	for _, name := range []string{"repos", "visibility", "env", "branch", "workflow", "clear-env", "clear-branch", "clear-workflow"} {
		if c.Flags().Changed(name) {
			return true
		}
	}
	return false
}
func configurationUsage(message string) error { return &ExitError{2, "Error: " + message} }
func (o configurationOptions) validate(c *cobra.Command) error {
	for _, f := range []string{"org", "repo", "variant", "variant-id"} {
		if c.Flags().Changed(f) {
			v, _ := c.Flags().GetString(f)
			if strings.TrimSpace(v) == "" {
				return configurationUsage("--" + f + " cannot be empty.")
			}
		}
	}
	if err := validateConfigurationRepositoryID(o.repo); err != nil {
		return err
	}
	if o.visibility != "" && !slices.Contains([]string{"all", "private", "selected"}, o.visibility) {
		return configurationUsage("--visibility must be all, private, or selected.")
	}
	if c.Flags().Changed("visibility") && o.visibility == "" {
		return configurationUsage("--visibility cannot be empty.")
	}
	if o.visibility == "selected" && len(o.repos) == 0 {
		return configurationUsage("--visibility selected requires --repos.")
	}
	if len(o.repos) > 0 && o.visibility != "" && o.visibility != "selected" {
		return configurationUsage("--repos cannot be combined with --visibility all or private.")
	}
	if c.Flags().Changed("repos") && len(o.repos) == 0 {
		return configurationUsage("--repos requires at least one repository; use --visibility all to remove restrictions.")
	}
	for _, r := range o.repos {
		if strings.TrimSpace(r) == "" {
			return configurationUsage("--repos contains an empty repository.")
		}
		if err := validateConfigurationRepositoryID(r); err != nil {
			return err
		}
	}
	if len(o.repos) > 100 {
		return configurationUsage("At most 100 repositories can be selected.")
	}
	for flag, patterns := range map[string][]string{"--env": o.envs, "--branch": o.branches, "--workflow": o.workflows} {
		for _, pattern := range patterns {
			if strings.ContainsAny(pattern, "?+[!\\") {
				return configurationUsage(flag + " supports only * and ** wildcards; ?, +, [, ! and backslashes are unsupported.")
			}
		}
		if len(patterns) > 50 {
			return configurationUsage("At most 50 patterns are allowed per category.")
		}
		for _, p := range patterns {
			if strings.TrimSpace(p) == "" || utf8.RuneCountInString(p) > 256 || strings.Count(p, "**") > 4 || strings.ContainsRune(p, 0) {
				return configurationUsage("Patterns must be nonempty, at most 256 characters, and contain at most four ** segments.")
			}
		}
	}
	if utf8.RuneCountInString(o.label) > 256 || strings.ContainsRune(o.label, 0) {
		return configurationUsage("Labels must be at most 256 characters and contain no NUL bytes.")
	}
	return nil
}

type configurationContext struct {
	cfg             config.Config
	api             *client.Client
	org, repo, kind string
}

func (x configurationContext) base() string {
	return "/orgs/" + url.PathEscape(x.org) + "/" + x.kind + "s"
}
func (x configurationContext) parent(id string) string { return x.base() + "/" + url.PathEscape(id) }
func (x configurationContext) variantPath(id string) string {
	return "/orgs/" + url.PathEscape(x.org) + "/" + x.kind + "-variants/" + url.PathEscape(id)
}
func (o configurationOptions) context(c *cobra.Command, d Dependencies, kind string) (configurationContext, error) {
	cfg, api, org, err := personContext(c.Context(), d, o.org, "Secret and variable management", "manage secrets or variables")
	x := configurationContext{cfg: cfg, api: api, org: org, kind: kind}
	if err != nil {
		return x, err
	}
	// Explicit organization availability suppresses ambient checkout defaults.
	if o.repo == "" && (c.Flags().Changed("org") || c.Flags().Changed("repos") || c.Flags().Changed("visibility")) {
		return x, nil
	}
	var repos []string
	if o.repo != "" {
		repos = []string{o.repo}
	}
	write := slices.Contains([]string{"set", "edit", "delete"}, c.Name())
	ids, err := resolveRepositories(c.Context(), d, cfg, api, org, repos, !write)
	if len(ids) > 0 {
		x.repo = ids[0]
		if shapeErr := validateConfigurationRepositoryID(x.repo); shapeErr != nil {
			return x, shapeErr
		}
	}
	if err == nil && x.repo == "" && write {
		return x, configurationUsage("No repository was detected. Use an explicit --org or --repo for mutations.")
	}
	return x, err
}
func validateConfigurationRepositoryID(repo string) error {
	if strings.HasPrefix(repo, "rep-") && !strings.Contains(repo, "/") && !repositoryIDPattern().MatchString(repo) {
		return configurationUsage("Repository IDs must be rep- followed by 32 lowercase hexadecimal characters.")
	}
	return nil
}

func configurationAPIError(cfg config.Config, err error) error {
	var he *client.HTTPError
	if errors.As(err, &he) && (he.Status == 400 || he.Status == 422) {
		message := "Invalid configuration. Check the name, label, scope patterns, and value size."
		if he.Detail != "" {
			message = output.CellText(he.Detail)
		}
		return &ExitError{2, "Error: " + message}
	}
	return apiError(cfg, err, "manage secrets and variables")
}
func configurationWriteStatus(err error) string {
	var he *client.HTTPError
	if errors.As(err, &he) && he.Status >= 400 && he.Status < 500 {
		return "failed"
	}
	return "unconfirmed"
}
func configurationWriteError(cfg config.Config, err error) error {
	if errors.Is(err, context.Canceled) {
		return &ExitError{130, "Error: Interrupted; write outcome is unconfirmed; do not retry automatically."}
	}
	if configurationWriteStatus(err) == "failed" {
		return configurationAPIError(cfg, err)
	}
	return fmt.Errorf("%w; write outcome is unconfirmed; do not retry automatically", err)
}
func configurationLabel(label *string) string {
	if label == nil {
		return "(unlabeled)"
	}
	return *label
}
func configurationPrompt(d Dependencies) Dependencies {
	d.Out = d.Err
	return d
}
func (x configurationContext) request(c *cobra.Command, method, path string, body, target any) error {
	return configurationAPIError(x.cfg, x.api.ConfigurationMetadataRequest(c.Context(), method, path, body, target))
}
func (x configurationContext) all(c *cobra.Command) ([]client.ConfigurationValue, error) {
	values := []client.ConfigurationValue{}
	err := x.request(c, http.MethodGet, x.base(), nil, &values)
	// Even an unexpected server field cannot make secret reads reveal values.
	if x.kind == "secret" {
		for i := range values {
			for j := range values[i].Variants {
				values[i].Variants[j].Value = nil
			}
		}
	}
	return values, err
}
func configurationNamed(values []client.ConfigurationValue, name string) *client.ConfigurationValue {
	for i := range values {
		if strings.EqualFold(values[i].Name, name) {
			return &values[i]
		}
	}
	return nil
}
func (o configurationOptions) availability(c *cobra.Command, d Dependencies, x configurationContext, base client.Availability) (client.Availability, error) {
	a := base
	if c.Flags().Changed("repos") {
		a.Repositories = nil
		a.PrivateOnly = false
		for _, repo := range o.repos {
			// Short names are resolved uniquely within this organization, including GitLab namespaces.
			id, err := configurationRepository(c, d, x, repo)
			if err != nil {
				return a, err
			}
			a.Repositories = append(a.Repositories, id)
		}
	}
	if c.Flags().Changed("visibility") {
		a.PrivateOnly = o.visibility == "private"
		if o.visibility != "selected" {
			a.Repositories = nil
		}
	}
	if c.Flags().Changed("env") || o.clearEnv {
		a.Environments = o.envs
	}
	if c.Flags().Changed("branch") || o.clearBranch {
		a.Branches = o.branches
	}
	if c.Flags().Changed("workflow") || o.clearWorkflow {
		a.Workflows = o.workflows
	}
	if a.PrivateOnly && len(a.Repositories)+len(a.Environments)+len(a.Branches)+len(a.Workflows) > 0 {
		return a, configurationUsage("Private-only availability cannot be combined with repository, environment, branch, or workflow restrictions.")
	}
	return a, nil
}
func configurationRepository(c *cobra.Command, d Dependencies, x configurationContext, name string) (string, error) {
	if strings.Contains(name, "/") || strings.HasPrefix(name, "rep-") {
		return resolveRepository(c.Context(), d, x.cfg, x.api, x.org, name, "")
	}
	ids := []string{}
	query := url.Values{"q": {name}, "limit": {"100"}}
	for page, err := range repositoryPages(c.Context(), x.api, x.org, query) {
		if err != nil {
			return "", configurationAPIError(x.cfg, err)
		}
		for _, r := range page.Items {
			full := fieldText(r, "full_name")
			parts := strings.Split(full, "/")
			if strings.EqualFold(parts[len(parts)-1], name) {
				ids = append(ids, fieldText(r, "repository_id"))
			}
		}
	}
	if len(ids) != 1 || ids[0] == "" {
		return "", configurationUsage("Repository " + output.CellText(name) + " is missing or ambiguous; use its full name or ID.")
	}
	return ids[0], nil
}
func canonicalAvailability(a client.Availability) client.Availability {
	normalize := func(v []string, fold bool) []string {
		out := append([]string{}, v...)
		if fold {
			for i := range out {
				out[i] = strings.ToLower(out[i])
			}
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	a.Repositories = normalize(a.Repositories, false)
	a.Environments = normalize(a.Environments, true)
	a.Branches = normalize(a.Branches, false)
	a.Workflows = normalize(a.Workflows, false)
	return a
}
func equalAvailability(a, b client.Availability) bool {
	a, b = canonicalAvailability(a), canonicalAvailability(b)
	return a.PrivateOnly == b.PrivateOnly && slices.Equal(a.Repositories, b.Repositories) && slices.Equal(a.Environments, b.Environments) && slices.Equal(a.Branches, b.Branches) && slices.Equal(a.Workflows, b.Workflows)
}
func (o configurationOptions) selectVariant(c *cobra.Command, d Dependencies, variants []client.ScopedValue) (client.ScopedValue, error) {
	candidates := variants
	if o.variant != "" || o.variantID != "" {
		candidates = nil
		for _, v := range variants {
			if o.variantID != "" && v.ID() == o.variantID || o.variant != "" && v.Label != nil && *v.Label == o.variant {
				candidates = append(candidates, v)
			}
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return client.ScopedValue{}, configurationUsage("No matching scoped value. Use get to inspect existing variants.")
	}
	if !d.IsInputTerminal() || d.Getenv("AVR_PROMPT_DISABLED") != "" {
		return client.ScopedValue{}, configurationUsage("Several scoped values match; specify --variant or --variant-id.")
	}
	for i, v := range candidates {
		label := configurationLabel(v.Label)
		_, _ = fmt.Fprintf(d.Err, "%d. %s (%s)\n", i+1, output.CellText(label), output.CellText(v.ID()))
	}
	prompt := configurationPrompt(d)
	answer, err := cardPrompt(c.Context(), prompt, bufio.NewReader(d.In), "Select scoped value number", false)
	if err != nil {
		return client.ScopedValue{}, err
	}
	n, err := strconv.Atoi(answer)
	if err != nil || n < 1 || n > len(candidates) {
		return client.ScopedValue{}, configurationUsage("Invalid scoped value selection.")
	}
	return candidates[n-1], nil
}
func configurationRecord(v any) map[string]any {
	b, _ := json.Marshal(v)
	var r map[string]any
	_ = json.Unmarshal(b, &r)
	return r
}

func configurationFields(kind string) []string {
	fields := []string{"name", "description", "updated_at", "variants"}
	if kind == "secret" {
		return append(fields, "secret_id", "as_file")
	}
	return append(fields, "variable_id")
}

var variableValueFields = []string{"variable_variant_id", "label", "availability", "value", "has_value", "current_version_id", "current_version", "current_version_created_at", "updated_at"}

func scopedConfigurationRecord(kind string, v client.ScopedValue) map[string]any {
	record := configurationRecord(v)
	if kind == "variable" {
		delete(record, "variant_id")
		delete(record, "rotation_reminder_days")
		record["has_value"] = v.Value != nil
	} else {
		delete(record, "variable_variant_id")
		delete(record, "value")
	}
	return record
}
func namedConfigurationRecord(kind string, v client.ConfigurationValue) map[string]any {
	record := configurationRecord(v)
	if kind == "variable" {
		delete(record, "secret_id")
		delete(record, "as_file")
	} else {
		delete(record, "variable_id")
	}
	variants := make([]map[string]any, 0, len(v.Variants))
	for _, variant := range v.Variants {
		variants = append(variants, scopedConfigurationRecord(kind, variant))
	}
	record["variants"] = variants
	return record
}
