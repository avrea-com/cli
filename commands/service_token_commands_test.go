package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

// Fixture IDs are split so no literal reads as another vendor's API key.
const (
	manageTokenID  = "key-" + "019a0000000070008000000000000011"
	manageGroupID  = "grp-" + "019a0000000070008000000000000012"
	manageRuleA    = "efr-" + "019a0000000070008000000000000013"
	manageRuleB    = "efr-" + "019a0000000070008000000000000014"
	manageRepoA    = "rep-" + "019a0000000070008000000000000015"
	manageRepoB    = "rep-" + "019a0000000070008000000000000016"
	manageSecret   = "avt_minted_fixture_secret"
	managePersonal = "avr_fixture_personal_key"
	manageBase     = "/orgs/" + inspectionOrg + "/service-tokens"
)

type manageRequest struct {
	method, path string
	query        url.Values
	body         map[string]any
}

type manageHarness struct {
	d           Dependencies
	out, stderr *bytes.Buffer
	requests    []manageRequest
}

type manageReply struct {
	status int
	header http.Header
	body   string
}

func newManageHarness(t *testing.T, token string, handler func(manageRequest) manageReply) *manageHarness {
	t.Helper()
	d, out, stderr := testDeps(t)
	h := &manageHarness{d: d, out: out, stderr: stderr}
	h.d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: serviceTokenHost, Token: token, Org: inspectionOrg}, nil
	}
	h.d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		request := manageRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query()}
		if r.Body != nil {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) > 0 {
				if err := json.Unmarshal(data, &request.body); err != nil {
					t.Fatal(err, string(data))
				}
			}
		}
		h.requests = append(h.requests, request)
		reply := handler(request)
		header := http.Header{"Content-Type": {"application/json"}}
		maps.Copy(header, reply.header)
		return &http.Response{StatusCode: reply.status, Header: header, Body: io.NopCloser(strings.NewReader(reply.body)), Request: r}, nil
	})}
	return h
}

func (h *manageHarness) run(t *testing.T, args ...string) int {
	t.Helper()
	return Execute(t.Context(), args, h.d)
}

func (h *manageHarness) operations() []string {
	operations := make([]string, len(h.requests))
	for i, r := range h.requests {
		operations[i] = r.method + " " + r.path
	}
	return operations
}

func manageRecord(overrides map[string]any) map[string]any {
	record := map[string]any{
		"id": manageTokenID, "organization_id": inspectionOrg, "group_id": manageGroupID, "created_by": "usr-019a0000000070008000000000000017",
		"name": "ci reader", "created_at": "2026-11-01T10:00:00Z", "expires_at": "2026-12-16T10:00:00Z", "expiring_soon": false,
		"status": "live", "rotated_at": "2026-11-01T10:00:00Z", "rotation_version": 3, "grants_version": 7,
		"previous_valid_until": nil, "last_used_at": nil, "revoked_at": nil, "revoked_reason": nil, "blocked_by": nil,
		"grants": []any{map[string]any{"resource_type": "repository", "resource_id": manageRepoA, "access_level": "read"}},
	}
	maps.Copy(record, overrides)
	return record
}

func manageJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// manageRoutes answers the reads every command needs and the writes with
// their documented success shapes.
func manageRoutes(t *testing.T, current map[string]any) func(manageRequest) manageReply {
	return func(r manageRequest) manageReply {
		switch {
		case r.method == "GET" && r.path == manageBase+"/"+manageTokenID:
			return manageReply{200, nil, manageJSON(t, current)}
		case r.method == "POST" && r.path == manageBase:
			return manageReply{201, nil, manageJSON(t, manageRecord(map[string]any{"token": manageSecret}))}
		case r.method == "POST" && r.path == manageBase+"/"+manageTokenID+"/rotate":
			return manageReply{200, nil, manageJSON(t, manageRecord(map[string]any{"token": manageSecret, "rotation_version": 4, "previous_valid_until": "2026-11-17T10:00:00Z"}))}
		case r.method == "PUT" && r.path == manageBase+"/"+manageTokenID+"/grants":
			return manageReply{200, nil, manageJSON(t, manageRecord(map[string]any{"grants_version": 8, "grants": r.body["grants"]}))}
		case r.method == "DELETE" && r.path == manageBase+"/"+manageTokenID:
			return manageReply{204, nil, ""}
		case r.method == "POST" && r.path == manageBase+"/revoke-all":
			return manageReply{200, nil, `{"revoked":["` + manageTokenID + `","key-` + `019a0000000070008000000000000018"]}`}
		case r.method == "GET" && r.path == "/orgs/"+inspectionOrg+"/repos/resolve":
			return manageReply{200, nil, `{"data":{"repository_id":"` + manageRepoB + `","full_name":"acme/web"}}`}
		case r.method == "GET" && r.path == "/users/me/organizations":
			return manageReply{200, nil, `{"data":[{"organization_id":"` + inspectionOrg + `","slug":"acme"}]}`}
		}
		t.Errorf("unexpected %s %s", r.method, r.path)
		return manageReply{599, nil, `{}`}
	}
}

func manageGrant(kind string, id any) map[string]any {
	return map[string]any{"resource_type": kind, "resource_id": id, "access_level": "read"}
}

func TestServiceTokenListFiltersAndPages(t *testing.T) {
	h := newManageHarness(t, managePersonal, func(r manageRequest) manageReply {
		count := 200
		if r.query.Get("group_id") != "eq."+manageGroupID || r.query.Get("status") != "eq.live" {
			t.Errorf("filters: %v", r.query)
		}
		if !r.query.Has("cursor") && r.query.Get("limit") != "200" {
			t.Errorf("first page: %v", r.query)
		}
		if r.query.Has("cursor") {
			count = 1
			if r.query.Get("limit") != "1" || r.query.Get("cursor") != "next +/" {
				t.Errorf("second page: %v", r.query)
			}
		}
		rows := make([]map[string]any, count)
		for i := range rows {
			rows[i] = manageRecord(map[string]any{"id": fmt.Sprintf("key-"+"019a0000000070008000%012x", i)})
		}
		return manageReply{200, nil, manageJSON(t, map[string]any{"data": rows, "pagination": map[string]any{"next_cursor": "next +/"}})}
	})
	code := h.run(t, "service-token", "list", "--group", manageGroupID, "--status", "live", "-L", "201", "--json", "id")
	var rows []map[string]any
	err := json.Unmarshal(h.out.Bytes(), &rows)
	if code != 0 || err != nil || len(rows) != 201 || !strings.Contains(h.stderr.String(), "first 201 service tokens") {
		t.Fatal(code, err, len(rows), h.stderr)
	}
	if want := []string{"GET " + manageBase, "GET " + manageBase}; !slices.Equal(h.operations(), want) {
		t.Fatal(h.operations())
	}
}

