//go:build windows

package commands

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func detectDeviceOS() (string, error) {
	version := windows.RtlGetVersion()
	return fmt.Sprintf("Windows %d.%d.%d", version.MajorVersion, version.MinorVersion, version.BuildNumber), nil
}
