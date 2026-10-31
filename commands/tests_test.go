package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

const resultTestJob = "job-019a0000000070008000000000000003"

func TestTestsShorthandOnComposedRoots(t *testing.T) {
	for _, name := range []string{"avr", "avr-admin"} {
		for _, args := range [][]string{
			{"tests", resultTestJob, "--json", "?"},
			{"--no-color", "tests", resultTestJob, "--json", "?"},
			{"tests", "awj-019a0000000070008000000000000007", "--json", "?"},
		} {
			t.Run(name+"/"+strings.Join(args, " "), func(t *testing.T) {
				d, out, stderr := testDeps(t)
				root := NewRoot(d)
				root.Use = name
				if code := ExecuteRoot(t.Context(), root, args, stderr, d); code != 0 || !strings.Contains(out.String(), "reporting_state") {
					t.Fatal(code, out, stderr)
				}
			})
		}
	}
}

func TestTestsLateFailureDoesNotEmitPartialResults(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	requests := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		status, body := 200, fmt.Sprintf(`{"job_id":%q,"data":[{"name":"first"}],"pagination":{"next_cursor":"next"}}`, resultTestJob)
		if requests == 1 && r.URL.Query().Get("limit") != "100" {
			t.Error(r.URL)
		}
		if requests == 2 {
			if r.URL.Query().Get("limit") != "99" || r.URL.Query().Get("cursor") != "next" {
				t.Error(r.URL)
			}
			status, body = 403, `{"detail":"fixture denied"}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	code := Execute(t.Context(), []string{"tests", resultTestJob, "--repo", "rep-019a0000000070008000000000000002"}, d)
	if code != 1 || requests != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "403") {
		t.Fatal(code, requests, out, stderr)
	}
}

func TestTestsHonorsCancellation(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		cancel()
		return nil, r.Context().Err()
	})}
	if code := Execute(ctx, []string{"tests", resultTestJob}, d); code != 130 || out.Len() != 0 {
		t.Fatal(code, out, stderr)
	}
}
