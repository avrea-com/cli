package commands

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
)

var runIDPattern = lazyRegexp(`^(?:run|awr)-[0-9a-f]{32}$`)
var decimalPattern = lazyRegexp(`^[0-9]+$`)
var githubNamePattern = lazyRegexp(`^[A-Za-z0-9_.-]+$`)
var githubStepFragmentPattern = lazyRegexp(`^step:[0-9]+:[0-9]+$`)
var orgSlugPattern = lazyRegexp(`^[A-Za-z0-9-]+$`)

type runReference struct {
	id, platformID, platformJobID, repository, orgSlug string
	attempt                                            int64
}

var errInvalidRunReference = &ExitError{1, "Error: Invalid run reference. Expected an Avrea run ID, a positive GitHub run ID, a GitHub Actions run or job URL, or an Avrea console run URL."}

func invalidRunReference() error { return errInvalidRunReference }

func positiveDecimal(raw string) (string, bool) {
	if !decimalPattern().MatchString(raw) {
		return "", false
	}
	value := strings.TrimLeft(raw, "0")
	return value, value != ""
}

func parseRunReference(raw, host string) (runReference, error) {
	if strings.HasPrefix(raw, "job-") {
		return runReference{}, &ExitError{2, "Error: This is a job ID, not a run ID. Use 'avr job view JOB_ID'."}
	}
	if strings.HasPrefix(raw, "https://gitlab.com/") {
		return runReference{}, &ExitError{2, "Error: Run commands support GitHub workflow runs. Use 'avr job view JOB_ID' for GitLab jobs."}
	}
	if runIDPattern().MatchString(raw) {
		return runReference{id: raw}, nil
	}
	if id, ok := positiveDecimal(raw); ok {
		return runReference{platformID: id}, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Host != u.Hostname() || strings.ContainsAny(raw, "\r\n\t") {
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
		if (len(parts) != 6 && len(parts) != 8) || parts[0] != "" || parts[3] != "actions" || parts[4] != "runs" || !githubNamePattern().MatchString(parts[1]) || !githubNamePattern().MatchString(parts[2]) {
			return runReference{}, invalidRunReference()
		}
		id, ok := positiveDecimal(parts[5])
		if !ok {
			return runReference{}, invalidRunReference()
		}
		ref := runReference{platformID: id, repository: parts[1] + "/" + parts[2]}
		if len(parts) == 8 {
			value, ok := positiveDecimal(parts[7])
			if !ok {
				return runReference{}, invalidRunReference()
			}
			switch parts[6] {
			case "job":
				ref.platformJobID = value
			case "attempts":
				n, e := strconv.ParseInt(value, 10, 64)
				if e != nil {
					return runReference{}, invalidRunReference()
				}
				ref.attempt = n
			default:
				return runReference{}, invalidRunReference()
			}
		}
		if u.Fragment != "" && (ref.platformJobID == "" || !githubStepFragmentPattern().MatchString(u.EscapedFragment())) {
			return runReference{}, invalidRunReference()
		}
		return ref, nil
	}
	if u.Fragment != "" {
		return runReference{}, invalidRunReference()
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
	if u.Hostname() != expected || len(parts) != 5 || parts[0] != "" || parts[1] != "org" || parts[3] != "runs" || !orgSlugPattern().MatchString(parts[2]) || !runIDPattern().MatchString(parts[4]) {
		return runReference{}, invalidRunReference()
	}
	return runReference{id: parts[4], orgSlug: parts[2]}, nil
}

func referenceAndOrg(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, raw, org string, wholeRunCommand ...string) (runReference, string, error) {
	ref, err := parseRunReference(raw, cfg.Host)
	if err != nil {
		return ref, "", err
	}
	if ref.platformJobID != "" && len(wholeRunCommand) > 0 {
		return ref, "", &ExitError{1, fmt.Sprintf("Error: This is a job URL, but `avr run %s` acts on a whole run. Pass the run instead: avr run %s %s", wholeRunCommand[0], wholeRunCommand[0], ref.platformID)}
	}
	return resolveRunOrg(ctx, d, cfg, api, ref, org)
}

func resolveRunOrg(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, ref runReference, org string) (runReference, string, error) {
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
			if fieldText(record, "organization_id") == id {
				slug = fieldText(record, "slug")
				break
			}
		}
		if slug == "" || !strings.EqualFold(slug, ref.orgSlug) {
			return ref, "", fmt.Errorf("avrea run URL organization %q does not match the selected organization %q", ref.orgSlug, slug)
		}
	}
	return ref, id, nil
}

