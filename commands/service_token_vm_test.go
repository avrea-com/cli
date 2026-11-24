package commands

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

const (
	manageVMA = "cvm-" + "019a0000000070008000000000000021"
	manageVMB = "cvm-" + "019a0000000070008000000000000022"
)

func manageVMGrant(id any, level string) map[string]any {
	grant := manageGrant("customer_vm", id)
	grant["access_level"] = level
	return grant
}

// checkVMCap fails unless body carries vm_cap exactly when set, with want.
func checkVMCap(t *testing.T, body map[string]any, want any, set bool) {
	t.Helper()
	if got, present := body["vm_cap"]; present != set || got != want {
		t.Fatalf("vm_cap %#v (present %v)", got, present)
	}
}

func TestServiceTokenCreateSendsVMAccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want map[string]any
	}{
		{"grants in flag order after rules", []string{"--repo", manageRepoA, "--vm", manageVMA + ":WRITE", "--firewall-rule", manageRuleA, "--vm", manageVMB, "--vm-create-limit", "5"},
			map[string]any{"vm_cap": float64(5), "grants": []any{manageGrant("repository", manageRepoA), manageGrant("egress_firewall_rule", manageRuleA), manageVMGrant(manageVMA, "write"), manageVMGrant(manageVMB, "read")}}},
		// A repeated grant at the same level is one grant.
		{"every VM after named ones", []string{"--vm", manageVMA + ":write", "--vm", manageVMA + ":write", "--all-vms", "read"},
			map[string]any{"grants": []any{manageVMGrant(manageVMA, "write"), manageVMGrant(nil, "read")}}},
		{"only the VM limit", []string{"--vm-create-limit", "3"}, map[string]any{"vm_cap": float64(3), "grants": []any{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, manageRoutes(t, nil))
			if code := h.run(t, append([]string{"service-token", "create", "--name", "ci", "--group", manageGroupID}, tc.args...)...); code != 0 {
				t.Fatal(code, h.stderr)
			}
			body := h.requests[len(h.requests)-1].body
			cap, set := tc.want["vm_cap"]
			checkVMCap(t, body, cap, set)
			if manageJSON(t, body["grants"]) != manageJSON(t, tc.want["grants"]) {
				t.Fatal(body["grants"])
			}
		})
	}
}

func TestServiceTokenVMOptionsRejectedBeforeRequests(t *testing.T) {
	tooMany := []string{"--all-vms", "read"}
	for i := range 100 {
		tooMany = append(tooMany, "--firewall-rule", fmt.Sprintf("efr-"+"019a0000000070008000%012x", i))
	}
	create := []string{"create", "--name", "ci", "--group", manageGroupID}
	replace := []string{"set-access", manageTokenID}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{append(slices.Clone(create), "--vm", "vm-1"), "Invalid --vm value 'vm-1': expected a VM ID, cvm- followed by 32"},
		{append(slices.Clone(create), "--vm", manageVMA+":admin"), "VM grants take read or write. Use --vm " + manageVMA + ", --vm " + manageVMA + ":read or --vm " + manageVMA + ":write."},
		{append(slices.Clone(create), "--vm", ":write"), "Use --vm ID, --vm ID:read or --vm ID:write."},
		{append(slices.Clone(create), "--vm", manageVMA, "--vm", manageVMA+":write"), "--vm names " + manageVMA + " at both read and write"},
		{append(slices.Clone(create), "--all-vms", "admin"), "Invalid value for '--all-vms': 'admin' is not one of read, write"},
		{append(slices.Clone(create), "--all-vms", "read", "--vm", manageVMA), "--all-vms read already grants every VM at read; drop --vm " + manageVMA},
		{append(slices.Clone(create), "--all-vms", "write", "--vm", manageVMB+":write"), "--all-vms write already grants every VM at write"},
		{append(slices.Clone(create), "--vm-create-limit", "0"), "--vm-create-limit must be between 1 and 2147483647"},
		{append(slices.Clone(create), "--vm-create-limit", "2147483648"), "--vm-create-limit must be between 1 and 2147483647"},
		{append(slices.Clone(create), tooMany...), "at most 100 grants (got 101)"},
		{append(slices.Clone(create), "--no-vm-create", "--repo", manageRepoA), "unknown flag: --no-vm-create"},
		{append(slices.Clone(replace), "--vm-create-limit", "2", "--no-vm-create"), "--vm-create-limit and --no-vm-create are mutually exclusive"},
	} {
		h := newManageHarness(t, managePersonal, func(manageRequest) manageReply { t.Error("request sent"); return manageReply{599, nil, "{}"} })
		code := h.run(t, append([]string{"service-token"}, tc.args...)...)
		if code != 2 || len(h.requests) != 0 || !strings.Contains(h.stderr.String(), tc.want) {
			t.Fatal(tc.args, code, h.stderr)
		}
	}
}