func TestServiceTokenListWithoutFiltersSendsOnlyLimit(t *testing.T) {
	h := newManageHarness(t, managePersonal, func(r manageRequest) manageReply {
		if len(r.query) != 1 || r.query.Get("limit") != "50" {
			t.Errorf("query: %v", r.query)
		}
		return manageReply{200, nil, `{"data":[]}`}
	})
	if code := h.run(t, "service-token", "list"); code != 0 || len(h.requests) != 1 {
		t.Fatal(code, h.stderr)
	}
}

func TestServiceTokenListTerminalAndPipedRows(t *testing.T) {
	rows := []map[string]any{
		manageRecord(map[string]any{"blocked_by": "group_not_active", "expiring_soon": true, "last_used_at": "2026-11-16T09:00:00Z"}),
		manageRecord(map[string]any{"id": "key-" + "019a0000000070008000000000000019", "expires_at": nil, "grants": []any{manageGrant("egress_firewall_rule", nil), manageGrant("repository", manageRepoB), manageGrant("repository", manageRepoA)}}),
		manageRecord(map[string]any{"id": "key-" + "019a000000007000800000000000001a", "status": "revoked", "grants": []any{manageGrant("egress_firewall_rule", manageRuleA), manageGrant("egress_firewall_rule", manageRuleB)}}),
	}
	page := manageJSON(t, map[string]any{"data": rows})
	t.Run("terminal", func(t *testing.T) {
		h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { return manageReply{200, nil, page} })
		h.d.IsTerminal = func() bool { return true }
		if code := h.run(t, "service-token", "list"); code != 0 {
			t.Fatal(code, h.stderr)
		}
		text := h.out.String()
		for _, want := range []string{"Token ID", "Group", "Status", "Expires", "Last used", "Grants", manageGroupID, "blocked: group not active", "(expiring soon)", "never", "revoked", "2 repos, all firewall rules", "2 firewall rules", "1 repo"} {
			if !strings.Contains(text, want) {
				t.Errorf("missing %q in\n%s", want, text)
			}
		}
	})
	t.Run("piped", func(t *testing.T) {
		h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { return manageReply{200, nil, page} })
		if code := h.run(t, "service-token", "list"); code != 0 {
			t.Fatal(code, h.stderr)
		}
		lines := strings.Split(strings.TrimSpace(h.out.String()), "\n")
		if len(lines) != 4 || lines[0] != "id\tname\tgroup_id\tstatus\tblocked_by\texpires_at\texpiring_soon\tlast_used_at\tgrants" {
			t.Fatal(h.out)
		}
		if want := manageTokenID + "\tci reader\t" + manageGroupID + "\tlive\tgroup_not_active\t2026-12-16T10:00:00Z\ttrue\t2026-11-16T09:00:00Z\t1 repo"; lines[1] != want {
			t.Fatalf("%q != %q", lines[1], want)
		}
	})
}

func TestServiceTokenListRejectsInvalidOptionsBeforeRequests(t *testing.T) {
	for _, args := range [][]string{
		{"--status", "active"}, {"--group", ""}, {"--group", " "}, {"-L", "0"}, {"-L", "1001"}, {"--jq", ".id"},
	} {
		h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { t.Error("request sent"); return manageReply{599, nil, "{}"} })
		if code := h.run(t, append([]string{"service-token", "list"}, args...)...); code != 2 || len(h.requests) != 0 {
			t.Fatal(args, code, h.stderr)
		}
	}
}

func TestServiceTokenViewTextAndJSON(t *testing.T) {
	record := manageRecord(map[string]any{"previous_valid_until": "2026-11-02T10:00:00Z", "grants": []any{manageGrant("repository", manageRepoA), manageGrant("egress_firewall_rule", nil)}})
	h := newManageHarness(t, managePersonal, manageRoutes(t, record))
	if code := h.run(t, "service-token", "view", manageTokenID); code != 0 {
		t.Fatal(code, h.stderr)
	}
	text := strings.Join(strings.Fields(h.out.String()), " ")
	if strings.Contains(text, "Rotated") {
		t.Errorf("a token's issue time is not a rotation:\n%s", text)
	}
	for _, want := range []string{"Token ID " + manageTokenID, "Group " + manageGroupID, "Status live", "Secret issued 2026-11-01T10:00:00.00Z", "Versions rotation 3, grants 7", "Old secret until 2026-11-02T10:00:00.00Z", "firewall rule | every firewall rule", "repository | " + manageRepoA} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	h = newManageHarness(t, managePersonal, manageRoutes(t, record))
	if code := h.run(t, "service-token", "view", manageTokenID, "--json", "grants_version,rotation_version", "--jq", ".grants_version"); code != 0 || strings.TrimSpace(h.out.String()) != "7" {
		t.Fatal(code, h.out, h.stderr)
	}
	if want := []string{"GET " + manageBase + "/" + manageTokenID}; !slices.Equal(h.operations(), want) {
		t.Fatal(h.operations())
	}
}

func TestServiceTokenViewRejectsMalformedID(t *testing.T) {
	h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { t.Error("request sent"); return manageReply{599, nil, "{}"} })
	if code := h.run(t, "service-token", "view", "key-ABC"); code != 2 || len(h.requests) != 0 {
		t.Fatal(code, h.stderr)
	}
}

func TestServiceTokenCreateRequestBody(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		check func(t *testing.T, body map[string]any)
	}{
		{"grants in order without repeats, default lifetime", []string{"--repo", manageRepoA, "--repo", "acme/web", "--repo", manageRepoA + ":read", "--firewall-rule", manageRuleB, "--firewall-rule", manageRuleA, "--firewall-rule", manageRuleB}, func(t *testing.T, body map[string]any) {
			want := []any{manageGrant("repository", manageRepoA), manageGrant("repository", manageRepoB), manageGrant("egress_firewall_rule", manageRuleB), manageGrant("egress_firewall_rule", manageRuleA)}
			if manageJSON(t, body["grants"]) != manageJSON(t, want) {
				t.Errorf("grants %v", body["grants"])
			}
			if _, ok := body["ttl_seconds"]; ok || body["never_expires"] != false {
				t.Errorf("lifetime %v", body)
			}
		}},
		{"every firewall rule is a null resource_id", []string{"--all-firewall-rules", "--ttl", "90d"}, func(t *testing.T, body map[string]any) {
			grants, _ := body["grants"].([]any)
			grant, _ := grants[0].(map[string]any)
			if id, present := grant["resource_id"]; len(grants) != 1 || !present || id != nil || grant["resource_type"] != "egress_firewall_rule" {
				t.Errorf("grants %v", body["grants"])
			}
			if body["ttl_seconds"] != float64(7776000) || body["never_expires"] != false {
				t.Errorf("lifetime %v", body)
			}
		}},
		{"never expires", []string{"--repo", manageRepoA, "--never-expires"}, func(t *testing.T, body map[string]any) {
			if _, ok := body["ttl_seconds"]; ok || body["never_expires"] != true {
				t.Errorf("lifetime %v", body)
			}
		}},
		{"shortest lifetime", []string{"--repo", manageRepoA, "--ttl", "60"}, func(t *testing.T, body map[string]any) {
			if body["ttl_seconds"] != float64(60) {
				t.Errorf("lifetime %v", body)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, manageRoutes(t, nil))
			args := append([]string{"service-token", "create", "--name", "ci reader", "--group", manageGroupID}, tc.args...)
			if code := h.run(t, args...); code != 0 {
				t.Fatal(code, h.stderr)
			}
			last := h.requests[len(h.requests)-1]
			if last.method != "POST" || last.path != manageBase || last.body["name"] != "ci reader" || last.body["group_id"] != manageGroupID {
				t.Fatal(last)
			}
			tc.check(t, last.body)
		})
	}
}

