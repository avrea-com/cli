package commands

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestPRTransportFailuresKeepUsefulContextWithoutPrivateDetails(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			d, _, stderr := testDeps(t)
			d.GitRemote = func(context.Context) (string, error) { return "", nil }
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "avr_fixture", Org: inspectionOrg}, nil
			}
			calls := 0
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == failAt {
					return nil, &net.OpError{Op: "dial", Net: "tcp", Err: io.ErrUnexpectedEOF}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"enabled\":true}")), Request: r}, nil
			})}
			if code := Execute(t.Context(), []string{"pr", "list"}, d); code != 1 || calls != failAt {
				t.Fatal(code, calls, stderr)
			}
			if !strings.Contains(stderr.String(), "Could not list pull requests") || !strings.Contains(stderr.String(), "ConnectError") || strings.Contains(stderr.String(), "unexpected EOF") || strings.Contains(stderr.String(), "avr_fixture") {
				t.Fatal(stderr.String())
			}
		})
	}
}
