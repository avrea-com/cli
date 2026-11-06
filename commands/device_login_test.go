package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
)

const deviceTestSecret = "private-device-secret"

var deviceTestKey = "avk_" + strings.Repeat("a", 43)

type deviceTestReply struct {
	status int
	data   any
	retry  string
}
type deviceTestFlow struct {
	mu                          sync.Mutex
	d                           Dependencies
	start, metadata             map[string]any
	replies                     []deviceTestReply
	polls, waits                []time.Duration
	now                         time.Time
	startDuration, pollDuration time.Duration
	requests                    []string
	host                        string
}

func newDeviceTestFlow(t *testing.T) *deviceTestFlow {
	t.Helper()
	d, _, _ := testDeps(t)
	f := &deviceTestFlow{d: d, now: time.Unix(0, 0)}
	f.start = map[string]any{"device_code": deviceTestSecret, "user_code": "BCDF-GHJK", "verification_uri": "https://console.example.com/device", "verification_uri_complete": "https://console.example.com/device?user_code=BCDF-GHJK", "expires_in": 600, "interval": 5}
	f.replies = []deviceTestReply{{200, map[string]any{"access_token": deviceTestKey, "token_type": "Bearer", "expires_in": 7776000, "scope": "avrea"}, ""}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/oauth/") || strings.HasPrefix(r.URL.Path, "/.well-known/") {
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Error("existing credentials leaked into device flow")
			}
			if r.Header.Get("User-Agent") == "" {
				t.Error("missing user agent")
			}
		}
		var data any
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			data = f.metadata
		case "/oauth/device_authorization":
			if r.Method != "POST" {
				t.Error("wrong authorization method")
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if len(r.PostForm) != 2 || r.PostForm.Get("client_id") != "avr-cli" || r.PostForm.Get("scope") != "avrea" {
				t.Error("wrong device request", r.PostForm)
			}
			f.now = f.now.Add(f.startDuration)
			data = f.start
		case "/oauth/token":
			if r.Method != "POST" {
				t.Error("wrong token method")
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if len(r.PostForm) != 3 || r.PostForm.Get("client_id") != "avr-cli" || r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" || r.PostForm.Get("device_code") != deviceTestSecret {
				t.Error("wrong token request")
			}
			f.polls = append(f.polls, f.now.Sub(time.Unix(0, 0)))
			f.now = f.now.Add(f.pollDuration)
			if len(f.replies) == 0 {
				t.Error("unexpected additional poll")
				w.WriteHeader(400)
				return
			}
			reply := f.replies[0]
			f.replies = f.replies[1:]
			if reply.status == 0 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
				return
			}
			if reply.retry != "" {
				w.Header().Set("Retry-After", reply.retry)
			}
			w.WriteHeader(reply.status)
			data = reply.data
		case "/users/me":
			if r.Header.Get("Authorization") != "Bearer "+deviceTestKey {
				t.Error("new key not used")
			}
			data = map[string]any{"email": "fixture@example.test"}
		case "/users/me/organizations":
			data = map[string]any{"data": []any{map[string]any{"organization_id": "org-fixture", "slug": "fixture"}}}
		default:
			t.Error("unexpected request", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if err := json.NewEncoder(w).Encode(data); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	f.host = server.URL
	f.metadata = map[string]any{"issuer": server.URL, "device_authorization_endpoint": server.URL + "/oauth/device_authorization", "token_endpoint": server.URL + "/oauth/token"}
	f.d.HTTP = server.Client()
	f.d.LoadConfig = func() (config.Config, error) { return config.Config{Host: server.URL, Token: "old-secret"}, nil }
	f.d.Now = func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
	f.d.Wait = func(ctx context.Context, delay time.Duration) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.waits = append(f.waits, delay)
		f.now = f.now.Add(delay)
		return ctx.Err()
	}
	f.d.OpenBrowser = func(context.Context, string) error { t.Error("device login opened browser"); return nil }
	return f
}

