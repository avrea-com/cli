//go:build unix

package commands

import (
	"errors"
	"os/signal"
	"syscall"
)

// IgnoreBrokenPipe lets command handlers treat a closed consumer as a normal
// end of output. Call only from an executable's main, never during composition.
func IgnoreBrokenPipe() { signal.Ignore(syscall.SIGPIPE) }

func isBrokenPipe(err error) bool { return errors.Is(err, syscall.EPIPE) }
