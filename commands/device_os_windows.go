//go:build windows

package commands

import "golang.org/x/sys/windows"

func detectDeviceOS() (string, error) {
	version := windows.RtlGetVersion()
	return windowsVersionName(version.MajorVersion, version.MinorVersion, version.BuildNumber), nil
}
