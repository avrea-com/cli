package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
)

func TestDiagnosticsConcurrentEvidenceAndGeneratedWindow(t *testing.T) {
	d, _, _ := testDeps(t)
	generated := time.Date(2026, 10, 29, 12, 0, 0, 123456000, time.UTC)
	d.Now = func() time.Time { return generated }
	d = d.defaults()
	var arrived atomic.Int32
	ready := make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if arrived.Add(1) == 3 {
			close(ready)
		}
		select {
		case <-ready:
		case <-r.Context().Done():
			return
		}
		if strings.HasSuffix(r.URL.Path, "/aggregate") {
			if r.URL.Query().Get("created_after") != "2026-09-29T12:00:00.123456Z" || r.URL.Query().Get("created_before") != "2026-10-29T12:00:00.123455Z" {
				t.Error(r.URL)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer server.Close()
	cfg := config.Config{Host: server.URL, Token: "avr_fixture"}
	report, err := buildRunDiagnostics(ctx, d, client.New(cfg, nil, "fixture"), inspectionOrg, map[string]any{
		"run_id": inspectionRun, "repository_id": "rep-019a0000000070008000000000000002",
		"platform_workflow_id": json.Number("42"), "status": "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if arrived.Load() != 3 {
		t.Fatal(arrived.Load())
	}
	baseline := diagnosticObject(report["baseline"])
	if baseline["window_end"] != "2026-10-29T12:00:00.123456Z" || baseline["window_start"] != "2026-09-29T12:00:00.123456Z" {
		t.Fatal(baseline)
	}
	warnings := objectList(report["warnings"])
	if len(warnings) != 1 || warnings[0]["code"] != "baseline_target_time_missing" || report["complete"] != false {
		t.Fatal(warnings, report["complete"])
	}
}

func TestDiagnosticsCancellationIsNotAnIncompleteSuccess(t *testing.T) {
	d, _, stderr := testDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/metrics") {
			cancel()
			<-r.Context().Done()
			return
		}
		response := map[string]any{"data": []any{}}
		if strings.HasSuffix(r.URL.Path, inspectionRun) {
			response["data"] = map[string]any{"run_id": inspectionRun, "created_at": "2026-01-01T00:00:00Z", "status": "completed"}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: server.URL, Token: "avr_fixture", Org: inspectionOrg}, nil
	}
	if code := Execute(ctx, []string{"run", "diagnose", inspectionRun, "--json"}, d); code != 130 {
		t.Fatal(code, stderr)
	}
}

func TestDiagnosticsTimingUsesCompletionSourceAndRejectsRegression(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 30, 0, time.UTC)
	timing, skew := diagnosticTiming("2026-01-01T00:00:01Z", "2026-01-01T00:00:05Z", "2026-01-01T00:00:03Z", "2026-01-01T00:00:10Z", now, true, "updated_at")
	if !skew || timing["queue_seconds"] != json.Number("4.0") || timing["execution_seconds"] != nil || timing["start_offset_seconds"] != nil || timing["execution_end_source"] != "updated_at" {
		t.Fatal(timing, skew)
	}
	timing, skew = diagnosticTiming(nil, "2026-01-01T00:00:05Z", nil, nil, now, false, "completed_at")
	if skew || timing["execution_seconds"] != json.Number("25.0") || timing["execution_end_source"] != "generated_at" {
		t.Fatal(timing, skew)
	}
}
