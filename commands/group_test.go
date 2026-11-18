package commands

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const (
	groupAcme     = "grp-" + "019a0000000070008000000000000021"
	groupPlatform = "grp-" + "019a0000000070008000000000000022"
	groupPlatCI   = "grp-" + "019a0000000070008000000000000023"
	groupData     = "grp-" + "019a0000000070008000000000000024"
	groupDataCI   = "grp-" + "019a0000000070008000000000000025"
	groupTools    = "grp-" + "019a0000000070008000000000000026"
	groupsPath    = "/orgs/" + inspectionOrg + "/groups"
)

func groupRecord(id, parent, name string) map[string]any {
	record := map[string]any{
		"group_id": id, "parent_id": nil, "display_name": name, "label": nil, "ownership": "avrea",
		"state": "active", "suspension_reason": nil, "soft_deleted_at": nil, "member_count": 2,
		"created_at": "2026-11-01T10:00:00Z", "updated_at": "2026-11-01T10:00:00Z",
	}
	if parent != "" {
		record["parent_id"] = parent
	}
	return record
}

// groupTree lists children before their parents, so output order cannot come
// from the response order.
func groupTree() []map[string]any {
	return []map[string]any{
		groupRecord(groupTools, groupAcme, "Platform-tools"),
		groupRecord(groupPlatCI, groupPlatform, "CI"),
		groupRecord(groupDataCI, groupData, "CI"),
		groupRecord(groupPlatform, groupAcme, "Platform"),
		groupRecord(groupData, groupAcme, "Data"),
		groupRecord(groupAcme, "", "Acme"),
	}
}

// groupPages serves rows two per page and answers the service-token routes.
func groupPages(t *testing.T, rows []map[string]any) func(manageRequest) manageReply {
	routes := manageRoutes(t, nil)
	return func(r manageRequest) manageReply {
		switch {
		case r.method == "GET" && r.path == groupsPath:
			if r.query.Get("include_deleted") != "" {
				t.Errorf("include_deleted sent: %v", r.query)
			}
			start := 0
			if cursor := r.query.Get("cursor"); cursor != "" {
				start = int(cursor[0] - '0')
			}
			end := min(start+2, len(rows))
			pagination := map[string]any{"next_cursor": nil}
			if end < len(rows) {
				pagination["next_cursor"] = string(rune('0' + end))
			}
			return manageReply{200, nil, manageJSON(t, map[string]any{"data": rows[start:end], "pagination": pagination})}
		case r.method == "GET" && r.path == manageBase:
			return manageReply{200, nil, `{"data":[]}`}
		}
		return routes(r)
	}
}

func groupLookups(h *manageHarness) int {
	return len(slices.DeleteFunc(h.operations(), func(op string) bool { return op != "GET "+groupsPath }))
}

func TestServiceTokenGroupIDSkipsGroupLookup(t *testing.T) {
	for _, args := range [][]string{
		{"service-token", "create", "--name", "ci", "--group", manageGroupID, "--repo", manageRepoA},
		{"service-token", "list", "--group", manageGroupID},
	} {
		h := newManageHarness(t, managePersonal, groupPages(t, groupTree()))
		if code := h.run(t, args...); code != 0 || groupLookups(h) != 0 {
			t.Fatal(args, code, h.operations(), h.stderr)
		}
		last := h.requests[len(h.requests)-1]
		if last.body["group_id"] != manageGroupID && last.query.Get("group_id") != "eq."+manageGroupID {
			t.Fatal(args, last)
		}
	}
}

func TestServiceTokenGroupResolvesNameOrPath(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"Platform", groupPlatform},
		{"Platform-tools", groupTools},
		{"Platform/CI", groupPlatCI},
		{"Acme/Data/CI", groupDataCI},
		{"Acme", groupAcme},
	} {
		t.Run(tc.value, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, groupPages(t, groupTree()))
			if code := h.run(t, "service-token", "create", "--name", "ci", "--group", tc.value, "--repo", manageRepoA); code != 0 {
				t.Fatal(code, h.stderr)
			}
			// Six groups at two per page: every page is read before the create.
			want := []string{"GET " + groupsPath, "GET " + groupsPath, "GET " + groupsPath, "POST " + manageBase}
			if !slices.Equal(h.operations(), want) {
				t.Fatal(h.operations())
			}
			if got := h.requests[3].body["group_id"]; got != tc.want {
				t.Fatalf("group_id %v, want %s", got, tc.want)
			}
			if h.requests[0].query.Get("limit") != "250" || h.requests[0].query.Has("cursor") || h.requests[1].query.Get("cursor") != "2" || h.requests[2].query.Get("cursor") != "4" {
				t.Fatal(h.requests[:3])
			}

			h = newManageHarness(t, managePersonal, groupPages(t, groupTree()))
			if code := h.run(t, "service-token", "list", "--group", tc.value); code != 0 {
				t.Fatal(code, h.stderr)
			}
			last := h.requests[len(h.requests)-1]
			if last.path != manageBase || last.query.Get("group_id") != "eq."+tc.want {
				t.Fatal(last)
			}
		})
	}
}

