package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var vmSessionName = lazyRegexp(`^[A-Za-z0-9_-]{1,64}$`)

func validateVMSession(value string) error {
	if !vmSessionName().MatchString(value) {
		return &ExitError{2, "Error: --session '" + output.CellText(value) + "' is not a valid session name; use letters, digits, '_' or '-' (max 64 chars)."}
	}
	return nil
}
func vmShellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuotePython(arg)
	}
	return strings.Join(quoted, " ")
}
func vmLoginCommand(args []string) string { return "bash -lc " + shellQuotePython(vmShellJoin(args)) }
func vmSSHArgv(host, user string, port int, identity, known string, extra []string) []string {
	args := []string{"ssh"}
	if identity != "" {
		args = append(args, "-i", identity)
	}
	args = append(args, "-p", strconv.Itoa(port))
	if known != "" {
		args = append(args, "-o", "UserKnownHostsFile="+known, "-o", "StrictHostKeyChecking=yes")
	} else {
		args = append(args, "-o", "StrictHostKeyChecking=accept-new")
	}
	args = append(args, extra...)
	return append(args, user+"@"+host)
}
func vmTempKnownHosts(ep map[string]any, host string, port int) (string, error) {
	key := fieldText(ep, "host_key")
	if key == "" {
		return "", nil
	}
	line, err := vmKnownHostsLine(key, host, port)
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "avr-vm-known-hosts-")
	if err != nil {
		return "", err
	}
	if _, err = io.WriteString(file, line); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", err
	}
	if err = file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

// Click forwards unknown options as remote arguments but still consumes known
// options before "--". Cobra's unknown-flag whitelist drops them, so split the
// known flags explicitly and leave the remote argv intact.
func vmSSHPassthrough(c *cobra.Command, args []string) ([]string, error) {
	known, remote := []string{}, []string{}
	f := c.Flags()
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			remote = append(remote, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "--") {
			name, _, assigned := strings.Cut(arg[2:], "=")
			flag := f.Lookup(name)
			if flag == nil {
				remote = append(remote, arg)
				continue
			}
			if assigned && flag.NoOptDefVal != "" {
				return nil, &ExitError{2, "Error: Option '--" + name + "' does not take a value."}
			}
			known = append(known, arg)
			if flag.NoOptDefVal == "" && !assigned {
				if i+1 == len(args) {
					return nil, &ExitError{2, "Error: Option '--" + name + "' requires an argument."}
				}
				i++
				known = append(known, args[i])
			}
			continue
		}
		if len(arg) > 1 && arg[0] == '-' {
			unknown := ""
			for j := 1; j < len(arg); j++ {
				short := arg[j : j+1]
				flag := f.ShorthandLookup(short)
				if len(arg) > 2 && c.InheritedFlags().ShorthandLookup(short) != nil {
					flag = nil
				}
				if flag == nil {
					unknown += short
					continue
				}
				token := "-" + short
				if flag.NoOptDefVal == "" {
					if j+1 < len(arg) {
						known = append(known, token, arg[j+1:])
					} else {
						if i+1 == len(args) {
							return nil, &ExitError{2, "Error: Option '" + token + "' requires an argument."}
						}
						i++
						known = append(known, token, args[i])
					}
					break
				}
				known = append(known, token)
			}
			if unknown != "" {
				remote = append(remote, "-"+unknown)
			}
			continue
		}
		remote = append(remote, arg)
	}
	if err := f.Parse(known); err != nil {
		return nil, &ExitError{2, "Error: " + err.Error()}
	}
	return remote, nil
}
func newVMSSH(d Dependencies) *cobra.Command {
	var org, identity, session string
	var login, printOnly bool
	c := &cobra.Command{Use: "ssh VM_ID [SSH_ARGS...]", Short: "Open an SSH session or run a command on a VM.", DisableFlagParsing: true}
	orgFlag(c, &org)
	f := c.Flags()
	f.StringVarP(&identity, "identity", "i", "", "SSH private key file.")
	f.StringVar(&session, "session", "", "Attach to or create a persistent tmux session.")
	f.BoolVar(&login, "login", false, "Run the remote command in a login shell.")
	f.BoolVar(&printOnly, "print", false, "Print the SSH command instead of executing it.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		// Persistent flags must be present before splitting known/remote arguments.
		cmd.Flags().AddFlagSet(cmd.InheritedFlags())
		args, err := vmSSHPassthrough(cmd, args)
		if err != nil {
			return err
		}
		if err = prepareGlobalOptions(cmd, d); err != nil {
			return err
		}
		if help, _ := cmd.Flags().GetBool("help"); help {
			return cmd.Help()
		}
		if len(args) == 0 {
			return &ExitError{2, "Error: Missing argument 'VM_ID'."}
		}
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		ep, err := vmSSHEndpoint(cmd.Context(), cfg, api, orgID, args[0])
		if err != nil {
			return err
		}
		host, user, port, err := vmSSHAddress(ep)
		if err != nil {
			return err
		}
		remote := args[1:]
		extra := []string{}
		if session != "" {
			if err = validateVMSession(session); err != nil {
				return err
			}
			if login && len(remote) > 0 {
				remote = []string{vmLoginCommand(remote)}
			}
			remote = append([]string{"tmux", "new-session", "-A", "-s", session}, remote...)
			extra = []string{"-t"}
		} else if login && len(remote) > 0 {
			remote = []string{vmLoginCommand(remote)}
		}
		if printOnly {
			argv := append(vmSSHArgv(host, user, port, identity, "", extra), remote...)
			_, err = fmt.Fprintln(d.Out, vmShellJoin(argv))
			return err
		}
		known, err := vmTempKnownHosts(ep, host, port)
		if err != nil {
			return err
		}
		if known != "" {
			defer func() { _ = os.Remove(known) }()
		} else {
			if _, err = fmt.Fprintln(d.Err, "Warning: this VM's endpoint has no SSH host key; the connection falls back to trust-on-first-use instead of pinning."); err != nil {
				return err
			}
		}
		argv := append(vmSSHArgv(host, user, port, identity, known, extra), remote...)
		restore := sshOwnsInterrupt(cmd.Context())
		defer restore()
		runner := d.RunSSH
		if runner == nil {
			runner = func(ctx context.Context, args []string) (int, error) { return executeVMSSH(ctx, d, args) }
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
func executeVMSSH(ctx context.Context, d Dependencies, args []string) (int, error) {
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
		return vmSSHExitCode(exit), nil
	}
	return 1, fmt.Errorf("failed to launch ssh: %w", err)
}
