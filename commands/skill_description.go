// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func skillDescription(path string) docgen.Description {
	agent := func(help string) docgen.Parameter {
		return docgen.Parameter{Param: docgen.Param{Name: "agents", Kind: "option", Opts: []string{"--agent"}, Type: "choice", Multiple: true, Nargs: 1, Help: descriptionText(help), Choices: skillAgents}, CompletionKind: "Choice", CaseSensitive: true}
	}
	project := docgen.Parameter{Param: docgen.Param{Name: "project", Kind: "option", Opts: []string{"--project"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Use the current git repository instead of your home directory.")}, CompletionKind: "Bool"}
	force := func(help string) docgen.Parameter {
		return docgen.Parameter{Param: docgen.Param{Name: "force", Kind: "option", Opts: []string{"--force"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText(help)}, CompletionKind: "Bool"}
	}
	switch path {
	case "avr skill":
		return docgen.Description{
			Help: "Teach coding agents to use avr.\n" +
				"\n" +
				"The skill follows the open Agent Skills format. It tells an agent when\n" +
				"avr is relevant and points it at avr skill show, so the guidance always\n" +
				"matches the installed avr. Claude Code reads it from .claude/skills;\n" +
				"Codex, OpenCode, Pi, Devin and other Agent Skills tools read it from\n" +
				".agents/skills. For an agent that reads neither, add a line to its\n" +
				"instructions that says to run avr skill show before working with\n" +
				"Avrea CI.\n",
			Short:    "Teach coding agents to use avr.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "install", Description: "Install the avr skill for coding agents.", AutoShort: true},
				{Name: "show", Description: "Print the avr guide for coding agents.", AutoShort: true},
				{Name: "uninstall", Description: "Remove the installed avr skill.", AutoShort: true},
			},
		}
	case "avr skill install":
		return docgen.Description{
			Help: "Install the avr skill for coding agents.\n" +
				"\n" +
				"Writes one SKILL.md per location: ~/.claude/skills/avrea-cli for\n" +
				"Claude Code (or under CLAUDE_CONFIG_DIR) and ~/.agents/skills/avrea-cli\n" +
				"for Codex, OpenCode, Pi, Devin and other Agent Skills tools. Without\n" +
				"--agent, both are installed; codex is accepted for agents. Running it\n" +
				"again updates the file. A SKILL.md that avr did not install is left\n" +
				"alone unless --force is given.\n" +
				"\n" +
				"With --project, the skill goes into .claude/skills and .agents/skills\n" +
				"at the top of the current git repository instead. Commit it so cloud\n" +
				"agents working on the repository, such as Devin or Codex cloud, find it.\n" +
				"Those environments also need avr installed and AVR_TOKEN set to a scoped\n" +
				"or service token.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr skill install\n" +
				"    avr skill install --agent claude\n" +
				"    avr skill install --project --agent agents\n",
			Short: "Install the avr skill for coding agents.",
			Usage: []string{"[OPTIONS]"},
			Parameters: []docgen.Parameter{
				agent("Location to install for (repeatable): claude, or agents for Codex, OpenCode, Pi, Devin and others. Defaults to both."),
				project,
				force("Replace a skill that avr did not install."),
			},
		}
	case "avr skill show":
		return docgen.Description{
			Help: "Print the avr guide for coding agents.\n" +
				"\n" +
				"Without a topic, prints the rules every task follows and the list of\n" +
				"topics. With a topic, prints that part of the guide. The guide belongs\n" +
				"to the installed avr version.\n" +
				"\n" +
				"\b\n" +
				"Topics:\n" +
				"  test      Push a change and follow its CI run, or dispatch a workflow\n" +
				"  diagnose  Find why a run failed\n" +
				"  speed     Find why CI is slow, caches and right-sized runners\n" +
				"  vms       On-demand Linux, macOS and Windows VMs\n",
			Short: "Print the avr guide for coding agents.",
			Usage: []string{"[OPTIONS]", "[TOPIC]"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "topic", Kind: "argument", Opts: []string{"topic"}, Type: "choice", Nargs: 1, Choices: skillTopicNames()}, CompletionKind: "Choice", CaseSensitive: true},
			},
		}
	case "avr skill uninstall":
		return docgen.Description{
			Help: "Remove the installed avr skill.\n" +
				"\n" +
				"Without --agent, the skill is removed from both locations. With\n" +
				"--project, it is removed from the current git repository. Other files\n" +
				"in the skill directory are kept.\n",
			Short: "Remove the installed avr skill.",
			Usage: []string{"[OPTIONS]"},
			Parameters: []docgen.Parameter{
				agent("Location to uninstall from (repeatable): claude or agents. Defaults to both."),
				project,
				force("Remove a skill that avr did not install."),
			},
		}
	}
	return docgen.Description{}
}
