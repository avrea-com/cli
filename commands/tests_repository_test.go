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

const (
	historyTestID        = "1111111111111111111111111111111111111111111111111111111111111111"
	historyConfiguration = "2222222222222222222222222222222222222222222222222222222222222222"
	otherTestID          = "3333333333333333333333333333333333333333333333333333333333333333"
)

// repositoryTestsDeps answers every request with handler's status and body.
func repositoryTestsDeps(t *testing.T, handler func(*http.Request) (int, string)) func(...string) (int, string, string) {
	t.Helper()
	d, out, stderr := testDeps(t)
	d.LoadConfig = func() (config.Config, error) {
		return config.Config{Host: "https://api.example.test", Token: "fixture", Org: inspectionOrg}, nil
	}
	d.HTTP = &http.Client{Transport: watchTransportFunc(func(r *http.Request) (*http.Response, error) {
		status, body := handler(r)
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	return func(args ...string) (int, string, string) {
		code := Execute(t.Context(), append([]string{"tests"}, args...), d)
		return code, out.String(), stderr.String()
	}
}

func discoveredTest(id, class, name, outcome string) string {
	return fmt.Sprintf(`{"test_id":%q,"configuration_digest":%q,"result_id":"result","suite":"pytest","class_name":%q,"name":%q,"outcome":%q,"duration_ms":12,"recorded_at":"2026-11-14T10:00:00Z","commit_sha":"abc","job_id":%q}`, id, historyConfiguration, class, name, outcome, resultTestJob)
}

func discoveryPage(filters, cursor string, rows ...string) string {
	return fmt.Sprintf(`{"organization_id":"`+inspectionOrg+`","repository_id":"`+resultTestRepo+`","status":"available","truncated":false,"filters":%s,"scope":{"complete":false,"reports_searched":7,"reports_indexing":2,"reports_unavailable":0,"oldest_recorded_at":"2026-11-01T00:00:00Z","newest_recorded_at":"2026-11-14T10:00:00Z"},"data":[%s],"pagination":{"next_cursor":%s}}`, filters, strings.Join(rows, ","), cursor)
}

func TestTestsSearchSendsFiltersAndShowsScope(t *testing.T) {
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		query := r.URL.Query()
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/repos/"+resultTestRepo+"/tests") || query.Get("q") != "login" || query.Get("outcome") != "in.(errored,failed)" || query.Get("limit") != "1" {
			t.Errorf("unexpected search request: %s", r.URL)
		}
		return 200, discoveryPage(`{"query":"login","outcomes":["failed","errored"]}`, `"next"`, discoveredTest(historyTestID, "tests.auth", "test_login", "failed"))
	})
	code, out, stderr := execute("search", " login ", "--repo", resultTestRepo, "--status", "failed", "--status", "errored", "-L", "1")
	if code != 0 {
		t.Fatal(code, out, stderr)
	}
	for _, want := range []string{"Reports searched: 7", "still indexing: 2", "Not every recorded report was searched", "tests.auth", "test_login", "failed", resultTestJob} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(stderr, "--cursor next") {
		t.Error(stderr)
	}
}

func TestTestsSearchFollowsShortPagesUpToTheLimit(t *testing.T) {
	requests := 0
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		requests++
		if requests == 1 {
			if r.URL.Query().Get("limit") != "2" || r.URL.Query().Has("cursor") {
				t.Errorf("unexpected first request: %s", r.URL)
			}
			return 200, discoveryPage(`{}`, `"next"`, discoveredTest(historyTestID, "a", "first", "passed"))
		}
		if r.URL.Query().Get("limit") != "1" || r.URL.Query().Get("cursor") != "next" {
			t.Errorf("unexpected second request: %s", r.URL)
		}
		return 200, discoveryPage(`{}`, `"later"`, discoveredTest(otherTestID, "a", "second", "passed"))
	})
	code, out, stderr := execute("search", "--repo", resultTestRepo, "-L", "2", "--json", "data,pagination")
	var record struct {
		Data       []map[string]any `json:"data"`
		Pagination map[string]any   `json:"pagination"`
	}
	if err := json.Unmarshal([]byte(out), &record); err != nil || code != 0 {
		t.Fatal(code, err, out, stderr)
	}
	if requests != 2 || len(record.Data) != 2 || record.Data[1]["name"] != "second" || record.Pagination["next_cursor"] != "later" {
		t.Fatal(requests, out)
	}
}

