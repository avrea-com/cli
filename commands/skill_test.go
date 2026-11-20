package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/kballard/go-shellquote"
)

func skillTestHome(t *testing.T) (claude, codex string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return filepath.Join(home, ".claude", "skills", "avrea-cli", "SKILL.md"), filepath.Join(home, ".agents", "skills", "avrea-cli", "SKILL.md")
}

func skillTestRun(t *testing.T, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	d, out, stderr := testDeps(t)
	d.Getenv = func(key string) string { return env[key] }
	code := Execute(context.Background(), append([]string{"skill"}, args...), d)
	return code, out.String(), stderr.String()
}

func skillTestRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// skillGuideCommands returns the avr commands in fenced blocks and inline code
// spans. A span such as `avr <command> --help` names no real command.
func skillGuideCommands(document string) []string {
	commands := []string{}
	fenced := false
	for _, line := range strings.Split(document, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			if strings.HasPrefix(line, "avr ") {
				commands = append(commands, line)
			}
			continue
		}
		for i, span := range strings.Split(line, "`") {
			if i%2 == 1 && strings.HasPrefix(span, "avr ") && !strings.HasPrefix(span, "avr <") {
				commands = append(commands, span)
			}
		}
	}
	return commands
}

// Every command the guide teaches must be accepted by the real command tree,
// so a renamed command or removed flag fails here instead of in an agent.
func TestSkillGuideCommandsParse(t *testing.T) {
	checked := 0
	documents := []string{skillGuide(""), skillStub}
	for _, topic := range skillTopicNames() {
		documents = append(documents, skillGuide(topic))
	}
	for _, document := range documents {
		for _, line := range skillGuideCommands(document) {
			words, err := shellquote.Split(line)
			if err != nil {
				t.Fatalf("%s: %v", line, err)
			}
			// A redirection belongs to the shell, not to avr.
			for i, word := range words {
				if strings.HasPrefix(word, ">") || strings.HasPrefix(word, "2>") {
					words = words[:i]
					break
				}
			}
			d, _, _ := testDeps(t)
			root := NewRoot(d)
			command, rest, err := root.Find(words[1:])
			if err != nil || !command.Runnable() {
				t.Errorf("%s: no such command (%v)", line, err)
				continue
			}
			// vm ssh passes unknown options to ssh, so a misspelt avr flag before
			// the separator would not fail. Require each one to exist.
			if command.DisableFlagParsing {
				command.Flags().AddFlagSet(command.InheritedFlags())
				for _, word := range rest {
					if word == "--" {
						break
					}
					name, _, _ := strings.Cut(strings.TrimPrefix(word, "--"), "=")
					if strings.HasPrefix(word, "--") && command.Flags().Lookup(name) == nil {
						t.Errorf("%s: unknown flag %s", line, word)
					}
				}
				checked++
				continue
			}
			if err = command.ParseFlags(rest); err != nil {
				t.Errorf("%s: %v", line, err)
				continue
			}
			if err = command.ValidateArgs(command.Flags().Args()); err != nil {
				t.Errorf("%s: %v", line, err)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("the guide contains no commands")
	}
}

func TestSkillShowNeedsNoCredentials(t *testing.T) {
	code, out, stderr := skillTestRun(t, nil, "show")
	if code != 0 || stderr != "" || !strings.Contains(out, "avr skill show diagnose") {
		t.Fatal(code, stderr, out)
	}
	code, out, stderr = skillTestRun(t, nil, "show", "diagnose")
	if code != 0 || stderr != "" || !strings.Contains(out, "avr run diagnose") {
		t.Fatal(code, stderr, out)
	}
}

// The index and the embedded files must agree: a listed topic without a file
// would print nothing, and a file without a topic could never be read.
func TestSkillTopicsMatchTheGuideFiles(t *testing.T) {
	entries, err := skillGuideFiles.ReadDir("skill/guide")
	if err != nil {
		t.Fatal(err)
	}
	files := []string{}
	for _, entry := range entries {
		files = append(files, strings.TrimSuffix(entry.Name(), ".md"))
	}
	want := append([]string{"shared"}, skillTopicNames()...)
	slices.Sort(files)
	slices.Sort(want)
	if !slices.Equal(files, want) {
		t.Fatalf("guide files %v, topics %v", files, want)
	}
	shared := skillGuide("")
	for _, topic := range skillTopicNames() {
		if !strings.Contains(shared, "`avr skill show "+topic+"`") || !strings.HasPrefix(skillGuide(topic), "# ") {
			t.Fatal(topic)
		}
	}
}

func TestSkillShowRejectsAnUnknownTopic(t *testing.T) {
	code, out, stderr := skillTestRun(t, nil, "show", "bogus")
	if code != 2 || out != "" || !strings.Contains(stderr, "'bogus' is not one of 'test', 'diagnose'") {
		t.Fatal(code, out, stderr)
	}
}

func TestSkillInstallAndUninstall(t *testing.T) {
	claude, codex := skillTestHome(t)

	code, out, stderr := skillTestRun(t, nil, "install")
	if code != 0 || stderr != "" || !strings.Contains(out, "Installed for Claude Code: "+claude) || !strings.Contains(out, "Installed for Agent Skills tools (Codex, OpenCode, Pi, Devin): "+codex) {
		t.Fatal(code, out, stderr)
	}
	if runtime.GOOS != "windows" {
		for _, path := range []string{claude, codex} {
			file, fileErr := os.Stat(path)
			dir, dirErr := os.Stat(filepath.Dir(path))
			if fileErr != nil || dirErr != nil || file.Mode().Perm() != 0o600 || dir.Mode().Perm() != 0o700 {
				t.Fatal("personal skill must be private", path, file, dir)
			}
		}
	}
	installed := skillTestRead(t, claude)
	// Agents load a skill only when its name matches the directory and it can
	// lead them to the guide.
	if !strings.HasPrefix(installed, "---\nname: avrea-cli\ndescription: ") || !strings.Contains(installed, "avr skill show") || skillTestRead(t, codex) != installed {
		t.Fatal(installed)
	}

	if code, out, _ = skillTestRun(t, nil, "install"); code != 0 || strings.Count(out, "Already installed for ") != 2 {
		t.Fatal(code, out)
	}

	// A stub left by another avr version carries the marker and is replaced.
	if err := os.WriteFile(claude, []byte("old\n<!-- "+skillMarker+". -->\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, out, _ = skillTestRun(t, nil, "install", "--agent", "claude"); code != 0 || out != "Updated for Claude Code: "+claude+"\n" || skillTestRead(t, claude) != installed {
		t.Fatal(code, out)
	}

	beside := filepath.Join(filepath.Dir(codex), "notes.md")
	if err := os.WriteFile(beside, []byte("mine"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, out, stderr = skillTestRun(t, nil, "uninstall"); code != 0 || stderr != "" || strings.Count(out, "Uninstalled for ") != 2 {
		t.Fatal(code, out, stderr)
	}
	if _, err := os.Stat(filepath.Dir(claude)); !os.IsNotExist(err) {
		t.Fatal("empty skill directory remains", err)
	}
	if _, err := os.Stat(codex); !os.IsNotExist(err) || skillTestRead(t, beside) != "mine" {
		t.Fatal("uninstall must remove only SKILL.md", err)
	}
	if code, out, _ = skillTestRun(t, nil, "uninstall"); code != 0 || strings.Count(out, "Not installed for ") != 2 {
		t.Fatal(code, out)
	}
}

func TestSkillLeavesForeignSkillAlone(t *testing.T) {
	claude, codex := skillTestHome(t)
	if err := os.MkdirAll(filepath.Dir(codex), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, []byte("my own skill"), 0600); err != nil {
		t.Fatal(err)
	}
	// The refusal comes before any write, including for the other agent.
	if code, out, stderr := skillTestRun(t, nil, "install"); code != 1 || out != "" || !strings.Contains(stderr, codex) || !strings.Contains(stderr, "--force") {
		t.Fatal(code, out, stderr)
	}
	if _, err := os.Stat(claude); !os.IsNotExist(err) || skillTestRead(t, codex) != "my own skill" {
		t.Fatal(err)
	}
	// Likewise uninstall keeps the other agent's skill.
	if code, _, stderr := skillTestRun(t, nil, "install", "--agent", "claude"); code != 0 {
		t.Fatal(code, stderr)
	}
	if code, out, stderr := skillTestRun(t, nil, "uninstall"); code != 1 || out != "" || !strings.Contains(stderr, codex) {
		t.Fatal(code, out, stderr)
	}
	if skillTestRead(t, claude) != skillStub || skillTestRead(t, codex) != "my own skill" {
		t.Fatal("uninstall changed a skill before refusing")
	}
	if code, _, stderr := skillTestRun(t, nil, "install", "--agent", "codex", "--force"); code != 0 || !strings.Contains(skillTestRead(t, codex), skillMarker) {
		t.Fatal(code, stderr)
	}
}

func TestSkillAgentSelection(t *testing.T) {
	claude, codex := skillTestHome(t)
	custom := filepath.Join(t.TempDir(), "claude-config")
	code, out, stderr := skillTestRun(t, map[string]string{"CLAUDE_CONFIG_DIR": custom}, "install", "--agent", "claude")
	want := filepath.Join(custom, "skills", "avrea-cli", "SKILL.md")
	if code != 0 || out != "Installed for Claude Code: "+want+"\n" {
		t.Fatal(code, out, stderr)
	}
	for _, path := range []string{claude, codex} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal(path, err)
		}
	}
	if code, _, stderr = skillTestRun(t, nil, "install", "--agent", "cursor"); code != 2 || !strings.Contains(stderr, "claude, agents, codex") {
		t.Fatal(code, stderr)
	}
}

// A skill directory linked from elsewhere, such as a dotfiles checkout, is the
// user's: uninstall removes only the SKILL.md avr wrote and keeps the link.
func TestSkillUninstallKeepsLinkedDirectory(t *testing.T) {
	_, codex := skillTestHome(t)
	target := filepath.Join(t.TempDir(), "dotfiles-skill")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(codex)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Dir(codex)); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := skillTestRun(t, nil, "install", "--agent", "codex"); code != 0 {
		t.Fatal(code, stderr)
	}
	if code, _, stderr := skillTestRun(t, nil, "uninstall", "--agent", "codex"); code != 0 {
		t.Fatal(code, stderr)
	}
	if info, err := os.Lstat(filepath.Dir(codex)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link removed: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("SKILL.md left behind: %v", err)
	}
}

func TestSkillRepeatedAgentIsHandledOnce(t *testing.T) {
	skillTestHome(t)
	for _, action := range []string{"install", "uninstall"} {
		code, out, stderr := skillTestRun(t, nil, action, "--agent", "claude", "--agent", "claude")
		if code != 0 || strings.Count(out, "Claude Code") != 1 {
			t.Fatalf("%s: code %d out %q stderr %q", action, code, out, stderr)
		}
	}
}

// codex predates the shared location's name and must keep writing there.
func TestSkillCodexIsTheSharedLocation(t *testing.T) {
	_, shared := skillTestHome(t)
	code, out, stderr := skillTestRun(t, nil, "install", "--agent", "codex", "--agent", "agents")
	if code != 0 || out != "Installed for Agent Skills tools (Codex, OpenCode, Pi, Devin): "+shared+"\n" {
		t.Fatal(code, out, stderr)
	}
}

func TestSkillProjectInstallsAtRepositoryTop(t *testing.T) {
	claudeHome, sharedHome := skillTestHome(t)
	repo := t.TempDir()
	if out, err := exec.CommandContext(t.Context(), "git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	nested := filepath.Join(repo, "src", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	top, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(top, ".claude", "skills", "avrea-cli", "SKILL.md")
	shared := filepath.Join(top, ".agents", "skills", "avrea-cli", "SKILL.md")
	elsewhere := filepath.Join(t.TempDir(), "claude-config")
	code, out, stderr := skillTestRun(t, map[string]string{"CLAUDE_CONFIG_DIR": elsewhere}, "install", "--project")
	if code != 0 || !strings.Contains(out, claude) || !strings.Contains(out, shared) {
		t.Fatal(code, out, stderr)
	}
	for _, path := range []string{claude, shared} {
		if skillTestRead(t, path) != skillStub {
			t.Fatal("unexpected content", path)
		}
		// Committed files must be readable by everyone who clones the
		// repository. Windows has no Unix permission bits to check.
		if runtime.GOOS != "windows" {
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
				t.Fatal(path, info, err)
			}
		}
	}
	for _, path := range []string{claudeHome, sharedHome, elsewhere} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("skill written outside the project", path, err)
		}
	}
	if code, out, _ = skillTestRun(t, nil, "uninstall", "--project"); code != 0 || strings.Count(out, "Uninstalled for ") != 2 {
		t.Fatal(code, out)
	}
	if _, err := os.Stat(shared); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestSkillProjectNeedsRepository(t *testing.T) {
	skillTestHome(t)
	t.Chdir(t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	if code, _, stderr := skillTestRun(t, nil, "install", "--project"); code != 1 || !strings.Contains(stderr, "needs a git repository") {
		t.Fatal(code, stderr)
	}
}

// The Agent Skills format caps a description at 1024 characters; agents
// ignore or truncate a longer one.
func TestSkillDescriptionFitsAgentSkillsLimit(t *testing.T) {
	for _, line := range strings.Split(skillStub, "\n") {
		if description, ok := strings.CutPrefix(line, "description: "); ok {
			if n := len([]rune(description)); n == 0 || n > 1024 {
				t.Fatalf("description is %d characters", n)
			}
			return
		}
	}
	t.Fatal("no description in the skill frontmatter")
}

func skillTestRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if out, err := exec.CommandContext(t.Context(), "git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	t.Chdir(repo)
	return repo
}

// A cloned repository decides what its .claude and .agents paths are, so a
// committed symlink must not let --project write or delete outside it.
func TestSkillProjectStaysInsideTheRepository(t *testing.T) {
	skillTestHome(t)
	repo := skillTestRepository(t)
	outside := t.TempDir()
	victim := filepath.Join(outside, "skills", "avrea-cli", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(victim), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte(skillStub), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, ".claude")); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"install", "--project"}, {"uninstall", "--project", "--force"}} {
		code, out, stderr := skillTestRun(t, nil, args...)
		if code != 1 || out != "" || !strings.Contains(stderr, "outside the repository") {
			t.Fatal(args, code, out, stderr)
		}
	}
	if skillTestRead(t, victim) != skillStub {
		t.Fatal("file outside the repository changed")
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents")); !os.IsNotExist(err) {
		t.Fatal("refusal must come before any write", err)
	}
	// A link that stays inside the repository is fine.
	if err := os.Remove(filepath.Join(repo, ".claude")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "config", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, "config", "claude"), filepath.Join(repo, ".claude")); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := skillTestRun(t, nil, "install", "--project", "--agent", "claude"); code != 0 {
		t.Fatal(code, stderr)
	}
	if skillTestRead(t, filepath.Join(repo, "config", "claude", "skills", "avrea-cli", "SKILL.md")) != skillStub {
		t.Fatal("skill not written through the in-repository link")
	}
}

