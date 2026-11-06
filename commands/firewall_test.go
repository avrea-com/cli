package commands

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestFirewallDefaultNeverCreatesAfterUpdatingExistingRule(t *testing.T) {
	for _, status := range []int{200, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			d, out, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "fixture", Org: "org-019a0000000070008000000000000001"}, nil
			}
			methods := []string{}
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				methods = append(methods, r.Method)
				body := "{}"
				code := status
				if r.Method == http.MethodGet {
					code = 200
					body = `[{"rule_id":"efr-019a0000000070008000000000000001","action":"deny","is_default":true}]`
				}
				return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			code := Execute(t.Context(), []string{"firewall", "set-default", "--action", "allow"}, d)
			if code != 1 || strings.Join(methods, ",") != "GET,PATCH" {
				t.Fatal(code, methods, out, stderr)
			}
		})
	}
}
