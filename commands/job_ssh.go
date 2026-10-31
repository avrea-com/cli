package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"
)

var sshHost = lazyRegexp(`^[A-Za-z0-9._:\[\]-]+$`)
var sshUser = lazyRegexp("^[A-Za-z0-9_.-]+$")

func newJobSSH(d Dependencies) *cobra.Command {
	var printCommand, showPassword bool
	c := &cobra.Command{Use: "ssh JOB_ID", Short: "SSH into a running job's VM.", Args: cobra.ExactArgs(1)}
	c.Flags().BoolVar(&printCommand, "print-command", false, "Print the SSH command without connecting.")
	c.Flags().BoolVar(&showPassword, "show-password", false, "Display the SSH password.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		// Forward setup may take longer than a normal read. Copy the client so
		// embedded commands never mutate a caller's transport or timeout.
		httpClient := *api.HTTP
		httpClient.Timeout = 60 * time.Second
		api.HTTP = &httpClient
		rec, err := api.SSHForwarding(cmd.Context(), args[0])
		if err != nil {
			return apiError(cfg, err, "connect to VM")
		}
		host, user, port := rec.Value.Host, rec.Value.Username, rec.Value.Port
		if host == "" || !sshHost().MatchString(host) || strings.HasPrefix(host, "-") || !sshUser().MatchString(user) || strings.HasPrefix(user, "-") || port < 1 || port > 65535 {
			return fmt.Errorf("avrea returned an invalid SSH endpoint")
		}
		argv := []string{"ssh", "-o", "StrictHostKeyChecking=accept-new", "-p", strconv.Itoa(port), user + "@" + host}
		password := fieldText(rec.Fields, "password")
		keys, _ := rec.Fields["ssh_keys_installed"].(bool)
		if printCommand {
			command := shellquote.Join(argv...)
			if password != "" && !keys && showPassword {
				if _, err := exec.LookPath("sshpass"); err == nil {
					command = "sshpass -p " + shellquote.Join(password) + " " + command
				}
			}
			if _, err = fmt.Fprintln(d.Out, output.CellText(command)); err != nil {
				return err
			}
		}
		if password != "" && !keys {
			message := "Password: ******** (hidden; re-run with --show-password to reveal)"
			if showPassword {
				message = "Password: " + output.CellText(password)
			}
			if _, err = fmt.Fprintln(d.Err, message); err != nil {
				return err
			}
		}
		if printCommand {
			return nil
		}
		known, err := vmTempKnownHosts(rec.Fields, host, port)
		if err != nil {
			return err
		}
		if known == "" {
			return &ExitError{1, "Error: The VM did not return an SSH host key; refusing an unverified connection."}
		}
		defer func() { _ = os.Remove(known) }()
		argv = []string{"ssh", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + known, "-p", strconv.Itoa(port), user + "@" + host}
		runner := d.RunSSH
		if runner == nil {
			runner = func(ctx context.Context, args []string) (int, error) { return executeSSH(ctx, d, args) }
		}
		code, err := runner(cmd.Context(), argv)
		if err != nil {
			return err
		}
		if code != 0 {
			return &ExitError{Code: code}
		}
		return nil
	})
	return c
}

func executeSSH(ctx context.Context, d Dependencies, args []string) (int, error) {
	child := exec.CommandContext(ctx, args[0], args[1:]...)
	child.Stdin = d.In
	child.Stdout = d.Out
	child.Stderr = d.Err
	child.Cancel = func() error {
		if err := child.Process.Signal(os.Interrupt); err != nil {
			return child.Process.Kill()
		}
		return nil
	}
	child.WaitDelay = 2 * time.Second
	err := child.Run()
	if ctx.Err() != nil {
		return 130, ctx.Err()
	}
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 1, fmt.Errorf("could not start ssh: %w", err)
}
