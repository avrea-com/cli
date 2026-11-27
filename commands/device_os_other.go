//go:build !windows

package commands

import (
	"os"
	"runtime"
)

func detectDeviceOS() (string, error) {
	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile("/etc/os-release")
		if err != nil {
			data, err = os.ReadFile("/usr/lib/os-release")
		}
		if err != nil {
			return "", err
		}
		return parseOSRelease(data), nil
	case "darwin":
		data, err := os.ReadFile("/System/Library/CoreServices/SystemVersion.plist")
		if err != nil {
			return "", err
		}
		return parseSystemVersionPlist(data), nil
	default:
		return "", nil
	}
}