func TestTestsSearchRejectsUnconfirmedFiltersAndBadInput(t *testing.T) {
	execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
		return 200, discoveryPage(`{"query":null,"outcomes":null}`, "null")
	})
	if code, out, stderr := execute("search", "login", "--repo", resultTestRepo); code != 1 || out != "" || !strings.Contains(stderr, "did not confirm") {
		t.Fatal(code, out, stderr)
	}
	for _, args := range [][]string{
		{"search", "--status", "broken"},
		{"search", "-L", "0"},
		{"search", "  "},
		{"search", "--cursor", " next"},
		{"history", "test_login", "-L", "101"},
		{"history", historyTestID, "--configuration", "short"},
		{"history", "test_login", "--configuration", historyConfiguration},
	} {
		if code, out, _ := execute(args...); code != 2 || out != "" {
			t.Errorf("%v: %d %q", args, code, out)
		}
	}
}

func TestTestsSearchNamesAnEmptyRepository(t *testing.T) {
	execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
		return 200, `{"status":"not_recorded","truncated":false,"filters":{},"scope":{"complete":true,"reports_searched":0,"reports_indexing":0,"reports_unavailable":0,"oldest_recorded_at":null,"newest_recorded_at":null},"data":[],"pagination":{"next_cursor":null}}`
	})
	if code, out, stderr := execute("search", "--repo", resultTestRepo); code != 0 || !strings.Contains(out, "No test reports are recorded for this repository.") {
		t.Fatal(code, out, stderr)
	}
}

const historyPage = `{"status":"available","scope":{"reports_searched":12,"reports_indexing":1,"reports_unavailable":2},"data":[
{"capture_id":"tec-1","captured_at":"2026-11-14T10:00:00Z","passed_count":0,"failed_count":1,"errored_count":0,"skipped_count":0,"duration_total_ms":301,"duration_sample_count":2,"duration_max_ms":200,"job_id":"job-019a0000000070008000000000000003"},
{"capture_id":"tec-2","captured_at":"2026-11-13T10:00:00Z","passed_count":1,"failed_count":0,"errored_count":0,"skipped_count":0,"duration_total_ms":0,"duration_sample_count":0,"duration_max_ms":null,"job_id":null}
],"pagination":{"next_cursor":"older"}}`

func TestTestsHistoryResolvesAnExactNameThenReadsItsRuns(t *testing.T) {
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/tests") {
			if r.URL.Query().Get("q") != "test_login" || r.URL.Query().Get("limit") != "100" {
				t.Errorf("unexpected resolution request: %s", r.URL)
			}
			return 200, discoveryPage(`{"query":"test_login"}`, "null",
				discoveredTest(otherTestID, "tests.auth", "test_login_expired", "passed"),
				discoveredTest(historyTestID, "tests.auth", "test_login", "failed"),
				discoveredTest(historyTestID, "tests.auth", "test_login", "passed"))
		}
		if !strings.HasSuffix(r.URL.Path, "/tests/history/"+historyTestID) || r.URL.Query().Get("configuration") != historyConfiguration || r.URL.Query().Get("limit") != "20" {
			t.Errorf("unexpected history request: %s", r.URL)
		}
		return 200, historyPage
	})
	code, out, stderr := execute("history", "test_login", "--repo", resultTestRepo)
	if code != 0 {
		t.Fatal(code, out, stderr)
	}
	for _, want := range []string{"Test: tests.auth.test_login  Suite: pytest", "Test ID: " + historyTestID, "Reports searched: 12; still indexing: 1; unavailable: 2", "still being updated", "151", "none", resultTestJob, "unknown"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(stderr, "avr tests history "+historyTestID+" --configuration "+historyConfiguration+" --repo "+resultTestRepo+" --cursor older") {
		t.Error(stderr)
	}
}

