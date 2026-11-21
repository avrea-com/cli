package commands

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
	"github.com/avrea-com/cli/plugins"
	"github.com/spf13/cobra"
)

var skillStub = plugins.AvreaSkill

// The installed skill only points at `avr skill show`, so the guide an agent
// reads always belongs to the avr version it is about to run. The stub names
// no topic, so a stub newer than the installed avr still works.
//
//go:embed skill/guide
var skillGuideFiles embed.FS

// skillTopics are the guide sections an agent reads one at a time, in the
// order `avr skill show` lists them. Each has skill/guide/<name>.md.
var skillTopics = []struct{ name, summary string }{
	{"test", "push a change and follow its CI run, or dispatch a workflow"},
	{"diagnose", "find why a run failed: diagnosis, logs, tests, metrics and reruns"},
	{"speed", "find why CI is slow: timings, caches and right-sized runners"},
	{"vms", "create, use and delete on-demand Linux, macOS and Windows VMs"},
}

func skillTopicNames() []string {
	names := []string{}
	for _, topic := range skillTopics {
		names = append(names, topic.name)
	}
	return names
}

// skillGuide returns one topic, or without one the shared rules and the
// topic index.
func skillGuide(topic string) string {
	if topic != "" {
		data, _ := skillGuideFiles.ReadFile("skill/guide/" + topic + ".md")
		return string(data)
	}
	data, _ := skillGuideFiles.ReadFile("skill/guide/shared.md")
	index := "\n## Topics\n\nRead the topic for your task:\n\n"
	for _, topic := range skillTopics {
		index += "- `avr skill show " + topic.name + "`: " + topic.summary + "\n"
	}
	return string(data) + index
}

const skillName = "avrea-cli"

// Every stub avr has installed ends with an HTML comment that starts with this
// text; a SKILL.md without that last line is not ours.
const skillMarker = "Managed by `avr skill install`"

// skillAgentDir names the directory holding an agent's skills folder, under
// the home directory and in a repository, as the skills CLI lists them:
// https://github.com/vercel-labs/skills/blob/main/docs/agents.md
type skillAgentDir struct {
	label, home, project string
}

// "agents" is the shared Agent Skills location (.agents/skills). Codex,
// Cursor, GitHub Copilot, Gemini CLI, Devin, OpenCode, Pi and other tools read
// it besides their own folder, so one copy serves them all. "codex" names it
// for compatibility with earlier documentation. Kiro documents only its own
// folder, so it is installed when named.
var skillAgentDirs = map[string]skillAgentDir{
	"claude": {"Claude Code", ".claude", ".claude"},
	"agents": {"Agent Skills tools (Codex, Cursor, Copilot, Gemini CLI, Devin and others)", ".agents", ".agents"},
	"kiro":   {"Kiro", ".kiro", ".kiro"},
}
var skillAgents = []string{"claude", "agents", "codex", "kiro"}
var skillDefaultAgents = []string{"claude", "agents"}

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
	roots := map[string]string{}
	if project != "" {
		for agent, dir := range skillAgentDirs {
			roots[agent] = filepath.Join(project, dir.project)
		}
		return roots, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	for agent, dir := range skillAgentDirs {
		roots[agent] = filepath.Join(home, dir.home)
	}
	if claude := d.Getenv("CLAUDE_CONFIG_DIR"); claude != "" {
		roots["claude"] = claude
	}
	return roots, nil
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
	case info.Mode()&os.ModeSymlink != 0:
		if !force {
			// Writing replaces the link with a file, so its source would go stale.
			return &ExitError{1, "Error: " + target.path + " is a symlink. Pass --force to replace or remove it."}
		}
		// --force acts on the link itself, so where it points, even nowhere,
		// does not matter.
		target.installed = true
		return nil
	case !info.Mode().IsRegular():
		return &ExitError{1, "Error: " + target.path + " is not a regular file."}
	}
	target.installed = true
	content, err := readTextFile(target.path, "Skill file")
	if err != nil && !force {
		return err
	}
	// With --force, content is only compared with the stub, so a file that
	// cannot be read as text is simply replaced or removed.
	target.content = content
	return nil
}

func skillTargets(d Dependencies, agents []string, project string, force bool) ([]skillTarget, error) {
	roots, err := skillRoots(d, project)
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		agents = skillDefaultAgents
	}
	targets := []skillTarget{}
	byFile := map[string]int{}
	for _, agent := range agents {
		agent = skillAgent(agent)
		path := filepath.Join(roots[agent], "skills", skillName, "SKILL.md")
		// Agents share a target when their skill directories are the same
		// directory. A linked SKILL.md is its own location: --force replaces or
		// removes the link, not the file it points to.
		dir, err := resolveSkillPath(filepath.Dir(path))
		if project != "" {
			// The directory holding SKILL.md is where the command writes and
			// removes, so it must stay in the repository.
			if err != nil {
				return nil, &ExitError{1, "Error: " + path + " cannot be resolved: " + err.Error()}
			}
			if !skillWithin(project, dir) {
				return nil, &ExitError{1, "Error: " + path + " leads outside the repository through a symlink."}
			}
		}
		if err != nil {
			return nil, err
		}
		key := filepath.Join(dir, "SKILL.md")
		if i, ok := byFile[key]; ok {
			if label := skillAgentDirs[agent].label; !slices.Contains(targets[i].labels, label) {
				targets[i].labels = append(targets[i].labels, label)
			}
			continue
		}
		byFile[key] = len(targets)
		target := skillTarget{labels: []string{skillAgentDirs[agent].label}, path: path}
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
	c.Flags().StringArrayVar(&o.agents, "agent", nil, "Agent to "+verb+" (repeatable): claude, agents, kiro.")
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
	show := &cobra.Command{Use: "show [TOPIC]", Short: "Print the avr guide for coding agents.", Args: cobra.MaximumNArgs(1), ValidArgs: skillTopicNames()}
	show.RunE = run(func(_ *cobra.Command, args []string) error {
		topic := ""
		if len(args) == 1 {
			topic = args[0]
			if !slices.Contains(skillTopicNames(), topic) {
				return &ExitError{2, "Error: Invalid value for '[TOPIC]': " + pythonQuote(output.CellText(topic)) + " is not one of '" + strings.Join(skillTopicNames(), "', '") + "'."}
			}
		}
		_, err := fmt.Fprint(d.Out, skillGuide(topic))
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
