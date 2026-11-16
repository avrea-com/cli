package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
	"github.com/spf13/cobra"
)

const (
	// Written in two parts: a whole key ID reads as another vendor's API key
	// to secret scanners.
	serviceTokenKeyID   = "key-" + "019a0000000070008000000000000002"
	serviceTokenFixture = "avt_fixture_private_secret"
	serviceTokenHost    = "https://api.example.test"
	serviceTokenSelf    = `{"organization_id":"` + inspectionOrg + `","api_key_id":"` + serviceTokenKeyID + `","name":"ci reader","group_id":"grp-019a0000000070008000000000000003","expires_at":"2026-12-16T10:00:00Z","expiring_soon":false,"grants":[{"resource_type":"repository","resource_id":"rep-019a0000000070008000000000000004","access_level":"read"},{"resource_type":"egress_firewall_rule","resource_id":null,"access_level":"read"}]}`
	// Older API revisions answer with the two identifiers only.
	serviceTokenSelfOld = `{"organization_id":"` + inspectionOrg + `","api_key_id":"` + serviceTokenKeyID + `"}`
)

type serviceTokenHarness struct {
	d           Dependencies
	out, stderr *bytes.Buffer
	requests    []string
}

func newServiceTokenHarness(t *testing.T, org string, handler func(*http.Request) (int, string)) *serviceTokenHarness {
	t.Helper()
	d, out, stderr := testDeps(t)
	h := &serviceTokenHarness{d: d, out: out, stderr: stderr}
	h.d.GitRemote = func(context.Context) (string, error) { return "", nil }
	h.d.Getenv = func(key string) string {
		switch key {
		case "AVR_TOKEN":
			return serviceTokenFixture
		case "AVR_ORG":
			// The only place a service token's organization is configured:
			// the stored default belongs to the signed-in person.
			return org
		}
		return ""
	}
	h.d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: serviceTokenHost, Token: serviceTokenFixture, Org: org}, nil
	}
	h.d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+serviceTokenFixture {
			t.Errorf("%s %s: missing bearer token", r.Method, r.URL.Path)
		}
		h.requests = append(h.requests, r.Method+" "+r.URL.Path)
		status, body := handler(r)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	return h
}

func (h *serviceTokenHarness) run(t *testing.T, args ...string) int {
	t.Helper()
	return Execute(t.Context(), args, h.d)
}

func serviceTokenRoutes(self string) func(*http.Request) (int, string) {
	return func(r *http.Request) (int, string) {
		switch {
		case r.URL.Path == "/service-tokens/self":
			return 200, self
		case r.URL.Path == "/orgs/"+inspectionOrg+"/repos":
			return 200, `{"data":[]}`
		case strings.HasPrefix(r.URL.Path, "/users/me"):
			return 404, `{"detail":"Not Found"}`
		}
		return 599, `{}`
	}
}

func TestServiceTokenOrganizationResolvedFromSelf(t *testing.T) {
	h := newServiceTokenHarness(t, "", serviceTokenRoutes(serviceTokenSelf))
	if code := h.run(t, "repo", "list"); code != 0 || !strings.Contains(h.out.String(), "No repositories found.") {
		t.Fatal(code, h.out, h.stderr)
	}
	if want := []string{"GET /service-tokens/self", "GET /orgs/" + inspectionOrg + "/repos"}; !slices.Equal(h.requests, want) {
		t.Fatal(h.requests)
	}
}

func TestServiceTokenOrganizationIDNeedsNoSelfRequest(t *testing.T) {
	for _, tc := range []struct{ org, option string }{{inspectionOrg, ""}, {"", inspectionOrg}} {
		h := newServiceTokenHarness(t, tc.org, serviceTokenRoutes(serviceTokenSelf))
		args := []string{"repo", "list"}
		if tc.option != "" {
			args = append(args, "--org", tc.option)
		}
		if code := h.run(t, args...); code != 0 {
			t.Fatal(code, h.stderr)
		}
		if want := []string{"GET /orgs/" + inspectionOrg + "/repos"}; !slices.Equal(h.requests, want) {
			t.Fatal(tc, h.requests)
		}
	}
}

