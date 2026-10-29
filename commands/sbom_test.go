package commands

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestSBOMPrefixStopsRepeatedHistoryCursor(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	requests := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if requests > 2 {
			t.Error("unbounded history")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"data\":[],\"pagination\":{\"next_cursor\":\"same\"}}")), Request: r}, nil
	})}
	if code := Execute(t.Context(), []string{"sbom", "view", "aaaaaaa", "--repo", "rep-019a0000000070008000000000000002"}, d); code != 1 || requests != 2 || !strings.Contains(stderr.String(), "pagination did not advance") {
		t.Fatal(code, requests, stderr)
	}
}

func TestSBOMPrefixHonorsCancellation(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		cancel()
		return nil, r.Context().Err()
	})}
	if code := Execute(ctx, []string{"sbom", "view", "aaaaaaa", "--repo", "rep-019a0000000070008000000000000002"}, d); code != 130 {
		t.Fatal(code, stderr)
	}
}
