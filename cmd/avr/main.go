package main

import (
	"context"
	"os"
	"runtime/debug"
	"strings"

	"github.com/avrea-com/cli/commands"
	"github.com/avrea-com/cli/internal/update"
	buildversion "github.com/avrea-com/cli/internal/version"
)

var version string
var commit = "dev"

func main() {
	if version == "" {
		version = buildversion.Current()
	}
	display := version + "+" + commit
	info, _ := debug.ReadBuildInfo()
	goInstall := commit == "dev" && buildversion.GoInstalled(info)
	if goInstall {
		// The module version names what `go install` fetched, including a
		// pseudo-version for @main, which the embedded VERSION does not.
		display = strings.TrimPrefix(info.Main.Version, "v")
	}
	enableVirtualTerminal(os.Stdout, os.Stderr)
	commands.IgnoreBrokenPipe()
	ctx, stop := commands.InterruptContext(context.Background())
	checker := update.Checker{Version: version, GoInstall: goInstall}
	code := commands.Execute(ctx, os.Args[1:], commands.Dependencies{
		Version:          display,
		StartUpdateCheck: checker.Start,
	})
	stop()
	os.Exit(code)
}
