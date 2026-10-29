package commands

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/kballard/go-shellquote"
)

func openBrowser(ctx context.Context, target string, getenv func(string) string) error {
	setting := getenv("AVR_BROWSER")
	if setting == "" {
		setting = getenv("BROWSER")
	}
	if setting != "" {
		for _, candidate := range strings.Split(setting, string(os.PathListSeparator)) {
			// Only split arguments. Never evaluate shell expansions or command substitutions.
			args, err := browserArgs(candidate, target)
			if err != nil {
				continue
			}
			if exec.CommandContext(ctx, args[0], args[1:]...).Run() == nil {
				return nil
			}
		}
		return errors.New("could not open configured browser")
	}
	var args []string
	switch runtime.GOOS {
	case "darwin":
		args = []string{"open", target}
	case "windows":
		args = []string{"rundll32.exe", "url.dll,FileProtocolHandler", target}
	default:
		args = []string{"xdg-open", target}
	}
	return exec.CommandContext(ctx, args[0], args[1:]...).Run()
}

func browserArgs(command, target string) ([]string, error) {
	args, err := shellquote.Split(command)
	if err != nil || len(args) == 0 {
		return nil, errors.New("invalid browser command")
	}
	replaced := false
	for i, arg := range args {
		if strings.Contains(arg, "%s") {
			args[i] = strings.ReplaceAll(arg, "%s", target)
			replaced = true
		}
	}
	if !replaced {
		args = append(args, target)
	}
	return args, nil
}