// The wildcard is the field list scripts see: it carries scope and none of the
// keys the history response retired.
func TestTestsHistoryWildcardJSONCarriesScopeAndNoRetiredKeys(t *testing.T) {
	execute := repositoryTestsDeps(t, func(*http.Request) (int, string) { return 200, historyPage })
	code, out, stderr := execute("history", historyTestID, "--configuration", historyConfiguration, "--repo", resultTestRepo, "--json", "*")
	if code != 0 {
		t.Fatal(code, out, stderr)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(out), &record); err != nil {
		t.Fatal(err, out)
	}
	scope, _ := record["scope"].(map[string]any)
	for key, want := range map[string]float64{"reports_searched": 12, "reports_indexing": 1, "reports_unavailable": 2} {
		if scope[key] != want {
			t.Errorf("scope.%s = %v, want %v in:\n%s", key, scope[key], want, out)
		}
	}
	for _, retired := range []string{"pending", "reason", "as_of_revision", "current_revision"} {
		if _, present := record[retired]; present {
			t.Errorf("retired key %q in:\n%s", retired, out)
		}
	}
}

func TestTestsHistoryNeverPicksAmongSeveralTests(t *testing.T) {
	requests := 0
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		requests++
		return 200, discoveryPage(`{"query":"test_login"}`, "null",
			discoveredTest(historyTestID, "tests.auth", "test_login", "passed"),
			discoveredTest(otherTestID, "tests.api", "test_login", "passed"))
	})
	code, out, stderr := execute("history", "test_login", "--repo", resultTestRepo, "--org", inspectionOrg)
	if code != 1 || out != "" || requests != 1 || !strings.Contains(stderr, "Several recorded tests match") || !strings.Contains(stderr, "avr tests history "+otherTestID+" --configuration "+historyConfiguration+" --repo "+resultTestRepo+" --org "+inspectionOrg) {
		t.Fatal(code, requests, out, stderr)
	}
}

func TestTestsHistoryByIDSkipsResolutionAndReportsNoMatch(t *testing.T) {
	requests := 0
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		requests++
		if strings.HasSuffix(r.URL.Path, "/tests") {
			return 200, discoveryPage(`{"query":"absent"}`, "null")
		}
		if r.URL.Query().Get("cursor") != "older" {
			t.Errorf("unexpected history request: %s", r.URL)
		}
		return 200, `{"status":"available","scope":{"reports_searched":3,"reports_indexing":0,"reports_unavailable":0},"data":[],"pagination":{"next_cursor":null}}`
	})
	code, out, stderr := execute("history", historyTestID, "--configuration", historyConfiguration, "--repo", resultTestRepo, "--cursor", "older")
	if code != 0 || requests != 1 || !strings.Contains(out, "No runs found in the reports searched on this page.") {
		t.Fatal(code, requests, out, stderr)
	}
	if code, _, stderr := execute("history", "absent", "--repo", resultTestRepo); code != 1 || !strings.Contains(stderr, "Not every recorded report was searched") {
		t.Fatal(code, stderr)
	}
}

func TestTestsHistoryDoesNotCallAStoppedReadEmpty(t *testing.T) {
	for _, status := range []string{"limited", "unavailable"} {
		execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
			return 200, `{"status":"` + status + `","scope":{"reports_searched":0,"reports_indexing":0,"reports_unavailable":0},"data":[],"pagination":{"next_cursor":null}}`
		})
		code, out, stderr := execute("history", historyTestID, "--configuration", historyConfiguration, "--repo", resultTestRepo)
		if code != 0 || !strings.Contains(out, "This read stopped early; run the command again to retry.") || strings.Contains(out, "No runs found") {
			t.Fatal(status, code, out, stderr)
		}
	}
}

