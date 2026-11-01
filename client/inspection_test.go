package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestRunsPreserveWireDataAndQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/org-019a0000000070008000000000000001/workflow-runs" {
			t.Errorf("path %s", r.URL.Path)
		}
		if values := r.URL.Query()["statuses"]; len(values) != 2 || values[0] != "queued" || values[1] != "in_progress" {
			t.Errorf("query %v", r.URL.Query())
		}
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing authorization")
		}
		_, _ = fmt.Fprint(w, `{"data":[{"run_id":"run-019a0000000070008000000000000001","status":"in_progress","conclusion":null,"duration_seconds":0.123456789012345,"jobs":[],"future_id":9007199254740993}],"pagination":{"next_cursor":"next-page"}}`)
	}))
	defer server.Close()
	api := New(config.Config{Host: server.URL, Token: "fixture"}, nil, "test")
	page, err := api.Runs(context.Background(), "org-019a0000000070008000000000000001", url.Values{"statuses": {"queued", "in_progress"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor != "next-page" {
		t.Fatalf("page %#v", page)
	}
	record := page.Items[0]
	if record.Value.Status != "in_progress" || !record.Value.Conclusion.IsNull() || len(record.Value.Jobs.GetOrEmpty()) != 0 {
		t.Fatalf("typed value %#v", record.Value)
	}
	if record.Fields["future_id"] != json.Number("9007199254740993") {
		t.Fatalf("raw number changed: %#v", record.Fields)
	}
	if record.Fields["duration_seconds"] != json.Number("0.123456789012345") {
		t.Fatal("fraction changed")
	}
}

func TestMalformedInspectionResponse(t *testing.T) {
	for _, body := range []map[string]any{
		{"data": "bad"},
		{"data": []any{"not-an-object"}},
		{"data": []any{map[string]any{"run_id": true}}},
	} {
		if _, err := decodePage[Run](body); err == nil {
			t.Fatalf("accepted malformed data: %#v", body)
		}
	}
}
