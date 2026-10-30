package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestRawContentDoesNotChangeJSONRequests(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		expected, contentType := "<xml>\x00\xff</xml>", "application/xml"
		if requests == 2 {
			expected, contentType = "{\"enabled\":true}", "application/json"
		}
		if string(body) != expected || r.Header.Get("Content-Type") != contentType || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Errorf("wrong request: %q %v", body, r.Header)
		}
		_, _ = io.WriteString(w, "{}")
	}))
	defer server.Close()
	c := New(config.Config{Host: server.URL, Token: "fixture"}, server.Client(), "test")
	if _, err := c.RawContent(t.Context(), http.MethodPost, "/saml", []byte("<xml>\x00\xff</xml>"), "application/xml"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(t.Context(), http.MethodPost, "/json", map[string]any{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatal(requests)
	}
}
func TestRawContentDoesNotFollowRedirect(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	c := New(config.Config{Host: origin.URL, Token: "fixture"}, origin.Client(), "test")
	_, err := c.RawContent(t.Context(), http.MethodPost, "/saml", []byte("private XML"), "application/xml")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 307 || called {
		t.Fatal(err, called)
	}
}