func TestServiceTokenCreatePrintsSecretOnce(t *testing.T) {
	h := newManageHarness(t, managePersonal, manageRoutes(t, nil))
	if code := h.run(t, "service-token", "create", "--name", "ci reader", "--group", manageGroupID, "--repo", manageRepoA); code != 0 {
		t.Fatal(code, h.stderr)
	}
	text := h.out.String()
	if strings.Count(text, manageSecret) != 1 || !strings.Contains(text, "export AVR_TOKEN="+manageSecret) || !strings.Contains(text, "shown only once") || !strings.Contains(text, manageGroupID) || strings.Contains(h.stderr.String(), manageSecret) {
		t.Fatal(text, h.stderr)
	}
	for _, fields := range []string{"token", "*", "id,token"} {
		h = newManageHarness(t, managePersonal, manageRoutes(t, nil))
		if code := h.run(t, "service-token", "create", "--name", "ci", "--group", manageGroupID, "--repo", manageRepoA, "--json", fields); code != 0 || strings.Count(h.out.String(), manageSecret) != 1 {
			t.Fatal(fields, code, h.out, h.stderr)
		}
	}
}

func TestServiceTokenCreateRefusesFilterThatDropsSecret(t *testing.T) {
	h := newManageHarness(t, managePersonal, manageRoutes(t, nil))
	if code := h.run(t, "service-token", "create", "--name", "ci", "--group", manageGroupID, "--repo", manageRepoA, "--json", "id"); code != 2 || len(h.requests) != 0 || !strings.Contains(h.stderr.String(), "must include the token field") {
		t.Fatal(code, h.requests, h.stderr)
	}
	// A jq filter can only be judged after the response: the unfiltered JSON
	// is delivered instead and the command fails.
	h = newManageHarness(t, managePersonal, manageRoutes(t, nil))
	code := h.run(t, "service-token", "create", "--name", "ci", "--group", manageGroupID, "--repo", manageRepoA, "--json", "id,token", "--jq", ".id")
	message := h.stderr.String()
	if code != 1 || strings.Count(h.out.String(), manageSecret) != 1 || !strings.Contains(message, tokenFilterRetain) || !strings.Contains(message, "avr service-token revoke "+manageTokenID) || strings.Contains(message, manageSecret) {
		t.Fatal(code, h.out, message)
	}
}

func TestServiceTokenCreateRejectsInvalidOptionsBeforeRequests(t *testing.T) {
	tooMany := []string{}
	for i := range 101 {
		tooMany = append(tooMany, "--firewall-rule", fmt.Sprintf("efr-"+"019a0000000070008000%012x", i))
	}
	base := []string{"--name", "ci", "--group", manageGroupID}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--group", manageGroupID, "--repo", manageRepoA}, "Missing option '--name'"},
		{[]string{"--name", "ci", "--repo", manageRepoA}, "Missing option '--group'"},
		{[]string{"--name", "ci", "--group", "", "--repo", manageRepoA}, "expected a group name, path or ID"},
		{base, "at least one of --repo, --firewall-rule or --all-firewall-rules"},
		{append(slices.Clone(base), "--all-firewall-rules", "--firewall-rule", manageRuleA), "already grants every firewall rule"},
		{append(slices.Clone(base), "--firewall-rule", "rule-1"), "expected a rule ID"},
		{append(slices.Clone(base), "--repo", "acme/api:admin"), "read level"},
		{append(slices.Clone(base), tooMany...), "at most 100 grants (got 101)"},
		{append(slices.Clone(base), "--repo", manageRepoA, "--ttl", "59"), "must be between 60s and 7776000s (got 59s)"},
		{append(slices.Clone(base), "--repo", manageRepoA, "--ttl", "91d"), "must be between 60s and 7776000s (got 7862400s)"},
		{append(slices.Clone(base), "--repo", manageRepoA, "--ttl", "soon"), "invalid duration"},
		{append(slices.Clone(base), "--repo", manageRepoA, "--ttl", "8h", "--never-expires"), "mutually exclusive"},
	} {
		h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { t.Error("request sent"); return manageReply{599, nil, "{}"} })
		code := h.run(t, append([]string{"service-token", "create"}, tc.args...)...)
		if code != 2 || len(h.requests) != 0 || !strings.Contains(h.stderr.String(), tc.want) {
			t.Fatal(tc.args, code, h.stderr)
		}
	}
}

func TestServiceTokenCreateRefusals(t *testing.T) {
	for _, tc := range []struct {
		name        string
		reply       manageReply
		code        int
		want, avoid []string
	}{
		{"recent sign-in", manageReply{403, http.Header{"X-Reauthentication-Required": {"recent-login"}}, `{"detail":"API key was created more than 1 hours ago"}`}, 4,
			[]string{"needs a recent sign-in to create a service token (HTTP 403): API key was created", "changing its access, need a recent sign-in", "avr auth login", "If AVR_TOKEN is set, unset it first"}, []string{"avr org list"}},
		{"plain 403 is not a sign-in refusal", manageReply{403, nil, `{"detail":"grants[1] names an object you cannot reach at that level"}`}, 1,
			[]string{"You don't have access to create a service token", "grants[N] counts from 0", "--firewall-rule values"}, []string{"recent sign-in"}},
		{"grant validation", manageReply{422, nil, `{"detail":[{"type":"target_not_found","loc":["body","grants",1],"msg":"target not found"}]}`}, 1,
			[]string{"grants[1]: target not found", "grants[N] counts from 0"}, nil},
		{"busy", manageReply{409, http.Header{"Retry-After": {"5"}}, `{"detail":"Management authorization is busy"}`}, 1,
			[]string{"Conflict while trying to create a service token (HTTP 409)", "Retry in 5s."}, []string{"changed after"}},
		{"group limit", manageReply{409, nil, `{"detail":"The group already owns the maximum number of live service tokens"}`}, 1,
			[]string{"maximum number of live service tokens"}, []string{"changed after", "Retry"}},
		{"nothing written", manageReply{503, http.Header{"Retry-After": {"60"}}, `{"detail":"Service tokens are not yet available in every service revision"}`}, 1,
			[]string{"No service token was created. Retry in 60s."}, []string{"may have been created"}},
		// Only the API's own reasons say that nothing was written.
		{"503 from elsewhere", manageReply{503, nil, `{"detail":"upstream connect error"}`}, 1,
			[]string{"may have been created"}, []string{"No service token was created"}},
		{"unknown outcome", manageReply{502, nil, `{}`}, 1,
			[]string{"may have been created", "avr service-token list --org " + inspectionOrg}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { return tc.reply })
			code := h.run(t, "service-token", "create", "--name", "ci", "--group", manageGroupID, "--repo", manageRepoA, "--firewall-rule", manageRuleA)
			message := h.stderr.String()
			if code != tc.code || len(h.requests) != 1 || h.out.Len() != 0 {
				t.Fatal(code, h.requests, message)
			}
			for _, want := range tc.want {
				if !strings.Contains(message, want) {
					t.Errorf("missing %q in %q", want, message)
				}
			}
			for _, avoid := range tc.avoid {
				if strings.Contains(message, avoid) {
					t.Errorf("unexpected %q in %q", avoid, message)
				}
			}
		})
	}
}

