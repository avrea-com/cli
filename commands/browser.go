package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/kballard/go-shellquote"
)

// A failed opener must not turn a successful --web lookup into a failed command.
func (d Dependencies) openWeb(ctx context.Context, target string) error {
	if err := d.OpenBrowser(ctx, target); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, err = fmt.Fprintln(d.Err, "Warning: could not open browser; open the URL above manually.")
		return err
	}
	return nil
}

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
	if err := ctx.Err(); err != nil {
		return err
	}
	return startBrowser(ctx, args)
}

func startBrowser(ctx context.Context, args []string) error {
	// Default openers can live as long as the browser window. Do not wait for
	// them or tie their lifetime to this command's context.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func browserArgs(command, target string) ([]string, error) {
	if executable, err := exec.LookPath(command); err == nil {
		return []string{executable, target}, nil
	}
	// Plain BROWSER entries are executable names, including Windows paths and
	// names with spaces. Quotes or a substitution explicitly opt into arguments.
	if !strings.ContainsAny(command, "\"'") && !strings.Contains(command, "%s") {
		if strings.TrimSpace(command) == "" {
			return nil, errors.New("invalid browser command")
		}
		return []string{command, target}, nil
	}
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
