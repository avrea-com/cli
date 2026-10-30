package commands

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestHealthTransportFailuresRetainJSONWithoutLeakingDetails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		reason string
	}{
		{"connect", errors.New("private network details"), "couldn't reach api.example.test"},
		{"timeout", &net.DNSError{Err: "private network details", IsTimeout: true}, "api.example.test took too long"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) { return config.Config{Host: "https://api.example.test"}, nil }
			calls := 0
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Authorization") != "" {
					t.Fatal("unexpected authentication")
				}
				return nil, tc.err
			})}
			code := Execute(t.Context(), []string{"health", "--json", "status"}, d)
			var record map[string]any
			if err := json.Unmarshal(out.Bytes(), &record); err != nil {
				t.Fatal(err, out)
			}
			if code != 1 || calls != 1 || record["status"] != "unreachable" || !strings.Contains(stderr.String(), tc.reason) || strings.Contains(stderr.String(), "private") {
				t.Fatal(code, calls, record, stderr)
			}
		})
	}
}