func resolveRun(ctx context.Context, cfg config.Config, api *client.Client, org string, ref runReference, include []string, action string) (map[string]any, error) {
	query := url.Values{}
	if len(include) > 0 {
		query["include"] = include
	}
	var record map[string]any
	var err error
	if ref.attempt != 0 || ref.platformJobID != "" {
		if ref.platformJobID != "" {
			query["include"] = slices.DeleteFunc(slices.Clone(include), func(s string) bool { return s == "jobs" })
		}
		query.Set("platform_run_id", ref.platformID)
		query.Set("limit", "1000")
		query.Set("order", "created_at.desc")
		var page client.Page
		page, err = api.Runs(ctx, org, query)
		if err == nil && ref.platformJobID != "" {
			record, err = attemptContainingJob(ctx, api, org, ref, page.Items, include)
		} else if err == nil {
			for _, candidate := range page.Items {
				if fieldText(candidate, "run_attempt") == strconv.FormatInt(ref.attempt, 10) {
					record = candidate
					break
				}
			}
			if record == nil {
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
	if fieldText(record, "run_id") == "" {
		return record, fmt.Errorf("avrea returned a workflow run without a run_id")
	}
	if ref.repository != "" {
		repo, _ := record["repository"].(map[string]any)
		actual := fieldText(repo, "full_name")
		if !strings.EqualFold(actual, ref.repository) {
			return record, &ExitError{1, fmt.Sprintf("Error: GitHub run URL repository %s does not match the Avrea run repository %s.", pythonQuote(ref.repository), pythonQuote(actual))}
		}
	}
	return record, nil
}

// A job URL identifies an attempt by its jobs, never by the latest-run fallback.
func attemptContainingJob(ctx context.Context, api *client.Client, org string, ref runReference, attempts []map[string]any, include []string) (map[string]any, error) {
	var empty map[string]any
	if len(attempts) == 0 {
		return empty, &ExitError{1, fmt.Sprintf("Error: GitHub run %s has no visible attempts in this organization.", ref.platformID)}
	}
	include = slices.Clone(include)
	if !slices.Contains(include, "jobs") {
		include = append(include, "jobs")
	}
	sawJobs := false
	for _, attempt := range attempts {
		id, ok := attempt["run_id"].(string)
		if !ok {
			continue
		}
		record, err := api.RunByReference(ctx, org, id, false, url.Values{"include": include})
		if err != nil {
			return empty, err
		}
		jobs := objectList(record["jobs"])
		sawJobs = sawJobs || len(jobs) > 0
		for _, job := range jobs {
			if fieldText(job, "platform_job_id") == ref.platformJobID {
				return record, nil
			}
		}
	}
	if !sawJobs {
		return empty, &ExitError{1, fmt.Sprintf("Error: Avrea returned no job data for GitHub run %s, so job %s cannot be matched to an attempt. Pass the run instead: avr run view %s", ref.platformID, ref.platformJobID, ref.platformID)}
	}
	return empty, fmt.Errorf("GitHub job %s is not listed in any visible attempt of run %s in this organization. Avrea may not track that job. To see the latest attempt, pass the run instead: avr run view %s", ref.platformJobID, ref.platformID, ref.platformID)
}
