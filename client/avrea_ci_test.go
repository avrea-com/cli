package client

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

func TestAvreaCISubmitUsesLongerTimeoutWithoutChangingSharedClient(t *testing.T) {
	const input = `{"failed_only":false,"invocation_key":"key-fixture"}`
	transport := &http.Client{Timeout: time.Second, Transport: gitLabTestTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) < 50*time.Second {
			t.Error("rerun source checks lack sixty-second budget")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != input {
			t.Error(string(body), err)
		}
		if r.Header.Get("Authorization") != "Bearer token-fixture" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("incorrect request headers")
		}
		return &http.Response{StatusCode: 202, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{}}`))}, nil
	})}
	api := New(config.Config{Host: "https://fixture.test", Token: "token-fixture"}, transport, "test")
	if _, err := api.SubmitAvreaCIRerun(context.Background(), "/rerun", map[string]any{"failed_only": false, "invocation_key": "key-fixture"}); err != nil {
		t.Fatal(err)
	}
	if api.HTTP.Timeout != time.Second || transport.Timeout != time.Second {
		t.Fatal("shared timeout mutated")
	}
}

func TestAvreaCIDispatchOutlastsTheServerDeadline(t *testing.T) {
	transport := &http.Client{Timeout: time.Second, Transport: gitLabTestTransport(func(r *http.Request) (*http.Response, error) {
		if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) < 50*time.Second {
			t.Error("dispatch lacks sixty-second budget")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != `{"inputs":{"count":"2"},"invocation_key":"key-fixture","ref":"main","workflow":"wfl-fixture"}` {
			t.Error(string(body), err)
		}
		if r.URL.Path != "/orgs/org-fixture/repos/rep-fixture/dispatch-workflow" {
			t.Error(r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{"ref":"main"}}`))}, nil
	})}
	api := New(config.Config{Host: "https://fixture.test", Token: "token-fixture"}, transport, "test")
	response, err := api.DispatchAvreaCIWorkflow(context.Background(), "org-fixture", "rep-fixture", "wfl-fixture", "main", "key-fixture", map[string]string{"count": "2"})
	if err != nil || response["ref"] != "main" {
		t.Fatal(response, err)
	}
}
