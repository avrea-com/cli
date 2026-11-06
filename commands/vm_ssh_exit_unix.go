//go:build unix

package commands

import (
	"os"
	"os/exec"
	"syscall"
)

func vmSSHExitCode(exit *exec.ExitError) int {
	if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		// Python passes subprocess's negative signal return code to sys.exit.
		return 256 - int(status.Signal())
	}
	return exit.ExitCode()
}

func vmTerminate(process *os.Process) error { return process.Signal(syscall.SIGTERM) }
