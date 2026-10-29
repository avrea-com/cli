package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

func TestMetricsWatchCadenceTailAndRawCursor(t *testing.T) {
	d, out, stderr := testDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	now := time.Unix(100, 0)
	d.Now = func() time.Time { return now }
	waits, metricsCalls, logCalls := 0, 0, 0
	d.Wait = func(ctx context.Context, interval time.Duration) error {
		if interval != time.Second {
			t.Error(interval)
		}
		now = now.Add(interval)
		waits++
		if waits == 7 {
			cancel()
		}
		return ctx.Err()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var response any
		switch {
		case strings.HasSuffix(r.URL.Path, "/metrics/cpu"):
			metricsCalls++
			if r.URL.RawQuery != "end=200&start=0" {
				t.Error(r.URL)
			}
			response = map[string]any{"unit": "1", "series": []any{}}
		case r.URL.Path == "/logs/search":
			logCalls++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			after := body["after_line"]
			first, last := 1, 10
			if logCalls > 1 {
				first, last = 11, 18
			}
			if logCalls > 2 {
				first, last = 19, 18
			}
			if logCalls == 2 && after != float64(10) {
				t.Errorf("second page cursor = %v", after)
			}
			if logCalls > 2 && after != float64(18) {
				t.Errorf("poll cursor = %v", after)
			}
			rows := []map[string]any{}
			for i := first; i <= last; i++ {
				level := "info"
				if i == 18 {
					level = "diagnostic"
				}
				rows = append(rows, map[string]any{"line_number": i, "content": fmt.Sprintf("line-%02d", i), "level": level})
			}
			response = map[string]any{"results": rows, "next_cursor": last, "has_more": logCalls == 1}
		default:
			response = map[string]any{"data": map[string]any{"job_name": "Build", "state": "in_progress"}}
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	code := Execute(ctx, []string{"job", "metrics", inspectionJob, "-w", "--source", "cpu", "--start", "0", "--end", "200"}, d)
	if code != 130 || metricsCalls != 2 || logCalls != 8 || waits != 7 || stderr.Len() != 0 {
		t.Fatal(code, metricsCalls, logCalls, waits, stderr)
	}
	for i := 1; i <= 18; i++ {
		count := strings.Count(out.String(), fmt.Sprintf("line-%02d\n", i))
		expected := 0
		if i >= 6 && i <= 17 {
			expected = 7
		}
		if count != expected {
			t.Errorf("line %d: count %d, expected %d", i, count, expected)
		}
	}
}

func TestMetricsWatchCancellationDuringFetch(t *testing.T) {
	d, _, stderr := testDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/metrics/") {
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
	if code != 130 || calls != 3 || stderr.Len() != 0 {
		t.Fatal(code, calls, stderr)
	}
	if !strings.Contains(out.String(), "metrics fetch failed: ConnectError") || !strings.Contains(out.String(), "log fetch failed: ConnectError") || strings.Contains(out.String(), "sensitive-network-address") {
		t.Fatal(out.String())
	}
}