func TestDeviceLoginStoresCredentialAndPinsOnlyOrganization(t *testing.T) {
	f := newDeviceTestFlow(t)
	f.replies = append([]deviceTestReply{{400, map[string]any{"error": "authorization_pending"}, ""}}, f.replies...)
	if code := Execute(t.Context(), []string{"auth", "device-login", "--no-qr"}, f.d); code != 0 {
		t.Fatal(code, f.d.Err)
	}
	if got := f.d.Store.Read().Value(f.host, "token"); got != deviceTestKey {
		t.Fatal("key not saved")
	}
	if got := f.d.Store.Read().Value(f.host, "default_org"); got != "org-fixture" {
		t.Fatal("org not pinned", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !reflect.DeepEqual(f.polls, []time.Duration{5 * time.Second, 10 * time.Second}) {
		t.Fatal(f.polls)
	}
	text := fmt.Sprint(f.d.Out, f.d.Err)
	if strings.Contains(text, deviceTestSecret) || strings.Contains(text, deviceTestKey) {
		t.Fatal("secret leaked")
	}
	if !strings.Contains(text, "Logged in as fixture@example.test") || !strings.Contains(text, "Default organization: fixture (org-fixture)") {
		t.Fatal(text)
	}
}

func TestDevicePollingBackoffAndDeadline(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		before                       []deviceTestReply
		expires, startTime, pollTime int
		want                         []time.Duration
		errorText                    string
	}{
		{"slow down persists", []deviceTestReply{{400, map[string]any{"error": "slow_down"}, ""}, {400, map[string]any{"error": "authorization_pending"}, ""}, {400, map[string]any{"error": "slow_down"}, ""}}, 600, 0, 0, []time.Duration{5, 15, 25, 40}, ""},
		{"network backoff resets", []deviceTestReply{{0, nil, ""}, {400, map[string]any{"error": "authorization_pending"}, ""}}, 600, 0, 0, []time.Duration{5, 15, 20}, ""},
		{"network exponential", []deviceTestReply{{0, nil, ""}, {0, nil, ""}}, 600, 0, 0, []time.Duration{5, 15, 35}, ""},
		{"server retry after", []deviceTestReply{{503, nil, "12"}, {429, nil, "7"}}, 600, 0, 0, []time.Duration{5, 17, 24}, ""},
		{"no poll at expiry", nil, 5, 0, 0, nil, deviceExpired},
		{"startup uses lifetime", nil, 10, 6, 0, nil, deviceExpired},
		{"eligible poll may finish", nil, 10, 0, 8, []time.Duration{5}, ""},
		{"network expiry", []deviceTestReply{{0, nil, ""}}, 10, 0, 0, []time.Duration{5}, "Could not complete device login before expiry"},
		{"server expiry", []deviceTestReply{{503, nil, "99999999999999999999999999999"}}, 600, 0, 0, []time.Duration{5}, "HTTP 503"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDeviceTestFlow(t)
			f.start["expires_in"] = tc.expires
			f.startDuration = time.Duration(tc.startTime) * time.Second
			f.pollDuration = time.Duration(tc.pollTime) * time.Second
			f.replies = append(tc.before, f.replies...)
			_, _, api, err := f.d.defaults().load()
			if err != nil {
				t.Fatal(err)
			}
			key, err := deviceLogin(t.Context(), f.d.defaults(), api, false)
			if tc.errorText == "" {
				if err != nil || key != deviceTestKey {
					t.Fatal(key, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.errorText) {
				t.Fatal(err)
			}
			want := append([]time.Duration(nil), tc.want...)
			for i := range want {
				want[i] *= time.Second
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if !reflect.DeepEqual(f.polls, want) {
				t.Fatalf("polls %v want %v", f.polls, want)
			}
		})
	}
}

func TestDeviceValidationPreservesExistingCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		value       any
		target      string
	}{
		{"empty device code", "device_code", "", "start"}, {"missing device code", "device_code", nil, "start"},
		{"unicode code", "user_code", "BCDF-é", "start"}, {"terminal code", "user_code", "secret\x1b[0m", "start"},
		{"long code", "user_code", strings.Repeat("a", 33), "start"}, {"boolean expiry", "expires_in", true, "start"},
		{"float expiry", "expires_in", 1.5, "start"}, {"long expiry", "expires_in", 86401, "start"}, {"zero interval", "interval", 0, "start"},
		{"string interval", "interval", "5", "start"}, {"unsafe link", "verification_uri", "http://evil.test/device", "start"},
		{"origin mismatch", "verification_uri_complete", "https://evil.test/device", "start"},
		{"issuer mismatch", "issuer", "https://evil.test", "metadata"}, {"token exfiltration", "token_endpoint", "https://evil.test/token", "metadata"},
		{"credentialed endpoint", "device_authorization_endpoint", "https://user:secret@api.test/device", "metadata"},
		{"bad token", "access_token", "server-secret", "token"}, {"wrong scope", "scope", "other", "token"},
		{"wrong type", "token_type", "Basic", "token"}, {"string expiry", "expires_in", "60", "token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDeviceTestFlow(t)
			switch tc.target {
			case "start":
				f.start[tc.field] = tc.value
			case "metadata":
				f.metadata[tc.field] = tc.value
			case "token":
				f.replies[0].data.(map[string]any)[tc.field] = tc.value
			}
			if err := f.d.Store.Set(f.host, "token", "old-secret"); err != nil {
				t.Fatal(err)
			}
			if code := Execute(t.Context(), []string{"auth", "device-login", "--no-qr"}, f.d); code != 1 {
				t.Fatal(code, f.d.Err)
			}
			if f.d.Store.Read().Value(f.host, "token") != "old-secret" {
				t.Fatal("old credential overwritten")
			}
			text := fmt.Sprint(f.d.Out, f.d.Err)
			for _, secret := range []string{deviceTestSecret, deviceTestKey, "server-secret", "old-secret"} {
				if strings.Contains(text, secret) {
					t.Fatal("secret leaked")
				}
			}
		})
	}
}

