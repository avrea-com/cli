package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

const (
	prCommentTestRepo    = "rep-019a0000000070008000000000000002"
	prCommentTestItem    = "pri-019a0000000070008000000000000007"
	prCommentTestThread  = "rth-019a0000000070008000000000000008"
	prCommentTestComment = "rcm-019a0000000070008000000000000009"
	prCommentTestToken   = "avr_fixture_private"
)

var (
	prCommentTestHead  = strings.Repeat("ab", 20)
	prCommentTestBase  = strings.Repeat("cd", 20)
	prCommentTestPulls = "/orgs/" + inspectionOrg + "/repos/" + prCommentTestRepo + "/mirror/pulls/42"
	prCommentTestPost  = prCommentTestPulls + "/comments"
	prCommentTestNew   = "/orgs/" + inspectionOrg + "/repos/" + prCommentTestRepo + "/mirror/review-threads"
	prCommentTestFlag  = "/orgs/" + inspectionOrg + "/feature-flags/" + prCommentsFeature
)

const prCommentTestPublication = `{"revision":1,"status":"pending","can_retry":false,"error":null}`

func prCommentItemJSON(publication string) string {
	return `{"id":"` + prCommentTestItem + `","kind":"COMMENT","context":null,"state":null,"title":null,"body":"Looks good","body_truncated":false,"details_url":null,"author":{"id":"usr-fixture","name":"Fixture","avatar_url":null,"provider":null},"deleted":false,"can_edit":true,"external_provider":null,"external_id":null,"publication":` + publication + `,"created_at":"2026-11-12T10:00:00Z","updated_at":"2026-11-12T10:00:00Z"}`
}

// A new thread carries no publication of its own; the inline comment does.
func prCommentThreadJSON(publication string) string {
	return `{"id":"` + prCommentTestThread + `","pull_number":42,"anchor":{"commit_sha":"` + prCommentTestHead + `","base_sha":"` + prCommentTestBase + `","path":"src/app.go","side":"RIGHT","start_line":18,"end_line":18},"created_by":"usr-fixture","resolved_by":null,"resolved_at":null,"external_provider":null,"external_id":null,"external_review_id":null,"publication":null,"can_reply":true,"can_resolve":true,"created_at":"2026-11-12T10:00:00Z","updated_at":"2026-11-12T10:00:00Z","comments":[{"id":"` + prCommentTestComment + `","parent_comment_id":null,"author":null,"body":"Handle this","body_truncated":false,"deleted":false,"can_edit":true,"external_provider":null,"external_id":null,"publication":` + publication + `,"created_at":"2026-11-12T10:00:00Z","updated_at":"2026-11-12T10:00:00Z"}],"comments_truncated":false,"comments_next_cursor":null}`
}

func prCommentPullJSON(head, base string) string {
	return `{"number":42,"title":"Fix","state":"open","draft":false,"merged":false,"author_login":"octo","base_ref":"main","head_ref":"fix","head_sha":` + head + `,"base_sha":` + base + `,"created_at":"2026-11-12T09:00:00Z","updated_at":"2026-11-12T09:30:00Z","body":null}`
}

// prCommentRoutes answers the requests of a successful comment.
func prCommentRoutes(r *http.Request) (int, string) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == prCommentTestFlag:
		return 200, `{"enabled":true}`
	case r.Method == http.MethodGet && r.URL.Path == "/orgs/"+inspectionOrg+"/repos/resolve":
		return 200, `{"data":{"repository_id":"` + prCommentTestRepo + `","full_name":"acme/web"}}`
	case r.Method == http.MethodGet && r.URL.Path == prCommentTestPulls:
		return 200, prCommentPullJSON(`"`+prCommentTestHead+`"`, `"`+prCommentTestBase+`"`)
	case r.Method == http.MethodPost && r.URL.Path == prCommentTestPost:
		return 201, prCommentItemJSON(prCommentTestPublication)
	case r.Method == http.MethodPost && r.URL.Path == prCommentTestNew:
		return 201, prCommentThreadJSON(prCommentTestPublication)
	}
	return 599, `{"detail":"unexpected request"}`
}

type prCommentRequest struct{ method, path, query, body string }

