package commands

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"
)

type vmTunnelProcess struct {
	done chan struct{}
	err  error
}

func vmStartProcess(ctx context.Context, d Dependencies, args []string) (*vmTunnelProcess, error) {
	child := exec.CommandContext(ctx, args[0], args[1:]...)
	child.Stdin = d.In
	child.Stdout = d.Out
	child.Stderr = d.Err
	child.Cancel = func() error { return vmTerminate(child.Process) }
	child.WaitDelay = 5 * time.Second
	if err := child.Start(); err != nil {
		return nil, err
	}
	proc := &vmTunnelProcess{done: make(chan struct{})}
	go func() { proc.err = child.Wait(); close(proc.done) }()
	return proc, nil
}
func (p *vmTunnelProcess) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return nil
	}
}
func (p *vmTunnelProcess) exitStatus() (int, bool) {
	select {
	case <-p.done:
		if p.err == nil {
			return 0, true
		}
		var exit *exec.ExitError
		if errors.As(p.err, &exit) {
			return exit.ExitCode(), true
		}
		return 1, true
	default:
		return 0, false
	}
}
func vmTunnelListening(ctx context.Context, proc *vmTunnelProcess, forwards []vmForward, timeout time.Duration, d Dependencies) (bool, error) {
	deadline := d.Now().Add(timeout)
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	for d.Now().Before(deadline) {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if _, exited := proc.exitStatus(); exited {
			return false, nil
		}
		ready := true
		for _, forward := range forwards {
			_, network, address := forward.listen()
			connection, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				ready = false
				break
			}
			_ = connection.Close()
		}
		if ready {
			return true, nil
		}
		if err := d.Wait(ctx, 250*time.Millisecond); err != nil {
			return false, err
		}
	}
	return false, nil
}

// vmTunnel is a running `ssh -N` port forward.
type vmTunnel struct {
	*vmTunnelProcess
	cancel context.CancelFunc
}

// close stops ssh and waits for it to exit.
func (t *vmTunnel) close() {
	t.cancel()
	<-t.done
}

// vmStartTunnel launches argv, an `ssh -N` command, and waits up to timeout
// for every forward's local end to accept a connection. ssh runs until close
// or until parent ends, so a caller can keep the tunnel past ctx. When ssh
// exits or the forwards do not come up in time, the tunnel is already closed
// and ready is false.
func vmStartTunnel(ctx, parent context.Context, d Dependencies, argv []string, forwards []vmForward, timeout time.Duration) (tunnel *vmTunnel, ready bool, err error) {
	childCtx, cancel := context.WithCancel(parent)
	proc, err := vmStartProcess(childCtx, d, argv)
	if err != nil {
		cancel()
		return nil, false, &ExitError{1, "Error: failed to launch ssh: " + err.Error()}
	}
	tunnel = &vmTunnel{proc, cancel}
	if ready, err = vmTunnelListening(ctx, proc, forwards, timeout, d); err != nil || !ready {
		tunnel.close()
		return nil, false, err
	}
	return tunnel, true, nil
}

func vmRunTunnel(ctx context.Context, d Dependencies, ep map[string]any, forwards []vmForward, identity string, onReady func(*vmTunnelProcess) error) (err error) {
	defer func() {
		if ctx.Err() != nil {
			err = &ExitError{130, "\nClosing tunnel."}
		}
	}()
	host, user, port, err := vmSSHAddress(ep)
	if err != nil {
		return err
	}
	known, err := vmTempKnownHosts(ep, host, port)
	if err != nil {
		return err
	}
	if known != "" {
		defer func() { _ = os.Remove(known) }()
	} else {
		if _, err = fmt.Fprintln(d.Err, "Warning: this VM's endpoint has no SSH host key; the tunnel falls back to trust-on-first-use instead of pinning."); err != nil {
			return err
		}
	}
	tunnel, ready, err := vmStartTunnel(ctx, ctx, d, vmTunnelArgv(host, user, port, forwards, identity, known), forwards, 60*time.Second)
	if err != nil {
		return err
	}
	if !ready {
		return &ExitError{1, "Error: SSH tunnel did not come up. A local port may be busy (bind a different one — for port-forward use --port LOCAL:GUEST) or the VM may be unreachable (check `avr vm show`)."}
	}
	defer tunnel.close()
	if err = onReady(tunnel.vmTunnelProcess); err != nil {
		return err
	}
	if status, exited := tunnel.exitStatus(); exited && status > 0 {
		return &ExitError{1, fmt.Sprintf("Error: SSH tunnel exited unexpectedly (status %d); the connection was lost.", status)}
	}
	return nil
}
func vmLaunchDesktop(ctx context.Context, d Dependencies, args []string, detaches bool, tunnel *vmTunnelProcess) error {
	if len(args) == 0 {
		if _, err := fmt.Fprintln(d.Err, "No auto-launch client is assumed on this platform; connect manually."); err != nil {
			return err
		}
		return tunnel.wait(ctx)
	}
	// A detached OS launcher owns its application after handing off. Reap the
	// launcher, but keep the SSH tunnel under this command's lifetime.
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	child, err := vmStartProcess(childCtx, d, args)
	if err != nil {
		if _, err = fmt.Fprintf(d.Err, "Could not launch %s: %s. Connect manually.\n", args[0], err); err != nil {
			return err
		}
		return tunnel.wait(ctx)
	}
	defer func() { cancel(); <-child.done }()
	if detaches {
		if _, err = fmt.Fprintln(d.Err, "Launched the desktop client. Press Ctrl-C to close the tunnel when done."); err != nil {
			return err
		}
		return tunnel.wait(ctx)
	}
	// A lost SSH transport must not leave a desktop session waiting forever.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-child.done:
		return nil
	case <-tunnel.done:
		return nil
	}
}
