package commands

import (
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// serviceTokenOpenRoutes are the only routes the API opens to a service token,
// all for GET. Every other route answers 404 to it.
var serviceTokenOpenRoutes = []string{
	"/service-tokens/self",
	"/orgs/{org_id}/firewall/rules",
	"/orgs/{org_id}/repos",
	"/orgs/{org_id}/repos/{repo_id}",
	"/orgs/{org_id}/repos/{repo_id}/firewall/rules",
	"/orgs/{org_id}/repos/{repo_id}/analysis/sbom",
	"/orgs/{org_id}/repos/{repo_id}/analysis/sbom/latest",
	"/orgs/{org_id}/repos/{repo_id}/analysis/sbom/commits/{commit_sha}",
	"/orgs/{org_id}/repos/{repo_id}/analysis/sbom/commits/{commit_sha}/artifacts/{filename}",
}

const (
	serviceTokenRepoID   = "rep-019a0000000070008000000000000004"
	serviceTokenSBOMSHA  = "0123456789abcdef0123456789abcdef01234567"
	serviceTokenArtifact = `{"bomFormat":"CycloneDX","specVersion":"1.6"}`
)

// serviceTokenOpenRoute returns the open route pattern a GET path matches, or
// "". A placeholder never matches the literal "resolve", so the closed
// /orgs/{org_id}/repos/resolve route is not mistaken for a repository.
func serviceTokenOpenRoute(method, path string) string {
	if method != http.MethodGet {
		return ""
	}
	parts := strings.Split(path, "/")
	for _, pattern := range serviceTokenOpenRoutes {
		want := strings.Split(pattern, "/")
		if len(want) != len(parts) {
			continue
		}
		matched := true
		for i, segment := range want {
			if strings.HasPrefix(segment, "{") {
				matched = parts[i] != "" && parts[i] != "resolve"
			} else {
				matched = parts[i] == segment
			}
			if !matched {
				break
			}
		}
		if matched {
			return pattern
		}
	}
	return ""
}

// serviceTokenOpenAPI answers the open routes with minimal bodies that the
// read commands parse to completion, and every other request with 404 while
// recording it in violations.
func serviceTokenOpenAPI(violations *[]string) func(*http.Request) (int, string) {
	sbom := `{"commit_sha":"` + serviceTokenSBOMSHA + `","branch":"main","is_release":false,"schema_version":"1","generated_at":"2026-11-02T10:00:00Z","recorded_at":"2026-11-02T10:01:00Z","artifacts":[{"filename":"bom.cyclonedx.json","media_type":"application/vnd.cyclonedx+json","raw_bytes":2048}],"summary":{"total_dependencies":12,"unresolved":0,"licenses":{"MIT":12},"ecosystems":{"npm":12},"copyleft":[]}}`
	bodies := map[string]string{
		"/service-tokens/self":                                                                   serviceTokenSelf,
		"/orgs/{org_id}/firewall/rules":                                                          `[{"rule_id":"efr-019a0000000070008000000000000006","position":0,"action":"allow","cidr":null,"fqdn":"org-registry.example.test","protocol":"tcp","port_start":443,"port_end":443,"is_default":false}]`,
		"/orgs/{org_id}/repos":                                                                   `{"data":[{"repository_id":"rep-019a0000000070008000000000000005","full_name":"acme/web","platform":"github","platform_repository_id":101},{"repository_id":"` + serviceTokenRepoID + `","full_name":"acme/api","platform":"github","platform_repository_id":102}],"pagination":{"next_cursor":null}}`,
		"/orgs/{org_id}/repos/{repo_id}":                                                         `{"repository_id":"` + serviceTokenRepoID + `","full_name":"acme/api","platform":"github","platform_repository_id":102}`,
		"/orgs/{org_id}/repos/{repo_id}/firewall/rules":                                          `[{"rule_id":"efr-019a0000000070008000000000000007","position":0,"action":"deny","cidr":null,"fqdn":"repo-blocked.example.test","protocol":"any","port_start":null,"port_end":null,"is_default":false}]`,
		"/orgs/{org_id}/repos/{repo_id}/analysis/sbom":                                           `{"data":[{"commit_sha":"` + serviceTokenSBOMSHA + `","branch":"main","is_release":false,"schema_version":"1","generated_at":"2026-11-02T10:00:00Z","recorded_at":"2026-11-02T10:01:00Z","total_dependencies":12,"unresolved":0,"copyleft_count":0,"artifacts":["bom.cyclonedx.json"]}],"pagination":{"next_cursor":null}}`,
		"/orgs/{org_id}/repos/{repo_id}/analysis/sbom/latest":                                    sbom,
		"/orgs/{org_id}/repos/{repo_id}/analysis/sbom/commits/{commit_sha}":                      sbom,
		"/orgs/{org_id}/repos/{repo_id}/analysis/sbom/commits/{commit_sha}/artifacts/{filename}": serviceTokenArtifact,
	}
	return func(r *http.Request) (int, string) {
		route := serviceTokenOpenRoute(r.Method, r.URL.Path)
		if route == "" {
			*violations = append(*violations, r.Method+" "+r.URL.Path)
			return 404, `{"detail":"Not Found"}`
		}
		return 200, bodies[route]
	}
}

func TestServiceTokenOpenRouteMatcher(t *testing.T) {
	org := "/orgs/" + inspectionOrg
	for path, want := range map[string]bool{
		"/service-tokens/self": true,
		org + "/repos":         true,
		org + "/repos/" + serviceTokenRepoID + "/analysis/sbom/commits/" + serviceTokenSBOMSHA + "/artifacts/bom.spdx.json": true,
		org + "/repos/resolve": false,
		org + "/repos/" + serviceTokenRepoID + "/firewall/resolved":   false,
		"/users/me/organizations":                                     false,
		org + "/repos/" + serviceTokenRepoID + "/analysis/sbom/extra": false,
	} {
		if got := serviceTokenOpenRoute(http.MethodGet, path) != ""; got != want {
			t.Errorf("%s: open=%v, want %v", path, got, want)
		}
	}
	if serviceTokenOpenRoute(http.MethodPost, org+"/firewall/rules") != "" {
		t.Error("POST must not be open")
	}
}

// Each case runs with no --org and no AVR_ORG, so the organization comes from
// /service-tokens/self. The expected request list is the command's full cost
// against the token's rate limit of 60 requests per minute.
func TestServiceTokenReadCommands(t *testing.T) {
	self := "GET /service-tokens/self"
	org := "/orgs/" + inspectionOrg
	repos := "GET " + org + "/repos"
	repo := org + "/repos/" + serviceTokenRepoID
	sboms := "GET " + repo + "/analysis/sbom"
	latest := sboms + "/latest"
	commit := sboms + "/commits/" + serviceTokenSBOMSHA
	target := filepath.Join(t.TempDir(), "bom.json")
	for _, tc := range []struct {
		name string
		args []string
		want []string
		out  string
	}{
		{"repo list", []string{"repo", "list"}, []string{self, repos}, "acme/api"},

		{"sbom list by name", []string{"sbom", "list", "--repo", "acme/api"}, []string{self, repos, sboms}, serviceTokenSBOMSHA[:12]},
		{"sbom list by ID", []string{"sbom", "list", "--repo", serviceTokenRepoID}, []string{self, sboms}, serviceTokenSBOMSHA[:12]},

		{"sbom view latest by name", []string{"sbom", "view", "--repo", "acme/api"}, []string{self, repos, latest}, "bom.cyclonedx.json"},
		{"sbom view latest by ID", []string{"sbom", "view", "--repo", serviceTokenRepoID}, []string{self, latest}, "bom.cyclonedx.json"},
		{"sbom view commit by name", []string{"sbom", "view", serviceTokenSBOMSHA, "--repo", "acme/api"}, []string{self, repos, commit}, "bom.cyclonedx.json"},
		{"sbom view commit by ID", []string{"sbom", "view", serviceTokenSBOMSHA, "--repo", serviceTokenRepoID}, []string{self, commit}, "bom.cyclonedx.json"},
		// A short prefix costs one extra history request to find the full SHA.
		{"sbom view commit prefix by ID", []string{"sbom", "view", serviceTokenSBOMSHA[:7], "--repo", serviceTokenRepoID}, []string{self, sboms, commit}, "bom.cyclonedx.json"},

		{"sbom download latest by name", []string{"sbom", "download", "--repo", "acme/api", "--out", "-"}, []string{self, repos, latest, commit + "/artifacts/bom.cyclonedx.json"}, serviceTokenArtifact},
		{"sbom download latest by ID", []string{"sbom", "download", "--repo", serviceTokenRepoID, "--out", "-"}, []string{self, latest, commit + "/artifacts/bom.cyclonedx.json"}, serviceTokenArtifact},
		{"sbom download commit by name", []string{"sbom", "download", serviceTokenSBOMSHA, "--repo", "acme/api", "--format", "spdx", "--out", "-"}, []string{self, repos, commit + "/artifacts/bom.spdx.json"}, serviceTokenArtifact},
		{"sbom download commit by ID to file", []string{"sbom", "download", serviceTokenSBOMSHA, "--repo", serviceTokenRepoID, "--out", target}, []string{self, commit + "/artifacts/bom.cyclonedx.json"}, "Saved bom.cyclonedx.json for " + serviceTokenSBOMSHA[:12]},

		{"firewall list organization", []string{"firewall", "list"}, []string{self, "GET " + org + "/firewall/rules"}, "org-registry.example.test"},
		{"firewall list repository by name", []string{"firewall", "list", "--repo", "acme/api"}, []string{self, repos, "GET " + repo + "/firewall/rules"}, "repo-blocked.example.test"},
		{"firewall list repository by ID", []string{"firewall", "list", "--repo", serviceTokenRepoID}, []string{self, "GET " + repo + "/firewall/rules"}, "repo-blocked.example.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var violations []string
			h := newServiceTokenHarness(t, "", serviceTokenOpenAPI(&violations))
			if code := h.run(t, tc.args...); code != 0 {
				t.Fatal(code, h.stderr)
			}
			if len(violations) != 0 {
				t.Fatal("closed routes requested:", violations)
			}
			if n := strings.Count(strings.Join(h.requests, "\n"), self); n > 1 {
				t.Fatalf("self requested %d times", n)
			}
			if !slices.Equal(h.requests, tc.want) {
				t.Fatalf("requests:\n  %s\nwant:\n  %s", strings.Join(h.requests, "\n  "), strings.Join(tc.want, "\n  "))
			}
			if !strings.Contains(h.out.String(), tc.out) {
				t.Fatalf("missing %q in:\n%s", tc.out, h.out)
			}
		})
	}
}

