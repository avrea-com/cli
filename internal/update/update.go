// Package update provides advisory release notices, never update installation.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const releasesURL = "https://api.github.com/repos/avrea-com/cli/releases/latest"
const maxAge = 24 * time.Hour

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?$`)

type Checker struct {
	Version string
	// Overrides are for isolated tests. Production always uses the public feed.
	cachePath  string
	endpoint   string
	client     *http.Client
	now        func() time.Time
	executable string
}

type cache struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest"`
}

func New(version string) Checker { return Checker{Version: version} }

// Start does no filesystem or network work on the command goroutine. The
// invocation may finish before a check does; the caller never joins this worker.
func (c Checker) Start(ctx context.Context) <-chan string {
	notices := make(chan string, 1)
	go func() {
		defer close(notices)
		if notice := c.check(ctx); notice != "" && ctx.Err() == nil {
			notices <- notice
		}
	}()
	return notices
}

func (c Checker) check(ctx context.Context) string {
	if !versionPattern.MatchString(c.Version) {
		return ""
	}
	if c.now == nil {
		c.now = time.Now
	}
	now := c.now()
	if c.cachePath == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return ""
		}
		c.cachePath = filepath.Join(dir, "avr", "update.json")
	}
	var saved cache
	if f, err := os.Open(c.cachePath); err == nil {
		_ = json.NewDecoder(io.LimitReader(f, 4096)).Decode(&saved)
		_ = f.Close()
	}
	age := now.Sub(saved.Checked)
	if saved.Checked.IsZero() || age < 0 || age >= maxAge {
		// Remember failed attempts too, so an outage is not retried every command.
		saved.Checked = now
		c.save(saved)
		if latest := c.latest(ctx); latest != "" {
			saved.Latest = latest
		}
		c.save(saved)
	}
	if !newer(saved.Latest, c.Version) {
		return ""
	}
	executable := c.executable
	if executable == "" {
		executable, _ = os.Executable()
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	return fmt.Sprintf("A new version of avr is available: %s → %s. %s", c.Version, saved.Latest, instruction(executable))
}

func (c Checker) latest(ctx context.Context) string {
	endpoint := c.endpoint
	if endpoint == "" {
		endpoint = releasesURL
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "avr-cli/"+c.Version)
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release) != nil || release.Draft || release.Prerelease {
		return ""
	}
	tag := strings.TrimPrefix(release.Tag, "v")
	if !versionPattern.MatchString(tag) || strings.Contains(tag, "-") {
		return ""
	}
	return tag
}

func (c Checker) save(value cache) {
	dir := filepath.Dir(c.cachePath)
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	file, err := os.CreateTemp(dir, ".update-*")
	if err != nil {
		return
	}
	defer func() { _ = os.Remove(file.Name()) }()
	err = json.NewEncoder(file).Encode(value)
	closeErr := file.Close()
	if err == nil && closeErr == nil {
		_ = os.Rename(file.Name(), c.cachePath)
	}
}

func newer(latest, current string) bool {
	a, b := versionPattern.FindStringSubmatch(latest), versionPattern.FindStringSubmatch(current)
	if a == nil || b == nil || a[4] != "" {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, errX := strconv.ParseUint(a[i], 10, 64)
		y, errY := strconv.ParseUint(b[i], 10, 64)
		if errX != nil || errY != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return b[4] != ""
}

func instruction(executable string) string {
	path := strings.ToLower(filepath.ToSlash(executable))
	switch {
	case strings.Contains(path, "/cellar/avr/") || strings.Contains(path, "/linuxbrew/"):
		return "Run: brew upgrade avr"
	case strings.Contains(path, "/mise/installs/"):
		return "Run: mise upgrade github:avrea-com/cli (or update your exact version pin)."
	case strings.Contains(path, "/pipx/venvs/avr-cli/"):
		return "Run: pipx upgrade avr-cli"
	case strings.Contains(path, "/uv/tools/avr-cli/"):
		return "Run: uv tool upgrade avr-cli"
	}
	marker, _ := os.ReadFile(filepath.Join(filepath.Dir(executable), "..", "share", "avr", "install-method"))
	if strings.TrimSpace(string(marker)) == "pypi" {
		return "Run: python -m pip install --upgrade avr-cli (in this environment)."
	}
	return "Install instructions: https://github.com/avrea-com/cli/blob/main/docs/INSTALL.md"
}
