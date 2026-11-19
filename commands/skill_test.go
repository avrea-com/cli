package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// Every command the guide teaches must be accepted by the real command tree,
// so a renamed command or removed flag fails here instead of in an agent.
func TestSkillGuideCommandsParse(t *testing.T) {
	checked := 0
	for _, document := range []string{skillGuide, skillStub} {
		fenced := false
		for _, line := range strings.Split(document, "\n") {
			if strings.HasPrefix(line, "```") {
				fenced = !fenced
				continue
			}
			if !fenced || !strings.HasPrefix(line, "avr ") {
				continue
			}
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
	if code != 0 || stderr != "" || !strings.Contains(out, "avr run diagnose") {
		t.Fatal(code, stderr, out)
	}
}

func TestSkillInstallAndUninstall(t *testing.T) {
	claude, codex := skillTestHome(t)

	code, out, stderr := skillTestRun(t, nil, "install")
	if code != 0 || stderr != "" || !strings.Contains(out, "Installed for Claude Code: "+claude) || !strings.Contains(out, "Installed for Agent Skills tools (Codex, OpenCode, Pi, Devin): "+codex) {
		t.Fatal(code, out, stderr)
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
	for _, action := range []string{"install", "uninstall"} {
		code, out, stderr := skillTestRun(t, nil, action)
		if code != 1 || out != "" || !strings.Contains(stderr, codex) || !strings.Contains(stderr, "--force") {
			t.Fatal(action, code, out, stderr)
		}
		// The refusal comes before any write, including for the other agent.
		if _, err := os.Stat(claude); !os.IsNotExist(err) || skillTestRead(t, codex) != "my own skill" {
			t.Fatal(action, err)
		}
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
	code, out, stderr := skillTestRun(t, nil, "install", "--project")
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
	for _, path := range []string{claudeHome, sharedHome} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("home skill written", path, err)
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
