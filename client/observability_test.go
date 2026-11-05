package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
	h.Transport.(*http.Transport).Proxy = func(*http.Request) (*url.URL, error) { t.Error("stream used a proxy"); return nil, nil }
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
