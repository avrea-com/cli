package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

func TestTokenListBoundsPagesAndTotal(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	calls := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		count := 200
		if calls == 1 {
			if r.URL.Query().Get("limit") != "200" || r.URL.Query().Has("cursor") {
				t.Fatal(r.URL)
			}
		} else {
			count = 1
			if calls != 2 || r.URL.Query().Get("limit") != "1" || r.URL.Query().Get("cursor") != "next +/" {
				t.Fatal(r.URL)
			}
		}
		rows := make([]map[string]any, count)
		for i := range rows {
			rows[i] = map[string]any{"id": fmt.Sprintf("key-019a0000000070008000%012x", i)}
		}
		body, err := json.Marshal(map[string]any{"data": rows, "pagination": map[string]any{"next_cursor": "next +/"}})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
	})}
	code := Execute(t.Context(), []string{"token", "list", "-L", "201", "--json", "id"}, d)
	var rows []map[string]any
	err := json.Unmarshal(out.Bytes(), &rows)
	if code != 0 || err != nil || calls != 2 || len(rows) != 201 || !strings.Contains(stderr.String(), "newest 201") {
		t.Fatal(code, calls, len(rows), err, stderr)
	}
}
func TestTokenListCancellationStopsPagination(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return nil, ctx.Err()
	})}
	code := Execute(ctx, []string{"token", "list"}, d)
	if code != 130 || calls != 1 {
		t.Fatal(code, calls, stderr)
	}
}
func TestTokenRevocationCancellationDoesNotSendRequest(t *testing.T) {
	d, _, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("request after cancellation"); return nil, nil })}
	code := Execute(ctx, []string{"token", "revoke", "key-" + "019a0000000070008000000000000004", "--yes"}, d)
	if code != 130 {
		t.Fatal(code, stderr)
	}
}
