package commands

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

// A service token writes without a user, so the table names its key; a
// person's key still shows the person, and a system write its type.
func TestAuditEventsTableNamesTheActingServiceToken(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	const (
		person = "usr-019a0000000070008000000000000002"
		// Split so the source does not look like a third-party secret.
		token = "key-" + "019a0000000070008000000000000003"
		key   = "key-" + "019a0000000070008000000000000004"
		vm    = "cvm-019a0000000070008000000000000005"
		run   = "run-019a0000000070008000000000000006"
		swept = "run-019a0000000070008000000000000007"
	)
	page := `{"data":[` +
		// Empty IDs name nobody, as null does.
		`{"event_id":4,"created_at":"2026-11-25T09:01:40Z","actor_user_id":"","actor_type":"SYSTEM","acting_api_key_id":"","resource_type":"workflow_run","resource_id":"` + swept + `","action":"UPDATE","event_data":{}},` +
		`{"event_id":3,"created_at":"2026-11-25T09:01:30Z","actor_user_id":null,"actor_type":"API_KEY","acting_api_key_id":"` + token + `","resource_type":"customer_vm","resource_id":"` + vm + `","action":"CREATE","event_data":{}},` +
		`{"event_id":2,"created_at":"2026-11-25T09:01:20Z","actor_user_id":"` + person + `","actor_type":"API_KEY","acting_api_key_id":"` + key + `","resource_type":"service_token","resource_id":"` + token + `","action":"UPDATE","event_data":{}},` +
		`{"event_id":1,"created_at":"2026-11-25T09:01:10Z","actor_user_id":null,"actor_type":"SYSTEM","acting_api_key_id":null,"resource_type":"workflow_run","resource_id":"` + run + `","action":"UPDATE","event_data":{}}` +
		`],"pagination":{"next_cursor":null}}`
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/orgs/"+inspectionOrg+"/audit-events" {
			t.Fatal("unexpected request", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(page)), Request: r}, nil
	})}
	if code := Execute(t.Context(), []string{"audit-events", "list"}, d); code != 0 {
		t.Fatal(code, stderr)
	}
	actors := map[string]string{}
	for _, line := range strings.Split(out.String(), "\n") {
		cells := strings.Split(line, "|")
		if len(cells) > 6 {
			actors[strings.TrimSpace(cells[5])] = strings.TrimSpace(cells[2])
		}
	}
	for target, actor := range map[string]string{vm: token, token: person, run: "SYSTEM", swept: "SYSTEM"} {
		if actors[target] != actor {
			t.Errorf("%s: actor %q, want %q in\n%s", target, actors[target], actor, out)
		}
	}
}
