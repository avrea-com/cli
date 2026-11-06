// Package packaging shares the native installer with VM bootstrap.
package packaging

import _ "embed"

// ShellInstaller verifies release archives and installs the native executable.
//
//go:embed install.sh
var ShellInstaller string
