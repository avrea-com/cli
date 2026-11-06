//go:build unix

package commands

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
)

// IgnoreBrokenPipe lets command handlers handle EPIPE without passing an
// ignored SIGPIPE disposition to child programs. Call only from main.
func IgnoreBrokenPipe() { signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE) }

func isBrokenPipe(err error) bool { return errors.Is(err, syscall.EPIPE) }
