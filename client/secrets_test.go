package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

type configurationTransport func(*http.Request) (*http.Response, error)

func (f configurationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestConfigurationWriteCannotReplayOrExposeServerEcho(t *testing.T) {
	for _, status := range []int{422, 500, 503} {
		trace := &bytes.Buffer{}
		calls := 0
		transport := &http.Client{Transport: configurationTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.GetBody != nil {
				t.Fatal("value write is replayable")
			}
			if r.Header.Get("Authorization") != "Bearer fixture" {
				t.Fatal("missing auth")
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"detail":"synthetic-secret","input":{"value":"synthetic-secret"}}`)), Request: r}, nil
		})}
		c := New(config.Config{Host: "https://api.example.test", Token: "fixture"}, transport, "test")
		c.Trace = trace
		err := c.ConfigurationRequest(context.Background(), http.MethodPut, "/orgs/org/secret-variants/variant/value", map[string]string{"value": "synthetic-secret"}, nil)
		var he *HTTPError
		if !errors.As(err, &he) || he.Status != status || he.Detail != "" || he.Payload != nil || calls != 1 || trace.Len() != 0 {
			t.Fatal(err, he, calls, trace)
		}
	}
}

func TestConfigurationMetadataErrorDetails(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		allowDetails bool
		status       int
		want         string
	}{
		{http.MethodPost, "/orgs/org/secrets/secret/variants", true, 422, "Maximum of 20 variants reached"},
		{http.MethodPatch, "/orgs/org/secrets/secret", true, 409, "Maximum of 20 variants reached"},
		{http.MethodPut, "/orgs/org/secret-variants/variant/value", false, 422, ""},
		{http.MethodPost, "/orgs/org/secrets/secret/variants", true, 500, ""},
		{http.MethodGet, "/orgs/org/secrets", true, 403, "Maximum of 20 variants reached"},
		{http.MethodPatch, "/orgs/org/secrets/secret", true, 503, "Maximum of 20 variants reached"},
	} {
		transport := &http.Client{Transport: configurationTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"detail":"Maximum of 20 variants reached","input":"never print raw payload"}`)), Request: r}, nil
		})}
		c := New(config.Config{Host: "https://api.example.test", Token: "fixture"}, transport, "test")
		request := c.ConfigurationRequest
		if tc.allowDetails {
			request = c.ConfigurationMetadataRequest
		}
		err := request(t.Context(), tc.method, tc.path, map[string]string{"label": "production"}, nil)
		var he *HTTPError
		if !errors.As(err, &he) || he.Detail != tc.want || he.Payload != nil {
			t.Fatal(err, he)
		}
	}
}

func TestConfigurationReadRetainsSanitizedTransportError(t *testing.T) {
	c := New(config.Config{Host: "https://api.example.test", Token: "fixture"}, &http.Client{Transport: configurationTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport echoed sensitive content")
	})}, "test")
	err := c.ConfigurationRequest(t.Context(), http.MethodGet, "/orgs/org/secrets", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "connection failed or timed out") || strings.Contains(err.Error(), "sensitive content") || strings.Contains(err.Error(), "confirmed") {
		t.Fatal(err)
	}
}

func TestConfigurationDetailsHiddenByDefault(t *testing.T) {
	for _, path := range []string{"/orgs/org/secrets", "/orgs/org/secret-variants/variant/value/", "/orgs/org/secret-variants/variant/value?request_id=test"} {
		t.Run(path, func(t *testing.T) {
			c := New(config.Config{Host: "https://api.example.test", Token: "fixture"}, &http.Client{Transport: configurationTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 422, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"detail":"synthetic-secret"}`)), Request: r}, nil
			})}, "test")
			err := c.ConfigurationRequest(t.Context(), http.MethodPut, path, map[string]string{"value": "synthetic-secret"}, nil)
			var he *HTTPError
			if !errors.As(err, &he) || he.Detail != "" || he.Payload != nil {
				t.Fatal(err, he)
			}
		})
	}
}
