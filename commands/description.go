package commands

import (
	"github.com/avrea-com/cli/docgen"
	"github.com/spf13/cobra"
	"strings"
)

func descriptionText(value string) *string { return &value }

// Description supplies Go-owned prose and semantic metadata lazily. Command
// membership, executable flags, defaults and aliases come from the live tree.
func Description(c *cobra.Command) docgen.Description {
	path := canonicalHelpPath(c)
	parts := strings.Fields(path)
	group := "root"
	if len(parts) > 1 {
		group = parts[1]
	}
	switch group {
	case "artifact":
		return artifactDescription(path)
	case "audit-events":
		return audit_eventsDescription(path)
	case "auth":
		return authDescription(path)
	case "billing":
		return billingDescription(path)
	case "cache":
		return cacheDescription(path)
	case "config":
		return configDescription(path)
	case "feedback":
		return feedbackDescription(path)
	case "firewall":
		return firewallDescription(path)
	case "health":
		return healthDescription(path)
	case "internal":
		return internalDescription(path)
	case "job":
		return jobDescription(path)
	case "log":
		return logDescription(path)
	case "login":
		return loginDescription(path)
	case "logout":
		return logoutDescription(path)
	case "org":
		return orgDescription(path)
	case "pr":
		return prDescription(path)
	case "repo":
		return repoDescription(path)
	case "run":
		return runDescription(path)
	case "sbom":
		return sbomDescription(path)
	case "settings":
		return settingsDescription(path)
	case "status":
		return statusDescription(path)
	case "tests":
		return testsDescription(path)
	case "token":
		return tokenDescription(path)
	case "vm":
		return vmDescription(path)
	case "workflow":
		return workflowDescription(path)
	case "root":
		return rootDescription(path)
	}
	return docgen.Description{}
}
func commandHelp(c *cobra.Command) (docgen.HelpSpec, bool, error) {
	spec := docgen.Help(c, Description)
	return spec, spec.Help != "", nil
}

// Documentation renders the public tree, including composed live extensions.
func Documentation(root *cobra.Command) (*docgen.Tree, error) {
	return docgen.Build(root, Description, []docgen.Section{docgen.Section{Name: "Core Commands", Commands: []string{"artifact", "status", "run", "pr", "job", "vm", "workflow", "cache", "tests", "sbom", "log"}}, docgen.Section{Name: "Setup & Config", Commands: []string{"auth", "token", "config", "settings", "firewall", "billing", "audit-events"}}, docgen.Section{Name: "Additional Commands", Commands: []string{"repo", "org", "health"}}})
}
