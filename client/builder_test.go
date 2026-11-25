package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestBuilderSessionResponses(t *testing.T) {
	body := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, body) }))
	defer server.Close()
	api := New(config.Config{Host: server.URL, Token: "secret"}, server.Client(), "avr-test")

	body = `{"data":{"builder_session_id":"bs-1","state":"RUNNING","endpoints":{"ssh":{"external_ip":"203.0.113.5","external_port":22}}}}`
	record, err := api.BuilderSession(t.Context(), "/session")
	if err != nil || record["builder_session_id"] != "bs-1" {
		t.Fatal(record, err)
	}
	if record, err = api.CreateBuilderSession(t.Context(), "/sessions", map[string]any{"platform": "linux/amd64"}); err != nil || record["state"] != "RUNNING" {
		t.Fatal(record, err)
	}
	if envelope, err := api.DeleteBuilderSession(t.Context(), "/session"); err != nil || envelope["data"] == nil {
		t.Fatal(envelope, err)
	}

	// The endpoint decides where ssh connects, so its fields are checked.
	for _, body = range []string{`{}`, `{"data":[]}`, `{"data":{"state":7}}`, `{"data":{"endpoints":{"ssh":{"external_port":"22; rm"}}}}`} {
		if _, err = api.BuilderSession(t.Context(), "/session"); err == nil {
			t.Errorf("accepted %s", body)
		}
		if _, err = api.CreateBuilderSession(t.Context(), "/sessions", nil); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
	body = `{"data":{"state":7}}`
	if _, err = api.DeleteBuilderSession(t.Context(), "/session"); err == nil {
		t.Fatal("accepted a malformed stop response")
	}
	body = ""
	if envelope, err := api.DeleteBuilderSession(t.Context(), "/session"); err != nil || envelope != nil {
		t.Fatal(envelope, err)
	}
}
