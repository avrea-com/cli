package commands

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

const (
	memberAda     = "usr-" + "019a0000000070008000000000000031"
	memberBob     = "usr-" + "019a0000000070008000000000000032"
	memberMe      = "usr-" + "019a0000000070008000000000000033"
	platformRoute = groupsPath + "/" + groupPlatform
	// groupDeleted is absent from the listing, as a deleted group is.
	groupDeleted  = "grp-" + "019a00000000700080000000000000dd"
	membersBulk   = platformRoute + "/members/bulk"
	memberLookups = "/orgs/" + inspectionOrg + "/member-lookups"
	orgMembers    = "/orgs/" + inspectionOrg + "/members"
)

// governanceAPI answers the group listing, /users/me and email lookups as the
// API does, and passes every other request to handle first.
func governanceAPI(t *testing.T, handle func(manageRequest) (manageReply, bool)) func(manageRequest) manageReply {
	t.Helper()
	pages := groupPages(t, groupTree())
	return func(r manageRequest) manageReply {
		if handle != nil {
			if reply, ok := handle(r); ok {
				return reply
			}
		}
		switch {
		case r.method == "GET" && r.path == groupsPath:
			return pages(r)
		case r.method == "GET" && r.path == "/users/me":
			return manageReply{200, nil, manageJSON(t, map[string]any{"id": memberMe, "email": "me@example.test"})}
		case r.method == "POST" && r.path == memberLookups:
			rows := []map[string]any{}
			if r.body["email"] == "ada@example.test" {
				rows = append(rows, map[string]any{"user_id": memberAda, "name": "Ada", "role": "member", "state": "active", "avatar_url": nil, "joined_at": "2026-11-01T10:00:00Z"})
			}
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": rows, "pagination": map[string]any{"next_cursor": nil}})}
		}
		t.Errorf("unexpected request %s %s", r.method, r.path)
		return manageReply{599, nil, "{}"}
	}
}

// bulkReply answers a bulk member change with a result per member: ok unless
// statuses names another status, "omit" to leave the member out, "other
// action" to answer for the opposite action, or "twice" to answer twice.
func bulkReply(t *testing.T, r manageRequest, statuses map[string]string) manageReply {
	t.Helper()
	results := []map[string]any{}
	for action, ids := range r.body {
		for _, id := range ids.([]any) {
			status := "ok"
			if s, ok := statuses[id.(string)]; ok {
				status = s
			}
			switch status {
			case "omit":
				continue
			case "other action":
				results = append(results, map[string]any{"user_id": id, "action": "remove", "status": "ok", "detail": nil})
				continue
			case "twice":
				results = append(results, map[string]any{"user_id": id, "action": action, "status": "ok", "detail": nil}, map[string]any{"user_id": id, "action": action, "status": "noop", "detail": nil})
				continue
			}
			var detail any
			if status == "error" {
				detail = "member is not active"
			}
			results = append(results, map[string]any{"user_id": id, "action": action, "status": status, "detail": detail})
		}
	}
	return manageReply{200, nil, manageJSON(t, map[string]any{"results": results})}
}

func requestsTo(h *manageHarness, method, path string) []manageRequest {
	return slices.DeleteFunc(slices.Clone(h.requests), func(r manageRequest) bool { return r.method != method || r.path != path })
}

func writes(h *manageHarness) []string {
	return slices.DeleteFunc(h.operations(), func(op string) bool {
		return strings.HasPrefix(op, "GET ") || op == "POST "+memberLookups
	})
}

func TestGroupCreatePrintsTheKeyBeforeSending(t *testing.T) {
	var h *manageHarness
	var before string
	h = newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		if r.method != "POST" || r.path != groupsPath {
			return manageReply{}, false
		}
		before = h.stderr.String()
		record := groupRecord("grp-"+"019a0000000070008000000000000027", groupPlatform, "Builds")
		record["label"] = "runners"
		return manageReply{201, nil, manageJSON(t, map[string]any{"data": record})}, true
	}))
	if code := h.run(t, "group", "create", "Builds", "--parent", "Acme/Platform", "--label", "runners", "--json", "group_id,idempotency_key"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	posts := requestsTo(h, "POST", groupsPath)
	if len(posts) != 1 {
		t.Fatal(h.operations())
	}
	key, _ := posts[0].body["idempotency_key"].(string)
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(key) {
		t.Fatalf("key %q", key)
	}
	want := map[string]any{"parent_id": groupPlatform, "display_name": "Builds", "label": "runners", "idempotency_key": key}
	if fmt.Sprint(posts[0].body) != fmt.Sprint(want) {
		t.Fatal(posts[0].body)
	}
	// A lost response still leaves the key that resumes this create.
	if !strings.Contains(before, "Idempotency key: "+key) {
		t.Fatalf("stderr before the request: %q", before)
	}
	var record map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &record); err != nil || record["idempotency_key"] != key {
		t.Fatal(err, h.out)
	}
}

func TestGroupCreateResumesWithTheGivenKey(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	h := newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		if r.method == "POST" && r.path == groupsPath {
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": groupRecord(groupPlatCI, groupPlatform, "CI")})}, true
		}
		return manageReply{}, false
	}))
	if code := h.run(t, "group", "create", "CI", "--parent", groupPlatform, "--idempotency-key", key); code != 0 {
		t.Fatal(code, h.stderr)
	}
	posts := requestsTo(h, "POST", groupsPath)
	if len(posts) != 1 || posts[0].body["idempotency_key"] != key || posts[0].body["parent_id"] != groupPlatform {
		t.Fatal(posts)
	}
	// A parent ID needs no listing, and no label is sent unless given.
	if groupLookups(h) != 0 || posts[0].body["label"] != nil {
		t.Fatal(h.operations(), posts[0].body)
	}
}

// An embedder can run one command tree many times: each create without
// --idempotency-key gets a key of its own.
func TestGroupCreateGeneratesAKeyPerRun(t *testing.T) {
	h := newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		if r.method == "POST" && r.path == groupsPath {
			return manageReply{201, nil, manageJSON(t, map[string]any{"data": groupRecord("grp-"+"019a0000000070008000000000000027", groupPlatform, fmt.Sprint(r.body["display_name"]))})}, true
		}
		return manageReply{}, false
	}))
	root := NewRoot(h.d)
	for _, name := range []string{"Builds", "Tests"} {
		if code := ExecuteRoot(t.Context(), root, []string{"group", "create", name, "--parent", groupPlatform}, h.d.Err, h.d); code != 0 {
			t.Fatal(name, code, h.stderr)
		}
	}
	posts := requestsTo(h, "POST", groupsPath)
	if len(posts) != 2 || posts[0].body["idempotency_key"] == posts[1].body["idempotency_key"] {
		t.Fatal(posts)
	}
}

func TestGroupCreateRefusesBeforeRequests(t *testing.T) {
	for _, args := range [][]string{
		{"group", "create", "Builds"},
		{"group", "create", " ", "--parent", "Acme"},
		{"group", "create", "Builds", "--parent", ""},
		{"group", "create", "Builds", "--parent", "Acme", "--idempotency-key", "not-a-key"},
	} {
		h := newManageHarness(t, managePersonal, governanceAPI(t, nil))
		if code := h.run(t, args...); code != 2 || len(h.requests) != 0 {
			t.Fatal(args, code, h.operations(), h.stderr)
		}
	}
}