type prCommentHarness struct {
	d           Dependencies
	out, stderr *bytes.Buffer
	requests    []prCommentRequest
}

func newPRCommentHarness(t *testing.T, handler func(*http.Request) (int, string)) *prCommentHarness {
	t.Helper()
	d, out, stderr := testDeps(t)
	h := &prCommentHarness{d: d, out: out, stderr: stderr}
	h.d.GitRemote = func(context.Context) (string, error) { return "", nil }
	h.d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: prCommentTestToken, Org: inspectionOrg}, nil
	}
	h.d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		var body []byte
		if r.Body != nil {
			var err error
			if body, err = io.ReadAll(r.Body); err != nil {
				t.Fatal(err)
			}
		}
		if r.Header.Get("Authorization") != "Bearer "+prCommentTestToken {
			t.Errorf("%s %s: missing bearer token", r.Method, r.URL.Path)
		}
		h.requests = append(h.requests, prCommentRequest{r.Method, r.URL.Path, r.URL.RawQuery, string(body)})
		status, response := handler(r)
		if status == 599 {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
	})}
	return h
}

func (h *prCommentHarness) run(t *testing.T, args ...string) int {
	t.Helper()
	return Execute(t.Context(), append([]string{"pr", "comment"}, args...), h.d)
}

func (h *prCommentHarness) posted(t *testing.T) map[string]any {
	t.Helper()
	var posts []prCommentRequest
	for _, r := range h.requests {
		if r.method == http.MethodPost {
			posts = append(posts, r)
		}
	}
	if len(posts) != 1 {
		t.Fatalf("POST requests: %v", h.requests)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(posts[0].body), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestPRCommentPostsGeneralComment(t *testing.T) {
	h := newPRCommentHarness(t, prCommentRoutes)
	if code := h.run(t, "42", "--repo", "acme/web", "--body", "Looks good"); code != 0 {
		t.Fatal(code, h.stderr)
	}
	want := []prCommentRequest{
		{http.MethodGet, prCommentTestFlag, "", ""},
		{http.MethodGet, "/orgs/" + inspectionOrg + "/repos/resolve", "name=acme%2Fweb", ""},
		{http.MethodPost, prCommentTestPost, "", `{"body":"Looks good"}`},
	}
	if !reflect.DeepEqual(h.requests, want) {
		t.Fatalf("requests:\n%v\nwant:\n%v", h.requests, want)
	}
	if got := h.out.String(); got != "Commented on acme/web#42 ("+prCommentTestItem+"). Avrea will publish it to GitHub.\n" {
		t.Fatal(got)
	}
}

func TestPRCommentStartsInlineReviewThread(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flags  []string
		anchor map[string]any
		line   string
	}{
		{"single line", []string{"--path", "src/app.go", "--line", "18"},
			map[string]any{"path": "src/app.go", "side": "RIGHT", "start_line": float64(18), "end_line": float64(18)}, "src/app.go:18 ("},
		{"left range", []string{"--path", "src/app.go", "--start-line", "10", "--line", "14", "--side", "LEFT"},
			map[string]any{"path": "src/app.go", "side": "LEFT", "start_line": float64(10), "end_line": float64(14)}, "src/app.go:10-14, left side ("},
		{"explicit single-line range", []string{"--path", "a b/ü.go", "--start-line", "3", "--line", "3", "--side", "right"},
			map[string]any{"path": "a b/ü.go", "side": "RIGHT", "start_line": float64(3), "end_line": float64(3)}, "a b/ü.go:3 ("},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newPRCommentHarness(t, prCommentRoutes)
			h.d.LoadConfig = func() (config.Config, error) {
				return config.Config{Host: "https://api.example.test", Token: prCommentTestToken, Org: inspectionOrg, Repo: prCommentTestRepo}, nil
			}
			if code := h.run(t, append([]string{"42", "--body", "Handle this"}, tc.flags...)...); code != 0 {
				t.Fatal(code, h.stderr)
			}
			paths := []string{}
			for _, r := range h.requests {
				paths = append(paths, r.method+" "+r.path)
			}
			if want := []string{"GET " + prCommentTestFlag, "GET " + prCommentTestPulls, "POST " + prCommentTestNew}; !reflect.DeepEqual(paths, want) {
				t.Fatal(paths)
			}
			anchor := map[string]any{"commit_sha": prCommentTestHead, "base_sha": prCommentTestBase}
			for k, v := range tc.anchor {
				anchor[k] = v
			}
			if want := map[string]any{"pull_number": float64(42), "body": "Handle this", "anchor": anchor}; !reflect.DeepEqual(h.posted(t), want) {
				t.Fatalf("%v\nwant %v", h.posted(t), want)
			}
			want := "Commented on " + prCommentTestRepo + "#42 at " + tc.line + prCommentTestComment + ", thread " + prCommentTestThread + "). Avrea will publish it to GitHub.\n"
			if h.out.String() != want {
				t.Fatalf("%q\nwant %q", h.out, want)
			}
		})
	}
}

