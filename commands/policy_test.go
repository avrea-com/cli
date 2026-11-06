package commands

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
)

func TestLoginRejectsUnboundCallbacksBeforeValidOne(t *testing.T) {
	for _, provider := range []string{"github", "google", "gitlab", "saml"} {
		t.Run(provider, func(t *testing.T) {
			d, _, stderr := testDeps(t)
			d.LoginTimeout = 3 * time.Second
			requests := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/sso/discover":
					_, _ = fmt.Fprint(w, `{"method":"saml","saml_login_path":"/sso/acme/login"}`)
				case "/users/me/api-keys":
					requests++
					_, _ = fmt.Fprint(w, `{"api_key":"fixture-key"}`)
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
			d.OpenBrowser = func(ctx context.Context, target string) error {
				opened, _ := url.Parse(target)
				raw := opened.Query().Get("next")
				if provider == "google" {
					raw = opened.Query().Get("final_redirect")
				}
				callback, _ := url.Parse(raw)
				if callback.Host != "127.0.0.1:8765" || len(callback.Query().Get("cli_state")) < 26 {
					return fmt.Errorf("callback missing binding")
				}
				state := callback.Query().Get("cli_state")
				for _, tc := range []struct {
					host, path, query string
					status            int
				}{
					{"127.0.0.1:8765", "/callback", "session=attacker", 400},
					{"127.0.0.1:8765", "/callback", "session=attacker&cli_state=wrong", 400},
					{"attacker.test", "/callback", "session=attacker&cli_state=" + state, 400},
					{"127.0.0.1:8765", "/callback", "cli_state=" + state, 400},
					{"127.0.0.1:8765", "/other", "session=attacker&cli_state=" + state, 404},
					{"127.0.0.1:8765", "/callback", "session=valid&csrf_token=fixture&cli_state=" + state, 200},
				} {
					req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+listener.Addr().String()+tc.path+"?"+tc.query, nil)
					req.Host = tc.host
					resp, e := http.DefaultClient.Do(req)
					if e != nil {
						return e
					}
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					if resp.StatusCode != tc.status {
						return fmt.Errorf("callback status %d, expected %d", resp.StatusCode, tc.status)
					}
				}
				return nil
			}
			args := []string{"auth", "login", "--provider", provider}
			if provider == "saml" {
				args = []string{"auth", "login", "--email", "fixture@example.test"}
			}
			if code := Execute(t.Context(), args, d); code != 0 || requests != 1 {
				t.Fatal(code, requests, stderr.String())
			}
		})
	}
}

func TestCredentialPreflightPreventsLoginSideEffects(t *testing.T) {
	for _, command := range []string{"login", "device-login", "logout"} {
		d, _, stderr := testDeps(t)
		if err := os.WriteFile(d.Store.Path, []byte(`broken`), 0600); err != nil {
			t.Fatal(err)
		}
		calls := 0
		d.OpenBrowser = func(context.Context, string) error { calls++; return nil }
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("unexpected request") })}
		if code := Execute(t.Context(), []string{"auth", command}, d); code == 0 || calls != 0 || !strings.Contains(stderr.String(), "malformed") {
			t.Fatal(code, calls, stderr.String())
		}
	}
}

func TestEnvironmentStoreWarningAndMutationProtection(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.Store.Path = t.TempDir()
	env := map[string]string{"AVR_HOST": "https://api.test", "AVR_TOKEN": "fixture", "AVR_ORG": "expected"}
	d.Getenv = func(k string) string { return env[k] }
	if code := Execute(t.Context(), []string{"config", "get", "org"}, d); code != 0 || out.String() != "expected\n" || !strings.Contains(stderr.String(), "without stored defaults") {
		t.Fatal(code, out, stderr)
	}
	if err := d.Store.Set("https://api.test", "token", "replacement"); err == nil {
		t.Fatal("mutation bypassed unreadable store")
	}
}

func TestBootstrapSecretsRequirePinBeforeSSH(t *testing.T) {
	d, _, stderr := testDeps(t)
	err := runBootstrapSteps(t.Context(), d, map[string]any{}, []bootstrapStep{{name: "first"}, {name: "credentials", secret: true, stdin: "private-fixture"}}, "")
	if err == nil || !strings.Contains(err.Error(), "host key") || strings.Contains(stderr.String(), "[1/") || strings.Contains(err.Error(), "private-fixture") {
		t.Fatal(err, stderr)
	}
}

