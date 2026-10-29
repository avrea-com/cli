package commands

import (
	"errors"

	"golang.org/x/sys/windows"
)

// IgnoreBrokenPipe is unnecessary on Windows.
func IgnoreBrokenPipe() {}

func isBrokenPipe(err error) bool {
	return errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_NO_DATA)
}
