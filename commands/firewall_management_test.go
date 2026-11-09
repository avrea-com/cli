package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

const firewallTestOrg = "org-019a0000000070008000000000000001"
const firewallTestRepo = "rep-019a0000000070008000000000000001"
const firewallTestRule = "efr-019a0000000070008000000000000001"

func firewallTestDeps(t *testing.T, handler func(*http.Request) (int, string)) (Dependencies, func([]string) (int, string, string)) {
	t.Helper()
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: firewallTestOrg}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		status, body := handler(r)
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	return d, func(args []string) (int, string, string) {
		code := Execute(t.Context(), append([]string{"firewall"}, args...), d)
		return code, out.String(), stderr.String()
	}
}

func TestFirewallEditPartialAndClearFields(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
		want  map[string]any
	}{
		{"action", []string{"--action", "deny"}, map[string]any{"action": "deny"}},
		{"target", []string{"--fqdn", "example.com"}, map[string]any{"fqdn": "example.com", "cidr": nil}},
		{"cidr", []string{"--cidr", "192.0.2.0/24"}, map[string]any{"cidr": "192.0.2.0/24", "fqdn": nil}},
		{"icmp", []string{"--proto", "icmp"}, map[string]any{"protocol": "icmp", "port_start": nil, "port_end": nil}},
		{"any", []string{"--proto", "any"}, map[string]any{"protocol": "any", "port_start": nil, "port_end": nil}},
		{"all ports", []string{"--ports", "all"}, map[string]any{"port_start": nil, "port_end": nil}},
		{"ports", []string{"--ports", "0-65535"}, map[string]any{"port_start": float64(0), "port_end": float64(65535)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, run := firewallTestDeps(t, func(r *http.Request) (int, string) {
				calls++
				if r.Method != "PATCH" || !strings.HasSuffix(r.URL.Path, "/rules/"+firewallTestRule) {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL)
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				a, _ := json.Marshal(got)
				b, _ := json.Marshal(tc.want)
				if string(a) != string(b) {
					t.Fatalf("body %s want %s", a, b)
				}
				return 200, `{"rule_id":"` + firewallTestRule + `"}`
			})
			code, out, err := run(append([]string{"edit", firewallTestRule}, tc.flags...))
			if code != 0 || calls != 1 {
				t.Fatal(code, out, err, calls)
			}
		})
	}
}

func TestFirewallInvalidEditsDoNotWrite(t *testing.T) {
	for _, flags := range [][]string{{}, {"--cidr", "::/0"}, {"--fqdn", "a.test", "--cidr", "192.0.2.0/24"}, {"--proto", "icmp", "--ports", "443"}, {"--ports", "65536"}, {"--fqdn", ""}} {
		_, run := firewallTestDeps(t, func(r *http.Request) (int, string) { t.Fatalf("unexpected request %s", r.URL); return 500, "" })
		code, _, _ := run(append([]string{"edit", firewallTestRule}, flags...))
		if code == 0 {
			t.Fatalf("accepted %v", flags)
		}
	}
}

func TestFirewallInheritOnlyDeletesRepoDefault(t *testing.T) {
	for _, exists := range []bool{false, true} {
		deletes := 0
		_, run := firewallTestDeps(t, func(r *http.Request) (int, string) {
			if !strings.Contains(r.URL.Path, "/repos/"+firewallTestRepo+"/") {
				t.Fatal(r.URL)
			}
			if r.Method == "GET" {
				if exists {
					return 200, `[{"rule_id":"` + firewallTestRule + `","is_default":true},{"rule_id":"efr-019a0000000070008000000000000002","fqdn":"example.com"}]`
				}
				return 200, "[]"
			}
			if r.Method != "DELETE" || !strings.HasSuffix(r.URL.Path, "/"+firewallTestRule) {
				t.Fatal(r.Method, r.URL)
			}
			deletes++
			return 204, ""
		})
		code, out, err := run([]string{"set-default", "--inherit", "--repo", firewallTestRepo})
		if code != 0 || exists != (deletes == 1) {
			t.Fatal(code, out, err, deletes)
		}
	}
}

func TestFirewallInheritRequiresRepoAndExcludesAction(t *testing.T) {
	for _, args := range [][]string{{"set-default", "--inherit"}, {"set-default", "--inherit", "--repo", firewallTestRepo, "--action", "allow"}, {"set-default", "--repo", firewallTestRepo}} {
		_, run := firewallTestDeps(t, func(r *http.Request) (int, string) { t.Fatal(r.URL); return 500, "" })
		code, _, _ := run(args)
		if code == 0 {
			t.Fatal(args)
		}
	}
}

