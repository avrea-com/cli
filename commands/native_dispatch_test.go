package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	shellquote "github.com/kballard/go-shellquote"
)

const nativeTestWorkflow = "awf-019a0000000070008000000000000004"
const nativeTestDispatch = "wap-019a0000000070008000000000000005"

func TestNativeDecimalPreservesPrecisionWithoutExpandingExponents(t *testing.T) {
	for input, want := range map[string]string{"1.00000000000000001": "1.00000000000000001", "+001.20": "1.20", ".5": "0.5", "1e1000000": "1E+1000000", "1e-1000000": "1E-1000000", "-0.00": "-0.00", "1.2300e3": "1230.0"} {
		got, err := nativeNumber(input)
		if err != nil || got != want {
			t.Errorf("%q: %q %v", input, got, err)
		}
	}
	for _, input := range []string{"NaN", "sNaN", "Infinity", "-inf", "1e99999999999999999999999", ".", "true"} {
		if _, err := nativeNumber(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestNativeReceiptWaitIsBoundedAndCancellable(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			d, _, _ := testDeps(t)
			d = d.defaults()
			now := time.Now()
			d.Now = func() time.Time { return now }
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			waits := 0
			d.Wait = func(ctx context.Context, delay time.Duration) error {
				waits++
				now = now.Add(delay)
				if cancelled {
					cancel()
				}
				return ctx.Err()
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(503)
				_, _ = io.WriteString(w, `{"detail":"retry"}`)
			}))
			defer server.Close()
			cfg := config.Config{Host: server.URL, Token: "fixture"}
			api := client.New(cfg, nil, "test")
			_, err := followNativeDispatch(ctx, d, cfg, api, "/orgs/org/repos/repo", nativeReceipt{id: nativeTestDispatch, state: "pending", retry: "replay-fixture"})
			if cancelled {
				if !errors.Is(err, context.Canceled) || waits != 1 {
					t.Fatal(waits, err)
				}
			} else if waits != 90 || err == nil || !strings.Contains(err.Error(), "replay-fixture") {
				t.Fatal(waits, err)
			}
		})
	}
}

func TestNativeDispatchLostResponseReplayFreezesRefInputsAndKey(t *testing.T) {
	d, _, stderr := testDeps(t)
	d = d.defaults()
	posts := 0
	metadata := 0
	var bodies, keys []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			metadata++
			_, _ = io.WriteString(w, `{"data":{"ref":"refs/heads/trunk","dispatchable":true,"can_dispatch":true,"inputs":[{"name":"count","type":"number"}]}}`)
			return
		}
		posts++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		bodies = append(bodies, string(body))
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("lost auth header")
		}
		if posts <= 2 {
			conn, _, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		_, _ = fmt.Fprintf(w, `{"data":{"dispatch_id":%q,"state":"pending","run_ids":[],"ref":"refs/heads/trunk","commit_sha":"abc"}}`, nativeTestDispatch)
	}))
	defer server.Close()
	cfg := config.Config{Host: server.URL, Token: "fixture"}
	api := client.New(cfg, nil, "test")
	_, err := submitNativeWorkflow(t.Context(), d, cfg, api, "org", "repo", nativeTestWorkflow, "", false, []string{"count=1.00000000000000001"}, nil, "")
	mu.Lock()
	initialPosts := posts
	mu.Unlock()
	if err == nil || initialPosts != 2 {
		t.Fatal(initialPosts, err)
	}
	_, command, ok := strings.Cut(err.Error(), "Retry the exact request:\n")
	if !ok {
		t.Fatal(err)
	}
	words, err := shellquote.Split(command)
	if err != nil {
		t.Fatal(err)
	}
	if len(words) < 10 || words[0] != "printf" || words[2] != `{"count":1.00000000000000001}` || words[5] != "AVR_HOST="+server.URL {
		t.Fatal(words)
	}
	flag := func(name string) string {
		for i, s := range words {
			if s == name && i+1 < len(words) {
				return words[i+1]
			}
		}
		t.Fatal("missing", name)
		return ""
	}
	if _, err := submitNativeWorkflow(t.Context(), d, cfg, api, flag("--org"), flag("--repo"), words[9], flag("--ref"), true, nil, []byte(words[2]), flag("--idempotency-key")); err != nil {
		t.Fatal(err, stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if metadata != 1 || posts != 3 || bodies[0] != bodies[1] || bodies[0] != bodies[2] || keys[0] != keys[1] || keys[0] != keys[2] {
		t.Fatal(metadata, posts, bodies, keys)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(bodies[2]), &body); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCancellationDoesNotResubmit(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	posts := 0
	transport := watchTransportFunc(func(r *http.Request) (*http.Response, error) { posts++; cancel(); return nil, ctx.Err() })
	cfg := config.Config{Host: "https://fixture.test", Token: "fixture"}
	api := client.New(cfg, &http.Client{Transport: transport}, "test")
	d, _, _ := testDeps(t)
	d = d.defaults()
	_, err := submitNativeWorkflow(ctx, d, cfg, api, "org", "repo", nativeTestWorkflow, "main", true, nil, nil, "019a0000000070008000000000000007")
	if !errors.Is(err, context.Canceled) || posts != 1 {
		t.Fatal(posts, err)
	}
}

func TestWorkflowInputCancellationDoesNotReadUnfinishedTee(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://fixture.test", Token: "fixture"}, nil
	}
	d.IsInputTerminal = func() bool { return false }
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	d.In = reader
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Execute(ctx, []string{"workflow", "run", nativeTestWorkflow, "--json"}, d) }()
	if _, err := writer.Write([]byte(`{"count":`)); err != nil {
		t.Fatal(err)
	}
	cancel()
	drained := make(chan struct{})
	go func() { _, _ = writer.Write([]byte(`1}`)); _ = writer.Close(); close(drained) }()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatal(code, stderr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stdin cancellation blocked")
	}
	<-drained
}