// testDigestN is a distinct valid test ID for generated fixtures.
func testDigestN(n int) string {
	return fmt.Sprintf("%064x", n+16)
}

func limitedPage(status string) string {
	return strings.Replace(discoveryPage(`{"query":"test_login"}`, "null"), `"status":"available"`, `"status":"`+status+`"`, 1)
}

func TestTestsSearchFailsWhenALaterPageCannotBeListed(t *testing.T) {
	for _, status := range []string{"limited", "unavailable"} {
		for _, args := range [][]string{{}, {"--json", "status,data"}} {
			t.Run(status+strings.Join(args, ""), func(t *testing.T) {
				requests := 0
				execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
					requests++
					if requests == 1 {
						return 200, discoveryPage(`{"query":"test_login"}`, `"next"`, discoveredTest(historyTestID, "tests.auth", "test_login", "failed"))
					}
					return 200, limitedPage(status)
				})
				code, out, stderr := execute(append([]string{"search", "test_login", "--repo", resultTestRepo}, args...)...)
				if code != 1 || out != "" || requests != 2 || !strings.Contains(stderr, testListingMessage(status)) {
					t.Fatal(code, requests, out, stderr)
				}
			})
		}
	}
}

func TestTestsSearchSaysWhenTheWalkWasTruncated(t *testing.T) {
	const hint = "Search stopped at 1,000 tests; more match."
	for _, truncated := range []bool{true, false} {
		t.Run(fmt.Sprint(truncated), func(t *testing.T) {
			execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
				page := discoveryPage(`{}`, "null", discoveredTest(historyTestID, "tests.auth", "test_login", "failed"))
				return 200, strings.Replace(page, `"truncated":false`, fmt.Sprintf(`"truncated":%t`, truncated), 1)
			})
			code, out, stderr := execute("search", "--repo", resultTestRepo)
			if code != 0 || !strings.Contains(out, "test_login") || strings.Contains(stderr, hint) != truncated {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

// historyAfterResolution answers resolution searches with pages in order and
// records which test's history is then read.
func historyAfterResolution(t *testing.T, read *string, pages ...string) func(...string) (int, string, string) {
	t.Helper()
	searches := 0
	return repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		if strings.HasSuffix(r.URL.Path, "/tests") {
			if searches >= len(pages) || (searches > 0) != r.URL.Query().Has("cursor") {
				t.Errorf("unexpected resolution request %d: %s", searches, r.URL)
				return 500, `{}`
			}
			searches++
			return 200, pages[searches-1]
		}
		*read = r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		return 200, historyPage
	})
}

func TestTestsHistoryReadsTheWholeSearchBeforeChoosing(t *testing.T) {
	filters := `{"query":"test_login"}`
	contained := make([]string, 100)
	for i := range contained {
		contained[i] = discoveredTest(testDigestN(i), "tests.auth", fmt.Sprintf("test_login_%d", i), "passed")
	}
	for _, tc := range []struct {
		name  string
		pages []string
	}{
		{"exact name in a full first page, more matches later", []string{
			discoveryPage(filters, `"next"`, append([]string{discoveredTest(historyTestID, "tests.auth", "test_login", "failed")}, contained[1:]...)...),
			discoveryPage(filters, "null", discoveredTest(otherTestID, "tests.auth", "test_login_expired", "passed")),
		}},
		{"exact name beyond the first 100 rows", []string{
			discoveryPage(filters, `"next"`, contained...),
			discoveryPage(filters, "null", discoveredTest(historyTestID, "tests.auth", "test_login", "failed")),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := ""
			code, out, stderr := historyAfterResolution(t, &read, tc.pages...)("history", "test_login", "--repo", resultTestRepo)
			if code != 0 || read != historyTestID || !strings.Contains(out, "Test: tests.auth.test_login  Suite: pytest") {
				t.Fatal(code, read, out, stderr)
			}
		})
	}
}

func TestTestsHistoryNameSelectionRequiresCompleteReportsForFallback(t *testing.T) {
	for _, coverage := range []struct {
		name       string
		scope      string
		incomplete bool
	}{
		{"complete", `{"complete":true,"reports_indexing":0,"reports_unavailable":0}`, false},
		{"older reports omitted", `{"complete":false,"reports_indexing":0,"reports_unavailable":0}`, true},
		{"indexing", `{"complete":true,"reports_indexing":1,"reports_unavailable":0}`, true},
		{"unavailable", `{"complete":true,"reports_indexing":0,"reports_unavailable":1}`, true},
	} {
		for _, match := range []string{"exact", "substring", "class only", "suite only", "absent"} {
			for _, jsonOutput := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/json=%t", coverage.name, match, jsonOutput), func(t *testing.T) {
					var rows []string
					if match != "absent" {
						name := "test_login"
						class := "tests.auth"
						switch match {
						case "substring":
							name = "TEST_LOGIN_expired"
						case "class only":
							name, class = "test_logout", "tests.test_login"
						case "suite only":
							name = "test_logout"
						}
						row := discoveredTest(historyTestID, class, name, "passed")
						if match == "suite only" {
							row = strings.Replace(row, `"suite":"pytest"`, `"suite":"test_login"`, 1)
						}
						rows = append(rows, row)
					}
					var page map[string]json.RawMessage
					if err := json.Unmarshal([]byte(discoveryPage(`{"query":"test_login"}`, "null", rows...)), &page); err != nil {
						t.Fatal(err)
					}
					page["scope"] = json.RawMessage(coverage.scope)
					body, err := json.Marshal(page)
					if err != nil {
						t.Fatal(err)
					}
					args := []string{"history", "test_login", "--repo", resultTestRepo, "--org", inspectionOrg}
					if jsonOutput {
						args = append(args, "--json", "data")
					}
					read := ""
					code, out, stderr := historyAfterResolution(t, &read, string(body))(args...)
					if match == "exact" || (match == "substring" && !coverage.incomplete) {
						if code != 0 || read != historyTestID || out == "" {
							t.Fatal(code, read, out, stderr)
						}
						return
					}
					if code != 1 || read != "" || out != "" {
						t.Fatal(code, read, out, stderr)
					}
					want := "No recorded test matches"
					if coverage.incomplete {
						want = "Not every recorded report was searched"
						if strings.Contains(stderr, "No recorded test matches") {
							t.Fatal(stderr)
						}
					}
					if !strings.Contains(stderr, want) {
						t.Fatalf("missing %q in: %s", want, stderr)
					}
					want = "avr tests search test_login --repo " + resultTestRepo + " --org " + inspectionOrg + " -L 1000 --json data"
					if match != "substring" {
						want = "avr tests search --repo " + resultTestRepo + " --org " + inspectionOrg
						if strings.Contains(stderr, "avr tests search test_login") || strings.Contains(stderr, "avr tests history "+historyTestID) {
							t.Fatal(stderr)
						}
					}
					if !strings.Contains(stderr, want) {
						t.Fatalf("missing %q in: %s", want, stderr)
					}
					if coverage.incomplete && match == "substring" {
						want = "avr tests history " + historyTestID + " --configuration " + historyConfiguration + " --repo " + resultTestRepo + " --org " + inspectionOrg
						if !strings.Contains(stderr, want) {
							t.Fatalf("missing %q in: %s", want, stderr)
						}
					}
				})
			}
		}
	}
}

