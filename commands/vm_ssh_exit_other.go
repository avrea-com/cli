//go:build !unix

package commands

import "os/exec"

func vmSSHExitCode(exit *exec.ExitError) int { return exit.ExitCode() }