func TestPRCommentReadsBodyFromFileAndStdin(t *testing.T) {
	text := "## Review\n\nTrailing newline and ü are kept.\n"
	file := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, source string
		inline       bool
	}{{"file", file, false}, {"stdin", "-", false}, {"inline stdin", "-", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newPRCommentHarness(t, prCommentRoutes)
			h.d.In = strings.NewReader(text)
			args := []string{"42", "--repo", prCommentTestRepo, "--body-file", tc.source}
			if tc.inline {
				args = append(args, "--path", "src/app.go", "--line", "1")
			}
			if code := h.run(t, args...); code != 0 {
				t.Fatal(code, h.stderr)
			}
			if got := h.posted(t)["body"]; got != text {
				t.Fatalf("%q", got)
			}
		})
	}
}

func TestPRCommentRejectsInvalidInputBeforeLoadingConfig(t *testing.T) {
	file := filepath.Join(t.TempDir(), "blank.md")
	if err := os.WriteFile(file, []byte(" \n\t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inline := []string{"--path", "src/app.go", "--line", "3"}
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"no body", []string{"42"}, "", "exactly one of --body or --body-file"},
		{"both bodies", []string{"42", "--body", "x", "--body-file", "-"}, "y", "exactly one of --body or --body-file"},
		{"blank body", []string{"42", "--body", " \n\t"}, "", "body is empty"},
		{"blank file", []string{"42", "--body-file", file}, "", "body is empty"},
		{"blank stdin", []string{"42", "--body-file", "-"}, "", "body is empty"},
		{"invalid UTF-8", []string{"42", "--body-file", "-"}, "ok \xff", "UTF-8"},
		{"general over limit", []string{"42", "--body-file", "-"}, strings.Repeat("x", prCommentBodyLimit+1), "262144-character limit for pull request comments"},
		{"general over byte cap", []string{"42", "--body-file", "-"}, strings.Repeat("😀", prCommentBodyLimit+1), "262144-character limit"},
		{"inline over limit", append([]string{"42", "--body-file", "-"}, inline...), strings.Repeat("x", prInlineCommentBodyLimit+1), "65536-character limit for inline comments"},
		{"line without path", []string{"42", "--body", "x", "--line", "3"}, "", "--line requires --path"},
		{"path without line", []string{"42", "--body", "x", "--path", "a.go"}, "", "--path requires --line"},
		{"start line without path", []string{"42", "--body", "x", "--start-line", "3"}, "", "require --path and --line"},
		{"side without path", []string{"42", "--body", "x", "--side", "left"}, "", "require --path and --line"},
		{"start after end", append([]string{"42", "--body", "x", "--start-line", "4"}, inline...), "", "--start-line must be between 1 and --line"},
		{"start zero", append([]string{"42", "--body", "x", "--start-line", "0"}, inline...), "", "--start-line must be between 1 and --line"},
		{"line zero", []string{"42", "--body", "x", "--path", "a.go", "--line", "0"}, "", "--line must be a positive line number"},
		{"line over integer", []string{"42", "--body", "x", "--path", "a.go", "--line", "2147483648"}, "", "--line must be a positive line number"},
		{"side", append([]string{"42", "--body", "x", "--side", "up"}, inline...), "", "invalid --side: up"},
		{"absolute path", []string{"42", "--body", "x", "--path", "/src/a.go", "--line", "1"}, "", "--path must be a file path relative"},
		{"parent path", []string{"42", "--body", "x", "--path", "src/../a.go", "--line", "1"}, "", "--path must be a file path relative"},
		{"dot path", []string{"42", "--body", "x", "--path", "./a.go", "--line", "1"}, "", "--path must be a file path relative"},
		{"empty segment", []string{"42", "--body", "x", "--path", "src//a.go", "--line", "1"}, "", "--path must be a file path relative"},
		{"directory path", []string{"42", "--body", "x", "--path", "src/", "--line", "1"}, "", "--path must be a file path relative"},
		{"empty path", []string{"42", "--body", "x", "--path", "", "--line", "1"}, "", "--path must be a file path relative"},
		{"long path", []string{"42", "--body", "x", "--path", strings.Repeat("a", prCommentPathLimit+1), "--line", "1"}, "", "--path must be a file path relative"},
		{"zero number", []string{"0", "--body", "x"}, "", "NUMBER must be a pull request number"},
		{"signed number", []string{"+4", "--body", "x"}, "", "NUMBER must be a pull request number"},
		{"text number", []string{"#42", "--body", "x"}, "", "NUMBER must be a pull request number"},
		{"number over integer", []string{"2147483648", "--body", "x"}, "", "NUMBER must be a pull request number"},
		{"missing number", []string{"--body", "x"}, "", "Missing argument 'NUMBER'"},
		{"extra argument", []string{"42", "43", "--body", "x"}, "", "unexpected extra argument"},
		{"jq without json", []string{"42", "--body", "x", "--jq", ".id"}, "", "--jq requires --json"},
		{"unknown json field", []string{"42", "--body", "x", "--json", "nope"}, "", "Unknown JSON field(s): nope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			d.In = strings.NewReader(tc.stdin)
			d.LoadConfig = func() (config.Config, error) {
				t.Fatal("invalid input reached config/API")
				return config.Config{}, nil
			}
			code := Execute(t.Context(), append([]string{"pr", "comment"}, tc.args...), d)
			want := 2
			if tc.name == "unknown json field" {
				want = 1 // Shared --json contract for every command.
			}
			if code != want || out.Len() != 0 || !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out, stderr)
			}
		})
	}
}

