package commands

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
)

var runIDPattern = regexp.MustCompile(`^run-[0-9a-f]{12}7[0-9a-f]{3}[89ab][0-9a-f]{15}$`)
var decimalPattern = regexp.MustCompile(`^[0-9]+$`)
var githubNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var orgSlugPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

type runReference struct {
	id, platformID, repository, orgSlug string
	attempt                             int64
}

func invalidRunReference() error {
	return fmt.Errorf("invalid run reference. Expected an Avrea run ID, a positive GitHub run ID, a GitHub Actions run URL, or an Avrea console run URL")
}

func positiveDecimal(raw string) (string, bool) {
	if !decimalPattern.MatchString(raw) {
		return "", false
	}
	value := strings.TrimLeft(raw, "0")
	return value, value != ""
}

func parseRunReference(raw, host string) (runReference, error) {
	if runIDPattern.MatchString(raw) {
		return runReference{id: raw}, nil
	}
	if id, ok := positiveDecimal(raw); ok {
		return runReference{platformID: id}, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Host != u.Hostname() || u.Fragment != "" || strings.ContainsAny(raw, "\r\n\t") {
		return runReference{}, invalidRunReference()
	}
	for _, r := range raw {
		if r < 32 || r == 127 {
			return runReference{}, invalidRunReference()
		}
	}
	// Use the escaped path: encoded slashes or delimiters must never acquire
	// meaning as URL structure after validation.
	parts := strings.Split(strings.TrimSuffix(u.EscapedPath(), "/"), "/")
	if strings.EqualFold(u.Hostname(), "github.com") {
		if (len(parts) != 6 && len(parts) != 8) || parts[0] != "" || parts[3] != "actions" || parts[4] != "runs" || !githubNamePattern.MatchString(parts[1]) || !githubNamePattern.MatchString(parts[2]) {
			return runReference{}, invalidRunReference()
		}
		id, ok := positiveDecimal(parts[5])
		if !ok {
			return runReference{}, invalidRunReference()
		}
		ref := runReference{platformID: id, repository: parts[1] + "/" + parts[2]}
		if len(parts) == 8 {
			n, e := strconv.ParseInt(parts[7], 10, 64)
			if parts[6] != "attempts" || !decimalPattern.MatchString(parts[7]) || e != nil || n < 1 {
				return runReference{}, invalidRunReference()
			}
			ref.attempt = n
		}
		return ref, nil
	}
	apiURL, err := url.Parse(host)
	if err != nil {
		return runReference{}, invalidRunReference()
	}
	expected := ""
	if strings.HasPrefix(apiURL.Hostname(), "api.") {
		expected = "console." + strings.TrimPrefix(apiURL.Hostname(), "api.")
	}
	if u.Hostname() == "console.avrea.com" && apiURL.Hostname() != "api.avrea.com" {
		return runReference{}, fmt.Errorf("this run URL belongs to a different Avrea environment. Switch with `avr auth switch https://api.avrea.com` and try again")
	}
	if u.Hostname() != expected || len(parts) != 5 || parts[0] != "" || parts[1] != "org" || parts[3] != "runs" || !orgSlugPattern.MatchString(parts[2]) || !runIDPattern.MatchString(parts[4]) {
		return runReference{}, invalidRunReference()
	}
	return runReference{id: parts[4], orgSlug: parts[2]}, nil
}

func referenceAndOrg(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, raw, org string) (runReference, string, error) {
	ref, err := parseRunReference(raw, cfg.Host)
	if err != nil {
		return ref, "", err
	}
	if ref.orgSlug != "" {
		if config.IsScoped(cfg.Token) {
			return ref, "", fmt.Errorf("a scoped token cannot resolve a run URL's organization. Pass the run ID instead")
		}
		if org == "" {
			org = ref.orgSlug
		}
	}
	id, err := resolveOrg(ctx, d, cfg, api, org)
	if err != nil {
		return ref, "", err
	}
	if ref.orgSlug != "" {
		// Unlike display links this check must fail closed; a fallback slug could
		// conceal a URL for a different organization.
		page, err := api.Organizations(ctx)
		if err != nil {
			return ref, "", apiError(cfg, err, "verify the organization")
		}
		slug := ""
		for _, record := range page.Items {
			if record.Value.OrganizationId == id {
				slug = record.Value.Slug
				break
			}
		}
		if slug == "" || !strings.EqualFold(slug, ref.orgSlug) {
			return ref, "", fmt.Errorf("avrea run URL organization %q does not match the selected organization %q", ref.orgSlug, slug)
		}
	}
	return ref, id, nil
}

func resolveRun(ctx context.Context, cfg config.Config, api *client.Client, org string, ref runReference, include []string, action string) (client.Record[client.Run], error) {
	query := url.Values{}
	if len(include) > 0 {
		query["include"] = include
	}
	var record client.Record[client.Run]
	var err error
	if ref.attempt != 0 {
		query.Set("platform_run_id", ref.platformID)
		query.Set("limit", "1000")
		query.Set("order", "created_at.desc")
		var page client.Page[client.Run]
		page, err = api.Runs(ctx, org, query)
		if err == nil {
			for _, candidate := range page.Items {
				if fieldText(candidate.Fields, "run_attempt") == strconv.FormatInt(ref.attempt, 10) {
					record = candidate
					break
				}
			}
			if record.Fields == nil {
				return record, fmt.Errorf("GitHub run %s has no visible attempt %d in this organization", ref.platformID, ref.attempt)
			}
		}
	} else {
		id := ref.id
		if id == "" {
			id = ref.platformID
		}
		record, err = api.RunByReference(ctx, org, id, ref.id == "", query)
	}
	if err != nil {
		return record, apiError(cfg, err, action)
	}
	if fieldText(record.Fields, "run_id") == "" {
		return record, fmt.Errorf("avrea returned a workflow run without a run_id")
	}
	if ref.repository != "" {
		repo, _ := record.Fields["repository"].(map[string]any)
		actual := fieldText(repo, "full_name")
		if !strings.EqualFold(actual, ref.repository) {
			return record, fmt.Errorf("GitHub run URL repository %q does not match the Avrea run repository %q", ref.repository, actual)
		}
	}
	return record, nil
}
