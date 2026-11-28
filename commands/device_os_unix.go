//go:build aix || dragonfly || freebsd || netbsd || openbsd || solaris

package commands

import "golang.org/x/sys/unix"

func detectDeviceOS() (string, error) {
	var info unix.Utsname
	if err := unix.Uname(&info); err != nil {
		return "", err
	}
	return unix.ByteSliceToString(info.Sysname[:]) + " " + unix.ByteSliceToString(info.Release[:]), nil
}
