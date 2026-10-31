package commands

import (
	"context"
	"fmt"
	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginConsumesCallbackWhileBrowserRemainsOpen(t *testing.T) {
	d, _, stderr := testDeps(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/me/api-keys":
			_, _ = fmt.Fprint(w, `{"api_key":"avr_fixture"}`)
		case "/users/me":
			_, _ = fmt.Fprint(w, `{"email":"fixture@example.test"}`)
		default:
			_, _ = fmt.Fprint(w, `{}`)
		}
	}))
	defer api.Close()
	d.LoadConfig = func() (config.Config, error) { return config.Config{Host: api.URL}, nil }
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	d.Listen = func(context.Context, string, string) (net.Listener, error) { return listener, nil }
	d.LoginTimeout = 500 * time.Millisecond
	release := make(chan struct{})
	exited := make(chan struct{})
	d.OpenBrowser = func(ctx context.Context, _ string) error {
		defer close(exited)
		defer func() { <-release }()
		req, err := http.NewRequestWithContext(ctx, "GET", "http://"+listener.Addr().String()+"/callback?session=fixture&csrf_token=fixture", nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		_, err = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return err
	}
	done := make(chan int, 1)
	go func() { done <- Execute(t.Context(), []string{"auth", "login"}, d) }()
	defer func() { close(release); <-exited }()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code, stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Error("login waited for browser process to exit")
	}
}

func TestRemoteGroupedFlagsDoNotConsumeInheritedShorthands(t *testing.T) {
	root := &cobra.Command{Use: "avr"}
	root.PersistentFlags().BoolP("verbose", "v", false, "")
	c := &cobra.Command{Use: "ssh"}
	root.AddCommand(c)
	c.Flags().StringP("identity", "i", "", "")
	c.Flags().AddFlagSet(c.InheritedFlags())
	for _, grouped := range []string{"-lv", "-rv", "-xyzv"} {
		got, err := vmSSHPassthrough(c, []string{"cvm-fixture", "command", grouped})
		if err != nil || strings.Join(got, " ") != "cvm-fixture command "+grouped {
			t.Fatal(got, err)
		}
		verbose, _ := c.Flags().GetBool("verbose")
		if verbose {
			t.Fatal("remote option enabled global tracing")
		}
	}
}

func TestTimestampCommonISOForms(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"2026-10-01T10:00", "2026-10-01T10:00:00"},
		{"2026-10-01T10", "2026-10-01T10:00:00"},
		{"2026-10-01 10:00:00+02:00", "2026-10-01T10:00:00+02:00"},
		{"2026-10-01T10:00:00+0200", "2026-10-01T10:00:00+02:00"},
		{"20261001", "2026-10-01T00:00:00"},
		{"2026-10-01T10:00:00.1+0200", "2026-10-01T10:00:00.100000+02:00"},
		{"20261001T100000,1234567Z", "2026-10-01T10:00:00.123456+00:00"},
	} {
		got, err := normalizeTimestamp(tc.in)
		if err != nil || got != tc.want {
			t.Fatal(tc.in, got, err)
		}
	}
	for _, in := range []string{"2026-02-30", "2026-10-01T25:00", "2026-10-01T10:00+2400", "2026-10-01T10:00+0260", "yesterday"} {
		if _, err := normalizeTimestamp(in); err == nil {
			t.Fatal("accepted", in)
		}
	}
}

func TestCompletionNotificationUsesOnlyTerminalStderr(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		d, out, stderr := testDeps(t)
		d.IsErrorTerminal = func() bool { return terminal }
		notifyCompletion(d, "Build: success")
		want := ""
		if terminal {
			want = "\x1b]9;Build: success\a"
		}
		if stderr.String() != want || out.Len() != 0 {
			t.Fatal(out, stderr)
		}
		stderr.Reset()
		notifyCompletion(d, "untrusted\a\x1b]9;forged")
		if strings.Contains(stderr.String(), "untrusted\a") {
			t.Fatal("unescaped control in notification")
		}
	}
}