// Bodies exactly at the limit pass validation and reach authentication, which
// fails here without a token. Limits count code points, not bytes.
func TestPRCommentBodyLimitBoundaries(t *testing.T) {
	inline := []string{"--path", "src/app.go", "--line", "3"}
	for _, tc := range []struct {
		name, stdin string
		inline      bool
	}{
		{"general", strings.Repeat("x", prCommentBodyLimit), false},
		{"general two-byte", strings.Repeat("ä", prCommentBodyLimit), false},
		{"general four-byte", strings.Repeat("😀", prCommentBodyLimit), false},
		{"inline", strings.Repeat("x", prInlineCommentBodyLimit), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, out, stderr := testDeps(t)
			d.In = strings.NewReader(tc.stdin)
			d.LoadConfig = func() (config.Config, error) { return config.Config{Host: "https://api.example.test"}, nil }
			args := []string{"pr", "comment", "42", "--body-file", "-"}
			if tc.inline {
				args = append(args, inline...)
			}
			if code := Execute(t.Context(), args, d); code != 4 || out.Len() != 0 || !strings.Contains(stderr.String(), "avr auth login") {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
		})
	}
}

func TestPRCommentJSONOutput(t *testing.T) {
	t.Run("general", func(t *testing.T) {
		h := newPRCommentHarness(t, prCommentRoutes)
		if code := h.run(t, "42", "--repo", prCommentTestRepo, "--body", "Looks good", "--json", "*"); code != 0 {
			t.Fatal(code, h.stderr)
		}
		var got map[string]any
		if err := json.Unmarshal(h.out.Bytes(), &got); err != nil {
			t.Fatal(err, h.out)
		}
		want := map[string]any{
			"id": prCommentTestItem, "thread_id": nil, "pull_number": float64(42), "repository_id": prCommentTestRepo,
			"path": nil, "side": nil, "start_line": nil, "end_line": nil, "commit_sha": nil, "base_sha": nil,
			"body": "Looks good", "created_at": "2026-11-12T10:00:00Z",
			"publication": map[string]any{"revision": float64(1), "status": "pending", "can_retry": false, "error": nil},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%v\nwant %v", got, want)
		}
	})
	t.Run("inline", func(t *testing.T) {
		h := newPRCommentHarness(t, prCommentRoutes)
		if code := h.run(t, "42", "--repo", prCommentTestRepo, "--body", "Handle this", "--path", "src/app.go", "--start-line", "17", "--line", "18", "--json", "id,thread_id,path,side,start_line,end_line,commit_sha,base_sha,publication"); code != 0 {
			t.Fatal(code, h.stderr)
		}
		var got map[string]any
		if err := json.Unmarshal(h.out.Bytes(), &got); err != nil {
			t.Fatal(err, h.out)
		}
		want := map[string]any{
			"id": prCommentTestComment, "thread_id": prCommentTestThread, "path": "src/app.go", "side": "RIGHT",
			"start_line": float64(17), "end_line": float64(18), "commit_sha": prCommentTestHead, "base_sha": prCommentTestBase,
			"publication": map[string]any{"revision": float64(1), "status": "pending", "can_retry": false, "error": nil},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%v\nwant %v", got, want)
		}
	})
	t.Run("jq", func(t *testing.T) {
		h := newPRCommentHarness(t, prCommentRoutes)
		if code := h.run(t, "42", "--repo", prCommentTestRepo, "--body", "Looks good", "--json", "id,publication", "--jq", ".publication.status"); code != 0 {
			t.Fatal(code, h.stderr)
		}
		if h.out.String() != "pending\n" {
			t.Fatalf("%q", h.out)
		}
	})
	t.Run("discovery", func(t *testing.T) {
		d, out, _ := testDeps(t)
		d.LoadConfig = func() (config.Config, error) { t.Fatal("field discovery loaded config"); return config.Config{}, nil }
		if code := Execute(t.Context(), []string{"pr", "comment", "42", "--json", "?"}, d); code != 0 || !strings.Contains(out.String(), "  thread_id\n") {
			t.Fatal(code, out)
		}
	})
}