func TestServiceTokenSetAccessReadsVersionThenReplaces(t *testing.T) {
	h := newManageHarness(t, managePersonal, manageRoutes(t, manageRecord(nil)))
	if code := h.run(t, "service-token", "set-access", manageTokenID, "--repo", manageRepoB, "--all-firewall-rules"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if want := []string{"GET " + manageBase + "/" + manageTokenID, "PUT " + manageBase + "/" + manageTokenID + "/grants"}; !slices.Equal(h.operations(), want) {
		t.Fatal(h.operations())
	}
	body := h.requests[1].body
	want := map[string]any{"expected_grants_version": float64(7), "grants": []any{manageGrant("repository", manageRepoB), manageGrant("egress_firewall_rule", nil)}}
	if manageJSON(t, body) != manageJSON(t, want) {
		t.Fatal(body)
	}
	if text := h.out.String(); !strings.Contains(text, "Replaced the access of service token "+manageTokenID) || !strings.Contains(text, "rotation 3, grants 8") {
		t.Fatal(text)
	}
}

func TestServiceTokenSetAccessNeedsGrants(t *testing.T) {
	h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { t.Error("request sent"); return manageReply{599, nil, "{}"} })
	if code := h.run(t, "service-token", "set-access", manageTokenID); code != 2 || len(h.requests) != 0 || !strings.Contains(h.stderr.String(), "at least one of --repo") {
		t.Fatal(code, h.stderr)
	}
}

func TestServiceTokenStaleVersionConflict(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		read  map[string]any
		stale string
	}{
		// A rerun resends the whole selection and would undo the other change,
		// so the hint says to look first and to pass the full selection.
		{[]string{"set-access", manageTokenID, "--repo", manageRepoA}, manageRecord(nil), "Check \x60avr service-token view " + manageTokenID + " --org " + inspectionOrg + "\x60, then run set-access again with the full selection you want."},
		// A retry of rotate may end an overlap, so its hint says to look first.
		{[]string{"rotate", manageTokenID}, manageRecord(nil), "Check \x60avr service-token view " + manageTokenID + " --org " + inspectionOrg + "\x60 before retrying: if it was rotated, rotating again retires the previous secret at once."},
		// The read already showed why the write is refused; the API's reason says so.
		{[]string{"rotate", manageTokenID}, manageRecord(map[string]any{"status": "revoked"}), ""},
		{[]string{"set-access", manageTokenID, "--repo", manageRepoA}, manageRecord(map[string]any{"blocked_by": "group_not_active"}), ""},
	} {
		routes := manageRoutes(t, tc.read)
		h := newManageHarness(t, managePersonal, func(r manageRequest) manageReply {
			if r.method == "GET" {
				return routes(r)
			}
			return manageReply{409, nil, `{"detail":"refused"}`}
		})
		code := h.run(t, append([]string{"service-token"}, tc.args...)...)
		message := h.stderr.String()
		if code != 1 || len(h.requests) != 2 || !strings.Contains(message, "(HTTP 409): refused") || strings.Contains(message, "changed after this command read it") != (tc.stale != "") || !strings.Contains(message, tc.stale) || strings.Contains(message, "Run the command again") || strings.Contains(message, "Retry in") {
			t.Fatal(tc.args, tc.read["status"], code, message)
		}
	}
}

func TestServiceTokenRotateRequestAndOutput(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want map[string]any
	}{
		{nil, map[string]any{"expected_rotation_version": float64(3), "never_expires": false}},
		{[]string{"--ttl", "30d", "--overlap", "2h"}, map[string]any{"expected_rotation_version": float64(3), "never_expires": false, "ttl_seconds": float64(2592000), "overlap_seconds": float64(7200)}},
		{[]string{"--never-expires", "--overlap", "1h"}, map[string]any{"expected_rotation_version": float64(3), "never_expires": true, "overlap_seconds": float64(3600)}},
		{[]string{"--overlap", "7d"}, map[string]any{"expected_rotation_version": float64(3), "never_expires": false, "overlap_seconds": float64(604800)}},
	} {
		h := newManageHarness(t, managePersonal, manageRoutes(t, manageRecord(nil)))
		if code := h.run(t, append([]string{"service-token", "rotate", manageTokenID}, tc.args...)...); code != 0 {
			t.Fatal(tc.args, code, h.stderr)
		}
		if want := []string{"GET " + manageBase + "/" + manageTokenID, "POST " + manageBase + "/" + manageTokenID + "/rotate"}; !slices.Equal(h.operations(), want) {
			t.Fatal(h.operations())
		}
		if got := h.requests[1].body; manageJSON(t, got) != manageJSON(t, tc.want) {
			t.Fatal(tc.args, got)
		}
		text := h.out.String()
		if strings.Count(text, manageSecret) != 1 || !strings.Contains(text, "export AVR_TOKEN="+manageSecret) || !strings.Contains(text, "The previous secret keeps working until 2026-11-17T10:00:00.00Z") || !strings.Contains(text, "rotation 4") {
			t.Fatal(text)
		}
	}
	h := newManageHarness(t, managePersonal, manageRoutes(t, manageRecord(nil)))
	if code := h.run(t, "service-token", "rotate", manageTokenID, "--json", "previous_valid_until,token", "--jq", ".token"); code != 0 || strings.TrimSpace(h.out.String()) != manageSecret {
		t.Fatal(code, h.out, h.stderr)
	}
}