func TestGroupCreateExplainsEachFailure(t *testing.T) {
	for _, tt := range []struct {
		name       string
		reply      manageReply
		want, deny []string
	}{
		{"key reused", manageReply{409, nil, `{"detail":"conflict","code":"idempotency_key_reused"}`}, []string{"Nothing changed.", "belongs to a create with another parent, name or label"}, nil},
		{"key group deleted", manageReply{409, nil, `{"detail":"conflict","code":"idempotency_key_group_deleted"}`}, []string{"has since been deleted"}, nil},
		{"name taken", manageReply{409, nil, `{"detail":"A live sibling group already has this name"}`}, []string{"Nothing changed.", "rerun that attempt with its idempotency key"}, nil},
		{"parent moved", manageReply{409, nil, `{"detail":"Group moved during the request; retry against its current parent"}`}, []string{"Nothing changed.", "retry against its current parent"}, []string{"another name"}},
		{"parent missing", manageReply{422, nil, `{"detail":"Parent group not found or deleted"}`}, []string{"Nothing changed."}, []string{"unknown"}},
		{"busy", manageReply{409, http.Header{"Retry-After": {"2"}}, `{"detail":"busy"}`}, []string{"Nothing changed. Another change holds the group; the request is safe to retry. Retry in 2s."}, nil},
		{"not ready", manageReply{503, nil, `{"detail":"Delegation authorization is not ready"}`}, []string{"Nothing changed."}, nil},
		{"proxy unavailable", manageReply{503, nil, `{"detail":"Service temporarily unavailable"}`}, []string{"may have taken effect", "The outcome is unknown. Rerun this command with --idempotency-key "}, []string{"Nothing changed", "Try again shortly"}},
		{"proxy error", manageReply{502, nil, `{}`}, []string{"may have taken effect", "The outcome is unknown. Rerun this command with --idempotency-key "}, []string{"Nothing changed", "Try again shortly"}},
		{"unreadable answer", manageReply{201, nil, `not json`}, []string{"The outcome is unknown", "--idempotency-key "}, nil},
		{"receipt without a group", manageReply{201, nil, `{"data":{}}`}, []string{"The outcome is unknown", "--idempotency-key "}, nil},
		{"receipt with a malformed group ID", manageReply{201, nil, `{"data":{"group_id":"grp-1"}}`}, []string{"The outcome is unknown", "--idempotency-key "}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
				if r.method == "POST" && r.path == groupsPath {
					return tt.reply, true
				}
				return manageReply{}, false
			}))
			if code := h.run(t, "group", "create", "Builds", "--parent", "Acme/Platform"); code != 1 {
				t.Fatal(code, h.stderr)
			}
			for _, want := range tt.want {
				if !strings.Contains(h.stderr.String(), want) {
					t.Errorf("missing %q in %s", want, h.stderr)
				}
			}
			for _, deny := range tt.deny {
				if strings.Contains(h.stderr.String(), deny) {
					t.Errorf("unexpected %q in %s", deny, h.stderr)
				}
			}
		})
	}
}

// A write whose answer is lost names a check that finds the same group by ID,
// so a rename or a new group at the old path cannot answer for it.
func TestGroupWritesCheckByIDAfterALostAnswer(t *testing.T) {
	for _, tt := range []struct {
		args       []string
		method     string
		path, want string
	}{
		{[]string{"edit", "Acme/Platform", "--name", "Builds"}, "PATCH", platformRoute, "Check it with `avr group view " + groupPlatform + " --org " + inspectionOrg + "` before retrying."},
		{[]string{"delete", "Acme/Platform", "--yes"}, "DELETE", platformRoute, "Check it with `avr group view " + groupPlatform + " --org " + inspectionOrg + "` before retrying: a deleted group shows when it was deleted"},
		{[]string{"limit", "set", "Acme/Platform", "--max-vcpu", "8", "--yes"}, "PUT", platformRoute + "/limits/max_vcpu", "Check it with `avr group limit list " + groupPlatform + " --org " + inspectionOrg + "` before retrying."},
		{[]string{"limit", "clear", "Acme/Platform", "--yes"}, "DELETE", platformRoute + "/limits/max_vcpu", "Check it with `avr group limit list " + groupPlatform + " --org " + inspectionOrg + "` before retrying."},
		{[]string{"member", "add", "Acme/Platform", memberAda}, "POST", membersBulk, groupMemberCheck},
		{[]string{"member", "remove", "Acme/Platform", "ada@example.test", "--yes"}, "POST", membersBulk, groupMemberCheck},
	} {
		t.Run(strings.Join(tt.args[:2], " "), func(t *testing.T) {
			h := newManageHarness(t, managePersonal, groupWriteAPI(t, nil, func(r manageRequest) (manageReply, bool) {
				return manageReply{502, nil, `{}`}, r.method == tt.method && r.path == tt.path
			}))
			if code := h.run(t, append([]string{"group"}, tt.args...)...); code != 1 {
				t.Fatal(code, h.stderr)
			}
			if !strings.Contains(h.stderr.String(), "The outcome is unknown. "+tt.want) || strings.Contains(h.stderr.String(), "Nothing changed") {
				t.Fatal(h.stderr)
			}
		})
	}
}

// A name may hold a slash, so Acme/Ops and Acme/Ops/ can both be groups, and
// so can a group named after another's ID with a slash. A value is matched as
// typed and without the slashes and spaces around it, and when the readings
// name different groups only the ID will do, for every command.
func TestGroupPathKeepsLiteralSlashes(t *testing.T) {
	const (
		groupOps      = "grp-" + "019a0000000070008000000000000027"
		groupOpsSlash = "grp-" + "019a0000000070008000000000000028"
		groupIDName   = "grp-" + "019a0000000070008000000000000029"
	)
	rows := append(groupTree(), groupRecord(groupOps, groupAcme, "Ops"), groupRecord(groupOpsSlash, groupAcme, "Ops/"), groupRecord(groupIDName, groupAcme, groupTools+"/"))
	api := func(t *testing.T) func(manageRequest) manageReply {
		pages := groupPages(t, rows)
		return func(r manageRequest) manageReply {
			if r.method == "PATCH" {
				return manageReply{200, nil, manageJSON(t, map[string]any{"data": groupRecord(strings.TrimPrefix(r.path, groupsPath+"/"), groupAcme, "Ops")})}
			}
			return pages(r)
		}
	}
	for _, tc := range []struct{ value, want, other string }{
		{"Acme/Ops", groupOps, ""},
		{"Acme/Ops/", "", groupOpsSlash},
		{"Ops/", "", groupOpsSlash},
		{"/Acme/Ops/", "", groupOpsSlash},
		{"Acme/Platform/", groupPlatform, ""},
		{"/Acme/Platform", groupPlatform, ""},
		{"/ Platform /", groupPlatform, ""},
		{"/" + groupPlatform + "/", groupPlatform, ""},
		{groupTools + "/", "", groupIDName},
	} {
		t.Run(tc.value, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, api(t))
			code := h.run(t, "group", "edit", tc.value, "--label", "new")
			if tc.want != "" {
				if code != 0 || !slices.Equal(writes(h), []string{"PATCH " + groupsPath + "/" + tc.want}) {
					t.Fatal(code, h.operations(), h.stderr)
				}
				return
			}
			bare := groupOps
			if tc.other == groupIDName {
				bare = groupTools
			}
			for _, want := range []string{"matches 2 groups:", "(" + bare + ")", "(" + tc.other + ")", "Hint: Pass the group ID: the value names different groups with and without the slashes around it."} {
				if !strings.Contains(h.stderr.String(), want) {
					t.Errorf("missing %q in %s", want, h.stderr)
				}
			}
			if code != 2 || len(writes(h)) != 0 {
				t.Fatal(code, h.operations())
			}
		})
	}
	h := newManageHarness(t, managePersonal, api(t))
	if code := h.run(t, "group", "member", "add", "Acme/Ops/", memberAda); code != 2 || len(writes(h)) != 0 {
		t.Fatal(code, h.operations(), h.stderr)
	}
	h = newManageHarness(t, managePersonal, api(t))
	if code := h.run(t, "group", "view", groupTools+"/"); code != 2 || h.out.Len() != 0 {
		t.Fatal(code, h.out, h.stderr)
	}
}

// A listed group shows its listing row and path without a second read, so
// the two cannot come from different versions of the group.
func TestGroupViewShowsTheListedRow(t *testing.T) {
	h := newManageHarness(t, managePersonal, governanceAPI(t, nil))
	if code := h.run(t, "group", "view", "Acme/Platform", "--json", "group_id,display_name,path"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if len(requestsTo(h, "GET", platformRoute)) != 0 || !strings.Contains(h.out.String(), `"path": "Acme/Platform"`) || !strings.Contains(h.out.String(), `"display_name": "Platform"`) {
		t.Fatal(h.operations(), h.out)
	}
}

// The listing leaves out deleted groups, so one named by ID is read directly.
func TestGroupViewShowsADeletedGroup(t *testing.T) {
	h := newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		record := groupRecord(groupDeleted, groupAcme, "Old")
		record["soft_deleted_at"] = "2026-11-20T10:00:00Z"
		return manageReply{200, nil, manageJSON(t, map[string]any{"data": record})}, r.method == "GET" && r.path == groupsPath+"/"+groupDeleted
	}))
	if code := h.run(t, "group", "view", groupDeleted); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if !regexp.MustCompile(`Deleted\s+2026-11-20T10:00:00Z`).MatchString(h.out.String()) {
		t.Fatal(h.out)
	}
}

func TestGroupEditSendsOnlyTheFieldsGiven(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want map[string]any
	}{
		{[]string{"--name", " Builds "}, map[string]any{"display_name": "Builds"}},
		{[]string{"--label", ""}, map[string]any{"label": nil}},
		{[]string{"--label", "runners", "--name", "Builds"}, map[string]any{"display_name": "Builds", "label": "runners"}},
	} {
		h := newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
			if r.method == "PATCH" && r.path == platformRoute {
				return manageReply{200, nil, manageJSON(t, map[string]any{"data": groupRecord(groupPlatform, groupAcme, "Builds")})}, true
			}
			return manageReply{}, false
		}))
		if code := h.run(t, append([]string{"group", "edit", "Acme/Platform"}, tt.args...)...); code != 0 {
			t.Fatal(tt.args, code, h.stderr)
		}
		patches := requestsTo(h, "PATCH", platformRoute)
		if len(patches) != 1 || fmt.Sprint(patches[0].body) != fmt.Sprint(tt.want) {
			t.Fatal(tt.args, patches)
		}
	}
	for _, args := range [][]string{{}, {"--name", " "}} {
		h := newManageHarness(t, managePersonal, governanceAPI(t, nil))
		if code := h.run(t, append([]string{"group", "edit", "Acme/Platform"}, args...)...); code != 2 || len(h.requests) != 0 {
			t.Fatal(args, code, h.operations())
		}
	}
}

