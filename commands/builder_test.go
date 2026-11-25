package commands

import (
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

const builderSessionRows = `{"data":[{"builder_session_id":"bs-1","platform":"linux/amd64","state":"RUNNING","state_reason":null,"cpu_count":4,"memory_mb":8192,"disk_gb":100,"repository_full_name":"acme/widgets","stop_at":"2026-01-02T03:04:05Z","created_at":"2026-01-02T02:04:05Z","updated_at":"2026-01-02T02:05:05Z"}],"pagination":{"next_cursor":%s}}`

func builderDeps(t *testing.T, respond func(*http.Request) (int, string)) (Dependencies, *strings.Builder, *[]string) {
	t.Helper()
	d, _, _ := testDeps(t)
	out := &strings.Builder{}
	d.Out = out
	requests := &[]string{}
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		*requests = append(*requests, r.Method+" "+r.URL.RequestURI())
		status, body := respond(r)
		return buildJSONResponse(r, status, nil, body), nil
	})}
	return d, out, requests
}

func TestBuilderList(t *testing.T) {
	rows := func(cursor string) func(*http.Request) (int, string) {
		return func(*http.Request) (int, string) { return 200, strings.Replace(builderSessionRows, "%s", cursor, 1) }
	}
	base := "GET /orgs/" + inspectionOrg + "/builder-sessions?"

	d, out, requests := builderDeps(t, rows("null"))
	stderr := &strings.Builder{}
	d.Err = stderr
	if code := Execute(t.Context(), []string{"builders", "list", "--state", "running", "-L", "7"}, d); code != 0 {
		t.Fatal(code, stderr)
	}
	if (*requests)[0] != base+"limit=7&state=RUNNING" {
		t.Fatal(*requests)
	}
	if want := "builder_session_id\tplatform\tstate\tstate_reason\trepository_full_name\tstop_at\tcreated_at\nbs-1\tlinux/amd64\tRUNNING\t\tacme/widgets\t2026-01-02T03:04:05Z\t2026-01-02T02:04:05Z\n"; out.String() != want {
		t.Fatalf("%q", out.String())
	}

	d, out, _ = builderDeps(t, rows(`"next page"`))
	d.IsTerminal = func() bool { return true }
	stderr = &strings.Builder{}
	d.Err = stderr
	if code := Execute(t.Context(), []string{"builder", "list", "--cursor", "abc"}, d); code != 0 {
		t.Fatal(code, stderr)
	}
	for _, text := range []string{"ID", "Stop at", "bs-1", "acme/widgets"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("missing %q in %s", text, out)
		}
	}
	if !strings.Contains(stderr.String(), "More results available. Next page: --cursor 'next page'") {
		t.Fatal(stderr)
	}

	d, out, _ = builderDeps(t, rows("null"))
	if code := Execute(t.Context(), []string{"builder", "list", "--json", "builder_session_id,state", "--jq", ".[0].state"}, d); code != 0 || strings.TrimSpace(out.String()) != `"RUNNING"` && strings.TrimSpace(out.String()) != "RUNNING" {
		t.Fatal(code, out)
	}

	d, out, _ = builderDeps(t, func(*http.Request) (int, string) { return 200, `{"data":[],"pagination":{}}` })
	d.IsTerminal = func() bool { return true }
	if code := Execute(t.Context(), []string{"builder", "list"}, d); code != 0 || out.String() != "No builder sessions found.\n" {
		t.Fatal(code, out)
	}

	for _, args := range [][]string{{"--state", "paused"}, {"--limit", "0"}, {"--limit", "101"}, {"--cursor", ""}, {"--jq", "."}, {"--json", "nope"}} {
		d, _, requests = builderDeps(t, rows("null"))
		if code := Execute(t.Context(), append([]string{"builder", "list"}, args...), d); code == 0 || len(*requests) != 0 {
			t.Errorf("%v: code=%d requests=%v", args, code, *requests)
		}
	}
	d, out, requests = builderDeps(t, rows("null"))
	if code := Execute(t.Context(), []string{"builder", "list", "--json", "?"}, d); code != 0 || len(*requests) != 0 || !strings.Contains(out.String(), "repository_full_name") {
		t.Fatal(code, out)
	}
}

func TestBuilderStop(t *testing.T) {
	path := "DELETE /orgs/" + inspectionOrg + "/builder-sessions/bs-1"
	stopping := func(*http.Request) (int, string) {
		return 200, `{"data":{"builder_session_id":"bs-1","state":"STOPPING"}}`
	}

	d, out, requests := builderDeps(t, stopping)
	if code := Execute(t.Context(), []string{"builder", "stop", "bs-1", "--yes"}, d); code != 0 || out.String() != "Builder session bs-1 is now STOPPING.\n" || (*requests)[0] != path {
		t.Fatal(code, out, *requests)
	}

	// An empty answer declines, and nothing is sent.
	d, out, requests = builderDeps(t, stopping)
	d.In = strings.NewReader("\n")
	if code := Execute(t.Context(), []string{"builder", "stop", "bs-1"}, d); code != 1 || len(*requests) != 0 || !strings.Contains(out.String(), "Stop builder session bs-1? [y/N]: ") {
		t.Fatal(code, out, *requests)
	}
	d, _, requests = builderDeps(t, stopping)
	d.In = strings.NewReader("y\n")
	if code := Execute(t.Context(), []string{"builder", "stop", "bs-1"}, d); code != 0 || len(*requests) != 1 {
		t.Fatal(code, *requests)
	}

	d, out, _ = builderDeps(t, stopping)
	if code := Execute(t.Context(), []string{"builder", "stop", "bs-1", "-y", "--json"}, d); code != 0 || !strings.Contains(out.String(), `"state": "STOPPING"`) {
		t.Fatal(code, out)
	}

	// No body still reports the stop.
	d, out, _ = builderDeps(t, func(*http.Request) (int, string) { return 204, "" })
	if code := Execute(t.Context(), []string{"builder", "stop", "bs-1", "-y"}, d); code != 0 || out.String() != "Builder session bs-1 is now STOPPING.\n" {
		t.Fatal(code, out)
	}

	for status, hint := range map[int]string{409: "Hint: safe to retry; the session's TTL will also reclaim it.", 404: "Hint: Run `avr builder list` to see your sessions."} {
		d, _, _ = builderDeps(t, func(*http.Request) (int, string) { return status, `{"detail":"refused"}` })
		stderr := &strings.Builder{}
		d.Err = stderr
		if code := Execute(t.Context(), []string{"builder", "stop", "bs-1", "-y"}, d); code != 1 || !strings.Contains(stderr.String(), hint) || !strings.Contains(stderr.String(), "refused") {
			t.Fatal(status, code, stderr)
		}
	}
}
