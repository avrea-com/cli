package plugins

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/avrea-com/cli/internal/version"
)

// Codex caches an installed plugin by version and refreshes it only when the
// version changes, so the manifest follows the CLI release that ships the skill.
func TestCodexPluginVersionFollowsTheCLI(t *testing.T) {
	data, err := os.ReadFile("avrea-cli/.codex-plugin/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != version.Current() {
		t.Fatalf("plugins/avrea-cli/.codex-plugin/plugin.json version is %q; set it to %q, the version in internal/version/VERSION", manifest.Version, version.Current())
	}
}