func groupDeleteAPI(t *testing.T, reply manageReply) func(manageRequest) manageReply {
	return governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		if r.method == "DELETE" && r.path == platformRoute {
			return reply, true
		}
		return manageReply{}, false
	})
}

func TestGroupDeleteAsksFirst(t *testing.T) {
	for _, tt := range []struct {
		input   string
		args    []string
		deleted bool
	}{
		{"", nil, false},
		{"n\n", nil, false},
		{"y\n", nil, true},
		{"", []string{"--yes"}, true},
	} {
		h := newManageHarness(t, managePersonal, groupDeleteAPI(t, manageReply{204, nil, ""}))
		h.d.In = strings.NewReader(tt.input)
		code := h.run(t, append([]string{"group", "delete", "Acme/Platform"}, tt.args...)...)
		if sent := len(requestsTo(h, "DELETE", platformRoute)) == 1; sent != tt.deleted || (code == 0) != tt.deleted {
			t.Fatal(tt, code, h.operations(), h.stderr)
		}
	}
}

func TestGroupDeleteJSONKeepsThePromptOffStandardOutput(t *testing.T) {
	h := newManageHarness(t, managePersonal, groupDeleteAPI(t, manageReply{204, nil, ""}))
	h.d.In = strings.NewReader("y\n")
	if code := h.run(t, "group", "delete", "Acme/Platform", "--json", "group_id,path,deleted"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	var record map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &record); err != nil {
		t.Fatal(err, h.out)
	}
	want := map[string]any{"group_id": groupPlatform, "path": "Acme/Platform", "deleted": true}
	if fmt.Sprint(record) != fmt.Sprint(want) || !strings.Contains(h.stderr.String(), "Delete group Acme/Platform ("+groupPlatform+")?") {
		t.Fatal(record, h.stderr)
	}
}

func TestGroupDeleteNamesWhatHoldsTheGroup(t *testing.T) {
	body := manageJSON(t, map[string]any{
		"code": "group_in_use", "detail": "The group is in use",
		"blockers": []map[string]any{
			{"type": "limit", "limit_key": "max_vcpu", "kind": "shared_pool"},
			{"type": "suspension", "suspension_id": "sus-1", "user_id": memberAda},
			{"type": "workflow", "workflow_id": "wf-1", "repository_id": "repo-1"},
			{"type": "quota"},
		},
		"hidden_blocker_count": 2,
	})
	h := newManageHarness(t, managePersonal, groupDeleteAPI(t, manageReply{409, nil, body}))
	if code := h.run(t, "group", "delete", "Acme/Platform", "--yes"); code != 1 {
		t.Fatal(code, h.stderr)
	}
	for _, want := range []string{
		"`avr group limit clear " + groupPlatform + " --org " + inspectionOrg + "`",
		"suspension of member " + memberAda + ": lift it in the group's settings",
		"workflow wf-1 of repository repo-1 charges its jobs to this group: an organization administrator with access to the repository can clear that with `avr api -X PUT /orgs/" + inspectionOrg + "/workflows/wf-1/attribution-context -F group_id=null`",
		"a blocker this version of avr does not know (quota)",
		"2 more workflows not listed: the server checks at most 100 at a time and names those in repositories it confirmed you can read. Once the listed ones are cleared, deleting again names more; a workflow in a repository you cannot read needs someone who can read it",
		"Nothing changed.",
	} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("missing %q in %s", want, h.stderr)
		}
	}
}

// Workflows the server did not name are still counted, in the singular too.
func TestGroupDeleteCountsOneUnlistedWorkflow(t *testing.T) {
	body := manageJSON(t, map[string]any{"code": "group_in_use", "detail": "The group is in use", "blockers": []any{}, "hidden_blocker_count": 1})
	h := newManageHarness(t, managePersonal, groupDeleteAPI(t, manageReply{409, nil, body}))
	if code := h.run(t, "group", "delete", "Acme/Platform", "--yes"); code != 1 {
		t.Fatal(code, h.stderr)
	}
	if !strings.Contains(h.stderr.String(), "The group is still used by:\n    - 1 more workflow not listed: the server checks at most 100") {
		t.Fatal(h.stderr)
	}
}

// A group its SAML mappings own is deleted after the mappings are removed.
func TestGroupDeleteOfAnIdPGroupNamesTheMappings(t *testing.T) {
	h := newManageHarness(t, managePersonal, groupDeleteAPI(t, manageReply{409, nil, `{"detail":"IdP-synced groups cannot be deleted manually"}`}))
	if code := h.run(t, "group", "delete", "Acme/Platform", "--yes"); code != 1 {
		t.Fatal(code, h.stderr)
	}
	if !strings.Contains(h.stderr.String(), "(HTTP 409): IdP-synced groups cannot be deleted manually\n  Hint: Nothing changed. Remove its SAML group mappings in the Avrea console (SSO / SAML settings), then delete it again.") {
		t.Fatal(h.stderr)
	}
}

// A refusal without blockers keeps the API's reason and advises no cleanup
// it did not ask for: the root group stays however empty it gets.
func TestGroupDeleteRefusalKeepsTheAPIReason(t *testing.T) {
	h := newManageHarness(t, managePersonal, groupDeleteAPI(t, manageReply{409, nil, `{"detail":"The root group is managed through the organization screens"}`}))
	if code := h.run(t, "group", "delete", "Acme/Platform", "--yes"); code != 1 {
		t.Fatal(code, h.stderr)
	}
	if !strings.Contains(h.stderr.String(), "(HTTP 409): The root group is managed through the organization screens\n  Hint: Nothing changed.") || strings.Contains(h.stderr.String(), "Remove") {
		t.Fatal(h.stderr)
	}
}

func TestGroupDeleteSaysWhatGoesWithTheGroup(t *testing.T) {
	h := newManageHarness(t, managePersonal, groupDeleteAPI(t, manageReply{204, nil, ""}))
	h.d.In = strings.NewReader("y\n")
	if code := h.run(t, "group", "delete", "Acme/Platform"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if !strings.Contains(h.out.String(), "? Its resource access grants go with it.") {
		t.Fatal(h.out)
	}
}

func groupMemberAPI(t *testing.T, statuses map[string]string) func(manageRequest) manageReply {
	return governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		if r.method == "POST" && r.path == membersBulk {
			return bulkReply(t, r, statuses), true
		}
		return manageReply{}, false
	})
}

func TestGroupMemberAddSendsOneChange(t *testing.T) {
	h := newManageHarness(t, managePersonal, groupMemberAPI(t, map[string]string{memberAda: "noop"}))
	if code := h.run(t, "group", "member", "add", "Acme/Platform", "@me", "ada@example.test", memberBob, memberBob, memberAda); code != 0 {
		t.Fatal(code, h.stderr)
	}
	bulk := requestsTo(h, "POST", membersBulk)
	if len(bulk) != 1 || fmt.Sprint(bulk[0].body) != fmt.Sprint(map[string]any{"add": []any{memberMe, memberAda, memberBob}}) {
		t.Fatal(bulk)
	}
	if len(requestsTo(h, "POST", memberLookups)) != 1 || len(requestsTo(h, "GET", "/users/me")) != 1 {
		t.Fatal(h.operations())
	}
	for _, want := range []string{"@me\t" + memberMe + "\tadded", "ada@example.test\t" + memberAda + "\talready a member", memberBob + "\t" + memberBob + "\tadded"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("missing %q in %s", want, h.out)
		}
	}
}