func TestTestsHistoryPreservesEmptyListingStatus(t *testing.T) {
	for _, tc := range []struct{ state, want string }{
		{"indexing", "Test results are still being indexed. Retry later"},
		{"unavailable", "Test results cannot currently be listed."},
		{"limited", "The test listing reached a read limit. Retry or narrow the filters"},
		{"expired", "No retained test reports were found in the searched portion."},
		{"not_recorded", "No retained test reports were found in the searched portion."},
	} {
		t.Run(tc.state, func(t *testing.T) {
			page := limitedPage(tc.state)
			if tc.state != "indexing" {
				page = strings.Replace(page, `"reports_indexing":2`, `"reports_indexing":0`, 1)
			}
			if tc.state == "unavailable" {
				page = strings.Replace(page, `"reports_unavailable":0`, `"reports_unavailable":1`, 1)
			}
			read := ""
			code, out, stderr := historyAfterResolution(t, &read, page)("history", "test_login", "--repo", resultTestRepo)
			if code != 1 || read != "" || out != "" || !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, "Not every recorded report was searched") {
				t.Fatal(code, read, out, stderr)
			}
		})
	}
}

func TestTestsHistoryNeverPicksFromAnUnfinishedSearch(t *testing.T) {
	filters := `{"query":"test_login"}`
	exact := discoveredTest(historyTestID, "tests.auth", "test_login", "failed")
	truncated := strings.Replace(discoveryPage(filters, "null", exact), `"truncated":false`, `"truncated":true`, 1)
	for _, tc := range []struct {
		name  string
		pages []string
		want  string
	}{
		{"truncated walk", []string{truncated}, "avr tests history " + historyTestID + " --configuration " + historyConfiguration + " --repo " + resultTestRepo + " --org " + inspectionOrg},
		{"later page limited", []string{discoveryPage(filters, `"next"`, exact), limitedPage("limited")}, testListingMessage("limited")},
		{"later page unavailable", []string{discoveryPage(filters, `"next"`, exact), limitedPage("unavailable")}, testListingMessage("unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := ""
			code, out, stderr := historyAfterResolution(t, &read, tc.pages...)("history", "test_login", "--repo", resultTestRepo, "--org", inspectionOrg)
			if code != 1 || out != "" || read != "" || strings.Contains(stderr, "Several") {
				t.Fatal(code, read, out, stderr)
			}
			for _, want := range []string{"may be beyond what was searched", "avr tests search test_login --repo " + resultTestRepo + " --org " + inspectionOrg + " -L 1000 --json data", tc.want} {
				if !strings.Contains(stderr, want) {
					t.Errorf("missing %q in:\n%s", want, stderr)
				}
			}
		})
	}
}

