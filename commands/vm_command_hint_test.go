package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestVMShowCommandKeepsUntrustedIDInOneArgument(t *testing.T) {
	for _, tc := range []struct{ id, argument string }{
		{"cvm-fixture", "cvm-fixture"},
		{"cvm-fixture'; printf injected; #", "cvm-fixture'; printf injected; #"},
		{"cvm-$(printf injected)`printf injected`\" & value", "cvm-$(printf injected)`printf injected`\" & value"},
		{"cvm-\n\x1b[31m", `cvm-\n\x1b[31m`},
	} {
		t.Run(tc.id, func(t *testing.T) {
			assertHintShellArguments(t, "avr", vmShowCommand(tc.id), "vm\x00show\x00"+tc.argument+"\x00")
		})
	}
	if got := vmShowCommand("cvm-fixture"); got != "avr vm show cvm-fixture" {
		t.Fatal(got)
	}
}

// Run only a fixture command implemented as a shell function; no CLI or desktop app is invoked.
func assertHintShellArguments(t *testing.T, name, hint, want string) {
	t.Helper()
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell not installed")
	}
	script := name + `() { printf '%s\000' "$@"; }; ` + hint
	got, err := exec.CommandContext(t.Context(), shell, "-c", script).CombinedOutput()
	if err != nil || string(got) != want {
		t.Fatalf("hint %q: output %q, want %q, error %v", hint, got, want, err)
	}
}

func TestVMCreateRecoveryHintsQuoteResponseID(t *testing.T) {
	const id = "cvm-fixture'; printf injected; #"
	const hint = `avr vm show 'cvm-fixture'"'"'; printf injected; #'`
	for _, scenario := range []string{"provisioning", "closed output", "failed wait"} {
		t.Run(scenario, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
			}
			posts := 0
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				status, body := 403, []byte(`{"detail":"denied"}`)
				if r.Method == http.MethodPost {
					posts++
					status = 201
					body, _ = json.Marshal(map[string]any{"data": map[string]any{
						"vm": map[string]any{"customer_vm_id": id, "state": "PENDING"}, "password": "one-time-fixture-secret",
					}})
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			args := []string{"vm", "create", "--os", "windows", "--size", "2-vcpu"}
			wantCode := 0
			switch scenario {
			case "closed output":
				d.Out = tokenBrokenWriter{}
				wantCode = 1
			case "failed wait":
				args = append(args, "--wait", "--json")
				wantCode = 1
			}
			code := Execute(t.Context(), args, d)
			if code != wantCode || posts != 1 || !strings.Contains(out.String()+stderr.String(), hint) {
				t.Fatal(code, posts, out, stderr)
			}
			if strings.Contains(stderr.String(), "one-time-fixture-secret") {
				t.Fatal("recovery hint exposed password")
			}
		})
	}
}

func TestVMWaitingHintsQuoteID(t *testing.T) {
	const id = "cvm-fixture'; printf injected; #"
	const hint = `avr vm show 'cvm-fixture'"'"'; printf injected; #'`
	d, out, _ := testDeps(t)
	_ = vmWaitExit(d, nil, "timeout", "RUNNING", 5, id)
	_ = vmDeleteOutcome(d, id, nil, "timeout", 5, false)
	lines := vmConnect(map[string]any{"customer_vm_id": id, "enable_remote_desktop": true, "os_type": "windows"}, "")
	if strings.Count(out.String(), hint) != 2 || !strings.Contains(strings.Join(lines, "\n"), hint) {
		t.Fatal(out, lines)
	}
}