func TestPRCommentReportsPublicationState(t *testing.T) {
	for _, tc := range []struct{ publication, want string }{
		{"null", "Avrea did not queue it for GitHub, so it is visible in Avrea only."},
		{`{"revision":1,"status":"synced"}`, "Avrea published it to GitHub."},
		{`{"revision":2,"status":"failed","error":"GitHub denied permission\u001b[31m"}`, "Publishing to GitHub failed: GitHub denied permission"},
		{`{"revision":1,"status":"failed","error":null}`, "Publishing to GitHub failed"},
	} {
		for _, inline := range []bool{false, true} {
			t.Run(fmt.Sprint(tc.publication, inline), func(t *testing.T) {
				h := newPRCommentHarness(t, func(r *http.Request) (int, string) {
					switch r.URL.Path {
					case prCommentTestPost:
						return 201, prCommentItemJSON(tc.publication)
					case prCommentTestNew:
						return 201, prCommentThreadJSON(tc.publication)
					}
					return prCommentRoutes(r)
				})
				args := []string{"42", "--repo", prCommentTestRepo, "--body", "x"}
				if inline {
					args = append(args, "--path", "src/app.go", "--line", "18")
				}
				if code := h.run(t, args...); code != 0 {
					t.Fatal(code, h.stderr)
				}
				line := h.out.String()
				if !strings.Contains(line, tc.want) || strings.Count(line, "\n") != 1 || strings.Contains(line, "\x1b") {
					t.Fatalf("%q", line)
				}
			})
		}
	}
}

