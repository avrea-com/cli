package commands

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avrea-com/cli/config"
)

func TestNearestMirrorCancelsUnresponsiveProbe(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	h := &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer git-fixture" || r.URL.Path != "/acme/web.git/info/refs" {
			t.Errorf("invalid probe request")
		}
		if r.URL.Host == "stuck.test" {
			close(started)
			<-r.Context().Done()
			close(stopped)
			return nil, r.Context().Err()
		}
		<-started
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/x-git-upload-pack-advertisement; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	got, err := nearestMirror(t.Context(), h, []string{"https://stuck.test", "https://ready.test"}, "acme/web", "git-fixture")
	if err != nil || got != "https://ready.test" {
		t.Fatal(got, err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("losing probe was not cancelled")
	}
}
func TestNearestMirrorRefusesRedirects(t *testing.T) {
	calls := 0
	h := &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "mirror.test" {
			t.Fatal("followed credential-bearing redirect")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://untrusted.test"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	_, err := nearestMirror(t.Context(), h, []string{"https://mirror.test"}, "acme/web", "git-fixture")
	if calls != 1 || err == nil || !strings.Contains(err.Error(), "HTTP 302") || strings.Contains(err.Error(), "git-fixture") {
		t.Fatal(calls, err)
	}
}
func TestNearestMirrorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	h := &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	_, err := nearestMirror(ctx, h, []string{"https://mirror.test"}, "acme/web", "git-fixture")
	if ctx.Err() == nil || !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestGitCredentialRejectsMalformedIDNBeforeMinting(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.In = strings.NewReader("protocol=https\nhost=xn--a\npath=acme/web.git\n\n")
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture"}, nil
	}
	calls := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"token\":\"git.fixture\",\"expires_at\":\"2099-01-01T00:00:00Z\"}")), Request: r}, nil
	})}
	code := Execute(t.Context(), []string{"auth", "git-credential", "--url", "https://xn--a/acme/web.git", "--repo", "acme/web", "get"}, d)
	if code != 1 || calls != 0 {
		t.Fatal(code, calls, stderr)
	}
}
