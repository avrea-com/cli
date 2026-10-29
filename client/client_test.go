package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
	"github.com/klauspost/compress/zstd"
)

func TestTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" || r.Header.Get("User-Agent") != "avr-cli/test" {
			t.Error("missing auth or user agent")
		}
		encoder, err := zstd.NewWriter(nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = encoder.Close() }()
		w.Header().Set("Content-Encoding", "zstd")
		_, _ = w.Write(encoder.EncodeAll([]byte(`{"n":9007199254740993,"nothing":null}`), nil))
	}))
	defer server.Close()
	c := New(config.Config{Host: server.URL, Token: "fixture"}, server.Client(), "avr-cli/test")
	result, err := c.Do(context.Background(), "GET", "/users/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["n"] != json.Number("9007199254740993") {
		t.Fatal(result)
	}
	if _, ok := result["nothing"]; !ok {
		t.Fatal("lost null")
	}
}

func TestNoCredentialRedirects(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(200) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer origin.Close()
	c := New(config.Config{Host: origin.URL, Token: "fixture"}, origin.Client(), "test")
	_, err := c.Do(context.Background(), "GET", "/users/me", nil)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 302 || called {
		t.Fatalf("redirect followed or wrong error: %v, %v", called, err)
	}
}

func TestMalformedResponses(t *testing.T) {
	for _, body := range []string{`[]`, `null`, `{"token":"sensitive"} trailing`, `not JSON`} {
		t.Run(body[:2], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, body) }))
			defer server.Close()
			c := New(config.Config{Host: server.URL}, server.Client(), "test")
			_, err := c.Do(context.Background(), "GET", "/users/me", nil)
			if err == nil {
				t.Fatal("accepted malformed response")
			}
		})
	}
}

func TestCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	c := New(config.Config{Host: server.URL}, server.Client(), "test")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.Do(ctx, "GET", "/users/me", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
