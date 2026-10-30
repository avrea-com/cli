package commands

import (
	"fmt"
	"github.com/avrea-com/cli/output"
)

func runCIValue(row map[string]any) any {
	if value, ok := row["ci_provider"]; ok {
		return value
	}
	return "github_actions"
}
func runCIProvider(row map[string]any) string { return pythonValue(runCIValue(row)) }
func sourcePlatform(row map[string]any) string {
	if _, ok := row["platform"]; !ok {
		return "github"
	}
	return pythonValue(row["platform"])
}
func jobCIProvider(row map[string]any) string {
	if provider := fieldText(row, "ci_provider"); provider != "" {
		return provider
	}
	switch sourcePlatform(row) {
	case "github":
		return "github_actions"
	case "gitlab":
		return "gitlab_ci"
	default:
		return "unknown"
	}
}
func workflowName(row map[string]any) string {
	workflow, _ := row["workflow"].(map[string]any)
	if name := fieldText(workflow, "name"); name != "" {
		return name
	}
	return fieldText(row, "workflow_name")
}
func runLabel(row map[string]any, id string) string {
	number, ok := row["run_number"]
	if !ok {
		return "#?"
	}
	if number == nil {
		return id
	}
	return "#" + fieldText(row, "run_number")
}
func requireExternalCI(row map[string]any, feature string) error {
	switch row["ci_provider"] {
	case nil, "github_actions", "gitlab_ci":
		return nil
	case "avrea_ci":
		return &ExitError{1, "Error: " + feature + " are not available for Avrea CI yet."}
	default:
		return &ExitError{1, "Error: Unsupported CI provider: " + output.CellText(fmt.Sprint(row["ci_provider"]))}
	}
}
func validateCIProvider(value string) error {
	switch value {
	case "", "github_actions", "gitlab_ci", "avrea_ci":
		return nil
	default:
		return &ExitError{2, "Error: Invalid value for --ci-provider: must be github_actions, gitlab_ci or avrea_ci."}
	}
}