func TestServiceTokenRefusesOrganizationSlug(t *testing.T) {
	for _, tc := range []struct{ org, option string }{{"acme", ""}, {"", "acme"}} {
		h := newServiceTokenHarness(t, tc.org, serviceTokenRoutes(serviceTokenSelf))
		args := []string{"repo", "list"}
		if tc.option != "" {
			args = append(args, "--org", tc.option)
		}
		code := h.run(t, args...)
		message := h.stderr.String()
		if code != 1 || len(h.requests) != 0 || !strings.Contains(message, "service token is bound to one organization") || !strings.Contains(message, "'acme'") || !strings.Contains(message, "org-...") {
			t.Fatal(tc, code, h.requests, message)
		}
	}
}

func TestServiceTokenSelfReadOncePerInvocation(t *testing.T) {
	h := newServiceTokenHarness(t, "", serviceTokenRoutes(serviceTokenSelf))
	cfg := config.Config{Host: serviceTokenHost, Token: serviceTokenFixture}
	probe := func(c *cobra.Command, _ []string) error {
		d := h.d.defaults()
		api := d.apiClient(cfg)
		for range 2 {
			id, err := resolveOrg(c.Context(), d, cfg, api, "")
			if err != nil || id != inspectionOrg {
				t.Errorf("id=%s err=%v", id, err)
			}
			if slug := orgSlug(c.Context(), cfg, api, id); slug != id {
				t.Errorf("slug=%s", slug)
			}
		}
		identity, err := serviceIdentity(c.Context(), cfg, api)
		if err != nil {
			return err
		}
		identity["organization_id"] = "changed by a caller"
		if again, _ := serviceIdentity(c.Context(), cfg, api); again["organization_id"] != inspectionOrg {
			t.Error("cached identity was changed through a caller's copy")
		}
		return nil
	}
	for want := 1; want <= 2; want++ {
		// Execute builds a new command tree for every invocation.
		root := NewRoot(h.d)
		root.AddCommand(&cobra.Command{Use: "probe", RunE: probe})
		if code := ExecuteRoot(t.Context(), root, []string{"probe"}, h.d.Err, h.d); code != 0 {
			t.Fatal(code, h.stderr)
		}
		if len(h.requests) != want {
			t.Fatalf("wanted %d self requests, got %v", want, h.requests)
		}
	}
}

func TestServiceTokenAuthStatusText(t *testing.T) {
	expiring := strings.Replace(serviceTokenSelf, `"expiring_soon":false`, `"expiring_soon":true`, 1)
	never := strings.Replace(serviceTokenSelf, `"expires_at":"2026-12-16T10:00:00Z"`, `"expires_at":null`, 1)
	for _, tc := range []struct {
		name, self  string
		want, avoid []string
	}{
		{"full", serviceTokenSelf, []string{"Credential    service token", "Name          ci reader", "Token ID      " + serviceTokenKeyID, "Organization  " + inspectionOrg, "Group         grp-019a0000000070008000000000000003", "Expires       2026-12-16T10:00:00.00Z", "Grants        1 repo, all firewall rules", "Host          " + serviceTokenHost, "Token         avt_****"}, []string{"expires soon", "avr auth login", "avr config set org"}},
		{"expiring soon", expiring, []string{"Expires       2026-12-16T10:00:00.00Z", "expires soon", "rotate"}, nil},
		{"never expires", never, []string{"Expires       never"}, []string{"expires soon"}},
		{"old API", serviceTokenSelfOld, []string{"Name          -", "Token ID      " + serviceTokenKeyID, "Organization  " + inspectionOrg, "Group         -", "Expires       -", "Grants        -"}, []string{"expires soon"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newServiceTokenHarness(t, "", serviceTokenRoutes(tc.self))
			if code := h.run(t, "auth", "status"); code != 0 {
				t.Fatal(code, h.stderr)
			}
			text := h.out.String()
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q in:\n%s", want, text)
				}
			}
			for _, avoid := range append(tc.avoid, serviceTokenFixture) {
				if strings.Contains(text, avoid) {
					t.Errorf("unexpected %q in:\n%s", avoid, text)
				}
			}
			if want := []string{"GET /service-tokens/self"}; !slices.Equal(h.requests, want) {
				t.Fatal(h.requests)
			}
		})
	}
}

