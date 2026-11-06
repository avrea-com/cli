package main

import (
	"golang.org/x/sys/windows"
	"os"
	"testing"
)

func TestClassicConsoleEnablesVTAndPreservesFlags(t *testing.T) {
	// CI often has only redirected handles. Allocate a real classic console
	// so this test exercises the Windows API rather than mocking mode flags.
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	allocated, _, _ := kernel.NewProc("AllocConsole").Call()
	if allocated != 0 {
		defer func() { _, _, _ = kernel.NewProc("FreeConsole").Call() }()
	}
	console, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		t.Fatal("open classic console", err)
	}
	defer func() { _ = console.Close() }()
	handle := windows.Handle(console.Fd())
	var original uint32
	if err := windows.GetConsoleMode(handle, &original); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.SetConsoleMode(handle, original) }()
	initial := (original | windows.ENABLE_WRAP_AT_EOL_OUTPUT) &^ windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if err := windows.SetConsoleMode(handle, initial); err != nil {
		t.Fatal(err)
	}
	enableVirtualTerminal(console)
	var got uint32
	if err := windows.GetConsoleMode(handle, &got); err != nil {
		t.Fatal(err)
	}
	want := initial | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING | windows.ENABLE_PROCESSED_OUTPUT
	if got != want {
		t.Fatalf("console mode %x, want %x", got, want)
	}
	file, err := os.CreateTemp(t.TempDir(), "redirected")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	enableVirtualTerminal(file)
	if _, err := file.WriteString("plain output"); err != nil {
		t.Fatal(err)
	}
}
