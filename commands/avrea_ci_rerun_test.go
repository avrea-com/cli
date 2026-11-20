package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestAvreaCIRerunReceiptIntegerCoercionAndBounds(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{json.Number("2"), "2"}, {json.Number("2.0"), "2"}, {json.Number("2e0"), "2"},
		{"2", "2"}, {"  +2.00 ", "2"}, {"2_0", "20"}, {"02", "2"},
		{"2e0", ""}, {"2.", ""}, {"2/1", ""}, {"2__0", ""}, {"_2", ""},
		{true, ""}, {nil, ""}, {json.Number("2.5"), ""}, {json.Number("1"), ""},
		{json.Number("1e999999999"), ""}, {json.Number("1e-999999999"), ""}, {strings.Repeat("9", 129), ""},
	} {
		t.Run(stringValueForTest(tc.value), func(t *testing.T) {
			receipt, ok := parseAvreaCIRerunReceipt(map[string]any{"data": map[string]any{"run_id": "run-" + strings.Repeat("a", 32), "status": "rerun_requested", "run_attempt": tc.value}})
			if tc.want == "" {
				if ok {
					t.Fatalf("accepted invalid receipt: %#v", receipt)
				}
				return
			}
			if !ok || receipt.attempt != tc.want {
				t.Fatalf("receipt %#v, valid %v; want %s", receipt, ok, tc.want)
			}
		})
	}
}

func stringValueForTest(v any) string { raw, _ := json.Marshal(v); return string(raw) }

func TestAvreaCIRerunSelectsJobByIDAndReportsNewAttempt(t *testing.T) {
	d, stdout, stderr := testDeps(t)
	const next = "run-019a0000000070008000000000000009"
	var submitted atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any = map[string]any{}
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rerun"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			submitted.Store(body)
			data = map[string]any{"run_id": next, "status": "rerun_requested", "failed_only": false, "run_attempt": 2, "original_run_id": inspectionRun}
		case strings.HasSuffix(r.URL.Path, "/rerun"):
			data = map[string]any{"can_rerun": true, "can_rerun_subset": true}
		case strings.HasSuffix(r.URL.Path, inspectionRun):
			data = map[string]any{"run_id": inspectionRun, "ci_provider": "avrea_ci", "status": "completed", "jobs": []any{
				map[string]any{"job_id": inspectionJob, "job_name": "build"},
			}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	if code := Execute(t.Context(), []string{"run", "rerun", inspectionRun, "--job", inspectionJob, "--yes"}, d); code != 0 {
		t.Fatal(code, stderr)
	}
	body, _ := submitted.Load().(map[string]any)
	if ids, _ := body["job_ids"].([]any); len(ids) != 1 || ids[0] != inspectionJob {
		t.Fatal(body)
	}
	if !strings.Contains(stdout.String(), next) || !strings.Contains(stdout.String(), "attempt 2") {
		t.Fatal(stdout)
	}
}
