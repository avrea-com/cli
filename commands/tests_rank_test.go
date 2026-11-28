package commands

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// rankedTest is a row whose results took duration ms on average; a null
// duration means no result recorded one, so the statistics have no sample.
// The newest result and the longest one differ from the mean, so a column
// showing the wrong one of the three is caught.
func rankedTest(name, outcome, duration string, failed, reports int) string {
	samples, latest, longest := 2, "7", duration+"0"
	if duration == "null" {
		samples, latest, longest = 0, "null", "null"
	}
	return fmt.Sprintf(`{"test_id":%q,"configuration_digest":%q,"result_id":"result","suite":"pytest","class_name":"tests.unit","name":%q,"outcome":%q,"duration_ms":%s,"recorded_at":"2026-11-14T10:00:00Z","commit_sha":"abc","job_id":%q,"statistics":{"reports":%d,"failed_reports":%d,"duration_mean_ms":%s,"duration_min_ms":%s,"duration_max_ms":%s,"duration_sample_count":%d}}`, historyTestID, historyConfiguration, name, outcome, latest, resultTestJob, reports, failed, duration, latest, longest, samples)
}

// rankColumns are the failed, mean and longest columns of the row for name.
func rankColumns(t *testing.T, out, name string) string {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if cells := strings.Split(line, "|"); len(cells) >= 6 && strings.TrimSpace(cells[2]) == name {
			return strings.Join(strings.Fields(strings.Join(cells[3:6], " ")), " ")
		}
	}
	t.Fatalf("no row for %s in:\n%s", name, out)
	return ""
}

func rankPage(order, filters, cursor string, rows ...string) string {
	page := discoveryPage(filters, cursor, rows...)
	return strings.Replace(page, `"status":"available",`, `"status":"available","order":"`+order+`",`, 1)
}

func TestTestsRankSendsOrderAndFiltersAndShowsStatistics(t *testing.T) {
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		query := r.URL.Query()
		if r.Method != "GET" || r.URL.Path != "/orgs/"+inspectionOrg+"/repos/"+resultTestRepo+"/tests/statistics" ||
			query.Get("order") != "failed_reports.desc" || query.Get("q") != "login" || query.Get("outcome") != "eq.failed" ||
			query.Get("framework") != "in.(go,pytest)" || query.Get("duration_ms") != "gte.100" || query.Get("limit") != "2" {
			t.Errorf("unexpected rank request: %s", r.URL)
		}
		filters := `{"query":"login","outcomes":["failed"],"frameworks":["pytest","go"],"min_duration_ms":100}`
		return 200, rankPage("failed_reports.desc", filters, `"next"`, rankedTest("test_login_flaky", "failed", "250", 4, 6), rankedTest("test_login_slow", "failed", "120", 1, 7))
	})
	code, out, stderr := execute("rank", "login", "--repo", resultTestRepo, "--order", "failed_reports.desc", "--status", "failed",
		"--framework", "pytest", "--framework", "go", "--framework", "go", "--min-duration", "100", "-L", "2")
	if code != 0 {
		t.Fatal(code, out, stderr)
	}
	for _, want := range []string{
		"Reports searched: 7", "the statistics cover only the reports above", "Order: most failed reports first",
		"Failed counts the searched reports",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if flaky, slow := rankColumns(t, out, "test_login_flaky"), rankColumns(t, out, "test_login_slow"); flaky != "4/6 250 2500" || slow != "1/7 120 1200" {
		t.Errorf("unexpected statistics columns: %q, %q in:\n%s", flaky, slow, out)
	}
	if !strings.Contains(stderr, "the same query, order and filters and --cursor next") {
		t.Error(stderr)
	}
	// A result ID is too long for a column, so each failed row names the
	// command that reads it, in the scope the ranking was read from.
	view := "Read a failed result and when it started failing:\n  tests.unit.test_login_flaky\n    avr tests view result --org " + inspectionOrg + " --repo " + resultTestRepo + "\n  tests.unit.test_login_slow\n"
	if !strings.Contains(out, view) {
		t.Errorf("missing the view commands in:\n%s", out)
	}
}

func TestRepositoryTestsNameNoViewCommandForResultsThatDidNotFail(t *testing.T) {
	for _, command := range []string{"search", "rank"} {
		execute := repositoryTestsDeps(t, func(*http.Request) (int, string) {
			return 200, rankPage("duration_mean_ms.desc", `{}`, `null`, rankedTest("test_slow", "passed", "250", 0, 6), rankedTest("test_skipped", "skipped", "null", 0, 6))
		})
		if code, out, stderr := execute(command, "--repo", resultTestRepo); code != 0 || !strings.Contains(out, "test_slow") || strings.Contains(out, "avr tests view") {
			t.Fatal(command, code, out, stderr)
		}
	}
}

func TestRepositoryTestMinimumDurationTakesUnits(t *testing.T) {
	for value, milliseconds := range map[string]string{"250": "250", "500ms": "500", "2s": "2000", "1m": "60000", " 2S ": "2000", "99999999": "99999999"} {
		execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
			if got := r.URL.Query().Get("duration_ms"); got != "gte."+milliseconds {
				t.Errorf("--min-duration %q sent %q", value, got)
			}
			return 200, discoveryPage(`{"min_duration_ms":`+milliseconds+`}`, `null`)
		})
		if code, out, stderr := execute("search", "--repo", resultTestRepo, "--min-duration", value); code != 0 {
			t.Fatal(value, code, out, stderr)
		}
	}
}