// `avr firewall show` merges organization and repository rules through
// /orgs/{org_id}/repos/{repo_id}/firewall/resolved, which is closed to service
// tokens. It fails today; `avr firewall list --repo` is the working form.
func TestServiceTokenFirewallShowNeedsResolvedRoute(t *testing.T) {
	self := "GET /service-tokens/self"
	org := "/orgs/" + inspectionOrg
	resolved := "GET " + org + "/repos/" + serviceTokenRepoID + "/firewall/resolved"
	for _, tc := range []struct {
		repo string
		want []string
	}{
		{"acme/api", []string{self, "GET " + org + "/repos", resolved}},
		{serviceTokenRepoID, []string{self, resolved}},
	} {
		t.Run(tc.repo, func(t *testing.T) {
			var violations []string
			h := newServiceTokenHarness(t, "", serviceTokenOpenAPI(&violations))
			code := h.run(t, "firewall", "show", "--repo", tc.repo)
			message := h.stderr.String()
			if code != 1 || !strings.Contains(message, "(HTTP 404)") || !strings.Contains(message, "a service token reaches only what its access names") {
				t.Fatal(code, message)
			}
			if !slices.Equal(violations, []string{resolved}) || !slices.Equal(h.requests, tc.want) {
				t.Fatal(violations, h.requests)
			}
		})
	}
}