// A token of the root group cannot create VMs; the API says so about vm_cap,
// which no grants[N] hint explains. Each command says what it can change.
func TestServiceTokenRootOwnedVMLimit(t *testing.T) {
	refusal := manageReply{422, nil, `{"detail":[{"type":"root_owned","loc":["body","vm_cap"],"msg":"A root-owned token cannot create VMs"}]}`}
	routes := manageRoutes(t, manageRecord(nil))
	for _, tc := range []struct {
		args       []string
		hint       string
		otherwise  string
		refusedFor string
	}{
		{[]string{"create", "--name", "ci", "--group", manageGroupID, "--vm-create-limit", "2"}, "Pass a group below it to --group", "Leave out --vm-create-limit", "POST"},
		{[]string{"set-access", manageTokenID, "--repo", manageRepoA, "--vm-create-limit", "2"}, "Leave out --vm-create-limit, or create a token in a group below the root group", "--group", "PUT"},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			h := newManageHarness(t, managePersonal, func(r manageRequest) manageReply {
				if r.method == tc.refusedFor {
					return refusal
				}
				return routes(r)
			})
			code := h.run(t, append([]string{"service-token"}, tc.args...)...)
			message := h.stderr.String()
			if code != 1 || !strings.Contains(message, "A root-owned token cannot create VMs") || !strings.Contains(message, tc.hint) || strings.Contains(message, tc.otherwise) || strings.Contains(message, "grants[N]") {
				t.Fatal(code, message)
			}
		})
	}
}

func TestServiceTokenSetAccessVMLimit(t *testing.T) {
	current := manageRecord(map[string]any{"vm_cap": 3})
	for _, tc := range []struct {
		name string
		args []string
		cap  any
		set  bool
	}{
		{"omitted keeps the limit", nil, nil, false},
		{"a new limit", []string{"--vm-create-limit", "4"}, float64(4), true},
		{"withdrawn", []string{"--no-vm-create", "--yes"}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, manageRoutes(t, current))
			if code := h.run(t, append([]string{"service-token", "set-access", manageTokenID, "--repo", manageRepoA}, tc.args...)...); code != 0 {
				t.Fatal(code, h.stderr)
			}
			checkVMCap(t, h.requests[len(h.requests)-1].body, tc.cap, tc.set)
		})
	}
}

func TestServiceTokenSetAccessListsRemovals(t *testing.T) {
	current := manageRecord(map[string]any{"vm_cap": 5, "grants": []any{
		manageGrant("repository", manageRepoA), manageVMGrant(manageVMA, "write"), manageGrant("egress_firewall_rule", nil),
	}})
	keep := []string{"--repo", manageRepoA, "--all-firewall-rules"}
	for _, tc := range []struct {
		name    string
		args    []string
		removed []string
	}{
		{"everything kept", append(slices.Clone(keep), "--vm", manageVMA+":write"), nil},
		{"covered by every VM", append(slices.Clone(keep), "--all-vms", "write"), nil},
		{"a higher limit", append(slices.Clone(keep), "--vm", manageVMA+":write", "--vm-create-limit", "9"), nil},
		{"a lowered VM", append(slices.Clone(keep), "--vm", manageVMA), []string{"VM " + manageVMA + " lowered from write to read"}},
		{"every VM at read does not cover write", append(slices.Clone(keep), "--all-vms", "read"), []string{"VM " + manageVMA + " at write"}},
		{"a dropped repository and rule", []string{"--vm", manageVMA + ":write"}, []string{"repository " + manageRepoA + " at read", "every firewall rule at read"}},
		{"a lowered limit", append(slices.Clone(keep), "--vm", manageVMA+":write", "--vm-create-limit", "2"), []string{"VM cap lowered from 5 to 2"}},
		{"a withdrawn limit", append(slices.Clone(keep), "--vm", manageVMA+":write", "--no-vm-create"), []string{"permission to create VMs (up to 5)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, manageRoutes(t, current))
			code := h.run(t, append([]string{"service-token", "set-access", manageTokenID}, tc.args...)...)
			message := h.stderr.String()
			replaced := slices.Contains(h.operations(), "PUT "+manageBase+"/"+manageTokenID+"/grants")
			if len(tc.removed) == 0 {
				if code != 0 || !replaced {
					t.Fatal(code, message)
				}
				return
			}
			// Without a terminal nobody can answer, so the command refuses.
			if code != 1 || replaced || h.out.Len() != 0 || !strings.Contains(message, "or pass --yes to apply the change") {
				t.Fatal(code, h.operations(), message)
			}
			for _, want := range tc.removed {
				if !strings.Contains(message, "\n    "+want+"\n") {
					t.Errorf("missing %q in %q", want, message)
				}
			}
		})
	}
}

