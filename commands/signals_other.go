//go:build !unix && !windows

package commands

// IgnoreBrokenPipe is unnecessary on platforms without Unix SIGPIPE handling.
func IgnoreBrokenPipe() {}

func isBrokenPipe(error) bool { return false }
