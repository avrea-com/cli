package commands

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestVMSSHPinCleanupOnLaunchFailureAndCancellation(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "launch failure", true: "external cancellation"}[cancelParent], func(t *testing.T) {
			d, _, _ := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: "org-019a0000000070008000000000000001"}, nil
			}
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				body := `{"data":{"endpoints":{"ssh":{"external_ip":"192.0.2.1","external_port":22,"username":"runner","host_key":"ssh-ed25519 fixture"}}}}`
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx, stop := InterruptContext(parent)
			defer stop()
			pin := ""
			d.RunSSH = func(childCtx context.Context, args []string) (int, error) {
				for _, arg := range args {
					if strings.HasPrefix(arg, "UserKnownHostsFile=") {
						pin = strings.TrimPrefix(arg, "UserKnownHostsFile=")
					}
				}
				raw, err := os.ReadFile(pin)
				if err != nil || string(raw) != "192.0.2.1 ssh-ed25519 fixture\n" {
					t.Fatal(pin, err)
				}
				if cancelParent {
					cancel()
					<-childCtx.Done()
					return 130, childCtx.Err()
				}
				return 1, errors.New("failed to launch ssh: fixture failure")
			}
			code := Execute(ctx, []string{"vm", "ssh", "cvm-019a0000000070008000000000000001"}, d)
			want := 1
			if cancelParent {
				want = 130
			}
			if code != want {
				t.Fatal(code)
			}
			if pin == "" {
				t.Fatal("SSH child was not called")
			}
			if _, err := os.Stat(pin); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("temporary pin remains", err)
			}
		})
	}
}

func TestVMSSHInvalidBooleanIsUsageError(t *testing.T) {
	d, _, _ := testDeps(t)
	if code := Execute(t.Context(), []string{"vm", "ssh", "--print=oops"}, d); code != 2 {
		t.Fatalf("exit=%d; want usage error 2", code)
	}
}