func TestServiceTokenSetAccessConfirmsRemovals(t *testing.T) {
	const question = "This replacement removes access service token " + manageTokenID + " holds:\n    repository " + manageRepoA + " at read\nReplace its access? [y/N]"
	for _, tc := range []struct {
		name, input        string
		terminal, stderrOK bool
		args               []string
		env                map[string]string
		prompted, replaced bool
	}{
		{"declined", "n\n", true, true, nil, nil, true, false},
		{"no answer", "", true, true, nil, nil, true, false},
		{"accepted", "y\n", true, true, nil, nil, true, true},
		{"--yes", "", true, true, []string{"--yes"}, nil, false, true},
		{"-y without a terminal", "", false, false, []string{"-y"}, nil, false, true},
		{"not a terminal", "y\n", false, true, nil, nil, false, false},
		// A question in a redirected log would block the run unseen.
		{"stderr redirected", "y\n", true, false, nil, nil, false, false},
		{"prompts disabled", "y\n", true, true, nil, map[string]string{"AVR_PROMPT_DISABLED": "1"}, false, false},
		{"prompts disabled with --yes", "", true, true, []string{"--yes"}, map[string]string{"AVR_PROMPT_DISABLED": "1"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, manageRoutes(t, manageRecord(nil)))
			h.d.In = strings.NewReader(tc.input)
			h.d.IsInputTerminal = func() bool { return tc.terminal }
			h.d.IsErrorTerminal = func() bool { return tc.stderrOK }
			h.d.Getenv = func(key string) string { return tc.env[key] }
			code := h.run(t, append([]string{"service-token", "set-access", manageTokenID, "--repo", manageRepoB}, tc.args...)...)
			message := h.stderr.String()
			// The question goes to stderr so stdout stays the record.
			if strings.Contains(message, question) != tc.prompted || strings.Contains(h.out.String(), "Replace its access?") {
				t.Fatalf("prompted: stdout %q stderr %q", h.out, message)
			}
			replaced := slices.Contains(h.operations(), "PUT "+manageBase+"/"+manageTokenID+"/grants")
			if replaced != tc.replaced || (code == 0) != tc.replaced {
				t.Fatal(code, h.operations(), message)
			}
			// A refusal without a question still names what would go.
			if !tc.replaced && (h.out.Len() != 0 || !strings.Contains(message, "\n    repository "+manageRepoA+" at read\n")) {
				t.Fatal(h.out, message)
			}
		})
	}
}

