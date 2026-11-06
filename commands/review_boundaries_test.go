package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
)

func TestClosedPipePreservesCommandOutcome(t *testing.T) {
	for _, tc := range []struct {
		path []string
		gate bool
		want int
	}{
		{[]string{"run", "list"}, false, 0},
		{[]string{"run", "watch"}, false, 0},
		{[]string{"run", "watch"}, true, 1},
		{[]string{"workflow", "run"}, true, 1},
		{[]string{"repo", "clone"}, false, 1},
		{[]string{"auth", "login"}, false, 1},
		{[]string{"vm", "create"}, false, 1},
	} {
		t.Run(strings.Join(tc.path, " ")+"/"+fmt.Sprint(tc.gate), func(t *testing.T) {
			d, _, _ := testDeps(t)
			root := NewRoot(d)
			cmd, _, err := root.Find(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.gate {
				if err := cmd.Flags().Set("exit-status", "true"); err != nil {
					t.Fatal(err)
				}
			}
			for _, failure := range []error{io.ErrClosedPipe, &ExitError{7, "original outcome"}, context.Canceled} {
				err := run(func(*cobra.Command, []string) error { return failure })(cmd, nil)
				want := tc.want
				if !errors.Is(failure, io.ErrClosedPipe) {
					want = 7
					if errors.Is(failure, context.Canceled) {
						want = 130
					}
				}
				var exit *ExitError
				if !errors.As(err, &exit) || exit.Code != want {
					t.Fatal(err, want)
				}
			}
		})
	}
}

func TestMissingDetectedRepositoryStopsWatchAndJobList(t *testing.T) {
	for _, args := range [][]string{{"run", "watch", "--exit-status"}, {"job", "list"}, {"run", "list"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			d, out, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
			}
			d.GitRemote = func(context.Context) (string, error) { return "git@github.com:me/untracked.git", nil }
			calls := []string{}
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.Path)
				status, body := 200, `{"data":[],"pagination":{"next_cursor":null}}`
				if strings.HasSuffix(r.URL.Path, "/repos/resolve") {
					status, body = 404, `{"detail":{}}`
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			code := Execute(t.Context(), args, d)
			if args[0] == "run" && args[1] == "list" {
				if code != 0 || len(calls) != 2 || !strings.Contains(stderr.String(), "org-wide") {
					t.Fatal(code, calls, stderr)
				}
			} else if code != 1 || len(calls) != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "not in org") {
				t.Fatal(code, calls, out, stderr)
			}
		})
	}
}

func TestCommittedSecretsRemainFailuresWhenOutputCloses(t *testing.T) {
	for _, args := range [][]string{
		{"vm", "create", "--os", "linux", "--size", "2-vcpu"},
		{"vm", "create", "--os", "linux", "--size", "2-vcpu", "--json"},
		{"org", "install", "rotate", "--installation-id", "ins-fixture", "--access-token-stdin"},
		{"org", "install", "add", "--platform", "gitlab", "--group-full-path", "acme", "--project-selection", "all", "--access-token-stdin"},
	} {
		for _, writer := range []io.Writer{tokenBrokenWriter{}, &tokenFlushFailure{}} {
			d, _, stderr := testDeps(t)
			d.Out, d.In = writer, strings.NewReader("fixture-pat\n")
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
			}
			calls := 0
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				body := `{"data":{"webhook_setup":{"secret":"lost-secret"}}}`
				if args[0] == "vm" {
					body = `{"data":{"vm":{"customer_vm_id":"cvm-fixture","state":"PENDING"},"password":"lost-secret"}}`
				}
				return &http.Response{StatusCode: 201, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			code := Execute(t.Context(), args, d)
			message := stderr.String()
			if code != 1 || calls != 1 || !strings.Contains(message, "may be lost") || strings.Contains(message, "lost-secret") || strings.Contains(message, "fixture-pat") {
				t.Fatal(args, code, calls, message)
			}
		}
	}
}
