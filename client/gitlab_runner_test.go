package client

import (
	"github.com/avrea-com/cli/config"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

type gitLabTestTransport func(*http.Request) (*http.Response, error)

func (f gitLabTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGitLabRunnerCredentialBoundary(t *testing.T) {
	for _, status := range []int{201, 302} {
		calls := 0
		jar, _ := cookiejar.New(nil)
		h := &http.Client{Timeout: time.Second, Jar: jar, Transport: gitLabTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.String() != "https://gitlab.com/api/v4/user/runners" || r.Method != "POST" {
				t.Fatal(r.URL, r.Method)
			}
			if r.Header.Get("PRIVATE-TOKEN") != "personal-fixture" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Fatal("credential boundary failed")
			}
			data, _ := io.ReadAll(r.Body)
			if strings.Contains(string(data), "personal-fixture") || strings.Contains(string(data), "api-fixture") {
				t.Fatal("token in body")
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://untrusted.example.test"}}, Body: io.NopCloser(strings.NewReader("{\"id\":42,\"token\":\"runner-fixture\"}")), Request: r}, nil
		})}
		c := New(config.Config{Host: "https://api.example.test", Token: "api-fixture"}, h, "fixture")
		var trace strings.Builder
		c.Trace = &trace
		id, token, e := c.CreateGitLabRunner(t.Context(), "personal-fixture", map[string]any{"runner_type": "group_type"})
		if status == 201 && (e != nil || id != "42" || token != "runner-fixture") {
			t.Fatal(id, e)
		}
		if status == 302 && e == nil {
			t.Fatal("redirect accepted")
		}
		if calls != 1 || trace.Len() != 0 || h.Timeout != time.Second || h.Jar != jar {
			t.Fatal("external request changed caller or emitted trace")
		}
	}
}
func TestGitLabRunnerInvalidMetadata(t *testing.T) {
	for _, body := range []string{"null", "[]", "{}", "{\"id\":true,\"token\":\"x\"}", "{\"id\":1.5,\"token\":\"x\"}", "{\"id\":-1,\"token\":\"x\"}", "{\"id\":1,\"token\":\"\"}"} {
		c := New(config.Config{}, &http.Client{Transport: gitLabTestTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 201, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}, "fixture")
		if _, _, e := c.CreateGitLabRunner(t.Context(), "private-fixture", nil); e == nil || strings.Contains(e.Error(), "private-fixture") {
			t.Fatal("unsafe metadata result", e)
		}
	}
}
