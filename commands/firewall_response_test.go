package commands

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestFirewallResponseFormats(t *testing.T) {
	second := "efr-019a0000000070008000000000000002"
	defaultID := "efr-019a0000000070008000000000000003"
	rules := `[{"rule_id":"` + firewallTestRule + `","position":0,"fqdn":"first.example.com","action":"allow"},{"rule_id":"` + second + `","position":1,"fqdn":"second.example.com","action":"deny"},{"rule_id":"` + defaultID + `","position":2,"is_default":true,"action":"deny"}]`
	for _, format := range []string{"array", "envelope"} {
		body := rules
		if format == "envelope" {
			body = `{"data":` + rules + `,"has_hidden_rules":false}`
		}
		for _, operation := range []string{"list", "json", "move"} {
			t.Run(format+"/"+operation, func(t *testing.T) {
				writes := 0
				_, run := firewallTestDeps(t, func(r *http.Request) (int, string) {
					if r.Method == "GET" && r.URL.Path == "/orgs/"+firewallTestOrg+"/firewall/rules" {
						return 200, body
					}
					if operation != "move" || r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/rules/reorder") {
						t.Fatalf("unexpected request %s %s", r.Method, r.URL)
					}
					var request struct {
						IDs []string `json:"rule_ids"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(request.IDs, []string{second, firewallTestRule, defaultID}) {
						t.Fatalf("reorder = %v", request.IDs)
					}
					writes++
					return 200, rules
				})
				args := []string{"list"}
				if operation == "json" {
					args = append(args, "--json")
				}
				if operation == "move" {
					args = []string{"move", firewallTestRule, "--to", "1"}
				}
				code, out, stderr := run(args)
				if code != 0 {
					t.Fatalf("exit %d: %s %s", code, out, stderr)
				}
				switch operation {
				case "json":
					var got, want []map[string]any
					if err := json.Unmarshal([]byte(out), &got); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(rules), &want); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("output = %s", out)
					}
				case "list":
					for _, matcher := range []string{"first.example.com", "second.example.com", "deny"} {
						if !strings.Contains(out, matcher) {
							t.Fatalf("missing %s: %s", matcher, out)
						}
					}
				case "move":
					if writes != 1 {
						t.Fatalf("writes = %d", writes)
					}
				}
			})
		}
	}
}

func TestFirewallResponseEmptyAndMalformed(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"empty array", "[]", true},
		{"empty envelope", `{"data":[],"has_hidden_rules":true}`, true},
		{"missing data", `{"has_hidden_rules":false}`, false},
		{"null data", `{"data":null,"has_hidden_rules":false}`, false},
		{"object data", `{"data":{}}`, false},
		{"non-record", `{"data":["rule"]}`, false},
		{"trailing JSON", `{"data":[]} []`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, run := firewallTestDeps(t, func(r *http.Request) (int, string) {
				if r.Method != "GET" {
					t.Fatalf("unexpected write %s", r.Method)
				}
				return 200, tc.body
			})
			code, out, stderr := run([]string{"list", "--json"})
			if (code == 0) != tc.valid {
				t.Fatalf("exit %d: %s %s", code, out, stderr)
			}
			if tc.valid && strings.TrimSpace(out) != "[]" {
				t.Fatalf("output = %s", out)
			}
		})
	}
}