func TestDeviceSafeURLs(t *testing.T) {
	for _, value := range []string{"http://api.example.com", "https://user:pass@api.test", "https://api.test/#fragment", "https://api.test/\n", "https://api.test/\u00a0", "https://api.test:65536", "//api.test", "file:///tmp/token"} {
		if _, err := deviceSafeURL(value, false); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, value := range []string{"https://api.test/token", "http://localhost:80", "http://127.0.0.1:1234", "http://[::1]:1234"} {
		if _, err := deviceSafeURL(value, false); err != nil {
			t.Errorf("rejected %q: %v", value, err)
		}
	}
	left, _ := deviceSafeURL("https://API.TEST:443/x", false)
	right, _ := deviceSafeURL("https://api.test/y", false)
	if deviceOrigin(left) != deviceOrigin(right) {
		t.Fatal("default port or case mismatch")
	}
}

func TestDeviceCancellationDoesNotPollOrSave(t *testing.T) {
	f := newDeviceTestFlow(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.d.Wait = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	_, _, api, err := f.d.defaults().load()
	if err != nil {
		t.Fatal(err)
	}
	_, err = deviceLogin(ctx, f.d.defaults(), api, false)
	f.mu.Lock()
	defer f.mu.Unlock()
	if !errors.Is(err, context.Canceled) || len(f.polls) != 0 {
		t.Fatal(err, f.polls)
	}
}

func TestDeviceRedirectIsNotFollowed(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Redirect(w, r, "/secret", http.StatusFound)
	}))
	defer server.Close()
	d, _, _ := testDeps(t)
	d.HTTP = server.Client()
	d.LoadConfig = func() (config.Config, error) { return config.Config{Host: server.URL}, nil }
	if code := Execute(t.Context(), []string{"auth", "device-login"}, d); code != 1 || requests != 1 {
		t.Fatal(code, requests, d.Err)
	}
}