func TestPRCommentFeatureFlagGatesRequests(t *testing.T) {
	for _, tc := range []struct {
		name, flag, want string
		status           int
	}{
		{"disabled", `{"enabled":false}`, "pull request commenting is not yet supported for this organization", 200},
		{"unknown organization", `{"detail":"Not Found"}`, "could not find this organization or confirm pull request commenting support", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newPRCommentHarness(t, func(r *http.Request) (int, string) {
				if r.URL.Path == prCommentTestFlag {
					return tc.status, tc.flag
				}
				return prCommentRoutes(r)
			})
			if code := h.run(t, "42", "--repo", "acme/web", "--body", "x"); code != 1 || len(h.requests) != 1 || !strings.Contains(h.stderr.String(), tc.want) {
				t.Fatal(code, h.requests, h.stderr)
			}
		})
	}
}

func TestPRCommentNotFoundResponses(t *testing.T) {
	for _, tc := range []struct {
		name, path, detail string
		inline             bool
	}{
		{"pull request", prCommentTestPost, "Pull request #42 not found", false},
		{"mirror", prCommentTestPost, "Repository mirror not available", false},
		{"inline pull request", prCommentTestPulls, "Pull request #42 not found", true},
		{"review context", prCommentTestNew, "Review context not found", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newPRCommentHarness(t, func(r *http.Request) (int, string) {
				if r.URL.Path == tc.path {
					return 404, `{"detail":"` + tc.detail + `"}`
				}
				return prCommentRoutes(r)
			})
			args := []string{"42", "--repo", prCommentTestRepo, "--body", "x"}
			if tc.inline {
				args = append(args, "--path", "src/app.go", "--line", "18")
			}
			message := ""
			if code := h.run(t, args...); code == 1 {
				message = h.stderr.String()
			}
			if !strings.Contains(message, "(HTTP 404): "+tc.detail) || strings.Contains(message, "may have been created") || h.out.Len() != 0 {
				t.Fatal(message, h.out)
			}
			if last := h.requests[len(h.requests)-1]; last.path != tc.path {
				t.Fatal("continued after 404:", h.requests)
			}
		})
	}
}

func TestPRCommentRequiresSyncedCommitsForInlineComments(t *testing.T) {
	for _, pull := range []string{prCommentPullJSON("null", `"`+prCommentTestBase+`"`), prCommentPullJSON(`"`+prCommentTestHead+`"`, "null"), prCommentPullJSON(`"main"`, `"`+prCommentTestBase+`"`)} {
		h := newPRCommentHarness(t, func(r *http.Request) (int, string) {
			if r.URL.Path == prCommentTestPulls {
				return 200, pull
			}
			return prCommentRoutes(r)
		})
		code := h.run(t, "42", "--repo", prCommentTestRepo, "--body", "x", "--path", "src/app.go", "--line", "1")
		if code != 1 || !strings.Contains(h.stderr.String(), "has not synced the head and base commits of pull request #42") {
			t.Fatal(code, h.stderr)
		}
		for _, r := range h.requests {
			if r.method == http.MethodPost {
				t.Fatal("posted without an anchor commit")
			}
		}
	}
}

func TestPRCommentTransportFailuresKeepContextWithoutPrivateDetails(t *testing.T) {
	for _, inline := range []bool{false, true} {
		requests := 3
		if inline {
			requests = 4
		}
		for failAt := 1; failAt <= requests; failAt++ {
			t.Run(fmt.Sprint(inline, failAt), func(t *testing.T) {
				h := newPRCommentHarness(t, prCommentRoutes)
				transport := h.d.HTTP.Transport
				calls := 0
				h.d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if calls == failAt {
						return nil, fmt.Errorf("dial with %s: %w", prCommentTestToken, &net.OpError{Op: "dial", Net: "tcp", Err: io.ErrUnexpectedEOF})
					}
					return transport.RoundTrip(r)
				})}
				args := []string{"42", "--repo", "acme/web", "--body", "x"}
				if inline {
					args = append(args, "--path", "src/app.go", "--line", "18")
				}
				if code := h.run(t, args...); code != 1 || calls != failAt {
					t.Fatal(code, calls, h.stderr)
				}
				message := h.stderr.String()
				if strings.Contains(message, "unexpected EOF") || strings.Contains(message, prCommentTestToken) || h.out.Len() != 0 {
					t.Fatal(message)
				}
				// Repository resolution reports its own failure; the others name the comment.
				want := "Could not comment on pull request #42 because the request to Avrea failed: ConnectError"
				if failAt == 2 {
					want = "could not reach https://api.example.test"
				}
				if !strings.Contains(message, want) {
					t.Fatal(message)
				}
				if posted := failAt == requests; posted != strings.Contains(message, "may have been created") {
					t.Fatal(message)
				}
			})
		}
	}
}