func TestFirewallImportPreviewAndApply(t *testing.T) {
	for _, repo := range []string{"", firewallTestRepo} {
		for _, mode := range []string{"append", "replace"} {
			for _, dry := range []bool{false, true} {
				t.Run(repo+mode+fmt.Sprint(dry), func(t *testing.T) {
					dir := t.TempDir()
					file := filepath.Join(dir, "rules.json")
					if err := os.WriteFile(file, []byte(`{"version":1,"rules":[{"action":"deny","fqdn":"example.com"}]}`), 0600); err != nil {
						t.Fatal(err)
					}
					applied := 0
					previews := 0
					_, run := firewallTestDeps(t, func(r *http.Request) (int, string) {
						expected := "/orgs/" + firewallTestOrg
						if repo != "" {
							expected += "/repos/" + repo
						}
						expected += "/firewall/rule-imports"
						var request map[string]any
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
							t.Fatal(err)
						}
						if request["mode"] != mode {
							t.Fatal(request)
						}
						if r.URL.Path == expected+"/preview" {
							previews++
							return 200, firewallPreviewJSON(false)
						}
						if r.URL.Path != expected {
							t.Fatal(r.URL)
						}
						if request["expected_revision"] != strings.Repeat("a", 64) {
							t.Fatal(request)
						}
						applied++
						return 200, "[]"
					})
					args := []string{"import", "--file", file, "--mode", mode, "--yes"}
					if repo != "" {
						args = append(args, "--repo", repo)
					}
					if dry {
						args = append(args, "--dry-run")
					}
					code, out, err := run(args)
					if code != 0 || previews != 1 || applied != boolCount(!dry) || !strings.Contains(out, "1 added") {
						t.Fatal(code, out, err, previews, applied)
					}
				})
			}
		}
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
func firewallPreviewJSON(noop bool) string {
	before := `{"version":1,"rules":[]}`
	after := `{"version":1,"rules":[{"action":"deny","fqdn":"example.com","protocol":"any","is_default":false}]}`
	if noop {
		before = after
	}
	return `{"policy":` + after + `,"before":` + before + `,"after":` + after + `,"revision":"` + strings.Repeat("a", 64) + `","added":1,"removed":0,"skipped":0}`
}

func TestFirewallImportNoopAndConflict(t *testing.T) {
	for _, noop := range []bool{false, true} {
		calls := 0
		d, _ := firewallTestDeps(t, func(r *http.Request) (int, string) {
			calls++
			if strings.HasSuffix(r.URL.Path, "/preview") {
				return 200, firewallPreviewJSON(noop)
			}
			return 409, `{"detail":"Policy changed since preview"}`
		})
		d.In = strings.NewReader(`{"version":1,"rules":[]}`)
		code := Execute(t.Context(), []string{"firewall", "import", "--file", "-"}, d)
		if noop {
			if code != 0 || calls != 1 {
				t.Fatal(code, calls)
			}
		} else if code == 0 || calls != 2 {
			t.Fatal(code, calls)
		}
	}
}

func TestFirewallImportRejectsInvalidInputAndRequiresConfirmation(t *testing.T) {
	for _, input := range []string{`{`, `[]`, `{"version":2,"rules":[]}`, `{"version":1}`, `{"version":1,"rules":[],"extra":true}`, `{"version":1,"rules":[]} {}`} {
		d, _ := firewallTestDeps(t, func(r *http.Request) (int, string) { t.Fatal("invalid input made request", r.URL); return 500, "" })
		d.In = strings.NewReader(input)
		if code := Execute(t.Context(), []string{"firewall", "import", "--file", "-"}, d); code == 0 {
			t.Fatal(input)
		}
	}
	calls := 0
	d, _ := firewallTestDeps(t, func(r *http.Request) (int, string) {
		calls++
		if !strings.HasSuffix(r.URL.Path, "/preview") {
			t.Fatal("unconfirmed write", r.URL)
		}
		return 200, firewallPreviewJSON(false)
	})
	d.In = strings.NewReader(`{"version":1,"rules":[]}`)
	if code := Execute(t.Context(), []string{"firewall", "import", "--file", "-", "--mode", "replace"}, d); code == 0 || calls != 1 {
		t.Fatal(code, calls)
	}
}

func TestFirewallExportUsesPortableAPI(t *testing.T) {
	for _, repo := range []string{"", firewallTestRepo} {
		_, run := firewallTestDeps(t, func(r *http.Request) (int, string) {
			if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/rules/export") {
				t.Fatal(r.Method, r.URL)
			}
			if strings.Contains(r.URL.Path, "/repos/") != (repo != "") {
				t.Fatal(r.URL)
			}
			return 200, `{"version":1,"rules":[]}`
		})
		args := []string{"export"}
		if repo != "" {
			args = append(args, "--repo", repo)
		}
		code, out, err := run(args)
		var doc map[string]any
		if code != 0 || json.Unmarshal([]byte(out), &doc) != nil || doc["version"] != float64(1) {
			t.Fatal(code, out, err)
		}
	}
}

