// Package config loads the shared Python/Go CLI configuration.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const DefaultHost = "https://api.avrea.com"

type Config struct {
	// HostSource records resolution provenance for composed CLI configuration.
	HostSource string
	Host       string
	Token      string
	Org        string
	Repo       string
}

func IsScoped(token string) bool { return strings.HasPrefix(token, "avs_") }

// ConfigDir preserves the Python CLI's platformdirs conventions.
func ConfigDir(getenv func(string) string, home, platform string) string {
	if override := getenv("AVR_CONFIG_DIR"); override != "" {
		if override == "~" {
			return home
		}
		if strings.HasPrefix(override, "~/") || strings.HasPrefix(override, "~\\") {
			return filepath.Join(home, override[2:])
		}
		return override
	}
	if platform == "windows" {
		base := getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "avrea", "avrea")
	}
	if platform == "darwin" && getenv("AVR_MACOS_NATIVE_PATHS") == "1" {
		return filepath.Join(home, "Library", "Application Support", "avrea")
	}
	base := getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "avrea")
}

func DefaultStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &Store{Path: filepath.Join(ConfigDir(os.Getenv, home, runtime.GOOS), "hosts.json")}, nil
}

func Load(store *Store, getenv func(string) string) (Config, error) {
	data := store.Read()
	host := getenv("AVR_HOST")
	hostSource := "AVR_HOST"
	stored := host == "" && data.DefaultHost != ""
	if host == "" {
		host = data.DefaultHost
		hostSource = "hosts.json"
	}
	if host == "" {
		host = DefaultHost
		hostSource = "default"
	}
	host = strings.TrimRight(host, "/")
	if err := ValidateHost(host); err != nil {
		if stored {
			return Config{}, fmt.Errorf("%w This is your stored default host in hosts.json: override it by setting AVR_HOST to a valid https:// URL, or edit hosts.json directly", err)
		}
		return Config{}, err
	}
	c := Config{Host: host, HostSource: hostSource, Token: getenv("AVR_TOKEN"), Org: getenv("AVR_ORG"), Repo: getenv("AVR_REPO")}
	if c.Token == "" {
		c.Token = data.Value(host, "token")
	}
	if c.Org == "" && !IsScoped(c.Token) {
		c.Org = data.Value(host, "default_org")
	}
	return c, nil
}

func ValidateHost(host string) error {
	reject := func(reason string) error { return fmt.Errorf("refusing to use host %q: %s", host, reason) }
	for _, c := range host {
		if c < 32 || c == 127 {
			return reject("control characters are not allowed in the API host.")
		}
	}
	u, err := url.Parse(host)
	if err != nil {
		return reject("expected a complete https:// URL with a valid port.")
	}
	if u.User != nil {
		return reject("remove the embedded credentials (user:pass@) from the API host.")
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 0 || n > 65535 {
			return reject("the port is not a valid number.")
		}
	}
	if u.Scheme == "https" && u.Hostname() != "" {
		return nil
	}
	local := strings.ToLower(u.Hostname())
	if u.Scheme == "http" && (local == "localhost" || local == "127.0.0.1" || local == "::1") {
		return nil
	}
	if u.Scheme == "http" && local != "" {
		return reject("a plain-http remote host would send the API key in cleartext. Use https:// (http:// is allowed only for localhost).")
	}
	return reject("expected a complete https:// URL such as https://api.avrea.com.")
}
