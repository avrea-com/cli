package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avrea-com/cli/config"
)

const resultTestRepo = "rep-019a0000000070008000000000000002"

func testEvidenceDeps(t *testing.T, handler func(*http.Request) string) func(...string) (int, string, string) {
	t.Helper()
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(handler(r))), Request: r}, nil
	})}
	return func(extra ...string) (int, string, string) {
		args := append([]string{"tests", resultTestJob, "--repo", resultTestRepo}, extra...)
		code := Execute(t.Context(), args, d)
		return code, out.String(), stderr.String()
	}
}

func testEvidencePage(filters, counts, total, cursor, outcome string) string {
	return fmt.Sprintf(`{"job_id":%q,"execution_id":"execution","status":"available","reporting_state":"complete","total_count":%s,"counts":%s,"filters":%s,"data":[{"name":"test_login","outcome":%q}],"pagination":{"next_cursor":%s}}`, resultTestJob, total, counts, filters, outcome, cursor)
}

func TestTestsSummaryReadsOnlyMetadata(t *testing.T) {
	for _, tc := range []struct{ name, counts, total, state, want string }{
		{"known", `{"passed":2,"failed":1,"skipped":1,"errored":1}`, "5", "complete", "passed: 2; failed: 1; skipped: 1; errored: 1"},
		{"zero", `{"passed":0,"failed":0,"skipped":0,"errored":0}`, "0", "complete", "Retained occurrences (all tests): 0"},
		{"unknown", "null", "null", "pending", "Counts: unknown"},
		{"partial", `{"passed":0,"failed":1,"skipped":0,"errored":0}`, "1", "partial", "Partial collection: only the reports received are included."},
		{"partial_unknown", "null", "null", "partial", "Partial collection: only the reports received are included."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			execute := testEvidenceDeps(t, func(r *http.Request) string {
				calls++
				if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/tests/summary") || r.URL.RawQuery != "" {
					t.Errorf("unexpected summary request: %s", r.URL)
				}
				return fmt.Sprintf(`{"job_id":%q,"status":"available","reporting_state":%q,"total_count":%s,"counts":%s}`, resultTestJob, tc.state, tc.total, tc.counts)
			})
			code, out, stderr := execute("--summary")
			if code != 0 || calls != 1 || !strings.Contains(out, tc.want) || strings.Contains(out, "shown:") || strings.Contains(out, "No more results") {
				t.Fatal(code, calls, out, stderr)
			}
			countsPosition := strings.Index(out, "Recorded outcomes")
			if tc.counts == "null" {
				countsPosition = strings.Index(out, "Counts: unknown")
			}
			if tc.state == "partial" && strings.Index(out, tc.want) > countsPosition {
				t.Fatal("Partial warning must precede counts:", out)
			}
		})
	}
}

func TestTestsSummaryJSONAndInvalidCombinations(t *testing.T) {
	for _, option := range []string{"--test", "--suite", "--class", "--status", "--cursor", "--limit", "-L"} {
		t.Run(option, func(t *testing.T) {
			execute := testEvidenceDeps(t, func(r *http.Request) string { t.Fatalf("unexpected request: %s", r.URL); return "" })
			value := "x"
			if option == "--status" {
				value = "failed"
			}
			if option == "--limit" || option == "-L" {
				value = "100"
			}
			code, out, stderr := execute("--summary", option, value)
			if code != 2 || out != "" || !strings.Contains(stderr, "--summary cannot be combined") {
				t.Fatal(code, out, stderr)
			}
		})
	}
	execute := testEvidenceDeps(t, func(r *http.Request) string {
		return fmt.Sprintf(`{"job_id":%q,"status":"expired","reporting_state":"complete","total_count":null,"counts":null}`, resultTestJob)
	})
	code, out, stderr := execute("--summary", "--json", "counts,total_count", "--jq", ".")
	if code != 0 {
		t.Fatal(code, out, stderr)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(out), &record); err != nil || len(record) != 2 || record["counts"] != nil || record["total_count"] != nil {
		t.Fatal(record, out, err)
	}
}

