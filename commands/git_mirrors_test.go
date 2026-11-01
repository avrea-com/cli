package commands

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestMirrorLostMutationResponsesAreNotRetried(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		message string
	}{
		{[]string{"enable", "--repo", "rep-019a0000000070008000000000000002"}, "the update may have been applied"},
		{[]string{"disable", "--repo", "rep-019a0000000070008000000000000002", "--yes"}, "the update may have been applied"},
		{[]string{"place", "gsc-fi", "--repo", "rep-019a0000000070008000000000000002"}, "placement response was lost"},
		{[]string{"unplace", "gsc-fi", "--repo", "rep-019a0000000070008000000000000002", "--yes"}, "before retrying"},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			d, _, stderr := testDeps(t)
			d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: "private_fixture", Org: inspectionOrg}, nil
			}
			calls := 0
			d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, &net.OpError{Op: "read", Net: "tcp", Err: errors.New("private transport details")}
			})}
			code := Execute(t.Context(), append([]string{"repo", "mirror"}, tc.args...), d)
			if code != 1 || calls != 1 || !strings.Contains(stderr.String(), tc.message) || strings.Contains(stderr.String(), "private") {
				t.Fatal(code, calls, stderr)
			}
		})
	}
}

func TestMirrorCreateCancellationBetweenMutations(t *testing.T) {
	d, _, stderr := testDeps(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	mutations := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		var payload any
		switch {
		case r.URL.Path == "/orgs/"+inspectionOrg+"/repos/resolve":
			payload = map[string]any{"data": map[string]any{"repository_id": "rep-019a0000000070008000000000000002", "full_name": "acme/web"}}
		case strings.HasSuffix(r.URL.Path, "/git-clusters"):
			payload = map[string]any{"data": []any{map[string]any{"cluster_id": "gsc-fi", "datacenter_id": "fi", "name": "Helsinki"}}, "pagination": map[string]any{"next_cursor": nil}}
		case r.Method == http.MethodGet:
			payload = map[string]any{"repository_id": "rep-019a0000000070008000000000000002", "full_name": "acme/web", "enabled": false, "placements": []any{}}
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/git-mirrors"):
			mutations++
			cancel()
			payload = map[string]any{"repository_id": "rep-019a0000000070008000000000000002", "full_name": "acme/web", "enabled": true, "placements": []any{}}
		default:
			t.Fatalf("unexpected call: %s %s", r.Method, r.URL)
		}
		raw, _ := json.Marshal(payload)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})}
	code := Execute(ctx, []string{"repo", "mirror", "create", "acme/web", "--location", "gsc-fi"}, d)
	if code != 130 || mutations != 1 {
		t.Fatal(code, mutations, stderr)
	}
}
