package client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestInvoiceRedirectsDoNotRestoreCredentialsAcrossOrigins(t *testing.T) {
	var origin *httptest.Server
	calls := []string{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "target")
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials crossed origins")
		}
		http.Redirect(w, r, origin.URL+"/finish", http.StatusFound)
	}))
	defer target.Close()
	origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/start":
			if r.Header.Get("Authorization") != "Bearer fixture" {
				t.Error("missing initial auth")
			}
			http.Redirect(w, r, "/same", http.StatusFound)
		case "/same":
			if r.Header.Get("Authorization") != "Bearer fixture" {
				t.Error("same-origin auth lost")
			}
			http.Redirect(w, r, target.URL, http.StatusFound)
		case "/finish":
			if r.Header.Get("Authorization") != "" {
				t.Error("credentials restored after cross-origin redirect")
			}
			_, _ = io.WriteString(w, "%PDF-fixture")
		default:
			t.Error("unexpected request")
		}
	}))
	defer origin.Close()
	c := New(config.Config{Host: origin.URL, Token: "fixture"}, origin.Client(), "test")
	body, err := c.DownloadInvoice(t.Context(), "/start")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(body)
	if err != nil || string(data) != "%PDF-fixture" || strings.Join(calls, ",") != "/start,/same,target,/finish" {
		t.Fatal(string(data), err, calls)
	}
}
func TestInvoiceRejectsHTTPSDowngradeAndRedirectLoop(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
	defer target.Close()
	calls := 0
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/loop" {
			http.Redirect(w, r, "/loop", http.StatusFound)
			return
		}
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()
	c := New(config.Config{Host: origin.URL, Token: "fixture"}, origin.Client(), "test")
	if _, err := c.DownloadInvoice(t.Context(), "/downgrade"); err == nil {
		t.Fatal("accepted downgrade")
	}
	if targetCalls != 0 {
		t.Fatal("contacted insecure redirect")
	}
	calls = 0
	if _, err := c.DownloadInvoice(t.Context(), "/loop"); err == nil {
		t.Fatal("accepted loop")
	}
	if calls != 21 {
		t.Fatal("redirect limit", calls)
	}
}
