package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

func TestJobFollowLateDrainAndDiagnosticCursor(t *testing.T) {
	d, out, stderr := testDeps(t)
	var cursors []any
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprintf(w, `{"data":{"job_id":%q,"state":"completed","conclusion":"failure","steps":[{"name":"Build","conclusion":"failure"}]}}`, inspectionJob)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		cursors = append(cursors, body["after_line"])
		polls++
		switch polls {
		case 1:
			_, _ = io.WriteString(w, `{"results":[{"line_number":1,"step_name":"Build","level":"diagnostic","content":"hidden"}],"has_more":false}`)
		case 2, 3:
			_, _ = io.WriteString(w, `{"results":[]}`)
		case 4:
			_, _ = io.WriteString(w, `{"results":[{"line_number":2,"step_name":"Build","content":"late tail"}],"has_more":true}`)
		case 5:
			_, _ = io.WriteString(w, `{"results":[{"line_number":3,"step_name":"Build","content":"final tail"}],"has_more":false}`)
		default:
			t.Error("unexpected poll")
			_, _ = io.WriteString(w, `{"results":[]}`)
		}
	}))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	waits := []time.Duration{}
	d.Wait = func(ctx context.Context, delay time.Duration) error { waits = append(waits, delay); return ctx.Err() }
	if got := Execute(t.Context(), []string{"job", "logs", inspectionJob, "--follow"}, d); got != 0 {
		t.Fatal(got, stderr.String())
	}
	if !reflect.DeepEqual(cursors, []any{nil, float64(1), float64(1), float64(1), float64(2)}) {
		t.Fatal(cursors)
	}
	if !reflect.DeepEqual(waits, []time.Duration{2 * time.Second, 2 * time.Second, 8 * time.Second}) {
		t.Fatal(waits)
	}
	for _, value := range []string{"late tail", "final tail", "Job completed: failure"} {
		if !strings.Contains(out.String(), value) {
			t.Fatal(out.String())
		}
	}
	if strings.Contains(out.String(), "hidden") || strings.Count(out.String(), "--- Build ---") != 1 {
		t.Fatal(out.String())
	}
}

func TestJobFollowCancellationAndFinalDrainFailure(t *testing.T) {
	for _, mode := range []string{"wait", "request", "drain-failure"} {
		t.Run(mode, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			polls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = fmt.Fprintf(w, `{"data":{"job_id":%q,"state":"completed","conclusion":"success"}}`, inspectionJob)
					return
				}
				polls++
				if mode == "request" {
					_, _ = io.Copy(io.Discard, r.Body)
					cancel()
					<-r.Context().Done()
					return
				}
				if mode == "drain-failure" && polls == 3 {
					w.WriteHeader(503)
					return
				}
				_, _ = io.WriteString(w, `{"results":[]}`)
			}))
			defer server.Close()
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
			}
			d.Wait = func(ctx context.Context, _ time.Duration) error {
				if mode == "wait" {
					cancel()
				}
				return ctx.Err()
			}
			code := Execute(ctx, []string{"job", "logs", inspectionJob, "--follow"}, d)
			if mode == "drain-failure" {
				if code != 0 || !strings.Contains(stderr.String(), "failed to drain final log batch") || !strings.Contains(out.String(), "Job completed: success") {
					t.Fatal(code, out, stderr)
				}
			} else if code != 130 || !strings.Contains(out.String(), "Stopped following.") {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestJobLogCursorCannotLoop(t *testing.T) {
	d, _, stderr := testDeps(t)
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprintf(w, `{"data":{"job_id":%q}}`, inspectionJob)
			return
		}
		polls++
		_, _ = io.WriteString(w, `{"results":[{"line_number":1,"content":"one"}],"has_more":true,"next_cursor":1}`)
	}))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	if code := Execute(t.Context(), []string{"job", "logs", inspectionJob}, d); code != 1 || polls != 2 || !strings.Contains(stderr.String(), "pagination did not advance") {
		t.Fatal(code, polls, stderr)
	}
}

