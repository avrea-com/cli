package commands

import (
	"fmt"
	"github.com/avrea-com/cli/config"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestVMFirewallStream(t *testing.T) {
	const vm = "cvm-019a0000000070008000000000000001"
	const snapshot = `{"snapshot_ts":"2026-11-20T01:00:02Z","live_since_ts":"2026-11-20T01:00:00Z","partial":true,"summary":{"blocked_dns_queries":[{"qname":"blocked.example","ts":"2026-11-20T01:00:01Z","count":3}],"drops":[{"label":"default-deny","packets":7,"bytes":420}],"top_destinations":[{"dst_ip":"192.0.2.1"}]}}`
	for _, scenario := range []string{"text", "name", "missing name", "json", "ended", "unavailable", "refused"} {
		t.Run(scenario, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			ref := vm
			if scenario == "name" || scenario == "missing name" {
				ref = "Dev Box"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("missing firewall request authorization")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Method == http.MethodGet && r.URL.EscapedPath() == vmBase(inspectionOrg)+"/"+url.PathEscape(ref) {
					if scenario == "missing name" {
						http.NotFound(w, r)
						return
					}
					_, _ = fmt.Fprintf(w, `{"data":{"customer_vm_id":%q}}`, vm)
					return
				}
				if scenario == "missing name" || r.URL.Path != vmBase(inspectionOrg)+"/"+vm+"/observability/stream" || r.URL.RawQuery != "signals=flows" {
					t.Errorf("unexpected firewall stream request %s", r.URL)
					http.NotFound(w, r)
					return
				}
				if scenario == "refused" {
					http.Error(w, "private", http.StatusForbidden)
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.CloseNow() }()
				frames := []string{`{"signal":"metrics","type":"complete"}`}
				switch scenario {
				case "text", "name", "json":
					frames = append(frames, `{"signal":"flows","type":"tap_lost"}`, `{"signal":"flows","type":"snapshot","snapshot":`+snapshot+`}`)
				case "unavailable":
					frames = append(frames, `{"signal":"flows","type":"error","message":"unavailable"}`)
				}
				frames = append(frames, `{"type":"complete","signal":"flows"}`)
				for _, f := range frames {
					if conn.Write(r.Context(), websocket.MessageText, []byte(f)) != nil {
						return
					}
				}
				_, _, _ = conn.Read(r.Context())
			}))
			defer server.Close()
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: server.URL, Org: inspectionOrg, Token: "fixture"}, nil
			}
			args := []string{"vm", "firewall", ref}
			if scenario == "json" {
				args = append(args, "--json")
			}
			code := Execute(t.Context(), args, d)
			switch scenario {
			case "text", "name":
				for _, part := range []string{"partial since", "blocked.example", "3", "default-deny", "7", "420"} {
					if !strings.Contains(out.String(), part) {
						t.Errorf("missing %q in %s", part, out)
					}
				}
				if code != 0 || strings.Contains(out.String(), "192.0.2.1") || !strings.Contains(stderr.String(), "interrupted") {
					t.Fatal(code, out, stderr)
				}
			case "json":
				if code != 0 || !strings.Contains(out.String(), `"packets":7`) || strings.Count(out.String(), "\n") != 1 || !strings.Contains(stderr.String(), "interrupted") {
					t.Fatal(code, out, stderr)
				}
			case "ended":
				if code != 0 || out.Len() != 0 || !strings.Contains(stderr.String(), "without a live snapshot") {
					t.Fatal(code, out, stderr)
				}
			case "missing name":
				if code == 0 || out.Len() != 0 || !strings.Contains(stderr.String(), "avr vm list") {
					t.Fatal(code, out, stderr)
				}
			default:
				if code == 0 || out.Len() != 0 || !strings.Contains(stderr.String(), map[string]string{"unavailable": "unavailable", "refused": "403"}[scenario]) {
					t.Fatal(code, out, stderr)
				}
			}
		})
	}
}
