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

func TestNativeSubmitUsesLongerTimeoutWithoutChangingSharedClient(t *testing.T) {
	const input = `{"inputs":{"number":1.00000000000000001}}`
	transport := &http.Client{Timeout: time.Second, Transport: gitLabTestTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) < 50*time.Second {
			t.Error("native source checks lack sixty-second budget")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != input {
			t.Error(string(body), err)
		}
		if r.Header.Get("Idempotency-Key") != "key-fixture" || r.Header.Get("Authorization") != "Bearer token-fixture" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("incorrect request headers")
		}
		return &http.Response{StatusCode: 202, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{}}`))}, nil
	})}
	api := New(config.Config{Host: "https://fixture.test", Token: "token-fixture"}, transport, "test")
	if _, err := api.SubmitNativeDispatch(context.Background(), "/dispatch", []byte(input), "key-fixture"); err != nil {
		t.Fatal(err)
	}
	if api.HTTP.Timeout != time.Second || transport.Timeout != time.Second {
		t.Fatal("shared timeout mutated")
	}
}
