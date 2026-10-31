package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kballard/go-shellquote"
)

var errPagerInterrupted = errors.New("pager interrupted")

func pageOutput(ctx context.Context, d Dependencies, content string, bypass bool) error {
	direct := func() error { _, err := fmt.Fprintln(d.Out, content); return err }
	if bypass || !d.IsTerminal() {
		return direct()
	}
	setting, set := d.LookupEnv("AVR_PAGER")
	if !set {
		setting, set = d.LookupEnv("PAGER")
	}
	if set && setting == "" {
		return direct()
	}
	if !set {
		setting = "less"
	}
	args, err := shellquote.Split(setting)
	if err != nil || len(args) == 0 {
		return fmt.Errorf("invalid pager command")
	}
	binary, err := exec.LookPath(args[0])
	if err != nil {
		return direct()
	}
	// Terminal Ctrl-C belongs to the pager (e.g. aborting a less search).
	// Explicit parent cancellation still uses the bounded teardown below.
	restore := sshOwnsInterrupt(ctx)
	defer restore()
	cmd := exec.CommandContext(ctx, binary, args[1:]...)
	// Give terminal pagers a chance to restore terminal modes on Ctrl-C.
	// Windows does not support Process.Signal(os.Interrupt); use its kill path.
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = strings.NewReader(content + "\n")
	cmd.Stdout = d.Out
	cmd.Stderr = d.Err
	cmd.Env = os.Environ()
	if _, ok := d.LookupEnv("LESS"); !ok {
		cmd.Env = append(cmd.Env, "LESS=FRXK")
	}
	// Quitting the pager, including Ctrl-C, is normal completion of paged
	// output. Other command cancellation still uses the standard exit 130.
	if err = cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Wait()
	if errors.Is(ctx.Err(), context.Canceled) {
		return errPagerInterrupted
	}
	return ctx.Err()
}