func TestGroupMemberChangeReportsFailedAndMissingResults(t *testing.T) {
	for _, tt := range []struct {
		name         string
		statuses     map[string]string
		out, err     string
		rerunAdvised bool
	}{
		{"refused", map[string]string{memberBob: "error"}, "refused: member is not active", "Not every member was changed: 1 changed, 1 refused.", false},
		{"missing", map[string]string{memberAda: "noop", memberBob: "omit"}, "unknown: the API returned no result for this member", "Not every member was changed: 1 already in place, 1 with an unknown outcome.", true},
		{"other action", map[string]string{memberBob: "other action"}, "unknown: the API returned a result for another action", "1 changed, 1 with an unknown outcome.", true},
		{"twice", map[string]string{memberBob: "twice"}, "unknown: the API returned more than one result for this member", "1 changed, 1 with an unknown outcome.", true},
		{"unknown status", map[string]string{memberBob: "pending"}, "unknown: the API returned a status this version of avr does not know: pending", "1 changed, 1 with an unknown outcome.", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, groupMemberAPI(t, tt.statuses))
			if code := h.run(t, "group", "member", "add", "Acme/Platform", memberAda, memberBob); code != 1 {
				t.Fatal(code, h.stderr)
			}
			if !strings.Contains(h.out.String(), tt.out) || !strings.Contains(h.stderr.String(), tt.err) || strings.Contains(h.stderr.String(), groupMemberCheck) != tt.rerunAdvised {
				t.Fatal(h.out, h.stderr)
			}
		})
	}
}

// groupMemberCheck is the advice after a member change with an unknown outcome.
const groupMemberCheck = "Check it with `avr group member list " + groupPlatform + " --org " + inspectionOrg + "` before retrying."

// The summary is the only report that a member did not change, so a failing
// --jq expression must not swallow it.
func TestGroupMemberSummarySurvivesAnOutputFailure(t *testing.T) {
	h := newManageHarness(t, managePersonal, groupMemberAPI(t, map[string]string{memberBob: "omit"}))
	if code := h.run(t, "group", "member", "add", "Acme/Platform", memberAda, memberBob, "--json", "user_id", "--jq", ".[] | .user_id | tonumber"); code != 1 {
		t.Fatal(code, h.stderr)
	}
	for _, want := range []string{"Not every member was changed: 1 changed, 1 with an unknown outcome.", groupMemberCheck, "jq"} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("missing %q in %s", want, h.stderr)
		}
	}
}

// Arguments and file lines name the same member however they are padded.
func TestGroupMemberArgumentsAndFileShareOneLimit(t *testing.T) {
	many := make([]string, groupMemberBatchLimit)
	for i := range many {
		many[i] = fmt.Sprintf("usr-%032x", i)
	}
	args := slices.Clone(many)
	args[0] = " " + many[0] + " "
	h := newManageHarness(t, managePersonal, groupMemberAPI(t, nil))
	h.d.In = strings.NewReader(many[0] + "\n")
	if code := h.run(t, append([]string{"group", "member", "add", "Acme/Platform", "--file", "-"}, args...)...); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if bulk := requestsTo(h, "POST", membersBulk); len(bulk) != 1 || len(bulk[0].body["add"].([]any)) != groupMemberBatchLimit {
		t.Fatal(h.operations())
	}
}

// A file line starting with # is a comment unless it is an email address:
// skipping #ops@example.test would leave that member's access in place.
func TestGroupMemberFileKeepsAddressesStartingWithHash(t *testing.T) {
	h := newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		switch {
		case r.method == "POST" && r.path == membersBulk:
			return bulkReply(t, r, nil), true
		case r.method == "POST" && r.path == memberLookups:
			verified := map[string]string{"#ops@example.test": memberBob, "#ops@localhost": memberMe}[fmt.Sprint(r.body["email"])]
			rows := []any{}
			if verified != "" {
				rows = append(rows, orgMember(verified, "Ops", "active"))
			}
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": rows, "pagination": map[string]any{"next_cursor": nil}})}, true
		}
		return manageReply{}, false
	}))
	// The API verifies addresses without a dot in the domain, such as
	// @localhost, so the CLI reads them as members too.
	h.d.In = strings.NewReader("# team leads\n#ops@example.test\n#ops@localhost\n" + memberAda + "\n# ada@example.test\n#" + memberMe + "\n")
	if code := h.run(t, "group", "member", "remove", "Acme/Platform", "--file", "-", "--yes"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	var looked []any
	for _, lookup := range requestsTo(h, "POST", memberLookups) {
		looked = append(looked, lookup.body["email"])
	}
	bulk := requestsTo(h, "POST", membersBulk)
	if fmt.Sprint(looked) != "[#ops@example.test #ops@localhost]" || len(bulk) != 1 || fmt.Sprint(bulk[0].body) != fmt.Sprint(map[string]any{"remove": []any{memberBob, memberMe, memberAda}}) {
		t.Fatal(h.operations(), bulk)
	}
}

// A sign-in refusal keeps its exit status and still says nothing changed.
func TestGroupWriteAuthRefusalSaysNothingChanged(t *testing.T) {
	h := newManageHarness(t, managePersonal, groupWriteAPI(t, nil, func(r manageRequest) (manageReply, bool) {
		return manageReply{401, nil, `{"detail":"Not authenticated"}`}, r.method == "PATCH"
	}))
	if code := h.run(t, "group", "edit", "Acme/Platform", "--name", "Builds"); code != 4 || !strings.Contains(h.stderr.String(), "Hint: Nothing changed.") {
		t.Fatal(code, h.stderr)
	}
	h = newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		return manageReply{401, nil, `{"detail":"Not authenticated"}`}, r.method == "GET" && r.path == groupsPath+"/"+groupDeleted
	}))
	if code := h.run(t, "group", "view", groupDeleted); code != 4 || strings.Contains(h.stderr.String(), "Nothing changed") {
		t.Fatal(code, h.stderr)
	}
}

func TestGroupMemberChangeRefusesBeforeWriting(t *testing.T) {
	many := make([]string, groupMemberBatchLimit+1)
	for i := range many {
		many[i] = fmt.Sprintf("usr-%032x", i)
	}
	manyFile := filepath.Join(t.TempDir(), "members.txt")
	if err := os.WriteFile(manyFile, []byte(strings.Join(append(many, many...), "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		args []string
		code int
		want string
		api  func(manageRequest) (manageReply, bool)
	}{
		{"display name", []string{"add", "Acme/Platform", "Ada"}, 2, "never by display name", nil},
		{"malformed ID", []string{"add", "Acme/Platform", "usr-ADA"}, 2, "is not a user ID", nil},
		{"email starting like an ID", []string{"add", "Acme/Platform", "usr-ops@example.test"}, 1, "No member you can manage has verified 'usr-ops@example.test'", nil},
		{"unknown email", []string{"add", "Acme/Platform", memberAda, "nobody@example.test"}, 1, "No member you can manage has verified 'nobody@example.test'", nil},
		{"lookup unavailable", []string{"add", "Acme/Platform", "ada@example.test"}, 1, "not available with this credential, organization or server", func(r manageRequest) (manageReply, bool) {
			return manageReply{404, nil, `{"detail":"Not Found"}`}, r.path == memberLookups
		}},
		{"lookup refused", []string{"add", "Acme/Platform", "ada@example.test"}, 1, "Only administrators who manage members can look them up by email.", func(r manageRequest) (manageReply, bool) {
			return manageReply{403, nil, `{"detail":"Forbidden"}`}, r.path == memberLookups
		}},
		{"stdin without yes", []string{"remove", "Acme/Platform", "--file", "-"}, 2, "needs --yes", nil},
		{"too many", append([]string{"add", "Acme/Platform"}, many...), 2, "at most 250 members; more were given", nil},
		{"too many in the file", []string{"add", "Acme/Platform", "--file", manyFile}, 2, "at most 250 members; more were given", nil},
		{"no members", []string{"add", "Acme/Platform"}, 2, "Pass at least one MEMBER", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, governanceAPI(t, tt.api))
			if code := h.run(t, append([]string{"group", "member"}, tt.args...)...); code != tt.code || len(writes(h)) != 0 {
				t.Fatal(code, h.operations(), h.stderr)
			}
			if !strings.Contains(h.stderr.String(), tt.want) {
				t.Fatalf("missing %q in %s", tt.want, h.stderr)
			}
			if tt.code == 2 && len(h.requests) != 0 {
				t.Fatal("a usage error sent requests:", h.operations())
			}
		})
	}
}

// An email lookup that matches one member without a valid user ID resolves
// no one, and the change is not sent for the other members either.
func TestGroupMemberEmailLookupNeedsAUserID(t *testing.T) {
	for _, id := range []any{nil, "", "usr-ADA", 42} {
		h := newManageHarness(t, managePersonal, groupWriteAPI(t, nil, func(r manageRequest) (manageReply, bool) {
			row := orgMember(memberAda, "Ada", "active")
			if id == nil {
				delete(row, "user_id")
			} else {
				row["user_id"] = id
			}
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": []any{row}, "pagination": map[string]any{"next_cursor": nil}})}, r.path == memberLookups
		}))
		if code := h.run(t, "group", "member", "add", "Acme/Platform", memberBob, "ada@example.test"); code != 1 || len(writes(h)) != 0 {
			t.Fatal(id, code, h.operations(), h.stderr)
		}
		if !strings.Contains(h.stderr.String(), "unexpected API response") {
			t.Fatal(id, h.stderr)
		}
	}
}