func TestTestsRankFollowsShortPagesWithTheSameOrder(t *testing.T) {
	calls := 0
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		calls++
		query := r.URL.Query()
		if query.Get("order") != "duration_mean_ms.desc" {
			t.Errorf("order not kept: %s", r.URL)
		}
		if calls == 1 {
			if query.Get("limit") != "3" || query.Has("cursor") {
				t.Errorf("unexpected first page: %s", r.URL)
			}
			return 200, rankPage("duration_mean_ms.desc", `{}`, `"c2"`, rankedTest("test_slow", "passed", "900", 0, 5))
		}
		if query.Get("limit") != "2" || query.Get("cursor") != "c2" {
			t.Errorf("unexpected next page: %s", r.URL)
		}
		return 200, rankPage("duration_mean_ms.desc", `{}`, `null`, rankedTest("test_skipped", "skipped", "null", 0, 5))
	})
	code, out, stderr := execute("rank", "--repo", resultTestRepo, "-L", "3")
	if code != 0 || calls != 2 || !strings.Contains(out, "Order: longest mean duration first") || stderr != "" {
		t.Fatal(code, calls, out, stderr)
	}
	// Both pages' rows are shown once, in the order the API ranked them. A
	// test without a duration sample ranks last and has no mean.
	slow, skipped := strings.Index(out, "test_slow"), strings.Index(out, "test_skipped")
	if slow < 0 || skipped < slow || strings.Count(out, "test_slow") != 1 || strings.Count(out, "test_skipped") != 1 {
		t.Errorf("rows lost, repeated or reordered:\n%s", out)
	}
	if rankColumns(t, out, "test_skipped") != "0/5 none none" {
		t.Error(out)
	}
}

func TestTestsRankSaysWhenAContinuationReachesTheWalkLimit(t *testing.T) {
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		if r.URL.Query().Get("cursor") != "c950" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		return 200, strings.Replace(rankPage("duration_mean_ms.desc", `{}`, `null`, rankedTest("test_slow", "passed", "900", 0, 5)), `"truncated":false`, `"truncated":true`, 1)
	})
	code, out, stderr := execute("rank", "--repo", resultTestRepo, "--cursor", "c950", "-L", "50")
	if code != 0 || !strings.Contains(out, "test_slow") || !strings.Contains(stderr, "Ranking stopped at 1,000 tests") || strings.Contains(stderr, "--cursor") {
		t.Fatal(code, out, stderr)
	}
}

func TestTestsRankRejectsUnconfirmedOrderOrFilters(t *testing.T) {
	for name, page := range map[string]string{
		"order":     rankPage("duration_mean_ms.desc", `{"frameworks":["go"]}`, `null`),
		"framework": rankPage("failed_reports.desc", `{"frameworks":["jest"]}`, `null`),
		"duration":  rankPage("failed_reports.desc", `{"frameworks":["go"],"min_duration_ms":5}`, `null`),
	} {
		t.Run(name, func(t *testing.T) {
			execute := repositoryTestsDeps(t, func(*http.Request) (int, string) { return 200, page })
			code, out, stderr := execute("rank", "--repo", resultTestRepo, "--order", "failed_reports.desc", "--framework", "go")
			if code != 1 || out != "" || !strings.Contains(stderr, "did not confirm the requested test filters") {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestTestsSearchSendsFrameworkAndMinimumDuration(t *testing.T) {
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		query := r.URL.Query()
		if query.Get("framework") != "eq.vitest" || query.Get("duration_ms") != "gte.1" || query.Has("order") {
			t.Errorf("unexpected search request: %s", r.URL)
		}
		return 200, discoveryPage(`{"frameworks":["vitest"],"min_duration_ms":1}`, `null`)
	})
	if code, out, stderr := execute("search", "--repo", resultTestRepo, "--framework", "vitest", "--min-duration", "1"); code != 0 || !strings.Contains(out, "No matching tests.") {
		t.Fatal(code, out, stderr)
	}
}

func TestRepositoryTestFiltersRejectBadInputBeforeRequests(t *testing.T) {
	execute := repositoryTestsDeps(t, func(r *http.Request) (int, string) {
		t.Errorf("unexpected request %s", r.URL)
		return 500, `{}`
	})
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"rank", "--order", "name.asc"}, "'--order'"},
		{[]string{"rank", "--framework", "mocha"}, "'--framework'"},
		{[]string{"search", "--framework", "Go"}, "'--framework'"},
		{[]string{"rank", "--min-duration", "0"}, "'--min-duration'"},
		{[]string{"search", "--min-duration", "0"}, "'--min-duration'"},
		{[]string{"search", "--min-duration", "100000000"}, "'--min-duration'"},
		{[]string{"search", "--min-duration", "100000s"}, "'--min-duration'"},
		{[]string{"rank", "--min-duration", "1667m"}, "'--min-duration'"},
		{[]string{"rank", "--min-duration", "1.5s"}, "a duration such as 500ms, 2s or 1m"},
		{[]string{"search", "--min-duration", "2h"}, "'--min-duration'"},
		{[]string{"search", "--min-duration", "-5"}, "'--min-duration'"},
		{[]string{"search", "--min-duration", "soon"}, "'--min-duration'"},
		{[]string{"rank", "-L", "1001"}, "'--limit'"},
		{[]string{"rank", "  "}, "QUERY"},
		// As before search and rank shared a constructor, --status is checked first.
		{[]string{"search", "  ", "--status", "bogus"}, "'--status'"},
	} {
		code, out, stderr := execute(append(tc.args, "--repo", resultTestRepo)...)
		if code != 2 || out != "" || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: %d %q %q", tc.args, code, out, stderr)
		}
	}
}
