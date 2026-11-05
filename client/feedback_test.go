package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestFeedbackInvalidResponse(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"id":"fdb-test"}`, `{"id":"fdb-test","status":"queued","created_at":"invalid"}`, `{"id":"fdb-test","status":"queued","created_at":"2026-11-05T12:00:00Z"} {}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, body) }))
			defer server.Close()
			api := New(config.Config{Host: server.URL, Token: "secret"}, server.Client(), "avr-test")
			if _, err := api.SubmitFeedback(t.Context(), map[string]any{"message": "hello"}, "key"); err == nil {
				t.Fatal("accepted invalid submission response")
			}
			if _, err := api.Feedback(t.Context(), "id"); err == nil {
				t.Fatal("accepted invalid status response")
			}
		})
	}
}

func TestFeedbackDoesNotFollowRedirect(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	api := New(config.Config{Host: server.URL, Token: "secret"}, server.Client(), "avr-test")
	if _, err := api.SubmitFeedback(t.Context(), map[string]any{"message": "hello"}, "key"); err == nil {
		t.Fatal("accepted redirect")
	}
	if calls != 0 {
		t.Fatal("feedback and credentials followed redirect")
	}
}
