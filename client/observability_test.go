package client

import (
	"context"
	"errors"
	"golang.org/x/net/http/httpproxy"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
	"github.com/coder/websocket"
)

func TestObservabilityIdleKeepsConnectionAndCancellationClosesIt(t *testing.T) {
	send := make(chan struct{})
	closed := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/prefix/vms/Dev%20Box/observability/stream?signals=metrics" || r.Header.Get("Authorization") != "Bearer fixture" || r.Header.Get("User-Agent") != "avr/test" || r.Header.Get("Cookie") != "" {
			t.Error("stream request changed", r.URL, r.Header)
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer close(closed)
		defer func() { _ = conn.CloseNow() }()
		<-send
		if err := conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"bundle"}`)); err != nil {
			t.Error(err)
			return
		}
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()
	h := server.Client()
	h.Transport.(*http.Transport).Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	c := New(config.Config{Host: server.URL + "/prefix", Token: "fixture"}, h, "avr/test")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream, err := c.OpenObservability(ctx, "/vms/Dev%20Box/observability/stream", "metrics", time.Second*5)
	if err != nil {
		close(send)
		t.Fatal(err)
	}
	defer stream.Close()
	for range 3 {
		data, err := stream.Next(ctx, time.Millisecond)
		if data != nil || err != nil {
			close(send)
			t.Fatal(string(data), err)
		}
	}
	close(send)
	data, err := stream.Next(ctx, time.Second*5)
	if err != nil || string(data) != `{"type":"bundle"}` {
		t.Fatal(string(data), err)
	}
	cancel()
	if _, err := stream.Next(ctx, time.Second*5); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	stream.Close()
	stream.Close() // Cleanup remains safe after errors and repeated calls.
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not observe cancellation")
	}
}

func TestObservabilityRefusesCredentialRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed credential redirect") }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	c := New(config.Config{Host: server.URL, Token: "fixture"}, server.Client(), "test")
	stream, err := c.OpenObservability(t.Context(), "/stream", "metrics", time.Second*5)
	if stream != nil {
		stream.Close()
		t.Fatal("redirect created a stream")
	}
	var failure *StreamError
	if !errors.As(err, &failure) || failure.HTTPStatus != 307 {
		t.Fatal(err)
	}
	for _, path := range []string{"//elsewhere", "/stream?token=secret", "/stream#fragment"} {
		if _, err := c.OpenObservability(t.Context(), path, "metrics", time.Second); err == nil {
			t.Fatal("unsafe path accepted", path)
		}
	}
}

func TestObservabilityBoundsFrameSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.CloseNow() }()
		// The peer may close as soon as the frame's size is known.
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(strings.Repeat("x", (16<<20)+1)))
	}))
	defer server.Close()
	c := New(config.Config{Host: server.URL}, server.Client(), "test")
	stream, err := c.OpenObservability(t.Context(), "/stream", "metrics", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	data, err := stream.Next(t.Context(), 5*time.Second)
	if data != nil || err == nil {
		t.Fatal("oversized frame was not rejected", len(data), err)
	}
}

func TestObservabilityHonorsProxyAndBypass(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.CloseNow() }()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte("proxied"))
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()
	var connections atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		if r.Method != "CONNECT" || r.Host != "stream.example:443" {
			t.Error("proxy request", r.Method, r.Host)
			http.Error(w, "bad tunnel", 400)
			return
		}
		upstream, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(r.Context(), "tcp", server.Listener.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = upstream.Close() }()
		downstream, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = downstream.Close() }()
		_, _ = io.WriteString(downstream, "HTTP/1.1 200 Connection Established\r\n\r\n")
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, downstream); close(done) }()
		_, _ = io.Copy(downstream, upstream)
		_ = downstream.Close()
		<-done
	}))
	defer proxy.Close()
	for _, bypass := range []bool{false, true} {
		h := server.Client()
		transport := h.Transport.(*http.Transport).Clone()
		// Use httpproxy's environment parser without net/http's global environment
		// cache, so both HTTPS_PROXY and NO_PROXY can be tested in one process.
		t.Setenv("HTTPS_PROXY", proxy.URL)
		t.Setenv("NO_PROXY", "")
		if bypass {
			t.Setenv("NO_PROXY", "stream.example")
		}
		proxyForURL := httpproxy.FromEnvironment().ProxyFunc()
		transport.Proxy = func(r *http.Request) (*url.URL, error) { return proxyForURL(r.URL) }
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		transport.TLSClientConfig.ServerName = "example.com"
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			if address == "stream.example:443" {
				address = server.Listener.Addr().String()
			}
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		h.Transport = transport
		before := connections.Load()
		c := New(config.Config{Host: "https://stream.example", Token: "fixture"}, h, "test")
		stream, err := c.OpenObservability(t.Context(), "/stream", "metrics", 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		data, err := stream.Next(t.Context(), 5*time.Second)
		stream.Close()
		if err != nil || string(data) != "proxied" {
			t.Fatal(err, string(data))
		}
		expected := int32(1)
		if bypass {
			expected = 0
		}
		if got := connections.Load() - before; got != expected {
			t.Fatalf("bypass=%v: proxy connections %d, want %d", bypass, got, expected)
		}
	}
}