func TestTestsRepeatableStatusFilters(t *testing.T) {
	for _, tc := range []struct {
		name           string
		statuses       []string
		selector, echo string
	}{
		{"passed", []string{"passed"}, "eq.passed", `{"outcome":"passed"}`},
		{"failed", []string{"failed"}, "eq.failed", `{"outcome":"failed"}`},
		{"skipped", []string{"skipped"}, "eq.skipped", `{"outcome":"skipped"}`},
		{"errored", []string{"errored"}, "eq.errored", `{"outcome":"errored"}`},
		{"duplicates", []string{"failed", "failed"}, "eq.failed", `{"outcome":"failed"}`},
		{"failures", []string{"failed", "errored", "failed"}, "in.(errored,failed)", `{"outcomes":["errored","failed"]}`},
		{"all", []string{"passed", "skipped", "failed", "errored"}, "in.(errored,failed,passed,skipped)", `{"outcomes":["errored","failed","passed","skipped"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			execute := testEvidenceDeps(t, func(r *http.Request) string {
				calls++
				if r.URL.Query().Get("outcome") != tc.selector || r.URL.Query().Get("name") != "eq.test_login" {
					t.Error(r.URL)
				}
				cursor := `"next"`
				if calls == 2 {
					cursor = "null"
					if r.URL.Query().Get("cursor") != "next" {
						t.Error(r.URL)
					}
				}
				filters := strings.TrimSuffix(tc.echo, "}") + `,"name":"test_login"}`
				return testEvidencePage(filters, `{"passed":2,"failed":1,"skipped":1,"errored":1}`, "5", cursor, tc.statuses[0])
			})
			args := []string{"--test", "test_login", "--json", "counts,data"}
			for _, outcome := range tc.statuses {
				args = append(args, "--status", outcome)
			}
			code, out, stderr := execute(args...)
			var record struct {
				Counts map[string]int
				Data   []any
			}
			err := json.Unmarshal([]byte(out), &record)
			if code != 0 || calls != 2 || err != nil || len(record.Data) != 2 || record.Counts["passed"] != 2 {
				t.Fatal(code, calls, record, err, out, stderr)
			}
		})
	}
}

func TestTestsRejectInvalidStatusBeforeRequests(t *testing.T) {
	for _, value := range []string{"", "FAILED", "failed,errored", "unknown"} {
		t.Run(value, func(t *testing.T) {
			execute := testEvidenceDeps(t, func(r *http.Request) string { t.Fatalf("unexpected request: %s", r.URL); return "" })
			code, out, stderr := execute("--status", "passed", "--status", value)
			if code != 2 || out != "" || !strings.Contains(stderr, "--status") {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestTestsRejectChangedCountsAndUnacknowledgedFiltersWithoutOutput(t *testing.T) {
	for _, change := range []string{"counts", "filters"} {
		t.Run(change, func(t *testing.T) {
			calls := 0
			execute := testEvidenceDeps(t, func(r *http.Request) string {
				calls++
				filters, counts, cursor := `{"outcomes":["errored","failed"]}`, `{"passed":2,"failed":1,"skipped":1,"errored":1}`, `"next"`
				if calls == 2 {
					cursor = "null"
					if change == "counts" {
						counts = `{"passed":1,"failed":2,"skipped":1,"errored":1}`
					} else {
						filters = `{"outcome":"failed"}`
					}
				}
				return testEvidencePage(filters, counts, "5", cursor, "failed")
			})
			code, out, stderr := execute("--status", "failed", "--status", "errored", "--json", "*")
			if code != 1 || calls != 2 || out != "" {
				t.Fatal(code, calls, out, stderr)
			}
		})
	}
}

func TestTestsRejectMalformedCounts(t *testing.T) {
	for _, counts := range []string{
		`{"passed":-1,"failed":2,"skipped":0,"errored":0}`,
		`{"passed":true,"failed":0,"skipped":0,"errored":0}`,
		`{"passed":1.5,"failed":0,"skipped":0,"errored":0}`,
		`{"passed":1,"failed":0,"skipped":0}`,
		`{"passed":2,"failed":0,"skipped":0,"errored":0}`,
		`{"passed":9223372036854775808,"failed":0,"skipped":0,"errored":0}`,
	} {
		for _, summary := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/summary=%t", counts, summary), func(t *testing.T) {
				execute := testEvidenceDeps(t, func(r *http.Request) string { return testEvidencePage(`{}`, counts, "1", "null", "failed") })
				args := []string{"--json", "*"}
				if summary {
					args = append(args, "--summary")
				}
				code, out, stderr := execute(args...)
				if code != 1 || out != "" || !strings.Contains(stderr, "invalid test outcome counts") {
					t.Fatal(code, out, stderr)
				}
			})
		}
	}
}

func TestTestsGitHubJobURLKeepsItsJobBeforeNameSelection(t *testing.T) {
	for _, name := range []string{"", "unit", "integration"} {
		for _, summary := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/summary=%t", name, summary), func(t *testing.T) {
				d, out, stderr := testDeps(t)
				d.LoadConfig = func() (config.Config, error) {
					return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
				}
				evidenceReads := 0
				d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
					var body string
					switch r.URL.Path {
					case "/orgs/" + inspectionOrg + "/workflow-runs":
						body = fmt.Sprintf(`{"data":[{"run_id":%q}],"pagination":{"next_cursor":null}}`, inspectionRun)
					case "/orgs/" + inspectionOrg + "/workflow-runs/" + inspectionRun:
						body = fmt.Sprintf(`{"run_id":%q,"repository":{"full_name":"owner/repo"},"jobs":[{"job_id":%q,"platform_job_id":456,"job_name":"unit"},{"job_id":"job-019a0000000070008000000000000004","platform_job_id":789,"job_name":"integration"}]}`, inspectionRun, resultTestJob)
					default:
						evidenceReads++
						path := "/orgs/" + inspectionOrg + "/repos/" + resultTestRepo + "/jobs/" + resultTestJob + "/tests"
						if summary {
							path += "/summary"
						}
						if r.URL.Path != path {
							t.Errorf("wrong evidence selection: %s", r.URL)
						}
						body = testEvidencePage(`{}`, "null", "null", "null", "failed")
					}
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})}
				args := []string{"tests", "https://github.com/owner/repo/actions/runs/123/job/456", "--repo", resultTestRepo}
				if name != "" {
					args = append(args, "--job", name)
				}
				if summary {
					args = append(args, "--summary")
				}
				code := Execute(t.Context(), args, d)
				if name == "integration" {
					if code != 1 || evidenceReads != 0 || out.Len() != 0 {
						t.Fatal(code, evidenceReads, out, stderr)
					}
				} else if code != 0 || evidenceReads != 1 {
					t.Fatal(code, evidenceReads, out, stderr)
				}
			})
		}
	}
}

func TestTestsListingStatesKeepCountsAndUncertainty(t *testing.T) {
	for _, tc := range []struct{ state, counts, total, want string }{
		{"indexing", `{"passed":0,"failed":1,"skipped":0,"errored":0}`, "1", "still being indexed"},
		{"unavailable", `{"passed":0,"failed":1,"skipped":0,"errored":0}`, "1", "cannot currently be listed"},
		{"limited", `{"passed":0,"failed":1,"skipped":0,"errored":0}`, "1", "reached a read limit"},
		{"expired", "null", "null", "Counts: unknown"},
		{"not_recorded", "null", "null", "Counts: unknown"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			execute := testEvidenceDeps(t, func(r *http.Request) string {
				return fmt.Sprintf(`{"job_id":%q,"status":%q,"reporting_state":"complete","counts":%s,"total_count":%s,"filters":{},"data":[],"pagination":{"next_cursor":null}}`, resultTestJob, tc.state, tc.counts, tc.total)
			})
			code, out, stderr := execute()
			if code != 0 || !strings.Contains(out, tc.want) || !strings.Contains(out, "Results: "+tc.state) || strings.Contains(out, "No more results") || strings.Contains(out, "zero test occurrences") {
				t.Fatal(code, out, stderr)
			}
			if tc.counts != "null" && !strings.Contains(out, "failed: 1") {
				t.Fatal(out)
			}
		})
	}
}

func TestTestsSummaryRejectsWrongJobOrListingState(t *testing.T) {
	for _, response := range []string{
		`{"job_id":"job-019a0000000070008000000000000004","status":"available"}`,
		fmt.Sprintf(`{"job_id":%q,"status":"indexing"}`, resultTestJob),
		fmt.Sprintf(`{"job_id":%q,"status":"expired","counts":{"passed":1,"failed":0,"skipped":0,"errored":0},"total_count":1}`, resultTestJob),
	} {
		t.Run(response, func(t *testing.T) {
			execute := testEvidenceDeps(t, func(r *http.Request) string { return response })
			code, out, stderr := execute("--summary", "--json", "*")
			if code != 1 || out != "" {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestTestsLaterListingFailureReportsAvailabilityWithoutPartialOutput(t *testing.T) {
	for _, state := range []string{"indexing", "unavailable", "limited"} {
		t.Run(state, func(t *testing.T) {
			calls := 0
			execute := testEvidenceDeps(t, func(r *http.Request) string {
				calls++
				if calls == 1 {
					return testEvidencePage(`{}`, "null", "null", `"next"`, "failed")
				}
				return fmt.Sprintf(`{"job_id":%q,"execution_id":"execution","status":%q,"reporting_state":"complete","total_count":null,"counts":null,"filters":{},"data":[],"pagination":{"next_cursor":null}}`, resultTestJob, state)
			})
			code, out, stderr := execute("--json", "*")
			if code != 1 || calls != 2 || out != "" || strings.Contains(stderr, "results changed") || strings.Contains(stderr, "--cursor") {
				t.Fatal(code, calls, out, stderr)
			}
			want := map[string]string{"indexing": "still being indexed", "unavailable": "cannot currently be listed", "limited": "reached a read limit"}[state]
			if !strings.Contains(stderr, want) {
				t.Fatal(stderr)
			}
		})
	}
}

func TestTestsMalformedJobURLUsageIncludesJobURLs(t *testing.T) {
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	code := Execute(t.Context(), []string{"tests", "https://github.com/owner/repo/actions/runs/123/job/456/extra"}, d)
	if code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "GitHub Actions run or job URL") {
		t.Fatal(code, out, stderr)
	}
}