// Reading a device or a pipe would never finish.
func TestSkillRefusesNonRegularFile(t *testing.T) {
	claude, _ := skillTestHome(t)
	if err := os.MkdirAll(claude, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"install", "uninstall"} {
		code, _, stderr := skillTestRun(t, nil, action, "--agent", "claude", "--force")
		if code != 1 || !strings.Contains(stderr, "not a regular file") {
			t.Fatal(action, code, stderr)
		}
	}
}

// Only the closing comment marks a stub as avr's. A user's skill that quotes
// the marker keeps its protection.
func TestSkillMarkerMustBeTheLastLine(t *testing.T) {
	claude, _ := skillTestHome(t)
	if err := os.MkdirAll(filepath.Dir(claude), 0o700); err != nil {
		t.Fatal(err)
	}
	mine := "My notes: avr writes <!-- " + skillMarker + ". --> at the end.\nMore notes.\n"
	if err := os.WriteFile(claude, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"install", "uninstall"} {
		if code, _, stderr := skillTestRun(t, nil, action, "--agent", "claude"); code != 1 || !strings.Contains(stderr, "--force") {
			t.Fatal(action, code, stderr)
		}
	}
	if skillTestRead(t, claude) != mine {
		t.Fatal("user skill changed")
	}
}

// Sharing one skills directory between agents through a link is common; both
// agents then name the same file, which must be handled once.
func TestSkillSharedDirectoryIsHandledOnce(t *testing.T) {
	claude, shared := skillTestHome(t)
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(shared)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(filepath.Dir(claude))), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(filepath.Dir(shared)), filepath.Dir(filepath.Dir(claude))); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"install", "uninstall"} {
		code, out, stderr := skillTestRun(t, nil, action)
		if code != 0 || strings.Count(out, "\n") != 1 || !strings.Contains(out, "Claude Code and Agent Skills tools") {
			t.Fatalf("%s: code %d out %q stderr %q", action, code, out, stderr)
		}
	}
}

