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
	"time"

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

// Every stub avr has installed ends with an HTML comment that starts with this
// text; a SKILL.md without that last line is not ours.
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

func skillManaged(content string) bool {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	return strings.HasPrefix(last, "<!-- "+skillMarker) && strings.HasSuffix(last, "-->")
}

// skillProjectRoot is the top of the git checkout holding the working
// directory, where a committed skill reaches cloud agents.
func skillProjectRoot(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return "", fmt.Errorf("find the git repository: %w", ctx.Err())
	case errors.As(err, &exitErr):
		return "", &ExitError{1, "Error: --project needs a git repository; run it inside the checkout."}
	case err != nil:
		return "", fmt.Errorf("find the git repository: %w", err)
	}
	return filepath.EvalSymlinks(strings.TrimSpace(string(out)))
}

// skillWithin refuses a project path that a committed symlink sends outside
// the repository. The path need not exist yet.
func skillWithin(root, path string) error {
	existing, rest := path, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = filepath.Dir(existing)
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err == nil {
		var rel string
		rel, err = filepath.Rel(root, filepath.Join(resolved, rest))
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return &ExitError{1, "Error: " + path + " leads outside the repository through a symlink."}
}

// skillRoots holds each agent's configuration directory: in the project when
// one is given, otherwise in the home directory.
func skillRoots(d Dependencies, project string) (map[string]string, error) {
	if project != "" {
		return map[string]string{"claude": filepath.Join(project, ".claude"), "agents": filepath.Join(project, ".agents")}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	claude := d.Getenv("CLAUDE_CONFIG_DIR")
	if claude == "" {
		claude = filepath.Join(home, ".claude")
	}
	return map[string]string{"claude": claude, "agents": filepath.Join(home, ".agents")}, nil
}

type skillTarget struct {
	agent, path string
	installed   bool
	content     string
}

func readSkill(target *skillTarget) error {
	info, err := os.Stat(target.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case !info.Mode().IsRegular():
		return &ExitError{1, "Error: " + target.path + " is not a regular file."}
	}
	target.content, err = readTextFile(target.path, "Skill file")
	target.installed = err == nil
	return err
}

func skillTargets(d Dependencies, agents []string, project string, force bool) ([]skillTarget, error) {
	if len(agents) == 0 {
		agents = skillDefaultAgents
	}
	roots, err := skillRoots(d, project)
	if err != nil {
		return nil, err
	}
	targets := []skillTarget{}
	seen := map[string]bool{}
	for _, agent := range agents {
		if err := validateChoice("agent", agent, skillAgents); err != nil {
			return nil, err
		}
		agent = skillAgent(agent)
		if seen[agent] {
			continue
		}
		seen[agent] = true
		target := skillTarget{agent: agent, path: filepath.Join(roots[agent], "skills", skillName, "SKILL.md")}
		if project != "" {
			if err := skillWithin(project, target.path); err != nil {
				return nil, err
			}
		}
		if err := readSkill(&target); err != nil {
			return nil, err
		}
		if target.installed && !force && !skillManaged(target.content) {
			return nil, &ExitError{1, "Error: " + target.path + " was not installed by avr. Pass --force to replace or remove it."}
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// writeSkill keeps a personal skill private. A project skill is committed,
// so it gets the permissions of ordinary repository files.
func writeSkill(path string, project bool) error {
	if !project {
		return writeTextAtomic(path, skillStub, 0o600)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeTextAtomic(path, skillStub, 0o644)
}

func removeSkill(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	// os.Remove keeps a directory that still holds the user's files. A linked
	// directory (a symlink, or a junction on Windows) belongs to the user even
	// when empty, and Lstat reports neither as a directory.
	if info, err := os.Lstat(filepath.Dir(path)); err == nil && info.IsDir() {
		_ = os.Remove(filepath.Dir(path))
	}
	return nil
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

// apply resolves and checks every target before act changes any of them, so
// a refusal leaves all locations untouched. act returns the state to print.
func (o *skillOptions) apply(ctx context.Context, d Dependencies, act func(skillTarget) (string, error)) error {
	project := ""
	if o.project {
		root, err := skillProjectRoot(ctx)
		if err != nil {
			return err
		}
		project = root
	}
	targets, err := skillTargets(d, o.agents, project, o.force)
	if err != nil {
		return err
	}
	for _, target := range targets {
		state, err := act(target)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(d.Out, "%s for %s: %s\n", state, skillAgentLabels[target.agent], target.path); err != nil {
			return err
		}
	}
	return nil
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
		return add.apply(cmd.Context(), d, func(target skillTarget) (string, error) {
			switch {
			case target.content == skillStub:
				return "Already installed", nil
			case target.installed:
				return "Updated", writeSkill(target.path, add.project)
			default:
				return "Installed", writeSkill(target.path, add.project)
			}
		})
	})

	var remove skillOptions
	uninstall := &cobra.Command{Use: "uninstall", Short: "Remove the installed avr skill.", Args: cobra.NoArgs}
	remove.bind(uninstall, "uninstall from")
	uninstall.RunE = run(func(cmd *cobra.Command, _ []string) error {
		return remove.apply(cmd.Context(), d, func(target skillTarget) (string, error) {
			if !target.installed {
				return "Not installed", nil
			}
			return "Uninstalled", removeSkill(target.path)
		})
	})
	c.AddCommand(show, install, uninstall)
	return c
}