func TestDeviceQRPresentation(t *testing.T) {
	if got := deviceQRText([][]bool{{false, false, true, true}, {false, true, false, true}}); got != "\x1b[37m\x1b[40m█▀▄ \x1b[0m\n" {
		t.Fatal(got)
	}
	for _, mode := range []string{"pipe", "dumb", "narrow", "no-color", "encoding", "terminal"} {
		t.Run(mode, func(t *testing.T) {
			d, out, _ := testDeps(t)
			d.IsTerminal = func() bool { return mode != "pipe" }
			d.Getenv = func(key string) string {
				switch {
				case key == "TERM" && mode == "dumb":
					return "dumb"
				case key == "COLUMNS" && mode == "narrow":
					return "20"
				case key == "PYTHONIOENCODING" && mode == "encoding":
					return "ascii"
				}
				return ""
			}
			d.ColorEnabled = func() bool { return mode != "no-color" }
			if err := showDeviceQR(d, "https://console.example.com/device?user_code=BCDF-GHJK"); err != nil {
				t.Fatal(err)
			}
			if (out.Len() > 0) != (mode == "terminal") {
				t.Fatal(out.String())
			}
		})
	}
}

func TestDeviceInvalidJSON(t *testing.T) {
	for _, body := range []string{"{", `{"issuer":"secret"} {}`, `["secret"]`, strings.Repeat("x", (1<<20)+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
		c := deviceClient{http: server.Client(), userAgent: "avr/test"}
		r, err := c.request(t.Context(), server.URL, nil)
		server.Close()
		if err != nil || r.fields != nil {
			t.Fatal(err, "accepted malformed body")
		}
	}
}

func TestDeviceCompletionSavesBeforeOptionalRequests(t *testing.T) {
	d, out, stderr := testDeps(t)
	cfg := config.Config{Host: "https://api.example.test"}
	requests := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if d.Store.Read().Value(cfg.Host, "token") != deviceTestKey {
			t.Error("profile requested before credential saved")
		}
		return nil, errors.New("private-transport-secret")
	})}
	api := client.New(cfg, d.HTTP, "avr/test")
	if err := completeLogin(t.Context(), d.defaults(), cfg, d.Store, api, deviceTestKey); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || !strings.Contains(out.String(), "Authentication complete.") || strings.Contains(stderr.String(), "private-transport-secret") {
		t.Fatal(requests, out, stderr)
	}
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &config.Store{Path: filepath.Join(path, "hosts.json")}
	err := completeLogin(t.Context(), d.defaults(), cfg, store, api, deviceTestKey)
	if err == nil || !strings.Contains(err.Error(), "Could not save credentials to "+store.Path) || !strings.Contains(err.Error(), "avr auth device-login or avr auth login again") || requests != 2 {
		t.Fatal(err, requests)
	}
}

func TestDeviceDefaultIntervalAndManualCode(t *testing.T) {
	f := newDeviceTestFlow(t)
	delete(f.start, "interval")
	delete(f.start, "verification_uri_complete")
	if code := Execute(t.Context(), []string{"auth", "device-login", "--no-qr"}, f.d); code != 0 {
		t.Fatal(code, f.d.Err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !reflect.DeepEqual(f.polls, []time.Duration{5 * time.Second}) || !strings.Contains(fmt.Sprint(f.d.Out), "Code: BCDF-GHJK") {
		t.Fatal(f.polls, f.d.Out)
	}
}

func TestDeviceTransportErrorsNeverExposeSecrets(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) { return config.Config{Host: "https://api.example.test"}, nil }
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New(deviceTestSecret) })}
	if code := Execute(t.Context(), []string{"auth", "device-login"}, d); code != 1 || strings.Contains(stderr.String(), deviceTestSecret) || !strings.Contains(stderr.String(), "Could not reach Avrea while trying to sign in with device login") {
		t.Fatal(code, stderr)
	}
}