// A misspelt agent is a usage error wherever the command runs.
func TestSkillValidatesAgentBeforeLookingForTheRepository(t *testing.T) {
	skillTestHome(t)
	t.Chdir(t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	if code, _, stderr := skillTestRun(t, nil, "install", "--project", "--agent", "bogus"); code != 2 || !strings.Contains(stderr, "--agent") {
		t.Fatal(code, stderr)
	}
}

// A SKILL.md linked from elsewhere, such as a dotfiles checkout, would be
// replaced by a plain file, leaving its source stale.
func TestSkillLinkedFileNeedsForce(t *testing.T) {
	claude, _ := skillTestHome(t)
	source := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(source, []byte(skillStub), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(claude), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, claude); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"install", "uninstall"} {
		if code, _, stderr := skillTestRun(t, nil, action, "--agent", "claude"); code != 1 || !strings.Contains(stderr, "symlink") || !strings.Contains(stderr, "--force") {
			t.Fatal(action, code, stderr)
		}
	}
	if info, err := os.Lstat(claude); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("link replaced", err)
	}
}

// A file link from one agent's SKILL.md to the other's is two locations, not
// one: with --force each is replaced or removed, so neither keeps a stale copy.
func TestSkillLinkedFileToTheOtherAgentIsSeparate(t *testing.T) {
	claude, shared := skillTestHome(t)
	old := "old stub\n<!-- " + skillMarker + ". -->\n"
	for _, dir := range []string{filepath.Dir(claude), filepath.Dir(shared)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(shared, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, claude); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := skillTestRun(t, nil, "install", "--force")
	if code != 0 || strings.Count(out, "\n") != 2 || skillTestRead(t, claude) != skillStub || skillTestRead(t, shared) != skillStub {
		t.Fatalf("install: code %d out %q stderr %q", code, out, stderr)
	}
	if err := os.Remove(claude); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, claude); err != nil {
		t.Fatal(err)
	}
	if code, out, stderr = skillTestRun(t, nil, "uninstall", "--force"); code != 0 || strings.Count(out, "Uninstalled") != 2 {
		t.Fatalf("uninstall: code %d out %q stderr %q", code, out, stderr)
	}
	for _, path := range []string{claude, shared} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("left behind", path, err)
		}
	}
}

