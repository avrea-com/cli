// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func rootDescription(path string) docgen.Description {
	switch path {
	case "avr":
		return docgen.Description{
			Help:         "Avrea on the command line.",
			Short:        "Avrea on the command line.",
			Usage:        []string{"[OPTIONS]", "[COMMAND] [ARGS]..."},
			Sections:     true,
			SectionOrder: []string{"Core Commands", "Setup & Config", "Additional Commands"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "version", Kind: "option", Opts: []string{"--version", "-V"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Show the version and exit.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "no_color", Kind: "option", Opts: []string{"--no-color"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Disable colored output. Also honors NO_COLOR=1."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "verbose", Kind: "option", Opts: []string{"--verbose", "-v"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Show debug information including HTTP requests."), Default: false}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "links", Kind: "option", Opts: []string{"--links"}, Secondary: []string{"--no-links"}, Type: "boolean", Nargs: 1, IsFlag: true, Envvar: "AVR_LINKS", Help: descriptionText("Make IDs clickable via OSC 8 hyperlinks. Auto-disabled off-TTY. Also honors AVR_LINKS=0."), ShowDefault: true, Default: true}, Suffix: "  [default: links]", CompletionKind: "Bool"},
			},
			Commands: []docgen.HelpCommand{
				{Name: "artifact", Description: "Manage workflow artifacts.", Section: descriptionText("Core Commands")},
				{Name: "audit-events", Description: "View audit events for organization writes.", Section: descriptionText("Setup & Config")},
				{Name: "auth", Description: "Authenticate and manage credentials.", Section: descriptionText("Setup & Config")},
				{Name: "billing", Description: "Manage billing, invoices, and payment methods.", Section: descriptionText("Setup & Config")},
				{Name: "cache", Description: "Inspect and manage the Avrea build cache.", Section: descriptionText("Core Commands")},
				{Name: "config", Description: "View and manage CLI configuration.", Section: descriptionText("Setup & Config")},
				{Name: "firewall", Description: "Manage the egress firewall rule list for orgs and repositories.", Section: descriptionText("Setup & Config")},
				{Name: "health", Description: "Check Avrea platform status.", Section: descriptionText("Additional Commands")},
				{Name: "job", Description: "Inspect Avrea job VMs (SSH, metrics, logs).", Section: descriptionText("Core Commands")},
				{Name: "log", Description: "Search across runner execution logs.", Section: descriptionText("Core Commands")},
				{Name: "org", Description: "Manage organizations and installations.", Section: descriptionText("Additional Commands")},
				{Name: "pr", Description: "View and comment on pull requests.", Section: descriptionText("Core Commands")},
				{Name: "repo", Description: "Manage repositories, git mirrors, and public mirrors.", Section: descriptionText("Additional Commands")},
				{Name: "run", Description: "View and manage CI workflow runs.", Section: descriptionText("Core Commands")},
				{Name: "sbom", Description: "View, generate, and download repository SBOMs.", Section: descriptionText("Core Commands")},
				{Name: "settings", Description: "View and toggle cache and runner settings.", Section: descriptionText("Setup & Config")},
				{Name: "status", Description: "Show recent runs, performance stats, and cache health.", Section: descriptionText("Core Commands")},
				{Name: "tests", Description: "Read a job's tests, timings and recorded failure messages.", Section: descriptionText("Core Commands")},
				{Name: "token", Description: "Create and manage scoped access tokens.", Section: descriptionText("Setup & Config")},
				{Name: "vm", Description: "Manage long-running VMs (SSH/RDP/VNC).", Section: descriptionText("Core Commands")},
				{Name: "workflow", Description: "List and view workflow definitions.", Section: descriptionText("Core Commands")},
			},
		}
	}
	return docgen.Description{}
}
