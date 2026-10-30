package commands

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
)

func gitOrigin(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "git", "remote", "get-url", "origin").Output()
	return strings.TrimSpace(string(data)), err
}

var scpRemote = regexp.MustCompile("^[^/@:]+@([^/:]+):(.+)$")

func parseGitRemote(raw string) (name, platform string) {
	raw = strings.TrimPrefix(raw, "git+")
	var host string
	if parts := scpRemote.FindStringSubmatch(raw); parts != nil {
		host, name = parts[1], parts[2]
	} else {
		u, err := url.Parse(raw)
		if err != nil {
			return "", ""
		}
		switch u.Scheme {
		case "ssh", "http", "https", "git":
		default:
			return "", ""
		}
		host, name = u.Hostname(), strings.TrimLeft(u.Path, "/")
	}
	name = strings.TrimSuffix(name, ".git")
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return "", ""
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", ""
		}
	}
	switch strings.ToLower(host) {
	case "github.com":
		platform = "github"
	case "gitlab.com":
		platform = "gitlab"
	}
	return name, platform
}

type missingRepository struct{ message string }

func (e *missingRepository) Error() string { return e.message }

func resolveRepository(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, repo, platform string) (string, error) {
	if strings.HasPrefix(repo, "rep-") && !strings.Contains(repo, "/") {
		return repo, nil
	}
	if !strings.Contains(repo, "/") {
		return "", fmt.Errorf("invalid repository format: %q\nUse org/repo (e.g. acme/web) or a repo ID (rep-...)", output.CellText(repo))
	}
	warnCase := func(actual string) {
		if actual != "" && actual != repo && strings.EqualFold(actual, repo) && d.IsErrorTerminal() {
			_, _ = fmt.Fprintln(d.Err, d.errorPaint(fmt.Sprintf("(using repo: %s → %s)", output.CellText(repo), output.CellText(actual)), "2"))
		}
	}
	if config.IsScoped(cfg.Token) {
		query := url.Values{"q": {repo}, "limit": {"100"}}
		seen := map[string]bool{}
		for {
			page, err := api.Repositories(ctx, org, query)
			if err != nil {
				return "", apiError(cfg, err, "resolve the repository")
			}
			for _, record := range page.Items {
				r := record.Value
				if platform != "" && (r.Platform == nil || string(*r.Platform) != platform) {
					continue
				}
				if strings.EqualFold(r.FullName, repo) && r.RepositoryId != "" {
					warnCase(r.FullName)
					return r.RepositoryId, nil
				}
			}
			if page.NextCursor == "" {
				return "", &missingRepository{fmt.Sprintf("This scoped token does not reach repository %q.\n  \x60avr repo list\x60 shows the repositories it reaches.\nAborted!", output.CellText(repo))}
			}
			if len(page.Items) == 0 || seen[page.NextCursor] {
				return "", errors.New("repository listing returned a cursor that does not advance")
			}
			seen[page.NextCursor] = true
			query.Set("cursor", page.NextCursor)
		}
	}
	query := url.Values{"name": {repo}}
	if platform != "" {
		query.Set("platform", platform)
	}
	record, err := api.ResolveRepository(ctx, org, query)
	if err == nil {
		if record.Value.RepositoryId == "" {
			return "", errors.New("unexpected API response: repository_id is missing")
		}
		warnCase(record.Value.FullName)
		return record.Value.RepositoryId, nil
	}
	var he *client.HTTPError
	if !errors.As(err, &he) || he.Status != 404 {
		return "", apiError(cfg, err, "resolve the repository")
	}
	var message strings.Builder
	detail, structured := he.Payload["detail"].(map[string]any)
	if structured {
		fmt.Fprintf(&message, "Repository %q is not in org %q.", output.CellText(repo), output.CellText(org))
	} else {
		fmt.Fprintf(&message, "Failed to resolve repository %s (HTTP 404).", pythonQuote(output.CellText(repo)))
		if he.Detail != "" {
			fmt.Fprintf(&message, "\n  %s", output.CellText(he.Detail))
		}
	}
	if names, ok := detail["nearby_full_names"].([]any); ok && len(names) > 0 {
		message.WriteString("\n\n  Did you mean one of these?")
		for _, name := range names {
			fmt.Fprintf(&message, "\n    %s", output.CellText(fmt.Sprint(name)))
		}
	}
	if orgs, ok := detail["other_orgs"].([]any); ok && len(orgs) > 0 {
		message.WriteString("\n\n  You also belong to:")
		for _, item := range orgs {
			if o, ok := item.(map[string]any); ok {
				fmt.Fprintf(&message, "\n    %s (%s)", output.CellText(fieldText(o, "slug")), output.CellText(fieldText(o, "organization_id")))
			}
		}
		message.WriteString("\n  Try --org <id>, or avr config set org <slug> to switch default.")
	} else if structured {
		message.WriteString("\n\n  Connect this repo: open the Avrea console and add it to the org.")
	}
	if platform == "gitlab" {
		message.WriteString("\nCheck the GitLab repository connection, your access, and whether GitLab support is enabled.")
	}
	message.WriteString("\nAborted!")
	return "", &missingRepository{message.String()}
}

func resolveRepositories(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org string, repos []string, soft bool) ([]string, error) {
	platform := ""
	detected := false
	if len(repos) == 0 {
		name := cfg.Repo
		if name == "" {
			remote, err := d.GitRemote(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, nil
			}
			name, platform = parseGitRemote(remote)
			detected = true
		}
		if name == "" {
			return nil, nil
		}
		if d.IsErrorTerminal() && d.Getenv("AVR_REPO") != name {
			_, _ = fmt.Fprintln(d.Err, d.errorPaint("(using repo: "+output.CellText(name)+")", "2"))
		}
		repos = []string{name}
	}
	result := make([]string, 0, len(repos))
	for _, repo := range repos {
		id, err := resolveRepository(ctx, d, cfg, api, org, repo, platform)
		if err != nil {
			var missing *missingRepository
			if detected && soft && errors.As(err, &missing) {
				note := fmt.Sprintf("auto-detected %s isn't in this org — showing org-wide results.", output.CellText(repo))
				if config.IsScoped(cfg.Token) {
					note = fmt.Sprintf("auto-detected %s is not a repository this scoped token reaches; showing all that it reaches.", output.CellText(repo))
				}
				_, _ = fmt.Fprintln(d.Err, d.errorPaint("  ("+note+")", "2"))
				return nil, nil
			}
			return nil, err
		}
		result = append(result, id)
	}
	return result, nil
}