func TestServiceTokenGroupMatchingRules(t *testing.T) {
	nested := "grp-" + "019a0000000070008000000000000027"
	deleted := groupRecord("grp-"+"019a0000000070008000000000000028", groupAcme, "Tools")
	deleted["soft_deleted_at"] = "2026-11-02T10:00:00Z"
	rows := append(groupTree(), groupRecord(nested, groupData, "Acme"), groupRecord(groupTools+"x", groupData, "Tools"), deleted)
	for _, tc := range []struct{ name, value, want string }{
		// No longer path names the root, so its whole path wins over
		// Acme/Data/Acme, which only ends with the name.
		{"whole path over suffix", "Acme", groupAcme},
		{"case does not matter", "platform/ci", groupPlatCI},
		// Without the deleted Acme/Tools this name has one match.
		{"deleted groups are left out", "Tools", groupTools + "x"},
		// Spaces and slashes around a path are not part of it.
		{"surrounding spaces and slashes", " /Platform/CI/ ", groupPlatCI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, groupPages(t, rows))
			if code := h.run(t, "service-token", "create", "--name", "ci", "--group", tc.value, "--repo", manageRepoA); code != 0 {
				t.Fatal(code, h.stderr)
			}
			if got := h.requests[len(h.requests)-1].body["group_id"]; got != tc.want {
				t.Fatalf("group_id %v, want %s", got, tc.want)
			}
		})
	}
}

func TestServiceTokenGroupNoMatch(t *testing.T) {
	for _, args := range [][]string{
		{"service-token", "create", "--name", "ci", "--group", "Platform/Nope", "--repo", manageRepoA},
		{"service-token", "list", "--group", "Platform/Nope"},
	} {
		h := newManageHarness(t, managePersonal, groupPages(t, groupTree()))
		code := h.run(t, args...)
		message := h.stderr.String()
		if code != 2 || groupLookups(h) != len(h.requests) || !strings.Contains(message, "no group is named 'Platform/Nope' or has that path") || !strings.Contains(message, "avr group list --org "+inspectionOrg) {
			t.Fatal(args, code, h.operations(), message)
		}
	}
	// A partial name is not a path suffix.
	h := newManageHarness(t, managePersonal, groupPages(t, groupTree()))
	if code := h.run(t, "service-token", "list", "--group", "form/CI"); code != 2 {
		t.Fatal(code, h.stderr)
	}
}

func TestServiceTokenGroupAmbiguousListsCandidates(t *testing.T) {
	h := newManageHarness(t, managePersonal, groupPages(t, groupTree()))
	code := h.run(t, "service-token", "create", "--name", "ci", "--group", "CI", "--repo", manageRepoA)
	message := h.stderr.String()
	want := "'CI' matches 2 groups:\n    Acme/Data/CI  (" + groupDataCI + ")\n    Acme/Platform/CI  (" + groupPlatCI + ")\n  Hint: Pass a longer path or the group ID."
	if code != 2 || groupLookups(h) != len(h.requests) || !strings.Contains(message, want) {
		t.Fatal(code, h.operations(), message)
	}
}

func TestServiceTokenGroupNameIsSanitized(t *testing.T) {
	h := newManageHarness(t, managePersonal, groupPages(t, groupTree()))
	if code := h.run(t, "service-token", "list", "--group", "a\x1b[31mb\nc"); code != 2 || strings.ContainsAny(h.stderr.String(), "\x1b") || strings.Contains(h.stderr.String(), "b\nc") {
		t.Fatalf("%d %q", code, h.stderr)
	}
}