func TestJobSSHArgumentsAndPasswordBoundary(t *testing.T) {
	for _, host := range []string{"runner.example.test", "-oProxyCommand=bad", "host\nbad"} {
		t.Run(host, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			called := false
			pin := ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/vms/ssh-forwarding" {
					t.Error(r.Method, r.URL)
				}
				var payload map[string]any
				_ = json.NewDecoder(r.Body).Decode(&payload)
				if payload["job_id"] != inspectionJob {
					t.Error(payload)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"host": host, "port": 2222, "username": "runner", "password": "private-fixture-password", "host_key": "ssh-ed25519 fixture", "ssh_keys_installed": false})
			}))
			defer server.Close()
			d.LoadConfig = func() (config.Config, error) { return config.Config{Host: server.URL, Token: "avr_fixture"}, nil }
			d.RunSSH = func(_ context.Context, args []string) (int, error) {
				called = true
				if len(args) != 8 || !strings.HasPrefix(args[4], "UserKnownHostsFile=") {
					t.Fatalf("invalid pinned SSH invocation: %q", args)
				}
				pin = strings.TrimPrefix(args[4], "UserKnownHostsFile=")
				content, e := os.ReadFile(pin)
				if e != nil || string(content) != "[runner.example.test]:2222 ssh-ed25519 fixture\n" {
					t.Fatalf("invalid pin: %q %v", content, e)
				}
				info, e := os.Stat(pin)
				if e != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("unsafe pin permissions: %v %v", info, e)
				}
				want := []string{"ssh", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + pin, "-p", "2222", "runner@runner.example.test"}
				if !reflect.DeepEqual(args, want) {
					t.Error(args)
				}
				return 7, nil
			}
			code := Execute(t.Context(), []string{"job", "ssh", inspectionJob}, d)
			if host == "runner.example.test" {
				if _, e := os.Stat(pin); !os.IsNotExist(e) {
					t.Fatalf("pin not cleaned up: %v", e)
				}
				if code != 7 || !called {
					t.Fatal(code, called)
				}
			} else if code != 1 || called {
				t.Fatal(code, called)
			}
			if strings.Contains(out.String()+stderr.String(), "private-fixture-password") {
				t.Fatal("password leaked")
			}
		})
	}
}

func TestSSHChild(t *testing.T) {
	mode := os.Getenv("AVR_TEST_SSH_CHILD")
	if mode == "" {
		return
	}
	if mode == "exit" {
		_, _ = io.Copy(os.Stdout, os.Stdin)
		os.Exit(23)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	fmt.Println("ready")
	select {
	case <-signals:
		fmt.Println("restored")
	case <-time.After(10 * time.Second):
		os.Exit(99)
	}
	os.Exit(0)
}

type cancelWriter struct{ cancel context.CancelFunc }

func (w cancelWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "ready") {
		w.cancel()
	}
	return len(p), nil
}

func TestSSHSubprocessIOExitAndCancellation(t *testing.T) {
	d, out, _ := testDeps(t)
	t.Setenv("AVR_TEST_SSH_CHILD", "exit")
	d.In = strings.NewReader("stdin fixture")
	code, err := executeSSH(t.Context(), d, []string{os.Args[0], "-test.run=^TestSSHChild$"})
	if err != nil || code != 23 || out.String() != "stdin fixture" {
		t.Fatal(code, err, out)
	}
	t.Setenv("AVR_TEST_SSH_CHILD", "wait")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	d.Out = cancelWriter{cancel}
	code, err = executeSSH(ctx, d, []string{os.Args[0], "-test.run=^TestSSHChild$"})
	if code != 130 || !errors.Is(err, context.Canceled) {
		t.Fatal(code, err)
	}
}

func TestJobMetadataEscapesServerControls(t *testing.T) {
	d, out, _ := testDeps(t)
	data := map[string]any{"state": "queued\x1b]2;unexpected\a", "job_name": "Build"}
	if err := renderJobView(d, data, inspectionJob, "https://api.test", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("server text executed terminal controls: %q", out.String())
	}
}
