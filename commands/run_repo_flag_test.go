package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

const runRepoFlagRepository = "rep-019a0000000070008000000000000002"

const runRepoFlagMissingRun = "run-019a00000000700080000000000000ff"

// runRepoFlagServer serves one run of acme/widgets and records list queries.
func runRepoFlagServer(t *testing.T, listed *[]string) *httptest.Server {
	t.Helper()
	run := map[string]any{
		"run_id": inspectionRun, "status": "completed", "conclusion": "success", "created_at": "2026-01-01T00:00:00Z",
		"repository_id": runRepoFlagRepository, "repository": map[string]any{"id": runRepoFlagRepository, "full_name": "acme/widgets"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any = []any{}
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/resolve"):
			data = map[string]any{"repository_id": runRepoFlagRepository, "full_name": r.URL.Query().Get("name")}
		case strings.HasSuffix(r.URL.Path, inspectionRun):
			data = run
		case strings.HasSuffix(r.URL.Path, runRepoFlagMissingRun):
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"detail": "Workflow run not found"})
			return
		case strings.HasSuffix(r.URL.Path, "/workflow-runs"):
			*listed = append(*listed, r.URL.Query().Get("repository_ids"))
			data = []any{run}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRunViewAndDiagnoseAcceptTheRunsRepository(t *testing.T) {
	for _, args := range [][]string{
		{"run", "view", inspectionRun, "--repo", "acme/widgets"},
		{"run", "view", inspectionRun, "-R", "Acme/Widgets"},
		{"run", "view", inspectionRun, "--repo", runRepoFlagRepository},
		{"run", "diagnose", inspectionRun, "--repo", "acme/widgets"},
		{"run", "diagnose", inspectionRun, "-R", runRepoFlagRepository, "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			d, stdout, stderr := testDeps(t)
			server := runRepoFlagServer(t, &[]string{})
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
			}
			if code := Execute(t.Context(), args, d); code != 0 {
				t.Fatal(code, stderr)
			}
			if stdout.Len() == 0 {
				t.Fatal(stderr)
			}
		})
	}
}

func TestRunViewAndDiagnoseRejectARunFromAnotherRepository(t *testing.T) {
	for _, tt := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"run", "view", inspectionRun, "--repo", "acme/other"}, 1, `belongs to repository 'acme/widgets', not --repo 'acme/other'`},
		{[]string{"run", "diagnose", inspectionRun, "--repo", "acme/other"}, 1, `belongs to repository 'acme/widgets', not --repo 'acme/other'`},
		{[]string{"run", "view", inspectionRun, "--repo", "rep-019a0000000070008000000000000099"}, 1, "not --repo 'rep-019a0000000070008000000000000099'"},
		{[]string{"run", "view", inspectionRun, "--repo", "widgets"}, 1, "invalid repository format"},
		{[]string{"run", "view", inspectionRun, "--repo", "rep-019A0000000070008000000000000002"}, 1, "invalid repository format"},
		{[]string{"run", "view", runRepoFlagMissingRun, "--repo", "widgets"}, 1, "invalid repository format"},
		{[]string{"run", "view", "--repo", "rep-typo"}, 1, "invalid repository format"},
		{[]string{"run", "diagnose", inspectionRun, "--repo", "rep-typo"}, 1, "invalid repository format"},
		{[]string{"run", "view", runRepoFlagMissingRun, "--repo", "acme/"}, 1, "invalid repository format"},
		{[]string{"run", "view", runRepoFlagMissingRun, "--repo", "/widgets"}, 1, "invalid repository format"},
		{[]string{"run", "diagnose", runRepoFlagMissingRun, "--repo", "acme/.."}, 1, "invalid repository format"},
		{[]string{"run", "view", "--repo", "acme/wid gets"}, 1, "invalid repository format"},
		{[]string{"run", "view", inspectionRun, "--repo", "group/sub/widgets"}, 1, `belongs to repository 'acme/widgets', not --repo 'group/sub/widgets'`},
		{[]string{"run", "view", inspectionRun, "--repo", ""}, 2, "--repo cannot be empty"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			d, stdout, stderr := testDeps(t)
			server := runRepoFlagServer(t, &[]string{})
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
			}
			if code := Execute(t.Context(), tt.args, d); code != tt.code {
				t.Fatal(code, stderr)
			}
			if !strings.Contains(stderr.String(), tt.want) || stdout.Len() != 0 {
				t.Fatal(stdout, stderr)
			}
		})
	}
}

func TestRunViewWithoutRunListsOnlyTheNamedRepository(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "view"}, ""},
		{[]string{"run", "view", "--repo", "acme/widgets"}, runRepoFlagRepository},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			d, stdout, stderr := testDeps(t)
			listed := []string{}
			server := runRepoFlagServer(t, &listed)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
			}
			if code := Execute(t.Context(), tt.args, d); code != 0 {
				t.Fatal(code, stderr)
			}
			if len(listed) != 1 || listed[0] != tt.want || !strings.Contains(stdout.String(), inspectionRun) {
				t.Fatal(listed, stdout)
			}
		})
	}
}