func TestServiceTokenRotateRejectsInvalidOptionsBeforeRequests(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--overlap", "59m"}, "must be between 3600s and 604800s (got 3540s)"},
		{[]string{"--overlap", "8d"}, "must be between 3600s and 604800s (got 691200s)"},
		{[]string{"--ttl", "91d"}, "must be between 60s and 7776000s"},
		{[]string{"--ttl", "1d", "--never-expires"}, "mutually exclusive"},
		{[]string{"--json", "id"}, "must include the token field"},
	} {
		h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { t.Error("request sent"); return manageReply{599, nil, "{}"} })
		code := h.run(t, append([]string{"service-token", "rotate", manageTokenID}, tc.args...)...)
		if code != 2 || len(h.requests) != 0 || !strings.Contains(h.stderr.String(), tc.want) {
			t.Fatal(tc.args, code, h.stderr)
		}
	}
}

func TestServiceTokenRotateRecentSignIn(t *testing.T) {
	routes := manageRoutes(t, manageRecord(nil))
	h := newManageHarness(t, managePersonal, func(r manageRequest) manageReply {
		if r.method == "GET" {
			return routes(r)
		}
		return manageReply{403, http.Header{"X-Reauthentication-Required": {"recent-login"}}, `{"detail":"Sign in again to continue"}`}
	})
	code := h.run(t, "service-token", "rotate", manageTokenID)
	message := h.stderr.String()
	if code != 4 || len(h.requests) != 2 || !strings.Contains(message, "recent sign-in to rotate the service token") || !strings.Contains(message, "avr auth login") || h.out.Len() != 0 {
		t.Fatal(code, message)
	}
}

func TestServiceTokenRevokeConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		args        []string
		env         map[string]string
		revoked     bool
	}{
		{"declined", "n\n", nil, nil, false},
		{"accepted", "y\n", nil, nil, true},
		{"--yes", "", []string{"--yes"}, nil, true},
		{"no input", "", nil, nil, false},
		{"prompts disabled", "y\n", nil, map[string]string{"AVR_PROMPT_DISABLED": "1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, manageRoutes(t, nil))
			h.d.In = strings.NewReader(tc.input)
			h.d.Getenv = func(key string) string { return tc.env[key] }
			code := h.run(t, append([]string{"service-token", "revoke", manageTokenID}, tc.args...)...)
			if tc.revoked {
				if code != 0 || !slices.Equal(h.operations(), []string{"DELETE " + manageBase + "/" + manageTokenID}) || !strings.Contains(h.out.String(), "Service token "+manageTokenID+" revoked.") {
					t.Fatal(code, h.operations(), h.out, h.stderr)
				}
				return
			}
			if code != 1 || len(h.requests) != 0 || !strings.Contains(h.stderr.String(), "Abort") {
				t.Fatal(code, h.operations(), h.stderr)
			}
		})
	}
}

func TestServiceTokenRevokeAllConfirmationNamesOrganization(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		args        []string
		revoked     bool
	}{
		{"declined", "no\n", nil, false},
		{"accepted", "yes\n", nil, true},
		{"--yes", "", []string{"--yes"}, true},
		{"no input", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, manageRoutes(t, nil))
			h.d.In = strings.NewReader(tc.input)
			code := h.run(t, append([]string{"service-token", "revoke-all"}, tc.args...)...)
			prompted := strings.Contains(h.out.String(), "Revoke every service token of organization acme ("+inspectionOrg+")?")
			if prompted == slices.Contains(tc.args, "--yes") {
				t.Fatal("prompt", h.out)
			}
			writes := slices.DeleteFunc(h.operations(), func(op string) bool { return strings.HasPrefix(op, "GET ") })
			if !tc.revoked {
				if code != 1 || len(writes) != 0 {
					t.Fatal(code, h.operations(), h.stderr)
				}
				return
			}
			label := "acme (" + inspectionOrg + ")"
			if slices.Contains(tc.args, "--yes") {
				// Nothing looked the slug up, so the ID names the organization.
				label = inspectionOrg
				if len(h.requests) != 1 {
					t.Fatal(h.operations())
				}
			}
			if code != 0 || !slices.Equal(writes, []string{"POST " + manageBase + "/revoke-all"}) || !strings.Contains(h.out.String(), "Revoked 2 service tokens of organization "+label+".") || !strings.Contains(h.out.String(), "  "+manageTokenID+"\n") {
				t.Fatal(code, h.operations(), h.out, h.stderr)
			}
		})
	}
}

func TestServiceTokenManagementRefusesOrganizationBoundTokens(t *testing.T) {
	commands := [][]string{
		{"list"}, {"view", manageTokenID}, {"create", "--name", "ci", "--group", manageGroupID, "--repo", manageRepoA},
		{"set-access", manageTokenID, "--repo", manageRepoA}, {"rotate", manageTokenID},
		{"revoke", manageTokenID, "--yes"}, {"revoke-all", "--yes"},
	}
	for _, token := range []string{"avs_fixture_scoped", serviceTokenFixture} {
		kind := config.Kind(token).String()
		for _, args := range commands {
			h := newManageHarness(t, token, func(r manageRequest) manageReply {
				t.Errorf("%v sent %s %s", args, r.method, r.path)
				return manageReply{599, nil, "{}"}
			})
			code := h.run(t, append([]string{"service-token"}, args...)...)
			message := h.stderr.String()
			if code != 4 || len(h.requests) != 0 || h.out.Len() != 0 || !strings.Contains(message, "Managing service tokens needs a signed-in account; "+kind+"s cannot manage service tokens") || !strings.Contains(message, "avr auth login") {
				t.Fatal(token, args, code, message)
			}
		}
	}
}

type cancelOnWrite struct {
	io.Writer
	cancel context.CancelFunc
}

func (w cancelOnWrite) Write(p []byte) (int, error) {
	defer w.cancel()
	return w.Writer.Write(p)
}

// A cancellation that lands after the secret reached stdout must not print it
// again: a consumer reads one JSON document and one secret, and stderr says
// that the change happened.
func TestServiceTokenSecretIsNotRepeatedAfterLateCancellation(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		outcome string
	}{
		{[]string{"service-token", "create", "--name", "ci", "--group", manageGroupID, "--repo", manageRepoA, "--json", "token"},
			"The service token was created and its output on stdout is complete; if that output was saved, do not run the command again. To discard it, run \x60avr service-token revoke " + manageTokenID + " --org " + inspectionOrg + "\x60."},
		{[]string{"service-token", "rotate", manageTokenID, "--json", "token"},
			"The service token was rotated and its output on stdout is complete; if that output was saved, do not run the command again. If the new secret was not saved, check \x60avr service-token view " + manageTokenID + " --org " + inspectionOrg + "\x60 and rotate again. Rotating again retires the previous secret at once."},
	} {
		h := newManageHarness(t, managePersonal, manageRoutes(t, manageRecord(nil)))
		ctx, cancel := context.WithCancel(t.Context())
		h.d.Out = cancelOnWrite{h.out, cancel}
		code := Execute(ctx, tc.args, h.d)
		var document map[string]any
		if err := json.Unmarshal(h.out.Bytes(), &document); err != nil || document["token"] != manageSecret {
			t.Fatal(tc.args, err, h.out)
		}
		if message := h.stderr.String(); code != 130 || !strings.Contains(message, "Interrupted. "+tc.outcome) || strings.Contains(message, manageSecret) {
			t.Fatal(tc.args, code, message)
		}
	}
}

