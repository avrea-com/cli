package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

func TestMetricsWatchTailAndRawCursor(t *testing.T) {
	d, out, stderr := testDeps(t)
	var streamCalls, logCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/observability/stream") {
			streamCalls.Add(1)
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = conn.CloseNow() }()
			for _, frame := range []string{`{"type":"window","signal":"metrics","running":true}`, `{"type":"bundle","signal":"metrics","metrics":{"cpu":{"series":[]}}}`, `{"type":"complete","signal":"metrics"}`} {
				if err := conn.Write(r.Context(), websocket.MessageText, []byte(frame)); err != nil {
					t.Error(err)
					return
				}
			}
			_, _, _ = conn.Read(r.Context())
			return
		}
		var response any
		if r.URL.Path == "/logs/search" {
			count := logCalls.Add(1)
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			first, last := 1, 10
			if count == 2 {
				first, last = 11, 18
			}
			if count > 2 {
				first, last = 19, 18
			}
			if count == 2 && body["after_line"] != float64(10) {
				t.Errorf("page cursor: %v", body)
			}
			if count > 2 && body["after_line"] != float64(18) {
				t.Errorf("final cursor: %v", body)
			}
			rows := []map[string]any{}
			for i := first; i <= last; i++ {
				level := "info"
				if i == 18 {
					level = "diagnostic"
				}
				rows = append(rows, map[string]any{"line_number": i, "content": fmt.Sprintf("line-%02d", i), "level": level})
			}
			response = map[string]any{"results": rows, "next_cursor": last, "has_more": count == 1}
		} else if strings.Contains(r.URL.Path, "/metrics/") {
			t.Error("watch polled metrics")
			response = map[string]any{}
		} else {
			response = map[string]any{"data": map[string]any{"job_name": "Build", "state": "completed", "conclusion": "success"}}
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "fixture", Org: inspectionOrg}, nil
	}
	code := Execute(t.Context(), []string{"job", "metrics", inspectionJob, "-w", "--source", "cpu"}, d)
	if code != 0 || streamCalls.Load() != 1 || logCalls.Load() != 3 || stderr.Len() != 0 {
		t.Fatal(code, streamCalls.Load(), logCalls.Load(), stderr)
	}
	for i := 1; i <= 18; i++ {
		want := 0
		if i >= 6 && i <= 17 {
			want = 3
		}
		if got := strings.Count(out.String(), fmt.Sprintf("line-%02d\n", i)); got != want {
			t.Errorf("line %d: %d, want %d", i, got, want)
		}
	}
}

func TestMetricsWatchCancellationDuringFetch(t *testing.T) {
	d, _, stderr := testDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/observability/stream") {
			cancel()
			<-r.Context().Done()
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"state": "in_progress"}})
	}))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	if code := Execute(ctx, []string{"job", "metrics", inspectionJob, "-w"}, d); code != 130 || stderr.Len() != 0 {
		t.Fatal(code, stderr)
	}
}

func TestMetricsWatchTransportRetrySanitizesError(t *testing.T) {
	d, out, stderr := testDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	calls := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			data, _ := json.Marshal(map[string]any{"data": map[string]any{"state": "in_progress"}})
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(data))), Request: r}, nil
		}
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("sensitive-network-address")}
	})}
	d.Wait = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	code := Execute(ctx, []string{"job", "metrics", inspectionJob, "-w", "--source", "cpu"}, d)
	if code != 130 || calls != 2 || !strings.Contains(stderr.String(), "reconnecting in 1s") {
		t.Fatal(code, calls, stderr)
	}
	if out.Len() != 0 || strings.Contains(stderr.String(), "sensitive-network-address") {
		t.Fatal(out.String(), stderr.String())
	}
}
