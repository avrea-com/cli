package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestStoreInteroperability(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "config", "hosts.json")}
	if err := s.Set("https://z.test", "token", "old"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("https://z.test", "default_org", "org-one"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("https://a.test", "token", "second"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("https://z.test", "token", "new"); err != nil {
		t.Fatal(err)
	}
	h := s.Read()
	if h.Value("https://z.test", "default_org") != "org-one" || h.DefaultHost != "https://z.test" {
		t.Fatalf("lost stored context: %#v", h)
	}
	if !reflect.DeepEqual(h.Order, []string{"https://z.test", "https://a.test"}) {
		t.Fatal(h.Order)
	}
	if runtime.GOOS != "windows" {
		stat, err := os.Stat(s.Path)
		if err != nil {
			t.Fatal(err)
		}
		if stat.Mode().Perm() != 0600 {
			t.Fatal(stat.Mode())
		}
	}
	if _, err := s.Clear("https://z.test"); err != nil {
		t.Fatal(err)
	}
	if s.Read().DefaultHost != "https://a.test" {
		t.Fatal("did not promote next host")
	}
	if _, err := s.Clear("https://a.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path); !os.IsNotExist(err) {
		t.Fatal("empty credential file retained")
	}
}

func TestMalformedStoreAndUnknownFields(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "hosts.json")}
	for _, raw := range []string{`null`, `[]`, `{"hosts": []}`, `{"hosts": null}`, `{"hosts": 4}`, `broken`} {
		if err := os.WriteFile(s.Path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if len(s.Read().Order) != 0 {
			t.Fatal(raw)
		}
		if err := s.Set("https://api.test", "token", "fixture"); err != nil {
			t.Fatal(err)
		}
	}
	raw := `{"default_host":"https://api.test","hosts":{"https://api.test":{"token":"old","future":{"integer":9007199254740993}},"https://bad.test":null}}`
	if err := os.WriteFile(s.Path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("https://api.test", "token", "new"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "9007199254740993") {
		t.Fatal("lost numeric precision")
	}
	if err := s.Set("https://bad.test", "token", "repaired"); err != nil {
		t.Fatal(err)
	}
	if len(s.Read().Order) != 2 {
		t.Fatal("duplicate host on repair")
	}
}

func TestResolution(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "hosts.json")}
	if err := s.Set("https://saved.test", "token", "saved-token"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("https://saved.test", "default_org", "org-saved"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		env              map[string]string
		host, token, org string
	}{
		{map[string]string{}, "https://saved.test", "saved-token", "org-saved"},
		{map[string]string{"AVR_TOKEN": "avs_fixture"}, "https://saved.test", "avs_fixture", ""},
		{map[string]string{"AVR_TOKEN": "avs_fixture", "AVR_ORG": "org-explicit"}, "https://saved.test", "avs_fixture", "org-explicit"},
		{map[string]string{"AVR_HOST": "https://new.test/", "AVR_TOKEN": "explicit"}, "https://new.test", "explicit", ""},
		{map[string]string{"AVR_HOST": "", "AVR_TOKEN": ""}, "https://saved.test", "saved-token", "org-saved"},
	}
	for _, tt := range tests {
		c, err := Load(s, func(k string) string { return tt.env[k] })
		if err != nil {
			t.Fatal(err)
		}
		if c.Host != tt.host || c.Token != tt.token || c.Org != tt.org {
			t.Fatalf("%v: %#v", tt.env, c)
		}
	}
}

func TestSecureHosts(t *testing.T) {
	for _, host := range []string{"https://api.test", "HTTPS://API.TEST", "http://localhost:8765", "http://127.0.0.1:8888", "http://[::1]:80"} {
		if err := ValidateHost(host); err != nil {
			t.Errorf("%s: %v", host, err)
		}
	}
	for _, host := range []string{"http://remote.test", "ftp://api.test", "api.test", "https://u:p@api.test", "https://api.test\n", "https://api.test:99999", "https://api.test:not-a-port", "https://"} {
		if err := ValidateHost(host); err == nil {
			t.Errorf("accepted %q", host)
		}
	}
}

func TestConfigPaths(t *testing.T) {
	for _, tt := range []struct {
		platform string
		env      map[string]string
		suffix   []string
	}{
		{"linux", map[string]string{}, []string{".config", "avrea"}},
		{"darwin", map[string]string{}, []string{".config", "avrea"}},
		{"darwin", map[string]string{"AVR_MACOS_NATIVE_PATHS": "1"}, []string{"Library", "Application Support", "avrea"}},
		{"windows", map[string]string{}, []string{"AppData", "Local", "avrea", "avrea"}},
	} {
		home := t.TempDir()
		got := ConfigDir(func(k string) string { return tt.env[k] }, home, tt.platform)
		want := filepath.Join(append([]string{home}, tt.suffix...)...)
		if got != want {
			t.Fatalf("%s: %s != %s", tt.platform, got, want)
		}
	}
}

func TestInvalidStoredDefaultRecovery(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "hosts.json")}
	if err := s.Set("http://remote.test", "token", "fixture"); err != nil {
		t.Fatal(err)
	}
	_, err := Load(s, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "AVR_HOST") {
		t.Fatal(err)
	}
}
