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
	"slices"
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

// resolveSkillPath resolves the symlinks in a path that need not exist yet:
// the missing components are appended to the deepest existing ancestor.
func resolveSkillPath(path string) (string, error) {
	existing, rest := path, ""
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if !errors.Is(err, fs.ErrNotExist) || parent == existing {
			return "", err
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, rest), nil
}

// skillWithin reports whether a resolved path stays inside the repository,
// which a committed symlink could otherwise send elsewhere.
func skillWithin(root, resolved string) bool {
	rel, err := filepath.Rel(root, resolved)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

// A target is one SKILL.md. Agents whose locations share a directory through
// a link name the same file, so a target can serve several agents.
type skillTarget struct {
	labels    []string
	path      string
	installed bool
	content   string
}

func readSkill(target *skillTarget, force bool) error {
	info, err := os.Lstat(target.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case info.Mode()&os.ModeSymlink != 0 && !force:
		// Writing replaces the link with a file, so its source would go stale.
		return &ExitError{1, "Error: " + target.path + " is a symlink. Pass --force to replace or remove it."}
	}
	if info, err = os.Stat(target.path); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
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
	byFile := map[string]int{}
	for _, agent := range agents {
		agent = skillAgent(agent)
		path := filepath.Join(roots[agent], "skills", skillName, "SKILL.md")
		resolved, err := resolveSkillPath(path)
		if project != "" && (err != nil || !skillWithin(project, resolved)) {
			return nil, &ExitError{1, "Error: " + path + " leads outside the repository through a symlink."}
		}
		if err != nil {
			return nil, err
		}
		if i, ok := byFile[resolved]; ok {
			if label := skillAgentLabels[agent]; !slices.Contains(targets[i].labels, label) {
				targets[i].labels = append(targets[i].labels, label)
			}
			continue
		}
		byFile[resolved] = len(targets)
		target := skillTarget{labels: []string{skillAgentLabels[agent]}, path: path}
		if err := readSkill(&target, force); err != nil {
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
	for _, agent := range o.agents {
		if err := validateChoice("agent", agent, skillAgents); err != nil {
			return err
		}
	}
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
		if _, err = fmt.Fprintf(d.Out, "%s for %s: %s\n", state, strings.Join(target.labels, " and "), target.path); err != nil {
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
