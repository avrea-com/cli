package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
)

func testDeps(t *testing.T) (Dependencies, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	return Dependencies{Store: &config.Store{Path: filepath.Join(t.TempDir(), "hosts.json")}, In: strings.NewReader(""), Out: out, Err: stderr, Getenv: func(string) string { return "" }, IsTerminal: func() bool { return false }}, out, stderr
}

func TestAuthBoundaries(t *testing.T) {
	for _, tt := range []struct {
		args []string
		code int
	}{
		{[]string{"auth", "status"}, 4},
		{[]string{"auth", "status", "--jq", ".email"}, 2},
		{[]string{"auth", "status", "--json", "unknown"}, 1},
		{[]string{"auth", "status", "--json", "?"}, 0},
		{[]string{"auth", "status", "--json"}, 2},
		{[]string{"auth", "status", "--unknown"}, 2},
		{[]string{"auth", "login", "--provider", "bad"}, 2},
		{[]string{"auth", "logout", "extra"}, 2},
	} {
		d, _, _ := testDeps(t)
		if got := Execute(context.Background(), tt.args, d); got != tt.code {
			t.Fatalf("%v: %d != %d", tt.args, got, tt.code)
		}
	}
}

func TestScopedAndTokenRedaction(t *testing.T) {
	for _, token := range []string{"short", "avr_fixture_private", "avs_fixture_private"} {
		for _, show := range []bool{false, true} {
			d, out, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) { return config.Config{Host: "https://api.test", Token: token}, nil }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `{"email":"fixture@example.test","token":"server-must-not-leak"}`)
			}))
			d.HTTP = server.Client()
			if !config.IsScoped(token) {
				d.LoadConfig = func() (config.Config, error) { return config.Config{Host: server.URL, Token: token}, nil }
			}
			args := []string{"auth", "status", "--json", "*"}
			if show {
				args = append(args, "--show-token")
			}
			code := Execute(context.Background(), args, d)
			server.Close()
			if code != 0 {
				t.Fatal(code, stderr.String())
			}
			var record map[string]any
			if err := json.Unmarshal(out.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if show {
				if record["token"] != token {
					t.Fatal(record)
				}
			} else if strings.Contains(out.String(), token) || strings.Contains(out.String(), "server-must-not-leak") {
				t.Fatal("secret leaked")
			}
		}
	}
}

func TestLoginExchange(t *testing.T) {
	for _, sso := range []bool{false, true} {
		t.Run(fmt.Sprint(sso), func(t *testing.T) {
			d, out, stderr := testDeps(t)
			var requests []string
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				if r.Header.Get("Authorization") != "" && r.Header.Get("Authorization") != "Bearer avr_fixture_new" {
					t.Error("old token used during login")
				}
				switch r.URL.Path {
				case "/sso/discover":
					if r.URL.RawQuery != "" {
						t.Error("email leaked to query")
					}
					_, _ = fmt.Fprint(w, `{"method":"saml","saml_login_path":"/sso/acme/login"}`)
				case "/users/me/api-keys":
					if r.Header.Get("Cookie") != "avrea_session=session-fixture" || r.Header.Get("X-CSRF-Token") != "csrf-fixture" {
						t.Error("lost session auth")
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["name"] != "CLI - test-host" || body["expires_days"] != float64(90) {
						t.Error(body)
					}
					_, _ = fmt.Fprint(w, `{"api_key":"avr_fixture_new"}`)
				case "/users/me/logout":
					if r.Header.Get("Cookie") == "" || r.Header.Get("X-CSRF-Token") == "" {
						t.Error("session not invalidated")
					}
					_, _ = fmt.Fprint(w, "{}")
				case "/users/me":
					_, _ = fmt.Fprint(w, `{"email":"fixture@example.test"}`)
				case "/users/me/organizations":
					_, _ = fmt.Fprint(w, `{"data":[{"organization_id":"org-one","slug":"only-org"}]}`)
				default:
					t.Errorf("unexpected route %s", r.URL.Path)
				}
			}))
			defer api.Close()
			d.LoadConfig = func() (config.Config, error) { return config.Config{Host: api.URL, Token: "avr_old_fixture"}, nil }
			d.HTTP = api.Client()
			d.Hostname = func() (string, error) { return "test-host", nil }
			listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			d.Listen = func(_ context.Context, _, address string) (net.Listener, error) {
				if address != "127.0.0.1:8765" {
					t.Error(address)
				}
				return listener, nil
			}
			d.OpenBrowser = func(ctx context.Context, target string) error {
				parsed, err := url.Parse(target)
				if err != nil {
					return err
				}
				expected := "/oauth/github/login"
				if sso {
					expected = "/sso/acme/login"
				}
				if parsed.Path != expected || parsed.Query().Get("next") != callbackURI {
					t.Error(target)
				}
				req, err := http.NewRequestWithContext(ctx, "GET", "http://"+listener.Addr().String()+"/callback?session=session-fixture&csrf_token=csrf-fixture", nil)
				if err != nil {
					return err
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					return err
				}
				defer func() { _ = resp.Body.Close() }()
				html, err := io.ReadAll(resp.Body)
				if err != nil {
					return err
				}
				if !bytes.Contains(html, []byte("fixture@example.test")) {
					t.Error("callback page lacks account")
				}
				return nil
			}
			args := []string{"auth", "login"}
			if sso {
				args = append(args, "--email", "fixture@example.test")
			}
			if code := Execute(context.Background(), args, d); code != 0 {
				t.Fatalf("%d: %s", code, stderr)
			}
			saved := d.Store.Read()
			if saved.Value(api.URL, "token") != "avr_fixture_new" || saved.Value(api.URL, "default_org") != "org-one" {
				t.Fatal(saved)
			}
			if !strings.Contains(out.String(), "Logged in as fixture@example.test") {
				t.Fatal(out)
			}
			expected := []string{"GET /users/me", "POST /users/me/api-keys", "POST /users/me/logout", "GET /users/me", "GET /users/me/organizations"}
			if sso {
				expected = append([]string{"POST /sso/discover"}, expected...)
			}
			if fmt.Sprint(requests) != fmt.Sprint(expected) {
				t.Fatalf("%v", requests)
			}
			// The callback listener must be released before the next invocation.
			retry, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			_ = retry.Close()
		})
	}
}