func TestServiceTokenNotFoundHintsNameTheOrganization(t *testing.T) {
	for _, args := range [][]string{
		{"view", manageTokenID},
		{"set-access", manageTokenID, "--repo", manageRepoA},
		{"rotate", manageTokenID},
		{"revoke", manageTokenID, "--yes"},
	} {
		h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { return manageReply{404, nil, `{"detail":"Not found"}`} })
		code := h.run(t, append([]string{"service-token"}, args...)...)
		if message := h.stderr.String(); code != 1 || !strings.Contains(message, "avr service-token list --org "+inspectionOrg+"\x60") {
			t.Fatal(args, code, message)
		}
	}
}

func TestServiceTokenRotateSaysWhatElseChanged(t *testing.T) {
	never := manageRecord(map[string]any{"expires_at": nil})
	overlapping := manageRecord(map[string]any{"previous_valid_until": "2026-11-16T12:00:00Z"})
	ended := manageRecord(map[string]any{"previous_valid_until": "2026-11-15T12:00:00Z"})
	const expiry, overlap = "did not expire before; its new secret expires at 2026-12-16T10:00:00.00Z", "earlier rotation was still in its overlap"
	// Following the advice at once would end the overlap the rotation opened.
	const cost = "switch consumers to the new secret first, then rotate with --never-expires: rotating before the overlap ends retires the previous secret at once"
	for _, tc := range []struct {
		name        string
		current     map[string]any
		args        []string
		want, avoid []string
	}{
		{"never-expiring token gets the default lifetime", never, nil, []string{expiry, cost}, []string{overlap}},
		{"lifetime was chosen", never, []string{"--ttl", "30d"}, nil, []string{"did not expire before"}},
		{"stays never-expiring", never, []string{"--never-expires"}, nil, []string{"did not expire before"}},
		{"earlier overlap still open", overlapping, nil, []string{overlap}, []string{"did not expire before"}},
		{"earlier overlap ended", ended, nil, nil, []string{overlap}},
		{"nothing else changed", manageRecord(nil), nil, nil, []string{"did not expire before", overlap}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, manageRoutes(t, tc.current))
			h.d.Now = func() time.Time { return time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC) }
			// With --json the notices must not join the record on stdout.
			if code := h.run(t, append([]string{"service-token", "rotate", manageTokenID, "--json", "token"}, tc.args...)...); code != 0 {
				t.Fatal(code, h.stderr)
			}
			message := h.stderr.String()
			// stdout holds exactly the selected record and no notice.
			decoder := json.NewDecoder(bytes.NewReader(h.out.Bytes()))
			var document map[string]any
			if err := decoder.Decode(&document); err != nil || manageJSON(t, document) != manageJSON(t, map[string]any{"token": manageSecret}) {
				t.Fatalf("stdout is not the record: %v %q", err, h.out)
			}
			if _, err := decoder.Token(); err != io.EOF {
				t.Fatalf("stdout has more than one JSON value: %q", h.out)
			}
			for _, want := range tc.want {
				if !strings.Contains(message, want) {
					t.Errorf("missing %q in %q", want, message)
				}
				if strings.Contains(h.out.String(), want) {
					t.Errorf("notice %q on stdout: %q", want, h.out)
				}
			}
			for _, avoid := range tc.avoid {
				if strings.Contains(message+h.out.String(), avoid) {
					t.Errorf("unexpected %q in %q", avoid, message+h.out.String())
				}
			}
		})
	}
}

// The notices are advice: when stderr fails, the rotation has still committed
// and the new secret must reach stdout.
func TestServiceTokenRotateNoticesDoNotBlockTheSecret(t *testing.T) {
	never := manageRecord(map[string]any{"expires_at": nil})
	overlapping := manageRecord(map[string]any{"previous_valid_until": "2026-11-16T12:00:00Z"})
	for _, current := range []map[string]any{never, overlapping} {
		for _, format := range [][]string{nil, {"--json", "token"}} {
			h := newManageHarness(t, managePersonal, manageRoutes(t, current))
			h.d.Now = func() time.Time { return time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC) }
			h.d.Err = tokenBrokenWriter{}
			code := h.run(t, append([]string{"service-token", "rotate", manageTokenID}, format...)...)
			if code != 0 || strings.Count(h.out.String(), manageSecret) != 1 || !slices.Contains(h.operations(), "POST "+manageBase+"/"+manageTokenID+"/rotate") {
				t.Fatal(current["expires_at"], format, code, h.out)
			}
		}
	}
}

// The notices go to stderr, so stderr decides whether they are colored.
func TestServiceTokenRotateNoticesColorFollowsStderr(t *testing.T) {
	never := manageRecord(map[string]any{"expires_at": nil, "previous_valid_until": "2026-11-16T12:00:00Z"})
	for _, stderrTerminal := range []bool{false, true} {
		h := newManageHarness(t, managePersonal, manageRoutes(t, never))
		h.d.Now = func() time.Time { return time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC) }
		h.d.IsTerminal = func() bool { return !stderrTerminal }
		h.d.IsErrorTerminal = func() bool { return stderrTerminal }
		if code := h.run(t, "service-token", "rotate", manageTokenID); code != 0 {
			t.Fatal(code, h.stderr)
		}
		message := h.stderr.String()
		if strings.Count(message, "\x1b[33m! ") != map[bool]int{false: 0, true: 2}[stderrTerminal] || !strings.Contains(message, "did not expire before") || !strings.Contains(message, "earlier rotation") {
			t.Fatalf("stderr terminal %v: %q", stderrTerminal, message)
		}
	}
}

func TestServiceTokenGrantLimitCountsDistinctGrants(t *testing.T) {
	args := []string{"service-token", "create", "--name", "ci", "--group", manageGroupID}
	for range 60 {
		args = append(args, "--repo", "acme/web", "--firewall-rule", manageRuleA)
	}
	h := newManageHarness(t, managePersonal, manageRoutes(t, nil))
	if code := h.run(t, args...); code != 0 {
		t.Fatal(code, h.stderr)
	}
	// 120 flags name two grants, and the repository resolves once.
	resolves := 0
	for _, operation := range h.operations() {
		if strings.HasSuffix(operation, "/repos/resolve") {
			resolves++
		}
	}
	grants, _ := h.requests[len(h.requests)-1].body["grants"].([]any)
	if resolves != 1 || len(grants) != 2 {
		t.Fatal(resolves, grants)
	}
}