func TestGroupListOrdersByPath(t *testing.T) {
	order := []string{"Acme", "Acme/Data", "Acme/Data/CI", "Acme/Platform", "Acme/Platform/CI", "Acme/Platform-tools"}
	t.Run("piped", func(t *testing.T) {
		h := newManageHarness(t, managePersonal, groupPages(t, groupTree()))
		if code := h.run(t, "group", "list"); code != 0 {
			t.Fatal(code, h.stderr)
		}
		lines := strings.Split(strings.TrimSpace(h.out.String()), "\n")
		if len(lines) != 7 || lines[0] != "group_id\tpath\tlabel\tstate\tmember_count" || lines[5] != groupPlatCI+"\tAcme/Platform/CI\t\tactive\t2" {
			t.Fatal(h.out)
		}
		for i, path := range order {
			if fields := strings.Split(lines[i+1], "\t"); fields[1] != path {
				t.Errorf("row %d: %q, want %q", i, fields[1], path)
			}
		}
		if h.stderr.Len() != 0 {
			t.Fatal(h.stderr)
		}
	})
	t.Run("terminal", func(t *testing.T) {
		h := newManageHarness(t, managePersonal, groupPages(t, groupTree()))
		h.d.IsTerminal = func() bool { return true }
		if code := h.run(t, "groups", "list"); code != 0 {
			t.Fatal(code, h.stderr)
		}
		text := h.out.String()
		for _, want := range []string{"Group ID", "Path", "Label", "State", "Members", groupTools} {
			if !strings.Contains(text, want) {
				t.Errorf("missing %q in\n%s", want, text)
			}
		}
		last := -1
		for _, path := range order {
			index := strings.Index(text, path+" ")
			if index <= last {
				t.Fatalf("%q out of order in\n%s", path, text)
			}
			last = index
		}
	})
	t.Run("json", func(t *testing.T) {
		h := newManageHarness(t, managePersonal, groupPages(t, groupTree()))
		if code := h.run(t, "group", "list", "--json", "group_id,path,parent_id"); code != 0 {
			t.Fatal(code, h.stderr)
		}
		var rows []map[string]any
		if err := json.Unmarshal(h.out.Bytes(), &rows); err != nil || len(rows) != 6 {
			t.Fatal(err, h.out)
		}
		for i, path := range order {
			if rows[i]["path"] != path {
				t.Errorf("row %d: %v, want %q", i, rows[i], path)
			}
		}
		if rows[0]["group_id"] != groupAcme || rows[0]["parent_id"] != nil || rows[4]["parent_id"] != groupPlatform {
			t.Fatal(rows)
		}
	})
}

// The response lists children before their parents, so the groups shown under
// a limit get their full paths only when every page is read first.
func TestGroupListLimitKeepsFullPaths(t *testing.T) {
	h := newManageHarness(t, managePersonal, groupPages(t, groupTree()))
	if code := h.run(t, "group", "list", "-L", "5", "--json", "path"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &rows); err != nil {
		t.Fatal(err, h.out)
	}
	paths := []string{}
	for _, row := range rows {
		paths = append(paths, row["path"].(string))
	}
	if !slices.Equal(paths, []string{"Acme", "Acme/Data", "Acme/Data/CI", "Acme/Platform", "Acme/Platform/CI"}) {
		t.Fatal(paths)
	}
	if len(h.requests) != 3 {
		t.Fatal(h.operations())
	}
	for _, r := range h.requests {
		if r.query.Get("limit") != "250" {
			t.Fatal(r.query)
		}
	}
	if got := h.stderr.String(); got != "\nShowing the first 5 of 6 groups. Raise --limit to see the rest.\n" {
		t.Fatalf("%q", got)
	}
	h = newManageHarness(t, managePersonal, groupPages(t, groupTree()))
	if code := h.run(t, "group", "list", "-L", "6"); code != 0 || h.stderr.Len() != 0 {
		t.Fatal(code, h.stderr)
	}
}

func TestGroupsNotAvailable(t *testing.T) {
	for _, args := range [][]string{
		{"group", "list"},
		{"service-token", "create", "--name", "ci", "--group", "CI", "--repo", manageRepoA},
		{"service-token", "list", "--group", "CI"},
	} {
		h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { return manageReply{404, nil, `{"detail":"Not Found"}`} })
		code := h.run(t, args...)
		message := h.stderr.String()
		if code != 1 || len(h.requests) != 1 || !strings.Contains(message, "Groups are not available for this organization (HTTP 404)") || strings.Contains(message, "Not found while trying") {
			t.Fatal(args, code, message)
		}
	}
}

func TestGroupListRejectsBeforeRequests(t *testing.T) {
	for _, args := range [][]string{{"-L", "0"}, {"-L", "1001"}, {"--jq", ".[]"}} {
		h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { t.Error("request sent"); return manageReply{599, nil, "{}"} })
		if code := h.run(t, append([]string{"group", "list"}, args...)...); code != 2 || len(h.requests) != 0 {
			t.Fatal(args, code, h.stderr)
		}
	}
	for _, token := range []string{"avs_fixture_scoped", serviceTokenFixture} {
		h := newManageHarness(t, token, func(manageRequest) manageReply { t.Error("request sent"); return manageReply{599, nil, "{}"} })
		if code := h.run(t, "group", "list"); code != 4 || !strings.Contains(h.stderr.String(), "Listing groups needs a signed-in account") {
			t.Fatal(token, code, h.stderr)
		}
	}
}