func TestTokenGrantsSummaryCountsFirewallRules(t *testing.T) {
	rule := func(id any) map[string]any {
		return map[string]any{"resource_type": "egress_firewall_rule", "resource_id": id, "access_level": "read"}
	}
	repo := map[string]any{"resource_type": "repository", "resource_id": "rep-1", "access_level": "read"}
	for _, tc := range []struct {
		grants []any
		want   string
	}{
		{nil, "-"},
		{[]any{repo, rule("fwr-1")}, "1 repo, 1 firewall rule"},
		{[]any{rule("fwr-1"), rule("fwr-2")}, "2 firewall rules"},
		{[]any{repo, repo, rule(nil)}, "2 repos, all firewall rules"},
	} {
		if got := tokenGrantsSummary(map[string]any{"grants": tc.grants}); got != tc.want {
			t.Errorf("%v: %q != %q", tc.grants, got, tc.want)
		}
	}
}

func TestServiceTokenAuthStatusJSON(t *testing.T) {
	for _, tc := range []struct {
		name, self string
		want       map[string]any
	}{
		{"full", serviceTokenSelf, map[string]any{"credential_type": "service_token", "name": "ci reader", "organization_id": inspectionOrg, "api_key_id": serviceTokenKeyID, "group_id": "grp-019a0000000070008000000000000003", "expires_at": "2026-12-16T10:00:00Z", "expiring_soon": false, "host": serviceTokenHost, "default_org": nil, "email": nil, "user_id": nil}},
		{"old API", serviceTokenSelfOld, map[string]any{"credential_type": "service_token", "name": nil, "organization_id": inspectionOrg, "api_key_id": serviceTokenKeyID, "group_id": nil, "expires_at": nil, "expiring_soon": nil, "grants": nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newServiceTokenHarness(t, "", serviceTokenRoutes(tc.self))
			if code := h.run(t, "auth", "status", "--json", "*"); code != 0 {
				t.Fatal(code, h.stderr)
			}
			var record map[string]any
			if err := json.Unmarshal(h.out.Bytes(), &record); err != nil {
				t.Fatal(err, h.out)
			}
			for key, want := range tc.want {
				if got, ok := record[key]; !ok || got != want {
					t.Errorf("%s: %#v (present %v) != %#v", key, got, ok, want)
				}
			}
			if _, ok := record["token"]; ok || strings.Contains(h.out.String(), serviceTokenFixture) {
				t.Fatal("token leaked", h.out)
			}
		})
	}
	h := newServiceTokenHarness(t, "", serviceTokenRoutes(serviceTokenSelf))
	if code := h.run(t, "auth", "status", "--json", "grants", "--jq", ".grants[1].resource_type"); code != 0 || strings.TrimSpace(h.out.String()) != "egress_firewall_rule" {
		t.Fatal(code, h.out, h.stderr)
	}
}

func TestAuthStatusFieldDiscoveryByCredential(t *testing.T) {
	serviceOnly := []string{"organization_id", "api_key_id", "group_id", "expires_at", "expiring_soon", "grants"}
	for _, token := range []string{"avr_fixture", "avs_fixture", serviceTokenFixture} {
		d, out, stderr := testDeps(t)
		d.LoadConfig = func() (config.Config, error) { return config.Config{Host: serviceTokenHost, Token: token}, nil }
		d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("discovery sent a request"); return nil, nil })}
		if code := Execute(t.Context(), []string{"auth", "status", "--json", "?"}, d); code != 0 {
			t.Fatal(code, stderr)
		}
		for _, field := range append(slices.Clone(statusFields), serviceOnly...) {
			listed := strings.Contains(out.String(), "  "+field+"\n")
			if want := slices.Contains(statusFields, field) || config.IsService(token); listed != want {
				t.Errorf("%s: field %s listed=%v", token, field, listed)
			}
		}
	}
}

