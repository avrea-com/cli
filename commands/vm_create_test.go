package commands

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestVMSpawnCredentialBoundaryAndNoRetries(t *testing.T) {
	for _, scenario := range []string{"success", "echoed credential", "transport failure"} {
		t.Run(scenario, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			credential := "synthetic-per-session-secret"
			global := "synthetic-environment-key"
			path := filepath.Join(t.TempDir(), "work-order")
			if err := os.WriteFile(path, []byte(" "+credential+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			env := map[string]string{"CLAUDE_RUNNER_ORDER_ID": "order-test", "CLAUDE_RUNNER_WORK_ORDER_FILE": path, "CLAUDE_RUNNER_ENVIRONMENT_KEY": global}
			d.Getenv = func(key string) string { return env[key] }
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: "org-019a0000000070008000000000000001"}, nil
			}
			calls := 0
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				var body map[string]any
				if err = json.Unmarshal(raw, &body); err != nil {
					t.Fatal(err)
				}
				bootstrap, ok := body["bootstrap"].(map[string]any)
				if !ok || bootstrap["provider"] != "claude_code" || bootstrap["credential"] != credential || body["idempotency_key"] != "order-test" || strings.Contains(string(raw), global) {
					t.Fatal("incorrect spawn request")
				}
				if scenario == "transport failure" {
					return nil, errors.New("network error " + credential)
				}
				status := 200
				response := `{"data":{"vm":{"customer_vm_id":"cvm-019a0000000070008000000000000001"}}}`
				if scenario == "echoed credential" {
					status = 422
					raw, _ := json.Marshal(map[string]string{"detail": "invalid credential " + credential})
					response = string(raw)
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
			})}
			code := Execute(t.Context(), []string{"vm", "create", "--from-spawn-script", "claude-code", "--verbose"}, d)
			want := 0
			if scenario == "echoed credential" {
				want = 2
			}
			if scenario == "transport failure" {
				want = 1
			}
			combined := out.String() + stderr.String()
			if calls != 1 || code != want || strings.Contains(combined, credential) || strings.Contains(combined, global) {
				t.Fatal("credential boundary or retry/exit contract violated", calls, code)
			}
			if scenario == "echoed credential" && !strings.Contains(combined, "[REDACTED]") {
				t.Fatal("missing redaction marker")
			}
		})
	}
}
func TestVMCreateDoesNotRetryUncertainOutcome(t *testing.T) {
	d, _, _ := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: "org-019a0000000070008000000000000001"}, nil
	}
	calls := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("uncertain outcome") })}
	code := Execute(t.Context(), []string{"vm", "create", "--name", "fixture", "--os", "linux", "--size", "4-vcpu", "--ephemeral", "--wait"}, d)
	if code != 1 || calls != 1 {
		t.Fatal(code, calls)
	}
}
func TestVMSpawnRejectsInvalidCredentialFilesBeforeRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{{"invalid UTF-8", []byte{0xff}}, {"oversized", []byte(strings.Repeat("x", (16<<20)+1))}} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, stderr := testDeps(t)
			path := filepath.Join(t.TempDir(), "order")
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			d.Getenv = func(key string) string {
				switch key {
				case "CLAUDE_RUNNER_ORDER_ID":
					return "fixture"
				case "CLAUDE_RUNNER_WORK_ORDER_FILE":
					return path
				}
				return ""
			}
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: "org-019a0000000070008000000000000001"}, nil
			}
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid file reached the network")
				return nil, nil
			})}
			code := Execute(t.Context(), []string{"vm", "create", "--from-spawn-script", "claude-code"}, d)
			if code != 2 || !strings.Contains(stderr.String(), "cannot read the work order file") {
				t.Fatal(code, stderr)
			}
		})
	}
}
