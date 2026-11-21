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
	"time"
)

// sshResolution is how the installed ssh would resolve a command line.
type sshResolution struct {
	identitiesOnly string
	identityFiles  []string
}

func installedSSH(t *testing.T) string {
	t.Helper()
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh is not installed")
	}
	return ssh
}

// sshResolve asks ssh how it would resolve args, without connecting. config
// replaces the user's ssh_config so their settings cannot mask the result.
func sshResolve(t *testing.T, ssh, config string, args ...string) sshResolution {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ssh_config")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), ssh, append([]string{"-G", "-F", path}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh -G %v: %v\n%s", args, err, out)
	}
	var resolved sshResolution
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "identitiesonly "); ok {
			resolved.identitiesOnly = value
		}
		if value, ok := strings.CutPrefix(line, "identityfile "); ok {
			resolved.identityFiles = append(resolved.identityFiles, value)
		}
	}
	if resolved.identitiesOnly == "" {
		t.Fatalf("ssh -G printed no identitiesonly:\n%s", out)
	}
	return resolved
}

// fixtureIdentity is a key file that exists: ssh drops an -i path it cannot
// read and lists its default identities instead.
func fixtureIdentity(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vm_key")
	if err := os.WriteFile(path, []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A config that already asks for IdentitiesOnly, to show an omitted -i
// leaves the user's choice in effect.
const sshConfigIdentitiesOnly = "Host *\n    IdentitiesOnly yes\n"

// With an agent holding several keys, ssh offers them before the -i key and
// the server can disconnect with "Too many authentication failures".
func TestVMSSHOffersOnlyTheGivenIdentity(t *testing.T) {
	ssh := installedSSH(t)
	identity := fixtureIdentity(t)
	builders := map[string]func(identity string) []string{
		"vm ssh": func(identity string) []string {
			return vmSSHArgv("192.0.2.1", "runner", 30022, identity, "", nil)
		},
		"vm tunnel": func(identity string) []string {
			return vmTunnelArgv("192.0.2.1", "runner", 30022, []vmForward{{local: 8080, guest: 80}}, identity, "")
		},
	}
	for command, build := range builders {
		t.Run(command, func(t *testing.T) {
			got := sshResolve(t, ssh, "", build(identity)[1:]...)
			if got.identitiesOnly != "yes" || !slices.Equal(got.identityFiles, []string{identity}) {
				t.Errorf("with identity: %+v, want identitiesonly yes and identityfile %s", got, identity)
			}
			if got := sshResolve(t, ssh, sshConfigIdentitiesOnly, build("")[1:]...); got.identitiesOnly != "yes" {
				t.Errorf("without identity: identitiesonly = %s, want the config's yes", got.identitiesOnly)
			}
			if got := sshResolve(t, ssh, "", build("")[1:]...); got.identitiesOnly != "no" {
				t.Errorf("without identity or config: identitiesonly = %s, want no", got.identitiesOnly)
			}
		})
	}
	t.Run("vm ssh-config", func(t *testing.T) {
		block, err := vmSSHConfigBlock("fixture", "192.0.2.1", "runner", 30022, identity, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := sshResolve(t, ssh, block, "fixture"); got.identitiesOnly != "yes" || !slices.Equal(got.identityFiles, []string{identity}) {
			t.Errorf("with identity: %+v, want identitiesonly yes and identityfile %s\n%s", got, identity, block)
		}
		block, err = vmSSHConfigBlock("fixture", "192.0.2.1", "runner", 30022, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if got := sshResolve(t, ssh, block+sshConfigIdentitiesOnly, "fixture"); got.identitiesOnly != "yes" {
			t.Errorf("without identity: identitiesonly = %s, want the config's yes\n%s", got.identitiesOnly, block)
		}
	})
}

func TestBootstrapSSHOffersOnlyTheGivenIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable stub")
	}
	ssh := installedSSH(t)
	identity := fixtureIdentity(t)
	directory := t.TempDir()
	captured := filepath.Join(directory, "argv")
	script := "#!/bin/sh\nfor arg in \"$@\"; do printf '%s\\n' \"$arg\"; done > '" + captured + "'\n"
	if err := os.WriteFile(filepath.Join(directory, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	d, _, _ := testDeps(t)
	d.defaults()
	ep := map[string]any{"external_ip": "192.0.2.1", "external_port": float64(22), "username": "runner"}

	code, err := bootstrapSSH(context.Background(), d, ep, []string{"true"}, identity, "", 30*time.Second, true)
	if code != 0 || err != nil {
		t.Fatal(code, err)
	}

	raw, err := os.ReadFile(captured)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if got := sshResolve(t, ssh, "", args...); got.identitiesOnly != "yes" || !slices.Equal(got.identityFiles, []string{identity}) {
		t.Errorf("bootstrap: %+v, want identitiesonly yes and identityfile %s (argv %v)", got, identity, args)
	}
}

func TestVMSSHConfigWarnsOfAnEarlierIdentitiesOnlyNo(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		want   bool
	}{
		{"catch-all turns it off", "Host *\n    IdentitiesOnly no\n", true},
		{"catch-all turns it off with equals", "Host *\n    identitiesonly=No\n", true},
		{"catch-all turns it on", "Host *\n    IdentitiesOnly yes\n", false},
		{"first catch-all value wins", "Host *\n    IdentitiesOnly yes\nHost *\n    IdentitiesOnly no\n", false},
		{"another host turns it off", "Host other\n    IdentitiesOnly no\n", false},
		{"not set", "Host *\n    StrictHostKeyChecking no\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := vmIdentitiesOnlyOverridden(path)
			if err != nil || got != tc.want {
				t.Fatalf("vmIdentitiesOnlyOverridden = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}