func TestServiceTokenGrantOrderHintOnlyWhereGrantsAreSent(t *testing.T) {
	refused := func(manageRequest) manageReply {
		return manageReply{403, nil, `{"detail":"grants[1] names an object you cannot reach at that level"}`}
	}
	h := newManageHarness(t, managePersonal, refused)
	if code := h.run(t, "service-token", "view", manageTokenID); code != 1 || strings.Contains(h.stderr.String(), "grants[N] counts from 0") {
		t.Fatal(code, h.stderr)
	}
	// A refusal that only mentions grants elsewhere names no grant.
	h = newManageHarness(t, managePersonal, func(manageRequest) manageReply {
		return manageReply{403, nil, `{"detail":"You cannot change the grants of this token"}`}
	})
	if code := h.run(t, "service-token", "create", "--name", "ci", "--group", manageGroupID, "--repo", manageRepoA); code != 1 || strings.Contains(h.stderr.String(), "grants[N] counts from 0") {
		t.Fatal(code, h.stderr)
	}
}

func TestServiceTokenListStopsAtACursorThatRepeats(t *testing.T) {
	h := newManageHarness(t, managePersonal, func(manageRequest) manageReply {
		return manageReply{200, nil, manageJSON(t, map[string]any{"data": []any{manageRecord(nil)}, "pagination": map[string]any{"next_cursor": "same"}})}
	})
	code := h.run(t, "service-token", "list", "--json", "id")
	if message := h.stderr.String(); code != 1 || len(h.requests) != 2 || h.out.Len() != 0 || !strings.Contains(message, "cursor that does not advance") {
		t.Fatal(code, len(h.requests), message)
	}
}

func TestServiceTokenListAtMaximumLimitAdvisesFilters(t *testing.T) {
	h := newManageHarness(t, managePersonal, func(r manageRequest) manageReply {
		rows := make([]map[string]any, 200)
		for i := range rows {
			rows[i] = manageRecord(nil)
		}
		return manageReply{200, nil, manageJSON(t, map[string]any{"data": rows, "pagination": map[string]any{"next_cursor": "after-" + r.query.Get("cursor")}})}
	})
	// Only a filter not yet applied can narrow the list.
	for _, tc := range []struct {
		filters []string
		advice  string
	}{
		{nil, "Narrow the list with --group or --status to see the rest."},
		{[]string{"--group", manageGroupID}, "Narrow the list with --status to see the rest."},
		{[]string{"--status", "revoked"}, "Narrow the list with --group to see the rest."},
		{[]string{"--group", manageGroupID, "--status", "revoked"}, "The CLI shows at most 1000 service tokens for these filters."},
	} {
		h.out.Reset()
		h.stderr.Reset()
		code := h.run(t, append([]string{"service-token", "list", "-L", "1000", "--json", "id"}, tc.filters...)...)
		if message := h.stderr.String(); code != 0 || !strings.Contains(message, "Showing the first 1000 service tokens. "+tc.advice+"\n") || strings.Contains(message, "Raise --limit") {
			t.Fatal(tc.filters, code, message)
		}
	}
}

// A rotation inside an earlier overlap retires the secret that rotation
// replaced, so an interactive run asks first; scripts are not asked.
func TestServiceTokenRotateConfirmsEndingAnOpenOverlap(t *testing.T) {
	overlapping := manageRecord(map[string]any{"previous_valid_until": "2026-11-16T12:00:00Z"})
	ended := manageRecord(map[string]any{"previous_valid_until": "2026-11-15T12:00:00Z"})
	const question = "still in its overlap until 2026-11-16T12:00:00.00Z. Rotating now retires the secret it replaced at once. Rotate? [y/N]"
	for _, tc := range []struct {
		name, input       string
		current           map[string]any
		terminal          bool
		args              []string
		env               map[string]string
		prompted, rotated bool
		stderrLogged      bool
	}{
		{"declined", "n\n", overlapping, true, nil, nil, true, false, false},
		{"no answer", "", overlapping, true, nil, nil, true, false, false},
		{"accepted", "y\n", overlapping, true, nil, nil, true, true, false},
		{"--yes", "", overlapping, true, []string{"--yes"}, nil, false, true, false},
		{"-y with --json", "", overlapping, true, []string{"-y", "--json", "token"}, nil, false, true, false},
		{"not a terminal", "", overlapping, false, nil, nil, false, true, false},
		// A question in a redirected log would block the run unseen.
		{"stderr redirected", "", overlapping, true, []string{"--json", "token"}, nil, false, true, true},
		{"overlap ended", "", ended, true, nil, nil, false, true, false},
		{"no earlier overlap", "", manageRecord(nil), true, nil, nil, false, true, false},
		{"prompts disabled", "y\n", overlapping, true, nil, map[string]string{"AVR_PROMPT_DISABLED": "1"}, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, manageRoutes(t, tc.current))
			h.d.Now = func() time.Time { return time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC) }
			h.d.In = strings.NewReader(tc.input)
			h.d.IsInputTerminal = func() bool { return tc.terminal }
			h.d.IsErrorTerminal = func() bool { return !tc.stderrLogged }
			h.d.Getenv = func(key string) string { return tc.env[key] }
			code := h.run(t, append([]string{"service-token", "rotate", manageTokenID}, tc.args...)...)
			message := h.stderr.String()
			// The question goes to stderr so stdout stays the secret.
			if strings.Contains(message, question) != tc.prompted || strings.Contains(h.out.String(), "Rotate?") {
				t.Fatalf("prompted: stdout %q stderr %q", h.out, message)
			}
			rotated := slices.Contains(h.operations(), "POST "+manageBase+"/"+manageTokenID+"/rotate")
			if rotated != tc.rotated {
				t.Fatal(h.operations(), message)
			}
			if tc.rotated {
				if code != 0 || strings.Count(h.out.String(), manageSecret) != 1 {
					t.Fatal(code, h.out, message)
				}
				if tc.current["previous_valid_until"] == overlapping["previous_valid_until"] && !strings.Contains(message, "earlier rotation was still in its overlap") {
					t.Fatal("missing overlap notice:", message)
				}
				return
			}
			if code != 1 || h.out.Len() != 0 || !strings.Contains(message, "Abort") {
				t.Fatal(code, h.out, message)
			}
		})
	}
}

// A revoked token keeps previous_valid_until, but there is no overlap to
// end: the API refuses the rotation, and that refusal is what the user sees.
func TestServiceTokenRotateDoesNotAskForATokenThatIsNotLive(t *testing.T) {
	revoked := manageRecord(map[string]any{"status": "revoked", "revoked_at": "2026-11-16T09:00:00Z", "previous_valid_until": "2026-11-16T12:00:00Z"})
	routes := manageRoutes(t, revoked)
	h := newManageHarness(t, managePersonal, func(r manageRequest) manageReply {
		if r.method == "GET" {
			return routes(r)
		}
		return manageReply{409, nil, `{"detail":"The token is revoked"}`}
	})
	h.d.Now = func() time.Time { return time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC) }
	h.d.In = strings.NewReader("")
	h.d.IsInputTerminal = func() bool { return true }
	h.d.IsErrorTerminal = func() bool { return true }
	code := h.run(t, "service-token", "rotate", manageTokenID)
	message := h.stderr.String()
	if code != 1 || strings.Contains(message, "Rotate?") || strings.Contains(message, "Abort") || !slices.Contains(h.operations(), "POST "+manageBase+"/"+manageTokenID+"/rotate") || !strings.Contains(message, "(HTTP 409): The token is revoked") {
		t.Fatal(code, h.operations(), message)
	}
}

