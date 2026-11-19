package commands

import (
	"compress/gzip"
	"github.com/avrea-com/cli/config"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPICommand(t *testing.T) {
	for _, tc := range []struct {
		args                              []string
		method, uri, body, response, want string
		status, code                      int
	}{
		{[]string{"x%2Fy?q=0", "-X", "GET", "-f", "q=1", "-F", "q=2", "-q", ".[0]"}, "GET", "/x%2Fy?q=0&q=1&q=2", "", "[123]", "123\n", 200, 0},
		{[]string{"/x", "-F", "a=true", "-f", "b=true"}, "POST", "/x", `{"a":true,"b":"true"}`, "ok", "ok", 200, 0},
		{[]string{"x", "--silent"}, "GET", "/x", "", "", "", 204, 0},
		{[]string{"x", "-q", ".ok"}, "GET", "/x", "", "invalid", "invalid", 400, 1},
		{[]string{"x"}, "GET", "/x", "", "redirect", "redirect", 302, 1},
		{args: []string{"x", "-F", "a=true", "-q", "["}, code: 2},
		{args: []string{"//elsewhere/x"}, code: 2},
		{args: []string{"https://elsewhere/x"}, code: 2},
		{args: []string{"x", "-H", "Accept-Encoding: br"}, code: 2},
		{[]string{"x", "-X", "DELETE", "-q", ".id"}, "DELETE", "/x", "", "", "", 204, 0},
		{[]string{"users:me/x"}, "GET", "/users:me/x", "", "ok", "ok", 200, 0},
		{[]string{"x"}, "GET", "/x", "", "denied", "denied", 401, 4},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			d, out, stderr := testDeps(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status == 0 {
					t.Error("invalid arguments sent a request")
					return
				}
				b, e := io.ReadAll(r.Body)
				if e != nil || r.Method != tc.method || r.RequestURI != tc.uri || string(b) != tc.body || r.Header.Get("Authorization") != "Bearer token" {
					t.Errorf("unexpected request %s %s %q", r.Method, r.RequestURI, b)
				}
				if tc.status == 302 {
					w.Header().Set("Location", "/unexpected")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.response)
			}))
			defer srv.Close()
			d.HTTP = srv.Client()
			d.LoadConfig = func() (config.Config, error) { return config.Config{Host: srv.URL, Token: "token"}, nil }
			code := Execute(t.Context(), append([]string{"api"}, tc.args...), d)
			if code != tc.code || out.String() != tc.want || (stderr.Len() > 0) != (tc.code != 0) {
				t.Fatalf("code %d stdout %q stderr %q", code, out, stderr)
			}
		})
	}
}

func TestAPICommandAuthentication(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.WriteHeader(401)
	}))
	defer srv.Close()
	d, out, stderr := testDeps(t)
	d.HTTP = srv.Client()
	d.LoadConfig = func() (config.Config, error) { return config.Config{Host: srv.URL}, nil }
	if code := Execute(t.Context(), []string{"api", "x"}, d); code != 4 || len(seen) != 0 || !strings.Contains(stderr.String(), "avr auth device-login") {
		t.Fatalf("code %d requests %q stdout %q stderr %q", code, seen, out, stderr)
	}
	stderr.Reset()
	// The caller's own credential is sent as-is, and its rejection is not ours to explain.
	if code := Execute(t.Context(), []string{"api", "x", "-H", "authorization: Bearer mine"}, d); code != 1 || len(seen) != 1 || seen[0] != "Bearer mine" || !strings.Contains(stderr.String(), "HTTP 401") {
		t.Fatalf("code %d requests %q stderr %q", code, seen, stderr)
	}
}

func TestAPICommandDecodedHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("X-Kept", "1")
		gz := gzip.NewWriter(w)
		_, _ = io.WriteString(gz, `{"ok":true}`)
		_ = gz.Close()
	}))
	defer srv.Close()
	d, out, stderr := testDeps(t)
	d.HTTP = srv.Client()
	d.LoadConfig = func() (config.Config, error) { return config.Config{Host: srv.URL, Token: "token"}, nil }
	code := Execute(t.Context(), []string{"api", "x", "-i"}, d)
	if code != 0 || strings.Contains(out.String(), "Content-Encoding") || strings.Contains(out.String(), "Content-Length") || !strings.Contains(out.String(), "X-Kept: 1") || !strings.HasSuffix(out.String(), "\n\n{\"ok\":true}") {
		t.Fatalf("code %d stdout %q stderr %q", code, out, stderr)
	}
}

type apiClosedOutput struct{}

func (apiClosedOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestAPICommandClosedPipe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"x"}, 0},
		{[]string{"x", "-X", "head"}, 0},
		{[]string{"x", "-f", "a=b"}, 1},
		{[]string{"x", "-X", "DELETE"}, 1},
	} {
		d, _, stderr := testDeps(t)
		d.Out = apiClosedOutput{}
		d.HTTP = srv.Client()
		d.LoadConfig = func() (config.Config, error) { return config.Config{Host: srv.URL, Token: "token"}, nil }
		if code := Execute(t.Context(), append([]string{"api"}, tc.args...), d); code != tc.code {
			t.Errorf("%q: code %d stderr %q", tc.args, code, stderr)
		}
	}
}
