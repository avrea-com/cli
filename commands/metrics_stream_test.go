package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/coder/websocket"
)

func TestMetricsStreamRetryBudgetResetsOnlyOnBundle(t *testing.T) {
	d, _, stderr := testDeps(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) != 4 {
			http.Error(w, "private server diagnostic", http.StatusServiceUnavailable)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.CloseNow() }()
		if err := conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"bundle","signal":"metrics","metrics":{}}`)); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	api := client.New(config.Config{Host: server.URL}, server.Client(), "test")
	var delays []time.Duration
	d.Wait = func(ctx context.Context, delay time.Duration) error { delays = append(delays, delay); return ctx.Err() }
	bundles := 0
	err := watchMetrics(t.Context(), d, api, "/stream", []string{"cpu"}, false, func(u metricUpdate) error {
		if u.sawBundle {
			bundles++
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "reconnect attempts exhausted (last error: HTTP 503)") || calls.Load() != 7 || bundles != 1 {
		t.Fatal(calls.Load(), bundles, err)
	}
	if !reflect.DeepEqual(delays, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, time.Second, 2 * time.Second, 4 * time.Second}) {
		t.Fatal(delays)
	}
	if strings.Contains(stderr.String(), "private server diagnostic") {
		t.Fatal(stderr)
	}
}

func TestVMStateNotificationsWakeFloorAndPermanentFallback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			d, _, _ := testDeps(t)
			now := time.Unix(100, 0)
			d.Now = func() time.Time { return now }
			var delays []time.Duration
			d.Wait = func(ctx context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				now = now.Add(delay)
				return ctx.Err()
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.RawQuery != "signals=status" {
					t.Error(r.URL)
				}
				if fail {
					http.Error(w, "unavailable", http.StatusForbidden)
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.CloseNow() }()
				for _, frame := range []string{`{"type":"changed","signal":"metrics"}`, `{"type":"changed","signal":"status"}`, `{"type":"changed"}`} {
					if err := conn.Write(r.Context(), websocket.MessageText, []byte(frame)); err != nil {
						t.Error(err)
						return
					}
				}
				_, _, _ = conn.Read(r.Context())
			}))
			defer server.Close()
			api := client.New(config.Config{Host: server.URL}, server.Client(), "test")
			notifications := &vmStateNotifications{api: api, path: "/vms/fixture", d: d}
			defer notifications.Close()
			for range 2 {
				if err := notifications.Wait(t.Context(), 3*time.Second); err != nil {
					t.Fatal(err)
				}
			}
			want := time.Second
			if fail {
				want = 3 * time.Second
			}
			if calls.Load() != 1 || !reflect.DeepEqual(delays, []time.Duration{want, want}) {
				t.Fatal(calls.Load(), delays)
			}
		})
	}
}