func TestTestsHistorySearchHintQuotesName(t *testing.T) {
	read := ""
	page := discoveryPage(`{"query":"login's test"}`, "null", discoveredTest(historyTestID, "tests.auth", "login's test expired", "passed"))
	code, out, stderr := historyAfterResolution(t, &read, page)("history", "login's test", "--repo", resultTestRepo)
	want := "avr tests search 'login'\"'\"'s test' --repo " + resultTestRepo + " -L 1000 --json data"
	if code != 1 || out != "" || read != "" || !strings.Contains(stderr, want) || strings.Contains(stderr, "--org") {
		t.Fatal(code, read, out, stderr)
	}
}

func TestTestsHistoryContinuationKeepsRepositoryAndOrganization(t *testing.T) {
	execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
		return 200, historyPage
	})
	code, out, stderr := execute("history", historyTestID, "--configuration", historyConfiguration, "--repo", resultTestRepo, "--org", inspectionOrg)
	if code != 0 {
		t.Fatal(code, out, stderr)
	}
	_, hint, _ := strings.Cut(stderr, "Continue with: ")
	for _, want := range []string{"avr tests history " + historyTestID + " --configuration " + historyConfiguration, " --repo " + resultTestRepo, " --org " + inspectionOrg, " --cursor older"} {
		if !strings.Contains(hint, want) {
			t.Errorf("missing %q in:\n%s", want, stderr)
		}
	}
}