func TestServiceTokenViewShowsVMAccess(t *testing.T) {
	grants := []any{manageVMGrant(manageVMA, "write"), manageVMGrant(nil, "read")}
	for _, tc := range []struct {
		name   string
		record map[string]any
		want   []string
	}{
		{"creates VMs", manageRecord(map[string]any{"vm_cap": 5, "vm_count": 2, "grants": grants}),
			[]string{"VM create         allowed, 2 of 5 used", "| VM   | " + manageVMA + " | write |", "| VM   | every VM                             | read  |"}},
		{"limit withdrawn", manageRecord(map[string]any{"vm_cap": nil, "vm_count": 1}), []string{"VM create         not allowed, 1 still owned"}},
		{"never created", manageRecord(nil), []string{"VM create         not allowed\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newManageHarness(t, managePersonal, manageRoutes(t, tc.record))
			if code := h.run(t, "service-token", "view", manageTokenID); code != 0 {
				t.Fatal(code, h.stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(h.out.String(), want) {
					t.Errorf("missing %q in:\n%s", want, h.out)
				}
			}
		})
	}
	h := newManageHarness(t, managePersonal, manageRoutes(t, manageRecord(map[string]any{"vm_cap": 5, "vm_count": 2})))
	if code := h.run(t, "service-token", "view", manageTokenID, "--json", "vm_cap,vm_count"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	var record map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &record); err != nil || record["vm_cap"] != float64(5) || record["vm_count"] != float64(2) {
		t.Fatal(err, h.out)
	}
}

func TestTokenGrantsSummaryCountsServiceTokenVMs(t *testing.T) {
	for _, tc := range []struct {
		row  map[string]any
		want string
	}{
		{map[string]any{"vm_cap": float64(5), "vm_count": float64(2)}, "VM create 2/5"},
		{map[string]any{"vm_cap": float64(5)}, "VM create 0/5"},
		{map[string]any{"grants": []any{manageVMGrant(manageVMA, "write")}, "vm_cap": nil, "vm_count": float64(1)}, "1 VM"},
	} {
		if got := tokenGrantsSummary(tc.row); got != tc.want {
			t.Errorf("%v: %q != %q", tc.row, got, tc.want)
		}
	}
}

func TestSplitTokenLevel(t *testing.T) {
	for _, tc := range []struct {
		spec, id, level string
	}{
		{manageVMA, manageVMA, "read"},
		{manageVMA + ":read", manageVMA, "read"},
		{manageVMA + ":Write", manageVMA, "write"},
	} {
		id, level, err := splitTokenLevel(tc.spec, "VM", "--vm", serviceTokenLevels...)
		if err != nil || id != tc.id || level != tc.level {
			t.Errorf("%s: %q %q %v", tc.spec, id, level, err)
		}
	}
	// A repository keeps its one-level message.
	if _, err := stripTokenLevel("acme/api:write", "repository", "--repo", "read"); err == nil || !strings.Contains(err.Error(), "repository grants are created at the read level. Use --repo acme/api or --repo acme/api:read.") {
		t.Fatal(err)
	}
}

// serviceTokenVMRoutes are the routes the API opens to a service token on a
// deployment that admits service-token VM access, by method, with bodies the
// vm commands parse to completion.
var serviceTokenVMRoutes = func() map[string]string {
	vm := `{"customer_vm_id":"` + manageVMA + `","display_name":"box","state":"RUNNING","os_type":"linux","size":"small","architecture":"x86_64","created_at":"2026-11-16T10:00:00Z"}`
	return map[string]string{
		"GET /service-tokens/self":                              serviceTokenSelf,
		"POST /orgs/{org_id}/vms":                               `{"data":{"vm":` + vm + `,"password":"one-time-fixture"}}`,
		"GET /orgs/{org_id}/vms":                                `{"data":[` + vm + `],"pagination":{"next_cursor":null}}`,
		"GET /orgs/{org_id}/vms/capabilities":                   `{"data":{}}`,
		"GET /orgs/{org_id}/vms/{vm_id}":                        `{"data":` + vm + `}`,
		"GET /orgs/{org_id}/vms/{vm_id}/metrics":                `{"data":{"samples":[]}}`,
		"PATCH /orgs/{org_id}/vms/{vm_id}":                      `{"data":` + vm + `}`,
		"POST /orgs/{org_id}/vms/{vm_id}/pause":                 `{"data":` + vm + `}`,
		"POST /orgs/{org_id}/vms/{vm_id}/https-label-rotations": `{"data":` + vm + `}`,
		"DELETE /orgs/{org_id}/vms/{vm_id}":                     `{"data":` + vm + `}`,
	}
}()

// serviceTokenVMRoute returns the open route a request matches, or "". A
// placeholder never matches the literal "capabilities".
func serviceTokenVMRoute(method, path string) string {
	return matchRoutePattern(slices.Collect(maps.Keys(serviceTokenVMRoutes)), method+" "+path, "capabilities")
}

// The service-token help names the avr vm commands a service token runs.
// Each must reach only the routes open to it, including name lookups.
func TestServiceTokenVMCommands(t *testing.T) {
	for _, args := range [][]string{
		{"vm", "create", "--os", "linux", "--size", "small"},
		{"vm", "list"},
		{"vm", "capabilities"},
		{"vm", "show", manageVMA},
		{"vm", "show", "box"},
		{"vm", "metrics", manageVMA},
		{"vm", "update", manageVMA, "--ttl", "2h"},
		{"vm", "pause", manageVMA},
		{"vm", "rotate-https-label", manageVMA},
		{"vm", "delete", manageVMA, "--yes"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var violations []string
			h := newServiceTokenHarness(t, "", routeModelAPI(serviceTokenVMRoute, serviceTokenVMRoutes, &violations))
			if code := h.run(t, args...); code != 0 || len(violations) != 0 {
				t.Fatal(code, violations, h.requests, h.stderr)
			}
		})
	}
}

// Resume and fork are closed to service tokens, as the help says; the API
// answers 404 and the command explains what a service token reaches.
func TestServiceTokenVMResumeAndForkClosed(t *testing.T) {
	for _, operation := range []string{"resume", "fork"} {
		t.Run(operation, func(t *testing.T) {
			var violations []string
			h := newServiceTokenHarness(t, "", routeModelAPI(serviceTokenVMRoute, serviceTokenVMRoutes, &violations))
			code := h.run(t, "vm", operation, manageVMA)
			want := "POST /orgs/" + inspectionOrg + "/vms/" + manageVMA + "/" + operation
			if code != 1 || !slices.Equal(violations, []string{want}) || !strings.Contains(h.stderr.String(), "a service token reaches only what its access names") {
				t.Fatal(code, violations, h.stderr)
			}
		})
	}
}