// A dangling link inside the repository is not an escape; say what it is.
func TestSkillProjectReportsADanglingLink(t *testing.T) {
	skillTestHome(t)
	repo := skillTestRepository(t)
	if err := os.Symlink(filepath.Join(repo, "missing"), filepath.Join(repo, ".claude")); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := skillTestRun(t, nil, "install", "--project", "--agent", "claude")
	if code != 1 || strings.Contains(stderr, "outside the repository") || !strings.Contains(stderr, "cannot be resolved") {
		t.Fatal(code, stderr)
	}
}

// --force acts on a linked SKILL.md itself, so the link's target may point back
// into the repository; the directory holding the link must still be inside.
func TestSkillProjectForceStaysInsideTheRepository(t *testing.T) {
	skillTestHome(t)
	repo := skillTestRepository(t)
	inside := filepath.Join(repo, "inside.md")
	if err := os.WriteFile(inside, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "avrea-cli")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "SKILL.md")
	if err := os.Symlink(inside, victim); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, ".claude", "skills", "avrea-cli")); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"install", "uninstall"} {
		code, out, stderr := skillTestRun(t, nil, action, "--project", "--force", "--agent", "claude")
		if code != 1 || out != "" || !strings.Contains(stderr, "outside the repository") {
			t.Fatal(action, code, out, stderr)
		}
	}
	if info, err := os.Lstat(victim); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("link outside the repository changed", err)
	}
}

