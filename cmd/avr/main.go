package main

import (
	"context"
	"os"

	"github.com/avrea-com/cli/commands"
)

var version = "0.5.0-go-preview"
var commit = "dev"

func main() {
	commands.IgnoreBrokenPipe()
	ctx, stop := commands.InterruptContext(context.Background())
	code := commands.Execute(ctx, os.Args[1:], commands.Dependencies{Version: version + "+" + commit})
	stop()
	os.Exit(code)
}