func TestFirewallShowSourcesAndRepoNameResolution(t *testing.T) {
	_, run := firewallTestDeps(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/repos/resolve") {
			if r.URL.Query().Get("name") != "acme/app" {
				t.Fatal(r.URL)
			}
			return 200, `{"data":{"repository_id":"` + firewallTestRepo + `","full_name":"acme/app"}}`
		}
		if !strings.Contains(r.URL.Path, "/repos/"+firewallTestRepo+"/") {
			t.Fatal(r.URL)
		}
		if strings.HasSuffix(r.URL.Path, "/resolved") {
			return 200, `{"rules":[{"rule_id":"` + firewallTestRule + `","fqdn":"repo.test","action":"deny"},{"rule_id":"efr-019a0000000070008000000000000002","fqdn":"org.test","action":"allow"},{"is_default":true,"action":"allow"}]}`
		}
		return 200, `[{"rule_id":"` + firewallTestRule + `"}]`
	})
	code, out, err := run([]string{"show", "--repo", "acme/app"})
	if code != 0 || !strings.Contains(out, "repository") || !strings.Contains(out, "organization") || !strings.Contains(out, "platform default") {
		t.Fatal(code, out, err)
	}
}

func TestFirewallImportZeroCountChangesStillApply(t *testing.T) {
	for _, after := range []string{
		`[{"action":"deny","is_default":true}]`,
		`[{"action":"allow","fqdn":"second.test"},{"action":"deny","fqdn":"first.test"}]`,
	} {
		applied := false
		d, _ := firewallTestDeps(t, func(r *http.Request) (int, string) {
			if strings.HasSuffix(r.URL.Path, "/preview") {
				before := `[{"action":"allow","is_default":true}]`
				if strings.Contains(after, "second.test") {
					before = `[{"action":"deny","fqdn":"first.test"},{"action":"allow","fqdn":"second.test"}]`
				}
				return 200, `{"policy":{"version":1,"rules":` + after + `},"before":{"version":1,"rules":` + before + `},"after":{"version":1,"rules":` + after + `},"revision":"` + strings.Repeat("b", 64) + `","added":0,"removed":0,"skipped":0}`
			}
			applied = true
			return 200, "[]"
		})
		d.In = strings.NewReader(`{"version":1,"rules":[]}`)
		if code := Execute(t.Context(), []string{"firewall", "import", "--file", "-", "--mode", "replace", "--yes"}, d); code != 0 || !applied {
			t.Fatal(code, applied)
		}
	}
}

func TestFirewallExportFileAndRefuseOverwrite(t *testing.T) {
	_, run := firewallTestDeps(t, func(r *http.Request) (int, string) { return 200, `{"version":1,"rules":[]}` })
	file := filepath.Join(t.TempDir(), "policy.json")
	code, out, stderr := run([]string{"export", "--file", file})
	if code != 0 {
		t.Fatal(code, out, stderr)
	}
	initial, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ = run([]string{"export", "--file", file})
	if code == 0 {
		t.Fatal("overwrote existing file")
	}
	actual, err := os.ReadFile(file)
	if err != nil || string(initial) != string(actual) {
		t.Fatal(err, string(actual))
	}
}

func TestFirewallShowImplicitDefaultAndJSONCompatibility(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		calls := 0
		_, run := firewallTestDeps(t, func(r *http.Request) (int, string) {
			calls++
			if strings.HasSuffix(r.URL.Path, "/resolved") {
				return 200, `{"rules":[]}`
			}
			return 200, "[]"
		})
		args := []string{"show", "--repo", firewallTestRepo}
		if asJSON {
			args = append(args, "--json")
		}
		code, out, err := run(args)
		if code != 0 {
			t.Fatal(code, out, err)
		}
		if asJSON {
			if strings.TrimSpace(out) != "[]" || calls != 1 {
				t.Fatal(out, calls)
			}
		} else if !strings.Contains(out, "platform default") || !strings.Contains(out, "allow") {
			t.Fatal(out)
		}
	}
}

func TestFirewallValidationDetailsArePreserved(t *testing.T) {
	_, run := firewallTestDeps(t, func(r *http.Request) (int, string) {
		return 422, `{"detail":[{"loc":["body","fqdn"],"msg":"Wildcard apex is a shared public suffix"}]}`
	})
	code, _, err := run([]string{"edit", firewallTestRule, "--fqdn", "*.com"})
	if code == 0 || !strings.Contains(err, "fqdn: Wildcard apex is a shared public suffix") {
		t.Fatal(code, err)
	}
}
