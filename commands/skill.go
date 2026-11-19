package commands

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// The installed skill only points at `avr skill show`, so the guide an agent
// reads always belongs to the avr version it is about to run.
//
//go:embed skill/SKILL.md
var skillStub string

//go:embed skill/guide.md
var skillGuide string

const skillName = "avrea-cli"

// Present in every stub avr has installed; a SKILL.md without it is not ours.
const skillMarker = "Managed by `avr skill install`"

// "agents" is the shared Agent Skills location (.agents/skills) that Codex,
// OpenCode, Pi, Devin and other tools read. "codex" names it for
// compatibility with earlier documentation.
var skillAgents = []string{"claude", "agents", "codex"}
var skillDefaultAgents = []string{"claude", "agents"}
var skillAgentLabels = map[string]string{"claude": "Claude Code", "agents": "Agent Skills tools (Codex, OpenCode, Pi, Devin)"}

func skillAgent(agent string) string {
	if agent == "codex" {
		return "agents"
	}
	return agent
}

// skillProjectRoot is the top of the git checkout holding the working
// directory, where a committed skill reaches cloud agents.
func skillProjectRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", &ExitError{1, "Error: --project needs a git repository; run it inside the checkout."}
	}
	return strings.TrimSpace(string(out)), nil
}

func skillPath(d Dependencies, agent, project string) (string, error) {
	var root string
	switch agent {
	case "claude":
		if project != "" {
			root = filepath.Join(project, ".claude")
		} else if root = d.Getenv("CLAUDE_CONFIG_DIR"); root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			root = filepath.Join(home, ".claude")
		}
	case "agents":
		if project != "" {
			root = filepath.Join(project, ".agents")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			root = filepath.Join(home, ".agents")
		}
	default:
		return "", fmt.Errorf("unknown agent %q", agent)
	}
	return filepath.Join(root, "skills", skillName, "SKILL.md"), nil
}

type skillTarget struct {
	agent, path string
	installed   bool
	content     string
}

func skillTargets(d Dependencies, agents []string, project string, force bool) ([]skillTarget, error) {
	if len(agents) == 0 {
		agents = skillDefaultAgents
	}
	targets := []skillTarget{}
	seen := map[string]bool{}
	for _, agent := range agents {
		if err := validateFirewallChoice("agent", agent, skillAgents); err != nil {
			return nil, err
		}
		agent = skillAgent(agent)
		if seen[agent] {
			continue
		}
		seen[agent] = true
		path, err := skillPath(d, agent, project)
		if err != nil {
			return nil, err
		}
		target := skillTarget{agent: agent, path: path}
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			target.installed, target.content = true, string(data)
		case !errors.Is(err, fs.ErrNotExist):
			return nil, err
		}
		if target.installed && !force && !strings.Contains(target.content, skillMarker) {
			return nil, &ExitError{1, "Error: " + path + " was not installed by avr. Pass --force to replace or remove it."}
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// writeSkill keeps a personal skill private. A project skill is committed,
// so it gets the permissions of ordinary repository files.
func writeSkill(path string, project bool) error {
	if !project {
		return vmAtomicText(path, skillStub)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := vmAtomicText(path, skillStub); err != nil {
		return err
	}
	return os.Chmod(path, 0o644)
}

type skillOptions struct {
	agents  []string
	project bool
	force   bool
}

func (o *skillOptions) bind(c *cobra.Command, verb string) {
	c.Flags().StringArrayVar(&o.agents, "agent", nil, "Agent to "+verb+" (repeatable): claude, agents.")
	c.Flags().BoolVar(&o.project, "project", false, "Use the current git repository instead of your home directory.")
	c.Flags().BoolVar(&o.force, "force", false, "Replace or remove a skill that avr did not install.")
}

func (o *skillOptions) targets(ctx context.Context, d Dependencies) ([]skillTarget, error) {
	project := ""
	if o.project {
		root, err := skillProjectRoot(ctx)
		if err != nil {
			return nil, err
		}
		project = root
	}
	return skillTargets(d, o.agents, project, o.force)
}

// NewSkill prints the agent guide and installs the skill that points at it.
func NewSkill(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "skill", Short: "Teach coding agents to use avr."}
	show := &cobra.Command{Use: "show", Short: "Print the avr guide for coding agents.", Args: cobra.NoArgs}
	show.RunE = run(func(*cobra.Command, []string) error {
		_, err := fmt.Fprint(d.Out, skillGuide)
		return err
	})

	var add skillOptions
	install := &cobra.Command{Use: "install", Short: "Install the avr skill for coding agents.", Args: cobra.NoArgs}
	add.bind(install, "install for")
	install.RunE = run(func(cmd *cobra.Command, _ []string) error {
		targets, err := add.targets(cmd.Context(), d)
		if err != nil {
			return err
		}
		for _, target := range targets {
			state := "Installed"
			switch {
			case target.content == skillStub:
				state = "Already installed"
			case target.installed:
				state = "Updated"
			}
			if target.content != skillStub {
				if err = writeSkill(target.path, add.project); err != nil {
					return err
				}
			}
			if _, err = fmt.Fprintf(d.Out, "%s for %s: %s\n", state, skillAgentLabels[target.agent], target.path); err != nil {
				return err
			}
		}
		return nil
	})

	var remove skillOptions
	uninstall := &cobra.Command{Use: "uninstall", Short: "Remove the installed avr skill.", Args: cobra.NoArgs}
	remove.bind(uninstall, "uninstall from")
	uninstall.RunE = run(func(cmd *cobra.Command, _ []string) error {
		targets, err := remove.targets(cmd.Context(), d)
		if err != nil {
			return err
		}
		for _, target := range targets {
			state := "Not installed"
			if target.installed {
				state = "Uninstalled"
				if err = os.Remove(target.path); err != nil {
					return err
				}
				// os.Remove keeps a directory that still holds the user's files. A
				// linked directory belongs to the user even when empty.
				if info, err := os.Lstat(filepath.Dir(target.path)); err == nil && info.Mode()&os.ModeSymlink == 0 {
					_ = os.Remove(filepath.Dir(target.path))
				}
			}
			if _, err = fmt.Fprintf(d.Out, "%s for %s: %s\n", state, skillAgentLabels[target.agent], target.path); err != nil {
				return err
			}
		}
		return nil
	})
	c.AddCommand(show, install, uninstall)
	return c
}
