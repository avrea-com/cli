package commands

import (
	"context"
	"errors"
	"fmt"
	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDispatchMatching(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		created, platform, expected string
		match                       bool
	}{
		{"2026-10-29T11:59:55Z", "42", "42", true},
		{"2026-10-29T11:59:54Z", "42", "42", false},
		{"2026-10-29T12:00:01Z", "43", "42", false},
		{"2026-10-29T12:00:01Z", "43", "", true},
		{"invalid", "42", "42", false},
	} {
		got := dispatchMatches(map[string]any{"created_at": test.created, "platform_workflow_id": test.platform}, now, test.expected)
		if got != test.match {
			t.Error(test, got)
		}
	}
}
func TestDispatchPollingBoundAndCancellation(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			d, _, _ := testDeps(t)
			d = d.defaults()
			now := time.Now()
			d.Now = func() time.Time { return now }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			waits := 0
			d.Wait = func(ctx context.Context, delay time.Duration) error {
				waits++
				now = now.Add(delay)
				if stop {
					cancel()
				}
				return ctx.Err()
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("repository_ids") != "rep-fixture" {
					t.Error(r.URL)
				}
				w.WriteHeader(429)
				_, _ = io.WriteString(w, "{}")
			}))
			defer server.Close()
			cfg := config.Config{Host: server.URL, Token: "avr_fixture"}
			row, err := pollDispatchedRun(ctx, d, cfg, client.New(cfg, nil, "fixture"), inspectionOrg, "rep-fixture", "42", now)
			if row != nil {
				t.Fatal(row)
			}
			if stop {
				if !errors.Is(err, context.Canceled) || waits != 1 {
					t.Fatal(err, waits)
				}
			} else if err != nil || calls != 90 || waits != 90 {
				t.Fatal(err, calls, waits)
			}
		})
	}
}

func TestWorkflowInputsBoundAndCancellation(t *testing.T) {
	if _, err := parseWorkflowInputs(t.Context(), strings.NewReader(strings.Repeat(" ", (16<<20)+1))); err == nil {
		t.Fatal("unbounded workflow input")
	}
	input, writer := io.Pipe()
	defer func() { _ = input.Close(); _ = writer.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := parseWorkflowInputs(ctx, input); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("workflow input ignored cancellation")
	}
}

func TestWorkflowInputsRejectTerminalBeforeNetwork(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.IsInputTerminal = func() bool { return true }
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	code := Execute(t.Context(), []string{"workflow", "run", "build.yml", "--json"}, d)
	if code != 1 || !strings.Contains(stderr.String(), "Pipe a JSON object") {
		t.Fatal(code, stderr)
	}
}
