package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
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

// downloadStreamClient keeps the real HTTP transport inside the synctest bubble.
// Loopback sockets cannot use its virtual clock; net.Pipe can, so runner load
// cannot consume the inactivity budget before response headers arrive.
func downloadStreamClient(t *testing.T, serve func(net.Conn)) *Client {
	t.Helper()
	server, conn := net.Pipe()
	done := make(chan struct{})
	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return conn, nil
		},
	}
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		_ = conn.Close()
		_ = server.Close()
		<-done
	})
	go func() {
		defer close(done)
		defer func() { _ = server.Close() }()
		req, err := http.ReadRequest(bufio.NewReader(server))
		if err != nil {
			t.Error(err)
			return
		}
		_ = req.Body.Close()
		if _, err := fmt.Fprint(server, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n"); err != nil {
			t.Error(err)
			return
		}
		serve(server)
	}()
	return New(config.Config{Host: "http://localhost"}, &http.Client{Transport: transport}, "test")
}

func TestDownloadInactivityAndParentCancellation(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(fmt.Sprint(parent), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := downloadStreamClient(t, func(conn net.Conn) {
					// Leave the response body stalled until the client cancels.
					_, _ = io.Copy(io.Discard, conn)
				})
				c.HTTP.Timeout = 40 * time.Millisecond
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				body, err := c.Download(ctx, "/download")
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = body.Close() }()
				start := time.Now()
				if parent {
					cancel()
				}
				_, err = io.ReadAll(body)
				if parent {
					if !errors.Is(err, context.Canceled) || time.Since(start) != 0 {
						t.Fatalf("parent cancellation must interrupt immediately: %v after %v", err, time.Since(start))
					}
				} else {
					var failure *DownloadError
					if !errors.As(err, &failure) || ctx.Err() != nil || time.Since(start) != c.HTTP.Timeout {
						t.Fatalf("inactivity must expire only the download after %v: %v after %v (parent: %v)", c.HTTP.Timeout, err, time.Since(start), ctx.Err())
					}
				}
			})
		})
	}
}

func TestDownloadProgressHasNoWholeFileDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := downloadStreamClient(t, func(conn net.Conn) {
			for range 8 {
				if _, err := fmt.Fprint(conn, "5\r\nchunk\r\n"); err != nil {
					t.Error(err)
					return
				}
				time.Sleep(25 * time.Millisecond)
			}
			_, err := fmt.Fprint(conn, "0\r\n\r\n")
			if err != nil {
				t.Error(err)
			}
		})
		c.HTTP.Timeout = 100 * time.Millisecond
		start := time.Now()
		body, err := c.Download(t.Context(), "/download")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = body.Close() }()
		data, err := io.ReadAll(body)
		if err != nil || string(data) != strings.Repeat("chunk", 8) {
			t.Fatal(string(data), err)
		}
		if elapsed := time.Since(start); elapsed != 200*time.Millisecond {
			t.Fatalf("stream must outlast the 100ms inactivity timeout: elapsed %v", elapsed)
		}
	})
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