// --force promises to replace or remove what is there, including a link to a
// deleted file and a file that is not text.
func TestSkillForceHandlesUnreadableSkills(t *testing.T) {
	claude, _ := skillTestHome(t)
	if err := os.MkdirAll(filepath.Dir(claude), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, plant := range map[string]func() error{
		"dangling link": func() error { return os.Symlink(filepath.Join(t.TempDir(), "deleted.md"), claude) },
		"not UTF-8":     func() error { return os.WriteFile(claude, []byte{0xff, 0xfe, '\n'}, 0o600) },
	} {
		for _, action := range []string{"install", "uninstall"} {
			if err := plant(); err != nil {
				t.Fatal(err)
			}
			if code, _, stderr := skillTestRun(t, nil, action, "--agent", "claude"); code != 1 || !strings.Contains(stderr, "--force") && !strings.Contains(stderr, "UTF-8") {
				t.Fatal(name, action, "without --force", code, stderr)
			}
			code, out, stderr := skillTestRun(t, nil, action, "--agent", "claude", "--force")
			if code != 0 {
				t.Fatal(name, action, code, stderr)
			}
			if action == "install" {
				if !strings.HasPrefix(out, "Updated") || skillTestRead(t, claude) != skillStub {
					t.Fatal(name, "not replaced", out)
				}
				if err := os.Remove(claude); err != nil {
					t.Fatal(err)
				}
			} else if _, err := os.Lstat(claude); !os.IsNotExist(err) || !strings.HasPrefix(out, "Uninstalled") {
				t.Fatal(name, "not removed", out, err)
			}
		}
		if err := os.MkdirAll(filepath.Dir(claude), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}