func TestDecodedDownloadCapAndCleanup(t *testing.T) {
	for _, length := range []int{31, 32, 33} {
		dir := t.TempDir()
		t.Setenv("TMPDIR", dir)
		t.Setenv("TMP", dir)
		t.Setenv("TEMP", dir)
		var compressed bytes.Buffer
		gz := gzip.NewWriter(&compressed)
		_, _ = gz.Write(bytes.Repeat([]byte("x"), length))
		_ = gz.Close()
		stream, err := gzip.NewReader(&compressed)
		if err != nil {
			t.Fatal(err)
		}
		file, count, err := spoolDownload(stream, 32)
		if length > 32 {
			if err == nil || file != nil || count != 33 {
				t.Fatal(file, count, err)
			}
		} else {
			if err != nil || count != int64(length) {
				t.Fatal(count, err)
			}
			raw, _ := io.ReadAll(file)
			if len(raw) != length {
				t.Fatal("spool not rewound")
			}
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatal("spool leaked")
		}
	}
}

func TestTerminalJQAndDeliberateFormatting(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.IsTerminal = func() bool { return true }
	d.ColorEnabled = func() bool { return true }
	evil := "line\n\x1b]52;c;payload\a\u202ereversed"
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.test", Token: "avs_fixture"}, nil
	}
	// A shared renderer preserves trusted styles and hyperlinks around escaped text.
	label := d.paint(output.CellText(evil), "4")
	if !strings.Contains(label, "\x1b[4m") || strings.Contains(label, "\x1b]52") {
		t.Fatal("underline/sanitization", label)
	}
	if err := output.Table(out, []string{"NAME"}, [][]string{{evil}}, false, [][]string{{"https://example.test/job"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b]8;;https://example.test/job") || strings.Contains(out.String(), "\x1b]52") {
		t.Fatal("link/sanitization", out)
	}
	for _, tty := range []bool{false, true} {
		out.Reset()
		if err := output.JSON(t.Context(), out, map[string]any{"text": evil}, []string{"text"}, ".text", tty); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "\x1b]52") == tty || !strings.Contains(out.String(), "line\n") {
			t.Fatal(tty, out, stderr)
		}
	}
}

func TestArtifactRejectsControlFilenames(t *testing.T) {
	for _, name := range []string{"bad\x1bname", "bad\x85name", "bad\u202ename"} {
		// Validation must reject the name before making a request.
		_, err := downloadArtifact(t.Context(), nil, "", map[string]any{"state": "complete", "size_bytes": json.Number("0"), "digest_sha256": strings.Repeat("0", 64), "file_name": name}, filepath.Join(t.TempDir(), "explicit"))
		if err == nil || !strings.Contains(err.Error(), "filename") {
			t.Fatal(err)
		}
	}
}

func TestUntrustedTerminalFieldsAndErrors(t *testing.T) {
	evil := "first\n\x1b]52;c;clipboard\a\x1b]0;title\a\u202elast"
	d, out, stderr := testDeps(t)
	setup := map[string]any{}
	for _, key := range []string{"group_full_path", "protected_branches", "fallback_image", "job_example", "documentation_url"} {
		setup[key] = evil
	}
	if err := renderGitLabRunners(d, map[string]any{"setup": setup}); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b\a\u202e") || !strings.Contains(out.String(), "first\n") {
		t.Fatal("GitLab setup controls/layout", out)
	}
	d.LoadConfig = func() (config.Config, error) { return config.Config{}, errors.New(evil) }
	if code := Execute(t.Context(), []string{"auth", "status"}, d); code == 0 || strings.ContainsAny(stderr.String(), "\x1b\a\u202e") || !strings.Contains(stderr.String(), "first\n") {
		t.Fatal("error controls/layout", code, stderr)
	}
}

func TestSAMLMetadataTerminalEscapingAndRawPipe(t *testing.T) {
	raw := "<metadata>\n\x1b]52;c;clipboard\a\u202e</metadata>\n"
	for _, tty := range []bool{false, true} {
		d, out, stderr := testDeps(t)
		d.IsTerminal = func() bool { return tty }
		d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
		}
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
			body := raw
			if r.URL.Path == "/users/me/organizations" {
				body = `{"data":[{"organization_id":"` + inspectionOrg + `","slug":"fixture"}]}`
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if code := Execute(t.Context(), []string{"org", "saml", "sp-metadata"}, d); code != 0 {
			t.Fatal(code, stderr)
		}
		if tty {
			if strings.ContainsAny(out.String(), "\x1b\a\u202e") || !strings.Contains(out.String(), "<metadata>\n") {
				t.Fatal("terminal controls/layout", out)
			}
		} else if out.String() != raw {
			t.Fatal("pipe changed", out)
		}
	}
}

func TestFirewallMutationResponseEscapesTerminalControls(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(`{"rule_id":"efr-\u001b]52;c;clipboard\u0007","position":1}`))}, nil
	})}
	if code := Execute(t.Context(), []string{"firewall", "add", "--action", "allow", "--fqdn", "example.test"}, d); code != 0 || strings.ContainsAny(out.String(), "\x1b\a") || !strings.Contains(out.String(), "Added rule") {
		t.Fatal(code, out, stderr)
	}
}
