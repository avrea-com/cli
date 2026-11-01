package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
)

// Only the credential on stdout is needed. Never echo gh's diagnostics, which
// may contain authentication material; bound a malfunctioning helper's output.
type bootstrapTokenBuffer struct{ bytes.Buffer }

func (b *bootstrapTokenBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > 1<<20 {
		return 0, errors.New("credential helper output exceeds 1 MiB")
	}
	return b.Buffer.Write(data)
}
func bootstrapGitHubToken(ctx context.Context) (string, error) {
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, "gh", "auth", "token")
	var stdout bootstrapTokenBuffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	command.WaitDelay = 2 * time.Second
	err := command.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if bounded.Err() != nil {
		return "", &ExitError{1, "Error: `gh auth token` timed out after 15s."}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "", &ExitError{1, "Error: `gh` CLI not found locally; install the GitHub CLI or drop --setup-github."}
	}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return "", &ExitError{1, "Error: failed to run `gh auth token`"}
		}
	}
	token := strings.TrimSpace(stdout.String())
	if err != nil || token == "" {
		return "", &ExitError{1, "Error: no local GitHub token; run `gh auth login` first, or drop --setup-github."}
	}
	return token, nil
}
func bootstrapSSH(ctx context.Context, d Dependencies, ep map[string]any, args []string, identity, stdin string, timeout time.Duration, capture bool) (int, error) {
	host, user, port, err := vmSSHAddress(ep)
	if err != nil {
		return 0, err
	}
	known, err := vmTempKnownHosts(ep, host, port)
	if err != nil {
		return 0, err
	}
	if known != "" {
		defer func() { _ = os.Remove(known) }()
	}
	argv := append(vmSSHArgv(host, user, port, identity, known, []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=15"}), args...)
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(bounded, argv[0], argv[1:]...)
	command.Stdin = strings.NewReader(stdin)
	command.Stdout = d.Out
	command.Stderr = d.Err
	if capture {
		command.Stdout = io.Discard
		command.Stderr = io.Discard
	}
	command.WaitDelay = 2 * time.Second
	err = command.Run()
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if bounded.Err() != nil {
		return 0, bounded.Err()
	}
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 0, err
}
func runBootstrapSteps(ctx context.Context, d Dependencies, ep map[string]any, steps []bootstrapStep, identity string) error {
	for attempt := 0; attempt < 5; attempt++ {
		code, err := bootstrapSSH(ctx, d, ep, []string{"true"}, identity, "", 20*time.Second, true)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil && code == 0 {
			break
		}
		var invalid *ExitError
		if errors.As(err, &invalid) {
			return err
		}
		if attempt == 4 {
			return &ExitError{1, "Error: could not reach the VM over SSH. Confirm it is RUNNING (`avr vm show`) and that your key is authorized."}
		}
		if _, err = fmt.Fprintln(d.Err, "  waiting for SSH to come up ..."); err != nil {
			return err
		}
		if err = d.Wait(ctx, 3*time.Second); err != nil {
			return err
		}
	}
	for index, step := range steps {
		if _, err := fmt.Fprintf(d.Err, "==> [%d/%d] %s\n", index+1, len(steps), output.CellText(step.description)); err != nil {
			return err
		}
		code, err := bootstrapSSH(ctx, d, ep, []string{"bash", "-lc", shellQuotePython(step.script)}, identity, step.stdin, 600*time.Second, false)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return &ExitError{1, "Error: bootstrap step '" + step.name + "' timed out after 600s"}
		}
		if err != nil {
			var invalid *ExitError
			if errors.As(err, &invalid) {
				return err
			}
			return fmt.Errorf("failed to launch ssh: %w", err)
		}
		if code != 0 {
			return &ExitError{1, fmt.Sprintf("Error: bootstrap step '%s' failed (exit %d).", step.name, code)}
		}
	}
	_, err := fmt.Fprintln(d.Err, "Bootstrap complete.")
	return err
}