// A server error or an unusable success response can follow a committed write.
func TestPRCommentAmbiguousWriteWarnsBeforeRetry(t *testing.T) {
	for _, tc := range []struct {
		name, response, want string
		status               int
		inline               bool
	}{
		{"server error", `{"detail":"Bad gateway"}`, "HTTP 502", 502, false},
		{"comment without id", `{"kind":"COMMENT"}`, "unexpected API response: comment id is missing", 201, false},
		{"thread without comments", `{"id":"` + prCommentTestThread + `","comments":[]}`, "unexpected API response: review thread or comment id is missing", 201, true},
		{"invalid comment fields", strings.Replace(prCommentThreadJSON("null"), `"created_at":"2026-11-12T10:00:00Z","updated_at":"2026-11-12T10:00:00Z"}]`, `"created_at":"yesterday","updated_at":"2026-11-12T10:00:00Z"}]`, 1), "created_at: expected RFC3339 timestamp", 201, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newPRCommentHarness(t, func(r *http.Request) (int, string) {
				if r.Method == http.MethodPost {
					return tc.status, tc.response
				}
				return prCommentRoutes(r)
			})
			args := []string{"42", "--repo", prCommentTestRepo, "--body", "x"}
			if tc.inline {
				args = append(args, "--path", "src/app.go", "--line", "18")
			}
			code := h.run(t, args...)
			if message := h.stderr.String(); code != 1 || h.out.Len() != 0 || !strings.Contains(message, tc.want) || !strings.Contains(message, "may have been created") {
				t.Fatal(code, message)
			}
		})
	}
}

func TestPRCommentValidationErrorFromServer(t *testing.T) {
	h := newPRCommentHarness(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodPost {
			return 422, `{"detail":"Comment exceeds the provider body limit including attribution"}`
		}
		return prCommentRoutes(r)
	})
	if code := h.run(t, "42", "--repo", prCommentTestRepo, "--body", "x"); code != 1 || !strings.Contains(h.stderr.String(), "(HTTP 422): Comment exceeds the provider body limit including attribution") || strings.Contains(h.stderr.String(), "may have been created") {
		t.Fatal(code, h.stderr)
	}
}

func TestPRCommentRejectsScopedTokensBeforeRequests(t *testing.T) {
	h := newPRCommentHarness(t, prCommentRoutes)
	h.d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "avs_fixture_private", Org: inspectionOrg}, nil
	}
	code := h.run(t, "42", "--repo", prCommentTestRepo, "--body", "x")
	if code != 4 || len(h.requests) != 0 || !strings.Contains(h.stderr.String(), "scoped tokens cannot post comments") || strings.Contains(h.stderr.String(), "avs_fixture_private") {
		t.Fatal(code, h.requests, h.stderr)
	}
}

func TestPRCommentRepositoryDetection(t *testing.T) {
	t.Run("git remote", func(t *testing.T) {
		h := newPRCommentHarness(t, prCommentRoutes)
		h.d.GitRemote = func(context.Context) (string, error) { return "git@github.com:acme/web.git", nil }
		if code := h.run(t, "42", "--body", "x"); code != 0 {
			t.Fatal(code, h.stderr)
		}
		if r := h.requests[1]; r.path != "/orgs/"+inspectionOrg+"/repos/resolve" || r.query != "name=acme%2Fweb&platform=github" {
			t.Fatal(h.requests)
		}
	})
	t.Run("undetectable", func(t *testing.T) {
		h := newPRCommentHarness(t, prCommentRoutes)
		if code := h.run(t, "42", "--body", "x"); code != 1 || !strings.Contains(h.stderr.String(), "Could not determine repository") {
			t.Fatal(code, h.stderr)
		}
		for _, r := range h.requests {
			if r.method == http.MethodPost {
				t.Fatal("posted without a repository")
			}
		}
	})
}
