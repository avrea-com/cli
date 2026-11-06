package main

import (
	"golang.org/x/sys/windows"
	"os"
)

// Classic Windows consoles need VT processing enabled to render ANSI colors,
// underlines and the login QR code. Pipes and files have no console mode.
func enableVirtualTerminal(files ...*os.File) {
	for _, file := range files {
		handle := windows.Handle(file.Fd())
		var mode uint32
		if windows.GetConsoleMode(handle, &mode) == nil {
			_ = windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.ENABLE_PROCESSED_OUTPUT)
		}
	}
}
