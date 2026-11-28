package commands

import "os"

func detectDeviceOS() (string, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		data, err = os.ReadFile("/usr/lib/os-release")
	}
	if err != nil {
		return "", err
	}
	return parseOSRelease(data), nil
}
