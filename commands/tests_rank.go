package commands

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var testRankFields = []string{"organization_id", "repository_id", "status", "order", "scope", "truncated", "filters", "data", "pagination"}
var testRankOrders = []string{"duration_mean_ms.desc", "failed_reports.desc"}

func newTestsRank(d Dependencies) *cobra.Command {
	var order string
	return newRepositoryTestsCommand(d, repositoryTestRead{
		use:        "rank [QUERY]",
		short:      "Rank a repository's tests by mean duration or failed reports.",
		statusHelp: "Rank only tests with a result of one of these outcomes (repeatable); every searched report still counts.",
		stopped:    "Ranking stopped at 1,000 tests; more match. Narrow the query or filters.",
		same:       "the same query, order and filters",
		fields:     testRankFields,
		flags: func(c *cobra.Command) {
			c.Flags().StringVar(&order, "order", testRankOrders[0], "Ranking order.")
		},
		validate: func(*cobra.Command) error {
			return validateChoice("order", order, testRankOrders)
		},
		read: func(ctx context.Context, api *client.Client, cfg config.Config, repoPath string, selection testSelection, limit int, cursor *string) (map[string]any, error) {
			query := url.Values{"order": {order}}
			selection.apply(query)
			confirmed := func(page map[string]any) bool {
				filters, _ := page["filters"].(map[string]any)
				return page["order"] == order && selection.confirmedBy(filters)
			}
			return walkRepositoryTests(ctx, api, cfg, repoPath+"/tests/statistics", "rank repository tests", query, confirmed, limit, cursor)
		},
		show: showTestRanking,
	})
}

func showTestRanking(w io.Writer, response map[string]any) error {
	var b strings.Builder
	repositoryScopeText(&b, response, "Not every recorded report was searched; the statistics cover only the reports above.")
	switch response["order"] {
	case "failed_reports.desc":
		b.WriteString("Order: most failed reports first\n")
	default:
		b.WriteString("Order: longest mean duration first\n")
	}
	repositoryRowsText(&b, response)
	rows := response["data"].([]any)
	if len(rows) > 0 {
		cells := make([][]string, 0, len(rows))
		for _, item := range rows {
			row := item.(map[string]any)
			stats, _ := row["statistics"].(map[string]any)
			cells = append(cells, []string{testText(row["class_name"]), testText(row["name"]), testText(stats["failed_reports"]) + "/" + testText(stats["reports"]), rankDuration(stats["duration_mean_ms"]), rankDuration(stats["duration_max_ms"]), testText(row["outcome"]), testText(row["job_id"])})
		}
		if err := output.Grid(&b, []string{"Class", "Test", "Failed", "Mean (ms)", "Max (ms)", "Outcome", "Job"}, cells); err != nil {
			return err
		}
		b.WriteString("Failed counts the searched reports in which the test failed or errored, out of those where it ran.\n")
		writeResultCommands(&b, response, rows, testLabel)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// rankDuration says none for a test without a duration sample, as history does.
func rankDuration(value any) string {
	if value == nil {
		return "none"
	}
	return testText(value)
}
