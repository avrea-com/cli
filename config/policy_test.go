package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCredentialWritesPreserveSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "actual.json")
	if err := os.WriteFile(target, []byte(`{"hosts":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "hosts.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	s := &Store{Path: link}
	if err := s.Set("https://api.test", "token", "fixture"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("credential write replaced symlink", err)
	}
	if (&Store{Path: target}).Read().Value("https://api.test", "token") != "fixture" {
		t.Fatal("target not updated")
	}
}

func TestMalformedCredentialMutationsPreserveFile(t *testing.T) {
	for _, raw := range []string{`broken`, `{"hosts":`, `null`, `{"hosts":[]}`} {
		s := &Store{Path: filepath.Join(t.TempDir(), "hosts.json")}
		if err := os.WriteFile(s.Path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		actions := []func() error{func() error { return s.Set("https://api.test", "token", "fixture") }, func() error { return s.Switch("https://api.test") }, func() error { _, e := s.Clear("https://api.test"); return e }, func() error { _, e := s.ClearDefaultOrg("https://api.test"); return e }}
		for _, action := range actions {
			if action() == nil {
				t.Fatal("accepted malformed credential store")
			}
			after, _ := os.ReadFile(s.Path)
			if string(after) != raw {
				t.Fatal("modified malformed credentials")
			}
		}
	}
}

func TestCredentialWriteInReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions")
	}
	dir := t.TempDir()
	s := &Store{Path: filepath.Join(dir, "hosts.json")}
	if err := os.WriteFile(s.Path, []byte(`{"hosts":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := s.Set("https://api.test", "token", "fixture"); err != nil {
		t.Fatal(err)
	}
	if s.Read().Value("https://api.test", "token") != "fixture" {
		t.Fatal("writable file in read-only directory was not updated")
	}
}

func TestEnvironmentCredentialsBypassUnreadableStore(t *testing.T) {
	s := &Store{Path: t.TempDir()}
	env := map[string]string{"AVR_HOST": "https://api.test", "AVR_TOKEN": "fixture"}
	cfg, err := Load(s, func(k string) string { return env[k] })
	if err != nil || cfg.Token != "fixture" || cfg.Host != "https://api.test" {
		t.Fatal("environment credentials blocked", err)
	}
	delete(env, "AVR_HOST")
	if _, err = Load(s, func(k string) string { return env[k] }); err == nil {
		t.Fatal("partial environment bypassed failure")
	}
}