// A lookup refused for an old sign-in keeps the sign-in exit status and
// advice, not the advice for a caller who lacks the privilege.
func TestGroupMemberEmailLookupKeepsTheSignInRefusal(t *testing.T) {
	h := newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		return manageReply{403, http.Header{"X-Reauthentication-Required": {"true"}}, `{"detail":"Recent authentication required"}`}, r.path == memberLookups
	}))
	if code := h.run(t, "group", "member", "add", "Acme/Platform", "ada@example.test"); code != 4 || len(writes(h)) != 0 {
		t.Fatal(code, h.operations(), h.stderr)
	}
	if got := h.stderr.String(); !strings.Contains(got, "needs a recent sign-in") || strings.Contains(got, "Only administrators") {
		t.Fatal(got)
	}
}

// Listing the JSON fields, or refusing an output option, reads no members:
// standard input stays unread, and remove asks for no --yes.
func TestGroupMemberChangeChecksOutputOptionsBeforeReadingMembers(t *testing.T) {
	for _, tt := range []struct {
		args []string
		ok   bool
	}{
		{[]string{"add", "Acme/Platform", "--file", "-", "--json", "?"}, true},
		{[]string{"remove", "Acme/Platform", "--file", "-", "--json", "?"}, true},
		{[]string{"add", "Acme/Platform", "--file", "-", "--json", "nope"}, false},
		{[]string{"remove", "Acme/Platform", "--file", "-", "--jq", "."}, false},
	} {
		h := newManageHarness(t, managePersonal, governanceAPI(t, nil))
		h.d.In = unreadInput{t}
		if code := h.run(t, append([]string{"group", "member"}, tt.args...)...); (code == 0) != tt.ok || len(h.requests) != 0 {
			t.Fatal(tt.args, code, h.operations(), h.stderr)
		}
		if tt.ok && !strings.Contains(h.out.String(), "user_id") {
			t.Fatal(tt.args, h.out)
		}
		if !tt.ok && strings.Contains(h.stderr.String(), "needs --yes") {
			t.Fatal(tt.args, h.stderr)
		}
	}
}

// unreadInput fails a test that reads it.
type unreadInput struct{ t *testing.T }

func (u unreadInput) Read([]byte) (int, error) {
	u.t.Error("standard input was read")
	return 0, io.EOF
}

// A member list abandoned by an interrupt stops reading: the rest of standard
// input belongs to whatever reads it next.
func TestGroupMemberFileReadStopsReadingAfterInterrupt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader, writer := io.Pipe()
		defer func() { _ = writer.Close() }()
		input := &countingReader{reader: reader}
		h := newManageHarness(t, managePersonal, groupMemberAPI(t, nil))
		h.d.In = input
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan int, 1)
		go func() { done <- Execute(ctx, []string{"group", "member", "add", "Acme/Platform", "--file", "-"}, h.d) }()
		// Each write returns once the command has read it.
		if _, err := writer.Write([]byte(memberAda + "\n")); err != nil {
			t.Fatal(err)
		}
		// The command waits in its next read when the interrupt comes.
		synctest.Wait()
		cancel()
		if code := <-done; code != 130 {
			t.Fatal(code, h.stderr)
		}
		// The read pending at the interrupt still takes this line; nothing
		// asks for more after it.
		if _, err := writer.Write([]byte(memberBob + "\n")); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if reads := input.reads.Load(); reads != 2 {
			t.Fatalf("%d reads", reads)
		}
	})
}

// countingReader counts the reads that reach reader.
type countingReader struct {
	reader io.Reader
	reads  atomic.Int32
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.reads.Add(1)
	return c.reader.Read(p)
}

// An interrupt ends a member list that standard input keeps open.
func TestGroupMemberFileReadStopsOnInterrupt(t *testing.T) {
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	h := newManageHarness(t, managePersonal, groupMemberAPI(t, nil))
	h.d.In = reader
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	go func() { done <- Execute(ctx, []string{"group", "member", "add", "Acme/Platform", "--file", "-"}, h.d) }()
	// The write returns once the command reads, so the read has started.
	if _, err := writer.Write([]byte(memberAda + "\n")); err != nil {
		t.Fatal(err)
	}
	cancel()
	if code := <-done; code != 130 || len(h.requests) != 0 {
		t.Fatal(code, h.operations(), h.stderr)
	}
}

func TestGroupMemberRemoveAsksFirst(t *testing.T) {
	file := filepath.Join(t.TempDir(), "members.txt")
	if err := os.WriteFile(file, []byte("# leavers\n"+memberAda+"\n\n"+memberBob+"\n"+memberAda+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		input   string
		args    []string
		removed bool
	}{
		{"", nil, false},
		{"y\n", nil, true},
		{"", []string{"--yes"}, true},
	} {
		h := newManageHarness(t, managePersonal, groupMemberAPI(t, nil))
		h.d.In = strings.NewReader(tt.input)
		code := h.run(t, append([]string{"group", "member", "remove", "Acme/Platform", "--file", file}, tt.args...)...)
		bulk := requestsTo(h, "POST", membersBulk)
		if (len(bulk) == 1) != tt.removed || (code == 0) != tt.removed {
			t.Fatal(tt, code, h.operations(), h.stderr)
		}
		if tt.removed && fmt.Sprint(bulk[0].body) != fmt.Sprint(map[string]any{"remove": []any{memberAda, memberBob}}) {
			t.Fatal(bulk[0].body)
		}
		if len(tt.args) == 0 && !strings.Contains(h.out.String(), "Remove 2 member(s) from group Acme/Platform ("+groupPlatform+"):\n  "+memberAda+"\n  "+memberBob) {
			t.Fatal(h.out)
		}
	}
}

func TestGroupMemberListFollowsPagesUpToTheLimit(t *testing.T) {
	members := []map[string]any{}
	for _, id := range []string{memberAda, memberBob, memberMe} {
		members = append(members, map[string]any{"user_id": id, "name": nil, "email": nil, "source": "manual", "stale_sources": []any{}, "added_at": "2026-11-01T10:00:00Z"})
	}
	api := func(t *testing.T) func(manageRequest) manageReply {
		return governanceAPI(t, func(r manageRequest) (manageReply, bool) {
			if r.method != "GET" || r.path != platformRoute+"/members" {
				return manageReply{}, false
			}
			start := 0
			if r.query.Get("cursor") != "" {
				start = 2
			}
			end := min(start+2, len(members))
			next := any(nil)
			if end < len(members) {
				next = "2"
			}
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": members[start:end], "pagination": map[string]any{"next_cursor": next}})}, true
		})
	}
	h := newManageHarness(t, managePersonal, api(t))
	if code := h.run(t, "group", "member", "list", "Acme/Platform", "--json", "user_id"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &rows); err != nil || len(rows) != 3 || h.stderr.Len() != 0 {
		t.Fatal(err, h.out, h.stderr)
	}
	h = newManageHarness(t, managePersonal, api(t))
	if code := h.run(t, "group", "member", "list", "Acme/Platform", "-L", "2"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if !strings.Contains(h.stderr.String(), "Showing the first 2 members. Raise --limit to see more.") {
		t.Fatal(h.stderr)
	}
	for _, r := range requestsTo(h, "GET", platformRoute+"/members") {
		if r.query.Get("limit") != "2" {
			t.Fatal(r.query)
		}
	}
}

func groupLimitAPI(t *testing.T, limits []map[string]any) func(manageRequest) manageReply {
	return groupWriteAPI(t, limits, nil)
}

// groupWriteAPI answers group limit and member writes as the API does, after
// handle, which can answer any request first.
func groupWriteAPI(t *testing.T, limits []map[string]any, handle func(manageRequest) (manageReply, bool)) func(manageRequest) manageReply {
	return governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		if handle != nil {
			if reply, ok := handle(r); ok {
				return reply, true
			}
		}
		switch {
		case r.method == "POST" && r.path == membersBulk:
			return bulkReply(t, r, nil), true
		case r.method == "GET" && r.path == platformRoute+"/limits":
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": limits})}, true
		case r.method == "PUT" && r.path == platformRoute+"/limits/max_vcpu":
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": map[string]any{"group_id": groupPlatform, "limit_key": "max_vcpu", "kind": "shared_pool", "limit_type": "concurrent", "value": r.body["value"], "period": nil, "timezone": nil, "inherited": false}})}, true
		case r.method == "DELETE" && r.path == platformRoute+"/limits/max_vcpu":
			return manageReply{204, nil, ""}, true
		}
		return manageReply{}, false
	})
}

func vcpuLimit(group string, value int, inherited bool) map[string]any {
	return map[string]any{"group_id": group, "limit_key": "max_vcpu", "kind": "shared_pool", "limit_type": "concurrent", "value": value, "period": nil, "timezone": nil, "inherited": inherited}
}

