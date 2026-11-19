package version

import (
	"runtime/debug"
	"testing"
)

func TestGoInstalled(t *testing.T) {
	module := func(version string, settings ...debug.BuildSetting) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: version}, Settings: settings}
	}
	for name, tc := range map[string]struct {
		info *debug.BuildInfo
		want bool
	}{
		"go install from the module proxy": {module("v0.8.0"), true},
		"go install of a pseudo-version":   {module("v0.8.1-0.20261119080000-84e3f3d0a1b2"), true},
		"build from a checkout":            {module("v0.8.1-0.20261119080000-84e3f3d0a1b2+dirty", debug.BuildSetting{Key: "vcs.revision", Value: "84e3f3d"}), false},
		"build without module version":     {module("(devel)"), false},
		"another main module":              {&debug.BuildInfo{Main: debug.Module{Path: "example.com/other", Version: "v1.0.0"}}, false},
		"no build info":                    {nil, false},
	} {
		if got := GoInstalled(tc.info); got != tc.want {
			t.Errorf("%s: GoInstalled = %v, want %v", name, got, tc.want)
		}
	}
}
