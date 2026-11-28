package commands

import (
	"os"

	"golang.org/x/sys/unix"
)

func detectDeviceOS() (string, error) {
	if version, err := unix.Sysctl("kern.osproductversion"); err == nil && version != "" {
		return "macOS " + version, nil
	}
	data, err := os.ReadFile("/System/Library/CoreServices/SystemVersion.plist")
	if err != nil {
		return "", err
	}
	return parseSystemVersionPlist(data), nil
}
