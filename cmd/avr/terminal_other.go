//go:build !windows

package main

import "os"

func enableVirtualTerminal(...*os.File) {}
