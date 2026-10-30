package commands

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

func TestInstallationWaitBudgetAndCancellation(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		d, out, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
		}
		now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
		d.Now = func() time.Time { return now }
		ctx, cancel := context.WithCancel(t.Context())
		waits, calls := 0, 0
		d.Wait = func(ctx context.Context, duration time.Duration) error {
			waits++
			if duration != 5*time.Second {
				t.Fatal(duration)
			}
			if interrupt {
				cancel()
				return ctx.Err()
			}
			now = now.Add(duration)
			return nil
		}
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != http.MethodGet || r.URL.Path != "/users/me/installations" {
				t.Fatal(r.Method, r.URL)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader("{\"data\":[{\"platform_installation_id\":\"42\"}]}"))}, nil
		})}
		code := Execute(ctx, []string{"org", "install", "add", "--no-browser", "--wait-seconds", "1"}, d)
		cancel()
		if waits != 1 {
			t.Fatal(waits)
		}
		if interrupt {
			if code != 130 || calls != 1 {
				t.Fatal(code, calls, out, stderr)
			}
		} else if code != 0 || calls != 2 || !strings.Contains(stderr.String(), "Installation not detected yet.") {
			t.Fatal(code, calls, out, stderr)
		}
	}
}
func TestInstallationRemoveCancellationPreventsSuspension(t *testing.T) {
	d, _, stderr := testDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.LoadConfig = func() (config.Config, error) {
		cancel()
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("cancelled suspension reached API")
		return nil, nil
	})}
	code := Execute(ctx, []string{"org", "install", "remove", "--installation-id", "ins-019a0000000070008000000000000004", "--yes"}, d)
	if code != 130 {
		t.Fatal(code, stderr)
	}
}
