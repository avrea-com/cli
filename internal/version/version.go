package version

import (
	_ "embed"
	"runtime/debug"
	"strings"
)

const modulePath = "github.com/avrea-com/cli"

//go:embed VERSION
var source string

func Current() string { return strings.TrimSpace(source) }

// GoInstalled reports a binary built by `go install` from a published module
// version. Such a build carries no VCS stamp; a build from a checkout does,
// and release builds also set the commit through ldflags.
func GoInstalled(info *debug.BuildInfo) bool {
	if info == nil || info.Main.Path != modulePath || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return false
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return false
		}
	}
	return true
}
