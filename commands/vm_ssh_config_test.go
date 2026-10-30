package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVMSSHConfigPrivateFilesAndIdempotentUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new", "nested", "config")
	original := "Host unrelated\n    HostName 198.51.100.7\n"
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	block, err := vmSSHConfigBlock("fixture", "192.0.2.1", "runner", 30022, "path with spaces/key", filepath.Join(dir, "known hosts"))
	if err != nil {
		t.Fatal(err)
	}
	action, err := vmUpsertSSHBlock(path, "fixture", block)
	if err != nil || action != "added" {
		t.Fatal(action, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	action, err = vmUpsertSSHBlock(path, "fixture", block)
	if err != nil || action != "updated" {
		t.Fatal(action, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || !strings.HasPrefix(string(after), original) || strings.Count(string(after), "# >>> avrea vm fixture >>>") != 1 {
		t.Fatal(string(after))
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatal(info.Mode())
		}
	}
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("OpenSSH is unavailable")
	}
	result, err := exec.CommandContext(t.Context(), ssh, "-G", "-F", path, "fixture").CombinedOutput()
	if err != nil {
		t.Fatal(err, string(result))
	}
	for _, want := range []string{"hostname 192.0.2.1", "user runner", "port 30022", "stricthostkeychecking true", "identityfile path with spaces/key"} {
		if !strings.Contains(string(result), want) {
			t.Fatalf("OpenSSH configuration missing %q", want)
		}
	}
}
func TestVMSSHConfigUnterminatedBlockLeavesFileIntact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	original := "# >>> avrea vm fixture >>>\nHost fixture\n# unrelated text must survive\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := vmUpsertSSHBlock(path, "fixture", "Host fixture\n"); err == nil {
		t.Fatal("unterminated block accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatal("file changed after validation failed")
	}
}
func TestVMSSHConfigRejectsEndpointInjection(t *testing.T) {
	for _, ep := range []map[string]any{
		{"external_ip": "-oProxyCommand=bad", "username": "runner", "external_port": 22},
		{"external_ip": "192.0.2.1", "username": "-oProxyCommand=bad", "external_port": 22},
		{"external_ip": "192.0.2.1\nProxyCommand bad", "username": "runner", "external_port": 22},
		{"external_ip": "192.0.2.1", "username": "runner\nProxyCommand bad", "external_port": 22},
		{"external_ip": "192.0.2.1", "username": "runner", "external_port": 0},
		{"external_ip": "192.0.2.1", "username": "runner", "external_port": 65536},
	} {
		if _, _, _, err := vmSSHAddress(ep); err == nil {
			t.Fatal("unsafe endpoint accepted", ep)
		}
	}
}
func TestVMKnownHostsPreservesOtherPinsAndRestrictsNewDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new", "nested", "known_hosts")
	line, err := vmKnownHostsLine("ssh-ed25519 fixture", "192.0.2.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	if err = vmUpsertKnownHosts(path, line); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte(line+"other ssh-rsa unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	replacement := "192.0.2.1 ssh-ed25519 replacement\n"
	if err = vmUpsertKnownHosts(path, replacement); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "other ssh-rsa unrelated\n"+replacement {
		t.Fatal(string(raw))
	}
	if runtime.GOOS != "windows" {
		for _, entry := range []string{filepath.Join(dir, "new"), filepath.Dir(path)} {
			info, err := os.Stat(entry)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0700 {
				t.Fatal(entry, info.Mode())
			}
		}
	}
}