func TestServiceTokenAPIErrors(t *testing.T) {
	for _, tc := range []struct {
		status      int
		args        []string
		want, avoid []string
	}{
		{401, []string{"auth", "status"}, []string{"rejected the service token (HTTP 401): token expired", "expired or revoked", "rotate it or mint a new one", "AVR_TOKEN"}, []string{"avr auth login", "avr token create"}},
		{403, []string{"repo", "list"}, []string{"(HTTP 403): refused", "a service token reaches only what its access names", "operations open to service tokens"}, []string{"avr org list", "avr auth login", "scoped token"}},
		{404, []string{"repo", "list"}, []string{"(HTTP 404): refused", "a service token reaches only what its access names"}, []string{"avr org list", "avr auth login", "scoped token"}},
	} {
		detail := "refused"
		if tc.status == 401 {
			detail = "token expired"
		}
		h := newServiceTokenHarness(t, inspectionOrg, func(*http.Request) (int, string) { return tc.status, `{"detail":"` + detail + `"}` })
		code := h.run(t, tc.args...)
		message := h.stderr.String()
		if code == 0 || strings.Contains(message, serviceTokenFixture) {
			t.Fatal(tc.status, code, message)
		}
		for _, want := range tc.want {
			if !strings.Contains(message, want) {
				t.Errorf("%d: missing %q in %q", tc.status, want, message)
			}
		}
		for _, avoid := range tc.avoid {
			if strings.Contains(message, avoid) {
				t.Errorf("%d: unexpected %q in %q", tc.status, avoid, message)
			}
		}
	}
}

func TestServiceTokenConfigStatus(t *testing.T) {
	t.Run("verified", func(t *testing.T) {
		h := newServiceTokenHarness(t, "", serviceTokenRoutes(serviceTokenSelf))
		if code := h.run(t, "config"); code != 0 {
			t.Fatal(code, h.stderr)
		}
		text := h.out.String()
		if !strings.Contains(text, "authenticated with service token ci reader (from AVR_TOKEN)") || !strings.Contains(text, "Active org:   "+inspectionOrg+" (from service token)") || strings.Contains(text, serviceTokenFixture) {
			t.Fatal(text)
		}
		if want := []string{"GET /service-tokens/self"}; !slices.Equal(h.requests, want) {
			t.Fatal(h.requests)
		}
	})
	t.Run("old API names the token by ID", func(t *testing.T) {
		h := newServiceTokenHarness(t, inspectionOrg, serviceTokenRoutes(serviceTokenSelfOld))
		if code := h.run(t, "config"); code != 0 || !strings.Contains(h.out.String(), "authenticated with service token "+serviceTokenKeyID+" (from AVR_TOKEN)") || !strings.Contains(h.out.String(), "Active org:   "+inspectionOrg+" (from AVR_ORG)") {
			t.Fatal(code, h.out, h.stderr)
		}
	})
	for _, status := range []int{401, 503} {
		h := newServiceTokenHarness(t, "", func(*http.Request) (int, string) { return status, `{"detail":"no"}` })
		if code := h.run(t, "config"); code != 0 {
			t.Fatal(status, code, h.stderr)
		}
		text := h.out.String()
		if !strings.Contains(text, "token set but /service-tokens/self failed (from AVR_TOKEN)") || strings.Contains(text, "/users/me") || !strings.Contains(text, "Active org:   (not set)") {
			t.Fatal(status, text)
		}
		if want := []string{"GET /service-tokens/self"}; !slices.Equal(h.requests, want) {
			t.Fatal(status, h.requests)
		}
	}
}

