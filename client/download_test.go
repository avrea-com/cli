package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

func TestDownloadRejectsCompressionAndRedirects(t *testing.T) {
	for _, encoding := range []string{"gzip", "zstd", "deflate", "br"} {
		t.Run(encoding, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("missing request contract")
				}
				w.Header().Set("Content-Encoding", encoding)
				_, _ = fmt.Fprint(w, "compressed bytes")
			}))
			defer server.Close()
			c := New(config.Config{Host: server.URL, Token: "fixture"}, server.Client(), "avr/test")
			body, err := c.Download(t.Context(), "/download")
			if body != nil {
				_ = body.Close()
			}
			var failure *DownloadError
			if !errors.As(err, &failure) {
				t.Fatal("must reject compressed identity response", err)
			}
		})
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("followed credential redirect")
		w.WriteHeader(200)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer origin.Close()
	c := New(config.Config{Host: origin.URL, Token: "fixture"}, origin.Client(), "test")
	_, err := c.Download(t.Context(), "/download")
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 302 {
		t.Fatal(err)
	}
}

func TestDownloadInactivityAndParentCancellation(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(fmt.Sprint(parent), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer server.Close()
			c := New(config.Config{Host: server.URL}, server.Client(), "test")
			c.HTTP.Timeout = 40 * time.Millisecond
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			body, err := c.Download(ctx, "/download")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = body.Close() }()
			if parent {
				cancel()
			}
			_, err = io.ReadAll(body)
			var failure *DownloadError
			if parent && !errors.Is(err, context.Canceled) || !parent && !errors.As(err, &failure) {
				t.Fatal(err)
			}
		})
	}
}

func TestDownloadProgressHasNoWholeFileDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range 8 {
			_, _ = fmt.Fprint(w, "chunk")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(25 * time.Millisecond):
			}
		}
	}))
	defer server.Close()
	c := New(config.Config{Host: server.URL}, server.Client(), "test")
	c.HTTP.Timeout = 100 * time.Millisecond
	body, err := c.Download(t.Context(), "/download")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(body)
	if err != nil || string(data) != strings.Repeat("chunk", 8) {
		t.Fatal(string(data), err)
	}
}

func TestDownloadErrorBodyBoundAndRetryHeader(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(fmt.Sprint(large), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "17")
				w.WriteHeader(429)
				detail := "denied"
				if large {
					detail = strings.Repeat("x", 128<<10)
				}
				_, _ = fmt.Fprintf(w, "{\"detail\":%q}", detail)
			}))
			defer server.Close()
			c := New(config.Config{Host: server.URL}, server.Client(), "test")
			_, err := c.Download(t.Context(), "/download")
			var he *HTTPError
			if !errors.As(err, &he) || he.Status != 429 || he.RetryAfter != "17" {
				t.Fatal(err)
			}
			if large && he.Detail != "" || !large && he.Detail != "denied" {
				t.Fatal(he.Detail)
			}
		})
	}
}
