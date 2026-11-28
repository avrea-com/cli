//go:build !(aix || dragonfly || freebsd || netbsd || openbsd || solaris || linux || darwin || windows)

package commands

func detectDeviceOS() (string, error) { return "", nil }
