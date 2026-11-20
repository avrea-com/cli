package commands

import "testing"

func TestRepositoryRefLinkUsesConsoleForMirroredRepositories(t *testing.T) {
	const base = "https://console.example.test/org/acme"
	mirrored := map[string]any{"id": "rep-019a0000000070008000000000000002", "full_name": "acme/app", "mirror_enabled": true}
	plain := map[string]any{"id": "rep-019a0000000070008000000000000002", "full_name": "acme/app", "mirror_enabled": false}
	for _, tt := range []struct {
		provider  string
		repo      map[string]any
		kind, ref string
		want      string
	}{
		{"avrea_ci", mirrored, "tree", "feature/a b", base + "/repos/rep-019a0000000070008000000000000002/tree/feature/a%20b"},
		{"github_actions", mirrored, "commit", "abc123", base + "/repos/rep-019a0000000070008000000000000002/commit/abc123"},
		{"gitlab_ci", mirrored, "tree", "main", base + "/repos/rep-019a0000000070008000000000000002/tree/main"},
		{"github_actions", plain, "tree", "feature/a b", "https://github.com/acme/app/tree/feature/a%20b"},
		{"", plain, "commit", "abc123", "https://github.com/acme/app/commit/abc123"},
		{"gitlab_ci", plain, "tree", "feature/a", "https://gitlab.com/acme/app/-/tree/feature/a"},
		{"gitlab_ci", plain, "commit", "abc123", "https://gitlab.com/acme/app/-/commit/abc123"},
		{"avrea_ci", plain, "tree", "main", ""},
		{"avrea_ci", map[string]any{"full_name": "acme/app", "platform": "gitlab"}, "tree", "main", "https://gitlab.com/acme/app/-/tree/main"},
		{"avrea_ci", map[string]any{"full_name": "acme/app", "platform": "github"}, "commit", "abc123", "https://github.com/acme/app/commit/abc123"},
		{"avrea_ci", mirrored, "tree", "", ""},
		{"github_actions", map[string]any{"full_name": "local"}, "tree", "main", ""},
	} {
		if got := repositoryRefLink(base, tt.provider, tt.repo, tt.kind, tt.ref); got != tt.want {
			t.Errorf("%s mirrored=%v %s %q: got %q, want %q", tt.provider, tt.repo["mirror_enabled"], tt.kind, tt.ref, got, tt.want)
		}
	}
}

func TestPlatformRunLinkFollowsTheCIProvider(t *testing.T) {
	for _, tt := range []struct {
		provider, name, run, attempt string
		label, want                  string
	}{
		{"github_actions", "acme/app", "42", "1", "GitHub", "https://github.com/acme/app/actions/runs/42"},
		{"github_actions", "acme/app", "42", "3", "GitHub", "https://github.com/acme/app/actions/runs/42/attempts/3"},
		{"", "acme/app", "42", "", "GitHub", "https://github.com/acme/app/actions/runs/42"},
		{"gitlab_ci", "acme/group/app", "42", "2", "GitLab", "https://gitlab.com/acme/group/app/-/pipelines/42"},
		{"avrea_ci", "acme/app", "42", "1", "", ""},
		{"github_actions", "acme/app", "", "1", "", ""},
	} {
		if label, got := platformRunLink(tt.provider, tt.name, tt.run, tt.attempt); label != tt.label || got != tt.want {
			t.Errorf("%s %s: got %q %q, want %q %q", tt.provider, tt.run, label, got, tt.label, tt.want)
		}
	}
}