// Setting a limit asks unless it raises or keeps the group's own limit: a
// first limit, a lower one, or one replacing a value avr cannot compare can
// make new jobs wait.
func TestGroupLimitSetAsksUnlessItRaisesTheLimit(t *testing.T) {
	own, inherited := vcpuLimit(groupPlatform, 64, false), vcpuLimit(groupAcme, 16, true)
	decimal, integral, unreadable := maps.Clone(own), maps.Clone(own), maps.Clone(own)
	decimal["value"], integral["value"], unreadable["value"] = 64.5, json.Number("64.0"), "lots"
	huge, hugeDecimal, hugeExponent := maps.Clone(own), maps.Clone(own), maps.Clone(own)
	huge["value"], hugeDecimal["value"], hugeExponent["value"] = json.Number("9007199254740993"), json.Number("9007199254740993.0"), json.Number("9.007199254740993e15")
	for _, tt := range []struct {
		name   string
		limits []map[string]any
		value  string
		input  string
		sent   bool
		prompt string
	}{
		{"raise", []map[string]any{own}, "128", "", true, ""},
		{"same", []map[string]any{own}, "64", "", true, ""},
		{"lower refused", []map[string]any{own}, "32", "", false, "from 64 to 32?"},
		{"lower confirmed", []map[string]any{own}, "32", "y\n", true, "from 64 to 32?"},
		{"inherited only refused", []map[string]any{inherited}, "8", "", false, "Set a vCPU limit of 8 on group Acme/Platform (" + groupPlatform + "), which has no limit of its own?"},
		{"inherited only confirmed", []map[string]any{inherited}, "8", "y\n", true, "which has no limit of its own?"},
		{"first limit refused", nil, "0", "", false, "Set a vCPU limit of 0 on group"},
		{"first limit confirmed", nil, "0", "y\n", true, "Set a vCPU limit of 0 on group"},
		{"below a decimal limit", []map[string]any{decimal}, "64", "", false, "from 64.5 to 64?"},
		{"above an integral decimal", []map[string]any{integral}, "65", "", true, ""},
		{"unreadable limit", []map[string]any{unreadable}, "128", "", false, "Replace the vCPU limit lots of group"},
		{"below past float precision", []map[string]any{huge}, "9007199254740992", "", false, "from 9007199254740993 to 9007199254740992?"},
		{"above past float precision", []map[string]any{huge}, "9007199254740994", "", true, ""},
		{"below a decimal past float precision", []map[string]any{hugeDecimal}, "9007199254740992", "", false, " to 9007199254740992?"},
		{"below an exponent past float precision", []map[string]any{hugeExponent}, "9007199254740992", "", false, " to 9007199254740992?"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, groupLimitAPI(t, tt.limits))
			h.d.In = strings.NewReader(tt.input)
			code := h.run(t, "group", "limit", "set", "Acme/Platform", "--max-vcpu", tt.value)
			puts := requestsTo(h, "PUT", platformRoute+"/limits/max_vcpu")
			if (len(puts) == 1) != tt.sent || (code == 0) != tt.sent {
				t.Fatal(code, h.operations(), h.stderr)
			}
			if asked := strings.Contains(h.out.String()+h.stderr.String(), "Running jobs keep running"); asked != (tt.prompt != "") || !strings.Contains(h.out.String()+h.stderr.String(), tt.prompt) {
				t.Fatal(h.out, h.stderr)
			}
			if tt.sent {
				got, err := json.Marshal(puts[0].body)
				if want := `{"kind":"shared_pool","limit_type":"concurrent","value":` + tt.value + `}`; err != nil || string(got) != want {
					t.Fatal(err, string(got))
				}
			}
		})
	}
	h := newManageHarness(t, managePersonal, groupLimitAPI(t, []map[string]any{own}))
	if code := h.run(t, "group", "limit", "set", "Acme/Platform", "--max-vcpu", "96", "--json", "value,previous_value"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if got := strings.Join(strings.Fields(h.out.String()), ""); got != `{"previous_value":64,"value":96}` {
		t.Fatal(got)
	}
	for _, args := range [][]string{{}, {"--max-vcpu", "-1"}} {
		h := newManageHarness(t, managePersonal, groupLimitAPI(t, nil))
		if code := h.run(t, append([]string{"group", "limit", "set", "Acme/Platform"}, args...)...); code != 2 || len(h.requests) != 0 {
			t.Fatal(args, code, h.operations())
		}
	}
}

// The summary escapes the previous and the new value as the prompt does: both
// come from the API.
func TestGroupLimitSetEscapesTheAPIValues(t *testing.T) {
	previous := vcpuLimit(groupPlatform, 0, false)
	previous["value"] = "\x1b[2Jlots"
	h := newManageHarness(t, managePersonal, groupWriteAPI(t, []map[string]any{previous}, func(r manageRequest) (manageReply, bool) {
		record := vcpuLimit(groupPlatform, 0, false)
		record["value"] = "\x1b]0;title\x07"
		return manageReply{200, nil, manageJSON(t, map[string]any{"data": record})}, r.method == "PUT"
	}))
	if code := h.run(t, "group", "limit", "set", "Acme/Platform", "--max-vcpu", "8", "--yes"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if got := h.out.String(); strings.ContainsAny(got, "\x1b\x07") || !strings.Contains(got, `to \x1b]0;title\x07 (was \x1b[2Jlots).`) {
		t.Fatalf("%q", got)
	}
}

func TestGroupLimitClearAsksFirst(t *testing.T) {
	for _, tt := range []struct {
		input   string
		args    []string
		cleared bool
	}{
		{"", nil, false},
		{"y\n", nil, true},
		{"", []string{"-y"}, true},
	} {
		h := newManageHarness(t, managePersonal, groupLimitAPI(t, nil))
		h.d.In = strings.NewReader(tt.input)
		code := h.run(t, append([]string{"group", "limit", "clear", "Acme/Platform"}, tt.args...)...)
		if sent := len(requestsTo(h, "DELETE", platformRoute+"/limits/max_vcpu")) == 1; sent != tt.cleared || (code == 0) != tt.cleared {
			t.Fatal(tt, code, h.operations(), h.stderr)
		}
	}
}

func TestGroupUsageShowsThePools(t *testing.T) {
	h := newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		if r.method == "GET" && r.path == platformRoute+"/usage" {
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": map[string]any{
				"group_id": groupPlatform, "group_state": "active", "measured_at": "2026-11-24T10:00:00Z", "used_vcpu": 12,
				"pools": []map[string]any{{"group_id": groupAcme, "limit_key": "max_vcpu", "value": 64, "used_vcpu": 40}},
			}})}, true
		}
		return manageReply{}, false
	}))
	if code := h.run(t, "group", "usage", "Acme/Platform"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	for _, want := range []string{"12 (this group and its subgroups)", groupAcme, "64", "40"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("missing %q in %s", want, h.out)
		}
	}
}

func orgMember(id, name, state string) map[string]any {
	return map[string]any{"user_id": id, "name": name, "avatar_url": nil, "role": "member", "state": state, "joined_at": "2026-11-01T10:00:00Z"}
}

func accessAPI(t *testing.T, members []map[string]any, overrides map[string]any) func(manageRequest) manageReply {
	return governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		switch {
		case r.method == "GET" && r.path == orgMembers:
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": members})}, true
		case r.method == "GET" && strings.HasPrefix(r.path, orgMembers+"/") && strings.HasSuffix(r.path, "/effective-access"):
			record := map[string]any{
				"org_role": "member", "org_role_source": "manual", "governance": map[string]any{}, "notes": []any{"Access through the code host is decided per repository."},
				"groups": []map[string]any{{"group_id": groupPlatform, "display_name": "Platform", "path": []any{"Acme", "Platform"}, "source": "manual", "stale_sources": []any{}, "state": "active"}},
				"resource_access": []map[string]any{
					{"via": "group_grant", "group_id": groupPlatform, "group_name": "Platform", "resource_type": "vm", "resource_id": nil, "access_level": "use"},
					{"via": "group_grant", "group_id": nil, "group_name": nil, "resource_type": "repository", "resource_id": nil, "access_level": "read"},
					{"via": "partner_grant", "resource_type": "repository", "resource_id": "repo-1", "access_level": "read"},
					{"via": "code_host", "resource_type": "repository", "resource_id": nil, "access_level": nil},
				},
				"resource_access_complete": false, "resource_access_truncated": true, "root_capabilities": []any{},
			}
			maps.Copy(record, overrides)
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": record})}, true
		}
		return manageReply{}, false
	})
}