func TestServiceTokenConfigOrgRefused(t *testing.T) {
	for _, args := range [][]string{{"config", "set", "org", "acme"}, {"config", "unset", "org"}} {
		h := newServiceTokenHarness(t, "", serviceTokenRoutes(serviceTokenSelf))
		if err := h.d.Store.Set(serviceTokenHost, "default_org", inspectionOrg); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(h.d.Store.Path)
		if err != nil {
			t.Fatal(err)
		}
		code := h.run(t, args...)
		message := h.stderr.String()
		after, _ := os.ReadFile(h.d.Store.Path)
		if code != 1 || len(h.requests) != 0 || !bytes.Equal(before, after) || !strings.Contains(message, "A service token is bound to one organization") || !strings.Contains(message, "resolved automatically") || strings.Contains(message, "scoped") {
			t.Fatal(args, code, h.requests, message)
		}
	}
}

func TestServiceTokenLogoutRefusedWithoutRequests(t *testing.T) {
	h := newServiceTokenHarness(t, "", func(r *http.Request) (int, string) {
		t.Errorf("logout sent %s %s", r.Method, r.URL.Path)
		return 599, `{}`
	})
	if err := h.d.Store.Set(serviceTokenHost, "token", "avr_stored_personal"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(h.d.Store.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"logout"}, {"auth", "logout"}} {
		h.stderr.Reset()
		code := h.run(t, args...)
		message := h.stderr.String()
		after, _ := os.ReadFile(h.d.Store.Path)
		if code != 1 || len(h.requests) != 0 || !bytes.Equal(before, after) || h.out.Len() != 0 || !strings.Contains(message, "does not revoke a service token") || !strings.Contains(message, "unset AVR_TOKEN") || !strings.Contains(message, "organization admin") {
			t.Fatal(args, code, h.requests, message)
		}
	}
}

func TestServiceTokenPersonOnlyCommandsRefusedBeforeRequests(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"feedback", "hello"}, "service tokens cannot send or read feedback"},
		{[]string{"feedback", "status", feedbackTestID}, "service tokens cannot send or read feedback"},
		{[]string{"pr", "comment", "42", "--repo", "acme/web", "--body", "x"}, "service tokens cannot post comments"},
	} {
		h := newServiceTokenHarness(t, inspectionOrg, func(r *http.Request) (int, string) {
			t.Errorf("%v sent %s %s", tc.args, r.Method, r.URL.Path)
			return 599, `{}`
		})
		code := h.run(t, tc.args...)
		message := h.stderr.String()
		if code != 4 || len(h.requests) != 0 || h.out.Len() != 0 || !strings.Contains(message, tc.want) || !strings.Contains(message, "unset AVR_TOKEN if it contains the service token") || strings.Contains(message, "scoped") {
			t.Fatal(tc.args, code, message)
		}
	}
}

func TestServiceTokenRunURLNeedsRunID(t *testing.T) {
	h := newServiceTokenHarness(t, "", serviceTokenRoutes(serviceTokenSelf))
	cfg := config.Config{Host: serviceTokenHost, Token: serviceTokenFixture}
	_, _, err := resolveRunOrg(t.Context(), h.d.defaults(), cfg, h.d.defaults().apiClient(cfg), runReference{id: "run-1", orgSlug: "acme"}, "")
	if err == nil || !strings.Contains(err.Error(), "a service token cannot resolve a run URL's organization") || len(h.requests) != 0 {
		t.Fatal(err, h.requests)
	}
}

func TestServiceTokenStatusChecksOutputOptionsBeforeRequest(t *testing.T) {
	for _, args := range [][]string{
		{"auth", "status", "--json", "organization_id", "--jq", "("},
		{"auth", "status", "--json", "token"},
	} {
		h := newServiceTokenHarness(t, "", serviceTokenRoutes(serviceTokenSelf))
		if code := h.run(t, args...); code == 0 || len(h.requests) != 0 {
			t.Fatal(args, code, h.requests, h.out, h.stderr)
		}
	}
}

func TestServiceTokenGitHubSetupRefusedBeforeRequest(t *testing.T) {
	h := newServiceTokenHarness(t, "", serviceTokenRoutes(serviceTokenSelf))
	code := h.run(t, "org", "install", "add", "--no-browser")
	if code != 4 || len(h.requests) != 0 || !strings.Contains(h.stderr.String(), "needs a signed-in account; service tokens cannot install the GitHub App") {
		t.Fatal(code, h.requests, h.stderr)
	}
}
