package main

import (
	"context"
	"os"

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
	enableVirtualTerminal(os.Stdout, os.Stderr)
	commands.IgnoreBrokenPipe()
	ctx, stop := commands.InterruptContext(context.Background())
	checker := update.New(version)
	code := commands.Execute(ctx, os.Args[1:], commands.Dependencies{
		Version:          version + "+" + commit,
		StartUpdateCheck: checker.Start,
	})
	stop()
	os.Exit(code)
}