func TestOrgMembersAccessResolvesANameOnReads(t *testing.T) {
	members := []map[string]any{orgMember(memberAda, "Ada", "active"), orgMember(memberBob, "ada", "inactive")}
	h := newManageHarness(t, managePersonal, accessAPI(t, members, nil))
	if code := h.run(t, "org", "members", "access", "ADA"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if len(requestsTo(h, "GET", orgMembers+"/"+memberAda+"/effective-access")) != 1 {
		t.Fatal(h.operations())
	}
	for _, want := range []string{"Ada (" + memberAda + ")", "Acme/Platform (" + groupPlatform + ")", "grant to group Platform (" + groupPlatform + ")", "all Avrea repositories", "repositories on the code host", "grant to group you cannot see", "unknown source (partner_grant)", "This list is partial: it leaves out VMs", "Note: Access through the code host"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("missing %q in %s", want, h.out)
		}
	}
}

// Entries can be left out for two reasons, and the note names the one that
// applies: hidden groups leave every VM in place.
func TestOrgMembersAccessSaysWhyTheListIsIncomplete(t *testing.T) {
	members := []map[string]any{orgMember(memberAda, "Ada", "active")}
	h := newManageHarness(t, managePersonal, accessAPI(t, members, map[string]any{"resource_access_truncated": false}))
	if code := h.run(t, "org", "members", "access", memberAda); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if !strings.Contains(h.out.String(), "Some entries come from groups you may not see") || strings.Contains(h.out.String(), "partial") {
		t.Fatal(h.out)
	}
}

func TestOrgMembersAccessRefusesAnAmbiguousName(t *testing.T) {
	members := []map[string]any{orgMember(memberAda, "Sam", "active"), orgMember(memberBob, "sam", "active")}
	h := newManageHarness(t, managePersonal, accessAPI(t, members, nil))
	if code := h.run(t, "org", "members", "access", "Sam"); code != 1 {
		t.Fatal(code, h.stderr)
	}
	if !strings.Contains(h.stderr.String(), "'Sam' names 2 members:\n    "+memberAda+"\n    "+memberBob) || slices.ContainsFunc(h.operations(), func(op string) bool { return strings.HasSuffix(op, "/effective-access") }) {
		t.Fatal(h.operations(), h.stderr)
	}
}

func TestOrgMembersReadsEveryPage(t *testing.T) {
	h := newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		if r.method != "GET" || r.path != orgMembers {
			return manageReply{}, false
		}
		if r.query.Get("cursor") == "" {
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": []any{orgMember(memberAda, "Ada", "active")}, "pagination": map[string]any{"next_cursor": "c2"}})}, true
		}
		return manageReply{200, nil, manageJSON(t, map[string]any{"data": []any{orgMember(memberBob, "Bob", "active")}, "pagination": map[string]any{"next_cursor": nil}})}, true
	}))
	if code := h.run(t, "org", "members", "--json", "user_id"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if got := strings.Join(strings.Fields(h.out.String()), ""); got != `[{"user_id":"`+memberAda+`"},{"user_id":"`+memberBob+`"}]` {
		t.Fatal(got)
	}
}

func TestOrgGovernanceShowsDecisionsAndGroups(t *testing.T) {
	h := newManageHarness(t, managePersonal, governanceAPI(t, func(r manageRequest) (manageReply, bool) {
		if r.method == "GET" && r.path == "/orgs/"+inspectionOrg+"/governance" {
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": map[string]any{
				"groups_enabled": true, "resource_access_enabled": true, "delegation_enabled": false, "eligible_root_admin": false, "manual_root_capabilities": []any{},
				"root_capabilities": []map[string]any{{"capability": "manage_members", "allowed": false, "denial": "not a root administrator"}},
				"groups": []map[string]any{{
					"group_id": groupPlatform, "display_name": "Platform", "can_recover_suspensions": true, "can_suspend_group": true,
					"capabilities": []map[string]any{
						{"capability": "manage_members", "allowed": true, "denial": nil},
						{"capability": "manage_limits", "allowed": false, "denial": "outside your groups"},
					},
				}},
			}})}, true
		}
		return manageReply{}, false
	}))
	if code := h.run(t, "org", "governance"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	for _, want := range []string{"denied: not a root administrator", "Groups:\n", groupPlatform, "Eligible root administrator"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("missing %q in %s", want, h.out)
		}
	}
	if !regexp.MustCompile(`Platform\s*\|\s*manage_members, lift suspensions, suspend the group\s*\|\s*manage_limits\s*\|`).MatchString(h.out.String()) || strings.Contains(h.out.String(), "map[") {
		t.Fatal(h.out)
	}
}

func TestGroupCommandsRefuseTokens(t *testing.T) {
	for _, args := range [][]string{
		{"group", "view", "Acme"},
		{"group", "create", "Builds", "--parent", "Acme"},
		{"group", "edit", "Acme", "--name", "Builds"},
		{"group", "delete", "Acme", "--yes"},
		{"group", "member", "list", "Acme"},
		{"group", "member", "add", "Acme", memberAda},
		{"group", "member", "remove", "Acme", memberAda, "--yes"},
		{"group", "limit", "list", "Acme"},
		{"group", "limit", "set", "Acme", "--max-vcpu", "8"},
		{"group", "limit", "clear", "Acme", "--yes"},
		{"group", "usage", "Acme"},
		{"org", "members", "access", "@me"},
		{"org", "governance"},
	} {
		for _, token := range []string{"avs_fixture_scoped", serviceTokenFixture} {
			h := newManageHarness(t, token, governanceAPI(t, nil))
			if code := h.run(t, args...); code != 4 || len(h.requests) != 0 || !strings.Contains(h.stderr.String(), "needs a signed-in account") {
				t.Fatal(args, token, code, h.operations(), h.stderr)
			}
		}
	}
}