// The API refuses with this 503 before it writes, so a retry is safe.
func TestServiceTokenDelegationNotReadyWroteNothing(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		want, kept string
	}{
		{[]string{"create", "--name", "ci", "--group", manageGroupID, "--repo", manageRepoA}, "No service token was created. Retry in 30s.", "may have been created"},
		{[]string{"set-access", manageTokenID, "--repo", manageRepoA}, "The token's access was not changed. Retry in 30s.", "may have been replaced"},
		{[]string{"rotate", manageTokenID}, "The token was not rotated. Retry in 30s.", "may have been rotated"},
	} {
		routes := manageRoutes(t, manageRecord(nil))
		h := newManageHarness(t, managePersonal, func(r manageRequest) manageReply {
			if r.method == "GET" {
				return routes(r)
			}
			return manageReply{503, http.Header{"Retry-After": {"30"}}, `{"detail":"Delegation authorization is not ready"}`}
		})
		code := h.run(t, append([]string{"service-token"}, tc.args...)...)
		if message := h.stderr.String(); code != 1 || !strings.Contains(message, tc.want) || strings.Contains(message, tc.kept) {
			t.Fatal(tc.args, code, message)
		}
	}
}

// While service tokens are switched off, the read succeeds and shows the
// block but rotating and widening access answer 404: the token exists, so
// listing tokens is the wrong advice.
func TestServiceTokenWriteNotFoundFollowsTheRead(t *testing.T) {
	disabled := manageRecord(map[string]any{"blocked_by": "feature_disabled"})
	for _, tc := range []struct {
		args    []string
		current map[string]any
		want    []string
		avoid   string
	}{
		{[]string{"rotate", manageTokenID}, disabled, []string{"Service tokens are not available for this change", "narrowing a token's access and revoking it still work", "support@avrea.com"}, "service-token list"},
		{[]string{"set-access", manageTokenID, "--repo", manageRepoA}, disabled, []string{"Service tokens are not available for this change", "support@avrea.com"}, "service-token list"},
		{[]string{"rotate", manageTokenID}, manageRecord(nil), []string{"avr service-token list --org " + inspectionOrg}, "not available for this change"},
		{[]string{"set-access", manageTokenID, "--repo", manageRepoA}, manageRecord(nil), []string{"avr service-token list --org " + inspectionOrg}, "not available for this change"},
	} {
		routes := manageRoutes(t, tc.current)
		h := newManageHarness(t, managePersonal, func(r manageRequest) manageReply {
			if r.method == "GET" {
				return routes(r)
			}
			return manageReply{404, nil, `{"detail":"Not found"}`}
		})
		code := h.run(t, append([]string{"service-token"}, tc.args...)...)
		message := h.stderr.String()
		if code != 1 || len(h.requests) != 2 || strings.Contains(message, tc.avoid) {
			t.Fatal(tc.args, tc.current["blocked_by"], code, message)
		}
		for _, want := range tc.want {
			if !strings.Contains(message, want) {
				t.Errorf("%v: missing %q in %q", tc.args, want, message)
			}
		}
	}
}

// After an unknown outcome the rotation version tells whether the attempt
// rotated, so the message names the version it started from.
func TestServiceTokenRotateUnknownOutcomeNamesStartingVersion(t *testing.T) {
	routes := manageRoutes(t, manageRecord(nil))
	h := newManageHarness(t, managePersonal, func(r manageRequest) manageReply {
		if r.method == "GET" {
			return routes(r)
		}
		return manageReply{502, nil, `{}`}
	})
	code := h.run(t, "service-token", "rotate", manageTokenID)
	if message := h.stderr.String(); code != 1 || !strings.Contains(message, "may have been rotated; it was at rotation version 3 before this attempt.") || !strings.Contains(message, "a higher version means a rotation happened") {
		t.Fatal(code, message)
	}
}

// A PUT whose response is lost or unreadable may have committed, so
// set-access says the access may have been replaced, as create and rotate do.
func TestServiceTokenSetAccessLostResponseMayHaveCommitted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		lost   func(cancel context.CancelFunc) (*http.Response, error)
		code   int
		reason string
	}{
		{"connection reset", func(context.CancelFunc) (*http.Response, error) {
			return nil, errors.New("connection reset by peer")
		}, 1, "could not reach " + serviceTokenHost},
		{"cancelled", func(cancel context.CancelFunc) (*http.Response, error) {
			cancel()
			return nil, context.Canceled
		}, 130, "context canceled"},
		// A malformed 2xx body was answered, so "not received" would be wrong.
		{"malformed 2xx body", func(context.CancelFunc) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("not json"))}, nil
		}, 1, "unexpected API response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, manageRoutes(t, manageRecord(nil)))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			inner := h.d.HTTP.Transport
			h.d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPut {
					return inner.RoundTrip(r)
				}
				// The change commits; then the answer is lost or unreadable.
				if _, err := inner.RoundTrip(r); err != nil {
					t.Fatal(err)
				}
				response, err := tc.lost(cancel)
				if response != nil {
					response.Request = r
				}
				return response, err
			})}
			code := Execute(ctx, []string{"service-token", "set-access", manageTokenID, "--repo", manageRepoA}, h.d)
			message := h.stderr.String()
			if code != tc.code || !strings.Contains(message, "Could not confirm the access change: "+tc.reason) || !strings.Contains(message, ". The access may have been replaced; check \x60avr service-token view "+manageTokenID+" --org "+inspectionOrg+"\x60 before retrying.") || strings.Contains(message, "not received") || h.out.Len() != 0 {
				t.Fatal(code, message)
			}
		})
	}
}

// A service token refuses an AVR_ORG slug, so the printed setup exports the
// resolved organization ID next to the secret, even when --org was a slug.
func TestServiceTokenSecretSetupExportsOrganizationID(t *testing.T) {
	for _, args := range [][]string{
		{"create", "--name", "ci", "--group", manageGroupID, "--repo", manageRepoA, "--org", "acme"},
		{"rotate", manageTokenID, "--org", "acme"},
	} {
		h := newManageHarness(t, managePersonal, manageRoutes(t, manageRecord(nil)))
		if code := h.run(t, append([]string{"service-token"}, args...)...); code != 0 {
			t.Fatal(args, code, h.stderr)
		}
		if text := h.out.String(); !strings.Contains(text, "  export AVR_TOKEN="+manageSecret+"\n  export AVR_ORG="+inspectionOrg+"\n") {
			t.Fatal(args, text)
		}
	}
}
