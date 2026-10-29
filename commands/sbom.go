package commands

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var sbomListFields = []string{"commit_sha", "branch", "is_release", "schema_version", "generated_at", "recorded_at", "total_dependencies", "unresolved", "copyleft_count", "artifacts"}
var sbomViewFields = []string{"commit_sha", "branch", "is_release", "schema_version", "generated_at", "recorded_at", "artifacts", "summary"}
var sbomCommitPattern = regexp.MustCompile("^[0-9a-f]{7,64}$")
var shellSafePattern = regexp.MustCompile("^[a-zA-Z0-9_@%+=:,./-]+$")

const noSBOMHint = "No SBOM is recorded for this repository or commit yet. Run " + "\x60avr sbom generate\x60" + " to create one."

// NewSBOM returns recorded repository SBOM inspection commands.
func NewSBOM(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "sbom", Short: "View, generate, and download repository SBOMs."}
	c.AddCommand(newSBOMList(d), newSBOMView(d))
	return c
}
func shellQuotePython(value string) string {
	if shellSafePattern.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
func sbomBase(org, repo string) string {
	return "/orgs/" + url.PathEscape(org) + "/repos/" + url.PathEscape(repo) + "/analysis/sbom"
}
func sbomContext(ctx context.Context, d Dependencies, org, repo string) (config.Config, *client.Client, string, string, error) {
	cfg, _, api, err := d.load()
	if err != nil {
		return cfg, api, "", "", err
	}
	if err = authenticated(cfg); err != nil {
		return cfg, api, "", "", err
	}
	id, err := resolveOrg(ctx, d, cfg, api, org)
	if err != nil {
		return cfg, api, "", "", err
	}
	rid, err := requiredWorkflowRepo(ctx, d, cfg, api, id, repo)
	return cfg, api, id, rid, err
}
func sbomError(cfg config.Config, err error, action string, hint bool) error {
	result := apiError(cfg, err, action)
	var he *client.HTTPError
	if !hint || !errors.As(err, &he) || he.Status != 404 && he.Status != 422 {
		return result
	}
	first, rest, found := strings.Cut(result.Error(), "\n")
	if found {
		rest = "\n" + rest
	}
	return errors.New(first + "\n  Hint: " + noSBOMHint + rest)
}
func newSBOMList(d Dependencies) *cobra.Command {
	var org, repo, cursor string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List the repository's recorded SBOMs, newest first.", Args: cobra.NoArgs}
	cacheScopeFlags(c, &org, &repo)
	o.flags(c)
	f := c.Flags()
	f.IntVarP(&limit, "limit", "L", 50, "Maximum SBOMs (1–1000).")
	f.StringVar(&cursor, "cursor", "", "Pagination cursor.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if done, err := o.prepare(cmd, d, sbomListFields); done || err != nil {
			return err
		}
		if f.Changed("cursor") && (cursor == "" || strings.TrimSpace(cursor) != cursor || utf8.RuneCountInString(cursor) > 512) {
			return &ExitError{2, "Error: --cursor must be 1..512 chars and not surrounded by whitespace"}
		}
		cfg, api, id, rid, err := sbomContext(cmd.Context(), d, org, repo)
		if err != nil {
			return err
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		page, err := api.SBOMs(cmd.Context(), sbomBase(id, rid), query)
		if err != nil {
			return sbomError(cfg, err, "list SBOMs", false)
		}
		rows := rawRecords(page)
		if f.Changed("json") {
			if err = o.list(cmd, d, rows, sbomListFields); err != nil {
				return err
			}
			if page.NextCursor != "" {
				_, err = fmt.Fprintf(d.Err, "next_cursor: %s\n", output.CellText(shellQuotePython(page.NextCursor)))
			}
			return err
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No data found.")
		} else {
			var cells [][]string
			for _, row := range rows {
				sha := []rune(fieldText(row, "commit_sha"))
				if len(sha) > 12 {
					sha = sha[:12]
				}
				release := "no"
				if row["is_release"] == true {
					release = "yes"
				}
				branch := "N/A"
				if value, ok := row["branch"]; ok {
					branch = pythonValue(value)
				}
				cells = append(cells, []string{string(sha), branch, release, sbomCount(row["total_dependencies"]), sbomCount(row["unresolved"]), sbomCount(row["copyleft_count"]), formatTimestamp(fieldText(row, "recorded_at"))})
			}
			err = output.Grid(d.Out, []string{"Commit", "Branch", "Release", "Deps", "Unresolved", "Copyleft", "Recorded"}, cells)
		}
		if err != nil {
			return err
		}
		if page.NextCursor != "" {
			_, err = fmt.Fprintf(d.Err, "\nMore results available. Re-run with --cursor %s\n", output.CellText(shellQuotePython(page.NextCursor)))
		}
		return err
	})
	return c
}
func validatedSBOMCommit(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !sbomCommitPattern.MatchString(value) {
		return "", &ExitError{2, "Error: must be a hex commit SHA or a prefix of at least 7 characters."}
	}
	return value, nil
}
func requestedSBOMCommit(args []string, option string, changed bool) (string, error) {
	var positional string
	var err error
	if len(args) > 0 {
		positional, err = validatedSBOMCommit(args[0])
		if err != nil {
			return "", err
		}
	}
	if changed {
		option, err = validatedSBOMCommit(option)
		if err != nil {
			return "", err
		}
	}
	if positional != "" && option != "" && positional != option {
		return "", &ExitError{2, "Error: Pass the commit either as the COMMIT argument or with --commit, not both."}
	}
	if positional != "" {
		return positional, nil
	}
	return option, nil
}
func resolveSBOMCommit(ctx context.Context, cfg config.Config, api *client.Client, base, commit string) (string, error) {
	if len(commit) == 40 || len(commit) == 64 {
		return commit, nil
	}
	matches := []string{}
	query := url.Values{"limit": {"1000"}}
	seen := map[string]bool{}
	for len(matches) < 2 {
		page, err := api.SBOMs(ctx, base, query)
		if err != nil {
			return "", sbomError(cfg, err, "list SBOMs", false)
		}
		for _, row := range page.Items {
			sha := fieldText(row.Fields, "commit_sha")
			if strings.HasPrefix(sha, commit) && !slices.Contains(matches, sha) {
				matches = append(matches, sha)
			}
		}
		if page.NextCursor == "" {
			break
		}
		if seen[page.NextCursor] {
			return "", errors.New("SBOM history pagination did not advance")
		}
		seen[page.NextCursor] = true
		query.Set("cursor", page.NextCursor)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("No recorded SBOM matches commit %s. Run \x60avr sbom list\x60 to see the recorded commits.", commit) //nolint:staticcheck // Preserve Python user-facing diagnostics.
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("Commit %s is ambiguous; it matches %s and %s. Use a longer prefix.", commit, matches[0], matches[1]) //nolint:staticcheck // Preserve Python user-facing diagnostics.
	}
	return matches[0], nil
}
func newSBOMView(d Dependencies) *cobra.Command {
	var org, repo, commit string
	var full bool
	var o jsonOptions
	c := &cobra.Command{Use: "view [COMMIT]", Short: "Show an SBOM's summary: dependency counts, licenses, and artifacts.", Args: cobra.MaximumNArgs(1)}
	cacheScopeFlags(c, &org, &repo)
	o.flags(c)
	f := c.Flags()
	f.StringVar(&commit, "commit", "", "Full SHA or a unique prefix of at least seven characters.")
	f.BoolVar(&full, "full", false, "List every license and copyleft dependency in the summary.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		// Click validates individual parameter callbacks before JSON field discovery.
		sha, err := requestedSBOMCommit(args, commit, f.Changed("commit"))
		// Conflicting valid argument/option values are checked after discovery.
		var conflict *ExitError
		isConflict := errors.As(err, &conflict) && strings.Contains(conflict.Message, "Pass the commit")
		if err != nil && !isConflict {
			return err
		}
		if done, e := o.prepare(cmd, d, sbomViewFields); done || e != nil {
			return e
		}
		if err != nil {
			return err
		}
		cfg, api, id, rid, err := sbomContext(cmd.Context(), d, org, repo)
		if err != nil {
			return err
		}
		base := sbomBase(id, rid)
		suffix := "/latest"
		if sha != "" {
			sha, err = resolveSBOMCommit(cmd.Context(), cfg, api, base, sha)
			if err != nil {
				return err
			}
			suffix = "/commits/" + url.PathEscape(sha)
		}
		record, err := api.SBOM(cmd.Context(), base+suffix)
		if err != nil {
			return sbomError(cfg, err, "get SBOM", true)
		}
		if f.Changed("json") {
			fields, e := output.Fields(o.fields, sbomViewFields)
			if e != nil {
				return e
			}
			return output.JSON(cmd.Context(), d.Out, record.Fields, fields, o.expression)
		}
		return renderSBOMView(d, record.Fields, id, rid, full)
	})
	return c
}