func TestReferenceDescribesArgumentsAndRequiredOptions(t *testing.T) {
	h := newManageHarness(t, managePersonal, governanceAPI(t, nil))
	if code := h.run(t, "reference", "--json", "command,params"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	var rows []struct {
		Command string `json:"command"`
		Params  []struct {
			Name     string `json:"name"`
			Kind     string `json:"kind"`
			Required bool   `json:"required"`
			Nargs    int    `json:"nargs"`
		} `json:"params"`
	}
	if err := json.Unmarshal(h.out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"avr group create":        {"argument name required", "option parent required"},
		"avr group member add":    {"argument group required", "argument member optional variadic"},
		"avr group member remove": {"argument group required", "argument member optional variadic"},
		"avr reference":           {"argument command optional variadic"},
	}
	for _, row := range rows {
		expected, ok := want[row.Command]
		if !ok {
			continue
		}
		var got []string
		for _, param := range row.Params {
			switch {
			case param.Kind == "argument" && param.Nargs == -1:
				got = append(got, "argument "+param.Name+" "+map[bool]string{true: "required", false: "optional"}[param.Required]+" variadic")
			case param.Kind == "argument":
				got = append(got, "argument "+param.Name+" "+map[bool]string{true: "required", false: "optional"}[param.Required])
			case param.Name == "parent" && param.Required:
				got = append(got, "option parent required")
			}
		}
		if slices.Sort(got); !slices.Equal(got, expected) {
			t.Errorf("%s: %q, want %q", row.Command, got, expected)
		}
		delete(want, row.Command)
	}
	if len(want) != 0 {
		t.Fatal("not listed:", want)
	}
}

func TestReferenceListsCommandsBelowAPrefix(t *testing.T) {
	h := newManageHarness(t, managePersonal, governanceAPI(t, nil))
	if code := h.run(t, "reference", "group", "member", "--json", "command"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if got := strings.Join(strings.Fields(h.out.String()), ""); got != `[{"command":"avrgroupmember"},{"command":"avrgroupmemberadd"},{"command":"avrgroupmemberlist"},{"command":"avrgroupmemberremove"}]` {
		t.Fatal(got)
	}
	if len(h.requests) != 0 {
		t.Fatal(h.operations())
	}
	// Aliases name the same commands, as they do on the command line.
	h = newManageHarness(t, managePersonal, governanceAPI(t, nil))
	if code := h.run(t, "reference", "groups", "members", "--json", "command"); code != 0 || !strings.HasPrefix(strings.Join(strings.Fields(h.out.String()), ""), `[{"command":"avrgroupmember"},`) {
		t.Fatal(code, h.out, h.stderr)
	}
	for _, args := range [][]string{{"group", "nope"}, {"groups", "nope"}, {"nope"}, {"internal"}} {
		h = newManageHarness(t, managePersonal, governanceAPI(t, nil))
		if code := h.run(t, append([]string{"reference"}, args...)...); code != 2 || !strings.Contains(h.stderr.String(), "No such command: avr "+strings.Join(args, " ")) {
			t.Fatal(args, code, h.stderr)
		}
	}
}

// The reference starts with avr itself, whose row lists its own options and
// marks those every command takes; a prefix lists commands only.
func TestReferenceListsGlobalOptions(t *testing.T) {
	var rows []struct {
		Command string `json:"command"`
		Params  []struct {
			Opts      []string `json:"opts"`
			Secondary []string `json:"secondary_opts"`
			Inherited *bool    `json:"inherited"`
		} `json:"params"`
	}
	h := newManageHarness(t, managePersonal, governanceAPI(t, nil))
	if code := h.run(t, "reference", "--json", "command,params"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	if err := json.Unmarshal(h.out.Bytes(), &rows); err != nil || len(rows) == 0 || rows[0].Command != "avr" {
		t.Fatal(err, h.out)
	}
	var opts []string
	for _, param := range rows[0].Params {
		opts = append(append(opts, param.Opts...), param.Secondary...)
		// What the row says a command inherits, a command accepts.
		if param.Inherited == nil {
			t.Fatal("no inherited mark on", param.Opts)
		}
		d, _, stderr := testDeps(t)
		code := Execute(t.Context(), []string{"group", "list", param.Opts[0], "--help"}, d)
		if accepted := code == 0; accepted != *param.Inherited {
			t.Errorf("%s: inherited %v, but group list exits %d: %s", param.Opts[0], *param.Inherited, code, stderr)
		}
	}
	for _, want := range []string{"--verbose", "--no-color", "--links", "--no-links", "--version"} {
		if !slices.Contains(opts, want) {
			t.Errorf("missing %s in %v", want, opts)
		}
	}
	h = newManageHarness(t, managePersonal, governanceAPI(t, nil))
	if code := h.run(t, "reference", "group", "--json", "command"); code != 0 || strings.Contains(h.out.String(), `"command": "avr"`) {
		t.Fatal(code, h.out)
	}
}

// Every command's reference fields are exactly what its --json accepts, so
// an agent reading the reference never asks for a field the command rejects.
func TestReferenceJSONFieldsMatchEachCommand(t *testing.T) {
	d, out, stderr := testDeps(t)
	if code := Execute(t.Context(), []string{"reference", "--json", "command,json_fields,params"}, d); code != 0 {
		t.Fatal(code, stderr)
	}
	var rows []struct {
		Command    string   `json:"command"`
		JSONFields []string `json:"json_fields"`
		Params     []struct {
			Name     string   `json:"name"`
			Kind     string   `json:"kind"`
			Opts     []string `json:"opts"`
			Required bool     `json:"required"`
			IsFlag   bool     `json:"is_flag"`
		} `json:"params"`
	}
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, row := range rows {
		// A bare --json prints the whole record and selects no fields.
		selects := slices.ContainsFunc(row.Params, func(param struct {
			Name     string   `json:"name"`
			Kind     string   `json:"kind"`
			Opts     []string `json:"opts"`
			Required bool     `json:"required"`
			IsFlag   bool     `json:"is_flag"`
		}) bool {
			return slices.Contains(param.Opts, "--json") && !param.IsFlag
		})
		if !selects {
			if len(row.JSONFields) > 0 {
				t.Errorf("%s lists JSON fields but selects none", row.Command)
			}
			continue
		}
		if referenceVaryingFields[row.Command] {
			continue
		}
		t.Run(row.Command, func(t *testing.T) {
			args := strings.Fields(row.Command)[1:]
			for _, param := range row.Params {
				switch {
				case !param.Required:
				case param.Kind == "argument":
					args = append(args, cmp.Or(referencePlaceholders[param.Name], "x"))
				case len(param.Opts) > 0:
					args = append(args, param.Opts[len(param.Opts)-1], cmp.Or(referencePlaceholders[param.Name], "1"))
				}
			}
			args = append(args, referenceArguments[row.Command]...)
			d, out, stderr := testDeps(t)
			if code := Execute(t.Context(), append(args, "--json", "?"), d); code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			var listed []string
			for _, line := range strings.Split(out.String(), "\n")[1:] {
				if field := strings.TrimSpace(line); field != "" {
					listed = append(listed, field)
				}
			}
			if !slices.Equal(slices.Sorted(slices.Values(listed)), slices.Sorted(slices.Values(row.JSONFields))) {
				t.Fatalf("--json ? lists %v; reference lists %v", listed, row.JSONFields)
			}
		})
		checked++
	}
	if checked < 80 {
		t.Fatalf("checked %d commands", checked)
	}
}

// referencePlaceholders stand in for required arguments that are checked
// before --json ? answers; referenceArguments add the options and optional
// arguments a command needs by then.
var (
	referencePlaceholders = map[string]string{"number": "1", "token_id": "key-" + strings.Repeat("0", 32), "metadata": "-", "project_selection": "all", "state": "on"}
	// referenceVaryingFields select fields that depend on the credential or
	// platform, so their help names each set and the reference lists none.
	referenceVaryingFields = map[string]bool{"avr auth status": true, "avr org install add": true}
	referenceArguments     = map[string][]string{
		"avr service-token create": {"--all-firewall-rules"},
		"avr group create":         {"--parent", "Acme"},
		"avr group edit":           {"--name", "x"},
		"avr group member add":     {"@me"},
		"avr group member remove":  {"@me"},
	}
)

// Each group, member and governance command reaches only operations the
// schema defines, in order, with the query parameters it documents.
func TestAPIContractGovernanceChains(t *testing.T) {
	document := loadContractDocument(t)
	const org = "/orgs/{org_id}"
	const group = org + "/groups/{group_id}"
	listing := []string{"GET " + org + "/groups", "GET " + org + "/groups", "GET " + org + "/groups"}
	chain := func(operations ...string) []string { return append(slices.Clone(listing), operations...) }
	for _, tc := range []struct {
		args       []string
		operations []string
	}{
		{[]string{"group", "view", "Acme/Platform"}, chain()},
		{[]string{"group", "view", groupDeleted}, chain("GET " + group)},
		{[]string{"group", "create", "Builds", "--parent", "Acme/Platform"}, chain("POST " + org + "/groups")},
		{[]string{"group", "edit", groupPlatform, "--label", ""}, []string{"PATCH " + group}},
		{[]string{"group", "delete", groupPlatform, "--yes"}, []string{"DELETE " + group}},
		{[]string{"group", "member", "list", groupPlatform, "-L", "2"}, []string{"GET " + group + "/members"}},
		{[]string{"group", "member", "add", groupPlatform, "@me", "ada@example.test"}, []string{"GET /users/me", "POST " + org + "/member-lookups", "POST " + group + "/members/bulk"}},
		{[]string{"group", "member", "remove", groupPlatform, memberAda, "--yes"}, []string{"POST " + group + "/members/bulk"}},
		{[]string{"group", "limit", "list", groupPlatform}, []string{"GET " + group + "/limits"}},
		{[]string{"group", "limit", "set", groupPlatform, "--max-vcpu", "8", "--yes"}, []string{"GET " + group + "/limits", "PUT " + group + "/limits/{limit_key}"}},
		{[]string{"group", "limit", "clear", groupPlatform, "--yes"}, []string{"DELETE " + group + "/limits/{limit_key}"}},
		{[]string{"group", "usage", groupPlatform}, []string{"GET " + group + "/usage"}},
		{[]string{"org", "members", "access", "Ada"}, []string{"GET " + org + "/members", "GET " + org + "/members/{user_id}/effective-access"}},
		{[]string{"org", "governance"}, []string{"GET " + org + "/governance"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			api := governanceAPI(t, func(r manageRequest) (manageReply, bool) {
				switch {
				case r.method == "POST" && r.path == membersBulk:
					return bulkReply(t, r, nil), true
				case r.method == "GET" && r.path == orgMembers:
					return manageReply{200, nil, manageJSON(t, map[string]any{"data": []any{orgMember(memberAda, "Ada", "active")}})}, true
				case r.method == "GET" && r.path == platformRoute+"/limits":
					return manageReply{200, nil, `{"data":[]}`}, true
				case r.method == "GET" && r.path == platformRoute+"/members":
					return manageReply{200, nil, `{"data":[],"pagination":{"next_cursor":null}}`}, true
				case r.method == "DELETE":
					return manageReply{204, nil, ""}, true
				case r.method == "GET" && r.path == groupsPath:
					return manageReply{}, false
				case r.method == "GET" && r.path == "/users/me", r.method == "POST" && r.path == memberLookups:
					return manageReply{}, false
				}
				return manageReply{200, nil, manageJSON(t, map[string]any{"data": groupRecord(groupPlatform, groupAcme, "Platform")})}, true
			})
			var operations []string
			h := newManageHarness(t, managePersonal, func(r manageRequest) manageReply {
				request := httptest.NewRequestWithContext(t.Context(), r.method, r.path+"?"+r.query.Encode(), nil)
				operation, err := document.checkRequest(request)
				if err != nil {
					t.Error(err)
				}
				operations = append(operations, operation)
				return api(r)
			})
			if code := h.run(t, tc.args...); code != 0 || !slices.Equal(operations, tc.operations) {
				t.Fatalf("code=%d operations=%v: %s", code, operations, h.stderr)
			}
		})
	}
}
