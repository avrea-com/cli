//go:build !unix

package commands

import (
	"os"
	"os/exec"
)

func vmSSHExitCode(exit *exec.ExitError) int { return exit.ExitCode() }

func vmTerminate(process *os.Process) error { return process.Kill() }
