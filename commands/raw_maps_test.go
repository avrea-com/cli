package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

// Request tests use an in-memory transport: no write command reaches a service.
func TestMapRequestsPreserveOmissionNullEmptyAndFalse(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		args                           []string
		method, suffix, want, response string
	}{
		{"billing empty email", []string{"billing", "update-settings", "--email", ""}, "PUT", "/billing/settings", `{"billing_emails":[]}`, `{"data":{}}`},
		{"billing empty tax", []string{"billing", "update-settings", "--tax-id", ""}, "PUT", "/billing/settings", `{"tax_id":""}`, `{"data":{}}`},
		{"firewall null matchers", []string{"firewall", "add", "--action", "allow", "--any"}, "POST", "/firewall/rules", `{"action":"allow","is_default":true,"protocol":"any","cidr":null,"fqdn":null,"port_start":null,"port_end":null}`, `{"rule_id":"efr-fixture","position":1}`},
		{"vm clear lists", []string{"vm", "update", "cvm-fixture", "--name", "", "--clear-https-ports", "--egress-rules", "[]"}, "PATCH", "/vms/cvm-fixture", `{"rotate_password":false,"display_name":"","https_ports":[],"egress_rules":[]}`, `{"data":{}}`},
		{"vm pause false", []string{"vm", "pause", "cvm-fixture"}, "POST", "/vms/cvm-fixture/pause", `{"memory":false}`, `{"data":{}}`},
		{"vm resume false", []string{"vm", "resume", "cvm-fixture"}, "POST", "/vms/cvm-fixture/resume", `{"discard_memory":false}`, `{"data":{}}`},
		{"vm create defaults", []string{"vm", "create", "--os", "linux", "--size", "2-vcpu", "--json"}, "POST", "/vms", `{"os_type":"linux","size":"2-vcpu","enable_remote_desktop":false,"ttl_seconds":28800,"ssh_public_keys":[]}`, `{"data":{"vm":{"customer_vm_id":"cvm-fixture"},"password":null}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
			}
			calls := 0
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != tc.method || !strings.HasSuffix(r.URL.Path, tc.suffix) {
					t.Errorf("request %s %s", r.Method, r.URL.Path)
				}
				var got, want map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("body: %#v; want %#v", got, want)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
			})}
			if code := Execute(t.Context(), tc.args, d); code != 0 || calls != 1 {
				t.Fatal(code, calls, stderr.String())
			}
		})
	}
}

func TestNativeReceiptRejectsWrongTypesBeforeFollowing(t *testing.T) {
	base := `{"data":{"dispatch_id":"wap-019a0000000070008000000000000001","state":"pending","ref":"main","commit_sha":"abc","run_ids":[],"reason":null}}`
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"dispatch_id", true}, {"state", 1}, {"ref", nil}, {"commit_sha", []any{}},
		{"run_ids", nil}, {"run_ids", []any{true}}, {"reason", map[string]any{}},
	} {
		var body map[string]any
		if err := json.Unmarshal([]byte(base), &body); err != nil {
			t.Fatal(err)
		}
		body["data"].(map[string]any)[tc.key] = tc.value
		if _, err := parseNativeReceipt(body); err == nil {
			t.Fatalf("accepted %s: %#v", tc.key, tc.value)
		}
	}
}

func TestEmptyMutationResponsesRemainNull(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		response string
	}{
		{[]string{"vm", "update", "cvm-fixture", "--name", "new", "--json"}, "null"},
		{[]string{"vm", "delete", "cvm-fixture", "--yes", "--json"}, ""},
	} {
		d, out, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) {
			return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
		}
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
		})}
		if code := Execute(t.Context(), tc.args, d); code != 0 || out.String() != "null\n" {
			t.Fatal(code, out.String(), stderr.String())
		}
	}
}

func TestMalformedDefaultResponseDoesNotRetryMutation(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	calls := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodPost {
			t.Errorf("expected atomic upsert, got %s", r.Method)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(`[{"rule_id":"efr-fixture","is_default":"true"}]`))}, nil
	})}
	if code := Execute(t.Context(), []string{"firewall", "set-default", "--action", "allow"}, d); code == 0 || calls != 1 || !strings.Contains(stderr.String(), "expected a JSON object") {
		t.Fatal(code, calls, stderr.String())
	}
}
