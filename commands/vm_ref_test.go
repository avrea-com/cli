package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

var vmRefCommit = strings.Repeat("ab", 20)

// vm create sends --ref in the form the API stores for each ref it preloads.
func TestVMCreateSendsEachPreloadRefForm(t *testing.T) {
	for _, tc := range []struct{ ref, want string }{
		{"main", "main"},
		{"feature/x-1", "feature/x-1"},
		{"refs/heads/feature/x-1", "feature/x-1"},
		{"refs/pull/12/head", "refs/pull/12/head"},
		{" refs/pull/1/head ", "refs/pull/1/head"},
		{strings.ToUpper(vmRefCommit), vmRefCommit},
		{vmRefCommit, vmRefCommit},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			d, _, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
			}
			sent := []any{}
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				sent = append(sent, body["ref"])
				return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(`{"data":{"vm":{"customer_vm_id":"cvm-fixture"},"password":null}}`))}, nil
			})}
			code := Execute(t.Context(), []string{"vm", "create", "--os", "linux", "--size", "2-vcpu", "--repo", "owner/repo", "--ref", tc.ref, "--json"}, d)
			if code != 0 || len(sent) != 1 || sent[0] != tc.want {
				t.Fatal(code, sent, stderr.String())
			}
		})
	}
}

// Refs the API does not preload fail locally, before any request.
func TestVMRefRefusesWhatTheAPIDoesNotPreload(t *testing.T) {
	for _, ref := range []string{
		"refs/tags/v1",
		"refs/pull/12/merge",
		"refs/pull/0/head",
		"refs/pull/12345678901/head",
		"refs/heads/" + vmRefCommit,
		"a..b",
		"-main",
		"refs/heads/",
	} {
		for _, command := range [][]string{
			{"vm", "create", "--os", "linux", "--size", "2-vcpu", "--repo", "owner/repo", "--ref", ref},
			{"vm", "bootstrap", "cvm-fixture", "--repo", "https://example.test/owner/repo.git", "--ref", ref, "--print"},
		} {
			t.Run(command[1]+" "+ref, func(t *testing.T) {
				d, out, stderr := testDeps(t)
				d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
					t.Fatal("sent a request")
					return nil, nil
				})}
				if code := Execute(t.Context(), command, d); code != 2 || !strings.Contains(stderr.String(), "is not a branch name, refs/pull/<number>/head or a 40-character commit SHA") {
					t.Fatal(code, out.String(), stderr.String())
				}
			})
		}
	}
}

// The bootstrap repo step checks out each ref form from a real remote: a
// branch by name, and a pull request head or a commit only a pull request
// ref reaches, both detached.
func TestBootstrapRepoStepChecksOutEachRefForm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("VM bootstrap uses POSIX shells")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test")
		raw, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(args, err, string(raw))
		}
		return strings.TrimSpace(string(raw))
	}
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	git(work, "init", "-q", "-b", "main")
	git(work, "commit", "-q", "--allow-empty", "-m", "base")
	git(work, "branch", "feature")
	git(work, "checkout", "-q", "-b", "contribution")
	git(work, "commit", "-q", "--allow-empty", "-m", "pull request head")
	head := git(work, "rev-parse", "HEAD")
	git(work, "checkout", "-q", "main")
	origin := filepath.Join(root, "repo.git")
	git(root, "clone", "-q", "--bare", work, origin)
	// Only refs/pull/7/head reaches the pull request's commit, as on GitHub.
	git(origin, "update-ref", "refs/pull/7/head", head)
	git(origin, "branch", "-D", "contribution")
	git(origin, "config", "uploadpack.allowReachableSHA1InWant", "true")
	base := git(origin, "rev-parse", "main")

	for _, tc := range []struct{ ref, want, branch string }{
		{"", base, "main"},
		{"feature", base, "feature"},
		{"refs/pull/7/head", head, ""},
		{head, head, ""},
	} {
		t.Run("ref "+tc.ref, func(t *testing.T) {
			home := t.TempDir()
			steps, err := bootstrapSteps(bootstrapOptions{repo: "file://" + origin, ref: tc.ref}, bootstrapEnvironment{}, nil, "", nil)
			if err != nil || len(steps) != 1 {
				t.Fatal(err, len(steps))
			}
			// Run twice: the second run takes the already-cloned path.
			for range 2 {
				cmd := exec.CommandContext(t.Context(), "bash", "-c", steps[0].script)
				cmd.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
				if raw, err := cmd.CombinedOutput(); err != nil {
					t.Fatal(err, string(raw))
				}
			}
			dest := filepath.Join(home, "repo")
			if got := git(dest, "rev-parse", "HEAD"); got != tc.want {
				t.Fatalf("HEAD %s; want %s", got, tc.want)
			}
			branch := ""
			if tc.branch != "" {
				branch = git(dest, "symbolic-ref", "--short", "HEAD")
			} else if out, err := exec.CommandContext(t.Context(), "git", "-C", dest, "symbolic-ref", "-q", "HEAD").Output(); err == nil {
				t.Fatalf("not detached: %s", out)
			}
			if branch != tc.branch {
				t.Fatalf("branch %q; want %q", branch, tc.branch)
			}
		})
	}
}