func TestLoginTimeoutCancellationAndBindFailure(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel", "occupied"} {
		t.Run(mode, func(t *testing.T) {
			d, _, stderr := testDeps(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var listener net.Listener
			d.Listen = func(ctx context.Context, _, _ string) (net.Listener, error) {
				if mode == "occupied" {
					return nil, errors.New("occupied")
				}
				var err error
				listener, err = (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
				return listener, err
			}
			d.LoginTimeout = 20 * time.Millisecond
			d.OpenBrowser = func(context.Context, string) error {
				if mode == "cancel" {
					cancel()
				}
				return nil
			}
			expected := 1
			if mode == "cancel" {
				expected = 130
			}
			if code := Execute(ctx, []string{"auth", "login"}, d); code != expected {
				t.Fatal(code, stderr)
			}
			if len(d.Store.Read().Order) != 0 {
				t.Fatal("stored credentials without login")
			}
			if listener != nil {
				again, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", listener.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
				_ = again.Close()
			}
		})
	}
}

func TestComposableCommandsAreIndependent(t *testing.T) {
	d1, out1, _ := testDeps(t)
	d2, out2, _ := testDeps(t)
	admin := &cobra.Command{Use: "avr-admin", SilenceErrors: true, SilenceUsage: true}
	admin.AddCommand(NewAuth(d1))
	admin.SetArgs([]string{"auth", "status", "--json", "?"})
	if err := admin.Execute(); err != nil {
		t.Fatal(err)
	}
	// Creating and mutating one tree must not mutate the next admin/public tree.
	public := NewRoot(d2)
	public.SetArgs([]string{"auth", "status", "--json", "?"})
	if err := public.Execute(); err != nil {
		t.Fatal(err)
	}
	if out1.String() != out2.String() {
		t.Fatal("shared constructor state")
	}
}

func TestGitHelperFailClosed(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.In = strings.NewReader("protocol=https\nhost=mirror.test\npath=owner/repo.git\n\n")
	args := []string{"auth", "git-credential", "--url", "https://mirror.test/owner/repo.git", "--repo", "owner/repo", "get"}
	if code := Execute(context.Background(), args, d); code != 4 || out.String() != "quit=1\n\n" {
		t.Fatal(code, out, stderr)
	}
	for _, raw := range []string{"http://remote.test/owner/repo.git", "https://user@mirror.test/owner/repo.git", "https://mirror.test:0443/owner/repo.git", "https://mirror.test/other.git", "https://mirror.test/owner/repo.git?x", "https://[::1%25en0]/owner/repo.git"} {
		if _, err := gitURL(raw, "owner/repo"); err == nil {
			t.Fatal("accepted unsafe URL", raw)
		}
	}
}

func TestCallbackCredentialsAndTerminalText(t *testing.T) {
	for _, cb := range []callback{{session: "bad\nvalue"}, {session: "ok", csrf: "bad\rvalue"}} {
		if _, err := sessionHeaders(cb); err == nil {
			t.Fatal("accepted control characters")
		}
	}
	for _, token := range []string{"a", "12345678", "123456789"} {
		masked := maskToken(token, false)
		if strings.Contains(masked, token) {
			t.Fatal("token leaked")
		}
	}
}

func TestTimingIsOptInAndKeepsJSONClean(t *testing.T) {
	for _, debug := range []string{"", "api", "timing"} {
		d, out, stderr := testDeps(t)
		d.Getenv = func(key string) string {
			switch key {
			case "AVR_DEBUG":
				return debug
			case "AVR_TOKEN":
				return "avs_fixture"
			}
			return ""
		}
		if code := Execute(t.Context(), []string{"auth", "status", "--json", "credential_type"}, d); code != 0 {
			t.Fatal(code, stderr)
		}
		var record map[string]any
		if err := json.Unmarshal(out.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if record["credential_type"] != "scoped_token" {
			t.Fatal(record)
		}
		hasTiming := strings.Contains(stderr.String(), "avr-timing ")
		if hasTiming != (debug == "timing") {
			t.Fatalf("debug=%q stderr=%q", debug, stderr.String())
		}
	}
}
