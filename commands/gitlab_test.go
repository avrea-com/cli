package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
)

func TestGitLabWriteDeadlineAndRecovery(t *testing.T) {
	transport := &http.Client{Timeout: time.Second, Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) < 65*time.Second {
			t.Fatalf("GitLab server deadline not allowed: %v", deadline)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
	})}
	api := client.New(config.Config{Host: "https://api.example.test", Token: "fixture"}, transport, "fixture")
	if _, e := gitLabWrite(t.Context(), api, http.MethodPost, "/gitlab-fixture", map[string]any{"access_token": "private"}); e != nil {
		t.Fatal(e)
	}
	if api.HTTP.Timeout != time.Second || transport.Timeout != time.Second {
		t.Fatal("write changed caller timeout")
	}
	for _, expression := range []string{".installation_id", "error(.webhook_setup.secret)"} {
		d, out, stderr := testDeps(t)
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		row := map[string]any{"installation_id": "ins-fixture", "webhook_setup": map[string]any{"secret": "one-time-secret"}}
		e := gitLabCommitted(cmd, d, row, []string{"installation_id", "webhook_setup"}, expression, "The GitLab installation was registered")
		if e == nil || !strings.Contains(out.String(), "one-time-secret") || strings.Contains(e.Error()+stderr.String(), "one-time-secret") {
			t.Fatalf("lost or leaked credential: %v %s %s", e, out, stderr)
		}
		if !strings.Contains(e.Error(), "rather than retrying") {
			t.Fatal(e)
		}
	}
}
func TestGitLabCreatedRunnerUncertainAndRejected(t *testing.T) {
	for _, status := range []int{403, 422, 503} {
		err := gitLabRunnerPutError(&client.HTTPError{Status: status, Detail: "fixture denied"}, "42", "small")
		if !strings.Contains(err.Error(), "42") {
			t.Fatal("missing recovery ID", err)
		}
		if status >= 500 && !strings.Contains(err.Error(), "could not be confirmed") {
			t.Fatal(err)
		}
		if status < 500 && !strings.Contains(err.Error(), "not configured in Avrea") {
			t.Fatal(err)
		}
	}
}
func TestGitLabSecretReadsOnlyFirstLineAndNeverEchoes(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.In = strings.NewReader("  first-secret  \nsecond-secret\n")
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	secret, e := gitLabSecret(cmd, d, true, "GitLab token", "fixture")
	if e != nil || secret != "first-secret" || out.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("secret read failed %v", e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd.SetContext(ctx)
	d.Out = &bytes.Buffer{}
	e = gitLabCommitted(cmd, d, map[string]any{"webhook_setup": map[string]any{"secret": "recover-after-interrupt"}}, []string{"webhook_setup"}, ".", "The credentials were rotated")
	if e == nil || !strings.Contains(d.Out.(*bytes.Buffer).String(), "recover-after-interrupt") {
		t.Fatal("interrupt lost secret", e)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal(ctx.Err())
	}
}

func TestInstallationReadTransportErrorNamesAction(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture"}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private transport details") })}
	code := Execute(t.Context(), []string{"org", "install", "list"}, d)
	if code != 1 || stderr.String() != "Error: Could not reach Avrea while trying to fetch installations. Try again shortly.\n" {
		t.Fatal(code, stderr)
	}
}
