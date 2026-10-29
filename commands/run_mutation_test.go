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

func TestRerunPollingDeadlineAndCancellation(t *testing.T) {
	for _, cancelPoll := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelPoll), func(t *testing.T) {
			d, _, _ := testDeps(t)
			now := time.Now()
			d.Now = func() time.Time { return now }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			waits := 0
			d.Wait = func(ctx context.Context, delay time.Duration) error {
				waits++
				if cancelPoll {
					cancel()
				}
				now = now.Add(delay)
				return ctx.Err()
			}
			polls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				polls++
				if r.URL.Query().Get("platform_run_id") != "9007199254740993" {
					t.Error(r.URL)
				}
				if polls%2 == 1 {
					w.WriteHeader(503)
					_, _ = io.WriteString(w, "{}")
					return
				}
				_, _ = io.WriteString(w, `{"data":[{"run_attempt":1}]}`)
			}))
			defer server.Close()
			cfg := config.Config{Host: server.URL, Token: "avr_fixture"}
			result, err := pollRerun(ctx, d, cfg, client.New(cfg, nil, "fixture"), inspectionOrg, "9007199254740993", "1")
			if result != nil {
				t.Fatal(result)
			}
			if cancelPoll {
				if !errors.Is(err, context.Canceled) || waits != 1 {
					t.Fatal(err, waits)
				}
			} else if err != nil || polls != 30 || waits != 30 {
				t.Fatal(err, polls, waits)
			}
		})
	}
}

func TestRunCancelDeclineNeverMutates(t *testing.T) {
	d, _, stderr := testDeps(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++; _, _ = io.WriteString(w, "{}") }))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	d.In = strings.NewReader("n\n")
	if code := Execute(t.Context(), []string{"run", "cancel", inspectionRun}, d); code != 1 || requests != 0 || !strings.Contains(stderr.String(), "Aborted!") {
		t.Fatal(code, requests, stderr)
	}
}

type promptSignalWriter struct{ ready chan struct{} }

func (w promptSignalWriter) Write(b []byte) (int, error) {
	select {
	case w.ready <- struct{}{}:
	default:
	}
	return len(b), nil
}

func TestRunConfirmationCancellation(t *testing.T) {
	d, _, _ := testDeps(t)
	input, writer := io.Pipe()
	defer func() { _ = input.Close() }()
	defer func() { _ = writer.Close() }()
	ready := make(chan struct{}, 1)
	d.In, d.Out = input, promptSignalWriter{ready}
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Execute(ctx, []string{"run", "cancel", inspectionRun}, d) }()
	<-ready
	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		_ = writer.Close()
		<-done
		t.Fatal("confirmation ignored context cancellation")
	}
}
