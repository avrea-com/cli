package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

func TestTimingSeparatesResponseWaitAndBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(12 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = fmt.Fprint(w, `{"secret":"response-secret"}`)
	}))
	defer server.Close()
	var log bytes.Buffer
	httpClient := server.Client()
	// Delay at Read, after the client records response_headers. A server-side
	// sleep can expire before the client is scheduled on a busy CI runner.
	httpClient.Transport = delayedBodyTransport{httpClient.Transport}
	c := New(config.Config{Host: server.URL, Token: "token-secret"}, httpClient, "test")
	c.Timings = &log
	for range 2 {
		if _, err := c.Do(t.Context(), http.MethodGet, "/users/me?session=query-secret", nil); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(log.String(), "secret") || strings.Contains(log.String(), server.URL) {
		t.Fatal("timings exposed request/response data")
	}
	lines := strings.Split(strings.TrimSpace(log.String()), "\n")
	if len(lines) != 2 {
		t.Fatal(log.String())
	}
	for i, line := range lines {
		var record struct {
			Reused   bool          `json:"connection_reused"`
			Duration float64       `json:"duration_ms"`
			Status   int           `json:"status"`
			Events   []TimingEvent `json:"events"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "avr-timing ")), &record); err != nil {
			t.Fatal(err)
		}
		event := map[string]float64{}
		for _, item := range record.Events {
			event[item.Name] = item.Milliseconds
		}
		if record.Status != 200 || record.Reused != (i == 1) {
			t.Fatalf("bad status/reuse: %#v", record)
		}
		if event["first_response_byte"]-event["request_written"] < 8 {
			t.Fatal("response delay missing")
		}
		if event["body_read"]-event["response_headers"] < 8 {
			t.Fatal("body delay missing")
		}
		if event["complete"] < event["body_read"] || record.Duration < event["complete"] {
			t.Fatal("invalid lifecycle ordering")
		}
		_, tls := event["tls_done"]
		if tls != (i == 0) {
			t.Fatal("wrong TLS events for fresh/reused connection")
		}
	}
}

type delayedBodyTransport struct{ http.RoundTripper }

func (t delayedBodyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.RoundTripper.RoundTrip(r)
	if err == nil {
		resp.Body = &delayedBody{ReadCloser: resp.Body}
	}
	return resp, err
}

type delayedBody struct {
	io.ReadCloser
	once sync.Once
}

func (b *delayedBody) Read(p []byte) (int, error) {
	b.once.Do(func() { time.Sleep(12 * time.Millisecond) })
	return b.ReadCloser.Read(p)
}

func TestTimingCallbacksAreConcurrentAndRedacted(t *testing.T) {
	trace := newRequestTiming()
	hooks := trace.hooks()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			hooks.DNSStart(httptrace.DNSStartInfo{Host: "private-host"})
			hooks.DNSDone(httptrace.DNSDoneInfo{Err: errors.New("private-error")})
			hooks.ConnectStart("tcp", "private-address")
			hooks.ConnectDone("tcp", "private-address", errors.New("private-error"))
		})
	}
	var log bytes.Buffer
	trace.finish(&log)
	wg.Wait()
	if strings.Contains(log.String(), "private") {
		t.Fatal("sensitive metadata logged")
	}
}

// BenchmarkRequestTiming isolates tracing/serialization cost from remote latency.
// The server and reused connection are identical in both sub-benchmarks.
func BenchmarkRequestTiming(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"email":"fixture@example.test"}`)
	}))
	defer server.Close()
	for _, enabled := range []bool{false, true} {
		b.Run(fmt.Sprint(enabled), func(b *testing.B) {
			c := New(config.Config{Host: server.URL, Token: "fixture"}, server.Client(), "benchmark")
			if enabled {
				c.Timings = io.Discard
			}
			if _, err := c.Do(b.Context(), http.MethodGet, "/users/me", nil); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for b.Loop() {
				if _, err := c.Do(b.Context(), http.MethodGet, "/users/me", nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
